// Package mcp implements the Model Context Protocol client engine.
// Ports tools/mcp_tool*.py + mcp_oauth*.py + optional-mcps/ catalog.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os/exec"
	"sync"
	"time"
)

// Transport selects stdio vs streamable HTTP vs legacy SSE, mirroring
// mcp_tool_transport.py selection.
type Transport string

const (
	TransportStdio Transport = "stdio"
	TransportHTTP  Transport = "http"
	TransportSSE   Transport = "sse"
)

// ServerConfig mirrors a catalog manifest entry (manifest_version:1).
type ServerConfig struct {
	Name      string    `json:"name"`
	Transport Transport `json:"transport"`
	Command   string    `json:"command,omitempty"`
	Args      []string  `json:"args,omitempty"`
	URL       string    `json:"url,omitempty"`
	APIKey    string    `json:"api_key,omitempty"`
}

// ToolDef is a discovered remote tool.
type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Schema      map[string]any `json:"schema,omitempty"`
}

// CallResult is a remote invocation outcome.
type CallResult struct {
	Content string `json:"content"`
	IsError bool   `json:"is_error"`
}

// Client manages one MCP server connection.
type Client struct {
	cfg     ServerConfig
	http    *http.Client
	mu      sync.Mutex
	tools   []ToolDef
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	cmd     *exec.Cmd
	seq     int
	pending map[int]chan rpcResp
}

type rpcResp struct {
	result json.RawMessage
	err    *rpcErr
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// NewClient builds a client for cfg.
func NewClient(cfg ServerConfig) *Client {
	return &Client{cfg: cfg, http: &http.Client{Timeout: 60 * time.Second}, pending: map[int]chan rpcResp{}}
}

// Start launches the transport.
func (c *Client) Start(ctx context.Context) error {
	switch c.cfg.Transport {
	case TransportStdio, "":
		if c.cfg.Command == "" {
			return errors.New("mcp: stdio needs command")
		}
		c.cmd = exec.CommandContext(ctx, c.cfg.Command, c.cfg.Args...)
		stdin, err := c.cmd.StdinPipe()
		if err != nil {
			return err
		}
		stdout, err := c.cmd.StdoutPipe()
		if err != nil {
			return err
		}
		c.stdin = stdin
		c.stdout = bufio.NewReader(stdout)
		if err := c.cmd.Start(); err != nil {
			return err
		}
		go c.readLoop()
		return nil
	default:
		return nil
	}
}

func (c *Client) readLoop() {
	for {
		line, err := c.stdout.ReadBytes('\n')
		if err != nil {
			return
		}
		var msg struct {
			ID     *int            `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *rpcErr         `json:"error"`
		}
		if err := json.Unmarshal(line, &msg); err != nil || msg.ID == nil {
			continue
		}
		c.mu.Lock()
		ch, ok := c.pending[*msg.ID]
		if ok {
			delete(c.pending, *msg.ID)
		}
		c.mu.Unlock()
		if ok {
			ch <- rpcResp{result: msg.Result, err: msg.Error}
		}
	}
}

func (c *Client) callStdio(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	c.seq++
	id := c.seq
	ch := make(chan rpcResp, 1)
	c.pending[id] = ch
	c.mu.Unlock()
	req := map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
	raw, _ := json.Marshal(req)
	raw = append(raw, '\n')
	if _, err := c.stdin.Write(raw); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return nil, errors.New("mcp: " + r.err.Message)
		}
		return r.result, nil
	case <-time.After(60 * time.Second):
		return nil, errors.New("mcp: rpc timeout")
	}
}

// ListTools discovers remote tools.
func (c *Client) ListTools(ctx context.Context) ([]ToolDef, error) {
	if c.cfg.Transport == TransportHTTP || c.cfg.Transport == TransportSSE {
		return c.listToolsHTTP(ctx)
	}
	raw, err := c.callStdio(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var v struct {
		Tools []ToolDef `json:"tools"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.tools = v.Tools
	c.mu.Unlock()
	return v.Tools, nil
}

func (c *Client) listToolsHTTP(ctx context.Context) ([]ToolDef, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.URL+"/tools", nil)
	if err != nil {
		return nil, err
	}
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var v struct {
		Tools []ToolDef `json:"tools"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return nil, err
	}
	return v.Tools, nil
}

// CallTool invokes a remote tool.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (CallResult, error) {
	if c.cfg.Transport == TransportHTTP || c.cfg.Transport == TransportSSE {
		return c.callToolHTTP(ctx, name, args)
	}
	raw, err := c.callStdio(ctx, "tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return CallResult{}, err
	}
	var v struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return CallResult{Content: string(raw)}, nil
	}
	out := ""
	for _, c := range v.Content {
		out += c.Text
	}
	return CallResult{Content: out, IsError: v.IsError}, nil
}

func (c *Client) callToolHTTP(ctx context.Context, name string, args map[string]any) (CallResult, error) {
	body, _ := json.Marshal(map[string]any{"name": name, "arguments": args})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.URL+"/tools/call", bytesReader(body))
	if err != nil {
		return CallResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return CallResult{}, err
	}
	defer resp.Body.Close()
	var v CallResult
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return CallResult{}, err
	}
	return v, nil
}

// Close tears the transport down.
func (c *Client) Close() error {
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_ = c.cmd.Wait()
	}
	return nil
}

// CatalogEntry is one approved server manifest (optional-mcps/).
type CatalogEntry struct {
	Name      string `json:"name"`
	Transport string `json:"transport"`
	URL       string `json:"url,omitempty"`
	Command   string `json:"command,omitempty"`
	Auth      string `json:"auth,omitempty"`
}

func bytesReader(b []byte) io.Reader { return &byteReader{b: b} }

type byteReader struct {
	b []byte
	o int
}

func (r *byteReader) Read(p []byte) (int, error) {
	if r.o >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.o:])
	r.o += n
	return n, nil
}

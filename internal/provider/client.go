package provider

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	json "github.com/goccy/go-json"

	"github.com/nousresearch/botreaper/pkg/types"
)

// ChatRequest is the OpenAI-compatible wire request.
type ChatRequest struct {
	Model       string         `json:"model"`
	Messages    []WireMessage  `json:"messages"`
	Tools       []WireTool     `json:"tools,omitempty"`
	ToolChoice  any            `json:"tool_choice,omitempty"`
	Stream      bool           `json:"stream"`
	Temperature *float64       `json:"temperature,omitempty"`
	MaxTokens   int            `json:"max_tokens,omitempty"`
	Extra       map[string]any `json:"-"`
}

// WireMessage is the API-facing message.
type WireMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	Name       string         `json:"name,omitempty"`
	ToolCalls  []WireToolCall `json:"tool_calls,omitempty"`
}

// WireToolCall is the API-facing invocation.
type WireToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// WireTool is the API-facing definition.
type WireTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	} `json:"function"`
}

// Client performs chat completions with SSE streaming, honoring ctx
// cancellation (Ctrl+C tears down the in-flight HTTP request).
type Client struct {
	http    *http.Client
	profile Profile
	apiKey  string
	baseURL string
}

// NewClient builds a provider client.
func NewClient(p Profile, apiKey, baseURLOverride string) *Client {
	base := p.BaseURL
	if baseURLOverride != "" {
		base = strings.TrimRight(baseURLOverride, "/")
	}
	return &Client{
		http:    &http.Client{Timeout: 120 * time.Second},
		profile: p,
		apiKey:  apiKey,
		baseURL: base,
	}
}

// StreamFunc receives token deltas as they arrive.
type StreamFunc func(text string)

// Complete sends one non-streaming request (retries/forks use this).
func (c *Client) Complete(ctx context.Context, req ChatRequest) (types.AssistantTurn, error) {
	var turn types.AssistantTurn
	err := c.doStream(ctx, req, func(string) {})
	if err != nil {
		return turn, err
	}
	return turn, errors.New("provider: use Stream for turns")
}

// Stream posts a chat request and invokes onToken per SSE delta, returning
// the fully assembled turn. The system prompt bytes are passed through
// untouched to preserve KV-cache reuse.
func (c *Client) Stream(ctx context.Context, req ChatRequest, onToken StreamFunc) (types.AssistantTurn, error) {
	var turn types.AssistantTurn
	req.Stream = true
	body, err := json.Marshal(req)
	if err != nil {
		return turn, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return turn, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		switch c.profile.AuthType {
		case "x-api-key":
			httpReq.Header.Set("x-api-key", c.apiKey)
		case "key":
			q := httpReq.URL.Query()
			q.Set("key", c.apiKey)
			httpReq.URL.RawQuery = q.Encode()
		case "none":
		default:
			httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
		}
	}
	for k, v := range c.profile.DefaultHeaders {
		httpReq.Header.Set(k, v)
	}
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return turn, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return turn, errors.New("provider: status " + resp.Status + ": " + string(raw))
	}
	return turn, c.readSSE(resp.Body, onToken, &turn)
}

func (c *Client) doStream(ctx context.Context, req ChatRequest, fn StreamFunc) error {
	_, err := c.Stream(ctx, req, fn)
	return err
}

type sseChunk struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			Reasoning string `json:"reasoning_content"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Content   string         `json:"content"`
			ToolCalls []WireToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func (c *Client) readSSE(r io.Reader, onToken StreamFunc, turn *types.AssistantTurn) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var content, reasoning strings.Builder
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}
		var ch sseChunk
		if err := json.Unmarshal([]byte(payload), &ch); err != nil {
			continue
		}
		for _, choice := range ch.Choices {
			if choice.Delta.Content != "" {
				content.WriteString(choice.Delta.Content)
				onToken(choice.Delta.Content)
			}
			if choice.Delta.Reasoning != "" {
				reasoning.WriteString(choice.Delta.Reasoning)
			}
			if choice.Message.Content != "" {
				content.WriteString(choice.Message.Content)
				onToken(choice.Message.Content)
			}
			for _, tc := range choice.Message.ToolCalls {
				turn.ToolCalls = append(turn.ToolCalls, types.ToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments})
			}
			if choice.FinishReason != "" {
				turn.Finish = choice.FinishReason
			}
		}
		if ch.Usage != nil {
			turn.PromptTok = ch.Usage.PromptTokens
			turn.ComplTok = ch.Usage.CompletionTokens
		}
	}
	turn.Content = content.String()
	turn.Reasoning = reasoning.String()
	return sc.Err()
}

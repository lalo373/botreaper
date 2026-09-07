package ws

import (
	"context"
	"sync"
	"time"

	json "github.com/goccy/go-json"
	"nhooyr.io/websocket"

	"github.com/nousresearch/botreaper/pkg/types"
)

// Coalescer merges rapid token/reasoning/thinking deltas on a 33ms flush
// timer, mirroring the TS client's coalescing. Hot deltas batch; tool and
// pty frames bypass untouched for minimal tail latency.
type Coalescer struct {
	mu      sync.Mutex
	hub     *Hub
	sess    string
	pending map[string][]byte
	timer   *time.Timer
	closed  bool
}

// NewCoalescer binds a coalescer to a session on hub.
func NewCoalescer(hub *Hub, sess string) *Coalescer {
	return &Coalescer{hub: hub, sess: sess, pending: map[string][]byte{}}
}

// Add queues a delta. Returns true when the frame was coalesced.
func (c *Coalescer) Add(typ string, text []byte) bool {
	switch typ {
	case types.EvTokenDelta, types.EvReasoningDelta, types.EvThinkingDelta:
	default:
		c.hub.Publish(c.sess, typ, types.KindEvent, text)
		return false
	}
	c.mu.Lock()
	c.pending[typ] = append(c.pending[typ], text...)
	if c.timer == nil {
		c.timer = time.AfterFunc(CoalesceMillis*time.Millisecond, c.flush)
	}
	c.mu.Unlock()
	return true
}

func (c *Coalescer) flush() {
	c.mu.Lock()
	pending := c.pending
	c.pending = map[string][]byte{}
	c.timer = nil
	c.mu.Unlock()
	for typ, data := range pending {
		cp := make([]byte, len(data))
		copy(cp, data)
		c.hub.Publish(c.sess, typ, types.KindEvent, cp)
	}
}

// Flush forces pending deltas out (turn end).
func (c *Coalescer) Flush() {
	c.mu.Lock()
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	c.mu.Unlock()
	c.flush()
}

// Client is the in-process/local dialer used by the TUI and tests.
// The TUI never calls the engine directly; everything crosses this client.
type Client struct {
	ws   *websocket.Conn
	recv chan types.Envelope
}

// Dial opens /api/ws with ticket auth and starts the receive pump.
func Dial(ctx context.Context, url, ticket, sess string) (*Client, error) {
	if sess != "" {
		sep := "?"
		for _, r := range url {
			if r == '?' {
				sep = "&"
				break
			}
		}
		url += sep + "sess=" + sess
	}
	if ticket != "" {
		sep := "?"
		has := false
		for _, r := range url {
			if r == '?' {
				has = true
				break
			}
		}
		_ = sep
		if has {
			url += "&ticket=" + ticket
		} else {
			url += "?ticket=" + ticket
		}
	}
	dctx, cancel := context.WithTimeout(ctx, ConnectTimeoutSecs*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(dctx, url, nil)
	if err != nil {
		return nil, err
	}
	cl := &Client{ws: ws, recv: make(chan types.Envelope, 256)}
	go cl.pump(ctx)
	return cl, nil
}

func (c *Client) pump(ctx context.Context) {
	defer close(c.recv)
	for {
		mt, raw, err := c.ws.Read(ctx)
		if err != nil {
			return
		}
		if mt == websocket.MessageBinary {
			if e, derr := types.DecodeBinary(raw); derr == nil {
				select {
				case c.recv <- *e:
				default:
				}
			}
			continue
		}
		var env types.Envelope
		if err := json.Unmarshal(raw, &env); err == nil && env.Type != "" {
			select {
			case c.recv <- env:
			default:
			}
			continue
		}
		var resp RPCResponse
		if err := json.Unmarshal(raw, &resp); err == nil {
			raw2, _ := json.Marshal(map[string]any{"result": resp.Result, "error": resp.Error, "id": resp.ID})
			select {
			case c.recv <- types.Envelope{V: 1, Kind: types.KindRPCResponse, Type: "rpc.response", Data: raw2}:
			default:
			}
		}
	}
}

// Recv exposes inbound envelopes.
func (c *Client) Recv() <-chan types.Envelope { return c.recv }

// Call issues a JSON-RPC request.
func (c *Client) Call(ctx context.Context, id any, method, sess string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	req := RPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: raw, Sess: sess}
	out, err := json.Marshal(req)
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return c.ws.Write(wctx, websocket.MessageText, out)
}

// Notify issues a fire-and-forget call.
func (c *Client) Notify(ctx context.Context, method, sess string, params any) error {
	return c.Call(ctx, nil, method, sess, params)
}

// Close shuts the socket down.
func (c *Client) Close() error { return c.ws.Close(websocket.StatusNormalClosure, "") }

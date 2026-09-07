package ws

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	json "github.com/goccy/go-json"
	"nhooyr.io/websocket"

	"github.com/nousresearch/botreaper/pkg/types"
)

// Conn is one server-side peer: a readLoop pushing inbound and a writeLoop
// draining an outbound queue plus ping ticks. The Hub owns shared state.
type Conn struct {
	hub    *Hub
	ws     *websocket.Conn
	send   chan types.Envelope
	sess   string
	closed chan struct{}
	once   func()
}

// emit queues e for the writeLoop without blocking the publisher.
func (c *Conn) emit(e types.Envelope) {
	select {
	case c.send <- e:
	default:
		// Slow reader: drop; client recovers via session.events.since.
	}
}

// Server is the WS gateway. One http.ServeMux serves /api/ws (agent RPC +
// events), /api/pty (binary terminal) and /api/events (broadcast bus).
type Server struct {
	hub     *Hub
	handler map[string]RPCHandler
	token   string
	mux     *http.ServeMux
}

// RPCHandler serves one JSON-RPC method. It may publish progress events via h.
type RPCHandler func(ctx context.Context, h *Hub, sess string, params []byte) (any, error)

// RPCRequest is the wire form of an inbound call.
type RPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  []byte `json:"params,omitempty"`
	Sess    string `json:"sess,omitempty"`
}

// RPCResponse is the wire form of an outbound reply.
type RPCResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *RPCError `json:"error,omitempty"`
}

// RPCError mirrors JSON-RPC error objects.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// NewServer builds a gateway bound to hub. Token "" disables auth (tests).
func NewServer(hub *Hub, token string) *Server {
	s := &Server{hub: hub, handler: map[string]RPCHandler{}, token: token, mux: http.NewServeMux()}
	s.mux.HandleFunc("/api/ws", s.handleWS)
	s.mux.HandleFunc("/api/events", s.handleWS)
	s.mux.HandleFunc("/api/pty", s.handlePTY)
	s.mux.HandleFunc("/api/auth/ws-ticket", s.handleTicket)
	return s
}

// On registers a method handler (data-driven table, never an if-ladder).
func (s *Server) On(method string, h RPCHandler) { s.handler[method] = h }

// Handler returns the mux for http.Serve.
func (s *Server) Handler() http.Handler { return s.mux }

// Serve starts listening with TCP_NODELAY + keepalive parity.
func (s *Server) Serve(ctx context.Context, addr string) error {
	srv := &http.Server{
		Addr:        addr,
		Handler:     s.mux,
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	return srv.ListenAndServe()
}

func (s *Server) authed(r *http.Request) bool {
	if s.token == "" {
		return true
	}
	if tok := r.Header.Get("X-BotReaper-Session-Token"); tok == s.token {
		return true
	}
	if q := r.URL.Query().Get("ticket"); q == s.token {
		return true
	}
	if c, err := r.Cookie("botreaper_session"); err == nil && c.Value == s.token {
		return true
	}
	return false
}

func (s *Server) handleTicket(w http.ResponseWriter, r *http.Request) {
	if !s.authed(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ticket": s.token})
}

func (s *Server) profileScope(r *http.Request) string {
	if p := r.URL.Query().Get("profile"); p != "" {
		return p
	}
	return "default"
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if !s.authed(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: false})
	if err != nil {
		return
	}
	defer ws.Close(websocket.StatusNormalClosure, "")
	ws.SetReadLimit(8 << 20)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	c := &Conn{hub: s.hub, ws: ws, send: make(chan types.Envelope, 256), closed: make(chan struct{})}
	c.once = cancel
	sess := r.URL.Query().Get("sess")
	if sess == "" {
		sess = s.profileScope(r)
	}
	c.sess = sess
	s.hub.Subscribe(c, sess)
	s.hub.Subscribe(c, "*")
	defer s.hub.Unsubscribe(c)

	go c.writeLoop(ctx)
	c.readLoop(ctx, s)
}

func (s *Server) handlePTY(w http.ResponseWriter, r *http.Request) {
	if !s.authed(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{})
	if err != nil {
		return
	}
	defer ws.Close(websocket.StatusNormalClosure, "")
	ctx := r.Context()
	// PTY socket is raw binary; greet then echo control acks. Real terminal
	// bytes arrive via Hub.Publish(KindPty) from the sandbox pumps.
	for {
		mt, raw, err := ws.Read(ctx)
		if err != nil {
			return
		}
		if mt == websocket.MessageBinary && len(raw) > 0 {
			if strings.HasPrefix(string(raw), "\x1b[RESIZE]") {
				continue
			}
		}
	}
}

func (c *Conn) writeLoop(ctx context.Context) {
	ticker := time.NewTicker(PingIntervalSecs * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.closed:
			return
		case e := <-c.send:
			wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			var err error
			if e.Kind == types.KindPty {
				buf := types.AcquireBuffer()
				types.EncodeBinary(buf, &e)
				err = c.ws.Write(wctx, websocket.MessageBinary, buf.Bytes())
				types.ReleaseBuffer(buf)
			} else {
				raw, merr := json.Marshal(e)
				if merr == nil {
					err = c.ws.Write(wctx, websocket.MessageText, raw)
				} else {
					err = merr
				}
			}
			cancel()
			if err != nil {
				return
			}
		case <-ticker.C:
			pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			_ = c.ws.Ping(pctx)
			cancel()
		}
	}
}

func (c *Conn) readLoop(ctx context.Context, s *Server) {
	for {
		mt, raw, err := c.ws.Read(ctx)
		if err != nil {
			return
		}
		if mt == websocket.MessageBinary {
			if e, derr := types.DecodeBinary(raw); derr == nil {
				s.dispatch(ctx, c, e.Type, e.Data, nil)
			}
			continue
		}
		var req RPCRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			// Maybe a bare event envelope.
			var env types.Envelope
			if err2 := json.Unmarshal(raw, &env); err2 == nil && env.Type != "" {
				s.dispatch(ctx, c, env.Type, env.Data, nil)
			}
			continue
		}
		if req.Method == "" {
			continue
		}
		sess := req.Sess
		if sess == "" {
			sess = c.sess
		}
		s.dispatch(ctx, c, req.Method, req.Params, req.ID)
	}
}

func (s *Server) dispatch(ctx context.Context, c *Conn, method string, params []byte, id any) {
	h, ok := s.handler[method]
	if !ok {
		if id != nil {
			s.reply(ctx, c, id, nil, &RPCError{Code: -32601, Message: "method not found: " + method})
		}
		return
	}
	rctx, cancel := context.WithTimeout(ctx, RPCTimeoutSecs*time.Second)
	defer cancel()
	res, err := h(rctx, s.hub, c.sess, params)
	if id == nil {
		return // notification
	}
	if err != nil {
		s.reply(ctx, c, id, nil, &RPCError{Code: -32000, Message: err.Error()})
		return
	}
	s.reply(ctx, c, id, res, nil)
}

func (s *Server) reply(ctx context.Context, c *Conn, id any, result any, rerr *RPCError) {
	resp := RPCResponse{JSONRPC: "2.0", ID: id, Result: result, Error: rerr}
	raw, err := json.Marshal(resp)
	if err != nil {
		return
	}
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_ = c.ws.Write(wctx, websocket.MessageText, raw)
}

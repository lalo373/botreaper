package discord

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"nhooyr.io/websocket"

	"github.com/nousresearch/botreaper/internal/gateway"
)

type mockAPI struct {
	t       *testing.T
	mu      sync.Mutex
	posts   []json.RawMessage
	auth    []string
	fail429 bool
}

func (m *mockAPI) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/channels/c1" && r.Method == http.MethodGet:
		_, _ = w.Write([]byte(`{"id":"c1","type":1}`))
	case r.URL.Path == "/channels/g1" && r.Method == http.MethodGet:
		_, _ = w.Write([]byte(`{"id":"g1","type":0}`))
	case strings.HasSuffix(r.URL.Path, "/messages") && r.Method == http.MethodPost:
		m.mu.Lock()
		m.auth = append(m.auth, r.Header.Get("Authorization"))
		var raw json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&raw)
		m.posts = append(m.posts, raw)
		fail := m.fail429
		m.fail429 = false
		m.mu.Unlock()
		if fail {
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"retry_after":0.01,"global":false}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"m9"}`))
	case strings.HasSuffix(r.URL.Path, "/typing"):
		w.WriteHeader(http.StatusNoContent)
	default:
		_, _ = w.Write([]byte(`{}`))
	}
}

func newRESTServer(t *testing.T) (*mockAPI, *httptest.Server) {
	m := &mockAPI{t: t}
	return m, httptest.NewServer(http.HandlerFunc(m.handler))
}

func TestRESTSend(t *testing.T) {
	m, srv := newRESTServer(t)
	defer srv.Close()
	r := NewREST(Config{Token: "TOK", RESTBase: srv.URL, HTTP: srv.Client()})
	sent, err := r.SendMessage(context.Background(), "c1", "hi", "m1")
	if err != nil || sent.ID != "m9" {
		t.Fatalf("send = %+v %v", sent, err)
	}
	if len(m.posts) != 1 || len(m.auth) != 1 || m.auth[0] != "Bot TOK" {
		t.Fatalf("posts=%d auth=%v", len(m.posts), m.auth)
	}
	var p map[string]any
	_ = json.Unmarshal(m.posts[0], &p)
	ref := p["message_reference"].(map[string]any)
	if ref["message_id"] != "m1" || ref["fail_if_not_exists"] != false {
		t.Fatalf("reference = %v", ref)
	}
}

func TestRESTSplit(t *testing.T) {
	m, srv := newRESTServer(t)
	defer srv.Close()
	a := &Adapter{rest: NewREST(Config{Token: "T", RESTBase: srv.URL, HTTP: srv.Client()})}
	if err := a.Send(context.Background(), "c1", strings.Repeat("y", 5000)); err != nil {
		t.Fatal(err)
	}
	if len(m.posts) != 3 {
		t.Fatalf("posts = %d, want 3", len(m.posts))
	}
}

func TestRESTRetry429(t *testing.T) {
	m, srv := newRESTServer(t)
	defer srv.Close()
	m.fail429 = true
	r := NewREST(Config{Token: "T", RESTBase: srv.URL, HTTP: srv.Client()})
	if _, err := r.SendMessage(context.Background(), "c1", "hi", ""); err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if len(m.posts) != 2 {
		t.Fatalf("posts = %d, want 2 (try+retry)", len(m.posts))
	}
}

func TestSplitCap(t *testing.T) {
	chunks := splitMessage(strings.Repeat("z", 2000*10))
	if len(chunks) != 8 || !strings.Contains(chunks[7], "truncated") {
		t.Fatalf("chunks = %d", len(chunks))
	}
}

// gatewayScript drives a scripted mock gateway: HELLO → expect IDENTIFY →
// READY → scripted dispatches.
type gatewayScript struct {
	t        *testing.T
	identify json.RawMessage
	frames   []string
	close    *websocket.StatusCode
}

func (s *gatewayScript) serve(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer ws.Close(websocket.StatusNormalClosure, "")
	ctx := r.Context()
	write := func(v any) {
		raw, _ := json.Marshal(v)
		_ = ws.Write(ctx, websocket.MessageText, raw)
	}
	write(map[string]any{"op": opHello, "d": map[string]any{"heartbeat_interval": 100}})
	_, raw, err := ws.Read(ctx)
	if err != nil {
		return
	}
	s.identify = raw
	n := 0
	write(map[string]any{"op": opDispatch, "s": &n, "t": "READY", "d": map[string]any{
		"session_id": "sess1", "resume_gateway_url": "", "user": map[string]any{"id": "self123"},
	}})
	for _, f := range s.frames {
		n++
		seq := n
		write(map[string]any{"op": opDispatch, "s": seq, "t": "MESSAGE_CREATE", "d": json.RawMessage(f)})
		time.Sleep(20 * time.Millisecond)
	}
	if s.close != nil {
		ws.Close(*s.close, "test")
		return
	}
	<-ctx.Done()
}

func wsURL(srv *httptest.Server, path string) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") + path
}

func TestGatewayDMFlow(t *testing.T) {
	m, srv := newRESTServer(t)
	defer srv.Close()
	script := &gatewayScript{t: t, frames: []string{
		`{"id":"m1","channel_id":"c1","type":0,"content":"hello","author":{"id":"u1","bot":false,"username":"bob"}}`,
	}}
	mux := http.NewServeMux()
	mux.HandleFunc("/gw", script.serve)
	mux.HandleFunc("/", m.handler)
	front := httptest.NewServer(mux)
	defer front.Close()

	got := make(chan gateway.Inbound, 4)
	rest := NewREST(Config{Token: "TOK", RESTBase: front.URL, HTTP: front.Client()})
	conn := NewConn(rest, "TOK", wsURL(front, "/gw"), func(ctx context.Context, in gateway.Inbound) { got <- in })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = conn.Run(ctx) }()

	select {
	case in := <-got:
		if in.Platform != "discord" || in.ChatID != "c1" || in.Text != "hello" || in.UserID != "u1" {
			t.Fatalf("inbound = %+v", in)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no inbound dispatched")
	}
	// IDENTIFY carried token + intents.
	var id struct {
		Op int `json:"op"`
		D  struct {
			Token   string `json:"token"`
			Intents int    `json:"intents"`
		} `json:"d"`
	}
	if err := json.Unmarshal(script.identify, &id); err != nil || id.Op != opIdentify || id.D.Token != "TOK" || id.D.Intents != intentBits {
		t.Fatalf("identify = %s", script.identify)
	}
	if conn.SelfID() != "self123" {
		t.Fatalf("self = %q", conn.SelfID())
	}
	cancel()
}

func TestGatewayMentionGate(t *testing.T) {
	_, srv := newRESTServer(t)
	defer srv.Close()
	script := &gatewayScript{t: t, frames: []string{
		// no mention → dropped
		`{"id":"m1","channel_id":"g1","guild_id":"G","type":0,"content":"hey all","author":{"id":"u1","bot":false,"username":"a"}}`,
		// bot author → dropped
		`{"id":"m2","channel_id":"g1","guild_id":"G","type":0,"content":"<@self123> hi","mentions":[{"id":"self123"}],"author":{"id":"b1","bot":true,"username":"bot"}}`,
		// mention → passes, stripped
		`{"id":"m3","channel_id":"g1","guild_id":"G","type":0,"content":"<@self123> help me","mentions":[{"id":"self123"}],"author":{"id":"u2","bot":false,"username":"carol"}}`,
	}}
	mux := http.NewServeMux()
	mux.HandleFunc("/gw", script.serve)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"g1","type":0}`))
	})
	front := httptest.NewServer(mux)
	defer front.Close()

	got := make(chan gateway.Inbound, 4)
	rest := NewREST(Config{Token: "T", RESTBase: front.URL, HTTP: front.Client()})
	conn := NewConn(rest, "T", wsURL(front, "/gw"), func(ctx context.Context, in gateway.Inbound) { got <- in })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = conn.Run(ctx) }()

	select {
	case in := <-got:
		if in.Text != "help me" || in.UserID != "u2" {
			t.Fatalf("inbound = %+v", in)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("mentioned message not dispatched")
	}
	select {
	case extra := <-got:
		t.Fatalf("unexpected extra inbound: %+v", extra)
	case <-time.After(300 * time.Millisecond):
	}
	cancel()
}

func TestGatewayFatalClose(t *testing.T) {
	code := websocket.StatusCode(4004)
	script := &gatewayScript{t: t, close: &code}
	srv := httptest.NewServer(http.HandlerFunc(script.serve))
	defer srv.Close()
	rest := NewREST(Config{Token: "BAD", RESTBase: srv.URL, HTTP: srv.Client()})
	conn := NewConn(rest, "BAD", wsURL(srv, "/"), nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := conn.Run(ctx)
	var fatal *FatalError
	if err == nil || !asFatal(err, &fatal) {
		t.Fatalf("err = %v", err)
	}
}

func asFatal(err error, target **FatalError) bool {
	if f, ok := err.(*FatalError); ok {
		*target = f
		return true
	}
	return false
}

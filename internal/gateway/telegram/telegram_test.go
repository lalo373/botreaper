package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nousresearch/botreaper/internal/gateway"
)

type mockBot struct {
	t      *testing.T
	mu     sync.Mutex
	calls  map[string][]json.RawMessage
	update string
}

func newMockBot(t *testing.T, update string) (*mockBot, *httptest.Server) {
	m := &mockBot{t: t, calls: map[string][]json.RawMessage{}, update: update}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := strings.TrimPrefix(r.URL.Path, "/botTOKEN/")
		var raw json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&raw)
		m.mu.Lock()
		m.calls[method] = append(m.calls[method], raw)
		m.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch method {
		case "getMe":
			_, _ = w.Write([]byte(`{"ok":true,"result":{"id":99,"is_bot":true,"username":"testbot"}}`))
		case "getUpdates":
			if m.update != "" {
				_, _ = w.Write([]byte(`{"ok":true,"result":[` + m.update + `]}`))
				m.mu.Lock()
				m.update = ""
				m.mu.Unlock()
			} else {
				_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
			}
		case "sendMessage":
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":7,"date":1}}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
		}
	}))
	return m, srv
}

func (m *mockBot) payloads(method string) []json.RawMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]json.RawMessage{}, m.calls[method]...)
}

const textUpdate = `{"update_id":10,"message":{"message_id":5,"date":1,"text":"hello","chat":{"id":-1001,"type":"supergroup"},"from":{"id":42,"is_bot":false,"first_name":"Ada"}}}`

func TestPollNormalizesMessage(t *testing.T) {
	_, srv := newMockBot(t, textUpdate)
	defer srv.Close()
	got := make(chan gateway.Inbound, 1)
	a := New(Config{Token: "TOKEN", BaseURL: srv.URL, DropPending: false, PollTimeout: 1}, func(ctx context.Context, in gateway.Inbound) {
		got <- in
	})
	a.client.cfg.HTTP = srv.Client()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = a.Start(ctx) }()
	select {
	case in := <-got:
		if in.Platform != "telegram" || in.ChatID != "-1001" || in.Text != "hello" || in.UserID != "42" || in.MessageID != "5" {
			t.Fatalf("inbound = %+v", in)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no inbound dispatched")
	}
	cancel()
	if a.BotID() != 99 {
		t.Fatalf("bot id = %d", a.BotID())
	}
}

func TestSendReplyAnchors(t *testing.T) {
	m, srv := newMockBot(t, "")
	defer srv.Close()
	a := New(Config{Token: "TOKEN", BaseURL: srv.URL}, nil)
	a.client.cfg.HTTP = srv.Client()
	in := gateway.Inbound{Platform: "telegram", ChatID: "-1001", MessageID: "5", ThreadID: "3"}
	if err := a.SendReply(context.Background(), in, "hi"); err != nil {
		t.Fatal(err)
	}
	got := m.payloads("sendMessage")
	if len(got) != 1 {
		t.Fatalf("sends = %d", len(got))
	}
	var p map[string]any
	_ = json.Unmarshal(got[0], &p)
	if p["reply_to_message_id"] != float64(5) || p["message_thread_id"] != float64(3) {
		t.Fatalf("payload = %v", p)
	}
}

func TestSendReplyDropsGeneralThread(t *testing.T) {
	m, srv := newMockBot(t, "")
	defer srv.Close()
	a := New(Config{Token: "TOKEN", BaseURL: srv.URL}, nil)
	a.client.cfg.HTTP = srv.Client()
	if err := a.SendReply(context.Background(), gateway.Inbound{ChatID: "1", ThreadID: "1"}, "hi"); err != nil {
		t.Fatal(err)
	}
	var p map[string]any
	_ = json.Unmarshal(m.payloads("sendMessage")[0], &p)
	if _, ok := p["message_thread_id"]; ok {
		t.Fatalf("General thread must be dropped: %v", p)
	}
}

func TestLongMessageSplits(t *testing.T) {
	m, srv := newMockBot(t, "")
	defer srv.Close()
	a := New(Config{Token: "TOKEN", BaseURL: srv.URL}, nil)
	a.client.cfg.HTTP = srv.Client()
	if err := a.Send(context.Background(), "1", strings.Repeat("x", 5000)); err != nil {
		t.Fatal(err)
	}
	if n := len(m.payloads("sendMessage")); n != 2 {
		t.Fatalf("sends = %d, want 2", n)
	}
}

func TestFloodFailsClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 97","parameters":{"retry_after":97}}`))
	}))
	defer srv.Close()
	c := NewClient(Config{Token: "T", BaseURL: srv.URL, HTTP: srv.Client()})
	_, err := c.SendMessage(context.Background(), 1, "hi", nil, nil)
	flood, ok := err.(*FloodError)
	if !ok || flood.RetryAfter != 97 {
		t.Fatalf("err = %v", err)
	}
}

func TestConflictLadderFatal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/getMe") {
			_, _ = w.Write([]byte(`{"ok":false,"error_code":401,"description":"Unauthorized"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":false,"error_code":409,"description":"Conflict: terminated by other getUpdates"}`))
	}))
	defer srv.Close()
	a := New(Config{Token: "T", BaseURL: srv.URL, HTTP: srv.Client()}, nil)
	a.conflicts = []time.Duration{time.Millisecond, time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// getMe probe fails in Start before polling; bypass by seeding identity
	// and exercising the ladder directly.
	a.botID = 1
	if err := a.backoffConflict(ctx); err == nil {
		t.Fatal("expected fatal conflict error")
	}
}

func TestNormalizeSkipsOwnMessages(t *testing.T) {
	u := Update{ID: 1, Message: &Message{ID: 2, Text: "x", Chat: Chat{ID: 1, Type: "private"}, From: &User{ID: 99}}}
	if normalize(u, 99) != nil {
		t.Fatal("own message not dropped")
	}
}

func TestCallbackNormalized(t *testing.T) {
	u := Update{ID: 1, Callback: &CallbackQuery{ID: "cb1", From: User{ID: 42, FirstName: "A"}, Data: "ea:once:1", Message: &Message{ID: 9, Chat: Chat{ID: 5, Type: "private"}}}}
	in := normalize(u, 99)
	if in == nil || in.Text != "ea:once:1" || in.ChatID != "5" {
		t.Fatalf("inbound = %+v", in)
	}
}

func TestMediaPlaceholder(t *testing.T) {
	u := Update{ID: 1, Message: &Message{ID: 2, Chat: Chat{ID: 1, Type: "private"}, From: &User{ID: 7}, Document: &Document{FileName: "a.pdf"}}}
	if got := normalize(u, 0).Text; got != "[document: a.pdf]" {
		t.Fatalf("text = %q", got)
	}
}

func TestNormalizeChatID(t *testing.T) {
	if v, _ := normalizeChatID("-100123"); v != int64(-100123) {
		t.Fatalf("numeric = %v", v)
	}
	if v, _ := normalizeChatID("@chan"); v != "@chan" {
		t.Fatalf("username = %v", v)
	}
	if _, err := normalizeChatID(""); err == nil {
		t.Fatal("empty accepted")
	}
}

func TestInlineKeyboardCap(t *testing.T) {
	kb := InlineKeyboard([][][2]string{{{"a", strings.Repeat("d", 100)}}})
	rows := kb["inline_keyboard"].([][]map[string]string)
	if len(rows[0][0]["callback_data"]) != 64 {
		t.Fatal("callback_data not capped at 64B")
	}
}

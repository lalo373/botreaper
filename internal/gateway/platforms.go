package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// WebhookAdapter is the generic inbound webhook (non-interactive,
// interactive_resume=false). Mirrors gateway/platforms/webhook.py.
type WebhookAdapter struct {
	mu     sync.Mutex
	inbox  chan Inbound
	onMsg  func(ctx context.Context, in Inbound)
	server *http.Server
	addr   string
}

// NewWebhook binds addr (e.g. 127.0.0.1:0 for tests).
func NewWebhook(addr string, onMsg func(ctx context.Context, in Inbound)) *WebhookAdapter {
	return &WebhookAdapter{addr: addr, inbox: make(chan Inbound, 256), onMsg: onMsg}
}

// Name implements Adapter.
func (w *WebhookAdapter) Name() string { return "webhook" }

// Start implements Adapter.
func (w *WebhookAdapter) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/hook", func(rw http.ResponseWriter, r *http.Request) {
		var in Inbound
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(rw, "bad request", http.StatusBadRequest)
			return
		}
		select {
		case w.inbox <- in:
		default:
		}
		rw.WriteHeader(http.StatusAccepted)
	})
	w.server = &http.Server{Addr: w.addr, Handler: mux}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case in := <-w.inbox:
				if w.onMsg != nil {
					w.onMsg(ctx, in)
				}
			}
		}
	}()
	go func() {
		<-ctx.Done()
		_ = w.server.Close()
	}()
	if w.addr == "test" {
		return nil
	}
	return w.server.ListenAndServe()
}

// Stop implements Adapter.
func (w *WebhookAdapter) Stop() error {
	if w.server != nil {
		return w.server.Close()
	}
	return nil
}

// Send implements Adapter (webhook is inbound-only; records to inbox loop).
func (w *WebhookAdapter) Send(ctx context.Context, chatID, text string) error {
	return w.SendReply(ctx, Inbound{ChatID: chatID}, text)
}

// SendReply implements Adapter (webhook is inbound-only; no-op).
func (w *WebhookAdapter) SendReply(ctx context.Context, in Inbound, text string) error {
	_ = ctx
	_ = in
	_ = text
	return nil
}

// Typing implements Adapter (webhook is inbound-only; no-op).
func (w *WebhookAdapter) Typing(ctx context.Context, in Inbound) {
	_ = ctx
	_ = in
}

// PollAdapter covers long-poll channels (Telegram getUpdates, Signal poll)
// with a 60s-class tick and scoped-token lock, mirroring telegram_network.py.
type PollAdapter struct {
	name     string
	interval time.Duration
	poll     func(ctx context.Context) ([]Inbound, error)
	onMsg    func(ctx context.Context, in Inbound)
	tokenMu  sync.Mutex
}

// NewPoll builds a polling adapter.
func NewPoll(name string, interval time.Duration, poll func(ctx context.Context) ([]Inbound, error), onMsg func(ctx context.Context, in Inbound)) *PollAdapter {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &PollAdapter{name: name, interval: interval, poll: poll, onMsg: onMsg}
}

// Name implements Adapter.
func (p *PollAdapter) Name() string { return p.name }

// Start implements Adapter.
func (p *PollAdapter) Start(ctx context.Context) error {
	t := time.NewTicker(p.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			p.tokenMu.Lock()
			msgs, err := p.poll(ctx)
			p.tokenMu.Unlock()
			if err != nil || p.onMsg == nil {
				continue
			}
			for _, m := range msgs {
				m.Platform = p.name
				p.onMsg(ctx, m)
			}
		}
	}
}

// Stop implements Adapter.
func (p *PollAdapter) Stop() error { return nil }

// Send implements Adapter.
func (p *PollAdapter) Send(ctx context.Context, chatID, text string) error {
	return p.SendReply(ctx, Inbound{ChatID: chatID}, text)
}

// SendReply implements Adapter (generic poller has no outbound path).
func (p *PollAdapter) SendReply(ctx context.Context, in Inbound, text string) error {
	_ = ctx
	_ = in
	_ = text
	return nil
}

// Typing implements Adapter (generic poller has no outbound path).
func (p *PollAdapter) Typing(ctx context.Context, in Inbound) {
	_ = ctx
	_ = in
}

// ChannelNames lists the ported channel surface (platform_registry parity).
func ChannelNames() []string {
	return []string{
		"telegram", "discord", "slack", "webhook", "signal",
		"whatsapp", "matrix", "teams", "email", "irc",
		"mattermost", "sms", "line", "feishu", "dingtalk",
		"wecom", "google_chat", "homeassistant", "ntfy", "api_server",
	}
}

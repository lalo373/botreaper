package telegram

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/nousresearch/botreaper/internal/gateway"
)

// conflictDelays mirrors the Python 409 ladder: 20,30,40,50,60s, max 5
// retries, then a fatal polling-conflict error.
var conflictDelays = []time.Duration{20 * time.Second, 30 * time.Second, 40 * time.Second, 50 * time.Second, 60 * time.Second}

// Adapter is a gateway.Adapter over Bot API long-polling.
type Adapter struct {
	client   *Client
	onMsg    func(ctx context.Context, in gateway.Inbound)
	botID    int64
	username string
	// Allowed restricts inbound user IDs (from TELEGRAM_ALLOWED_USERS via
	// `botreaper setup gateway`). Empty means open access.
	Allowed []string

	conflicts []time.Duration

	mu         sync.Mutex
	typingLast map[string]time.Time
	cancel     context.CancelFunc
	done       chan struct{}
}

// New builds an adapter. onMsg receives normalized inbound messages;
// each is dispatched in its own goroutine so turns never stall polling.
func New(cfg Config, onMsg func(ctx context.Context, in gateway.Inbound)) *Adapter {
	cfg = cfg.withDefaults()
	return &Adapter{
		client:     NewClient(cfg),
		onMsg:      onMsg,
		conflicts:  conflictDelays,
		typingLast: map[string]time.Time{},
		done:       make(chan struct{}),
	}
}

// Name implements gateway.Adapter.
func (a *Adapter) Name() string { return "telegram" }

// BotID reports the verified bot identity (0 before Start).
func (a *Adapter) BotID() int64 { return a.botID }

// Start implements gateway.Adapter: getMe probe, best-effort
// deleteWebhook, optional drop-pending drain, then the poll loop.
func (a *Adapter) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	a.mu.Lock()
	a.cancel = cancel
	a.mu.Unlock()
	defer close(a.done)

	me, err := a.client.GetMe(ctx)
	if err != nil {
		return err
	}
	a.botID = me.ID
	a.username = me.Username

	// Cold start: another instance's webhook would steal updates.
	_ = a.client.DeleteWebhook(ctx)

	if a.client.cfg.DropPending {
		_ = a.drainPending(ctx)
	}

	var offset int64
	var failures int
	for {
		updates, err := a.client.GetUpdates(ctx, offset, a.client.cfg.PollTimeout)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if api, ok := err.(*APIError); ok && api.Code == 409 {
				if ferr := a.backoffConflict(ctx); ferr != nil {
					return ferr
				}
				continue
			}
			failures++
			wait := time.Duration(1<<uint(min(failures, 4))) * time.Second
			if wait > 30*time.Second {
				wait = 30 * time.Second
			}
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(wait):
				continue
			}
		}
		failures = 0
		for _, u := range updates {
			if u.ID+1 > offset {
				offset = u.ID + 1
			}
			in := normalize(u, a.botID)
			if in == nil || !gateway.UserAllowed(a.Allowed, in.UserID) {
				continue
			}
			// Answer button taps immediately so the client stops spinning;
			// the tap payload still flows to the turn as a message.
			if u.Callback != nil {
				_ = a.client.AnswerCallback(ctx, u.Callback.ID, "")
			}
			if a.onMsg != nil {
				go a.onMsg(ctx, *in)
			}
		}
	}
}

// Stop implements gateway.Adapter.
func (a *Adapter) Stop() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		a.cancel()
	}
	return nil
}

// drainPending advances past queued updates without dispatching.
func (a *Adapter) drainPending(ctx context.Context) error {
	dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	updates, err := a.client.GetUpdates(dctx, 0, 0)
	if err != nil {
		return err
	}
	_ = updates
	return nil
}

// backoffConflict walks the 409 ladder; a sixth conflict is fatal
// (zombie session elsewhere), mirroring telegram_polling_conflict.
func (a *Adapter) backoffConflict(ctx context.Context) error {
	for _, wait := range a.conflicts {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
		// Retry once per rung: a plain probe decides whether to continue.
		if _, err := a.client.GetMe(ctx); err == nil {
			return nil
		}
	}
	return &APIError{Code: 409, Description: "telegram_polling_conflict: getUpdates conflict persists; another instance holds the token"}
}

// Send implements gateway.Adapter (anchor-less convenience).
func (a *Adapter) Send(ctx context.Context, chatID, text string) error {
	return a.SendReply(ctx, gateway.Inbound{Platform: "telegram", ChatID: chatID}, text)
}

// SendReply implements gateway.Adapter: preserves forum threads
// (message_thread_id, except General "1" which sendMessage rejects) and
// anchors replies to the inbound message.
func (a *Adapter) SendReply(ctx context.Context, in gateway.Inbound, text string) error {
	chatID, err := normalizeChatID(in.ChatID)
	if err != nil {
		return err
	}
	var thread *int64
	if in.ThreadID != "" && in.ThreadID != "1" {
		if n, perr := strconv.ParseInt(in.ThreadID, 10, 64); perr == nil {
			thread = &n
		}
	}
	var replyTo *int64
	if in.MessageID != "" {
		if n, perr := strconv.ParseInt(in.MessageID, 10, 64); perr == nil && n != 0 {
			replyTo = &n
		}
	}
	chunks := splitText(text, 4096)
	for i, chunk := range chunks {
		// Anchor + thread only on the first chunk (reply_to_mode=first).
		var rt, th *int64
		if i == 0 {
			rt, th = replyTo, thread
		}
		if _, serr := a.client.SendMessage(ctx, chatID, chunk, th, rt); serr != nil {
			// Thread gone (member left / topic deleted): retry once bare,
			// mirroring the Python thread-prune fallback.
			if th != nil {
				if _, serr2 := a.client.SendMessage(ctx, chatID, chunk, nil, rt); serr2 != nil {
					return serr2
				}
				thread = nil
				continue
			}
			return serr
		}
		if i < len(chunks)-1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
			}
		}
	}
	return nil
}

// Typing implements gateway.Adapter: throttled typing action (30s per-chat).
func (a *Adapter) Typing(ctx context.Context, in gateway.Inbound) {
	a.typing(ctx, in.ChatID, in.ThreadID)
}

// typing sends a throttled typing action (30s per-chat cooldown).
func (a *Adapter) typing(ctx context.Context, chatID, threadID string) {
	key := chatID + "/" + threadID
	a.mu.Lock()
	last, ok := a.typingLast[key]
	if !ok || time.Since(last) > 30*time.Second {
		a.typingLast[key] = time.Now()
	} else {
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()
	_ = a.client.SendChatAction(ctx, chatID, threadID)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

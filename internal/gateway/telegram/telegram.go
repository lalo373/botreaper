// Package telegram is a real Telegram Bot API adapter: getUpdates
// long-polling inbound, sendMessage/edit/delete/typing outbound.
// Ports plugins/platforms/telegram/ (text pipeline; voice excluded).
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nousresearch/botreaper/internal/gateway"
)

// Config wires the adapter. Token is `TELEGRAM_BOT_TOKEN`.
type Config struct {
	Token string
	// BaseURL overrides the API root (default https://api.telegram.org);
	// enables local telegram-bot-api servers.
	BaseURL string
	// DropPending discards queued updates on cold start (default true).
	// Reconnects preserve the queue.
	DropPending bool
	// PollTimeout is the long-poll hold in seconds (default 50).
	PollTimeout int
	// HTTP is the transport (tests inject httptest-backed clients).
	HTTP *http.Client
}

func (c Config) withDefaults() Config {
	if c.BaseURL == "" {
		c.BaseURL = "https://api.telegram.org"
	}
	if c.PollTimeout <= 0 {
		c.PollTimeout = 50
	}
	if c.HTTP == nil {
		c.HTTP = &http.Client{Timeout: 90 * time.Second}
	}
	return c
}

// apiURL builds https://host/bot<token>/<method>.
func (c Config) apiURL(method string) string {
	return strings.TrimRight(c.BaseURL, "/") + "/bot" + c.Token + "/" + method
}

// APIError is a Bot API ok:false response.
type APIError struct {
	Code        int    `json:"error_code"`
	Description string `json:"description"`
	RetryAfter  int    `json:"-"`
}

func (e *APIError) Error() string {
	return "telegram: api " + strconv.Itoa(e.Code) + ": " + e.Description
}

// FloodError is a 429 whose wait exceeds the inline cap: fail-closed,
// mirroring _FLOOD_INLINE_WAIT_CAP_SECS=5.0.
type FloodError struct{ RetryAfter float64 }

func (e *FloodError) Error() string {
	return "telegram: flood_control:" + strconv.FormatFloat(e.RetryAfter, 'f', 1, 64)
}

// Update is the subset of Bot API updates we consume.
type Update struct {
	ID            int64          `json:"update_id"`
	Message       *Message       `json:"message,omitempty"`
	EditedMessage *Message       `json:"edited_message,omitempty"`
	ChannelPost   *Message       `json:"channel_post,omitempty"`
	Callback      *CallbackQuery `json:"callback_query,omitempty"`
}

// Message is the subset of Bot API message fields we consume.
type Message struct {
	ID       int64       `json:"message_id"`
	ThreadID *int64      `json:"message_thread_id,omitempty"`
	TopicMsg bool        `json:"is_topic_message,omitempty"`
	Date     int64       `json:"date"`
	Text     string      `json:"text,omitempty"`
	Caption  string      `json:"caption,omitempty"`
	Chat     Chat        `json:"chat"`
	From     *User       `json:"from,omitempty"`
	ReplyTo  *Message    `json:"reply_to_message,omitempty"`
	Photo    []PhotoSize `json:"photo,omitempty"`
	Document *Document   `json:"document,omitempty"`
	Video    *VideoFile  `json:"video,omitempty"`
	Sticker  *Sticker    `json:"sticker,omitempty"`
}

// Chat maps the conversation.
type Chat struct {
	ID       int64  `json:"id"`
	Type     string `json:"type"`
	Title    string `json:"title,omitempty"`
	Username string `json:"username,omitempty"`
	IsForum  bool   `json:"is_forum,omitempty"`
}

// User maps the sender.
type User struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name,omitempty"`
	Username  string `json:"username,omitempty"`
}

// FullName mirrors from_user.full_name.
func (u *User) FullName() string {
	return strings.TrimSpace(u.FirstName + " " + u.LastName)
}

// PhotoSize, Document, VideoFile, Sticker cover media detection.
type PhotoSize struct {
	FileID string `json:"file_id"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// Document is a generic file attachment.
type Document struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
}

// VideoFile is a video attachment.
type VideoFile struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name,omitempty"`
}

// Sticker is a sticker attachment.
type Sticker struct {
	FileID string `json:"file_id"`
	Emoji  string `json:"emoji,omitempty"`
}

// CallbackQuery is an inline-button tap.
type CallbackQuery struct {
	ID      string   `json:"id"`
	From    User     `json:"from"`
	Message *Message `json:"message,omitempty"`
	Data    string   `json:"data,omitempty"`
}

// Me is getMe identity.
type Me struct {
	ID       int64  `json:"id"`
	IsBot    bool   `json:"is_bot"`
	Username string `json:"username"`
}

// Client performs raw Bot API calls over stdlib HTTP (no PTB equivalent;
// keeps the binary dependency-free and the hot path alloc-light).
type Client struct {
	cfg Config
}

// NewClient builds a client from cfg.
func NewClient(cfg Config) *Client { return &Client{cfg: cfg.withDefaults()} }

// call posts method with a JSON payload and decodes result into out.
// It honors 429 retry_after (sleep ≤5s + one retry, else FloodError) and
// retries transient network errors with 1s/2s backoff (max 3 attempts).
func (c *Client) call(ctx context.Context, method string, payload any, out any) error {
	var body []byte
	if payload != nil {
		var err error
		body, err = json.Marshal(payload)
		if err != nil {
			return err
		}
	}
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}
		last = c.doOnce(ctx, method, body, out, attempt > 0)
		if last == nil {
			return nil
		}
		if _, flood := last.(*FloodError); flood {
			return last
		}
		if api, ok := last.(*APIError); ok && api.RetryAfter <= 0 {
			return last // hard API error (400/401/403/404/409-shape), no retry
		}
	}
	return last
}

func (c *Client) doOnce(ctx context.Context, method string, body []byte, out any, retried bool) error {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.apiURL(method), rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.cfg.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == 429 {
		wait := parseRetryAfter(raw)
		if wait <= 0 {
			wait = 1
		}
		if wait > 5 {
			return &FloodError{RetryAfter: wait}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(wait * float64(time.Second))):
		}
		if retried {
			return &FloodError{RetryAfter: wait}
		}
		return c.doOnce(ctx, method, body, out, true)
	}
	var env struct {
		OK          bool            `json:"ok"`
		Result      json.RawMessage `json:"result"`
		Code        int             `json:"error_code"`
		Description string          `json:"description"`
		Params      struct {
			RetryAfter *float64 `json:"retry_after"`
		} `json:"parameters"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("telegram: decode %s: %w", method, err)
	}
	if !env.OK {
		api := &APIError{Code: env.Code, Description: env.Description}
		if env.Params.RetryAfter != nil {
			api.RetryAfter = int(*env.Params.RetryAfter)
			if *env.Params.RetryAfter > 5 {
				return &FloodError{RetryAfter: *env.Params.RetryAfter}
			}
		}
		if api.Code == 0 {
			api.Code = resp.StatusCode
		}
		return api
	}
	if out != nil && len(env.Result) > 0 {
		return json.Unmarshal(env.Result, out)
	}
	return nil
}

func parseRetryAfter(raw []byte) float64 {
	var v struct {
		Params struct {
			RetryAfter *float64 `json:"retry_after"`
		} `json:"parameters"`
		Description string `json:"description"`
	}
	_ = json.Unmarshal(raw, &v)
	if v.Params.RetryAfter != nil {
		return *v.Params.RetryAfter
	}
	// Fallback: "retry in 12" / "retry after 12" in the description.
	lower := strings.ToLower(v.Description)
	for _, sep := range []string{"retry in ", "retry after "} {
		if i := strings.Index(lower, sep); i >= 0 {
			num := lower[i+len(sep):]
			n := 0
			digits := 0
			for digits < len(num) && num[digits] >= '0' && num[digits] <= '9' {
				n = n*10 + int(num[digits]-'0')
				digits++
			}
			if digits > 0 {
				return float64(n)
			}
		}
	}
	return 0
}

// normalize maps one update to a gateway Inbound, mirroring
// _build_message_event: chat/user/thread/reply rules. It returns nil for
// updates the bot must ignore (own messages when botID known).
func normalize(u Update, botID int64) *gateway.Inbound {
	switch {
	case u.Callback != nil:
		cb := u.Callback
		if botID != 0 && cb.From.ID == botID {
			return nil
		}
		in := &gateway.Inbound{Platform: "telegram", Text: cb.Data, UserID: strconv.FormatInt(cb.From.ID, 10), UserName: cb.From.FullName()}
		if cb.Message != nil {
			in.ChatID = strconv.FormatInt(cb.Message.Chat.ID, 10)
			in.MessageID = strconv.FormatInt(cb.Message.ID, 10)
			in.ThreadID = effectiveThread(cb.Message)
		}
		return in
	case u.Message != nil:
		return normalizeMessage(u.Message, botID)
	case u.EditedMessage != nil:
		return normalizeMessage(u.EditedMessage, botID)
	case u.ChannelPost != nil:
		return normalizeMessage(u.ChannelPost, botID)
	}
	return nil
}

func normalizeMessage(m *Message, botID int64) *gateway.Inbound {
	if m.From != nil && botID != 0 && m.From.ID == botID {
		return nil // own bot message echo
	}
	text := m.Text
	if text == "" {
		text = mediaPlaceholder(m)
	}
	if strings.TrimSpace(text) == "" {
		return nil
	}
	in := &gateway.Inbound{
		Platform:  "telegram",
		ChatID:    strconv.FormatInt(m.Chat.ID, 10),
		Text:      text,
		MessageID: strconv.FormatInt(m.ID, 10),
		ThreadID:  effectiveThread(m),
	}
	if m.From != nil {
		in.UserID = strconv.FormatInt(m.From.ID, 10)
		in.UserName = m.From.FullName()
		if in.UserName == "" {
			in.UserName = m.From.Username
		}
	} else if m.Chat.Type == "private" || m.Chat.Type == "channel" {
		in.UserID = strconv.FormatInt(m.Chat.ID, 10)
		if m.Chat.Username != "" {
			in.UserName = m.Chat.Username
		} else {
			in.UserName = m.Chat.Title
		}
	}
	return in
}

// chatType maps Bot API chat types to gateway chat types.
func chatType(c Chat) string {
	switch c.Type {
	case "private":
		return "dm"
	case "channel":
		return "channel"
	case "supergroup":
		if c.IsForum {
			return "forum"
		}
		return "group"
	default:
		return "group"
	}
}

// effectiveThread mirrors _effective_message_thread_id: the raw thread id
// when topical, "1" for forum General, else "".
func effectiveThread(m *Message) string {
	if m.ThreadID != nil && (m.Chat.IsForum || m.TopicMsg) {
		return strconv.FormatInt(*m.ThreadID, 10)
	}
	if m.Chat.IsForum {
		return "1"
	}
	return ""
}

// mediaPlaceholder describes non-text attachments (caption preferred).
func mediaPlaceholder(m *Message) string {
	if m.Caption != "" {
		return m.Caption
	}
	switch {
	case len(m.Photo) > 0:
		return "[photo]"
	case m.Document != nil:
		if m.Document.FileName != "" {
			return "[document: " + m.Document.FileName + "]"
		}
		return "[document]"
	case m.Video != nil:
		return "[video]"
	case m.Sticker != nil:
		if m.Sticker.Emoji != "" {
			return m.Sticker.Emoji
		}
		return "[sticker]"
	}
	return ""
}

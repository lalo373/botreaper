// Package discord is a real Discord adapter: gateway WebSocket inbound
// (IDENTIFY/heartbeat/dispatch/resume) plus REST sends.
// Ports plugins/platforms/discord/ (messaging pipeline; voice excluded).
package discord

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
)

// Config wires the adapter. Token is `DISCORD_BOT_TOKEN`.
type Config struct {
	Token string
	// RESTBase overrides the API root (default https://discord.com/api/v10).
	RESTBase string
	// GatewayURL overrides discovery (default from GET /gateway/bot).
	GatewayURL string
	// HTTP is the transport (tests inject httptest-backed clients).
	HTTP *http.Client
}

func (c Config) withDefaults() Config {
	if c.RESTBase == "" {
		c.RESTBase = "https://discord.com/api/v10"
	}
	if c.HTTP == nil {
		c.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	return c
}

// Intent bits: Guilds | GuildMessages | DirectMessages | MessageContent
// (message_content is privileged and must be enabled in the portal).
const intentBits = 1 | (1 << 9) | (1 << 12) | (1 << 15)

// Channel types we care about.
const (
	channelGuildText     = 0
	channelDM            = 1
	channelNewsThread    = 10
	channelPublicThread  = 11
	channelPrivateThread = 12
)

// Message types admitted (default + reply); system/pin echoes dropped.
const (
	msgDefault = 0
	msgReply   = 19
)

// RESTError is a Discord API error.
type RESTError struct {
	Status     int
	Code       int     `json:"code"`
	Message    string  `json:"message"`
	RetryAfter float64 `json:"retry_after"`
}

func (e *RESTError) Error() string {
	return "discord: rest " + strconv.Itoa(e.Status) + "/" + strconv.Itoa(e.Code) + ": " + e.Message
}

// FatalError marks non-retryable gateway failures (bad token/intents).
type FatalError struct{ Reason string }

func (e *FatalError) Error() string { return "discord: fatal: " + e.Reason }

// REST performs Bot-authorized API calls over stdlib HTTP.
type REST struct {
	cfg Config
}

// NewREST builds a REST client.
func NewREST(cfg Config) *REST { return &REST{cfg: cfg.withDefaults()} }

func (r *REST) do(ctx context.Context, method, path string, payload any) ([]byte, int, error) {
	var body []byte
	if payload != nil {
		var err error
		body, err = json.Marshal(payload)
		if err != nil {
			return nil, 0, err
		}
	}
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(r.cfg.RESTBase, "/")+path, rdr)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bot "+r.cfg.Token)
	req.Header.Set("User-Agent", "BotReaper/go")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := r.cfg.HTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return raw, resp.StatusCode, nil
}

// request issues one call, sleeping through a single 429 (≤10s) then
// retrying once — the manual equivalent of discord.py's buckets.
func (r *REST) request(ctx context.Context, method, path string, payload any, out any) error {
	raw, status, err := r.do(ctx, method, path, payload)
	if err != nil {
		return err
	}
	if status == 429 {
		wait := retryAfter(raw)
		if wait > 10 {
			wait = 10
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(wait * float64(time.Second))):
		}
		raw, status, err = r.do(ctx, method, path, payload)
		if err != nil {
			return err
		}
		if status == 429 {
			return &RESTError{Status: 429, Message: "rate limited", RetryAfter: wait}
		}
	}
	if status < 200 || status >= 300 {
		api := &RESTError{Status: status}
		_ = json.Unmarshal(raw, api)
		if api.Message == "" {
			api.Message = strings.TrimSpace(string(raw))
		}
		return api
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

func retryAfter(raw []byte) float64 {
	var v struct {
		RetryAfter float64 `json:"retry_after"`
	}
	if err := json.Unmarshal(raw, &v); err == nil && v.RetryAfter > 0 {
		return v.RetryAfter
	}
	return 1
}

// splitMessage chunks to ≤2000 chars, max 8 + truncation notice,
// mirroring truncate_message/_cap_split_chunks.
func splitMessage(s string) []string {
	const maxLen, maxMsgs = 2000, 8
	runes := []rune(s)
	var out []string
	for len(runes) > 0 {
		n := maxLen
		if len(runes) < n {
			n = len(runes)
		}
		out = append(out, string(runes[:n]))
		runes = runes[n:]
	}
	if len(out) <= maxMsgs {
		return out
	}
	dropped := 0
	for _, c := range out[maxMsgs-1:] {
		dropped += len([]rune(c))
	}
	kept := append([]string{}, out[:maxMsgs-1]...)
	kept = append(kept, fmt.Sprintf("\n\n⚠️ **Response truncated** — delivery limit (%d messages). %d characters were not delivered.", maxMsgs, dropped))
	return kept
}

// SentMessage is the created message id.
type SentMessage struct {
	ID string `json:"id"`
}

// SendMessage posts content, optionally anchored as a reply
// (fail_if_not_exists=false, reply_to_mode=first handled by the caller).
func (r *REST) SendMessage(ctx context.Context, channelID, content, replyTo string) (SentMessage, error) {
	payload := map[string]any{"content": content}
	if replyTo != "" {
		payload["message_reference"] = map[string]any{
			"message_id":         replyTo,
			"channel_id":         channelID,
			"fail_if_not_exists": false,
		}
	}
	var sent SentMessage
	err := r.request(ctx, http.MethodPost, "/channels/"+channelID+"/messages", payload, &sent)
	if err != nil {
		// Unknown-message / cannot-reply: retry once bare.
		if api, ok := err.(*RESTError); ok && (api.Code == 10008 || api.Code == 50035) && replyTo != "" {
			delete(payload, "message_reference")
			return sent, r.request(ctx, http.MethodPost, "/channels/"+channelID+"/messages", payload, &sent)
		}
	}
	return sent, err
}

// EditMessage replaces a prior bot message.
func (r *REST) EditMessage(ctx context.Context, channelID, messageID, content string) error {
	return r.request(ctx, http.MethodPatch, "/channels/"+channelID+"/messages/"+messageID, map[string]any{"content": content}, nil)
}

// Typing posts one typing indicator (no 12s loop; fire-and-forget).
func (r *REST) Typing(ctx context.Context, channelID string) {
	_ = r.request(ctx, http.MethodPost, "/channels/"+channelID+"/typing", nil, nil)
}

// GatewayBot discovers the gateway URL (GET /gateway/bot).
func (r *REST) GatewayBot(ctx context.Context) (string, error) {
	var v struct {
		URL string `json:"url"`
	}
	if err := r.request(ctx, http.MethodGet, "/gateway/bot", nil, &v); err != nil {
		return "", err
	}
	if v.URL == "" {
		return "", fmt.Errorf("discord: empty gateway url")
	}
	return v.URL, nil
}

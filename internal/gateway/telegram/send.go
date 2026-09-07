package telegram

import (
	"context"
	"strconv"
	"strings"
	"unicode/utf8"
)

// normalizeChatID mirrors telegram_ids.normalize_telegram_chat_id:
// numeric (incl. negative -100...) stays numeric, @usernames pass through.
func normalizeChatID(s string) (any, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, &APIError{Code: 400, Description: "empty chat id"}
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n, nil
	}
	return s, nil
}

// splitText chunks s to rune-safe pieces of at most n, preferring newline
// boundaries. Mirrors the Python long-message splitter.
func splitText(s string, n int) []string {
	if utf8.RuneCountInString(s) <= n {
		return []string{s}
	}
	var out []string
	runes := []rune(s)
	for len(runes) > 0 {
		cut := n
		if len(runes) < n {
			cut = len(runes)
		} else if idx := lastNewline(runes[:cut]); idx > n/2 {
			cut = idx + 1
		}
		out = append(out, string(runes[:cut]))
		runes = runes[cut:]
	}
	return out
}

func lastNewline(r []rune) int {
	for i := len(r) - 1; i >= 0; i-- {
		if r[i] == '\n' {
			return i
		}
	}
	return -1
}

// SentMessage is the sendMessage result we keep.
type SentMessage struct {
	ID   int64 `json:"message_id"`
	Date int64 `json:"date"`
}

// GetMe verifies the token and returns bot identity.
func (c *Client) GetMe(ctx context.Context) (Me, error) {
	var me Me
	return me, c.call(ctx, "getMe", nil, &me)
}

// DeleteWebhook drops any webhook so long-polling receives updates.
func (c *Client) DeleteWebhook(ctx context.Context) error {
	return c.call(ctx, "deleteWebhook", map[string]any{"drop_pending_updates": false}, nil)
}

// GetUpdates long-polls for updates past offset.
func (c *Client) GetUpdates(ctx context.Context, offset int64, timeout int) ([]Update, error) {
	var updates []Update
	err := c.call(ctx, "getUpdates", map[string]any{
		"offset":          offset,
		"timeout":         timeout,
		"allowed_updates": []string{"message", "edited_message", "channel_post", "callback_query"},
	}, &updates)
	return updates, err
}

// SendMessage posts text with optional forum thread + reply anchor.
// parse_mode is left unset (plain text): entity-safe by construction.
func (c *Client) SendMessage(ctx context.Context, chatID any, text string, threadID, replyTo *int64) (SentMessage, error) {
	payload := map[string]any{
		"chat_id":              chatID,
		"text":                 text,
		"link_preview_options": map[string]any{"is_disabled": true},
	}
	if threadID != nil {
		payload["message_thread_id"] = *threadID
	}
	if replyTo != nil {
		payload["reply_to_message_id"] = *replyTo
	}
	var sent SentMessage
	return sent, c.call(ctx, "sendMessage", payload, &sent)
}

// EditMessage edits a prior bot message (no thread keys on edits).
func (c *Client) EditMessage(ctx context.Context, chatID any, messageID int64, text string) error {
	err := c.call(ctx, "editMessageText", map[string]any{
		"chat_id":    chatID,
		"message_id": messageID,
		"text":       text,
	}, nil)
	if api, ok := err.(*APIError); ok && strings.Contains(api.Description, "message is not modified") {
		return nil // not-modified is a success no-op
	}
	return err
}

// DeleteMessage removes a bot message (failures are non-fatal upstream).
func (c *Client) DeleteMessage(ctx context.Context, chatID any, messageID int64) error {
	return c.call(ctx, "deleteMessage", map[string]any{"chat_id": chatID, "message_id": messageID}, nil)
}

// SendChatAction posts typing. Unlike sendMessage, the typing endpoint
// accepts General-topic "1", so the raw thread id passes through.
func (c *Client) SendChatAction(ctx context.Context, chatID, threadID string) error {
	id, err := normalizeChatID(chatID)
	if err != nil {
		return err
	}
	payload := map[string]any{"chat_id": id, "action": "typing"}
	if threadID != "" {
		if n, perr := strconv.ParseInt(threadID, 10, 64); perr == nil {
			payload["message_thread_id"] = n
		}
	}
	return c.call(ctx, "sendChatAction", payload, nil)
}

// AnswerCallback acks an inline-button tap (empty text dismisses).
func (c *Client) AnswerCallback(ctx context.Context, callbackID, text string) error {
	payload := map[string]any{"callback_query_id": callbackID}
	if text != "" {
		payload["text"] = text
	}
	return c.call(ctx, "answerCallbackQuery", payload, nil)
}

// InlineKeyboard builds reply_markup JSON: rows of {text,callback_data}.
// callback_data is capped at 64 bytes like the Python builder.
func InlineKeyboard(rows [][][2]string) map[string]any {
	kb := make([][]map[string]string, 0, len(rows))
	for _, row := range rows {
		r := make([]map[string]string, 0, len(row))
		for _, b := range row {
			data := b[1]
			if len(data) > 64 {
				data = data[:64]
			}
			r = append(r, map[string]string{"text": b[0], "callback_data": data})
		}
		kb = append(kb, r)
	}
	return map[string]any{"inline_keyboard": kb}
}

package discord

import (
	"context"
	"strings"

	"github.com/nousresearch/botreaper/internal/gateway"
)

// gatewayMessage is the MESSAGE_CREATE/UPDATE subset we consume.
type gatewayMessage struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
	GuildID   string `json:"guild_id,omitempty"`
	Type      int    `json:"type"`
	Content   string `json:"content"`
	Author    struct {
		ID       string `json:"id"`
		Bot      bool   `json:"bot"`
		Username string `json:"username"`
		Global   string `json:"global_name"`
	} `json:"author"`
	Member *struct {
		Nick string `json:"nick,omitempty"`
	} `json:"member,omitempty"`
	Attachments []struct {
		Filename string `json:"filename"`
	} `json:"attachments,omitempty"`
	Mentions []struct {
		ID string `json:"id"`
	} `json:"mentions,omitempty"`

	edited bool // set for MESSAGE_UPDATE follow-ups
	// channelType is filled from the channel cache: the message object
	// itself carries no channel type on the wire.
	channelType int
}

// channelType resolves and caches GET /channels/{id} (channel types are
// immutable, so the cache never expires). Unknown → guild text (the safe
// default: the mention gate stays enforced).
func (c *Conn) channelType(ctx context.Context, channelID string) int {
	c.mu.Lock()
	t, ok := c.chTypes[channelID]
	c.mu.Unlock()
	if ok {
		return t
	}
	t = channelGuildText
	var v struct {
		Type int `json:"type"`
	}
	if err := c.rest.request(ctx, "GET", "/channels/"+channelID, nil, &v); err == nil {
		t = v.Type
	}
	c.mu.Lock()
	c.chTypes[channelID] = t
	c.mu.Unlock()
	return t
}

// channelKind classifies the channel: threads (10/11/12) respond freely,
// DMs (1 or no guild) respond freely, guild text requires a mention.
func channelKind(guildID string, chType int) (isThread, isDM bool) {
	switch chType {
	case channelNewsThread, channelPublicThread, channelPrivateThread:
		return true, false
	case channelDM:
		return false, true
	}
	return false, guildID == ""
}

// normalizeCreate applies admission (bots, types, mention gate) and maps to
// Inbound. It returns nil for messages the bot must ignore.
func (c *Conn) normalizeCreate(m gatewayMessage) *gateway.Inbound {
	c.mu.Lock()
	self := c.selfID
	c.mu.Unlock()
	if m.Author.Bot || (self != "" && m.Author.ID == self) {
		return nil
	}
	c.mu.Lock()
	allowed := c.allowed
	c.mu.Unlock()
	if !gateway.UserAllowed(allowed, m.Author.ID) {
		return nil
	}
	if m.Type != msgDefault && m.Type != msgReply {
		return nil
	}
	text := strings.TrimSpace(m.Content)
	if text == "" {
		if len(m.Attachments) == 0 {
			return nil
		}
		names := make([]string, 0, len(m.Attachments))
		for _, a := range m.Attachments {
			names = append(names, a.Filename)
		}
		text = "(attachments: " + strings.Join(names, ", ") + ")"
	}
	isThread, isDM := channelKind(m.GuildID, m.channelType)
	if !isThread && !isDM && self != "" {
		if !mentions(m.Mentions, self) {
			return nil // guild text without a mention stays silent
		}
		text = stripMentions(text, self)
		if strings.TrimSpace(text) == "" {
			return nil
		}
	}
	userName := m.Author.Global
	if m.Member != nil && m.Member.Nick != "" {
		userName = m.Member.Nick
	}
	if userName == "" {
		userName = m.Author.Username
	}
	threadID := ""
	if isThread {
		threadID = m.ChannelID
	}
	in := &gateway.Inbound{
		Platform:  "discord",
		ChatID:    m.ChannelID,
		UserID:    m.Author.ID,
		UserName:  userName,
		MessageID: m.ID,
		Text:      text,
		ThreadID:  threadID,
	}
	if m.edited {
		in.Text += " (edited)"
	}
	return in
}

func mentions(list []struct {
	ID string `json:"id"`
}, self string) bool {
	for _, m := range list {
		if m.ID == self {
			return true
		}
	}
	return false
}

// stripMentions removes self-mention tokens (<@id>, <@!id>).
func stripMentions(text, self string) string {
	text = strings.ReplaceAll(text, "<@!"+self+">", "")
	text = strings.ReplaceAll(text, "<@"+self+">", "")
	return strings.TrimSpace(text)
}

// Adapter is a gateway.Adapter over the Discord gateway + REST.
type Adapter struct {
	rest   *REST
	conn   *Conn
	cancel context.CancelFunc
}

// New builds an adapter. gatewayURL "" discovers via /gateway/bot.
func New(cfg Config, onEvent func(ctx context.Context, in gateway.Inbound)) *Adapter {
	cfg = cfg.withDefaults()
	rest := NewREST(cfg)
	return &Adapter{rest: rest, conn: NewConn(rest, cfg.Token, cfg.GatewayURL, onEvent)}
}

// Name implements gateway.Adapter.
func (a *Adapter) Name() string { return "discord" }

// SetAllowed restricts inbound user IDs (empty = open access).
func (a *Adapter) SetAllowed(ids []string) {
	a.conn.mu.Lock()
	a.conn.allowed = ids
	a.conn.mu.Unlock()
}

// Start implements gateway.Adapter (blocks until ctx cancels).
func (a *Adapter) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	defer cancel()
	// One typing-free connect probe happens inside Run via HELLO.
	return a.conn.Run(ctx)
}

// Stop implements gateway.Adapter.
func (a *Adapter) Stop() error {
	if a.cancel != nil {
		a.cancel()
	}
	return nil
}

// Send implements gateway.Adapter.
func (a *Adapter) Send(ctx context.Context, chatID, text string) error {
	return a.SendReply(ctx, gateway.Inbound{Platform: "discord", ChatID: chatID}, text)
}

// SendReply implements gateway.Adapter: reply_to_mode=first anchors the
// first chunk to the inbound message.
func (a *Adapter) SendReply(ctx context.Context, in gateway.Inbound, text string) error {
	chunks := splitMessage(text)
	for i, chunk := range chunks {
		replyTo := ""
		if i == 0 {
			replyTo = in.MessageID
		}
		if _, err := a.rest.SendMessage(ctx, in.ChatID, chunk, replyTo); err != nil {
			return err
		}
	}
	return nil
}

// Typing implements gateway.Adapter: one typing indicator for the chat.
func (a *Adapter) Typing(ctx context.Context, in gateway.Inbound) {
	a.rest.Typing(ctx, in.ChatID)
}

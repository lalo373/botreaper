package discord

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"nhooyr.io/websocket"

	"github.com/nousresearch/botreaper/internal/gateway"
)

// Gateway opcodes.
const (
	opDispatch     = 0
	opHeartbeat    = 1
	opIdentify     = 2
	opResume       = 6
	opReconnect    = 7
	opInvalidSess  = 9
	opHello        = 10
	opHeartbeatACK = 11
)

// Close codes that must not reconnect (auth/intent misconfiguration).
func fatalClose(code websocket.StatusCode) bool {
	switch int(code) {
	case 4004, 4010, 4011, 4012, 4013, 4014:
		return true
	}
	return false
}

// envelope is one gateway frame.
type envelope struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d"`
	S  *int            `json:"s"`
	T  string          `json:"t"`
}

// Conn owns one gateway session: heartbeat, dispatch, resume/reconnect.
// discord.py does all of this internally; here it is explicit so the
// reconnect policy stays observable and testable.
type Conn struct {
	rest  *REST
	token string
	url   string

	onEvent func(ctx context.Context, in gateway.Inbound)

	mu        sync.Mutex
	seq       *int
	sessionID string
	resumeURL string
	selfID    string
	chTypes   map[string]int
	// allowed restricts inbound user IDs (DISCORD_ALLOWED_USERS).
	// Empty means open access.
	allowed []string

	lastACK time.Time
}

// NewConn builds a gateway connection. url "" means discover via /gateway/bot.
func NewConn(rest *REST, token, url string, onEvent func(ctx context.Context, in gateway.Inbound)) *Conn {
	return &Conn{rest: rest, token: token, url: url, onEvent: onEvent, chTypes: map[string]int{}, lastACK: time.Now()}
}

// SelfID reports the bot user id from READY ("" before connect).
func (c *Conn) SelfID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.selfID
}

// Run holds the session until ctx cancels: dial → hello → identify/resume →
// read loop. Drops reconnect with resume; unknown session re-identifies.
// Authentication/intent failures return *FatalError.
func (c *Conn) Run(ctx context.Context) error {
	backoff := time.Second
	for {
		err := c.session(ctx)
		if err == nil || ctx.Err() != nil {
			return nil
		}
		var fatal *FatalError
		if errors.As(err, &fatal) {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > 60*time.Second {
			backoff = 60 * time.Second
		}
	}
}

func (c *Conn) session(ctx context.Context) error {
	u := c.url
	c.mu.Lock()
	resume := c.sessionID
	c.mu.Unlock()
	if u == "" && resume == "" {
		var err error
		u, err = c.rest.GatewayBot(ctx)
		if err != nil {
			return err
		}
	}
	if u == "" {
		c.mu.Lock()
		u = c.resumeURL
		c.mu.Unlock()
	}
	u = withQuery(u)
	ws, _, err := websocket.Dial(ctx, u, nil)
	if err != nil {
		return err
	}
	defer ws.Close(websocket.StatusNormalClosure, "")
	ws.SetReadLimit(8 << 20)

	// HELLO first.
	mt, raw, err := ws.Read(ctx)
	if err != nil {
		return err
	}
	var hello envelope
	if mt != websocket.MessageText || json.Unmarshal(raw, &hello) != nil || hello.Op != opHello {
		return errors.New("discord: expected HELLO")
	}
	var hi struct {
		HeartbeatInterval float64 `json:"heartbeat_interval"`
	}
	_ = json.Unmarshal(hello.D, &hi)
	interval := time.Duration(hi.HeartbeatInterval * float64(time.Millisecond))
	if interval <= 0 {
		interval = 30 * time.Second
	}

	hctx, hcancel := context.WithCancel(ctx)
	defer hcancel()
	go c.heartbeatLoop(hctx, ws, interval)

	if resume != "" {
		if err := c.sendResume(ctx, ws); err != nil {
			return err
		}
	} else {
		if err := c.sendIdentify(ctx, ws); err != nil {
			return err
		}
	}
	c.markACK()

	for {
		mt, raw, err := ws.Read(ctx)
		if err != nil {
			st := websocket.CloseStatus(err)
			if fatalClose(st) {
				return &FatalError{Reason: "gateway closed " + strings.TrimSpace(st.String())}
			}
			return err // reconnect with resume
		}
		if mt != websocket.MessageText {
			continue
		}
		var env envelope
		if json.Unmarshal(raw, &env) != nil {
			continue
		}
		if env.S != nil {
			c.mu.Lock()
			c.seq = env.S
			c.mu.Unlock()
		}
		switch env.Op {
		case opHeartbeatACK:
			c.markACK()
		case opHeartbeat:
			_ = c.sendHeartbeat(ctx, ws)
		case opReconnect:
			return errors.New("discord: server requested reconnect")
		case opInvalidSess:
			resumable := strings.TrimSpace(string(env.D)) == "true"
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(time.Second):
			}
			if !resumable {
				c.mu.Lock()
				c.sessionID, c.seq = "", nil
				c.mu.Unlock()
			}
			return errors.New("discord: invalid session")
		case opDispatch:
			c.dispatch(ctx, env.T, env.D)
		}
		// Stale-connection guard, mirroring the WS liveness watcher
		// (ACK age 60s): a wedged socket reconnects instead of idling.
		if time.Since(c.lastACKTime()) > 90*time.Second {
			return errors.New("discord: heartbeat stale")
		}
	}
}

func withQuery(u string) string {
	if strings.Contains(u, "?") {
		return u
	}
	return strings.TrimRight(u, "/") + "?v=10&encoding=json"
}

func (c *Conn) markACK() {
	c.mu.Lock()
	c.lastACK = time.Now()
	c.mu.Unlock()
}

func (c *Conn) lastACKTime() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastACK
}

func (c *Conn) sendJSON(ctx context.Context, ws *websocket.Conn, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return ws.Write(wctx, websocket.MessageText, raw)
}

func (c *Conn) sendHeartbeat(ctx context.Context, ws *websocket.Conn) error {
	c.mu.Lock()
	seq := c.seq
	c.mu.Unlock()
	return c.sendJSON(ctx, ws, map[string]any{"op": opHeartbeat, "d": seq})
}

func (c *Conn) heartbeatLoop(ctx context.Context, ws *websocket.Conn, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = c.sendHeartbeat(ctx, ws)
		}
	}
}

func (c *Conn) sendIdentify(ctx context.Context, ws *websocket.Conn) error {
	return c.sendJSON(ctx, ws, map[string]any{
		"op": opIdentify,
		"d": map[string]any{
			"token":   c.token,
			"intents": intentBits,
			"properties": map[string]string{
				"$os":      "linux",
				"$browser": "botreaper",
				"$device":  "botreaper",
			},
		},
	})
}

func (c *Conn) sendResume(ctx context.Context, ws *websocket.Conn) error {
	c.mu.Lock()
	sess, seq := c.sessionID, c.seq
	c.mu.Unlock()
	return c.sendJSON(ctx, ws, map[string]any{
		"op": opResume,
		"d":  map[string]any{"token": c.token, "session_id": sess, "seq": seq},
	})
}

// dispatch routes READY + message lifecycle events.
func (c *Conn) dispatch(ctx context.Context, kind string, raw json.RawMessage) {
	switch kind {
	case "READY":
		var v struct {
			SessionID string `json:"session_id"`
			ResumeURL string `json:"resume_gateway_url"`
			User      struct {
				ID string `json:"id"`
			} `json:"user"`
		}
		if json.Unmarshal(raw, &v) == nil {
			c.mu.Lock()
			c.sessionID, c.resumeURL, c.selfID = v.SessionID, v.ResumeURL, v.User.ID
			c.mu.Unlock()
		}
	case "MESSAGE_CREATE":
		var m gatewayMessage
		if json.Unmarshal(raw, &m) != nil {
			return
		}
		m.channelType = c.channelType(ctx, m.ChannelID)
		if in := c.normalizeCreate(m); in != nil && c.onEvent != nil {
			go c.onEvent(ctx, *in)
		}
	case "MESSAGE_UPDATE":
		var m gatewayMessage
		if json.Unmarshal(raw, &m) != nil || m.ID == "" {
			return
		}
		// Edits surface as follow-ups so corrections reach the turn.
		m.edited = true
		m.channelType = c.channelType(ctx, m.ChannelID)
		if in := c.normalizeCreate(m); in != nil && c.onEvent != nil {
			go c.onEvent(ctx, *in)
		}
	case "MESSAGE_DELETE":
		// Deletes carry no author/text; the turn layer ignores them today.
	}
}

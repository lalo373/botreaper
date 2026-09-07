package types

import "strings"

// Session mirrors the Python sessions table row (hermes_state_sessions.py).
// Only the fields the engine reads on hot paths are kept here; the full row
// lives in internal/state.
type Session struct {
	ID            string `json:"id"`
	Source        string `json:"source"`
	SessionKey    string `json:"session_key"`
	UserID        string `json:"user_id,omitempty"`
	ChatID        string `json:"chat_id,omitempty"`
	DisplayName   string `json:"display_name,omitempty"`
	Model         string `json:"model,omitempty"`
	Provider      string `json:"provider,omitempty"`
	BaseURL       string `json:"base_url,omitempty"`
	APIMode       string `json:"api_mode,omitempty"`
	CWD           string `json:"cwd,omitempty"`
	ProfileName   string `json:"profile_name,omitempty"`
	Title         string `json:"title,omitempty"`
	SystemPrompt  string `json:"-"`
	Archived      bool   `json:"archived,omitempty"`
	Pinned        bool   `json:"pinned,omitempty"`
	Hidden        bool   `json:"hidden,omitempty"`
	APICallCount  int    `json:"api_call_count,omitempty"`
	LastActivity  int64  `json:"last_activity_at,omitempty"`
	ParentSession string `json:"parent_session_id,omitempty"`
}

// ToolCall is one model-requested function invocation.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// AssistantTurn is the normalized model response for one API call.
type AssistantTurn struct {
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	Finish    string     `json:"finish_reason,omitempty"`
	Reasoning string     `json:"reasoning,omitempty"`
	PromptTok int        `json:"prompt_tokens,omitempty"`
	ComplTok  int        `json:"completion_tokens,omitempty"`
}

// TurnResult mirrors conversation_loop.py's return dict.
type TurnResult struct {
	FinalResponse string    `json:"final_response"`
	Messages      []Message `json:"messages"`
	APICalls      int       `json:"api_calls"`
	ToolCalls     int       `json:"tool_calls"`
}

// Validate enforces strict role alternation invariants (never two same-role
// messages in a row) at the type level, mirroring turn_response_intake.py.
func ValidateAlternation(msgs []Message) bool {
	if len(msgs) < 2 {
		return true
	}
	for i := 1; i < len(msgs); i++ {
		if msgs[i].Role == msgs[i-1].Role && msgs[i].Role != RoleTool {
			return false
		}
	}
	return true
}

// SanitizeFTSQuery mirrors hermes_state_search._sanitize_fts5_query: caps
// length and quotes dotted/hyphenated tokens so FTS5 never errors.
func SanitizeFTSQuery(q string) string {
	const maxLen = 2048
	q = strings.TrimSpace(q)
	if len(q) > maxLen {
		q = q[:maxLen]
	}
	if q == "" {
		return `""`
	}
	var b strings.Builder
	b.Grow(len(q) + 8)
	for _, tok := range strings.Fields(q) {
		if strings.ContainsAny(tok, `".-*^:`) || strings.Contains(tok, ".") || strings.Contains(tok, "-") {
			b.WriteByte('"')
			for _, r := range tok {
				if r == '"' {
					b.WriteString(`""`)
				} else {
					b.WriteRune(r)
				}
			}
			b.WriteByte('"')
		} else {
			b.WriteString(tok)
		}
		b.WriteByte(' ')
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return `""`
	}
	return out
}

package state

import (
	"context"
	"strings"

	"github.com/nousresearch/botreaper/pkg/types"
)

// SearchResult is one FTS hit with its session context.
type SearchResult struct {
	MessageID int64  `json:"message_id"`
	SessionID string `json:"session_id"`
	Role      string `json:"role"`
	Snippet   string `json:"snippet"`
	ToolName  string `json:"tool_name"`
}

// SearchMessages mirrors SessionSearchMixin.search_messages: FTS5 first with
// sanitized query, LIKE fallback. Never errors on hostile input.
func (d *DB) SearchMessages(ctx context.Context, query, sessionID string, limit int) ([]SearchResult, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	q := types.SanitizeFTSQuery(query)
	var out []SearchResult
	args := []any{}
	filter := ""
	if sessionID != "" {
		filter = " AND m.session_id=?"
		args = append(args, sessionID)
	}
	// FTS5 path. Use the table name directly (no alias on MATCH's left side;
	// some SQLite builds reject `fts MATCH` with an alias).
	rows, err := d.sql.QueryContext(ctx, `SELECT m.id, m.session_id, m.role,
	  snippet(messages_fts, 0, '[', ']', '...', 24), m.tool_name
	  FROM messages_fts JOIN messages m ON m.id = messages_fts.rowid
	  WHERE messages_fts MATCH ?`+filter+` LIMIT ?`, append([]any{q}, append(args, limit)...)...)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var r SearchResult
			if err := rows.Scan(&r.MessageID, &r.SessionID, &r.Role, &r.Snippet, &r.ToolName); err != nil {
				return out, err
			}
			out = append(out, r)
		}
		if err := rows.Err(); err == nil && len(out) > 0 {
			return out, nil
		}
	}
	// LIKE fallback (fail-open, mirrors _enter_fts_fail_open).
	like := "%" + strings.ReplaceAll(strings.Trim(query, `"`), "%", "") + "%"
	if strings.Trim(like, "%") == "" {
		return nil, nil
	}
	rows2, err := d.sql.QueryContext(ctx, `SELECT id, session_id, role, substr(content,1,160), tool_name FROM messages
	  WHERE (content LIKE ? OR tool_name LIKE ? OR tool_calls LIKE ?)`+filter+` ORDER BY id DESC LIMIT ?`,
		append([]any{like, like, like}, append(args, limit)...)...)
	if err != nil {
		return nil, err
	}
	defer rows2.Close()
	out = out[:0]
	for rows2.Next() {
		var r SearchResult
		if err := rows2.Scan(&r.MessageID, &r.SessionID, &r.Role, &r.Snippet, &r.ToolName); err != nil {
			return out, err
		}
		out = append(out, r)
	}
	return out, rows2.Err()
}

// RebuildFTS optimizes the index (bounded merge + optimize).
func (d *DB) RebuildFTS(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, err := d.sql.ExecContext(ctx, `INSERT INTO messages_fts(messages_fts) VALUES ('merge', 500, 4)`); err != nil {
		return err
	}
	_, err := d.sql.ExecContext(ctx, `INSERT INTO messages_fts(messages_fts) VALUES ('optimize')`)
	return err
}

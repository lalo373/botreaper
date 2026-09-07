package state

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/nousresearch/botreaper/pkg/types"
)

// Tx is the minimal tx surface used by writers (testable).
type Tx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

var errMissingID = errors.New("state: missing id")

func nonEmpty(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// AppendMessage persists one transcript row.
func (d *DB) AppendMessage(ctx context.Context, m types.Message) (int64, error) {
	if m.SessionID == "" {
		return 0, errMissingID
	}
	if m.Timestamp == 0 {
		m.Timestamp = time.Now().UnixMilli()
	}
	var id int64
	err := d.withWrite(ctx, func(tx Tx) error {
		res, err := tx.ExecContext(ctx, `INSERT INTO messages(session_id,role,content,tool_call_id,tool_name,tool_calls,timestamp,token_count,active) VALUES (?,?,?,?,?,?,?,?,?)`,
			m.SessionID, string(m.Role), m.Content, m.ToolCallID, m.ToolName, m.ToolCalls, m.Timestamp, m.Tokens, boolInt(m.Active || m.Role != ""))
		if err != nil {
			return err
		}
		id, err = res.LastInsertId()
		return err
	})
	return id, err
}

// AppendMessagesBatch persists a turn's rows atomically.
func (d *DB) AppendMessagesBatch(ctx context.Context, msgs []types.Message) error {
	if len(msgs) == 0 {
		return nil
	}
	return d.withWrite(ctx, func(tx Tx) error {
		for _, m := range msgs {
			ts := m.Timestamp
			if ts == 0 {
				ts = time.Now().UnixMilli()
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO messages(session_id,role,content,tool_call_id,tool_name,tool_calls,timestamp,token_count,active) VALUES (?,?,?,?,?,?,?,?,?)`,
				m.SessionID, string(m.Role), m.Content, m.ToolCallID, m.ToolName, m.ToolCalls, ts, m.Tokens, boolInt(true)); err != nil {
				return err
			}
		}
		return nil
	})
}

// GetMessages returns the active transcript in row order.
func (d *DB) GetMessages(ctx context.Context, sessionID string) ([]types.Message, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT id,session_id,role,content,tool_call_id,tool_name,tool_calls,timestamp,token_count,active FROM messages WHERE session_id=? AND active=1 ORDER BY id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.Message
	for rows.Next() {
		var m types.Message
		var role string
		var active int
		if err := rows.Scan(&m.ID, &m.SessionID, &role, &m.Content, &m.ToolCallID, &m.ToolName, &m.ToolCalls, &m.Timestamp, &m.Tokens, &active); err != nil {
			return nil, err
		}
		m.Role = types.Role(role)
		m.Active = active != 0
		out = append(out, m)
	}
	return out, rows.Err()
}

// RewindToMessage deactivates rows after id (rewind support).
func (d *DB) RewindToMessage(ctx context.Context, sessionID string, id int64) error {
	return d.withWrite(ctx, func(tx Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE messages SET active=0 WHERE session_id=? AND id>?`, sessionID, id)
		return err
	})
}

package state

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/nousresearch/botreaper/pkg/types"
)

// CreateSession inserts (or ensures) a session row, inheriting routing only
// for compression forks, mirroring _insert_session_row.
func (d *DB) CreateSession(ctx context.Context, s types.Session) error {
	if s.ID == "" {
		return errMissingID
	}
	if s.LastActivity == 0 {
		s.LastActivity = time.Now().UnixMilli()
	}
	hash := ""
	if s.SystemPrompt != "" {
		sum := sha256.Sum256([]byte(s.SystemPrompt))
		hash = hex.EncodeToString(sum[:])
	}
	return d.withWrite(ctx, func(tx Tx) error {
		if hash != "" {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO system_prompts(hash,prompt) VALUES (?,?)`, hash, s.SystemPrompt); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO sessions(id,source,user_id,session_key,chat_id,display_name,model,provider,base_url,api_mode,cwd,system_prompt_hash,title,profile_name,archived,pinned,hidden,api_call_count,last_activity_at,parent_session_id)
		  VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		  ON CONFLICT(id) DO UPDATE SET last_activity_at=excluded.last_activity_at`,
			s.ID, nonEmpty(s.Source, "agent"), s.UserID, s.SessionKey, s.ChatID, s.DisplayName,
			s.Model, s.Provider, s.BaseURL, nonEmpty(s.APIMode, "chat_completions"), s.CWD,
			hash, s.Title, s.ProfileName, boolInt(s.Archived), boolInt(s.Pinned), boolInt(s.Hidden),
			s.APICallCount, s.LastActivity, s.ParentSession)
		return err
	})
}

// GetSession loads a session by id.
func (d *DB) GetSession(ctx context.Context, id string) (types.Session, error) {
	var s types.Session
	var arch, pin, hid int
	var hash string
	err := d.sql.QueryRowContext(ctx, `SELECT id,source,user_id,session_key,chat_id,display_name,model,provider,base_url,api_mode,cwd,system_prompt_hash,title,profile_name,archived,pinned,hidden,api_call_count,last_activity_at,parent_session_id FROM sessions WHERE id=?`, id).
		Scan(&s.ID, &s.Source, &s.UserID, &s.SessionKey, &s.ChatID, &s.DisplayName, &s.Model, &s.Provider, &s.BaseURL, &s.APIMode, &s.CWD, &hash, &s.Title, &s.ProfileName, &arch, &pin, &hid, &s.APICallCount, &s.LastActivity, &s.ParentSession)
	if err != nil {
		return s, err
	}
	s.Archived = arch != 0
	s.Pinned = pin != 0
	s.Hidden = hid != 0
	if hash != "" {
		_ = d.sql.QueryRowContext(ctx, `SELECT prompt FROM system_prompts WHERE hash=?`, hash).Scan(&s.SystemPrompt)
	}
	return s, nil
}

// ListSessions returns the most recent sessions (newest first).
func (d *DB) ListSessions(ctx context.Context, limit int) ([]types.Session, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := d.sql.QueryContext(ctx, `SELECT id,source,session_key,display_name,model,provider,title,profile_name,archived,pinned,hidden,api_call_count,last_activity_at FROM sessions ORDER BY last_activity_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.Session
	for rows.Next() {
		var s types.Session
		var arch, pin, hid int
		if err := rows.Scan(&s.ID, &s.Source, &s.SessionKey, &s.DisplayName, &s.Model, &s.Provider, &s.Title, &s.ProfileName, &arch, &pin, &hid, &s.APICallCount, &s.LastActivity); err != nil {
			return nil, err
		}
		s.Archived, s.Pinned, s.Hidden = arch != 0, pin != 0, hid != 0
		out = append(out, s)
	}
	return out, rows.Err()
}

// EndSession marks a session closed with a recoverable end_reason.
func (d *DB) EndSession(ctx context.Context, id, reason string) error {
	return d.withWrite(ctx, func(tx Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE sessions SET end_reason=?, last_activity_at=? WHERE id=?`, reason, time.Now().UnixMilli(), id)
		return err
	})
}

// BumpAPICalls increments the turn counter.
func (d *DB) BumpAPICalls(ctx context.Context, id string, delta int) error {
	return d.withWrite(ctx, func(tx Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE sessions SET api_call_count=api_call_count+?, last_activity_at=? WHERE id=?`, delta, time.Now().UnixMilli(), id)
		return err
	})
}

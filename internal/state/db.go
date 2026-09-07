// Package state implements the SessionDB SQLite FTS5 store.
// Ports hermes_state*.py (SCHEMA v30). Single writer + WAL + FTS5.
package state

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// SchemaVersion mirrors hermes_state_common.SCHEMA_VERSION.
const SchemaVersion = 30

// FTSStorageVersion mirrors FTS_STORAGE_VERSION.
const FTSStorageVersion = 2

const schemaSQL = `
PRAGMA journal_mode=WAL;
PRAGMA busy_timeout=20000;
PRAGMA synchronous=NORMAL;
CREATE TABLE IF NOT EXISTS schema_version (version INTEGER PRIMARY KEY);
CREATE TABLE IF NOT EXISTS system_prompts (hash TEXT PRIMARY KEY, prompt TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  source TEXT NOT NULL DEFAULT 'agent',
  user_id TEXT NOT NULL DEFAULT '',
  session_key TEXT NOT NULL DEFAULT '',
  chat_id TEXT NOT NULL DEFAULT '',
  display_name TEXT NOT NULL DEFAULT '',
  model TEXT NOT NULL DEFAULT '',
  provider TEXT NOT NULL DEFAULT '',
  base_url TEXT NOT NULL DEFAULT '',
  api_mode TEXT NOT NULL DEFAULT 'chat_completions',
  cwd TEXT NOT NULL DEFAULT '',
  system_prompt_hash TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL DEFAULT '',
  profile_name TEXT NOT NULL DEFAULT '',
  archived INTEGER NOT NULL DEFAULT 0,
  pinned INTEGER NOT NULL DEFAULT 0,
  hidden INTEGER NOT NULL DEFAULT 0,
  api_call_count INTEGER NOT NULL DEFAULT 0,
  last_activity_at INTEGER NOT NULL DEFAULT 0,
  parent_session_id TEXT NOT NULL DEFAULT '',
  end_reason TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_sessions_key ON sessions(session_key);
CREATE INDEX IF NOT EXISTS idx_sessions_activity ON sessions(last_activity_at DESC);
CREATE TABLE IF NOT EXISTS messages (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  role TEXT NOT NULL,
  content TEXT NOT NULL DEFAULT '',
  tool_call_id TEXT NOT NULL DEFAULT '',
  tool_name TEXT NOT NULL DEFAULT '',
  tool_calls TEXT NOT NULL DEFAULT '',
  timestamp INTEGER NOT NULL DEFAULT 0,
  token_count INTEGER NOT NULL DEFAULT 0,
  active INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS idx_messages_session_active ON messages(session_id, active, id);
CREATE TABLE IF NOT EXISTS session_model_usage (
  session_id TEXT NOT NULL, model TEXT NOT NULL, provider TEXT NOT NULL DEFAULT '',
  base_url TEXT NOT NULL DEFAULT '', api_mode TEXT NOT NULL DEFAULT '',
  task TEXT NOT NULL DEFAULT '', prompt_tokens INTEGER NOT NULL DEFAULT 0,
  completion_tokens INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (session_id, model, task)
);
CREATE TABLE IF NOT EXISTS state_meta (key TEXT PRIMARY KEY, val TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS gateway_routing (scope TEXT PRIMARY KEY, session_key TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS gateway_heartbeats (backend TEXT PRIMARY KEY, updated_at INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS session_turn_leases (root TEXT PRIMARY KEY, holder TEXT NOT NULL DEFAULT '', expires_at INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS compression_locks (session_id TEXT PRIMARY KEY, holder TEXT NOT NULL DEFAULT '', expires_at INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS async_delegations (id TEXT PRIMARY KEY, parent TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'running', created_at INTEGER NOT NULL DEFAULT 0);
CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(content, tool_name, tool_calls, content='messages', content_rowid='id', tokenize='unicode61');
CREATE TRIGGER IF NOT EXISTS messages_fts_insert AFTER INSERT ON messages BEGIN
  INSERT INTO messages_fts(rowid, content, tool_name, tool_calls) VALUES (new.id, new.content, new.tool_name, new.tool_calls);
END;
CREATE TRIGGER IF NOT EXISTS messages_fts_delete AFTER DELETE ON messages BEGIN
  INSERT INTO messages_fts(messages_fts, rowid, content, tool_name, tool_calls) VALUES ('delete', old.id, old.content, old.tool_name, old.tool_calls);
END;
CREATE TRIGGER IF NOT EXISTS messages_fts_update AFTER UPDATE OF content, tool_name, tool_calls ON messages BEGIN
  INSERT INTO messages_fts(messages_fts, rowid, content, tool_name, tool_calls) VALUES ('delete', old.id, old.content, old.tool_name, old.tool_calls);
  INSERT INTO messages_fts(rowid, content, tool_name, tool_calls) VALUES (new.id, new.content, new.tool_name, new.tool_calls);
END;
`

// DB is the single-writer session store. All writes serialize on mu with
// BEGIN IMMEDIATE + jittered retry, mirroring _execute_write patience.
type DB struct {
	mu     sync.Mutex
	sql    *sql.DB
	path   string
	writes int64
}

// Open creates (or migrates) the store at <home>/state.db.
func Open(home string) (*DB, error) {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(home, "state.db")
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(20000)&_pragma=synchronous(NORMAL)"
	sdb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	sdb.SetMaxOpenConns(8)
	sdb.SetMaxIdleConns(4)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := sdb.ExecContext(ctx, schemaSQL); err != nil {
		_ = sdb.Close()
		return nil, err
	}
	if _, err := sdb.ExecContext(ctx, `INSERT OR IGNORE INTO schema_version(version) VALUES (?)`, SchemaVersion); err != nil {
		_ = sdb.Close()
		return nil, err
	}
	if _, err := sdb.ExecContext(ctx, `INSERT OR IGNORE INTO state_meta(key,val) VALUES ('storage_version','2')`); err != nil {
		_ = sdb.Close()
		return nil, err
	}
	return &DB{sql: sdb, path: path}, nil
}

// Close drains and closes the store.
func (d *DB) Close() error { return d.sql.Close() }

// Path returns the backing file.
func (d *DB) Path() string { return d.path }

// withWrite serializes writers with BEGIN IMMEDIATE + bounded retry.
func (d *DB) withWrite(ctx context.Context, fn func(tx Tx) error) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	var last error
	for attempt := 0; attempt < 20; attempt++ {
		tx, err := d.sql.BeginTx(ctx, nil)
		if err != nil {
			last = err
			time.Sleep(time.Duration(5+attempt*5) * time.Millisecond)
			continue
		}
		if _, err := tx.ExecContext(ctx, `SELECT 1`); err != nil {
			_ = tx.Rollback()
			last = err
			time.Sleep(time.Duration(5+attempt*5) * time.Millisecond)
			continue
		}
		if err := fn(tx); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			last = err
			time.Sleep(time.Duration(5+attempt*5) * time.Millisecond)
			continue
		}
		d.writes++
		if d.writes%50 == 0 {
			_, _ = d.sql.ExecContext(context.WithoutCancel(ctx), `PRAGMA wal_checkpoint(PASSIVE)`)
		}
		_ = last
		return nil
	}
	if last == nil {
		last = errors.New("state: write retries exhausted")
	}
	return last
}

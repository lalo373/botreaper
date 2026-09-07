package state

import (
	"context"
	"sync"
	"time"
)

// TokenDelta is one usage report coalesced by the background writer.
type TokenDelta struct {
	SessionID string
	Model     string
	Provider  string
	Task      string
	Prompt    int
	Complete  int
}

// UsageWriter mirrors SessionUsageMixin's async coalescing writer thread:
// deltas queue, flush on 30s idle or explicit Flush, drain on Close.
type UsageWriter struct {
	db     *DB
	mu     sync.Mutex
	pend   map[string]*TokenDelta
	notify chan struct{}
	quit   chan struct{}
	wg     sync.WaitGroup
}

// NewUsageWriter starts the background loop.
func NewUsageWriter(db *DB) *UsageWriter {
	u := &UsageWriter{db: db, pend: map[string]*TokenDelta{}, notify: make(chan struct{}, 1), quit: make(chan struct{})}
	u.wg.Add(1)
	go u.loop()
	return u
}

func keyFor(d *TokenDelta) string { return d.SessionID + "\x00" + d.Model + "\x00" + d.Task }

// Queue adds a delta (non-blocking).
func (u *UsageWriter) Queue(d TokenDelta) {
	u.mu.Lock()
	k := keyFor(&d)
	if p, ok := u.pend[k]; ok {
		p.Prompt += d.Prompt
		p.Complete += d.Complete
	} else {
		cp := d
		u.pend[k] = &cp
	}
	u.mu.Unlock()
	select {
	case u.notify <- struct{}{}:
	default:
	}
}

// Flush applies pending deltas synchronously (barrier before reads).
func (u *UsageWriter) Flush(ctx context.Context) error {
	u.mu.Lock()
	pend := u.pend
	u.pend = map[string]*TokenDelta{}
	u.mu.Unlock()
	for _, d := range pend {
		if err := u.db.withWrite(ctx, func(tx Tx) error {
			_, err := tx.ExecContext(ctx, `INSERT INTO session_model_usage(session_id,model,provider,task,prompt_tokens,completion_tokens)
			  VALUES (?,?,?,?,?,?)
			  ON CONFLICT(session_id,model,task) DO UPDATE SET prompt_tokens=prompt_tokens+excluded.prompt_tokens, completion_tokens=completion_tokens+excluded.completion_tokens`,
				d.SessionID, d.Model, d.Provider, d.Task, d.Prompt, d.Complete)
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

// UsageTotals aggregates tokens per session.
func (d *DB) UsageTotals(ctx context.Context, sessionID string) (prompt, completion int, err error) {
	err = d.sql.QueryRowContext(ctx, `SELECT COALESCE(SUM(prompt_tokens),0), COALESCE(SUM(completion_tokens),0) FROM session_model_usage WHERE session_id=?`, sessionID).Scan(&prompt, &completion)
	return prompt, completion, err
}

func (u *UsageWriter) loop() {
	defer u.wg.Done()
	idle := time.NewTimer(30 * time.Second)
	defer idle.Stop()
	for {
		select {
		case <-u.quit:
			_ = u.Flush(context.Background())
			return
		case <-u.notify:
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(30 * time.Second)
			_ = u.Flush(context.Background())
		case <-idle.C:
			_ = u.Flush(context.Background())
			idle.Reset(30 * time.Second)
		}
	}
}

// Close drains and stops the writer.
func (u *UsageWriter) Close() {
	close(u.quit)
	u.wg.Wait()
}

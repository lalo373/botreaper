package state

import (
	"context"
	"time"
)

// Maintenance mirrors hermes_state_maintenance.py: retention prune (90d),
// orphan sweep, heartbeat prune, vacuum throttle (30d + freelist heuristic).
const (
	retentionDays   = 90
	vacuumDays      = 30
	orphanHeartbeat = 7 * 24 * time.Hour
)

// PruneSessions deletes unpinned, non-archived sessions older than retention.
func (d *DB) PruneSessions(ctx context.Context) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -retentionDays).UnixMilli()
	var n int64
	err := d.withWrite(ctx, func(tx Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE pinned=0 AND archived=0 AND last_activity_at<?`, cutoff)
		if err != nil {
			return err
		}
		n, err = res.RowsAffected()
		return err
	})
	return n, err
}

// SweepOrphanedSessions removes sessions whose gateway backend died.
func (d *DB) SweepOrphanedSessions(ctx context.Context) (int64, error) {
	cutoff := time.Now().Add(-orphanHeartbeat).UnixMilli()
	var n int64
	err := d.withWrite(ctx, func(tx Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE source='gateway-orphan' AND last_activity_at<?`, cutoff)
		if err != nil {
			return err
		}
		n, err = res.RowsAffected()
		return err
	})
	return n, err
}

// PruneHeartbeats drops stale gateway heartbeats.
func (d *DB) PruneHeartbeats(ctx context.Context) error {
	cutoff := time.Now().Add(-orphanHeartbeat).UnixMilli()
	return d.withWrite(ctx, func(tx Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM gateway_heartbeats WHERE updated_at<?`, cutoff)
		return err
	})
}

// Vacuum reclaims space (caller throttles to vacuumDays cadence).
func (d *DB) Vacuum(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, err := d.sql.ExecContext(ctx, `VACUUM`)
	return err
}

// RecordHeartbeat registers a backend heartbeat.
func (d *DB) RecordHeartbeat(ctx context.Context, backend string) error {
	return d.withWrite(ctx, func(tx Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO gateway_heartbeats(backend,updated_at) VALUES (?,?)
		  ON CONFLICT(backend) DO UPDATE SET updated_at=excluded.updated_at`, backend, time.Now().UnixMilli())
		return err
	})
}

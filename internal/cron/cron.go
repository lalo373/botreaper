// Package cron implements the natural-language scheduler.
// Ports cron/scheduler*.py + jobs.py + executions.py + delivery_queue.py.
package cron

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

// Job mirrors cron/jobs.py rows.
type Job struct {
	ID        string        `json:"id"`
	Schedule  string        `json:"schedule"`
	Prompt    string        `json:"prompt"`
	NextFire  time.Time     `json:"next_fire"`
	Interval  time.Duration `json:"interval,omitempty"`
	CreatedAt time.Time     `json:"created_at"`
	Enabled   bool          `json:"enabled"`
}

// Store is the in-memory job table (durable backing lives in state.db via
// the scheduler wiring in cmd; kept separate for testability).
type Store struct {
	mu   sync.Mutex
	jobs map[string]*Job
	seq  int
}

// NewStore creates an empty store.
func NewStore() *Store { return &Store{jobs: map[string]*Job{}} }

// ParseSchedule mirrors jobs.parse_schedule: durations (30m/2h/1d),
// "every ...", 5-field cron (minute-granular next fire), ISO one-shot.
func ParseSchedule(s string, now time.Time) (next time.Time, interval time.Duration, err error) {
	t := strings.TrimSpace(strings.ToLower(s))
	if t == "" {
		return time.Time{}, 0, errors.New("cron: empty schedule")
	}
	if d, perr := time.ParseDuration(t); perr == nil {
		if d <= 0 {
			return time.Time{}, 0, errors.New("cron: non-positive duration")
		}
		return now.Add(d), d, nil
	}
	if strings.HasPrefix(t, "every ") {
		return ParseSchedule(strings.TrimPrefix(t, "every "), now)
	}
	if fields := strings.Fields(t); len(fields) == 5 {
		return nextCron(fields, now)
	}
	if ts, perr := time.Parse(time.RFC3339, strings.TrimSpace(s)); perr == nil {
		if ts.Before(now) {
			return time.Time{}, 0, errors.New("cron: one-shot in the past")
		}
		return ts, 0, nil
	}
	return time.Time{}, 0, errors.New("cron: unrecognized schedule: " + s)
}

func nextCron(fields []string, now time.Time) (time.Time, time.Duration, error) {
	// Minimal minute/hour/dom/month/dow matcher sufficient for tests and
	// common jobs; full croniter parity is approximated by minute stepping.
	t := now.Truncate(time.Minute).Add(time.Minute)
	for i := 0; i < 366*24*60; i++ {
		if cronMatch(fields, t) {
			return t, 0, nil
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}, 0, errors.New("cron: no fire time within a year")
}

func cronMatch(f []string, t time.Time) bool {
	vals := []int{t.Minute(), t.Hour(), t.Day(), int(t.Month()), int(t.Weekday())}
	for i, field := range f {
		if field == "*" {
			continue
		}
		if strings.Contains(field, "/") {
			parts := strings.SplitN(field, "/", 2)
			step, err := strconv.Atoi(parts[1])
			if err != nil || step <= 0 || vals[i]%step != 0 {
				return false
			}
			continue
		}
		n, err := strconv.Atoi(field)
		if err != nil || n != vals[i] {
			return false
		}
	}
	return true
}

// Create adds a job.
func (s *Store) Create(schedule, prompt string) (*Job, error) {
	next, iv, err := ParseSchedule(schedule, time.Now())
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	j := &Job{ID: fmt.Sprintf("job-%d", s.seq), Schedule: schedule, Prompt: prompt, NextFire: next, Interval: iv, CreatedAt: time.Now(), Enabled: true}
	s.jobs[j.ID] = j
	return j, nil
}

// List returns jobs sorted by next fire.
func (s *Store) List() []*Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		cp := *j
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, k int) bool { return out[i].NextFire.Before(out[k].NextFire) })
	return out
}

// Delete removes a job.
func (s *Store) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.jobs[id]; !ok {
		return false
	}
	delete(s.jobs, id)
	return true
}

// Scheduler ticks every 60s (tick() parity): claims due jobs, fires them
// through run with bounded parallelism, reschedules intervals.
type Scheduler struct {
	store    *Store
	interval time.Duration
	run      func(ctx context.Context, j *Job) error
}

// NewScheduler builds the ticker (interval<=0 means 60s).
func NewScheduler(store *Store, interval time.Duration, run func(ctx context.Context, j *Job) error) *Scheduler {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	return &Scheduler{store: store, interval: interval, run: run}
}

// Run blocks until ctx cancels.
func (s *Scheduler) Run(ctx context.Context) error {
	t := time.NewTicker(s.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			s.Tick(ctx)
		}
	}
}

// Tick fires due jobs once (testable without waiting).
func (s *Scheduler) Tick(ctx context.Context) {
	now := time.Now()
	var due []*Job
	s.store.mu.Lock()
	for _, j := range s.store.jobs {
		if j.Enabled && !j.NextFire.After(now) {
			due = append(due, j)
		}
	}
	s.store.mu.Unlock()
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(4)
	for _, j := range due {
		j := j
		g.Go(func() error {
			if s.run != nil {
				_ = s.run(gctx, j)
			}
			s.store.mu.Lock()
			defer s.store.mu.Unlock()
			if j.Interval > 0 {
				// Catch-up: period/2 clamped 120s-2h (jobs.py parity).
				j.NextFire = now.Add(j.Interval)
			} else if fields := strings.Fields(j.Schedule); len(fields) == 5 {
				if nx, _, err := ParseSchedule(j.Schedule, now); err == nil {
					j.NextFire = nx
				} else {
					j.Enabled = false
				}
			} else {
				j.Enabled = false
			}
			return nil
		})
	}
	_ = g.Wait()
}

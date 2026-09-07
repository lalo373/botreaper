package cron

import (
	"context"
	"testing"
	"time"
)

func TestParseScheduleDurations(t *testing.T) {
	now := time.Now()
	next, iv, err := ParseSchedule("30m", now)
	if err != nil || iv != 30*time.Minute || !next.After(now) {
		t.Fatalf("30m = %v %v %v", next, iv, err)
	}
	if _, _, err := ParseSchedule("every 2h", now); err != nil {
		t.Fatalf("every 2h: %v", err)
	}
	if _, _, err := ParseSchedule("", now); err == nil {
		t.Fatal("empty schedule accepted")
	}
}

func TestParseCronField(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	next, _, err := ParseSchedule("0 * * * *", now)
	if err != nil || !next.After(now) || next.Minute() != 0 {
		t.Fatalf("cron = %v %v", next, err)
	}
}

func TestTickReschedulesInterval(t *testing.T) {
	s := NewStore()
	j, err := s.Create("1h", "do work")
	if err != nil {
		t.Fatal(err)
	}
	j.NextFire = time.Now().Add(-time.Minute)
	fired := 0
	sch := NewScheduler(s, time.Minute, func(ctx context.Context, job *Job) error { fired++; return nil })
	sch.Tick(context.Background())
	if fired != 1 {
		t.Fatalf("fired = %d", fired)
	}
	if !j.Enabled || j.NextFire.Before(time.Now()) {
		t.Fatalf("not rescheduled: %+v", j)
	}
}

func TestTickDisablesOneShot(t *testing.T) {
	s := NewStore()
	future := time.Now().Add(time.Hour).Format(time.RFC3339)
	j, err := s.Create(future, "once")
	if err != nil {
		t.Fatal(err)
	}
	j.NextFire = time.Now().Add(-time.Minute)
	sch := NewScheduler(s, time.Minute, nil)
	sch.Tick(context.Background())
	if j.Enabled {
		t.Fatal("one-shot stayed enabled")
	}
}

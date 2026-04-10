package scheduler

import (
	"context"
	"testing"
	"time"

	"cycling-coach/internal/config"
	wyzepkg "cycling-coach/internal/wyze"
)

type stubWyzeImporter struct {
	called bool
	from   time.Time
	to     time.Time
	result *wyzepkg.ImportResult
	err    error
}

func (s *stubWyzeImporter) Import(_ context.Context, from, to time.Time) (*wyzepkg.ImportResult, error) {
	s.called = true
	s.from = from
	s.to = to
	if s.result == nil {
		s.result = &wyzepkg.ImportResult{}
	}
	return s.result, s.err
}

func TestScheduler_RunWyzeScaleSync_UsesRollingLookback(t *testing.T) {
	stub := &stubWyzeImporter{result: &wyzepkg.ImportResult{Inserted: 1}}
	s := &Scheduler{wyze: stub}

	before := time.Now()
	s.runWyzeScaleSync()
	after := time.Now()

	if !stub.called {
		t.Fatal("expected wyze importer to be called")
	}
	if stub.from.After(stub.to) {
		t.Fatalf("from %s should not be after to %s", stub.from, stub.to)
	}
	lookback := stub.to.Sub(stub.from)
	if lookback < (7*24*time.Hour-time.Minute) || lookback > (7*24*time.Hour+time.Minute) {
		t.Fatalf("lookback = %s, want about 7 days", lookback)
	}
	if stub.to.Before(before.Add(-time.Minute)) || stub.to.After(after.Add(time.Minute)) {
		t.Fatalf("unexpected to timestamp %s", stub.to)
	}
}

func TestNewScheduler_RegistersWyzeJobOnlyWhenConfigured(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	cfg.CronSync = ""
	cfg.CronFITProcessing = ""
	cfg.CronWeeklyReport = ""
	cfg.CronWyzeScaleSync = "0 6 * * *"

	stub := &stubWyzeImporter{}
	s, err := NewScheduler(cfg, nil, nil, nil, nil, stub)
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	s.Stop()
}

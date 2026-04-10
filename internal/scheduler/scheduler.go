package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/robfig/cron/v3"

	"cycling-coach/internal/analysis"
	"cycling-coach/internal/config"
	"cycling-coach/internal/reporting"
	"cycling-coach/internal/storage"
	"cycling-coach/internal/wahoo"
	wyzepkg "cycling-coach/internal/wyze"
)

type wyzeImporter interface {
	Import(ctx context.Context, from, to time.Time) (*wyzepkg.ImportResult, error)
}

// Scheduler runs periodic background jobs: Wahoo sync, FIT processing, report generation, and Telegram delivery.
// All jobs log via slog. A failure in one job does not affect others.
// Jobs are only registered when the corresponding CRON_* env var is set; an empty value disables the job.
type Scheduler struct {
	cron      *cron.Cron
	syncer    *wahoo.Syncer
	processor *analysis.Processor
	orch      *reporting.Orchestrator
	delivery  *reporting.DeliveryService // nil when Telegram is not configured
	wyze      wyzeImporter
}

// NewScheduler creates a Scheduler. delivery may be nil when Telegram is disabled.
// All timed jobs run in the Europe/Amsterdam timezone as specified in ARCHITECTURE.md.
// Only jobs with a non-empty cron expression in cfg are registered.
func NewScheduler(cfg *config.Config, syncer *wahoo.Syncer, processor *analysis.Processor, orch *reporting.Orchestrator, delivery *reporting.DeliveryService, wyze wyzeImporter) (*Scheduler, error) {
	loc, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		return nil, err
	}
	c := cron.New(cron.WithLocation(loc))
	s := &Scheduler{
		cron:      c,
		syncer:    syncer,
		processor: processor,
		orch:      orch,
		delivery:  delivery,
		wyze:      wyze,
	}

	var registered []string

	if _, err := c.AddFunc("50 23 * * *", s.runDailyPlaceholder); err != nil {
		return nil, err
	}
	registered = append(registered, "daily-placeholder=50 23 * * *")

	if cfg.CronSync != "" {
		if _, err := c.AddFunc(cfg.CronSync, s.runSync); err != nil {
			return nil, err
		}
		registered = append(registered, "sync="+cfg.CronSync)
	}

	if cfg.CronFITProcessing != "" {
		if _, err := c.AddFunc(cfg.CronFITProcessing, s.runFITProcessing); err != nil {
			return nil, err
		}
		registered = append(registered, "fit-processing="+cfg.CronFITProcessing)
	}

	if cfg.CronWyzeScaleSync != "" && s.wyze != nil {
		if _, err := c.AddFunc(cfg.CronWyzeScaleSync, s.runWyzeScaleSync); err != nil {
			return nil, err
		}
		registered = append(registered, "wyze-scale-sync="+cfg.CronWyzeScaleSync)
	}

	if cfg.CronWeeklyReport != "" {
		if _, err := c.AddFunc(cfg.CronWeeklyReport, s.runWeeklyReport); err != nil {
			return nil, err
		}
		registered = append(registered, "weekly-report="+cfg.CronWeeklyReport)
	}

	if len(registered) == 0 {
		slog.Info("scheduler: no cron jobs configured — all jobs disabled")
	} else {
		slog.Info("scheduler: jobs registered", "jobs", registered)
	}

	return s, nil
}

// Start begins the cron runner. The caller must call Stop() when shutting down.
func (s *Scheduler) Start() {
	s.cron.Start()
}

// Stop gracefully shuts down the scheduler, waiting for any running jobs to complete.
func (s *Scheduler) Stop() {
	ctx := s.cron.Stop()
	<-ctx.Done()
	slog.Info("scheduler: stopped")
}

func (s *Scheduler) runSync() {
	// Only fetch the last 7 days to avoid re-pulling the entire history.
	// The admin UI manual sync still supports arbitrary date ranges.
	from := time.Now().AddDate(0, 0, -7)
	slog.Info("scheduler: wahoo sync starting", "from", from.Format("2006-01-02"))
	result, err := s.syncer.Sync(context.Background(), wahoo.SyncOptions{From: from})
	if err != nil {
		slog.Error("scheduler: wahoo sync failed", "err", err)
		return
	}
	slog.Info("scheduler: wahoo sync complete",
		"inserted", result.Inserted,
		"skipped", result.Skipped,
		"errors", len(result.Errors),
	)
}

func (s *Scheduler) runFITProcessing() {
	r := s.processor.ProcessAll(context.Background())
	if len(r.Errors) > 0 || len(r.ParseErrors) > 0 {
		slog.Warn("scheduler: FIT processing completed with issues",
			"processed", r.Processed,
			"skipped_no_fit", r.SkippedNoFIT,
			"parse_errors", len(r.ParseErrors),
			"errors", len(r.Errors),
		)
		return
	}
	if r.Processed > 0 {
		slog.Info("scheduler: FIT processing complete",
			"processed", r.Processed,
			"skipped_no_fit", r.SkippedNoFIT,
		)
	}
}

func (s *Scheduler) runWyzeScaleSync() {
	if s.wyze == nil {
		return
	}
	from := time.Now().AddDate(0, 0, -7)
	to := time.Now()
	slog.Info("scheduler: wyze scale sync starting",
		"from", from.Format("2006-01-02"),
		"to", to.Format("2006-01-02"),
	)
	result, err := s.wyze.Import(context.Background(), from, to)
	if err != nil {
		slog.Error("scheduler: wyze scale sync failed", "err", err)
		return
	}
	slog.Info("scheduler: wyze scale sync complete",
		"inserted", result.Inserted,
		"updated", result.Updated,
		"skipped", result.Skipped,
		"conflict_with_manual", result.ConflictWithManual,
		"errors", len(result.Errors),
	)
}

func (s *Scheduler) runWeeklyReport() {
	slog.Info("scheduler: weekly report job starting")

	loc, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		slog.Error("scheduler: load timezone", "err", err)
		return
	}
	now := time.Now().In(loc)

	// Compute the Monday→Sunday window for the current week.
	// time.Weekday: Sunday=0, Monday=1, ..., Saturday=6
	// daysSinceMon: Sun→6, Mon→0, ..., Sat→5
	daysSinceMon := (int(now.Weekday()) + 6) % 7
	weekStart := now.AddDate(0, 0, -daysSinceMon).Truncate(24 * time.Hour)
	weekEnd := weekStart.AddDate(0, 0, 7)

	// Generate weekly report.
	reportID, err := s.orch.Generate(context.Background(), storage.ReportTypeWeeklyReport, weekStart, weekEnd, "")
	if err != nil {
		slog.Error("scheduler: report generation failed",
			"week_start", weekStart.Format("2006-01-02"),
			"err", err,
		)
		return
	}
	slog.Info("scheduler: report generated", "report_id", reportID, "week_start", weekStart.Format("2006-01-02"))

	// Generate weekly plan for the upcoming week.
	planStart := weekEnd
	planEnd := planStart.AddDate(0, 0, 7)
	planID, err := s.orch.Generate(context.Background(), storage.ReportTypeWeeklyPlan, planStart, planEnd, "")
	if err != nil {
		slog.Error("scheduler: plan generation failed",
			"plan_start", planStart.Format("2006-01-02"),
			"err", err,
		)
		// Continue — try to deliver the report even if plan fails.
	} else {
		slog.Info("scheduler: plan generated", "plan_id", planID, "plan_start", planStart.Format("2006-01-02"))
	}

	// Deliver all pending reports via Telegram.
	if s.delivery == nil {
		slog.Info("scheduler: Telegram delivery disabled, skipping send")
		return
	}

	n, err := s.delivery.SendAllUndelivered(context.Background())
	if err != nil {
		slog.Error("scheduler: delivery failed", "err", err)
		return
	}
	slog.Info("scheduler: delivery complete", "sent", n)
}

func (s *Scheduler) runDailyPlaceholder() {
	if s.processor == nil {
		slog.Warn("scheduler: daily placeholder skipped, processor not configured")
		return
	}

	loc, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		slog.Error("scheduler: daily placeholder timezone", "err", err)
		return
	}

	now := time.Now().In(loc)
	id, inserted, err := storage.UpsertDailyPlaceholderWorkout(s.processor.DB(), now, loc)
	if err != nil {
		slog.Error("scheduler: daily placeholder failed", "err", err)
		return
	}
	if inserted {
		slog.Info("scheduler: daily placeholder created", "workout_id", id, "day", now.Format("2006-01-02"))
	}
}

package analysis

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	fitpkg "cycling-coach/internal/fit"
	"cycling-coach/internal/storage"
)

// Processor walks unprocessed workouts, parses their FIT files,
// computes ride metrics, and writes results to the DB.
type Processor struct {
	db           *sql.DB
	fitFilesPath string
}

// NewProcessor constructs a Processor.
func NewProcessor(db *sql.DB, fitFilesPath string) *Processor {
	return &Processor{db: db, fitFilesPath: fitFilesPath}
}

// DB returns the underlying *sql.DB for use by handlers that need storage access.
func (p *Processor) DB() *sql.DB { return p.db }

// ProcessOptions controls which workouts are processed in a single run.
type ProcessOptions struct {
	// WahooID restricts processing to a single workout by its Wahoo ID.
	// The workout is (re)processed regardless of its processed flag.
	WahooID string

	// From/To restrict processing to workouts started in [From, To].
	// Zero values mean no bound. Workouts are reprocessed regardless of processed flag.
	From time.Time
	To   time.Time

	// ReprocessAll reprocesses every workout in the DB, ignoring the processed flag.
	ReprocessAll bool

	// Limit caps the number of unprocessed workouts (ignored when WahooID, From/To, or ReprocessAll is set).
	Limit int
}

// ParseError records a FIT parse failure for a specific workout.
type ParseError struct {
	WahooID string
	Err     error
}

func (e ParseError) Error() string {
	return fmt.Sprintf("%s: %v", e.WahooID, e.Err)
}

// ProcessResult breaks down the outcome of a processing run into three categories
// so callers (and the admin UI) can surface each distinctly.
type ProcessResult struct {
	// Processed is the number of workouts whose FIT file was parsed and metrics saved.
	Processed int
	// SkippedNoFIT is the number of workouts with no FIT file on disk (expected for
	// manually-entered or non-recorded rides). These are marked processed silently.
	SkippedNoFIT int
	// ParseErrors lists workouts whose FIT file exists but could not be parsed
	// (corrupt download, invalid basetype, CRC mismatch). Each entry includes the
	// Wahoo ID so the user can take corrective action (reset + re-sync).
	ParseErrors []ParseError
	// Errors holds unexpected failures (DB errors). Processing continues past these.
	Errors []error
}

// ProcessAll processes every currently unprocessed workout.
// Used by the scheduler.
func (p *Processor) ProcessAll(ctx context.Context) *ProcessResult {
	return p.Process(ctx, ProcessOptions{})
}

// Process fetches and processes workouts according to opts.
// Processing continues past individual failures; all outcomes are returned in ProcessResult.
func (p *Processor) Process(ctx context.Context, opts ProcessOptions) *ProcessResult {
	result := &ProcessResult{}
	var workouts []storage.Workout

	switch {
	case opts.WahooID != "":
		w, err := storage.GetWorkoutByWahooID(p.db, opts.WahooID)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("analysis.Processor.Process: get %s: %w", opts.WahooID, err))
			return result
		}
		workouts = []storage.Workout{*w}

	case opts.ReprocessAll:
		all, err := storage.ListWorkoutsByDateRange(p.db, time.Time{}, time.Now().AddDate(1, 0, 0))
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("analysis.Processor.Process: list all: %w", err))
			return result
		}
		workouts = all

	case !opts.From.IsZero() || !opts.To.IsZero():
		to := opts.To
		if to.IsZero() {
			to = time.Now().AddDate(1, 0, 0)
		}
		all, err := storage.ListWorkoutsByDateRange(p.db, opts.From, to)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("analysis.Processor.Process: list range: %w", err))
			return result
		}
		workouts = all

	default:
		all, err := storage.ListUnprocessedWorkouts(p.db)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("analysis.Processor.Process: list: %w", err))
			return result
		}
		workouts = all
		if opts.Limit > 0 && len(workouts) > opts.Limit {
			workouts = workouts[:opts.Limit]
		}
	}

	for i := range workouts {
		if ctx.Err() != nil {
			break
		}
		outcome, err := p.processOne(ctx, &workouts[i])
		switch outcome {
		case outcomeOK:
			result.Processed++
		case outcomeNoFIT:
			result.SkippedNoFIT++
		case outcomeParseErr:
			result.ParseErrors = append(result.ParseErrors, ParseError{WahooID: workouts[i].WahooID, Err: err})
			slog.Warn("analysis: FIT parse error",
				"wahoo_id", workouts[i].WahooID,
				"err", err,
			)
		case outcomeDBErr:
			result.Errors = append(result.Errors, err)
			slog.Warn("analysis: process workout DB error",
				"wahoo_id", workouts[i].WahooID,
				"err", err,
			)
		}
	}
	return result
}

// ResetFIT clears the FIT file path in the DB, deletes the file from disk if it exists,
// and resets processed=false so the workout will be picked up on the next processing run.
// Called when a FIT file is known to be corrupt and needs to be re-downloaded.
func (p *Processor) ResetFIT(ctx context.Context, wahooID string) error {
	w, err := storage.GetWorkoutByWahooID(p.db, wahooID)
	if err != nil {
		return fmt.Errorf("analysis.Processor.ResetFIT: get workout: %w", err)
	}

	// Remove file from disk (ignore if not present).
	fitPath := ""
	if w.FITFilePath != nil && *w.FITFilePath != "" {
		fitPath = *w.FITFilePath
	} else {
		fitPath = filepath.Join(p.fitFilesPath, wahooID+".fit")
	}
	if err := os.Remove(fitPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("analysis.Processor.ResetFIT: remove file: %w", err)
	}

	// Clear fit_file_path and reset processed=false in the DB.
	if _, err := p.db.ExecContext(ctx,
		`UPDATE workouts SET fit_file_path = NULL, processed = 0 WHERE wahoo_id = ?`, wahooID,
	); err != nil {
		return fmt.Errorf("analysis.Processor.ResetFIT: update DB: %w", err)
	}

	slog.Info("analysis: FIT reset", "wahoo_id", wahooID, "file_removed", fitPath)
	return nil
}

type processOutcome int

const (
	outcomeOK       processOutcome = iota
	outcomeNoFIT                   // file not found — expected, mark processed silently
	outcomeParseErr                // file exists but corrupt — actionable by user
	outcomeDBErr                   // unexpected DB/config error
)

func (p *Processor) processOne(ctx context.Context, w *storage.Workout) (processOutcome, error) {
	fitPath := ""
	if w.FITFilePath != nil && *w.FITFilePath != "" {
		fitPath = *w.FITFilePath
	} else {
		fitPath = filepath.Join(p.fitFilesPath, w.WahooID+".fit")
	}

	parsed, err := fitpkg.ParseFile(fitPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// No FIT file for this workout — expected. Mark processed and move on.
			if markErr := storage.MarkWorkoutProcessed(p.db, w.ID); markErr != nil {
				return outcomeDBErr, fmt.Errorf("analysis.Processor.processOne %s: mark processed: %w", w.WahooID, markErr)
			}
			return outcomeNoFIT, nil
		}
		// File exists but is corrupt (CRC, invalid basetype, etc.).
		// Do NOT mark as processed — leave processed=false so a re-sync + reset can fix it.
		return outcomeParseErr, err
	}

	zones, err := LoadZoneConfig(p.db)
	if err != nil {
		return outcomeDBErr, fmt.Errorf("analysis.Processor.processOne %s: load zones: %w", w.WahooID, err)
	}

	computed := Compute(parsed, zones)
	if err := storage.UpsertRideMetrics(p.db, computed.ToStorageMetrics(w.ID)); err != nil {
		return outcomeDBErr, fmt.Errorf("analysis.Processor.processOne %s: upsert metrics: %w", w.WahooID, err)
	}
	if err := storage.MarkWorkoutProcessed(p.db, w.ID); err != nil {
		return outcomeDBErr, fmt.Errorf("analysis.Processor.processOne %s: mark processed: %w", w.WahooID, err)
	}

	slog.Info("analysis: workout processed",
		"workout_id", w.ID,
		"wahoo_id", w.WahooID,
		"np_watts", computed.NormalizedPower,
		"tss", computed.TSS,
		"decoupling_pct", computed.DecouplingPct,
	)
	return outcomeOK, nil
}

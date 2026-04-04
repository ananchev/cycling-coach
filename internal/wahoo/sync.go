package wahoo

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"cycling-coach/internal/storage"
)

// SyncOptions controls which workouts are fetched during a sync.
// Zero values mean no filter (fetch all).
type SyncOptions struct {
	From time.Time
	To   time.Time
}

// SyncResult holds statistics from a single sync run.
type SyncResult struct {
	Inserted int
	Skipped  int     // workouts already present in DB
	Errors   []error // per-workout errors; sync continues on individual failures
}

// Syncer fetches workouts from the Wahoo API and persists them to SQLite.
type Syncer struct {
	client       *Client
	db           *sql.DB
	fitFilesPath string
	mu           sync.Mutex // prevents concurrent sync runs
}

// NewSyncer constructs a Syncer.
func NewSyncer(client *Client, db *sql.DB, fitFilesPath string) *Syncer {
	return &Syncer{
		client:       client,
		db:           db,
		fitFilesPath: fitFilesPath,
	}
}

// Sync pages through Wahoo workouts and upserts each into SQLite.
// opts.From / opts.To optionally restrict the fetch to a date window (zero = no filter).
// For workouts that include a FIT file URL, the file is downloaded to fitFilesPath
// only if it does not already exist on disk.
// Per-workout errors are collected in SyncResult.Errors; the overall sync is not
// aborted on individual failures.
func (s *Syncer) Sync(ctx context.Context, opts SyncOptions) (*SyncResult, error) {
	if !s.mu.TryLock() {
		slog.Info("wahoo: sync already in progress, skipping")
		return &SyncResult{}, nil
	}
	defer s.mu.Unlock()

	const perPage = 30
	result := &SyncResult{}
	page := 1

	for {
		resp, err := s.client.ListWorkouts(ctx, page, perPage, opts.From, opts.To)
		if err != nil {
			return result, fmt.Errorf("wahoo.Syncer.Sync: page %d: %w", page, err)
		}

		for i := range resp.Workouts {
			if err := s.ingestWorkout(ctx, &resp.Workouts[i], result); err != nil {
				slog.Warn("wahoo: ingest error", "wahoo_id", resp.Workouts[i].ID, "err", err)
				result.Errors = append(result.Errors, err)
			}
		}

		if len(resp.Workouts) == 0 || page*perPage >= resp.Total {
			break
		}
		page++
	}

	slog.Info("wahoo: sync complete",
		"inserted", result.Inserted,
		"skipped", result.Skipped,
		"errors", len(result.Errors),
	)
	return result, nil
}

// IngestAPIWorkout ingests a single workout received from a webhook event.
// It reuses the same idempotent logic as the polling sync — duplicate events
// are silently skipped. Returns true if the workout was newly inserted.
func (s *Syncer) IngestAPIWorkout(ctx context.Context, w *APIWorkout) (bool, error) {
	result := &SyncResult{}
	err := s.ingestWorkout(ctx, w, result)
	return result.Inserted == 1, err
}

func (s *Syncer) ingestWorkout(ctx context.Context, w *APIWorkout, result *SyncResult) error {
	workout := w.ToWorkout()

	_, inserted, err := storage.UpsertWorkout(s.db, workout)
	if err != nil {
		return fmt.Errorf("wahoo.Syncer.ingestWorkout %s: upsert: %w", workout.WahooID, err)
	}

	if inserted {
		result.Inserted++
		slog.Info("wahoo: workout inserted",
			"wahoo_id", workout.WahooID,
			"started_at", workout.StartedAt,
		)
	} else {
		result.Skipped++
	}

	// Download FIT file if a URL is present and the file does not already exist.
	if w.Summary == nil || w.Summary.File == nil || w.Summary.File.URL == "" {
		return nil
	}
	destPath := filepath.Join(s.fitFilesPath, workout.WahooID+".fit")
	if _, statErr := os.Stat(destPath); statErr == nil {
		return nil // already on disk
	}

	if err := s.client.DownloadFIT(ctx, w.Summary.File.URL, destPath); err != nil {
		// FIT download failure is logged and recorded but does not block ingestion.
		return fmt.Errorf("wahoo.Syncer.ingestWorkout %s: download FIT: %w", workout.WahooID, err)
	}

	// Record the local path in the DB.
	if _, err := s.db.ExecContext(ctx,
		`UPDATE workouts SET fit_file_path = ? WHERE wahoo_id = ?`, destPath, workout.WahooID,
	); err != nil {
		slog.Warn("wahoo: update fit_file_path failed", "wahoo_id", workout.WahooID, "err", err)
	}
	slog.Info("wahoo: FIT file downloaded", "wahoo_id", workout.WahooID, "path", destPath)
	return nil
}

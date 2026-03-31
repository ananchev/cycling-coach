package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// Workout represents a row in the workouts table.
type Workout struct {
	ID          int64
	WahooID     string
	StartedAt   time.Time
	DurationSec *int64
	DistanceM   *float64
	Calories    *int64
	AvgHR       *int64
	MaxHR       *int64
	AvgPower    *float64
	MaxPower    *float64
	AvgCadence  *float64
	WorkoutType *string
	FITFilePath *string
	Source      string // 'api', 'csv', 'manual'
	Processed   bool
	CreatedAt   time.Time
}

// UpsertWorkout inserts a workout if one with the same wahoo_id does not already exist.
// Returns the row ID of the existing or newly inserted row.
func UpsertWorkout(db *sql.DB, w *Workout) (int64, error) {
	_, err := db.Exec(`
		INSERT INTO workouts(
			wahoo_id, started_at, duration_sec, distance_m, calories,
			avg_hr, max_hr, avg_power, max_power, avg_cadence,
			workout_type, fit_file_path, source, processed
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(wahoo_id) DO NOTHING`,
		w.WahooID, w.StartedAt, w.DurationSec, w.DistanceM, w.Calories,
		w.AvgHR, w.MaxHR, w.AvgPower, w.MaxPower, w.AvgCadence,
		w.WorkoutType, w.FITFilePath, w.Source, w.Processed,
	)
	if err != nil {
		return 0, fmt.Errorf("storage.UpsertWorkout: %w", err)
	}

	var id int64
	err = db.QueryRow(`SELECT id FROM workouts WHERE wahoo_id = ?`, w.WahooID).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("storage.UpsertWorkout: select id: %w", err)
	}
	return id, nil
}

// GetWorkoutByWahooID returns the workout with the given wahoo_id, or sql.ErrNoRows.
func GetWorkoutByWahooID(db *sql.DB, wahooID string) (*Workout, error) {
	row := db.QueryRow(`
		SELECT id, wahoo_id, started_at, duration_sec, distance_m, calories,
		       avg_hr, max_hr, avg_power, max_power, avg_cadence,
		       workout_type, fit_file_path, source, processed, created_at
		FROM workouts WHERE wahoo_id = ?`, wahooID)
	w, err := scanWorkout(row)
	if err != nil {
		return nil, fmt.Errorf("storage.GetWorkoutByWahooID: %w", err)
	}
	return w, nil
}

// ListUnprocessedWorkouts returns all workouts where processed = false, ordered by started_at ASC.
func ListUnprocessedWorkouts(db *sql.DB) ([]Workout, error) {
	rows, err := db.Query(`
		SELECT id, wahoo_id, started_at, duration_sec, distance_m, calories,
		       avg_hr, max_hr, avg_power, max_power, avg_cadence,
		       workout_type, fit_file_path, source, processed, created_at
		FROM workouts WHERE processed = 0 ORDER BY started_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("storage.ListUnprocessedWorkouts: %w", err)
	}
	defer rows.Close()
	return scanWorkouts(rows)
}

// MarkWorkoutProcessed sets processed = true for the given workout ID.
func MarkWorkoutProcessed(db *sql.DB, id int64) error {
	_, err := db.Exec(`UPDATE workouts SET processed = 1 WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("storage.MarkWorkoutProcessed: %w", err)
	}
	return nil
}

// ListWorkoutsByDateRange returns workouts with started_at in [from, to], ordered by started_at ASC.
func ListWorkoutsByDateRange(db *sql.DB, from, to time.Time) ([]Workout, error) {
	rows, err := db.Query(`
		SELECT id, wahoo_id, started_at, duration_sec, distance_m, calories,
		       avg_hr, max_hr, avg_power, max_power, avg_cadence,
		       workout_type, fit_file_path, source, processed, created_at
		FROM workouts WHERE started_at >= ? AND started_at <= ? ORDER BY started_at ASC`,
		from, to)
	if err != nil {
		return nil, fmt.Errorf("storage.ListWorkoutsByDateRange: %w", err)
	}
	defer rows.Close()
	return scanWorkouts(rows)
}

// scanner abstracts *sql.Row and *sql.Rows so a single scan function handles both.
type scanner interface {
	Scan(dest ...any) error
}

func scanWorkout(s scanner) (*Workout, error) {
	var w Workout
	err := s.Scan(
		&w.ID, &w.WahooID, &w.StartedAt,
		&w.DurationSec, &w.DistanceM, &w.Calories,
		&w.AvgHR, &w.MaxHR, &w.AvgPower, &w.MaxPower, &w.AvgCadence,
		&w.WorkoutType, &w.FITFilePath, &w.Source, &w.Processed, &w.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &w, nil
}

func scanWorkouts(rows *sql.Rows) ([]Workout, error) {
	var out []Workout
	for rows.Next() {
		w, err := scanWorkout(rows)
		if err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		out = append(out, *w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	return out, nil
}

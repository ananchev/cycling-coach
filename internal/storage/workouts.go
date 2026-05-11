package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// Workout represents a row in the workouts table.
type Workout struct {
	ID            int64
	WahooID       string
	StartedAt     time.Time
	DurationSec   *int64
	DistanceM     *float64
	Calories      *int64
	AvgHR         *int64
	MaxHR         *int64
	AvgPower      *float64
	MaxPower      *float64
	AvgCadence    *float64
	WorkoutType   *string
	WorkoutTypeID *int64
	FITFilePath   *string
	Source        string // 'api', 'csv', 'manual'
	Processed     bool
	CreatedAt     time.Time
}

// UpsertWorkout inserts a workout if one with the same wahoo_id does not already exist.
// Returns the row ID of the existing or newly inserted row, and whether the row was
// newly inserted (true) or already existed (false).
func UpsertWorkout(db *sql.DB, w *Workout) (id int64, inserted bool, err error) {
	res, err := db.Exec(`
		INSERT INTO workouts(
			wahoo_id, started_at, duration_sec, distance_m, calories,
			avg_hr, max_hr, avg_power, max_power, avg_cadence,
			workout_type, workout_type_id, fit_file_path, source, processed
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(wahoo_id) DO NOTHING`,
		w.WahooID, w.StartedAt, w.DurationSec, w.DistanceM, w.Calories,
		w.AvgHR, w.MaxHR, w.AvgPower, w.MaxPower, w.AvgCadence,
		w.WorkoutType, w.WorkoutTypeID, w.FITFilePath, w.Source, w.Processed,
	)
	if err != nil {
		return 0, false, fmt.Errorf("storage.UpsertWorkout: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return 0, false, fmt.Errorf("storage.UpsertWorkout: rows affected: %w", err)
	}

	if n == 1 {
		// Newly inserted row — LastInsertId is reliable.
		id, err = res.LastInsertId()
		if err != nil {
			return 0, true, fmt.Errorf("storage.UpsertWorkout: last insert id: %w", err)
		}
		return id, true, nil
	}

	// Row already existed — SELECT to get its ID.
	err = db.QueryRow(`SELECT id FROM workouts WHERE wahoo_id = ?`, w.WahooID).Scan(&id)
	if err != nil {
		return 0, false, fmt.Errorf("storage.UpsertWorkout: select existing id: %w", err)
	}
	return id, false, nil
}

// GetWorkoutByWahooID returns the workout with the given wahoo_id, or sql.ErrNoRows.
func GetWorkoutByWahooID(db *sql.DB, wahooID string) (*Workout, error) {
	row := db.QueryRow(`
		SELECT id, wahoo_id, started_at, duration_sec, distance_m, calories,
		       avg_hr, max_hr, avg_power, max_power, avg_cadence,
		       workout_type, workout_type_id, fit_file_path, source, processed, created_at
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
		       workout_type, workout_type_id, fit_file_path, source, processed, created_at
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
		       workout_type, workout_type_id, fit_file_path, source, processed, created_at
		FROM workouts WHERE started_at >= ? AND started_at <= ? ORDER BY started_at ASC`,
		from, to)
	if err != nil {
		return nil, fmt.Errorf("storage.ListWorkoutsByDateRange: %w", err)
	}
	defer rows.Close()
	return scanWorkouts(rows)
}

// WorkoutWithMetrics is a joined row for the admin workouts grid.
type WorkoutWithMetrics struct {
	ID          int64
	WahooID     string
	StartedAt   time.Time
	DurationSec *int64
	WorkoutType *string // resolved description from workout_types table
	Source      string
	Processed   bool
	FITFilePath *string
	AvgCadence  *float64
	// Metrics (nil when not yet computed)
	AvgPower        *float64
	AvgHR           *float64
	NormalizedPower *float64
	TSS             *float64
	HRDriftPct      *float64
	IntensityFactor *float64
	PwrZ1Pct        *float64
	PwrZ2Pct        *float64
	PwrZ3Pct        *float64
	PwrZ4Pct        *float64
	PwrZ5Pct        *float64
	HRZ1Pct         *float64
	HRZ2Pct         *float64
	HRZ3Pct         *float64
	HRZ4Pct         *float64
	HRZ5Pct         *float64
	CadLT70Pct      *float64
	Cad70To85Pct    *float64
	Cad85To100Pct   *float64
	CadGE100Pct     *float64
	VariabilityIndex *float64
	ZoneTimeline     *string
	HRZoneTimeline   *string
	// Notes linked to this workout, split by type
	RideNotes    *string // type='ride' notes concatenated
	GeneralNotes *string // type='note' notes concatenated
}

// ListWorkoutsWithMetrics returns workouts LEFT JOINed with ride_metrics and workout_types, ordered by started_at DESC.
func ListWorkoutsWithMetrics(db *sql.DB, from, to time.Time, limit int) ([]WorkoutWithMetrics, error) {
	query := `
		SELECT w.id, w.wahoo_id, w.started_at, w.duration_sec,
		       COALESCE(wt.description, w.workout_type),
		       w.source, w.processed, w.fit_file_path, w.avg_cadence,
		       m.avg_power, m.avg_hr, m.normalized_power, m.tss, m.hr_drift_pct, m.intensity_factor,
		       m.pwr_z1_pct, m.pwr_z2_pct, m.pwr_z3_pct, m.pwr_z4_pct, m.pwr_z5_pct,
		       m.hr_z1_pct, m.hr_z2_pct, m.hr_z3_pct, m.hr_z4_pct, m.hr_z5_pct,
		       m.cad_lt70_pct, m.cad_70_85_pct, m.cad_85_100_pct, m.cad_ge100_pct,
		       m.zone_timeline, m.hr_zone_timeline, m.variability_index,
		       GROUP_CONCAT(CASE WHEN n.type='ride' THEN n.note END, ' | '),
		       GROUP_CONCAT(CASE WHEN n.type='note' THEN n.note END, ' | ')
		FROM workouts w
		LEFT JOIN ride_metrics m ON m.workout_id = w.id
		LEFT JOIN workout_types wt ON wt.id = w.workout_type_id
		LEFT JOIN athlete_notes n ON n.workout_id = w.id AND n.note IS NOT NULL AND n.note != ''
		WHERE 1=1`
	args := []any{}
	if !from.IsZero() {
		query += " AND w.started_at >= ?"
		args = append(args, from)
	}
	if !to.IsZero() {
		query += " AND w.started_at <= ?"
		args = append(args, to)
	}
	query += " GROUP BY w.id ORDER BY w.started_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("storage.ListWorkoutsWithMetrics: %w", err)
	}
	defer rows.Close()

	var out []WorkoutWithMetrics
	for rows.Next() {
		var wm WorkoutWithMetrics
		if err := rows.Scan(
			&wm.ID, &wm.WahooID, &wm.StartedAt, &wm.DurationSec, &wm.WorkoutType,
			&wm.Source, &wm.Processed, &wm.FITFilePath, &wm.AvgCadence,
			&wm.AvgPower, &wm.AvgHR, &wm.NormalizedPower, &wm.TSS, &wm.HRDriftPct, &wm.IntensityFactor,
			&wm.PwrZ1Pct, &wm.PwrZ2Pct, &wm.PwrZ3Pct, &wm.PwrZ4Pct, &wm.PwrZ5Pct,
			&wm.HRZ1Pct, &wm.HRZ2Pct, &wm.HRZ3Pct, &wm.HRZ4Pct, &wm.HRZ5Pct,
			&wm.CadLT70Pct, &wm.Cad70To85Pct, &wm.Cad85To100Pct, &wm.CadGE100Pct,
			&wm.ZoneTimeline, &wm.HRZoneTimeline, &wm.VariabilityIndex,
			&wm.RideNotes, &wm.GeneralNotes,
		); err != nil {
			return nil, fmt.Errorf("storage.ListWorkoutsWithMetrics: scan: %w", err)
		}
		out = append(out, wm)
	}
	return out, rows.Err()
}

// GetWorkoutWithMetricsByID returns a single workout row with joined metrics and resolved workout type.
func GetWorkoutWithMetricsByID(db *sql.DB, id int64) (*WorkoutWithMetrics, error) {
	row := db.QueryRow(`
		SELECT w.id, w.wahoo_id, w.started_at, w.duration_sec,
		       COALESCE(wt.description, w.workout_type),
		       w.source, w.processed, w.fit_file_path, w.avg_cadence,
		       m.avg_power, m.avg_hr, m.normalized_power, m.tss, m.hr_drift_pct, m.intensity_factor,
		       m.pwr_z1_pct, m.pwr_z2_pct, m.pwr_z3_pct, m.pwr_z4_pct, m.pwr_z5_pct,
		       m.hr_z1_pct, m.hr_z2_pct, m.hr_z3_pct, m.hr_z4_pct, m.hr_z5_pct,
		       m.cad_lt70_pct, m.cad_70_85_pct, m.cad_85_100_pct, m.cad_ge100_pct,
		       m.zone_timeline, m.hr_zone_timeline, m.variability_index,
		       GROUP_CONCAT(CASE WHEN n.type='ride' THEN n.note END, ' | '),
		       GROUP_CONCAT(CASE WHEN n.type='note' THEN n.note END, ' | ')
		FROM workouts w
		LEFT JOIN ride_metrics m ON m.workout_id = w.id
		LEFT JOIN workout_types wt ON wt.id = w.workout_type_id
		LEFT JOIN athlete_notes n ON n.workout_id = w.id AND n.note IS NOT NULL AND n.note != ''
		WHERE w.id = ?
		GROUP BY w.id`, id)

	var wm WorkoutWithMetrics
	if err := row.Scan(
		&wm.ID, &wm.WahooID, &wm.StartedAt, &wm.DurationSec, &wm.WorkoutType,
		&wm.Source, &wm.Processed, &wm.FITFilePath, &wm.AvgCadence,
		&wm.AvgPower, &wm.AvgHR, &wm.NormalizedPower, &wm.TSS, &wm.HRDriftPct, &wm.IntensityFactor,
		&wm.PwrZ1Pct, &wm.PwrZ2Pct, &wm.PwrZ3Pct, &wm.PwrZ4Pct, &wm.PwrZ5Pct,
		&wm.HRZ1Pct, &wm.HRZ2Pct, &wm.HRZ3Pct, &wm.HRZ4Pct, &wm.HRZ5Pct,
		&wm.CadLT70Pct, &wm.Cad70To85Pct, &wm.Cad85To100Pct, &wm.CadGE100Pct,
		&wm.ZoneTimeline, &wm.HRZoneTimeline, &wm.VariabilityIndex,
		&wm.RideNotes, &wm.GeneralNotes,
	); err != nil {
		return nil, fmt.Errorf("storage.GetWorkoutWithMetricsByID: %w", err)
	}
	return &wm, nil
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
		&w.WorkoutType, &w.WorkoutTypeID, &w.FITFilePath, &w.Source, &w.Processed, &w.CreatedAt,
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

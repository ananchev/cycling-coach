package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// RideMetrics represents a row in the ride_metrics table.
type RideMetrics struct {
	ID               int64
	WorkoutID        int64
	DurationMin      *float64
	AvgHR            *float64
	MaxHR            *float64
	AvgPower         *float64
	MaxPower         *float64
	AvgCadence       *float64
	NormalizedPower  *float64
	IntensityFactor  *float64
	TSS              *float64
	TRIMP            *float64
	EfficiencyFactor *float64
	HRDriftPct       *float64
	DecouplingPct    *float64
	HRZ1Pct          *float64
	HRZ2Pct          *float64
	HRZ3Pct          *float64
	HRZ4Pct          *float64
	HRZ5Pct          *float64
	PwrZ1Pct         *float64
	PwrZ2Pct         *float64
	PwrZ3Pct         *float64
	PwrZ4Pct         *float64
	PwrZ5Pct         *float64
	ZoneTimeline     *string // JSON array of power zone segments
	CreatedAt        time.Time
}

// UpsertRideMetrics inserts or updates ride metrics for a given workout.
// On conflict (workout_id already exists) all metric columns are updated.
func UpsertRideMetrics(db *sql.DB, m *RideMetrics) error {
	_, err := db.Exec(`
		INSERT INTO ride_metrics(
			workout_id, duration_min, avg_hr, max_hr, avg_power, max_power,
			avg_cadence, normalized_power, intensity_factor, tss, trimp,
			efficiency_factor, hr_drift_pct, decoupling_pct,
			hr_z1_pct, hr_z2_pct, hr_z3_pct, hr_z4_pct, hr_z5_pct,
			pwr_z1_pct, pwr_z2_pct, pwr_z3_pct, pwr_z4_pct, pwr_z5_pct,
			zone_timeline
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(workout_id) DO UPDATE SET
			duration_min=excluded.duration_min,
			avg_hr=excluded.avg_hr,
			max_hr=excluded.max_hr,
			avg_power=excluded.avg_power,
			max_power=excluded.max_power,
			avg_cadence=excluded.avg_cadence,
			normalized_power=excluded.normalized_power,
			intensity_factor=excluded.intensity_factor,
			tss=excluded.tss,
			trimp=excluded.trimp,
			efficiency_factor=excluded.efficiency_factor,
			hr_drift_pct=excluded.hr_drift_pct,
			decoupling_pct=excluded.decoupling_pct,
			hr_z1_pct=excluded.hr_z1_pct,
			hr_z2_pct=excluded.hr_z2_pct,
			hr_z3_pct=excluded.hr_z3_pct,
			hr_z4_pct=excluded.hr_z4_pct,
			hr_z5_pct=excluded.hr_z5_pct,
			pwr_z1_pct=excluded.pwr_z1_pct,
			pwr_z2_pct=excluded.pwr_z2_pct,
			pwr_z3_pct=excluded.pwr_z3_pct,
			pwr_z4_pct=excluded.pwr_z4_pct,
			pwr_z5_pct=excluded.pwr_z5_pct,
			zone_timeline=excluded.zone_timeline`,
		m.WorkoutID, m.DurationMin, m.AvgHR, m.MaxHR, m.AvgPower, m.MaxPower,
		m.AvgCadence, m.NormalizedPower, m.IntensityFactor, m.TSS, m.TRIMP,
		m.EfficiencyFactor, m.HRDriftPct, m.DecouplingPct,
		m.HRZ1Pct, m.HRZ2Pct, m.HRZ3Pct, m.HRZ4Pct, m.HRZ5Pct,
		m.PwrZ1Pct, m.PwrZ2Pct, m.PwrZ3Pct, m.PwrZ4Pct, m.PwrZ5Pct,
		m.ZoneTimeline,
	)
	if err != nil {
		return fmt.Errorf("storage.UpsertRideMetrics: %w", err)
	}
	return nil
}

// GetRideMetrics returns metrics for the given workout ID, or sql.ErrNoRows.
func GetRideMetrics(db *sql.DB, workoutID int64) (*RideMetrics, error) {
	row := db.QueryRow(`
		SELECT id, workout_id, duration_min, avg_hr, max_hr, avg_power, max_power,
		       avg_cadence, normalized_power, intensity_factor, tss, trimp,
		       efficiency_factor, hr_drift_pct, decoupling_pct,
		       hr_z1_pct, hr_z2_pct, hr_z3_pct, hr_z4_pct, hr_z5_pct,
		       pwr_z1_pct, pwr_z2_pct, pwr_z3_pct, pwr_z4_pct, pwr_z5_pct,
		       zone_timeline, created_at
		FROM ride_metrics WHERE workout_id = ?`, workoutID)

	var m RideMetrics
	err := row.Scan(
		&m.ID, &m.WorkoutID, &m.DurationMin, &m.AvgHR, &m.MaxHR, &m.AvgPower, &m.MaxPower,
		&m.AvgCadence, &m.NormalizedPower, &m.IntensityFactor, &m.TSS, &m.TRIMP,
		&m.EfficiencyFactor, &m.HRDriftPct, &m.DecouplingPct,
		&m.HRZ1Pct, &m.HRZ2Pct, &m.HRZ3Pct, &m.HRZ4Pct, &m.HRZ5Pct,
		&m.PwrZ1Pct, &m.PwrZ2Pct, &m.PwrZ3Pct, &m.PwrZ4Pct, &m.PwrZ5Pct,
		&m.ZoneTimeline, &m.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("storage.GetRideMetrics: %w", err)
	}
	return &m, nil
}

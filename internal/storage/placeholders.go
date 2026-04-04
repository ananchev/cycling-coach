package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const PlaceholderWorkoutType = "No training logged"

// PlaceholderWorkoutWahooID returns the synthetic Wahoo ID used for daily
// placeholder workouts that represent a day with no recorded training.
func PlaceholderWorkoutWahooID(day time.Time, loc *time.Location) string {
	return "manual-empty-" + day.In(loc).Format("2006-01-02")
}

// UpsertDailyPlaceholderWorkout inserts a manual placeholder workout for the
// given local day if and only if no workout already exists in that day window.
// The inserted row is marked processed because no FIT file is expected.
func UpsertDailyPlaceholderWorkout(db *sql.DB, day time.Time, loc *time.Location) (int64, bool, error) {
	localDay := day.In(loc)
	dayStart := time.Date(localDay.Year(), localDay.Month(), localDay.Day(), 0, 0, 0, 0, loc)
	dayEnd := dayStart.AddDate(0, 0, 1).Add(-time.Nanosecond)

	workouts, err := ListWorkoutsByDateRange(db, dayStart.UTC(), dayEnd.UTC())
	if err != nil {
		return 0, false, fmt.Errorf("storage.UpsertDailyPlaceholderWorkout: list day workouts: %w", err)
	}
	if len(workouts) > 0 {
		return 0, false, nil
	}

	wType := PlaceholderWorkoutType
	id, inserted, err := UpsertWorkout(db, &Workout{
		WahooID:     PlaceholderWorkoutWahooID(localDay, loc),
		StartedAt:   time.Date(localDay.Year(), localDay.Month(), localDay.Day(), 12, 0, 0, 0, loc).UTC(),
		WorkoutType: &wType,
		Source:      "manual",
		Processed:   true,
	})
	if err != nil {
		return 0, false, fmt.Errorf("storage.UpsertDailyPlaceholderWorkout: upsert: %w", err)
	}
	return id, inserted, nil
}

// ReconcilePlaceholderWorkout moves any notes from the placeholder workout for
// the given day to the actual workout and removes the placeholder row.
func ReconcilePlaceholderWorkout(db *sql.DB, actualWorkoutID int64, day time.Time, loc *time.Location) error {
	placeholder, err := GetWorkoutByWahooID(db, PlaceholderWorkoutWahooID(day, loc))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("storage.ReconcilePlaceholderWorkout: get placeholder: %w", err)
	}
	if placeholder.ID == actualWorkoutID {
		return nil
	}

	if _, err := db.Exec(`UPDATE athlete_notes SET workout_id = ? WHERE workout_id = ?`, actualWorkoutID, placeholder.ID); err != nil {
		return fmt.Errorf("storage.ReconcilePlaceholderWorkout: move notes: %w", err)
	}
	if _, err := db.Exec(`DELETE FROM workouts WHERE id = ?`, placeholder.ID); err != nil {
		return fmt.Errorf("storage.ReconcilePlaceholderWorkout: delete placeholder: %w", err)
	}
	return nil
}

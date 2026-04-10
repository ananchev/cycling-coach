package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// NoteType is the discriminator for athlete_notes rows.
type NoteType string

const (
	NoteTypeRide   NoteType = "ride"
	NoteTypeNote   NoteType = "note"
	NoteTypeWeight NoteType = "weight"
)

// AthleteNote represents a row in the athlete_notes table.
type AthleteNote struct {
	ID           int64
	Timestamp    time.Time
	Type         NoteType
	RPE          *int64
	WeightKG     *float64
	BodyFatPct   *float64
	MuscleMassKG *float64
	BodyWaterPct *float64
	BMRKcal      *float64
	Note         *string
	WorkoutID    *int64
	CreatedAt    time.Time
}

// InsertNote inserts an athlete note and returns its ID.
func InsertNote(db *sql.DB, n *AthleteNote) (int64, error) {
	res, err := db.Exec(`
		INSERT INTO athlete_notes(timestamp, type, rpe, weight_kg, body_fat_pct, muscle_mass_kg, body_water_pct, bmr_kcal, note, workout_id)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.Timestamp, string(n.Type), n.RPE, n.WeightKG, n.BodyFatPct, n.MuscleMassKG, n.BodyWaterPct, n.BMRKcal, n.Note, n.WorkoutID,
	)
	if err != nil {
		return 0, fmt.Errorf("storage.InsertNote: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("storage.InsertNote: last insert id: %w", err)
	}
	return id, nil
}

// ListNotesByDateRange returns notes with timestamp in [from, to], ordered by timestamp ASC.
func ListNotesByDateRange(db *sql.DB, from, to time.Time) ([]AthleteNote, error) {
	rows, err := db.Query(`
		SELECT id, timestamp, type, rpe, weight_kg, body_fat_pct, muscle_mass_kg, body_water_pct, bmr_kcal, note, workout_id, created_at
		FROM athlete_notes
		WHERE timestamp >= ? AND timestamp <= ?
		ORDER BY timestamp ASC`,
		from, to)
	if err != nil {
		return nil, fmt.Errorf("storage.ListNotesByDateRange: %w", err)
	}
	defer rows.Close()

	var out []AthleteNote
	for rows.Next() {
		var note AthleteNote
		var noteType string
		err := rows.Scan(
			&note.ID, &note.Timestamp, &noteType,
			&note.RPE, &note.WeightKG, &note.BodyFatPct, &note.MuscleMassKG, &note.BodyWaterPct, &note.BMRKcal,
			&note.Note, &note.WorkoutID, &note.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("storage.ListNotesByDateRange: scan: %w", err)
		}
		note.Type = NoteType(noteType)
		out = append(out, note)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage.ListNotesByDateRange: rows: %w", err)
	}
	return out, nil
}

// ListAllNotes returns all notes ordered by timestamp DESC, with optional limit.
func ListAllNotes(db *sql.DB, limit int) ([]AthleteNote, error) {
	rows, err := db.Query(`
		SELECT id, timestamp, type, rpe, weight_kg, body_fat_pct, muscle_mass_kg, body_water_pct, bmr_kcal, note, workout_id, created_at
		FROM athlete_notes
		ORDER BY timestamp DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("storage.ListAllNotes: %w", err)
	}
	defer rows.Close()

	var out []AthleteNote
	for rows.Next() {
		var note AthleteNote
		var noteType string
		err := rows.Scan(
			&note.ID, &note.Timestamp, &noteType,
			&note.RPE, &note.WeightKG, &note.BodyFatPct, &note.MuscleMassKG, &note.BodyWaterPct, &note.BMRKcal,
			&note.Note, &note.WorkoutID, &note.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("storage.ListAllNotes: scan: %w", err)
		}
		note.Type = NoteType(noteType)
		out = append(out, note)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage.ListAllNotes: rows: %w", err)
	}
	return out, nil
}

// ListBodyMetrics returns weight-type notes that have at least one body metric, ordered by timestamp ASC.
// Zero from/to values mean no lower/upper bound.
func ListBodyMetrics(db *sql.DB, from, to time.Time, limit int) ([]AthleteNote, error) {
	query := `
		SELECT id, timestamp, type, rpe, weight_kg, body_fat_pct, muscle_mass_kg, body_water_pct, bmr_kcal, note, workout_id, created_at
		FROM athlete_notes n
		WHERE type = 'weight'
		  AND id NOT IN (SELECT manual_note_id FROM wyze_scale_conflicts)
		  AND NOT EXISTS (
			SELECT 1
			FROM wyze_scale_imports wi
			JOIN athlete_notes wn ON wn.id = wi.athlete_note_id
			WHERE wi.athlete_note_id != n.id
			  AND date(wn.timestamp) = date(n.timestamp)
			  AND (
				(n.weight_kg IS NOT NULL AND wn.weight_kg = n.weight_kg) OR
				(n.body_fat_pct IS NOT NULL AND wn.body_fat_pct = n.body_fat_pct) OR
				(n.muscle_mass_kg IS NOT NULL AND wn.muscle_mass_kg = n.muscle_mass_kg) OR
				(n.body_water_pct IS NOT NULL AND wn.body_water_pct = n.body_water_pct) OR
				(n.bmr_kcal IS NOT NULL AND wn.bmr_kcal = n.bmr_kcal)
			  )
		  )
		  AND (weight_kg IS NOT NULL OR body_fat_pct IS NOT NULL OR muscle_mass_kg IS NOT NULL OR body_water_pct IS NOT NULL OR bmr_kcal IS NOT NULL)`
	args := []any{}
	if !from.IsZero() {
		query += " AND timestamp >= ?"
		args = append(args, from)
	}
	if !to.IsZero() {
		query += " AND timestamp <= ?"
		args = append(args, to)
	}
	query += " ORDER BY timestamp ASC LIMIT ?"
	args = append(args, limit)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("storage.ListBodyMetrics: %w", err)
	}
	defer rows.Close()

	var out []AthleteNote
	for rows.Next() {
		var note AthleteNote
		var noteType string
		err := rows.Scan(
			&note.ID, &note.Timestamp, &noteType,
			&note.RPE, &note.WeightKG, &note.BodyFatPct, &note.MuscleMassKG, &note.BodyWaterPct, &note.BMRKcal,
			&note.Note, &note.WorkoutID, &note.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("storage.ListBodyMetrics: scan: %w", err)
		}
		note.Type = NoteType(noteType)
		out = append(out, note)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage.ListBodyMetrics: rows: %w", err)
	}
	return out, nil
}

// ListNotesByWorkout returns notes linked to a specific workout, ordered by timestamp ASC.
// If noteType is non-empty, only notes of that type are returned.
func ListNotesByWorkout(db *sql.DB, workoutID int64, noteType string) ([]AthleteNote, error) {
	query := `SELECT id, timestamp, type, rpe, weight_kg, body_fat_pct, muscle_mass_kg, body_water_pct, bmr_kcal, note, workout_id, created_at
		FROM athlete_notes
		WHERE workout_id = ?`
	args := []any{workoutID}
	if noteType != "" {
		query += " AND type = ?"
		args = append(args, noteType)
	}
	query += " ORDER BY timestamp ASC"
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("storage.ListNotesByWorkout: %w", err)
	}
	defer rows.Close()

	var out []AthleteNote
	for rows.Next() {
		var note AthleteNote
		var noteType string
		err := rows.Scan(
			&note.ID, &note.Timestamp, &noteType,
			&note.RPE, &note.WeightKG, &note.BodyFatPct, &note.MuscleMassKG, &note.BodyWaterPct, &note.BMRKcal,
			&note.Note, &note.WorkoutID, &note.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("storage.ListNotesByWorkout: scan: %w", err)
		}
		note.Type = NoteType(noteType)
		out = append(out, note)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage.ListNotesByWorkout: rows: %w", err)
	}
	return out, nil
}

// UpdateNote updates an existing note's mutable fields.
func UpdateNote(db *sql.DB, n *AthleteNote) error {
	_, err := db.Exec(`
		UPDATE athlete_notes
		SET type = ?, rpe = ?, weight_kg = ?, body_fat_pct = ?, muscle_mass_kg = ?, body_water_pct = ?, bmr_kcal = ?, note = ?
		WHERE id = ?`,
		string(n.Type), n.RPE, n.WeightKG, n.BodyFatPct, n.MuscleMassKG, n.BodyWaterPct, n.BMRKcal, n.Note, n.ID,
	)
	if err != nil {
		return fmt.Errorf("storage.UpdateNote: %w", err)
	}
	return nil
}

// DeleteNote deletes a note by ID.
func DeleteNote(db *sql.DB, id int64) error {
	res, err := db.Exec(`DELETE FROM athlete_notes WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("storage.DeleteNote: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// LinkNoteToWorkout associates a note with a workout by setting workout_id.
func LinkNoteToWorkout(db *sql.DB, noteID, workoutID int64) error {
	_, err := db.Exec(`UPDATE athlete_notes SET workout_id = ? WHERE id = ?`, workoutID, noteID)
	if err != nil {
		return fmt.Errorf("storage.LinkNoteToWorkout: %w", err)
	}
	return nil
}

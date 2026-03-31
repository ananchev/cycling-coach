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
	ID        int64
	Timestamp time.Time
	Type      NoteType
	RPE       *int64
	WeightKG  *float64
	Note      *string
	WorkoutID *int64
	CreatedAt time.Time
}

// InsertNote inserts an athlete note and returns its ID.
func InsertNote(db *sql.DB, n *AthleteNote) (int64, error) {
	res, err := db.Exec(`
		INSERT INTO athlete_notes(timestamp, type, rpe, weight_kg, note, workout_id)
		VALUES(?, ?, ?, ?, ?, ?)`,
		n.Timestamp, string(n.Type), n.RPE, n.WeightKG, n.Note, n.WorkoutID,
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
		SELECT id, timestamp, type, rpe, weight_kg, note, workout_id, created_at
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
			&note.RPE, &note.WeightKG, &note.Note, &note.WorkoutID, &note.CreatedAt,
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

// LinkNoteToWorkout associates a note with a workout by setting workout_id.
func LinkNoteToWorkout(db *sql.DB, noteID, workoutID int64) error {
	_, err := db.Exec(`UPDATE athlete_notes SET workout_id = ? WHERE id = ?`, workoutID, noteID)
	if err != nil {
		return fmt.Errorf("storage.LinkNoteToWorkout: %w", err)
	}
	return nil
}

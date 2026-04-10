package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const WyzeConflictTypeManual = "conflict_with_manual"

type WyzeScaleConflict struct {
	ID           int64
	WyzeRecordID string
	ManualNoteID int64
	WyzeNoteID   int64
	ConflictType string
	CreatedAt    time.Time
}

type WyzeScaleConflictDetail struct {
	WyzeScaleConflict
	ManualNote AthleteNote
	WyzeNote   AthleteNote
}

func InsertWyzeScaleConflict(db *sql.DB, conflict *WyzeScaleConflict) (int64, error) {
	res, err := db.Exec(`
		INSERT INTO wyze_scale_conflicts(wyze_record_id, manual_note_id, wyze_note_id, conflict_type)
		VALUES(?, ?, ?, ?)`,
		conflict.WyzeRecordID, conflict.ManualNoteID, conflict.WyzeNoteID, conflict.ConflictType,
	)
	if err != nil {
		return 0, fmt.Errorf("storage.InsertWyzeScaleConflict: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("storage.InsertWyzeScaleConflict: last insert id: %w", err)
	}
	return id, nil
}

func ListWyzeScaleConflicts(db *sql.DB, limit int) ([]WyzeScaleConflictDetail, error) {
	rows, err := db.Query(`
		SELECT
			c.id, c.wyze_record_id, c.manual_note_id, c.wyze_note_id, c.conflict_type, c.created_at,
			m.id, m.timestamp, m.type, m.rpe, m.weight_kg, m.body_fat_pct, m.muscle_mass_kg, m.body_water_pct, m.bmr_kcal, m.note, m.workout_id, m.created_at,
			w.id, w.timestamp, w.type, w.rpe, w.weight_kg, w.body_fat_pct, w.muscle_mass_kg, w.body_water_pct, w.bmr_kcal, w.note, w.workout_id, w.created_at
		FROM wyze_scale_conflicts c
		JOIN athlete_notes m ON m.id = c.manual_note_id
		JOIN athlete_notes w ON w.id = c.wyze_note_id
		ORDER BY c.created_at DESC
		LIMIT ?`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("storage.ListWyzeScaleConflicts: %w", err)
	}
	defer rows.Close()

	var out []WyzeScaleConflictDetail
	for rows.Next() {
		var detail WyzeScaleConflictDetail
		var manualType, wyzeType string
		if err := rows.Scan(
			&detail.ID, &detail.WyzeRecordID, &detail.ManualNoteID, &detail.WyzeNoteID, &detail.ConflictType, &detail.CreatedAt,
			&detail.ManualNote.ID, &detail.ManualNote.Timestamp, &manualType, &detail.ManualNote.RPE, &detail.ManualNote.WeightKG, &detail.ManualNote.BodyFatPct, &detail.ManualNote.MuscleMassKG, &detail.ManualNote.BodyWaterPct, &detail.ManualNote.BMRKcal, &detail.ManualNote.Note, &detail.ManualNote.WorkoutID, &detail.ManualNote.CreatedAt,
			&detail.WyzeNote.ID, &detail.WyzeNote.Timestamp, &wyzeType, &detail.WyzeNote.RPE, &detail.WyzeNote.WeightKG, &detail.WyzeNote.BodyFatPct, &detail.WyzeNote.MuscleMassKG, &detail.WyzeNote.BodyWaterPct, &detail.WyzeNote.BMRKcal, &detail.WyzeNote.Note, &detail.WyzeNote.WorkoutID, &detail.WyzeNote.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("storage.ListWyzeScaleConflicts: scan: %w", err)
		}
		detail.ManualNote.Type = NoteType(manualType)
		detail.WyzeNote.Type = NoteType(wyzeType)
		out = append(out, detail)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage.ListWyzeScaleConflicts: rows: %w", err)
	}
	return out, nil
}

func GetWyzeScaleConflict(db *sql.DB, id int64) (*WyzeScaleConflictDetail, error) {
	conflicts, err := ListWyzeScaleConflicts(db, 1000)
	if err != nil {
		return nil, err
	}
	for _, c := range conflicts {
		if c.ID == id {
			return &c, nil
		}
	}
	return nil, sql.ErrNoRows
}

func DeleteWyzeScaleConflict(db *sql.DB, id int64) error {
	res, err := db.Exec(`DELETE FROM wyze_scale_conflicts WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("storage.DeleteWyzeScaleConflict: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteWyzeConflictEntry resolves a conflict by deleting either the manual or
// the Wyze entry. Deleting the Wyze entry also removes the associated import row.
func DeleteWyzeConflictEntry(db *sql.DB, conflictID int64, side string) error {
	conflict, err := GetWyzeScaleConflict(db, conflictID)
	if err != nil {
		return fmt.Errorf("storage.DeleteWyzeConflictEntry: get conflict: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("storage.DeleteWyzeConflictEntry: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	switch side {
	case "manual":
		if _, err := tx.Exec(`DELETE FROM wyze_scale_conflicts WHERE id = ?`, conflictID); err != nil {
			return fmt.Errorf("storage.DeleteWyzeConflictEntry: delete conflict: %w", err)
		}
		if _, err := tx.Exec(`DELETE FROM athlete_notes WHERE id = ?`, conflict.ManualNoteID); err != nil {
			return fmt.Errorf("storage.DeleteWyzeConflictEntry: delete manual note: %w", err)
		}
	case "wyze":
		if _, err := tx.Exec(`DELETE FROM wyze_scale_conflicts WHERE id = ?`, conflictID); err != nil {
			return fmt.Errorf("storage.DeleteWyzeConflictEntry: delete conflict: %w", err)
		}
		if _, err := tx.Exec(`DELETE FROM wyze_scale_imports WHERE wyze_record_id = ?`, conflict.WyzeRecordID); err != nil {
			return fmt.Errorf("storage.DeleteWyzeConflictEntry: delete wyze import: %w", err)
		}
		if _, err := tx.Exec(`DELETE FROM athlete_notes WHERE id = ?`, conflict.WyzeNoteID); err != nil {
			return fmt.Errorf("storage.DeleteWyzeConflictEntry: delete wyze note: %w", err)
		}
	default:
		return fmt.Errorf("storage.DeleteWyzeConflictEntry: %w", errors.New("side must be manual or wyze"))
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage.DeleteWyzeConflictEntry: commit: %w", err)
	}
	return nil
}

// DeleteBodyMetricRecord deletes a body-metric row directly from the Wyze records table.
// For manual rows it deletes just the note and any associated conflict rows.
// For Wyze rows it deletes the note, import mapping, and any associated conflict rows.
func DeleteBodyMetricRecord(db *sql.DB, noteID int64, source string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("storage.DeleteBodyMetricRecord: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	switch source {
	case "manual":
		if _, err := tx.Exec(`DELETE FROM wyze_scale_conflicts WHERE manual_note_id = ?`, noteID); err != nil {
			return fmt.Errorf("storage.DeleteBodyMetricRecord: delete manual conflicts: %w", err)
		}
		if _, err := tx.Exec(`DELETE FROM athlete_notes WHERE id = ?`, noteID); err != nil {
			return fmt.Errorf("storage.DeleteBodyMetricRecord: delete manual note: %w", err)
		}
	case "wyze":
		if _, err := tx.Exec(`DELETE FROM wyze_scale_conflicts WHERE wyze_note_id = ?`, noteID); err != nil {
			return fmt.Errorf("storage.DeleteBodyMetricRecord: delete wyze conflicts: %w", err)
		}
		if _, err := tx.Exec(`DELETE FROM wyze_scale_imports WHERE athlete_note_id = ?`, noteID); err != nil {
			return fmt.Errorf("storage.DeleteBodyMetricRecord: delete wyze import: %w", err)
		}
		if _, err := tx.Exec(`DELETE FROM athlete_notes WHERE id = ?`, noteID); err != nil {
			return fmt.Errorf("storage.DeleteBodyMetricRecord: delete wyze note: %w", err)
		}
	default:
		return fmt.Errorf("storage.DeleteBodyMetricRecord: %w", errors.New("source must be manual or wyze"))
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage.DeleteBodyMetricRecord: commit: %w", err)
	}
	return nil
}

// FindClosestManualBodyMetric finds the closest non-Wyze weight note within the
// given window around measuredAt. It is used to flag duplicates as
// conflict_with_manual while still importing the Wyze row.
func FindClosestManualBodyMetric(db *sql.DB, measuredAt time.Time, within time.Duration) (*AthleteNote, error) {
	row := db.QueryRow(`
		SELECT n.id, n.timestamp, n.type, n.rpe, n.weight_kg, n.body_fat_pct, n.muscle_mass_kg, n.body_water_pct, n.bmr_kcal, n.note, n.workout_id, n.created_at
		FROM athlete_notes n
		LEFT JOIN wyze_scale_imports wi ON wi.athlete_note_id = n.id
		WHERE n.type = 'weight'
		  AND wi.id IS NULL
		  AND n.timestamp >= ?
		  AND n.timestamp <= ?
		  AND (
			n.weight_kg IS NOT NULL OR
			n.body_fat_pct IS NOT NULL OR
			n.muscle_mass_kg IS NOT NULL OR
			n.body_water_pct IS NOT NULL OR
			n.bmr_kcal IS NOT NULL
		  )
		ORDER BY ABS(unixepoch(n.timestamp) - unixepoch(?)) ASC
		LIMIT 1`,
		measuredAt.Add(-within),
		measuredAt.Add(within),
		measuredAt,
	)

	var note AthleteNote
	var noteType string
	if err := row.Scan(
		&note.ID, &note.Timestamp, &noteType, &note.RPE, &note.WeightKG, &note.BodyFatPct, &note.MuscleMassKG, &note.BodyWaterPct, &note.BMRKcal, &note.Note, &note.WorkoutID, &note.CreatedAt,
	); err != nil {
		return nil, fmt.Errorf("storage.FindClosestManualBodyMetric: %w", err)
	}
	note.Type = NoteType(noteType)
	return &note, nil
}

package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// BodyMetricRecordDetail is the admin-facing joined view of body-metric rows,
// covering both manual notes and Wyze-imported notes.
type BodyMetricRecordDetail struct {
	Note        AthleteNote
	Source      string
	WyzeRecordID *string
	ConflictID  *int64
	ConflictType *string
	Counterpart *AthleteNote
}

// ListBodyMetricRecords returns body-metric rows with optional Wyze import and
// conflict metadata, ordered by timestamp descending.
func ListBodyMetricRecords(db *sql.DB, from, to time.Time, limit int) ([]BodyMetricRecordDetail, error) {
	query := `
		SELECT
			n.id, n.timestamp, n.type, n.rpe, n.weight_kg, n.body_fat_pct, n.muscle_mass_kg, n.body_water_pct, n.bmr_kcal, n.note, n.workout_id, n.created_at,
			wi.wyze_record_id,
			c.id, c.wyze_record_id, c.conflict_type,
			other.id, other.timestamp, other.type, other.rpe, other.weight_kg, other.body_fat_pct, other.muscle_mass_kg, other.body_water_pct, other.bmr_kcal, other.note, other.workout_id, other.created_at
		FROM athlete_notes n
		LEFT JOIN wyze_scale_imports wi ON wi.athlete_note_id = n.id
		LEFT JOIN wyze_scale_conflicts c ON (c.manual_note_id = n.id OR c.wyze_note_id = n.id)
		LEFT JOIN athlete_notes other ON other.id = CASE
			WHEN c.manual_note_id = n.id THEN c.wyze_note_id
			WHEN c.wyze_note_id = n.id THEN c.manual_note_id
			ELSE NULL
		END
		WHERE n.type = 'weight'
		  AND (n.weight_kg IS NOT NULL OR n.body_fat_pct IS NOT NULL OR n.muscle_mass_kg IS NOT NULL OR n.body_water_pct IS NOT NULL OR n.bmr_kcal IS NOT NULL)`
	args := []any{}
	if !from.IsZero() {
		query += " AND n.timestamp >= ?"
		args = append(args, from)
	}
	if !to.IsZero() {
		query += " AND n.timestamp <= ?"
		args = append(args, to)
	}
	query += " ORDER BY n.timestamp DESC LIMIT ?"
	args = append(args, limit)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("storage.ListBodyMetricRecords: %w", err)
	}
	defer rows.Close()

	var out []BodyMetricRecordDetail
	for rows.Next() {
		var detail BodyMetricRecordDetail
		var noteType string
		var wyzeRecordID sql.NullString
		var conflictID sql.NullInt64
		var conflictWyzeRecordID sql.NullString
		var conflictType sql.NullString
		var otherID sql.NullInt64
		var otherTS sql.NullTime
		var otherType sql.NullString
		var otherRPE sql.NullInt64
		var otherWeight, otherBodyFat, otherMuscle, otherWater, otherBMR sql.NullFloat64
		var otherNote sql.NullString
		var otherWorkoutID sql.NullInt64
		var otherCreatedAt sql.NullTime

		if err := rows.Scan(
			&detail.Note.ID, &detail.Note.Timestamp, &noteType, &detail.Note.RPE, &detail.Note.WeightKG, &detail.Note.BodyFatPct, &detail.Note.MuscleMassKG, &detail.Note.BodyWaterPct, &detail.Note.BMRKcal, &detail.Note.Note, &detail.Note.WorkoutID, &detail.Note.CreatedAt,
			&wyzeRecordID,
			&conflictID, &conflictWyzeRecordID, &conflictType,
			&otherID, &otherTS, &otherType, &otherRPE, &otherWeight, &otherBodyFat, &otherMuscle, &otherWater, &otherBMR, &otherNote, &otherWorkoutID, &otherCreatedAt,
		); err != nil {
			return nil, fmt.Errorf("storage.ListBodyMetricRecords: scan: %w", err)
		}
		detail.Note.Type = NoteType(noteType)
		if wyzeRecordID.Valid {
			v := wyzeRecordID.String
			detail.WyzeRecordID = &v
			detail.Source = "wyze"
		} else {
			detail.Source = "manual"
			if conflictWyzeRecordID.Valid {
				v := conflictWyzeRecordID.String
				detail.WyzeRecordID = &v
			}
		}
		if conflictID.Valid {
			v := conflictID.Int64
			detail.ConflictID = &v
		}
		if conflictType.Valid {
			v := conflictType.String
			detail.ConflictType = &v
		}
		if otherID.Valid {
			other := AthleteNote{
				ID:        otherID.Int64,
				Timestamp: otherTS.Time,
				Type:      NoteType(otherType.String),
				Note:      nil,
				CreatedAt: otherCreatedAt.Time,
			}
			if otherRPE.Valid {
				v := otherRPE.Int64
				other.RPE = &v
			}
			if otherWeight.Valid {
				v := otherWeight.Float64
				other.WeightKG = &v
			}
			if otherBodyFat.Valid {
				v := otherBodyFat.Float64
				other.BodyFatPct = &v
			}
			if otherMuscle.Valid {
				v := otherMuscle.Float64
				other.MuscleMassKG = &v
			}
			if otherWater.Valid {
				v := otherWater.Float64
				other.BodyWaterPct = &v
			}
			if otherBMR.Valid {
				v := otherBMR.Float64
				other.BMRKcal = &v
			}
			if otherNote.Valid {
				v := otherNote.String
				other.Note = &v
			}
			if otherWorkoutID.Valid {
				v := otherWorkoutID.Int64
				other.WorkoutID = &v
			}
			detail.Counterpart = &other
		}
		out = append(out, detail)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage.ListBodyMetricRecords: rows: %w", err)
	}
	applyInferredWyzeDuplicates(out)
	return out, nil
}

func applyInferredWyzeDuplicates(records []BodyMetricRecordDetail) {
	wyzeByDay := map[string][]*BodyMetricRecordDetail{}
	for i := range records {
		rec := &records[i]
		if rec.Source != "wyze" {
			continue
		}
		day := rec.Note.Timestamp.Format("2006-01-02")
		wyzeByDay[day] = append(wyzeByDay[day], rec)
	}
	for i := range records {
		rec := &records[i]
		if rec.Source != "manual" || rec.Counterpart != nil {
			continue
		}
		day := rec.Note.Timestamp.Format("2006-01-02")
		for _, wyze := range wyzeByDay[day] {
			if manualMatchesWyze(rec.Note, wyze.Note) {
				rec.Counterpart = &wyze.Note
				rec.WyzeRecordID = wyze.WyzeRecordID
				if rec.ConflictType == nil {
					v := "duplicate_with_wyze"
					rec.ConflictType = &v
				}
				if wyze.Counterpart == nil {
					copy := rec.Note
					wyze.Counterpart = &copy
				}
				break
			}
		}
	}
}

func manualMatchesWyze(manual, wyze AthleteNote) bool {
	matched := false
	if manual.WeightKG != nil {
		if wyze.WeightKG == nil || *manual.WeightKG != *wyze.WeightKG {
			return false
		}
		matched = true
	}
	if manual.BodyFatPct != nil {
		if wyze.BodyFatPct == nil || *manual.BodyFatPct != *wyze.BodyFatPct {
			return false
		}
		matched = true
	}
	if manual.MuscleMassKG != nil {
		if wyze.MuscleMassKG == nil || *manual.MuscleMassKG != *wyze.MuscleMassKG {
			return false
		}
		matched = true
	}
	if manual.BodyWaterPct != nil {
		if wyze.BodyWaterPct == nil || *manual.BodyWaterPct != *wyze.BodyWaterPct {
			return false
		}
		matched = true
	}
	if manual.BMRKcal != nil {
		if wyze.BMRKcal == nil || *manual.BMRKcal != *wyze.BMRKcal {
			return false
		}
		matched = true
	}
	return matched
}

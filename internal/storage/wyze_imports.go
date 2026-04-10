package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// WyzeScaleImport tracks the mapping between a Wyze scale record and its
// normalized athlete_notes row so historical re-sync can stay idempotent.
type WyzeScaleImport struct {
	ID             int64
	WyzeRecordID   string
	AthleteNoteID  int64
	MeasuredAt     time.Time
	PayloadHash    string
	RawPayloadJSON *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	LastSeenAt     time.Time
}

// GetWyzeScaleImportByRecordID returns the import tracking row for a Wyze record.
func GetWyzeScaleImportByRecordID(db *sql.DB, recordID string) (*WyzeScaleImport, error) {
	var imp WyzeScaleImport
	err := db.QueryRow(`
		SELECT id, wyze_record_id, athlete_note_id, measured_at, payload_hash, raw_payload_json, created_at, updated_at, last_seen_at
		FROM wyze_scale_imports
		WHERE wyze_record_id = ?`,
		recordID,
	).Scan(
		&imp.ID, &imp.WyzeRecordID, &imp.AthleteNoteID, &imp.MeasuredAt,
		&imp.PayloadHash, &imp.RawPayloadJSON, &imp.CreatedAt, &imp.UpdatedAt, &imp.LastSeenAt,
	)
	if err != nil {
		return nil, fmt.Errorf("storage.GetWyzeScaleImportByRecordID: %w", err)
	}
	return &imp, nil
}

// UpsertWyzeScaleImport inserts or updates import tracking for a Wyze scale record.
// The row is keyed by wyze_record_id so repeated period syncs remain idempotent.
func UpsertWyzeScaleImport(db *sql.DB, imp *WyzeScaleImport) (int64, error) {
	_, err := db.Exec(`
		INSERT INTO wyze_scale_imports(wyze_record_id, athlete_note_id, measured_at, payload_hash, raw_payload_json, updated_at, last_seen_at)
		VALUES(?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT(wyze_record_id) DO UPDATE SET
			athlete_note_id=excluded.athlete_note_id,
			measured_at=excluded.measured_at,
			payload_hash=excluded.payload_hash,
			raw_payload_json=excluded.raw_payload_json,
			updated_at=CURRENT_TIMESTAMP,
			last_seen_at=CURRENT_TIMESTAMP`,
		imp.WyzeRecordID, imp.AthleteNoteID, imp.MeasuredAt, imp.PayloadHash, imp.RawPayloadJSON,
	)
	if err != nil {
		return 0, fmt.Errorf("storage.UpsertWyzeScaleImport: %w", err)
	}

	var id int64
	if err := db.QueryRow(`SELECT id FROM wyze_scale_imports WHERE wyze_record_id = ?`, imp.WyzeRecordID).Scan(&id); err != nil {
		return 0, fmt.Errorf("storage.UpsertWyzeScaleImport: select id: %w", err)
	}
	return id, nil
}

// DeleteWyzeScaleImportByNoteID removes the import mapping for a Wyze note.
func DeleteWyzeScaleImportByNoteID(db *sql.DB, noteID int64) error {
	res, err := db.Exec(`DELETE FROM wyze_scale_imports WHERE athlete_note_id = ?`, noteID)
	if err != nil {
		return fmt.Errorf("storage.DeleteWyzeScaleImportByNoteID: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

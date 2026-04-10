package wyze

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"cycling-coach/internal/storage"
)

const manualConflictWindow = 2 * time.Hour

type ImportResult struct {
	Inserted           int
	Updated            int
	Skipped            int
	ConflictWithManual int
	Errors             []error
}

type Importer struct {
	db      *sql.DB
	sidecar Sidecar
}

func NewImporter(db *sql.DB, sidecar Sidecar) *Importer {
	return &Importer{db: db, sidecar: sidecar}
}

func (i *Importer) Import(ctx context.Context, from, to time.Time) (*ImportResult, error) {
	records, err := i.sidecar.QueryScaleRecords(ctx, from, to)
	if err != nil {
		return nil, fmt.Errorf("wyze.Importer.Import: query sidecar: %w", err)
	}

	result := &ImportResult{}
	for _, rec := range records {
		if err := i.importRecord(rec, result); err != nil {
			result.Errors = append(result.Errors, err)
		}
	}
	return result, nil
}

func (i *Importer) importRecord(rec ScaleRecord, result *ImportResult) error {
	payloadJSON, payloadHash, err := normalizePayload(rec)
	if err != nil {
		return fmt.Errorf("wyze.Importer.importRecord %s: normalize payload: %w", rec.ExternalID, err)
	}

	existing, err := storage.GetWyzeScaleImportByRecordID(i.db, rec.ExternalID)
	if err == nil {
		if existing.PayloadHash == payloadHash {
			result.Skipped++
			return nil
		}
		if err := storage.UpdateNote(i.db, toAthleteNote(existing.AthleteNoteID, rec)); err != nil {
			return fmt.Errorf("wyze.Importer.importRecord %s: update note: %w", rec.ExternalID, err)
		}
		if _, err := storage.UpsertWyzeScaleImport(i.db, &storage.WyzeScaleImport{
			WyzeRecordID:   rec.ExternalID,
			AthleteNoteID:  existing.AthleteNoteID,
			MeasuredAt:     rec.MeasuredAt,
			PayloadHash:    payloadHash,
			RawPayloadJSON: &payloadJSON,
		}); err != nil {
			return fmt.Errorf("wyze.Importer.importRecord %s: update import: %w", rec.ExternalID, err)
		}
		result.Updated++
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("wyze.Importer.importRecord %s: get import: %w", rec.ExternalID, err)
	}

	noteID, err := storage.InsertNote(i.db, toAthleteNote(0, rec))
	if err != nil {
		return fmt.Errorf("wyze.Importer.importRecord %s: insert note: %w", rec.ExternalID, err)
	}
	if _, err := storage.UpsertWyzeScaleImport(i.db, &storage.WyzeScaleImport{
		WyzeRecordID:   rec.ExternalID,
		AthleteNoteID:  noteID,
		MeasuredAt:     rec.MeasuredAt,
		PayloadHash:    payloadHash,
		RawPayloadJSON: &payloadJSON,
	}); err != nil {
		return fmt.Errorf("wyze.Importer.importRecord %s: insert import: %w", rec.ExternalID, err)
	}

	result.Inserted++

	manual, err := storage.FindClosestManualBodyMetric(i.db, rec.MeasuredAt, manualConflictWindow)
	if err == nil {
		if _, err := storage.InsertWyzeScaleConflict(i.db, &storage.WyzeScaleConflict{
			WyzeRecordID: rec.ExternalID,
			ManualNoteID: manual.ID,
			WyzeNoteID:   noteID,
			ConflictType: storage.WyzeConflictTypeManual,
		}); err != nil {
			return fmt.Errorf("wyze.Importer.importRecord %s: insert conflict: %w", rec.ExternalID, err)
		}
		result.ConflictWithManual++
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("wyze.Importer.importRecord %s: find manual conflict: %w", rec.ExternalID, err)
	}
	return nil
}

func toAthleteNote(id int64, rec ScaleRecord) *storage.AthleteNote {
	return &storage.AthleteNote{
		ID:           id,
		Timestamp:    rec.MeasuredAt,
		Type:         storage.NoteTypeWeight,
		WeightKG:     rec.WeightKG,
		BodyFatPct:   rec.BodyFatPct,
		MuscleMassKG: rec.MuscleMassKG,
		BodyWaterPct: rec.BodyWaterPct,
		BMRKcal:      rec.BMRKcal,
	}
}

func normalizePayload(rec ScaleRecord) (string, string, error) {
	type payload struct {
		ExternalID   string   `json:"external_id"`
		MeasuredAt   string   `json:"measured_at"`
		DeviceID     *string  `json:"device_id,omitempty"`
		WeightKG     *float64 `json:"weight_kg,omitempty"`
		BodyFatPct   *float64 `json:"body_fat_pct,omitempty"`
		MuscleMassKG *float64 `json:"muscle_mass_kg,omitempty"`
		BodyWaterPct *float64 `json:"body_water_pct,omitempty"`
		BMRKcal      *float64 `json:"bmr_kcal,omitempty"`
		RawSource    string   `json:"raw_source"`
	}

	body, err := json.Marshal(payload{
		ExternalID:   rec.ExternalID,
		MeasuredAt:   rec.MeasuredAt.Format(time.RFC3339),
		DeviceID:     rec.DeviceID,
		WeightKG:     rec.WeightKG,
		BodyFatPct:   rec.BodyFatPct,
		MuscleMassKG: rec.MuscleMassKG,
		BodyWaterPct: rec.BodyWaterPct,
		BMRKcal:      rec.BMRKcal,
		RawSource:    rec.RawSource,
	})
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256(body)
	return string(body), hex.EncodeToString(sum[:]), nil
}

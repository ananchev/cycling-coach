package storage

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"
)

func TestBuildProgressSnapshot(t *testing.T) {
	db := openTestDB(t)

	insertProgressWorkout(t, db, progressWorkoutSeed{
		WahooID:    "pp-1",
		StartedAt:  time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC),
		DurationSec: 60 * 60,
		Calories:   500,
		TSS:        60,
		TRIMP:      70,
		IF:         0.78,
		EF:         1.30,
	})
	insertProgressWorkout(t, db, progressWorkoutSeed{
		WahooID:    "pp-2",
		StartedAt:  time.Date(2026, 4, 3, 9, 0, 0, 0, time.UTC),
		DurationSec: 100 * 60,
		Calories:   700,
		TSS:        85,
		TRIMP:      90,
		IF:         0.82,
		EF:         1.38,
		Decoupling: 6.4,
	})
	insertProgressWorkout(t, db, progressWorkoutSeed{
		WahooID:    "pp-3",
		StartedAt:  time.Date(2026, 4, 5, 9, 0, 0, 0, time.UTC),
		DurationSec: 70 * 60,
		Calories:   450,
		TSS:        55,
		TRIMP:      65,
		IF:         0.76,
		EF:         1.32,
	})

	insertProgressWorkout(t, db, progressWorkoutSeed{
		WahooID:    "sp-1",
		StartedAt:  time.Date(2026, 4, 8, 9, 0, 0, 0, time.UTC),
		DurationSec: 65 * 60,
		Calories:   600,
		TSS:        80,
		TRIMP:      82,
		IF:         0.79,
		EF:         1.40,
	})
	insertProgressWorkout(t, db, progressWorkoutSeed{
		WahooID:    "sp-2",
		StartedAt:  time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC),
		DurationSec: 110 * 60,
		Calories:   750,
		TSS:        95,
		TRIMP:      100,
		IF:         0.77,
		EF:         1.44,
		Decoupling: 4.2,
	})
	insertProgressWorkout(t, db, progressWorkoutSeed{
		WahooID:    "sp-3",
		StartedAt:  time.Date(2026, 4, 11, 9, 0, 0, 0, time.UTC),
		DurationSec: 75 * 60,
		Calories:   520,
		TSS:        70,
		TRIMP:      78,
		IF:         0.74,
		EF:         1.43,
	})
	insertProgressWorkout(t, db, progressWorkoutSeed{
		WahooID:    "sp-4",
		StartedAt:  time.Date(2026, 4, 14, 9, 0, 0, 0, time.UTC),
		DurationSec: 95 * 60,
		Calories:   580,
		TSS:        72,
		TRIMP:      85,
		IF:         0.81,
		EF:         1.52,
		Decoupling: 4.8,
	})

	insertManualWorkout(t, db, "manual-empty-2026-04-02", time.Date(2026, 4, 2, 23, 50, 0, 0, time.UTC))
	insertManualWorkout(t, db, "manual-empty-2026-04-09", time.Date(2026, 4, 9, 23, 50, 0, 0, time.UTC))

	insertWeightNote(t, db, time.Date(2026, 4, 2, 7, 0, 0, 0, time.UTC), 75.0)
	insertWeightNote(t, db, time.Date(2026, 4, 5, 7, 0, 0, 0, time.UTC), 74.8)
	insertWeightNote(t, db, time.Date(2026, 4, 6, 7, 0, 0, 0, time.UTC), 74.9)
	insertWeightNote(t, db, time.Date(2026, 4, 9, 7, 0, 0, 0, time.UTC), 74.4)
	insertWeightNote(t, db, time.Date(2026, 4, 13, 7, 0, 0, 0, time.UTC), 74.2)
	insertWeightNote(t, db, time.Date(2026, 4, 14, 7, 0, 0, 0, time.UTC), 74.3)

	from := time.Date(2026, 4, 8, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 4, 14, 0, 0, 0, 0, time.UTC)
	got, err := BuildProgressSnapshot(db, from, to, true)
	if err != nil {
		t.Fatalf("BuildProgressSnapshot: %v", err)
	}

	if got.SelectedRange.Days != 7 {
		t.Fatalf("SelectedRange.Days = %d, want 7", got.SelectedRange.Days)
	}
	if got.PriorRange.From.Format("2006-01-02") != "2026-04-01" || got.PriorRange.To.Format("2006-01-02") != "2026-04-07" {
		t.Fatalf("prior range = %s..%s, want 2026-04-01..2026-04-07", got.PriorRange.From.Format("2006-01-02"), got.PriorRange.To.Format("2006-01-02"))
	}

	if got.AerobicEfficiency.Current == nil || *got.AerobicEfficiency.Current <= 1.42 {
		t.Errorf("AerobicEfficiency.Current = %v, want > 1.42", got.AerobicEfficiency.Current)
	}
	if got.AerobicEfficiency.Prior == nil || *got.AerobicEfficiency.Prior >= 1.32 {
		t.Errorf("AerobicEfficiency.Prior = %v, want < 1.32", got.AerobicEfficiency.Prior)
	}
	if got.AerobicEfficiency.Trend != ProgressTrendUp {
		t.Errorf("AerobicEfficiency.Trend = %q, want up", got.AerobicEfficiency.Trend)
	}

	if got.EnduranceDurability.Trend != ProgressTrendDown {
		t.Errorf("EnduranceDurability.Trend = %q, want down", got.EnduranceDurability.Trend)
	}
	if got.ActiveCalories.Current == nil || *got.ActiveCalories.Current != 2450 {
		t.Errorf("ActiveCalories.Current = %v, want 2450", got.ActiveCalories.Current)
	}
	if got.ActiveCalories.Trend != ProgressTrendUp {
		t.Errorf("ActiveCalories.Trend = %q, want up", got.ActiveCalories.Trend)
	}
	if got.CumulativeTSS.Current == nil || *got.CumulativeTSS.Current != 317 {
		t.Errorf("CumulativeTSS.Current = %v, want 317", got.CumulativeTSS.Current)
	}
	if got.CumulativeTSS.Trend != ProgressTrendUp {
		t.Errorf("CumulativeTSS.Trend = %q, want up", got.CumulativeTSS.Trend)
	}
	if got.AverageIF.Trend != ProgressTrendSteady {
		t.Errorf("AverageIF.Trend = %q, want steady", got.AverageIF.Trend)
	}
	if got.CompletionRate.Current == nil || *got.CompletionRate.Current < 57 || *got.CompletionRate.Current > 58 {
		t.Errorf("CompletionRate.Current = %v, want ~57.14", got.CompletionRate.Current)
	}
	if got.CompletionRate.Trend != ProgressTrendUp {
		t.Errorf("CompletionRate.Trend = %q, want up", got.CompletionRate.Trend)
	}
	if got.AverageWeightKG.Current == nil || *got.AverageWeightKG.Current < 74.29 || *got.AverageWeightKG.Current > 74.31 {
		t.Errorf("AverageWeightKG.Current = %v, want ~74.3", got.AverageWeightKG.Current)
	}
	if got.AverageWeightKG.Trend != ProgressTrendSteady {
		t.Errorf("AverageWeightKG.Trend = %q, want steady", got.AverageWeightKG.Trend)
	}
	if len(got.WeeklyLoad) != 2 {
		t.Fatalf("len(WeeklyLoad) = %d, want 2", len(got.WeeklyLoad))
	}
	if got.WeeklyLoad[0].WeekStart.Format("2006-01-02") != "2026-04-06" {
		t.Errorf("WeeklyLoad[0].WeekStart = %s, want 2026-04-06", got.WeeklyLoad[0].WeekStart.Format("2006-01-02"))
	}
}

func TestBuildProgressSnapshot_InclusiveDatesAndZeroPrior(t *testing.T) {
	db := openTestDB(t)
	insertProgressWorkout(t, db, progressWorkoutSeed{
		WahooID:    "only-current",
		StartedAt:  time.Date(2026, 4, 14, 18, 0, 0, 0, time.UTC),
		DurationSec: 60 * 60,
		Calories:   400,
		TSS:        50,
		TRIMP:      60,
		IF:         0.75,
		EF:         1.41,
	})

	from := time.Date(2026, 4, 14, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 4, 14, 0, 0, 0, 0, time.UTC)
	got, err := BuildProgressSnapshot(db, from, to, false)
	if err != nil {
		t.Fatalf("BuildProgressSnapshot: %v", err)
	}
	if got.CumulativeTSS.Trend != ProgressTrendUp {
		t.Errorf("CumulativeTSS.Trend = %q, want up when prior is zero", got.CumulativeTSS.Trend)
	}
	if got.ActiveCalories.Trend != ProgressTrendUp {
		t.Errorf("ActiveCalories.Trend = %q, want up when prior is zero", got.ActiveCalories.Trend)
	}
	if got.CompletionRate.Current == nil || *got.CompletionRate.Current != 100 {
		t.Errorf("CompletionRate.Current = %v, want 100", got.CompletionRate.Current)
	}
}

func TestBuildProgressSnapshot_WeightRequiresThreePointsInBothPeriods(t *testing.T) {
	db := openTestDB(t)
	insertProgressWorkout(t, db, progressWorkoutSeed{
		WahooID:     "w-1",
		StartedAt:   time.Date(2026, 4, 8, 9, 0, 0, 0, time.UTC),
		DurationSec: 60 * 60,
		Calories:    300,
		TSS:         40,
		TRIMP:       50,
		IF:          0.70,
		EF:          1.30,
	})
	insertWeightNote(t, db, time.Date(2026, 4, 8, 7, 0, 0, 0, time.UTC), 74.5)
	insertWeightNote(t, db, time.Date(2026, 4, 9, 7, 0, 0, 0, time.UTC), 74.4)
	insertWeightNote(t, db, time.Date(2026, 4, 10, 7, 0, 0, 0, time.UTC), 74.3)
	insertWeightNote(t, db, time.Date(2026, 4, 2, 7, 0, 0, 0, time.UTC), 75.0)
	insertWeightNote(t, db, time.Date(2026, 4, 5, 7, 0, 0, 0, time.UTC), 74.8)

	got, err := BuildProgressSnapshot(db, time.Date(2026, 4, 8, 0, 0, 0, 0, time.UTC), time.Date(2026, 4, 14, 0, 0, 0, 0, time.UTC), false)
	if err != nil {
		t.Fatalf("BuildProgressSnapshot: %v", err)
	}
	if got.AverageWeightKG.Current != nil || got.AverageWeightKG.Prior != nil {
		t.Fatalf("AverageWeightKG should be hidden when one side has fewer than 3 points: %+v", got.AverageWeightKG)
	}
}

func TestUpsertProgressAnalysis_ReplacesSingleRow(t *testing.T) {
	db := openTestDB(t)

	first := &ProgressAnalysis{
		PeriodFrom:   time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		PeriodTo:     time.Date(2026, 4, 4, 23, 59, 59, 0, time.UTC),
		SnapshotJSON: `{"period":"first"}`,
		SystemPrompt: "first system prompt",
		UserPrompt:   "first user prompt",
		Narrative:    "first interpretation",
	}
	if err := UpsertProgressAnalysis(db, first); err != nil {
		t.Fatalf("UpsertProgressAnalysis(first): %v", err)
	}

	second := &ProgressAnalysis{
		PeriodFrom:   time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC),
		PeriodTo:     time.Date(2026, 4, 4, 23, 59, 59, 0, time.UTC),
		SnapshotJSON: `{"period":"second"}`,
		SystemPrompt: "second system prompt",
		UserPrompt:   "second user prompt",
		Narrative:    "second interpretation",
	}
	if err := UpsertProgressAnalysis(db, second); err != nil {
		t.Fatalf("UpsertProgressAnalysis(second): %v", err)
	}

	got, err := GetProgressAnalysis(db)
	if err != nil {
		t.Fatalf("GetProgressAnalysis: %v", err)
	}
	if got.ID != 1 {
		t.Errorf("ID = %d, want 1", got.ID)
	}
	if got.Narrative != "second interpretation" {
		t.Errorf("Narrative = %q, want second interpretation", got.Narrative)
	}
	if got.SystemPrompt != "second system prompt" {
		t.Errorf("SystemPrompt = %q, want second system prompt", got.SystemPrompt)
	}
	if got.UserPrompt != "second user prompt" {
		t.Errorf("UserPrompt = %q, want second user prompt", got.UserPrompt)
	}
	if got.PeriodFrom.Format("2006-01-02") != "2026-03-15" {
		t.Errorf("PeriodFrom = %s, want 2026-03-15", got.PeriodFrom.Format("2006-01-02"))
	}

	var payload map[string]string
	if err := json.Unmarshal([]byte(got.SnapshotJSON), &payload); err != nil {
		t.Fatalf("snapshot json: %v", err)
	}
	if payload["period"] != "second" {
		t.Errorf("snapshot payload = %v, want second", payload)
	}
}

type progressWorkoutSeed struct {
	WahooID     string
	StartedAt   time.Time
	DurationSec int64
	Calories    int64
	TSS         float64
	TRIMP       float64
	IF          float64
	EF          float64
	Decoupling  float64
}

func insertProgressWorkout(t *testing.T, db *sql.DB, seed progressWorkoutSeed) int64 {
	t.Helper()
	id, _, err := UpsertWorkout(db, &Workout{
		WahooID:     seed.WahooID,
		StartedAt:   seed.StartedAt,
		DurationSec: &seed.DurationSec,
		Calories:    &seed.Calories,
		Source:      "api",
	})
	if err != nil {
		t.Fatalf("UpsertWorkout(%s): %v", seed.WahooID, err)
	}
	if err := UpsertRideMetrics(db, &RideMetrics{
		WorkoutID:        id,
		DurationMin:      floatPtr(float64(seed.DurationSec) / 60),
		TSS:              floatPtr(seed.TSS),
		TRIMP:            floatPtr(seed.TRIMP),
		IntensityFactor:  floatPtr(seed.IF),
		EfficiencyFactor: floatPtr(seed.EF),
		DecouplingPct:    maybeFloatPtr(seed.Decoupling),
	}); err != nil {
		t.Fatalf("UpsertRideMetrics(%s): %v", seed.WahooID, err)
	}
	return id
}

func insertManualWorkout(t *testing.T, db *sql.DB, wahooID string, startedAt time.Time) {
	t.Helper()
	id, _, err := UpsertWorkout(db, &Workout{
		WahooID:   wahooID,
		StartedAt: startedAt,
		Source:    "manual",
		Processed: true,
	})
	if err != nil {
		t.Fatalf("insertManualWorkout: %v", err)
	}
	if id == 0 {
		t.Fatal("insertManualWorkout returned id=0")
	}
}

func insertWeightNote(t *testing.T, db *sql.DB, ts time.Time, weight float64) {
	t.Helper()
	if _, err := InsertNote(db, &AthleteNote{
		Timestamp: ts,
		Type:      NoteTypeWeight,
		WeightKG:  floatPtr(weight),
	}); err != nil {
		t.Fatalf("InsertNote(weight): %v", err)
	}
}

func maybeFloatPtr(v float64) *float64 {
	if v == 0 {
		return nil
	}
	return floatPtr(v)
}

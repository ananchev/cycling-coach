package reporting

import (
	"context"
	"time"

	"cycling-coach/internal/storage"
)

// Provider generates coaching content from structured report input.
// Implementations include ClaudeProvider (production) and StubProvider (tests).
type Provider interface {
	Generate(ctx context.Context, input *ReportInput) (*ReportOutput, error)
}

// ReportInput contains all data needed to generate a weekly report or plan.
type ReportInput struct {
	Type           storage.ReportType
	WeekStart      time.Time
	WeekEnd        time.Time
	AthleteProfile string        // raw markdown content of athlete-profile.md
	Rides          []RideSummary // one entry per workout in the window
	Notes          []NoteSummary // athlete notes logged during the window
	// PriorPlanNarrative is the narrative from the weekly_plan that covered this window.
	// Populated when generating a weekly_report so Claude can compare plan vs. reality.
	// Empty when generating a weekly_plan (no prior context needed).
	PriorPlanNarrative string
	// PrecedingReports holds the most recent weekly_report narratives (oldest first)
	// from the periods leading up to a plan window. Populated when generating a
	// weekly_plan so Claude can extend the just-finished period's recommendations
	// rather than restart progression. The last entry is the period that just ended.
	PrecedingReports []PrecedingReport
	// UserPrompt is optional free-text from the athlete providing constraints or
	// clarifications for the upcoming plan (e.g. "travelling Tuesday, only 30 min Wed").
	// Used when generating a weekly_plan.
	UserPrompt string
}

// PrecedingReport is a digest of a previously generated weekly_report carried
// forward into plan generation for continuity.
type PrecedingReport struct {
	WeekStart time.Time
	WeekEnd   time.Time
	Narrative string
}

// RideSummary is a per-ride digest assembled for the report prompt.
// Metric fields are nil when ride_metrics have not yet been computed (Phase 6).
type RideSummary struct {
	Date            time.Time
	WorkoutType     string
	DurationMin     *float64
	AvgPower        *float64
	AvgHR           *float64
	AvgCadence      *float64
	NormalizedPower *float64
	IntensityFactor *float64
	HRDriftPct      *float64
	TSS             *float64
	// Zone distributions (%)
	HRZ1Pct, HRZ2Pct, HRZ3Pct, HRZ4Pct, HRZ5Pct          *float64
	PwrZ1Pct, PwrZ2Pct, PwrZ3Pct, PwrZ4Pct, PwrZ5Pct     *float64
	CadLT70Pct, Cad70To85Pct, Cad85To100Pct, CadGE100Pct *float64
	// Power zone timeline — JSON array of {zone, start_min, duration_min, avg_power}
	ZoneTimeline *string
	// HR zone timeline — JSON array of {zone, start_min, duration_min, avg_hr}
	HRZoneTimeline *string
}

// NoteSummary is a per-note digest assembled for the report prompt.
type NoteSummary struct {
	Timestamp    time.Time
	Type         storage.NoteType
	RPE          *int64
	WeightKG     *float64
	BodyFatPct   *float64
	MuscleMassKG *float64
	BodyWaterPct *float64
	BMRKcal      *float64
	Text         *string
}

// ReportOutput is the generated content returned by a Provider.
type ReportOutput struct {
	// Summary is a compact 5-line text for Telegram delivery (Phase 5).
	Summary string
	// Narrative is the full coaching text (markdown) for HTML rendering.
	Narrative string
}

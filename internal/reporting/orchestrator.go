package reporting

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"cycling-coach/internal/storage"
)

// Orchestrator coordinates report generation: assembles input, calls the provider,
// renders HTML, and persists the result.
type Orchestrator struct {
	db          *sql.DB
	profilePath string
	provider    Provider
}

// NewOrchestrator creates an Orchestrator with the given dependencies.
func NewOrchestrator(db *sql.DB, profilePath string, provider Provider) *Orchestrator {
	return &Orchestrator{
		db:          db,
		profilePath: profilePath,
		provider:    provider,
	}
}

// EvolveProfile regenerates the athlete profile using the last lastN weekly reports.
// The Orchestrator's provider must be a *ClaudeProvider; an error is returned
// when using a stub provider (e.g. in tests).
func (o *Orchestrator) EvolveProfile(ctx context.Context, lastN int) (*EvolveProfileResult, error) {
	cp, ok := o.provider.(*ClaudeProvider)
	if !ok {
		return nil, fmt.Errorf("reporting.Orchestrator.EvolveProfile: provider does not support raw calls (not a *ClaudeProvider)")
	}
	return EvolveProfile(ctx, o.db, o.profilePath, cp, lastN)
}

// Generate runs the full report pipeline for the given type and week window.
// userPrompt is optional free-text from the athlete (constraints for the plan).
// Returns the ID of the persisted report row.
func (o *Orchestrator) Generate(ctx context.Context, reportType storage.ReportType, weekStart, weekEnd time.Time, userPrompt string) (int64, error) {
	slog.Info("reporting: generating", "type", string(reportType), "week_start", weekStart.Format("2006-01-02"))

	input, err := AssembleInput(ctx, o.db, o.profilePath, reportType, weekStart, weekEnd)
	if err != nil {
		return 0, fmt.Errorf("reporting.Orchestrator.Generate: assemble: %w", err)
	}
	input.UserPrompt = userPrompt

	slog.Info("reporting: assembled input", "rides", len(input.Rides), "notes", len(input.Notes))

	output, err := o.provider.Generate(ctx, input)
	if err != nil {
		return 0, fmt.Errorf("reporting.Orchestrator.Generate: provider: %w", err)
	}

	html, err := RenderHTML(reportType, weekStart, weekEnd, output)
	if err != nil {
		return 0, fmt.Errorf("reporting.Orchestrator.Generate: render: %w", err)
	}

	report := &storage.Report{
		Type:          reportType,
		WeekStart:     weekStart,
		WeekEnd:       weekEnd,
		SummaryText:   &output.Summary,
		NarrativeText: &output.Narrative,
		FullHTML:      &html,
	}

	id, err := storage.UpsertReport(o.db, report)
	if err != nil {
		return 0, fmt.Errorf("reporting.Orchestrator.Generate: upsert report: %w", err)
	}

	slog.Info("reporting: report persisted", "id", id, "type", string(reportType))
	return id, nil
}

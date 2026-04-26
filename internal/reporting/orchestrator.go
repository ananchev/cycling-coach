package reporting

import (
	"context"
	"database/sql"
	"errors"
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

// CloseBlockResult is the combined output of closing a completed block and generating the next plan.
type CloseBlockResult struct {
	ReportID        int64
	PlanID          int64
	BlockStart      time.Time
	BlockEnd        time.Time
	PlanStart       time.Time
	PlanEnd         time.Time
	ProfilePatched  bool
	PatchBackupPath string
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
// precedingReports is optional context — when non-empty (typically only on plan
// generation from the close-block flow) it is attached to the prompt so Claude
// can extend recent recommendations rather than restart progression. Pass nil
// when no preceding context applies.
// Returns the ID of the persisted report row.
func (o *Orchestrator) Generate(ctx context.Context, reportType storage.ReportType, weekStart, weekEnd time.Time, userPrompt string, precedingReports []PrecedingReport) (int64, error) {
	slog.Info("reporting: generating", "type", string(reportType), "week_start", weekStart.Format("2006-01-02"))

	input, err := AssembleInput(ctx, o.db, o.profilePath, reportType, weekStart, weekEnd)
	if err != nil {
		return 0, fmt.Errorf("reporting.Orchestrator.Generate: assemble: %w", err)
	}
	input.UserPrompt = userPrompt
	input.PrecedingReports = precedingReports
	systemPrompt := input.AthleteProfile
	userPromptText := BuildPrompt(input)

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
		SystemPrompt:  systemPrompt,
		UserPrompt:    userPromptText,
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

// GenerateCloseBlock closes the current training block and immediately creates the next 7-day plan.
// The closed block starts on the day after the latest weekly report's end date.
// On first use, when no prior weekly report exists, initialBlockStart must be provided.
func (o *Orchestrator) GenerateCloseBlock(ctx context.Context, blockEnd time.Time, initialBlockStart *time.Time, userPrompt string) (*CloseBlockResult, error) {
	lastClosed, err := storage.GetLatestReport(o.db, storage.ReportTypeWeeklyReport)
	var blockStart time.Time
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if initialBlockStart == nil || initialBlockStart.IsZero() {
				return nil, fmt.Errorf("reporting.Orchestrator.GenerateCloseBlock: no prior weekly report exists; provide an initial block start for the first close-block run")
			}
			blockStart = dateOnly(*initialBlockStart)
		} else {
			return nil, fmt.Errorf("reporting.Orchestrator.GenerateCloseBlock: latest weekly report: %w", err)
		}
	} else {
		blockStart = dateOnly(lastClosed.WeekEnd).AddDate(0, 0, 1)
	}

	blockEnd = dateOnly(blockEnd)
	if blockEnd.Before(blockStart) {
		return nil, fmt.Errorf("reporting.Orchestrator.GenerateCloseBlock: block end %s is before inferred block start %s", blockEnd.Format("2006-01-02"), blockStart.Format("2006-01-02"))
	}

	// Assemble input first — we need the rides/metrics for both the report and the patch.
	reportInput, err := AssembleInput(ctx, o.db, o.profilePath, storage.ReportTypeWeeklyReport, blockStart, blockEnd)
	if err != nil {
		return nil, fmt.Errorf("reporting.Orchestrator.GenerateCloseBlock: assemble: %w", err)
	}

	reportID, err := o.Generate(ctx, storage.ReportTypeWeeklyReport, blockStart, blockEnd, "", nil)
	if err != nil {
		return nil, fmt.Errorf("reporting.Orchestrator.GenerateCloseBlock: weekly report: %w", err)
	}

	// Attempt weekly profile patch between report and plan generation.
	// The patch updates rolling context so the plan benefits from the freshest profile.
	// Patch failure is non-fatal — log and continue with plan generation.
	var profilePatched bool
	var patchBackupPath string
	if cp, ok := o.provider.(*ClaudeProvider); ok {
		report, getErr := storage.GetReportByID(o.db, reportID)
		if getErr != nil {
			slog.Warn("reporting: could not fetch report for profile patch", "err", getErr)
		} else {
			narrative := ""
			if report.NarrativeText != nil {
				narrative = *report.NarrativeText
			}
			weekMetrics := ComputeWeekMetrics(reportInput)
			patchResult, patchErr := PatchProfile(ctx, o.profilePath, cp, narrative, weekMetrics)
			if patchErr != nil {
				slog.Warn("reporting: profile patch failed (non-fatal, continuing with plan)", "err", patchErr)
			} else {
				profilePatched = true
				patchBackupPath = patchResult.BackupPath
				slog.Info("reporting: profile patched after block close", "backup", patchResult.BackupPath)
			}
		}
	}

	planStart := blockEnd.AddDate(0, 0, 1)
	planEnd := planStart.AddDate(0, 0, 6)

	precedingReports, err := o.LoadPrecedingReports(3)
	if err != nil {
		// Non-fatal — proceed without continuity context.
		slog.Warn("reporting: could not load preceding reports for plan continuity", "err", err)
	}

	planID, err := o.Generate(ctx, storage.ReportTypeWeeklyPlan, planStart, planEnd, userPrompt, precedingReports)
	if err != nil {
		return nil, fmt.Errorf("reporting.Orchestrator.GenerateCloseBlock: weekly plan: %w", err)
	}

	return &CloseBlockResult{
		ReportID:        reportID,
		PlanID:          planID,
		BlockStart:      blockStart,
		BlockEnd:        blockEnd,
		PlanStart:       planStart,
		PlanEnd:         planEnd,
		ProfilePatched:  profilePatched,
		PatchBackupPath: patchBackupPath,
	}, nil
}

func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// LoadPrecedingReports returns up to n most recent weekly_report narratives,
// ordered chronologically (oldest first). Reports without a narrative are skipped.
// Used to give plan generation continuity with recently generated reports.
func (o *Orchestrator) LoadPrecedingReports(n int) ([]PrecedingReport, error) {
	rows, err := storage.ListReports(o.db, storage.ReportTypeWeeklyReport, n)
	if err != nil {
		return nil, fmt.Errorf("reporting.Orchestrator.LoadPrecedingReports: %w", err)
	}
	out := make([]PrecedingReport, 0, len(rows))
	for i := len(rows) - 1; i >= 0; i-- {
		r := rows[i]
		if r.NarrativeText == nil || *r.NarrativeText == "" {
			continue
		}
		out = append(out, PrecedingReport{
			WeekStart: r.WeekStart,
			WeekEnd:   r.WeekEnd,
			Narrative: *r.NarrativeText,
		})
	}
	return out, nil
}

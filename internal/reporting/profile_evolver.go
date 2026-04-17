package reporting

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cycling-coach/internal/storage"
)

// EvolveProfileResult is returned by EvolveProfile.
type EvolveProfileResult struct {
	// BackupPath is the full path of the old profile that was preserved.
	BackupPath string
}

// EvolveValidationError is returned when Claude's output fails validation.
// The generated content is saved to RejectedPath so the user can inspect,
// manually fix, and copy it over the live profile if they choose to.
type EvolveValidationError struct {
	Reason       string // human-readable description of what failed
	RejectedPath string // path where the rejected content was saved
}

func (e *EvolveValidationError) Error() string {
	return fmt.Sprintf("validation failed: %s — rejected output saved to %s", e.Reason, e.RejectedPath)
}

// EvolveProfile fetches the last lastN weekly_report summaries, sends them to
// Claude together with the current athlete profile, and asks Claude to produce
// an updated profile. The existing profile is backed up with a timestamp suffix
// before the new content is written.
func EvolveProfile(ctx context.Context, db *sql.DB, profilePath string, provider *ClaudeProvider, lastN int) (*EvolveProfileResult, error) {
	// 1. Read the current profile.
	currentProfile, err := os.ReadFile(profilePath)
	if err != nil {
		return nil, fmt.Errorf("reporting.EvolveProfile: read profile: %w", err)
	}

	// 2. Fetch the last N weekly reports (most recent first from DB, then reverse).
	reports, err := storage.ListReports(db, storage.ReportTypeWeeklyReport, lastN)
	if err != nil {
		return nil, fmt.Errorf("reporting.EvolveProfile: list reports: %w", err)
	}
	if len(reports) == 0 {
		return nil, fmt.Errorf("reporting.EvolveProfile: no weekly reports found — generate at least one report before evolving the profile")
	}

	// 3. Build the prompt (reports in chronological order, oldest first).
	var reportsSection strings.Builder
	for i := len(reports) - 1; i >= 0; i-- {
		r := reports[i]
		fmt.Fprintf(&reportsSection, "\n### Week of %s\n", r.WeekStart.Format("2006-01-02"))
		// Prefer the full narrative for richer context; fall back to summary for old rows.
		switch {
		case r.NarrativeText != nil && *r.NarrativeText != "":
			reportsSection.WriteString(*r.NarrativeText)
		case r.SummaryText != nil && *r.SummaryText != "":
			reportsSection.WriteString(*r.SummaryText)
		default:
			reportsSection.WriteString("(no content available)")
		}
		reportsSection.WriteString("\n")
	}

	prompt := buildProfileEvolutionPrompt(string(currentProfile), reportsSection.String(), len(reports))

	// 4. Call Claude for the updated profile text.
	newProfile, err := provider.CallRaw(ctx, "", prompt)
	if err != nil {
		return nil, fmt.Errorf("reporting.EvolveProfile: call Claude: %w", err)
	}

	// 5. Validate the evolved profile before touching the filesystem.
	// On failure, save the rejected content so the user can inspect and fix it manually.
	if err := validateEvolvedProfile(newProfile); err != nil {
		rejectedPath := rejectedProfilePath(profilePath)
		// Best-effort save — if this also fails, report the original validation error.
		if writeErr := os.WriteFile(rejectedPath, []byte(newProfile), 0644); writeErr != nil {
			return nil, fmt.Errorf("reporting.EvolveProfile: validation: %w (also failed to save rejected output: %v)", err, writeErr)
		}
		return nil, &EvolveValidationError{
			Reason:       err.Error(),
			RejectedPath: rejectedPath,
		}
	}

	// 6. Back up the existing profile with a timestamp suffix.
	backupPath := profileBackupPath(profilePath)
	if err := os.WriteFile(backupPath, currentProfile, 0644); err != nil {
		return nil, fmt.Errorf("reporting.EvolveProfile: backup profile: %w", err)
	}

	// 7. Write the new profile.
	if err := os.WriteFile(profilePath, []byte(newProfile), 0644); err != nil {
		return nil, fmt.Errorf("reporting.EvolveProfile: write new profile: %w", err)
	}

	return &EvolveProfileResult{BackupPath: backupPath}, nil
}

// rejectedProfilePath returns the path where a failed evolution output is saved.
// e.g. /data/athlete-profile.md → /data/athlete-profile.rejected.md
func rejectedProfilePath(profilePath string) string {
	dir := filepath.Dir(profilePath)
	base := filepath.Base(profilePath)
	ext := filepath.Ext(base)
	name := base[:len(base)-len(ext)]
	return filepath.Join(dir, name+".rejected"+ext)
}

// profileBackupPath produces a timestamped copy path for the given profile file.
// e.g. /data/athlete-profile.md → /data/athlete-profile.2026-04-02T14-30.md
func profileBackupPath(profilePath string) string {
	dir := filepath.Dir(profilePath)
	base := filepath.Base(profilePath)
	ext := filepath.Ext(base)
	name := base[:len(base)-len(ext)]
	stamp := time.Now().Format("2006-01-02T15-04")
	return filepath.Join(dir, fmt.Sprintf("%s.%s%s", name, stamp, ext))
}

// requiredProfileSections lists heading substrings that must appear in any
// evolved profile. These sections are load-bearing for report and plan quality:
// the analysis engine and Claude both depend on them.
var requiredProfileSections = []string{
	"Heart Rate Zones",
	"Power Zones",
	"How to interpret the pre-computed metrics",
	"Your role as coach",
	"Weekly structure template",
	"Warning flags",
	"Recent training weeks",
	"Stelvio readiness milestones",
}

// validateEvolvedProfile returns an error if the evolved profile is suspiciously
// short or is missing any of the sections that must survive every iteration.
func validateEvolvedProfile(profile string) error {
	if len(strings.TrimSpace(profile)) < 500 {
		return fmt.Errorf("evolved profile is too short (%d chars) — likely a truncated or refused response", len(profile))
	}
	for _, section := range requiredProfileSections {
		if !strings.Contains(profile, section) {
			return fmt.Errorf("required section %q is missing from evolved profile", section)
		}
	}
	return nil
}

func buildProfileEvolutionPrompt(currentProfile, reportsSection string, n int) string {
	return fmt.Sprintf(`You are a cycling coach maintaining an athlete profile that serves as the system prompt for generating weekly training reports and plans.

Below is the current athlete profile followed by the last %d weekly training reports (oldest first). Based on the training trends, performance progression, and patterns in these reports, produce an UPDATED version of the athlete profile.

Rules:
- Output ONLY the updated profile markdown — no preamble, no explanation, no code fences
- Preserve the exact markdown structure and ALL section headings of the original
- The following sections MUST appear verbatim in the output (you may update their content but never remove or rename them):
  - "Heart Rate Zones"
  - "Power Zones"
  - "How to interpret the pre-computed metrics"
  - "Your role as coach"
  - "Weekly structure template"
  - "Warning flags"
  - "Recent training weeks"
  - "Stelvio readiness milestones"
- Update numeric values (FTP, HR zones, power zones, weight) ONLY when there is clear evidence of sustained change across multiple reports
- Update "Training history and current phase" to reflect recent progression from the reports
- Update "Last Updated" to today's date
- Keep coaching philosophy and personal goals intact unless the reports clearly indicate a shift

## Current athlete profile

%s

## Last %d weekly training reports (oldest first)
%s`, n, currentProfile, n, reportsSection)
}

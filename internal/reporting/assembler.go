package reporting

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"cycling-coach/internal/storage"
)

// AssembleInput gathers all data needed to generate a report for the given window.
// Ride metrics are optional — if not yet computed the RideSummary fields remain nil.
func AssembleInput(ctx context.Context, db *sql.DB, profilePath string, reportType storage.ReportType, weekStart, weekEnd time.Time) (*ReportInput, error) {
	profile, err := os.ReadFile(profilePath)
	if err != nil {
		return nil, fmt.Errorf("reporting.AssembleInput: read profile: %w", err)
	}

	workouts, err := storage.ListWorkoutsByDateRange(db, weekStart, weekEnd)
	if err != nil {
		return nil, fmt.Errorf("reporting.AssembleInput: list workouts: %w", err)
	}

	rides := make([]RideSummary, 0, len(workouts))
	for _, w := range workouts {
		rs := RideSummary{
			Date: w.StartedAt,
		}
		if w.WorkoutType != nil {
			rs.WorkoutType = *w.WorkoutType
		}

		metrics, err := storage.GetRideMetrics(db, w.ID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("reporting.AssembleInput: get metrics for workout %d: %w", w.ID, err)
		}
		if metrics != nil {
			rs.DurationMin = metrics.DurationMin
			rs.AvgPower = metrics.AvgPower
			rs.AvgHR = metrics.AvgHR
			rs.AvgCadence = metrics.AvgCadence
			rs.NormalizedPower = metrics.NormalizedPower
			rs.IntensityFactor = metrics.IntensityFactor
			rs.HRDriftPct = metrics.HRDriftPct
			rs.TSS = metrics.TSS
			rs.HRZ1Pct = metrics.HRZ1Pct
			rs.HRZ2Pct = metrics.HRZ2Pct
			rs.HRZ3Pct = metrics.HRZ3Pct
			rs.HRZ4Pct = metrics.HRZ4Pct
			rs.HRZ5Pct = metrics.HRZ5Pct
			rs.PwrZ1Pct = metrics.PwrZ1Pct
			rs.PwrZ2Pct = metrics.PwrZ2Pct
			rs.PwrZ3Pct = metrics.PwrZ3Pct
			rs.PwrZ4Pct = metrics.PwrZ4Pct
			rs.PwrZ5Pct = metrics.PwrZ5Pct
			rs.CadLT70Pct = metrics.CadLT70Pct
			rs.Cad70To85Pct = metrics.Cad70To85Pct
			rs.Cad85To100Pct = metrics.Cad85To100Pct
			rs.CadGE100Pct = metrics.CadGE100Pct
			rs.ZoneTimeline = metrics.ZoneTimeline
			rs.HRZoneTimeline = metrics.HRZoneTimeline
		}

		rides = append(rides, rs)
	}

	notes, err := storage.ListNotesByDateRange(db, weekStart, weekEnd)
	if err != nil {
		return nil, fmt.Errorf("reporting.AssembleInput: list notes: %w", err)
	}

	noteSummaries := make([]NoteSummary, 0, len(notes))
	for _, n := range notes {
		noteSummaries = append(noteSummaries, NoteSummary{
			Timestamp:    n.Timestamp,
			Type:         n.Type,
			RPE:          n.RPE,
			WeightKG:     n.WeightKG,
			BodyFatPct:   n.BodyFatPct,
			MuscleMassKG: n.MuscleMassKG,
			Text:         n.Note,
		})
	}

	input := &ReportInput{
		Type:           reportType,
		WeekStart:      weekStart,
		WeekEnd:        weekEnd,
		AthleteProfile: string(profile),
		Rides:          rides,
		Notes:          noteSummaries,
	}

	// For weekly reports, pull in the plan that was written for this window so Claude
	// can compare what was planned against what actually happened.
	if reportType == storage.ReportTypeWeeklyReport {
		plan, err := storage.GetReport(db, storage.ReportTypeWeeklyPlan, weekStart)
		if err == nil {
			// Prefer the full narrative (session-by-session prescription) for comparison.
			// Fall back to summary only when narrative is absent (e.g. old rows before migration).
			if plan.NarrativeText != nil && *plan.NarrativeText != "" {
				input.PriorPlanNarrative = *plan.NarrativeText
			} else if plan.SummaryText != nil {
				input.PriorPlanNarrative = *plan.SummaryText
			}
		}
		// Missing plan is not an error — report is still generated without it.
	}

	return input, nil
}

// BuildPrompt constructs the LLM prompt from a ReportInput.
// The result is deterministic for a given input — no randomness or I/O.
func BuildPrompt(input *ReportInput) string {
	var b strings.Builder

	isReport := input.Type == storage.ReportTypeWeeklyReport

	if isReport {
		fmt.Fprintf(&b, "Generate a training report for the period from %s to %s.\n\n",
			input.WeekStart.Format("2006-01-02"),
			input.WeekEnd.Format("2006-01-02"),
		)
		b.WriteString("Analyse what actually happened in this period. Where the athlete followed the plan, confirm it. Where they deviated, explain the implications. Highlight key metrics (HR drift/decoupling, TSS, power trends).\n\n")
	} else {
		fmt.Fprintf(&b, "Generate a training plan for the period from %s to %s.\n\n",
			input.WeekStart.Format("2006-01-02"),
			input.WeekEnd.Format("2006-01-02"),
		)
		b.WriteString("Based on the athlete profile and recent training load, prescribe specific sessions for each day. Include target zones, duration, and the coaching rationale for each session.\n\n")
	}

	b.WriteString("## Exact calendar for this period\n\n")
	b.WriteString("Use the exact weekday/date mappings below when naming days in the output. Do not infer or rename weekdays separately from these dates.\n\n")
	b.WriteString(formatPeriodCalendar(input.WeekStart, input.WeekEnd))
	b.WriteString("\n")

	b.WriteString("## Rides in this period\n\n")
	if len(input.Rides) == 0 {
		b.WriteString("No rides recorded in this period.\n\n")
	} else {
		b.WriteString("Date       | Type            | Dur(min) | AvgP(W) | NP(W) | IF   | AvgHR | AvgCad | Drift% | TSS\n")
		b.WriteString("-----------|-----------------|----------|---------|-------|------|-------|--------|--------|-----\n")
		for _, r := range input.Rides {
			fmt.Fprintf(&b, "%s | %-15s | %s | %s | %s | %s | %s | %s | %s | %s\n",
				r.Date.Format("2006-01-02"),
				padOrDash(r.WorkoutType, 15),
				fmtOptFloat(r.DurationMin, "%.0f"),
				fmtOptFloat(r.AvgPower, "%.0f"),
				fmtOptFloat(r.NormalizedPower, "%.0f"),
				fmtOptFloat(r.IntensityFactor, "%.2f"),
				fmtOptFloat(r.AvgHR, "%.0f"),
				fmtOptFloat(r.AvgCadence, "%.0f"),
				fmtOptFloat(r.HRDriftPct, "%.1f"),
				fmtOptFloat(r.TSS, "%.0f"),
			)
		}
		b.WriteString("\n")

		// Per-ride detail: zone distributions and power zone timeline.
		for _, r := range input.Rides {
			hasZones := r.PwrZ1Pct != nil
			hasCadenceDist := r.CadLT70Pct != nil
			hasPowerTimeline := r.ZoneTimeline != nil && *r.ZoneTimeline != ""
			hasHRTimeline := r.HRZoneTimeline != nil && *r.HRZoneTimeline != ""
			if !hasZones && !hasCadenceDist && !hasPowerTimeline && !hasHRTimeline {
				continue
			}
			fmt.Fprintf(&b, "### %s %s\n\n", r.Date.Format("2006-01-02"), r.WorkoutType)

			if hasZones {
				b.WriteString("Power zones: ")
				fmt.Fprintf(&b, "Z1=%.0f%% Z2=%.0f%% Z3=%.0f%% Z4=%.0f%% Z5=%.0f%%",
					derefFloat(r.PwrZ1Pct), derefFloat(r.PwrZ2Pct), derefFloat(r.PwrZ3Pct),
					derefFloat(r.PwrZ4Pct), derefFloat(r.PwrZ5Pct))
				b.WriteString("\n")
				if r.HRZ1Pct != nil {
					b.WriteString("HR zones:    ")
					fmt.Fprintf(&b, "Z1=%.0f%% Z2=%.0f%% Z3=%.0f%% Z4=%.0f%% Z5=%.0f%%",
						derefFloat(r.HRZ1Pct), derefFloat(r.HRZ2Pct), derefFloat(r.HRZ3Pct),
						derefFloat(r.HRZ4Pct), derefFloat(r.HRZ5Pct))
					b.WriteString("\n")
				}
			}
			if hasCadenceDist {
				b.WriteString("Cadence:     ")
				fmt.Fprintf(&b, "<70=%.0f%% 70-85=%.0f%% 85-100=%.0f%% 100+=%.0f%%",
					derefFloat(r.CadLT70Pct), derefFloat(r.Cad70To85Pct), derefFloat(r.Cad85To100Pct), derefFloat(r.CadGE100Pct))
				b.WriteString("\n")
			}

			if hasPowerTimeline {
				b.WriteString("\nPower zone timeline:\n")
				b.WriteString(formatZoneTimeline(*r.ZoneTimeline))
			}
			if hasHRTimeline {
				b.WriteString("\nHR zone timeline:\n")
				b.WriteString(formatHRZoneTimeline(*r.HRZoneTimeline))
			}
			b.WriteString("\n")
		}
	}

	if len(input.Notes) > 0 {
		b.WriteString("## Athlete notes\n\n")
		for _, n := range input.Notes {
			fmt.Fprintf(&b, "- %s [%s]", n.Timestamp.Format("2006-01-02 15:04"), string(n.Type))
			if n.RPE != nil {
				fmt.Fprintf(&b, " RPE=%d", *n.RPE)
			}
			if n.WeightKG != nil {
				fmt.Fprintf(&b, " weight=%.1fkg", *n.WeightKG)
			}
			if n.BodyFatPct != nil {
				fmt.Fprintf(&b, " bf=%.1f%%", *n.BodyFatPct)
			}
			if n.MuscleMassKG != nil {
				fmt.Fprintf(&b, " muscle=%.1fkg", *n.MuscleMassKG)
			}
			if n.Text != nil && *n.Text != "" {
				fmt.Fprintf(&b, " — %s", *n.Text)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	if !isReport && input.UserPrompt != "" {
		b.WriteString("## Athlete constraints / notes for this period\n\n")
		b.WriteString(input.UserPrompt)
		b.WriteString("\n\n")
		b.WriteString("Take these constraints into account when building the plan.\n\n")
	}

	if isReport && input.PriorPlanNarrative != "" {
		b.WriteString("## Training plan for this period (what was prescribed)\n\n")
		b.WriteString(input.PriorPlanNarrative)
		b.WriteString("\n\n")
		b.WriteString("Compare actual execution against this plan. Note compliance, deviations, and their likely causes.\n\n")
		if reportExtendsBeyondPlannedWeek(input) {
			b.WriteString("The actual execution window extends beyond the original planned 7-day block. Treat the extra days as a continuation after the planned week ended: explain how execution drifted beyond the original window, assess what that means for fatigue and progression, and use that extended reality when framing the next period.\n\n")
		}
	}

	b.WriteString(`## Output format

Respond with a JSON object containing exactly two fields:
- "summary": a compact plain-text coaching summary (max 5 lines, no markdown) suitable for Telegram
- "narrative": the full coaching analysis in markdown, with sections, observations, and recommendations

Example:
{"summary":"...", "narrative":"..."}`)

	return b.String()
}

func derefFloat(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

func reportExtendsBeyondPlannedWeek(input *ReportInput) bool {
	if input == nil {
		return false
	}
	plannedEnd := input.WeekStart.AddDate(0, 0, 6)
	for _, r := range input.Rides {
		if r.Date.After(plannedEnd) {
			return true
		}
	}
	for _, n := range input.Notes {
		if n.Timestamp.After(plannedEnd) {
			return true
		}
	}
	return false
}

func formatPeriodCalendar(start, end time.Time) string {
	start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
	end = time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, end.Location())
	if end.Before(start) {
		return ""
	}
	var b strings.Builder
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		fmt.Fprintf(&b, "- %s = %s\n", d.Format("2006-01-02"), d.Format("Monday, January 2, 2006"))
	}
	return b.String()
}

// formatZoneTimeline converts the JSON zone timeline into a human-readable block.
// Input: [{"zone":2,"start_min":0,"duration_min":12,"avg_power":155}, ...]
// Output:
//
//	0:00–0:12  Z2  12 min  155W avg
//	0:12–0:32  Z4  20 min  240W avg
func formatZoneTimeline(jsonStr string) string {
	type seg struct {
		Zone        int     `json:"zone"`
		StartMin    float64 `json:"start_min"`
		DurationMin float64 `json:"duration_min"`
		AvgPower    float64 `json:"avg_power"`
	}
	var segs []seg
	if err := json.Unmarshal([]byte(jsonStr), &segs); err != nil || len(segs) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, s := range segs {
		startM := int(s.StartMin)
		startS := int((s.StartMin - float64(startM)) * 60)
		endMin := s.StartMin + s.DurationMin
		endM := int(endMin)
		endS := int((endMin - float64(endM)) * 60)
		fmt.Fprintf(&sb, "%d:%02d–%d:%02d  Z%d  %.0f min  %.0fW avg\n",
			startM, startS, endM, endS, s.Zone, s.DurationMin, s.AvgPower)
	}
	return sb.String()
}

// formatHRZoneTimeline converts the JSON HR zone timeline into a human-readable block.
// Input: [{"zone":2,"start_min":0,"duration_min":12,"avg_hr":132}, ...]
func formatHRZoneTimeline(jsonStr string) string {
	type seg struct {
		Zone        int     `json:"zone"`
		StartMin    float64 `json:"start_min"`
		DurationMin float64 `json:"duration_min"`
		AvgHR       float64 `json:"avg_hr"`
	}
	var segs []seg
	if err := json.Unmarshal([]byte(jsonStr), &segs); err != nil || len(segs) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, s := range segs {
		startM := int(s.StartMin)
		startS := int((s.StartMin - float64(startM)) * 60)
		endMin := s.StartMin + s.DurationMin
		endM := int(endMin)
		endS := int((endMin - float64(endM)) * 60)
		fmt.Fprintf(&sb, "%d:%02d–%d:%02d  Z%d  %.0f min  %.0fbpm avg\n",
			startM, startS, endM, endS, s.Zone, s.DurationMin, s.AvgHR)
	}
	return sb.String()
}

func fmtOptFloat(v *float64, format string) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf(format, *v)
}

func padOrDash(s string, width int) string {
	if s == "" {
		return fmt.Sprintf("%-*s", width, "-")
	}
	if len(s) >= width {
		return s[:width]
	}
	return fmt.Sprintf("%-*s", width, s)
}

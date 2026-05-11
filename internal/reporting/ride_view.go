package reporting

import (
	"fmt"
	"strings"

	"cycling-coach/internal/storage"
)

// FormatWorkoutSummaryTable formats the per-ride summary row in the same shape
// used when assembling report input for Claude.
func FormatWorkoutSummaryTable(w *storage.WorkoutWithMetrics) string {
	if w == nil {
		return ""
	}

	var b strings.Builder
	b.WriteString("Date       | Type            | Dur(min) | AvgP(W) | NP(W) | IF    |  VI  | AvgHR | AvgCad | Drift% | TSS\n")
	fmt.Fprintf(&b, "%-10s | %-15s | %8s | %7s | %5s | %5s | %4s | %5s | %6s | %6s | %3s",
		w.StartedAt.Format("2006-01-02"),
		padOrDash(workoutTypeLabel(w.WorkoutType), 15),
		fmtDurationMin(w.DurationSec),
		fmtOptFloat(w.AvgPower, "%.0f"),
		fmtOptFloat(w.NormalizedPower, "%.0f"),
		fmtOptFloat(w.IntensityFactor, "%.2f"),
		fmtOptFloat(w.VariabilityIndex, "%.2f"),
		fmtOptFloat(w.AvgHR, "%.0f"),
		fmtOptFloat(w.AvgCadence, "%.0f"),
		fmtOptFloat(w.HRDriftPct, "%.1f"),
		fmtOptFloat(w.TSS, "%.0f"),
	)
	return b.String()
}

// FormatWorkoutZoneDetail formats the zone section for a single ride in the same
// general shape used in the Claude prompt.
func FormatWorkoutZoneDetail(w *storage.WorkoutWithMetrics) string {
	if w == nil {
		return ""
	}

	hasZones := w.PwrZ1Pct != nil
	hasCadenceDist := w.CadLT70Pct != nil
	hasPowerTimeline := w.ZoneTimeline != nil && *w.ZoneTimeline != ""
	hasHRTimeline := w.HRZoneTimeline != nil && *w.HRZoneTimeline != ""
	if !hasZones && !hasCadenceDist && !hasPowerTimeline && !hasHRTimeline {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "### %s %s\n\n", w.StartedAt.Format("2006-01-02"), workoutTypeLabel(w.WorkoutType))

	if hasZones {
		fmt.Fprintf(&b, "Power zones: Z1=%.0f%% Z2=%.0f%% Z3=%.0f%% Z4=%.0f%% Z5=%.0f%%\n",
			derefFloat(w.PwrZ1Pct), derefFloat(w.PwrZ2Pct), derefFloat(w.PwrZ3Pct), derefFloat(w.PwrZ4Pct), derefFloat(w.PwrZ5Pct))
		if w.HRZ1Pct != nil {
			fmt.Fprintf(&b, "HR zones:    Z1=%.0f%% Z2=%.0f%% Z3=%.0f%% Z4=%.0f%% Z5=%.0f%%\n",
				derefFloat(w.HRZ1Pct), derefFloat(w.HRZ2Pct), derefFloat(w.HRZ3Pct), derefFloat(w.HRZ4Pct), derefFloat(w.HRZ5Pct))
		}
	}
	if hasCadenceDist {
		fmt.Fprintf(&b, "Cadence:     <70=%.0f%% 70-85=%.0f%% 85-100=%.0f%% 100+=%.0f%%\n",
			derefFloat(w.CadLT70Pct), derefFloat(w.Cad70To85Pct), derefFloat(w.Cad85To100Pct), derefFloat(w.CadGE100Pct))
	}

	if hasPowerTimeline {
		if hasZones {
			b.WriteString("\n")
		}
		b.WriteString("Power zone timeline:\n")
		b.WriteString(formatZoneTimeline(*w.ZoneTimeline))
	}
	if hasHRTimeline {
		if hasZones || hasPowerTimeline {
			b.WriteString("\n")
		}
		b.WriteString("HR zone timeline:\n")
		b.WriteString(formatHRZoneTimeline(*w.HRZoneTimeline))
	}

	return strings.TrimSpace(b.String())
}

func workoutTypeLabel(v *string) string {
	if v == nil || *v == "" {
		return "-"
	}
	return *v
}

func fmtDurationMin(durationSec *int64) string {
	if durationSec == nil {
		return "-"
	}
	return fmt.Sprintf("%.0f", float64(*durationSec)/60.0)
}

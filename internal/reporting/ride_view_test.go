package reporting

import (
	"strings"
	"testing"
	"time"

	"cycling-coach/internal/storage"
)

func TestFormatWorkoutSummaryTable(t *testing.T) {
	durSec := int64(3600)
	avgP := 195.0
	np := 210.0
	ifv := 0.84
	avgHR := 142.0
	drift := 3.2
	tss := 70.0

	got := FormatWorkoutSummaryTable(&storage.WorkoutWithMetrics{
		WahooID:         "42001",
		StartedAt:       time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC),
		DurationSec:     &durSec,
		WorkoutType:     ptrString("Biking Indoor"),
		AvgPower:        &avgP,
		NormalizedPower: &np,
		IntensityFactor: &ifv,
		AvgHR:           &avgHR,
		HRDriftPct:      &drift,
		TSS:             &tss,
	})

	for _, want := range []string{
		"Date       | Type",
		"2026-04-02",
		"Biking Indoor",
		"195",
		"210",
		"0.84",
		"142",
		"3.2",
		"70",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary table missing %q:\n%s", want, got)
		}
	}
}

func TestFormatWorkoutZoneDetail(t *testing.T) {
	p1, p2, p3, p4, p5 := 5.0, 45.0, 10.0, 35.0, 5.0
	h1, h2, h3, h4, h5 := 8.0, 50.0, 12.0, 25.0, 5.0
	timeline := `[{"zone":2,"start_min":0,"duration_min":12,"avg_power":155},{"zone":4,"start_min":12,"duration_min":20,"avg_power":240}]`

	got := FormatWorkoutZoneDetail(&storage.WorkoutWithMetrics{
		StartedAt:    time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC),
		WorkoutType:  ptrString("Biking Indoor"),
		PwrZ1Pct:     &p1,
		PwrZ2Pct:     &p2,
		PwrZ3Pct:     &p3,
		PwrZ4Pct:     &p4,
		PwrZ5Pct:     &p5,
		HRZ1Pct:      &h1,
		HRZ2Pct:      &h2,
		HRZ3Pct:      &h3,
		HRZ4Pct:      &h4,
		HRZ5Pct:      &h5,
		ZoneTimeline: &timeline,
	})

	for _, want := range []string{
		"### 2026-04-02 Biking Indoor",
		"Power zones: Z1=5% Z2=45% Z3=10% Z4=35% Z5=5%",
		"HR zones:    Z1=8% Z2=50% Z3=12% Z4=25% Z5=5%",
		"Power zone timeline:",
		"0:00",
		"Z2",
		"155W avg",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("zone detail missing %q:\n%s", want, got)
		}
	}
}

func ptrString(v string) *string { return &v }

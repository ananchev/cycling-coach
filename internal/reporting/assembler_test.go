package reporting_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"cycling-coach/internal/reporting"
	"cycling-coach/internal/storage"
)

func TestAssembleInput_NoRidesNoNotes(t *testing.T) {
	db := openTestDB(t)
	profile := writeTempProfile(t, "# Athlete Profile\nFTP: 251W")

	weekStart := time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC)
	weekEnd := weekStart.Add(7 * 24 * time.Hour)

	input, err := reporting.AssembleInput(context.Background(), db, profile, storage.ReportTypeWeeklyReport, weekStart, weekEnd)
	if err != nil {
		t.Fatalf("AssembleInput error: %v", err)
	}

	if len(input.Rides) != 0 {
		t.Errorf("expected 0 rides, got %d", len(input.Rides))
	}
	if len(input.Notes) != 0 {
		t.Errorf("expected 0 notes, got %d", len(input.Notes))
	}
	if input.AthleteProfile != "# Athlete Profile\nFTP: 251W" {
		t.Errorf("unexpected profile content: %q", input.AthleteProfile)
	}
	if input.Type != storage.ReportTypeWeeklyReport {
		t.Errorf("unexpected type: %v", input.Type)
	}
}

func TestAssembleInput_WithRideAndNote(t *testing.T) {
	db := openTestDB(t)
	profile := writeTempProfile(t, "FTP: 251W")

	weekStart := time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC)
	weekEnd := weekStart.Add(7 * 24 * time.Hour)

	// Insert a workout inside the window.
	wType := "cycling"
	w := &storage.Workout{
		WahooID:     "w1",
		StartedAt:   weekStart.Add(24 * time.Hour),
		WorkoutType: &wType,
		Source:      "api",
	}
	wID, _, err := storage.UpsertWorkout(db, w)
	if err != nil {
		t.Fatalf("UpsertWorkout: %v", err)
	}

	// Insert metrics for that workout.
	dur := 90.0
	avgPwr := 200.0
	avgHR := 135.0
	drift := 4.5
	tss := 80.0
	if err := storage.UpsertRideMetrics(db, &storage.RideMetrics{
		WorkoutID:   wID,
		DurationMin: &dur,
		AvgPower:    &avgPwr,
		AvgHR:       &avgHR,
		HRDriftPct:  &drift,
		TSS:         &tss,
	}); err != nil {
		t.Fatalf("UpsertRideMetrics: %v", err)
	}

	// Insert a note inside the window.
	rpe := int64(6)
	noteText := "felt good"
	if _, err := storage.InsertNote(db, &storage.AthleteNote{
		Timestamp: weekStart.Add(25 * time.Hour),
		Type:      storage.NoteTypeRide,
		RPE:       &rpe,
		Note:      &noteText,
	}); err != nil {
		t.Fatalf("InsertNote: %v", err)
	}

	input, err := reporting.AssembleInput(context.Background(), db, profile, storage.ReportTypeWeeklyReport, weekStart, weekEnd)
	if err != nil {
		t.Fatalf("AssembleInput error: %v", err)
	}

	if len(input.Rides) != 1 {
		t.Fatalf("expected 1 ride, got %d", len(input.Rides))
	}
	r := input.Rides[0]
	if r.WorkoutType != "cycling" {
		t.Errorf("unexpected workout type: %q", r.WorkoutType)
	}
	if r.DurationMin == nil || *r.DurationMin != 90.0 {
		t.Errorf("unexpected DurationMin: %v", r.DurationMin)
	}
	if r.HRDriftPct == nil || *r.HRDriftPct != 4.5 {
		t.Errorf("unexpected HRDriftPct: %v", r.HRDriftPct)
	}

	if len(input.Notes) != 1 {
		t.Fatalf("expected 1 note, got %d", len(input.Notes))
	}
	n := input.Notes[0]
	if n.RPE == nil || *n.RPE != 6 {
		t.Errorf("unexpected RPE: %v", n.RPE)
	}
	if n.Text == nil || *n.Text != "felt good" {
		t.Errorf("unexpected note text: %v", n.Text)
	}
}

func TestBuildPrompt_ContainsKeyFields(t *testing.T) {
	dur := 60.0
	avgPwr := 230.0
	drift := 6.2
	weekStart := time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC)
	weekEnd := weekStart.Add(7 * 24 * time.Hour)

	input := &reporting.ReportInput{
		Type:           storage.ReportTypeWeeklyReport,
		WeekStart:      weekStart,
		WeekEnd:        weekEnd,
		AthleteProfile: "FTP: 251W",
		Rides: []reporting.RideSummary{
			{
				Date:        weekStart.Add(24 * time.Hour),
				WorkoutType: "cycling",
				DurationMin: &dur,
				AvgPower:    &avgPwr,
				HRDriftPct:  &drift,
			},
		},
	}

	prompt := reporting.BuildPrompt(input)

	checks := []string{
		"2026-03-09",
		"weekly training report",
		"cycling",
		"60",  // duration
		"230", // power
		"6.2", // drift
		"Output format",
	}
	for _, want := range checks {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestBuildPrompt_NoRides(t *testing.T) {
	weekStart := time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC)
	input := &reporting.ReportInput{
		Type:      storage.ReportTypeWeeklyReport,
		WeekStart: weekStart,
		WeekEnd:   weekStart.Add(7 * 24 * time.Hour),
	}
	prompt := reporting.BuildPrompt(input)
	if !strings.Contains(prompt, "No rides recorded") {
		t.Errorf("prompt should mention no rides; got:\n%s", prompt)
	}
}

func TestBuildPrompt_WeeklyPlan(t *testing.T) {
	weekStart := time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC)
	input := &reporting.ReportInput{
		Type:      storage.ReportTypeWeeklyPlan,
		WeekStart: weekStart,
		WeekEnd:   weekStart.Add(7 * 24 * time.Hour),
	}
	prompt := reporting.BuildPrompt(input)
	if !strings.Contains(prompt, "weekly training plan") {
		t.Errorf("prompt should reference 'weekly training plan'; got:\n%s", prompt)
	}
}

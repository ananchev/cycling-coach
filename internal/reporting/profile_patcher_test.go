package reporting_test

import (
	"strings"
	"testing"
	"time"

	"cycling-coach/internal/reporting"
	"cycling-coach/internal/storage"
)

const testProfileWithSections = `# Cycling Coach — Athlete Profile

## Athlete

- **Goal:** Summit the Stelvio Pass (July 2026)
- **FTP:** 250 W

---

## Heart Rate Zones

<!-- PROTECTED -->

| Zone | Range (bpm) | Purpose |
|------|-------------|---------|
| Z1 | <110 | Recovery |
| Z2 | 110–127 | Endurance |

---

## Power Zones

<!-- PROTECTED -->

| Zone | Power (W) | Purpose |
|------|-----------|---------|
| Z1 | <138 | Recovery |
| Z2 | 139–188 | Endurance |

---

## How to interpret the pre-computed metrics

<!-- PROTECTED -->

| Metric | What it means | How to interpret |
|--------|--------------|-----------------|
| **TSS** | Training Stress Score | Single session: <75 low |

---

## Recent training weeks

<!-- PROTECTED -->

| Week ending | TSS | Key session | Avg drift | Notes |
|-------------|-----|-------------|-----------|-------|
| Apr 4, 2026 | 220 | 2026-04-02 55min 145W | 3.8% | Base steady |
| Apr 11, 2026 | 245 | 2026-04-09 60min 150W | 4.1% | Tempo introduced |

---

## Stelvio readiness milestones

<!-- PROTECTED -->

Target: Stelvio Pass, July 2026

| # | Milestone | Status |
|---|-----------|--------|
| 1 | Z2 power stable ≥140 W for 2+ consecutive weeks | ⬜ Not yet |
| 2 | HR drift consistently <5% on 60 min Z2 sessions | ⬜ Not yet |
| 3 | Complete 2×15 min sweetspot intervals with drift <8% | ⬜ Not yet |
| 4 | Complete 90 min endurance ride outdoors with <6% drift | ⬜ Not yet |
| 5 | Sustain 70–75 rpm cadence for full 60 min session | ⬜ Not yet |
| 6 | Complete 2×20 min threshold intervals at >0.88 IF | ⬜ Not yet |
| 7 | Complete 3+ hour outdoor ride with negative-split power | ⬜ Not yet |

---

## Your role as coach

<!-- PROTECTED -->

When generating a **weekly report:**
- Summarise what actually happened

---

## Weekly structure template

<!-- PROTECTED -->

| Day | Focus |
|-----|-------|
| Mon | Recovery |

---

## Warning flags

<!-- PROTECTED -->

- **HR drift >8%:** flag and explain

---

## Training history and current phase

### Current phase
- Aerobic base established

---

## Last Updated

**2026-04-11**

- Base phase stable
`

func TestApplyProfilePatch_AppendsRecentWeekRow(t *testing.T) {
	patch := reporting.ProfilePatchOutput{
		RecentWeeksRow: "| Apr 18, 2026 | 241 | 2×15 min @ 155W ✅ | 2.1% | Sweetspot intro |",
		Milestones: map[string]string{
			"1": "✅ Met",
			"2": "🟡 Close",
			"3": "⬜ Not yet",
			"4": "⬜ Not yet",
			"5": "⬜ Not yet",
			"6": "⬜ Not yet",
			"7": "⬜ Not yet",
		},
		LastUpdated: "2026-04-18",
	}

	result, err := reporting.ApplyProfilePatch(testProfileWithSections, patch)
	if err != nil {
		t.Fatalf("ApplyProfilePatch error: %v", err)
	}

	// Should contain the new row
	if !strings.Contains(result, "Apr 18, 2026") {
		t.Error("patched profile missing new recent weeks row")
	}

	// Should still contain the old rows
	if !strings.Contains(result, "Apr 4, 2026") {
		t.Error("patched profile lost old row 1")
	}
	if !strings.Contains(result, "Apr 11, 2026") {
		t.Error("patched profile lost old row 2")
	}

	// Milestone 1 should be updated
	if !strings.Contains(result, "✅ Met") {
		t.Error("patched profile missing milestone 1 update to Met")
	}

	// Milestone 2 should be Close
	if !strings.Contains(result, "🟡 Close") {
		t.Error("patched profile missing milestone 2 update to Close")
	}

	// Last Updated should be changed
	if !strings.Contains(result, "**2026-04-18**") {
		t.Error("patched profile missing updated Last Updated date")
	}

	// Old date should be gone
	if strings.Contains(result, "**2026-04-11**") {
		t.Error("patched profile still contains old Last Updated date")
	}
}

func TestApplyProfilePatch_DropsOldestWhenOver8Rows(t *testing.T) {
	// Build a profile with 8 existing rows
	var rows []string
	for i := 1; i <= 8; i++ {
		rows = append(rows, "| row "+string(rune('0'+i))+" | 200 | session | 3.0% | note |")
	}

	profile := `## Recent training weeks

| Week ending | TSS | Key session | Avg drift | Notes |
|-------------|-----|-------------|-----------|-------|
` + strings.Join(rows, "\n") + `

---

## Stelvio readiness milestones

| # | Milestone | Status |
|---|-----------|--------|
| 1 | Test | ⬜ Not yet |

---

## Heart Rate Zones

Zones here

---

## Power Zones

Zones here

---

## How to interpret the pre-computed metrics

Metrics here

---

## Your role as coach

Role here

---

## Weekly structure template

Template here

---

## Warning flags

Flags here

---

## Last Updated

**2026-04-11**
`

	patch := reporting.ProfilePatchOutput{
		RecentWeeksRow: "| Apr 18, 2026 | 250 | new session | 2.5% | new note |",
		Milestones:     map[string]string{"1": "⬜ Not yet"},
		LastUpdated:    "2026-04-18",
	}

	result, err := reporting.ApplyProfilePatch(profile, patch)
	if err != nil {
		t.Fatalf("ApplyProfilePatch error: %v", err)
	}

	// row 1 should be dropped (oldest)
	if strings.Contains(result, "row 1") {
		t.Error("expected oldest row to be dropped when >8 rows")
	}

	// row 2 should survive
	if !strings.Contains(result, "row 2") {
		t.Error("expected row 2 to survive")
	}

	// new row should be present
	if !strings.Contains(result, "Apr 18, 2026") {
		t.Error("new row missing")
	}
}

func TestApplyProfilePatch_PlaceholderRowIsReplaced(t *testing.T) {
	profile := `## Recent training weeks

| Week ending | TSS | Key session | Avg drift | Notes |
|-------------|-----|-------------|-----------|-------|
| *(no data yet — rows are added automatically after each block close)* |

---

## Stelvio readiness milestones

| # | Milestone | Status |
|---|-----------|--------|
| 1 | Test | ⬜ Not yet |

---

## Heart Rate Zones

Z

---

## Power Zones

Z

---

## How to interpret the pre-computed metrics

M

---

## Your role as coach

R

---

## Weekly structure template

T

---

## Warning flags

W

---

## Last Updated

**2026-04-01**
`

	patch := reporting.ProfilePatchOutput{
		RecentWeeksRow: "| Apr 18, 2026 | 200 | first session | 4.0% | getting started |",
		Milestones:     map[string]string{"1": "⬜ Not yet"},
		LastUpdated:    "2026-04-18",
	}

	result, err := reporting.ApplyProfilePatch(profile, patch)
	if err != nil {
		t.Fatalf("ApplyProfilePatch error: %v", err)
	}

	if strings.Contains(result, "no data yet") {
		t.Error("placeholder row should have been removed")
	}
	if !strings.Contains(result, "Apr 18, 2026") {
		t.Error("new row should be present")
	}
}

func TestApplyProfilePatch_PreservesProtectedSections(t *testing.T) {
	patch := reporting.ProfilePatchOutput{
		RecentWeeksRow: "| Apr 18, 2026 | 241 | session | 2.1% | note |",
		Milestones:     map[string]string{"1": "⬜ Not yet", "2": "⬜ Not yet", "3": "⬜ Not yet", "4": "⬜ Not yet", "5": "⬜ Not yet", "6": "⬜ Not yet", "7": "⬜ Not yet"},
		LastUpdated:    "2026-04-18",
	}

	result, err := reporting.ApplyProfilePatch(testProfileWithSections, patch)
	if err != nil {
		t.Fatalf("ApplyProfilePatch error: %v", err)
	}

	for _, section := range []string{
		"Heart Rate Zones",
		"Power Zones",
		"How to interpret the pre-computed metrics",
		"Your role as coach",
		"Weekly structure template",
		"Warning flags",
		"Recent training weeks",
		"Stelvio readiness milestones",
	} {
		if !strings.Contains(result, section) {
			t.Errorf("patched profile missing protected section %q", section)
		}
	}
}

func TestApplyProfilePatch_MissingSectionReturnsError(t *testing.T) {
	profile := "## Just some content\n\nNo relevant sections here.\n"
	patch := reporting.ProfilePatchOutput{
		RecentWeeksRow: "| row |",
		Milestones:     map[string]string{},
		LastUpdated:    "2026-04-18",
	}

	_, err := reporting.ApplyProfilePatch(profile, patch)
	if err == nil {
		t.Fatal("expected error when required sections are missing")
	}
}

func TestComputeWeekMetrics_SumsTSSAndAveragesDrift(t *testing.T) {
	tss1, tss2 := 80.0, 120.0
	drift1, drift2 := 4.0, 6.0
	dur := 60.0
	pwr := 150.0

	input := &reporting.ReportInput{
		Type:      storage.ReportTypeWeeklyReport,
		WeekStart: time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC),
		WeekEnd:   time.Date(2026, 4, 18, 0, 0, 0, 0, time.UTC),
		Rides: []reporting.RideSummary{
			{
				Date:        time.Date(2026, 4, 13, 0, 0, 0, 0, time.UTC),
				DurationMin: &dur,
				AvgPower:    &pwr,
				TSS:         &tss1,
				HRDriftPct:  &drift1,
			},
			{
				Date:        time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC),
				DurationMin: &dur,
				AvgPower:    &pwr,
				TSS:         &tss2,
				HRDriftPct:  &drift2,
			},
		},
	}

	wm := reporting.ComputeWeekMetrics(input)

	if wm.TotalTSS != 200.0 {
		t.Errorf("TotalTSS = %.0f, want 200", wm.TotalTSS)
	}
	if wm.AvgDrift != 5.0 {
		t.Errorf("AvgDrift = %.1f, want 5.0", wm.AvgDrift)
	}
	if wm.WeekEnding != "Apr 18, 2026" {
		t.Errorf("WeekEnding = %q, want Apr 18, 2026", wm.WeekEnding)
	}
	// Key session should be the ride with highest TSS
	if !strings.Contains(wm.KeySession, "2026-04-15") {
		t.Errorf("KeySession = %q, should reference the higher-TSS ride on 2026-04-15", wm.KeySession)
	}
}

func TestComputeWeekMetrics_NoRides(t *testing.T) {
	input := &reporting.ReportInput{
		Type:      storage.ReportTypeWeeklyReport,
		WeekStart: time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC),
		WeekEnd:   time.Date(2026, 4, 18, 0, 0, 0, 0, time.UTC),
	}

	wm := reporting.ComputeWeekMetrics(input)

	if wm.TotalTSS != 0 {
		t.Errorf("TotalTSS = %.0f, want 0", wm.TotalTSS)
	}
	if wm.AvgDrift != 0 {
		t.Errorf("AvgDrift = %.1f, want 0", wm.AvgDrift)
	}
}

func TestFindSection_FindsCorrectBounds(t *testing.T) {
	profile := `## Section One

Content one

---

## Section Two

Content two

---

## Section Three

Content three
`

	start, end, found := reporting.FindSection(profile, "## Section Two")
	if !found {
		t.Fatal("expected to find Section Two")
	}

	section := profile[start:end]
	if !strings.Contains(section, "Content two") {
		t.Errorf("section should contain 'Content two', got: %q", section)
	}
	if strings.Contains(section, "Content one") {
		t.Errorf("section should not contain 'Content one'")
	}
	if strings.Contains(section, "Content three") {
		t.Errorf("section should not contain 'Content three'")
	}
}

func TestFindSection_NotFound(t *testing.T) {
	_, _, found := reporting.FindSection("## Other\n\ncontent\n", "## Missing")
	if found {
		t.Error("expected not found for missing section")
	}
}

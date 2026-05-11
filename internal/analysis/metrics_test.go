package analysis

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	fitpkg "cycling-coach/internal/fit"
)

var testZones = ZoneConfig{
	FTPWatts: 200,
	HRMax:    180,
	HRZ1Max:  110, HRZ2Max: 130, HRZ3Max: 150, HRZ4Max: 165,
	PwrZ1Max: 110, PwrZ2Max: 160, PwrZ3Max: 180, PwrZ4Max: 200,
}

func makeRecords(count int, hr uint8, power uint16, cadence uint8) []fitpkg.Record {
	records := make([]fitpkg.Record, count)
	for i := range records {
		records[i] = fitpkg.Record{
			Timestamp: time.Now().Add(time.Duration(i) * time.Second),
			HeartRate: hr,
			Power:     power,
			Cadence:   cadence,
		}
	}
	return records
}

func TestCompute_BasicAverages(t *testing.T) {
	records := makeRecords(120, 140, 200, 80)
	p := &fitpkg.ParsedFIT{
		Session: fitpkg.Session{DurationSec: 120},
		Records: records,
	}
	m := Compute(p, testZones)

	if m.AvgHR != 140 {
		t.Errorf("AvgHR = %f, want 140", m.AvgHR)
	}
	if m.MaxHR != 140 {
		t.Errorf("MaxHR = %f, want 140", m.MaxHR)
	}
	if m.AvgPower != 200 {
		t.Errorf("AvgPower = %f, want 200", m.AvgPower)
	}
	if m.AvgCadence != 80 {
		t.Errorf("AvgCadence = %f, want 80", m.AvgCadence)
	}
	if m.DurationMin != 2.0 {
		t.Errorf("DurationMin = %f, want 2.0", m.DurationMin)
	}
}

func TestCompute_ZeroHRAndPowerIgnored(t *testing.T) {
	records := []fitpkg.Record{
		{HeartRate: 0, Power: 0},
		{HeartRate: 150, Power: 200},
		{HeartRate: 0, Power: 0},
	}
	p := &fitpkg.ParsedFIT{Records: records}
	m := Compute(p, testZones)

	if m.AvgHR != 150 {
		t.Errorf("AvgHR = %f, want 150 (zeros excluded)", m.AvgHR)
	}
	if m.AvgPower != 200 {
		t.Errorf("AvgPower = %f, want 200 (zeros excluded)", m.AvgPower)
	}
}

func TestCompute_HRZoneDistribution(t *testing.T) {
	// 60 recs in Z2 (120 bpm), 60 recs in Z4 (160 bpm).
	records := append(
		makeRecords(60, 120, 150, 70),
		makeRecords(60, 160, 180, 70)...,
	)
	p := &fitpkg.ParsedFIT{Records: records}
	m := Compute(p, testZones)

	if m.HRZ2Pct != 50.0 {
		t.Errorf("HRZ2Pct = %f, want 50.0", m.HRZ2Pct)
	}
	if m.HRZ4Pct != 50.0 {
		t.Errorf("HRZ4Pct = %f, want 50.0", m.HRZ4Pct)
	}
	total := m.HRZ1Pct + m.HRZ2Pct + m.HRZ3Pct + m.HRZ4Pct + m.HRZ5Pct
	if total < 99.9 || total > 100.1 {
		t.Errorf("HR zone percentages sum = %f, want ~100", total)
	}
}

func TestCompute_PowerZoneDistribution(t *testing.T) {
	// 60 recs in Z1 (100W), 60 recs in Z5 (250W).
	records := append(
		makeRecords(60, 120, 100, 70),
		makeRecords(60, 130, 250, 75)...,
	)
	p := &fitpkg.ParsedFIT{Records: records}
	m := Compute(p, testZones)

	if m.PwrZ1Pct != 50.0 {
		t.Errorf("PwrZ1Pct = %f, want 50.0", m.PwrZ1Pct)
	}
	if m.PwrZ5Pct != 50.0 {
		t.Errorf("PwrZ5Pct = %f, want 50.0", m.PwrZ5Pct)
	}
}

func TestCompute_TRIMP_PositiveForValidHR(t *testing.T) {
	records := makeRecords(3600, 140, 180, 80) // 1 hour at 140 bpm
	p := &fitpkg.ParsedFIT{
		Session: fitpkg.Session{DurationSec: 3600},
		Records: records,
	}
	m := Compute(p, testZones)

	if m.TRIMP <= 0 {
		t.Errorf("TRIMP = %f, want > 0", m.TRIMP)
	}
}

func TestCompute_TSS_IntensityFactor(t *testing.T) {
	// Constant 200W = 100% FTP → IF = 1.0, TSS ≈ 100 for 1 hour.
	// Use 3630 records (1 hour + 30 for NP rolling window).
	records := makeRecords(3630, 150, 200, 80)
	p := &fitpkg.ParsedFIT{
		Session: fitpkg.Session{DurationSec: 3600},
		Records: records,
	}
	m := Compute(p, testZones)

	if m.IntensityFactor < 0.95 || m.IntensityFactor > 1.05 {
		t.Errorf("IntensityFactor = %f, want ~1.0", m.IntensityFactor)
	}
	if m.TSS < 90 || m.TSS > 110 {
		t.Errorf("TSS = %f, want ~100 for 1h at FTP", m.TSS)
	}
}

func TestCompute_EfficiencyFactor(t *testing.T) {
	records := makeRecords(120, 150, 180, 80)
	p := &fitpkg.ParsedFIT{Records: records}
	m := Compute(p, testZones)

	wantEF := 180.0 / 150.0
	if m.EfficiencyFactor < wantEF-0.01 || m.EfficiencyFactor > wantEF+0.01 {
		t.Errorf("EfficiencyFactor = %f, want %f", m.EfficiencyFactor, wantEF)
	}
}

func TestCompute_DecouplingZeroWhenInsufficientRecords(t *testing.T) {
	records := makeRecords(30, 140, 180, 80)
	p := &fitpkg.ParsedFIT{Records: records}
	m := Compute(p, testZones)

	if m.DecouplingPct != 0 {
		t.Errorf("DecouplingPct = %f, want 0 for < 60 records", m.DecouplingPct)
	}
}

func TestCompute_DecouplingDetectsDrift(t *testing.T) {
	// First half: 200W at 130 bpm → EF = 1.538
	// Second half: 200W at 150 bpm → EF = 1.333
	// Decoupling = (1.538-1.333)/1.538 * 100 ≈ 13.3%
	first := makeRecords(60, 130, 200, 80)
	second := makeRecords(60, 150, 200, 80)
	records := append(first, second...)

	p := &fitpkg.ParsedFIT{Records: records}
	m := Compute(p, testZones)

	if m.DecouplingPct < 10 || m.DecouplingPct > 20 {
		t.Errorf("DecouplingPct = %f, want ~13 (drift detected)", m.DecouplingPct)
	}
	if m.HRDriftPct <= 0 {
		t.Errorf("HRDriftPct = %f, want > 0 (HR rose in second half)", m.HRDriftPct)
	}
}

func TestHRZoneIndex(t *testing.T) {
	tests := []struct {
		hr   int
		want int
	}{
		{100, 0}, // Z1
		{110, 0}, // Z1 boundary
		{111, 1}, // Z2
		{130, 1}, // Z2 boundary
		{131, 2}, // Z3
		{150, 2}, // Z3 boundary
		{151, 3}, // Z4
		{165, 3}, // Z4 boundary
		{166, 4}, // Z5
		{190, 4}, // Z5 (above max)
	}
	for _, tt := range tests {
		got := hrZoneIndex(tt.hr, testZones)
		if got != tt.want {
			t.Errorf("hrZoneIndex(%d) = %d, want %d", tt.hr, got, tt.want)
		}
	}
}

func TestPwrZoneIndex(t *testing.T) {
	tests := []struct {
		pwr  int
		want int
	}{
		{50, 0}, {110, 0},
		{111, 1}, {160, 1},
		{161, 2}, {180, 2},
		{181, 3}, {200, 3},
		{201, 4}, {300, 4},
	}
	for _, tt := range tests {
		got := pwrZoneIndex(tt.pwr, testZones)
		if got != tt.want {
			t.Errorf("pwrZoneIndex(%d) = %d, want %d", tt.pwr, got, tt.want)
		}
	}
}

func TestToStorageMetrics_NilForZeros(t *testing.T) {
	m := ComputedMetrics{} // all zeros
	sm := m.ToStorageMetrics(42)

	if sm.WorkoutID != 42 {
		t.Errorf("WorkoutID = %d, want 42", sm.WorkoutID)
	}
	if sm.AvgHR != nil {
		t.Errorf("AvgHR should be nil when zero")
	}
	if sm.TSS != nil {
		t.Errorf("TSS should be nil when zero")
	}
}

func TestToStorageMetrics_NonNilForNonZeros(t *testing.T) {
	m := ComputedMetrics{AvgHR: 150, TSS: 75.5}
	sm := m.ToStorageMetrics(1)

	if sm.AvgHR == nil || *sm.AvgHR != 150 {
		t.Errorf("AvgHR = %v, want 150", sm.AvgHR)
	}
	if sm.TSS == nil || *sm.TSS != 75.5 {
		t.Errorf("TSS = %v, want 75.5", sm.TSS)
	}
}

// --- Zone timeline tests ---

func TestComputeZoneTimeline_SteadyZ2(t *testing.T) {
	// 300 seconds at 150W = all Z2 (PwrZ2Max=160) → single segment.
	records := makeRecords(300, 130, 150, 80)
	segs := ComputeZoneTimeline(records, testZones, 60, 0)

	if len(segs) != 1 {
		t.Fatalf("got %d segments, want 1", len(segs))
	}
	if segs[0].Zone != 2 {
		t.Errorf("zone = %d, want 2", segs[0].Zone)
	}
	if segs[0].DurationMin < 4.9 || segs[0].DurationMin > 5.1 {
		t.Errorf("duration = %.1f min, want ~5.0", segs[0].DurationMin)
	}
}

func TestComputeZoneTimeline_Intervals(t *testing.T) {
	// Simulate 2x120s Z4 intervals with 120s Z2 recovery between and around.
	// Z2 warmup (120s) → Z4 (120s) → Z2 recovery (120s) → Z4 (120s) → Z2 cooldown (120s)
	var records []fitpkg.Record
	base := time.Now()
	addBlock := func(dur int, power uint16) {
		offset := len(records)
		for i := 0; i < dur; i++ {
			records = append(records, fitpkg.Record{
				Timestamp: base.Add(time.Duration(offset+i) * time.Second),
				HeartRate: 140,
				Power:     power,
				Cadence:   80,
			})
		}
	}
	addBlock(120, 150) // Z2 warmup
	addBlock(120, 195) // Z4 interval (PwrZ3Max=180, PwrZ4Max=200 → 195 is Z4)
	addBlock(120, 150) // Z2 recovery
	addBlock(120, 195) // Z4 interval
	addBlock(120, 150) // Z2 cooldown

	segs := ComputeZoneTimeline(records, testZones, 60, 0)

	if len(segs) != 5 {
		t.Fatalf("got %d segments, want 5; segs: %+v", len(segs), segs)
	}

	// Check zone alternation.
	wantZones := []int{2, 4, 2, 4, 2}
	for i, s := range segs {
		if s.Zone != wantZones[i] {
			t.Errorf("seg[%d].Zone = %d, want %d", i, s.Zone, wantZones[i])
		}
	}
}

func TestComputeZoneTimeline_ShortSpikeMerged(t *testing.T) {
	// 180s Z2, 30s Z4 spike (< 60s minimum), 180s Z2 → should merge to single Z2.
	var records []fitpkg.Record
	base := time.Now()
	addBlock := func(dur int, power uint16) {
		offset := len(records)
		for i := 0; i < dur; i++ {
			records = append(records, fitpkg.Record{
				Timestamp: base.Add(time.Duration(offset+i) * time.Second),
				HeartRate: 140,
				Power:     power,
				Cadence:   80,
			})
		}
	}
	addBlock(180, 150) // Z2
	addBlock(30, 195)  // Z4 spike — too short, should be absorbed
	addBlock(180, 150) // Z2

	segs := ComputeZoneTimeline(records, testZones, 60, 0)

	if len(segs) != 1 {
		t.Fatalf("got %d segments, want 1 (spike should be merged); segs: %+v", len(segs), segs)
	}
	if segs[0].Zone != 2 {
		t.Errorf("zone = %d, want 2", segs[0].Zone)
	}
}

func TestComputeZoneTimeline_TooFewRecords(t *testing.T) {
	records := makeRecords(30, 130, 150, 80) // less than minSegmentSec
	segs := ComputeZoneTimeline(records, testZones, 60, 0)
	if segs != nil {
		t.Errorf("got %d segments, want nil for too few records", len(segs))
	}
}

func TestZoneTimelineJSON_RoundTrip(t *testing.T) {
	records := makeRecords(300, 130, 150, 80)
	jsonStr := ZoneTimelineJSON(records, testZones)
	if jsonStr == "" {
		t.Fatal("got empty JSON, want non-empty")
	}

	var segs []ZoneSegment
	if err := json.Unmarshal([]byte(jsonStr), &segs); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(segs) != 1 {
		t.Errorf("got %d segments from JSON, want 1", len(segs))
	}
}

func TestComputeHRZoneTimeline_SteadyZ2(t *testing.T) {
	// 300 seconds at 130 bpm = all Z2 (HRZ2Max=140) -> single segment.
	records := makeRecords(300, 130, 150, 80)
	segs := ComputeHRZoneTimeline(records, testZones, 60)

	if len(segs) != 1 {
		t.Fatalf("got %d segments, want 1", len(segs))
	}
	if segs[0].Zone != 2 {
		t.Errorf("zone = %d, want 2", segs[0].Zone)
	}
	if segs[0].DurationMin < 4.9 || segs[0].DurationMin > 5.1 {
		t.Errorf("duration = %.1f min, want ~5.0", segs[0].DurationMin)
	}
}

// TestComputeZoneTimeline_SmoothedBreaksCascade verifies that the 30-second rolling
// average prevents the cascade-collapse that occurs when every raw power segment is
// shorter than minSegmentSec.
//
// The ride is built from 5-second blocks that alternate between two zone-boundary-
// crossing power values, so every raw segment is 5 s (well below the 60 s minimum).
// Without smoothing, the merge loop absorbs them all into the first segment's zone.
// With 30s smoothing the rolling average reveals the dominant zone in each block.
//
// testZones: PwrZ1Max=110, PwrZ2Max=160, PwrZ3Max=180, PwrZ4Max=200
func TestComputeZoneTimeline_SmoothedBreaksCascade(t *testing.T) {
	var records []fitpkg.Record
	base := time.Now()
	// Alternate between loW and hiW every 5 seconds.
	addAlternating := func(totalSec int, loW, hiW uint16) {
		offset := len(records)
		for i := 0; i < totalSec; i++ {
			p := loW
			if (i/5)%2 == 1 {
				p = hiW
			}
			records = append(records, fitpkg.Record{
				Timestamp: base.Add(time.Duration(offset+i) * time.Second),
				HeartRate: 140,
				Power:     p,
				Cadence:   80,
			})
		}
	}
	// 300 s Z1↔Z2 (108/150 W): raw segs are Z1(5s)/Z2(5s); 30s avg = 129 W → Z2.
	addAlternating(300, 108, 150)
	// 120 s Z2↔Z4 (150/195 W): raw segs are Z2(5s)/Z4(5s); 30s avg = 172 W → Z3.
	addAlternating(120, 150, 195)
	// 300 s Z1↔Z2 again.
	addAlternating(300, 108, 150)

	// Without smoothing: first raw segment is 5 s < 60 s; every subsequent short
	// segment is absorbed into it, collapsing the whole ride to one zone.
	unsmoothed := ComputeZoneTimeline(records, testZones, 60, 0)
	if len(unsmoothed) != 1 {
		t.Fatalf("unsmoothed: got %d segments, want 1 (cascade expected)", len(unsmoothed))
	}

	// With smoothing: at least the Z2 and Z3 blocks should emerge as distinct
	// segments, and the max zone must be above Z1.
	smoothed := ComputeZoneTimeline(records, testZones, 60, 30)
	if len(smoothed) < 2 {
		t.Fatalf("smoothed: got %d segments, want ≥2; segs: %+v", len(smoothed), smoothed)
	}
	maxZone := 0
	for _, s := range smoothed {
		if s.Zone > maxZone {
			maxZone = s.Zone
		}
	}
	if maxZone < 2 {
		t.Errorf("smoothed: max zone = %d, want ≥2; segs: %+v", maxZone, smoothed)
	}
}

func TestHRZoneTimelineJSON_RoundTrip(t *testing.T) {
	records := makeRecords(300, 130, 150, 80)
	jsonStr := HRZoneTimelineJSON(records, testZones)
	if jsonStr == "" {
		t.Fatal("got empty JSON, want non-empty")
	}

	var segs []HRZoneSegment
	if err := json.Unmarshal([]byte(jsonStr), &segs); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(segs) != 1 {
		t.Errorf("got %d segments from JSON, want 1", len(segs))
	}
}

func TestComputeCadenceDistribution(t *testing.T) {
	var records []fitpkg.Record
	base := time.Now()
	for i := 0; i < 60; i++ {
		records = append(records, fitpkg.Record{Timestamp: base.Add(time.Duration(i) * time.Second), HeartRate: 130, Power: 150, Cadence: 60})
	}
	for i := 60; i < 120; i++ {
		records = append(records, fitpkg.Record{Timestamp: base.Add(time.Duration(i) * time.Second), HeartRate: 130, Power: 150, Cadence: 75})
	}
	for i := 120; i < 180; i++ {
		records = append(records, fitpkg.Record{Timestamp: base.Add(time.Duration(i) * time.Second), HeartRate: 130, Power: 150, Cadence: 90})
	}
	for i := 180; i < 240; i++ {
		records = append(records, fitpkg.Record{Timestamp: base.Add(time.Duration(i) * time.Second), HeartRate: 130, Power: 150, Cadence: 105})
	}

	m := Compute(&fitpkg.ParsedFIT{
		Session: fitpkg.Session{DurationSec: 240},
		Records: records,
	}, testZones)

	if math.Abs(m.CadLT70Pct-25) > 0.1 {
		t.Errorf("CadLT70Pct = %.1f, want 25", m.CadLT70Pct)
	}
	if math.Abs(m.Cad70To85Pct-25) > 0.1 {
		t.Errorf("Cad70To85Pct = %.1f, want 25", m.Cad70To85Pct)
	}
	if math.Abs(m.Cad85To100Pct-25) > 0.1 {
		t.Errorf("Cad85To100Pct = %.1f, want 25", m.Cad85To100Pct)
	}
	if math.Abs(m.CadGE100Pct-25) > 0.1 {
		t.Errorf("CadGE100Pct = %.1f, want 25", m.CadGE100Pct)
	}
}

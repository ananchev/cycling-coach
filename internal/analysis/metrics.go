package analysis

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"

	fitpkg "cycling-coach/internal/fit"
	"cycling-coach/internal/storage"
)

// ZoneConfig holds training zone boundaries loaded from athlete_config.
type ZoneConfig struct {
	FTPWatts int
	HRMax    int
	// HR zone upper bounds: Z1 ≤ Z1Max, Z2: Z1Max < HR ≤ Z2Max, ..., Z5: HR > Z4Max
	HRZ1Max, HRZ2Max, HRZ3Max, HRZ4Max int
	// Power zone upper bounds (same logic)
	PwrZ1Max, PwrZ2Max, PwrZ3Max, PwrZ4Max int
}

// DefaultZoneConfig returns the developer-time defaults for local testing.
// In production these are always overridden by values in athlete_config.
var DefaultZoneConfig = ZoneConfig{
	FTPWatts: 251,
	HRMax:    184,
	HRZ1Max:  109, HRZ2Max: 127, HRZ3Max: 145, HRZ4Max: 164,
	PwrZ1Max: 138, PwrZ2Max: 188, PwrZ3Max: 226, PwrZ4Max: 263,
}

// LoadZoneConfig reads zone boundaries from athlete_config in the DB.
// If a key is missing it falls back to DefaultZoneConfig for that field.
func LoadZoneConfig(db *sql.DB) (ZoneConfig, error) {
	cfg, err := storage.GetAllConfig(db)
	if err != nil {
		return ZoneConfig{}, fmt.Errorf("analysis.LoadZoneConfig: %w", err)
	}
	z := DefaultZoneConfig
	readInt := func(key string, dst *int) {
		if v, ok := cfg[key]; ok {
			if n, err := strconv.Atoi(v); err == nil {
				*dst = n
			}
		}
	}
	readInt("ftp_watts", &z.FTPWatts)
	readInt("hr_max", &z.HRMax)
	readInt("hr_z1_max", &z.HRZ1Max)
	readInt("hr_z2_max", &z.HRZ2Max)
	readInt("hr_z3_max", &z.HRZ3Max)
	readInt("hr_z4_max", &z.HRZ4Max)
	readInt("pwr_z1_max", &z.PwrZ1Max)
	readInt("pwr_z2_max", &z.PwrZ2Max)
	readInt("pwr_z3_max", &z.PwrZ3Max)
	readInt("pwr_z4_max", &z.PwrZ4Max)
	return z, nil
}

// ZoneSegment represents a contiguous block of time spent in a single power zone.
type ZoneSegment struct {
	Zone        int     `json:"zone"`      // 1–5
	StartMin    float64 `json:"start_min"` // minutes from ride start
	DurationMin float64 `json:"duration_min"`
	AvgPower    float64 `json:"avg_power"` // average power in this segment
}

// HRZoneSegment represents a contiguous block of time spent in a single HR zone.
type HRZoneSegment struct {
	Zone        int     `json:"zone"`      // 1–5
	StartMin    float64 `json:"start_min"` // minutes from ride start
	DurationMin float64 `json:"duration_min"`
	AvgHR       float64 `json:"avg_hr"` // average HR in this segment
}

// ComputeZoneTimeline derives contiguous power zone segments from per-second records.
// smoothingWindowSec, if > 0, applies a centred rolling average over that many seconds
// before zone classification, eliminating 1–2 second power spikes from real power meters.
// Segments shorter than minSegmentSec are merged into the surrounding segment.
// Elapsed time is derived from actual record timestamps, so coasting gaps are preserved.
func ComputeZoneTimeline(records []fitpkg.Record, z ZoneConfig, minSegmentSec int, smoothingWindowSec int) []ZoneSegment {
	if len(records) == 0 {
		return nil
	}

	// Extract valid power records, keeping the original timestamp for each.
	type validRec struct {
		ts  time.Time
		pwr float64
	}
	valid := make([]validRec, 0, len(records))
	for _, r := range records {
		if !r.ValidPower() {
			continue
		}
		valid = append(valid, validRec{r.Timestamp, float64(r.Power)})
	}
	if len(valid) < minSegmentSec {
		return nil
	}

	// Determine the zone for each record, optionally via a centred rolling average.
	zoneFor := make([]int, len(valid))
	if smoothingWindowSec > 0 {
		half := smoothingWindowSec / 2
		for i := range valid {
			lo, hi := i-half, i+half+1
			if lo < 0 {
				lo = 0
			}
			if hi > len(valid) {
				hi = len(valid)
			}
			var sum float64
			for j := lo; j < hi; j++ {
				sum += valid[j].pwr
			}
			zoneFor[i] = pwrZoneIndex(int(sum/float64(hi-lo)), z) + 1
		}
	} else {
		for i, r := range valid {
			zoneFor[i] = pwrZoneIndex(int(r.pwr), z) + 1
		}
	}

	// Build initial segments from consecutive same-zone records.
	type rawSeg struct {
		zone     int
		startIdx int
		endIdx   int // exclusive
		pwrSum   float64
	}
	segs := []rawSeg{{zone: zoneFor[0], startIdx: 0, endIdx: 1, pwrSum: valid[0].pwr}}
	for i := 1; i < len(valid); i++ {
		cur := &segs[len(segs)-1]
		if zoneFor[i] == cur.zone {
			cur.endIdx = i + 1
			cur.pwrSum += valid[i].pwr
		} else {
			segs = append(segs, rawSeg{zone: zoneFor[i], startIdx: i, endIdx: i + 1, pwrSum: valid[i].pwr})
		}
	}

	// Merge short segments into neighbours. Repeat until stable.
	for changed := true; changed; {
		changed = false
		merged := make([]rawSeg, 0, len(segs))
		for _, s := range segs {
			dur := s.endIdx - s.startIdx
			if dur < minSegmentSec && len(merged) > 0 {
				merged[len(merged)-1].endIdx = s.endIdx
				merged[len(merged)-1].pwrSum += s.pwrSum
				changed = true
			} else {
				if len(merged) > 0 && merged[len(merged)-1].zone == s.zone {
					merged[len(merged)-1].endIdx = s.endIdx
					merged[len(merged)-1].pwrSum += s.pwrSum
					changed = true
				} else {
					merged = append(merged, s)
				}
			}
		}
		segs = merged
	}

	// Convert to output format using actual timestamps for elapsed time.
	startRef := valid[0].ts
	out := make([]ZoneSegment, 0, len(segs))
	for _, s := range segs {
		n := float64(s.endIdx - s.startIdx)
		startMin := valid[s.startIdx].ts.Sub(startRef).Minutes()
		// Add 1 second for the last record so the segment covers its full second.
		endMin := valid[s.endIdx-1].ts.Sub(startRef).Minutes() + 1.0/60.0
		out = append(out, ZoneSegment{
			Zone:        s.zone,
			StartMin:    startMin,
			DurationMin: endMin - startMin,
			AvgPower:    s.pwrSum / n,
		})
	}
	return out
}

// ComputeHRZoneTimeline derives contiguous HR zone segments from per-second records.
// Segments shorter than minSegmentSec are merged into the surrounding segment.
// Elapsed time is derived from actual record timestamps.
func ComputeHRZoneTimeline(records []fitpkg.Record, z ZoneConfig, minSegmentSec int) []HRZoneSegment {
	if len(records) == 0 {
		return nil
	}

	type validRec struct {
		ts time.Time
		hr float64
	}
	valid := make([]validRec, 0, len(records))
	for _, r := range records {
		if !r.ValidHR() {
			continue
		}
		valid = append(valid, validRec{r.Timestamp, float64(r.HeartRate)})
	}
	if len(valid) < minSegmentSec {
		return nil
	}

	type rawSeg struct {
		zone     int
		startIdx int
		endIdx   int // exclusive
		hrSum    float64
	}
	firstZone := hrZoneIndex(int(valid[0].hr), z) + 1
	segs := []rawSeg{{zone: firstZone, startIdx: 0, endIdx: 1, hrSum: valid[0].hr}}
	for i := 1; i < len(valid); i++ {
		zi := hrZoneIndex(int(valid[i].hr), z) + 1
		cur := &segs[len(segs)-1]
		if zi == cur.zone {
			cur.endIdx = i + 1
			cur.hrSum += valid[i].hr
		} else {
			segs = append(segs, rawSeg{zone: zi, startIdx: i, endIdx: i + 1, hrSum: valid[i].hr})
		}
	}

	for changed := true; changed; {
		changed = false
		merged := make([]rawSeg, 0, len(segs))
		for _, s := range segs {
			dur := s.endIdx - s.startIdx
			if dur < minSegmentSec && len(merged) > 0 {
				merged[len(merged)-1].endIdx = s.endIdx
				merged[len(merged)-1].hrSum += s.hrSum
				changed = true
			} else {
				if len(merged) > 0 && merged[len(merged)-1].zone == s.zone {
					merged[len(merged)-1].endIdx = s.endIdx
					merged[len(merged)-1].hrSum += s.hrSum
					changed = true
				} else {
					merged = append(merged, s)
				}
			}
		}
		segs = merged
	}

	startRef := valid[0].ts
	out := make([]HRZoneSegment, 0, len(segs))
	for _, s := range segs {
		n := float64(s.endIdx - s.startIdx)
		startMin := valid[s.startIdx].ts.Sub(startRef).Minutes()
		endMin := valid[s.endIdx-1].ts.Sub(startRef).Minutes() + 1.0/60.0
		out = append(out, HRZoneSegment{
			Zone:        s.zone,
			StartMin:    startMin,
			DurationMin: endMin - startMin,
			AvgHR:       s.hrSum / n,
		})
	}
	return out
}

// ZoneTimelineJSON computes the zone timeline and returns it as a JSON string.
// A 30-second centred rolling average is applied before zone classification to
// eliminate per-pedal-stroke noise from real power meters.
// Returns empty string if no timeline could be computed.
func ZoneTimelineJSON(records []fitpkg.Record, z ZoneConfig) string {
	segs := ComputeZoneTimeline(records, z, 60, 30)
	if len(segs) == 0 {
		return ""
	}
	b, err := json.Marshal(segs)
	if err != nil {
		return ""
	}
	return string(b)
}

// HRZoneTimelineJSON computes the HR zone timeline and returns it as a JSON string.
// Returns empty string if no timeline could be computed.
func HRZoneTimelineJSON(records []fitpkg.Record, z ZoneConfig) string {
	segs := ComputeHRZoneTimeline(records, z, 60)
	if len(segs) == 0 {
		return ""
	}
	b, err := json.Marshal(segs)
	if err != nil {
		return ""
	}
	return string(b)
}

// ComputedMetrics holds per-ride derived metrics.
type ComputedMetrics struct {
	DurationMin                                          float64
	AvgHR                                                float64
	MaxHR                                                float64
	AvgPower                                             float64
	MaxPower                                             float64
	AvgCadence                                           float64
	NormalizedPower                                      float64
	IntensityFactor                                      float64
	TSS                                                  float64
	TRIMP                                                float64
	EfficiencyFactor                                     float64
	HRDriftPct                                           float64
	DecouplingPct                                        float64
	HRZ1Pct, HRZ2Pct, HRZ3Pct, HRZ4Pct, HRZ5Pct          float64
	PwrZ1Pct, PwrZ2Pct, PwrZ3Pct, PwrZ4Pct, PwrZ5Pct     float64
	CadLT70Pct, Cad70To85Pct, Cad85To100Pct, CadGE100Pct float64
	VariabilityIndex                                     float64 // NP / AvgPower; 0 when not computable
	ZoneTimeline                                         string  // JSON array of ZoneSegment
	HRZoneTimeline                                       string  // JSON array of HRZoneSegment
}

const hrRest = 50 // assumed resting HR for TRIMP; no config key yet

// Compute derives all ride metrics from parsed FIT data and zone configuration.
// session is used for overall duration and session-level summaries;
// records provide the per-second stream used for zone distributions, NP, TRIMP, and HR drift.
func Compute(p *fitpkg.ParsedFIT, z ZoneConfig) ComputedMetrics {
	m := ComputedMetrics{}

	durationSec := p.Session.DurationSec
	if durationSec > 0 {
		m.DurationMin = durationSec / 60.0
	}

	// Per-record aggregates.
	var (
		hrSum, hrN      float64
		pwrSum, pwrN    float64
		cadSum, cadN    float64
		maxHR, maxPower float64
		hrZone          [5]int
		pwrZone         [5]int
		cadBands        [4]int
		totalHRRecs     int
		totalPwrRecs    int
		totalCadRecs    int
		trimp           float64
	)

	for _, r := range p.Records {
		if r.ValidHR() {
			hr := float64(r.HeartRate)
			hrSum += hr
			hrN++
			if hr > maxHR {
				maxHR = hr
			}
			hrZone[hrZoneIndex(int(r.HeartRate), z)]++
			totalHRRecs++

			// Banister TRIMP: dt=1s=1/60min, HRr clamped [0,1].
			if z.HRMax > hrRest {
				hrr := (hr - hrRest) / float64(z.HRMax-hrRest)
				if hrr < 0 {
					hrr = 0
				}
				if hrr > 1 {
					hrr = 1
				}
				trimp += (1.0 / 60.0) * hrr * math.Exp(1.92*hrr)
			}
		}
		if r.ValidPower() {
			pwr := float64(r.Power)
			pwrSum += pwr
			pwrN++
			if pwr > maxPower {
				maxPower = pwr
			}
			pwrZone[pwrZoneIndex(int(r.Power), z)]++
			totalPwrRecs++
		}
		if r.ValidCadence() {
			cad := float64(r.Cadence)
			cadSum += cad
			cadN++
			cadBands[cadenceBandIndex(int(r.Cadence))]++
			totalCadRecs++
		}
	}

	if hrN > 0 {
		m.AvgHR = hrSum / hrN
		m.MaxHR = maxHR
	}
	if pwrN > 0 {
		m.AvgPower = pwrSum / pwrN
		m.MaxPower = maxPower
	}
	if cadN > 0 {
		m.AvgCadence = cadSum / cadN
	}

	// Zone distribution as percentages.
	if totalHRRecs > 0 {
		n := float64(totalHRRecs)
		m.HRZ1Pct = float64(hrZone[0]) / n * 100
		m.HRZ2Pct = float64(hrZone[1]) / n * 100
		m.HRZ3Pct = float64(hrZone[2]) / n * 100
		m.HRZ4Pct = float64(hrZone[3]) / n * 100
		m.HRZ5Pct = float64(hrZone[4]) / n * 100
	}
	if totalPwrRecs > 0 {
		n := float64(totalPwrRecs)
		m.PwrZ1Pct = float64(pwrZone[0]) / n * 100
		m.PwrZ2Pct = float64(pwrZone[1]) / n * 100
		m.PwrZ3Pct = float64(pwrZone[2]) / n * 100
		m.PwrZ4Pct = float64(pwrZone[3]) / n * 100
		m.PwrZ5Pct = float64(pwrZone[4]) / n * 100
	}
	if totalCadRecs > 0 {
		n := float64(totalCadRecs)
		m.CadLT70Pct = float64(cadBands[0]) / n * 100
		m.Cad70To85Pct = float64(cadBands[1]) / n * 100
		m.Cad85To100Pct = float64(cadBands[2]) / n * 100
		m.CadGE100Pct = float64(cadBands[3]) / n * 100
	}

	m.TRIMP = trimp

	// Normalized Power (30-second rolling average algorithm).
	m.NormalizedPower = fitpkg.NormalizedPower(p.Records)

	// Intensity Factor and TSS.
	if z.FTPWatts > 0 && m.NormalizedPower > 0 {
		m.IntensityFactor = m.NormalizedPower / float64(z.FTPWatts)
		if durationSec > 0 {
			m.TSS = (durationSec * m.NormalizedPower * m.IntensityFactor) / (float64(z.FTPWatts) * 3600) * 100
		}
	}

	// Variability Index = NP / AvgPower (measures ride intermittency; >1.15 signals stop-go/technical terrain).
	if m.NormalizedPower > 0 && m.AvgPower > 0 {
		m.VariabilityIndex = m.NormalizedPower / m.AvgPower
	}

	// Efficiency Factor = AvgPower / AvgHR.
	if m.AvgHR > 0 && m.AvgPower > 0 {
		m.EfficiencyFactor = m.AvgPower / m.AvgHR
	}

	// HR Drift / Pa:HR Decoupling.
	m.HRDriftPct, m.DecouplingPct = computeDecoupling(p.Records)

	// Power/HR zone timelines.
	m.ZoneTimeline = ZoneTimelineJSON(p.Records, z)
	m.HRZoneTimeline = HRZoneTimelineJSON(p.Records, z)

	return m
}

// ToStorageMetrics converts ComputedMetrics to *storage.RideMetrics for persistence.
func (m ComputedMetrics) ToStorageMetrics(workoutID int64) *storage.RideMetrics {
	p := func(v float64) *float64 {
		if v == 0 {
			return nil
		}
		return &v
	}
	pStr := func(s string) *string {
		if s == "" {
			return nil
		}
		return &s
	}
	return &storage.RideMetrics{
		WorkoutID:        workoutID,
		DurationMin:      p(m.DurationMin),
		AvgHR:            p(m.AvgHR),
		MaxHR:            p(m.MaxHR),
		AvgPower:         p(m.AvgPower),
		MaxPower:         p(m.MaxPower),
		AvgCadence:       p(m.AvgCadence),
		NormalizedPower:  p(m.NormalizedPower),
		IntensityFactor:  p(m.IntensityFactor),
		TSS:              p(m.TSS),
		TRIMP:            p(m.TRIMP),
		EfficiencyFactor: p(m.EfficiencyFactor),
		HRDriftPct:       p(m.HRDriftPct),
		DecouplingPct:    p(m.DecouplingPct),
		HRZ1Pct:          p(m.HRZ1Pct),
		HRZ2Pct:          p(m.HRZ2Pct),
		HRZ3Pct:          p(m.HRZ3Pct),
		HRZ4Pct:          p(m.HRZ4Pct),
		HRZ5Pct:          p(m.HRZ5Pct),
		PwrZ1Pct:         p(m.PwrZ1Pct),
		PwrZ2Pct:         p(m.PwrZ2Pct),
		PwrZ3Pct:         p(m.PwrZ3Pct),
		PwrZ4Pct:         p(m.PwrZ4Pct),
		PwrZ5Pct:         p(m.PwrZ5Pct),
		CadLT70Pct:       p(m.CadLT70Pct),
		Cad70To85Pct:     p(m.Cad70To85Pct),
		Cad85To100Pct:    p(m.Cad85To100Pct),
		CadGE100Pct:      p(m.CadGE100Pct),
		VariabilityIndex: p(m.VariabilityIndex),
		ZoneTimeline:     pStr(m.ZoneTimeline),
		HRZoneTimeline:   pStr(m.HRZoneTimeline),
	}
}

// hrZoneIndex returns [0,4] for Z1–Z5 based on HR and zone config.
func hrZoneIndex(hr int, z ZoneConfig) int {
	switch {
	case hr <= z.HRZ1Max:
		return 0
	case hr <= z.HRZ2Max:
		return 1
	case hr <= z.HRZ3Max:
		return 2
	case hr <= z.HRZ4Max:
		return 3
	default:
		return 4
	}
}

// pwrZoneIndex returns [0,4] for Z1–Z5 based on power and zone config.
func pwrZoneIndex(pwr int, z ZoneConfig) int {
	switch {
	case pwr <= z.PwrZ1Max:
		return 0
	case pwr <= z.PwrZ2Max:
		return 1
	case pwr <= z.PwrZ3Max:
		return 2
	case pwr <= z.PwrZ4Max:
		return 3
	default:
		return 4
	}
}

func cadenceBandIndex(cad int) int {
	switch {
	case cad < 70:
		return 0
	case cad < 85:
		return 1
	case cad < 100:
		return 2
	default:
		return 3
	}
}

// computeDecoupling computes the Pa:HR decoupling (aerobic efficiency drift).
// Splits records into two halves; returns (hrDriftPct, decouplingPct).
// decouplingPct = (EF_first - EF_second) / EF_first * 100
// where EF = avgPower / avgHR.
// Returns (0, 0) if there is insufficient data.
func computeDecoupling(records []fitpkg.Record) (hrDriftPct, decouplingPct float64) {
	if len(records) < 60 { // need at least 60 data points for a meaningful split
		return 0, 0
	}
	mid := len(records) / 2
	first := records[:mid]
	second := records[mid:]

	ef1 := halfEF(first)
	ef2 := halfEF(second)

	if ef1 == 0 {
		return 0, 0
	}

	// Pa:HR decoupling: how much EF changed across the ride.
	dec := (ef1 - ef2) / ef1 * 100

	// HR drift: change in avg HR between halves relative to overall avg HR.
	hr1 := halfAvgHR(first)
	hr2 := halfAvgHR(second)
	if hr1 > 0 {
		hrDriftPct = (hr2 - hr1) / hr1 * 100
	}

	return hrDriftPct, dec
}

func halfEF(records []fitpkg.Record) float64 {
	var pwrSum, hrSum float64
	var n float64
	for _, r := range records {
		if r.ValidPower() && r.ValidHR() {
			pwrSum += float64(r.Power)
			hrSum += float64(r.HeartRate)
			n++
		}
	}
	if n == 0 || hrSum == 0 {
		return 0
	}
	return (pwrSum / n) / (hrSum / n)
}

func halfAvgHR(records []fitpkg.Record) float64 {
	var sum float64
	var n float64
	for _, r := range records {
		if r.ValidHR() {
			sum += float64(r.HeartRate)
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / n
}

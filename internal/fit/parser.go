package fit

import (
	"fmt"
	"io"
	"math"
	"os"
	"time"

	"github.com/muktihari/fit/decoder"
	"github.com/muktihari/fit/profile/basetype"
	"github.com/muktihari/fit/profile/filedef"
	"github.com/muktihari/fit/profile/mesgdef"
)

// Record represents one data point from the FIT file (typically 1 Hz).
type Record struct {
	Timestamp time.Time
	HeartRate uint8   // bpm; 0 means not recorded
	Power     uint16  // watts; 0 means not recorded
	Cadence   uint8   // rpm; 0 means not recorded
	DistanceM float64 // cumulative meters
	SpeedMS   float64 // m/s
}

// Session holds the FIT session-level summary.
type Session struct {
	StartTime       time.Time
	DurationSec     float64 // total elapsed time (includes pauses)
	TimerSec        float64 // active timer time (excludes pauses)
	DistanceM       float64
	Calories        uint16
	AvgHR           uint8
	MaxHR           uint8
	AvgPower        uint16
	MaxPower        uint16
	NormalizedPower uint16
	AvgCadence      uint8
	AvgSpeedMS      float64
}

// ParsedFIT is the result of parsing a FIT activity file.
type ParsedFIT struct {
	Session Session
	Records []Record
}

// ParseFile opens the file at path and parses it as a FIT activity.
func ParseFile(path string) (*ParsedFIT, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("fit.ParseFile: open %q: %w", path, err)
	}
	defer f.Close()
	return Parse(f)
}

// Parse reads a FIT activity from r.
func Parse(r io.Reader) (*ParsedFIT, error) {
	lis := filedef.NewListener()
	defer lis.Close()

	dec := decoder.New(r, decoder.WithMesgListener(lis))
	if _, err := dec.Decode(); err != nil {
		return nil, fmt.Errorf("fit.Parse: decode: %w", err)
	}

	act, ok := lis.File().(*filedef.Activity)
	if !ok {
		return nil, fmt.Errorf("fit.Parse: not an activity FIT file")
	}

	return buildParsedFIT(act), nil
}

func buildParsedFIT(act *filedef.Activity) *ParsedFIT {
	out := &ParsedFIT{}

	if len(act.Sessions) > 0 {
		out.Session = convertSession(act.Sessions[0])
	}

	out.Records = make([]Record, 0, len(act.Records))
	for _, r := range act.Records {
		out.Records = append(out.Records, convertRecord(r))
	}

	return out
}

func convertSession(s *mesgdef.Session) Session {
	sess := Session{
		StartTime: s.StartTime,
	}

	if s.TotalElapsedTime != basetype.Uint32Invalid {
		sess.DurationSec = float64(s.TotalElapsedTime) / 1000.0
	}
	if s.TotalTimerTime != basetype.Uint32Invalid {
		sess.TimerSec = float64(s.TotalTimerTime) / 1000.0
	}
	if s.TotalDistance != basetype.Uint32Invalid {
		sess.DistanceM = float64(s.TotalDistance) / 100.0
	}
	if s.TotalCalories != basetype.Uint16Invalid {
		sess.Calories = s.TotalCalories
	}
	if s.AvgHeartRate != basetype.Uint8Invalid {
		sess.AvgHR = s.AvgHeartRate
	}
	if s.MaxHeartRate != basetype.Uint8Invalid {
		sess.MaxHR = s.MaxHeartRate
	}
	if s.AvgPower != basetype.Uint16Invalid {
		sess.AvgPower = s.AvgPower
	}
	if s.MaxPower != basetype.Uint16Invalid {
		sess.MaxPower = s.MaxPower
	}
	if s.NormalizedPower != basetype.Uint16Invalid {
		sess.NormalizedPower = s.NormalizedPower
	}
	if s.AvgCadence != basetype.Uint8Invalid {
		sess.AvgCadence = s.AvgCadence
	}
	// Prefer EnhancedAvgSpeed (higher precision) over AvgSpeed.
	if s.EnhancedAvgSpeed != basetype.Uint32Invalid && s.EnhancedAvgSpeed != 0 {
		sess.AvgSpeedMS = float64(s.EnhancedAvgSpeed) / 1000.0
	} else if s.AvgSpeed != basetype.Uint16Invalid {
		sess.AvgSpeedMS = float64(s.AvgSpeed) / 1000.0
	}

	return sess
}

func convertRecord(r *mesgdef.Record) Record {
	rec := Record{Timestamp: r.Timestamp}

	if r.HeartRate != basetype.Uint8Invalid {
		rec.HeartRate = r.HeartRate
	}
	if r.Power != basetype.Uint16Invalid {
		rec.Power = r.Power
	}
	if r.Cadence != basetype.Uint8Invalid {
		rec.Cadence = r.Cadence
	}
	if r.Distance != basetype.Uint32Invalid {
		rec.DistanceM = float64(r.Distance) / 100.0
	}
	// Prefer EnhancedSpeed over Speed.
	if r.EnhancedSpeed != basetype.Uint32Invalid && r.EnhancedSpeed != 0 {
		rec.SpeedMS = float64(r.EnhancedSpeed) / 1000.0
	} else if r.Speed != basetype.Uint16Invalid {
		rec.SpeedMS = float64(r.Speed) / 1000.0
	}

	return rec
}

// ValidPower returns true if the record has a meaningful power reading.
func (r Record) ValidPower() bool {
	return r.Power != 0 && r.Power != basetype.Uint16Invalid
}

// ValidHR returns true if the record has a meaningful HR reading.
func (r Record) ValidHR() bool {
	return r.HeartRate != 0 && r.HeartRate != basetype.Uint8Invalid
}

// ValidCadence returns true if the record has a meaningful cadence reading.
func (r Record) ValidCadence() bool {
	return r.Cadence != 0 && r.Cadence != basetype.Uint8Invalid
}

// FilterRecords returns only records for which keep returns true.
func FilterRecords(records []Record, keep func(Record) bool) []Record {
	out := make([]Record, 0, len(records))
	for _, r := range records {
		if keep(r) {
			out = append(out, r)
		}
	}
	return out
}

// MeanPower computes the mean of non-zero power values across records.
// Returns 0 if no valid power records exist.
func MeanPower(records []Record) float64 {
	var sum float64
	var n int
	for _, r := range records {
		if r.ValidPower() {
			sum += float64(r.Power)
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

// NormalizedPower computes the 30-second rolling average NP from records.
// Uses the standard algorithm: 30s rolling avg → raise to 4th power → mean → 4th root.
// Returns 0 if fewer than 30 valid power records exist.
func NormalizedPower(records []Record) float64 {
	powers := make([]float64, 0, len(records))
	for _, r := range records {
		if r.ValidPower() {
			powers = append(powers, float64(r.Power))
		}
	}
	if len(powers) < 30 {
		return 0
	}

	// 30-second rolling averages.
	rollAvgs := make([]float64, len(powers)-29)
	for i := range rollAvgs {
		var sum float64
		for j := i; j < i+30; j++ {
			sum += powers[j]
		}
		rollAvgs[i] = sum / 30.0
	}

	// Mean of fourth powers.
	var sumPow4 float64
	for _, v := range rollAvgs {
		sumPow4 += v * v * v * v
	}
	mean4 := sumPow4 / float64(len(rollAvgs))
	return math.Pow(mean4, 0.25)
}

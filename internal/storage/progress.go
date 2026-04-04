package storage

import (
	"database/sql"
	"fmt"
	"math"
	"time"
)

const progressTrendThreshold = 0.02

type ProgressTrend string

const (
	ProgressTrendUp     ProgressTrend = "up"
	ProgressTrendDown   ProgressTrend = "down"
	ProgressTrendSteady ProgressTrend = "steady"
)

type ProgressRange struct {
	From time.Time
	To   time.Time
	Days int
}

type ProgressMetric struct {
	Current  *float64
	Prior    *float64
	Delta    *float64
	DeltaPct *float64
	Trend    ProgressTrend
}

type ProgressSnapshot struct {
	SelectedRange         ProgressRange
	PriorRange            ProgressRange
	AerobicOnlyEF         bool
	AerobicEfficiency     ProgressMetric
	EnduranceDurability   ProgressMetric
	ActiveCalories        ProgressMetric
	CumulativeTSS         ProgressMetric
	CumulativeTRIMP       ProgressMetric
	AverageIF             ProgressMetric
	CompletionRate        ProgressMetric
	AverageWeightKG       ProgressMetric
	WeeklyLoad            []WeeklyLoadPoint
	PriorWeeklyLoad       []WeeklyLoadPoint
}

type WeeklyLoadPoint struct {
	WeekStart time.Time
	TSS       float64
	TRIMP     float64
}

type ProgressAnalysis struct {
	ID           int64
	PeriodFrom   time.Time
	PeriodTo     time.Time
	SnapshotJSON string
	SystemPrompt string
	UserPrompt   string
	Narrative    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// BuildProgressSnapshot returns selected-period KPIs, the immediately preceding
// equal-length prior-period KPIs, and weekly load sums within the selected
// period. Dates are treated as inclusive whole-day boundaries.
func BuildProgressSnapshot(db *sql.DB, from, to time.Time, aerobicOnlyEF bool) (*ProgressSnapshot, error) {
	selected, prior, err := progressRanges(from, to)
	if err != nil {
		return nil, fmt.Errorf("storage.BuildProgressSnapshot: %w", err)
	}

	selectedAgg, err := loadProgressAggregates(db, selected, aerobicOnlyEF)
	if err != nil {
		return nil, fmt.Errorf("storage.BuildProgressSnapshot: selected: %w", err)
	}
	priorAgg, err := loadProgressAggregates(db, prior, aerobicOnlyEF)
	if err != nil {
		return nil, fmt.Errorf("storage.BuildProgressSnapshot: prior: %w", err)
	}
	weekly, err := loadWeeklyLoad(db, selected)
	if err != nil {
		return nil, fmt.Errorf("storage.BuildProgressSnapshot: weekly load: %w", err)
	}
	priorWeekly, err := loadWeeklyLoad(db, prior)
	if err != nil {
		return nil, fmt.Errorf("storage.BuildProgressSnapshot: prior weekly load: %w", err)
	}

	return &ProgressSnapshot{
		SelectedRange:       selected,
		PriorRange:          prior,
		AerobicOnlyEF:       aerobicOnlyEF,
		AerobicEfficiency:   compareProgressValues(selectedAgg.avgEF, priorAgg.avgEF),
		EnduranceDurability: compareProgressValues(selectedAgg.avgDecoupling, priorAgg.avgDecoupling),
		ActiveCalories:      compareProgressValues(selectedAgg.activeCalories, priorAgg.activeCalories),
		CumulativeTSS:       compareProgressValues(selectedAgg.totalTSS, priorAgg.totalTSS),
		CumulativeTRIMP:     compareProgressValues(selectedAgg.totalTRIMP, priorAgg.totalTRIMP),
		AverageIF:           compareProgressValues(selectedAgg.avgIF, priorAgg.avgIF),
		CompletionRate:      compareProgressValues(selectedAgg.completionRate, priorAgg.completionRate),
		AverageWeightKG:     compareWeightValues(selectedAgg.avgWeight, priorAgg.avgWeight),
		WeeklyLoad:          weekly,
		PriorWeeklyLoad:     priorWeekly,
	}, nil
}

// UpsertProgressAnalysis persists the single saved progress interpretation row.
// Subsequent saves replace the prior content.
func UpsertProgressAnalysis(db *sql.DB, a *ProgressAnalysis) error {
	_, err := db.Exec(`
		INSERT INTO progress_analyses(id, period_from, period_to, snapshot_json, system_prompt, user_prompt, narrative_text, updated_at)
		VALUES(1, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET
			period_from=excluded.period_from,
			period_to=excluded.period_to,
			snapshot_json=excluded.snapshot_json,
			system_prompt=excluded.system_prompt,
			user_prompt=excluded.user_prompt,
			narrative_text=excluded.narrative_text,
			updated_at=CURRENT_TIMESTAMP`,
		a.PeriodFrom, a.PeriodTo, a.SnapshotJSON, a.SystemPrompt, a.UserPrompt, a.Narrative,
	)
	if err != nil {
		return fmt.Errorf("storage.UpsertProgressAnalysis: %w", err)
	}
	return nil
}

// GetProgressAnalysis returns the single saved progress interpretation row.
func GetProgressAnalysis(db *sql.DB) (*ProgressAnalysis, error) {
	var a ProgressAnalysis
	err := db.QueryRow(`
		SELECT id, period_from, period_to, snapshot_json, system_prompt, user_prompt, narrative_text, created_at, updated_at
		FROM progress_analyses
		WHERE id = 1`,
	).Scan(&a.ID, &a.PeriodFrom, &a.PeriodTo, &a.SnapshotJSON, &a.SystemPrompt, &a.UserPrompt, &a.Narrative, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("storage.GetProgressAnalysis: %w", err)
	}
	return &a, nil
}

type progressAggregates struct {
	avgEF          *float64
	avgDecoupling  *float64
	activeCalories *float64
	totalTSS       *float64
	totalTRIMP     *float64
	avgIF          *float64
	completionRate *float64
	avgWeight      *float64
}

func loadProgressAggregates(db *sql.DB, rng ProgressRange, aerobicOnlyEF bool) (*progressAggregates, error) {
	var out progressAggregates

	realWorkoutDays, err := countWorkoutDays(db, rng, false)
	if err != nil {
		return nil, err
	}
	completion := 0.0
	if rng.Days > 0 {
		completion = float64(realWorkoutDays) / float64(rng.Days) * 100
	}
	out.completionRate = floatPtr(completion)

	metricQuery := `
		SELECT
			AVG(CASE WHEN (? = 0 OR m.intensity_factor < 0.8) THEN m.efficiency_factor END),
			AVG(CASE WHEN w.duration_sec >= 5400 THEN m.decoupling_pct END),
			SUM(w.calories),
			SUM(m.tss),
			SUM(m.trimp),
			AVG(m.intensity_factor)
		FROM workouts w
		JOIN ride_metrics m ON m.workout_id = w.id
		WHERE w.source != 'manual'
		  AND w.started_at >= ?
		  AND w.started_at <= ?`

	var avgEF, avgDec, activeCalories, totalTSS, totalTRIMP, avgIF sql.NullFloat64
	if err := db.QueryRow(metricQuery, boolToInt(aerobicOnlyEF), rng.From, rng.To).Scan(
		&avgEF, &avgDec, &activeCalories, &totalTSS, &totalTRIMP, &avgIF,
	); err != nil {
		return nil, fmt.Errorf("storage.loadProgressAggregates: metrics: %w", err)
	}
	out.avgEF = nullFloatPtr(avgEF)
	out.avgDecoupling = nullFloatPtr(avgDec)
	out.activeCalories = nullFloatPtr(activeCalories)
	out.totalTSS = nullFloatPtr(totalTSS)
	out.totalTRIMP = nullFloatPtr(totalTRIMP)
	out.avgIF = nullFloatPtr(avgIF)

	var avgWeight sql.NullFloat64
	var weightPoints int
	if err := db.QueryRow(`
		SELECT AVG(weight_kg), COUNT(weight_kg)
		FROM athlete_notes
		WHERE type = 'weight'
		  AND weight_kg IS NOT NULL
		  AND timestamp >= ?
		  AND timestamp <= ?`,
		rng.From, rng.To,
	).Scan(&avgWeight, &weightPoints); err != nil {
		return nil, fmt.Errorf("storage.loadProgressAggregates: weight: %w", err)
	}
	if weightPoints >= 3 {
		out.avgWeight = nullFloatPtr(avgWeight)
	}

	return &out, nil
}

func loadWeeklyLoad(db *sql.DB, rng ProgressRange) ([]WeeklyLoadPoint, error) {
	rows, err := db.Query(`
		SELECT w.started_at, m.tss, m.trimp
		FROM workouts w
		JOIN ride_metrics m ON m.workout_id = w.id
		WHERE w.source != 'manual'
		  AND w.started_at >= ?
		  AND w.started_at <= ?
		ORDER BY w.started_at ASC`,
		rng.From, rng.To,
	)
	if err != nil {
		return nil, fmt.Errorf("storage.loadWeeklyLoad: query: %w", err)
	}
	defer rows.Close()

	points := map[string]*WeeklyLoadPoint{}
	order := []string{}
	for rows.Next() {
		var startedAt time.Time
		var tss, trimp sql.NullFloat64
		if err := rows.Scan(&startedAt, &tss, &trimp); err != nil {
			return nil, fmt.Errorf("storage.loadWeeklyLoad: scan: %w", err)
		}
		weekStart := weekStartMonday(startedAt)
		key := weekStart.Format("2006-01-02")
		point := points[key]
		if point == nil {
			point = &WeeklyLoadPoint{WeekStart: weekStart}
			points[key] = point
			order = append(order, key)
		}
		if tss.Valid {
			point.TSS += tss.Float64
		}
		if trimp.Valid {
			point.TRIMP += trimp.Float64
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage.loadWeeklyLoad: rows: %w", err)
	}

	out := make([]WeeklyLoadPoint, 0, len(order))
	for _, key := range order {
		out = append(out, *points[key])
	}
	return out, nil
}

func countWorkoutDays(db *sql.DB, rng ProgressRange, includeManual bool) (int, error) {
	query := `
		SELECT COUNT(DISTINCT date(started_at))
		FROM workouts
		WHERE started_at >= ?
		  AND started_at <= ?`
	if !includeManual {
		query += " AND source != 'manual'"
	}

	var count int
	if err := db.QueryRow(query, rng.From, rng.To).Scan(&count); err != nil {
		return 0, fmt.Errorf("storage.countWorkoutDays: %w", err)
	}
	return count, nil
}

func progressRanges(from, to time.Time) (ProgressRange, ProgressRange, error) {
	from = dayStart(from)
	to = dayEnd(to)
	if to.Before(from) {
		return ProgressRange{}, ProgressRange{}, fmt.Errorf("from must be on or before to")
	}
	days := inclusiveDays(from, to)
	selected := ProgressRange{From: from, To: to, Days: days}
	priorTo := from.Add(-time.Nanosecond)
	priorFrom := dayStart(from.AddDate(0, 0, -days))
	prior := ProgressRange{From: priorFrom, To: dayEnd(priorTo), Days: days}
	return selected, prior, nil
}

func compareProgressValues(current, prior *float64) ProgressMetric {
	out := ProgressMetric{
		Current: current,
		Prior:   prior,
		Trend:   ProgressTrendSteady,
	}
	if current == nil && prior == nil {
		return out
	}
	if current != nil && prior == nil {
		switch {
		case *current > 0:
			out.Trend = ProgressTrendUp
		case *current < 0:
			out.Trend = ProgressTrendDown
		}
		return out
	}
	if current == nil {
		return out
	}

	delta := *current - *prior
	out.Delta = floatPtr(delta)
	if *prior == 0 {
		switch {
		case delta > 0:
			out.Trend = ProgressTrendUp
		case delta < 0:
			out.Trend = ProgressTrendDown
		default:
			out.Trend = ProgressTrendSteady
		}
		return out
	}

	deltaPct := delta / math.Abs(*prior)
	out.DeltaPct = floatPtr(deltaPct)
	switch {
	case math.Abs(deltaPct) <= progressTrendThreshold:
		out.Trend = ProgressTrendSteady
	case deltaPct > 0:
		out.Trend = ProgressTrendUp
	default:
		out.Trend = ProgressTrendDown
	}
	return out
}

func compareWeightValues(current, prior *float64) ProgressMetric {
	if current == nil || prior == nil {
		return ProgressMetric{Trend: ProgressTrendSteady}
	}
	return compareProgressValues(current, prior)
}

func dayStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func dayEnd(t time.Time) time.Time {
	return dayStart(t).Add(24*time.Hour - time.Nanosecond)
}

func inclusiveDays(from, to time.Time) int {
	return int(dayStart(to).Sub(dayStart(from)).Hours()/24) + 1
}

func weekStartMonday(t time.Time) time.Time {
	t = dayStart(t)
	offset := (int(t.Weekday()) + 6) % 7
	return t.AddDate(0, 0, -offset)
}

func nullFloatPtr(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	return floatPtr(v.Float64)
}

func floatPtr(v float64) *float64 {
	return &v
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

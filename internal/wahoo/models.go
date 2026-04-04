package wahoo

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"

	"cycling-coach/internal/storage"
)

// flexFloat64 unmarshals a JSON number or a JSON string containing a number.
// If the string is non-numeric (e.g. "--") it decodes as 0.
type flexFloat64 float64

func (f *flexFloat64) UnmarshalJSON(b []byte) error {
	// Try plain number first.
	var n float64
	if err := json.Unmarshal(b, &n); err == nil {
		*f = flexFloat64(n)
		return nil
	}
	// Fall back to string.
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	if s == "" || s == "--" || s == "N/A" {
		*f = 0
		return nil
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		*f = 0
		return nil // treat any unparseable string as zero rather than hard-failing
	}
	*f = flexFloat64(n)
	return nil
}

// WorkoutListResponse is the paginated response from GET /v1/workouts.
type WorkoutListResponse struct {
	Workouts []APIWorkout `json:"workouts"`
	Total    int          `json:"total"`
	Page     int          `json:"page"`
	PerPage  int          `json:"per_page"`
}

// APIWorkout is a single workout entry from the Wahoo API.
type APIWorkout struct {
	ID            int64       `json:"id"`
	Name          string      `json:"name"`
	Starts        time.Time   `json:"starts"`
	WorkoutTypeID int64       `json:"workout_type_id"`
	Summary       *APISummary `json:"workout_summary"`
}

// APISummary holds per-workout performance data from the Wahoo API.
// All numeric fields use flexFloat64 because the Wahoo API occasionally returns
// string values (e.g. "--") for fields that have no data.
type APISummary struct {
	CaloriesAccum       flexFloat64 `json:"calories_accum"`
	CadenceAvg          flexFloat64 `json:"cadence_avg"`
	DistanceAccum       flexFloat64 `json:"distance_accum"`
	DurationActiveAccum flexFloat64 `json:"duration_active_accum"`
	DurationTotalAccum  flexFloat64 `json:"duration_total_accum"`
	HeartRateAvg        flexFloat64 `json:"heart_rate_avg"`
	HeartRateMax        flexFloat64 `json:"heart_rate_max"`
	PowerAvg            flexFloat64 `json:"power_avg"`
	PowerMax            flexFloat64 `json:"power_max"`
	File                *APIFile    `json:"file"`
}

// APIFile holds the FIT file download URL from the Wahoo CDN.
type APIFile struct {
	URL string `json:"url"`
}

// WebhookWorkout is the nested "workout" object inside Wahoo's documented
// workout_summary webhook payload.
type WebhookWorkout struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	Starts        time.Time `json:"starts"`
	WorkoutTypeID int64     `json:"workout_type_id"`
}

// WebhookWorkoutSummary is the documented "workout_summary" object posted by
// the Wahoo webhook. Unlike the polling API shape, the workout identity and
// start time live under the nested "workout" object while the FIT URL lives on
// the summary object itself.
type WebhookWorkoutSummary struct {
	ID                  int64           `json:"id"`
	CaloriesAccum       flexFloat64     `json:"calories_accum"`
	CadenceAvg          flexFloat64     `json:"cadence_avg"`
	DistanceAccum       flexFloat64     `json:"distance_accum"`
	DurationActiveAccum flexFloat64     `json:"duration_active_accum"`
	DurationTotalAccum  flexFloat64     `json:"duration_total_accum"`
	HeartRateAvg        flexFloat64     `json:"heart_rate_avg"`
	PowerAvg            flexFloat64     `json:"power_avg"`
	File                *APIFile        `json:"file"`
	Workout             *WebhookWorkout `json:"workout"`
}

// ToAPIWorkout converts the webhook-specific nested payload shape into the same
// APIWorkout shape used by the sync/polling path so ingestion logic can be shared.
func (w *WebhookWorkoutSummary) ToAPIWorkout() *APIWorkout {
	if w == nil || w.Workout == nil {
		return nil
	}
	return &APIWorkout{
		ID:            w.Workout.ID,
		Name:          w.Workout.Name,
		Starts:        w.Workout.Starts,
		WorkoutTypeID: w.Workout.WorkoutTypeID,
		Summary: &APISummary{
			CaloriesAccum:       w.CaloriesAccum,
			CadenceAvg:          w.CadenceAvg,
			DistanceAccum:       w.DistanceAccum,
			DurationActiveAccum: w.DurationActiveAccum,
			DurationTotalAccum:  w.DurationTotalAccum,
			HeartRateAvg:        w.HeartRateAvg,
			PowerAvg:            w.PowerAvg,
			File:                w.File,
		},
	}
}

// ToWorkout converts an APIWorkout to the internal storage type.
func (w *APIWorkout) ToWorkout() *storage.Workout {
	wt := workoutTypeName(w.WorkoutTypeID)
	wtID := w.WorkoutTypeID
	out := &storage.Workout{
		WahooID:       strconv.FormatInt(w.ID, 10),
		StartedAt:     w.Starts,
		Source:        "api",
		WorkoutType:   &wt,
		WorkoutTypeID: &wtID,
		Processed:     false,
	}
	if s := w.Summary; s != nil {
		out.DurationSec = optionalInt(float64(s.DurationTotalAccum))
		out.DistanceM = optionalFloat(float64(s.DistanceAccum))
		out.Calories = optionalInt(float64(s.CaloriesAccum))
		out.AvgHR = optionalInt(float64(s.HeartRateAvg))
		out.MaxHR = optionalInt(float64(s.HeartRateMax))
		out.AvgPower = optionalFloat(float64(s.PowerAvg))
		out.MaxPower = optionalFloat(float64(s.PowerMax))
		out.AvgCadence = optionalFloat(float64(s.CadenceAvg))
	}
	return out
}

// workoutTypeName maps a Wahoo workout_type_id to a human-readable string.
// IDs not in the map are stored as "wahoo_type_<id>" for later inspection.
var knownWorkoutTypes = map[int64]string{
	2:  "cycling",
	3:  "mountain_biking",
	4:  "cyclocross",
	14: "indoor_cycling",
	25: "indoor_cycling",
}

func workoutTypeName(id int64) string {
	if name, ok := knownWorkoutTypes[id]; ok {
		return name
	}
	return fmt.Sprintf("wahoo_type_%d", id)
}

// WebhookEvent is the JSON payload POSTed by Wahoo to /wahoo/webhook.
// Only "workout_summary" events are acted on; all others are logged and acknowledged.
type WebhookEvent struct {
	EventType      string                 `json:"event_type"`
	WebhookToken   string                 `json:"webhook_token"`
	WorkoutSummary *WebhookWorkoutSummary `json:"workout_summary"`
}

// optionalFloat returns a pointer to v, or nil when v is zero.
func optionalFloat(v float64) *float64 {
	if v == 0 {
		return nil
	}
	return &v
}

// optionalInt rounds v and returns a pointer, or nil when v is zero.
func optionalInt(v float64) *int64 {
	if v == 0 {
		return nil
	}
	n := int64(math.Round(v))
	return &n
}

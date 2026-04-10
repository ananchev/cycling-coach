package wahoo

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// newMockAPIClient creates an HTTP client that always returns the given workouts.
func newMockAPIClient(t *testing.T, workouts []APIWorkout) *http.Client {
	t.Helper()
	body, err := json.Marshal(WorkoutListResponse{
		Workouts: workouts,
		Total:    len(workouts),
		Page:     1,
		PerPage:  30,
	})
	if err != nil {
		t.Fatalf("newMockAPIClient: marshal: %v", err)
	}
	return &http.Client{Transport: wahooSyncRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(string(body))),
		}, nil
	})}
}

func testWorkouts() []APIWorkout {
	return []APIWorkout{
		{
			ID:            2001,
			Starts:        time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC),
			WorkoutTypeID: 25,
			Summary: &APISummary{
				DurationTotalAccum: 3600,
				HeartRateAvg:       125,
				PowerAvg:           180,
			},
		},
		{
			ID:            2002,
			Starts:        time.Date(2026, 3, 11, 9, 0, 0, 0, time.UTC),
			WorkoutTypeID: 25,
			Summary: &APISummary{
				DurationTotalAccum: 5400,
				HeartRateAvg:       120,
				PowerAvg:           155,
			},
		},
	}
}

func TestSyncer_Sync_InsertsNewWorkouts(t *testing.T) {
	db := openTestDB(t)

	client := newClientForTest(newMockAPIClient(t, testWorkouts()), "https://example.test")
	syncer := NewSyncer(client, db, t.TempDir())

	result, err := syncer.Sync(context.Background(), SyncOptions{})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if result.Inserted != 2 {
		t.Errorf("Inserted = %d, want 2", result.Inserted)
	}
	if result.Skipped != 0 {
		t.Errorf("Skipped = %d, want 0", result.Skipped)
	}
	if len(result.Errors) != 0 {
		t.Errorf("Errors = %v, want none", result.Errors)
	}
}

func TestSyncer_Sync_Idempotent(t *testing.T) {
	db := openTestDB(t)

	client := newClientForTest(newMockAPIClient(t, testWorkouts()), "https://example.test")
	syncer := NewSyncer(client, db, t.TempDir())

	// First sync: both workouts are new.
	r1, err := syncer.Sync(context.Background(), SyncOptions{})
	if err != nil {
		t.Fatalf("first Sync: %v", err)
	}
	if r1.Inserted != 2 || r1.Skipped != 0 {
		t.Errorf("first sync: inserted=%d skipped=%d, want inserted=2 skipped=0", r1.Inserted, r1.Skipped)
	}

	// Second sync: same data — both should be skipped.
	r2, err := syncer.Sync(context.Background(), SyncOptions{})
	if err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if r2.Inserted != 0 || r2.Skipped != 2 {
		t.Errorf("second sync: inserted=%d skipped=%d, want inserted=0 skipped=2", r2.Inserted, r2.Skipped)
	}
}

func TestSyncer_Sync_PartialUpdate(t *testing.T) {
	db := openTestDB(t)

	// First sync with one workout.
	client1 := newClientForTest(newMockAPIClient(t, testWorkouts()[:1]), "https://example.test")
	syncer1 := NewSyncer(client1, db, t.TempDir())

	r1, err := syncer1.Sync(context.Background(), SyncOptions{})
	if err != nil {
		t.Fatalf("first Sync: %v", err)
	}
	if r1.Inserted != 1 {
		t.Fatalf("first sync inserted=%d, want 1", r1.Inserted)
	}

	// Second sync with both workouts: one new, one existing.
	client2 := newClientForTest(newMockAPIClient(t, testWorkouts()), "https://example.test")
	syncer2 := NewSyncer(client2, db, t.TempDir())

	r2, err := syncer2.Sync(context.Background(), SyncOptions{})
	if err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if r2.Inserted != 1 || r2.Skipped != 1 {
		t.Errorf("second sync: inserted=%d skipped=%d, want inserted=1 skipped=1", r2.Inserted, r2.Skipped)
	}
}

func TestSyncer_Sync_EmptyResponse(t *testing.T) {
	db := openTestDB(t)

	client := newClientForTest(newMockAPIClient(t, []APIWorkout{}), "https://example.test")
	syncer := NewSyncer(client, db, t.TempDir())

	result, err := syncer.Sync(context.Background(), SyncOptions{})
	if err != nil {
		t.Fatalf("Sync on empty response: %v", err)
	}
	if result.Inserted != 0 || result.Skipped != 0 {
		t.Errorf("expected 0 inserted and 0 skipped, got %+v", result)
	}
}

func TestToWorkout_Mapping(t *testing.T) {
	pwr := flexFloat64(185.5)
	hr := flexFloat64(127.0)
	w := &APIWorkout{
		ID:            9999,
		WorkoutTypeID: 25,
		Starts:        time.Date(2026, 3, 15, 8, 30, 0, 0, time.UTC),
		Summary: &APISummary{
			DurationTotalAccum: 3600,
			DistanceAccum:      42000,
			CaloriesAccum:      800,
			HeartRateAvg:       hr,
			HeartRateMax:       150,
			PowerAvg:           pwr,
			PowerMax:           400,
			CadenceAvg:         72,
		},
	}

	got := w.ToWorkout()

	if got.WahooID != "9999" {
		t.Errorf("WahooID = %q, want %q", got.WahooID, "9999")
	}
	if got.Source != "api" {
		t.Errorf("Source = %q, want %q", got.Source, "api")
	}
	if got.WorkoutType == nil || *got.WorkoutType != "indoor_cycling" {
		t.Errorf("WorkoutType = %v, want indoor_cycling", got.WorkoutType)
	}
	if got.DurationSec == nil || *got.DurationSec != 3600 {
		t.Errorf("DurationSec = %v, want 3600", got.DurationSec)
	}
	if got.AvgPower == nil || *got.AvgPower != float64(pwr) {
		t.Errorf("AvgPower = %v, want %f", got.AvgPower, pwr)
	}
	if got.AvgHR == nil || *got.AvgHR != int64(hr) {
		t.Errorf("AvgHR = %v, want %d", got.AvgHR, int64(hr))
	}
}

func TestToWorkout_ZeroSummaryFieldsAreNil(t *testing.T) {
	w := &APIWorkout{
		ID:            8888,
		WorkoutTypeID: 25,
		Starts:        time.Date(2026, 3, 15, 8, 30, 0, 0, time.UTC),
		Summary: &APISummary{
			DurationTotalAccum: 3600,
			// HeartRateAvg, PowerAvg, CadenceAvg all zero
		},
	}
	got := w.ToWorkout()
	if got.AvgHR != nil {
		t.Errorf("AvgHR should be nil when API value is 0, got %v", got.AvgHR)
	}
	if got.AvgPower != nil {
		t.Errorf("AvgPower should be nil when API value is 0, got %v", got.AvgPower)
	}
	if got.AvgCadence != nil {
		t.Errorf("AvgCadence should be nil when API value is 0, got %v", got.AvgCadence)
	}
}

type wahooSyncRoundTripFunc func(*http.Request) (*http.Response, error)

func (f wahooSyncRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

package wahoo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)



// mockWorkoutResponse builds a WorkoutListResponse JSON body for the given page.
func mockWorkoutResponse(t *testing.T, workouts []APIWorkout, total int) []byte {
	t.Helper()
	resp := WorkoutListResponse{
		Workouts: workouts,
		Total:    total,
		Page:     1,
		PerPage:  30,
	}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("mockWorkoutResponse: marshal: %v", err)
	}
	return b
}

func twoAPIWorkouts() []APIWorkout {
	pwr := flexFloat64(180.0)
	hr := flexFloat64(125.0)
	return []APIWorkout{
		{
			ID:            1001,
			Name:          "Morning Ride",
			Starts:        time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC),
			WorkoutTypeID: 25,
			Summary: &APISummary{
				DurationTotalAccum: 3600,
				DistanceAccum:      40000,
				CaloriesAccum:      750,
				HeartRateAvg:       hr,
				HeartRateMax:       155,
				PowerAvg:           pwr,
				PowerMax:           350,
				CadenceAvg:         72,
			},
		},
		{
			ID:            1002,
			Name:          "Evening Z2",
			Starts:        time.Date(2026, 3, 11, 17, 0, 0, 0, time.UTC),
			WorkoutTypeID: 25,
			Summary: &APISummary{
				DurationTotalAccum: 5400,
				DistanceAccum:      55000,
				CaloriesAccum:      900,
				HeartRateAvg:       120,
				HeartRateMax:       140,
				PowerAvg:           155,
				PowerMax:           220,
				CadenceAvg:         70,
			},
		},
	}
}

func TestClient_ListWorkouts_ParsesResponse(t *testing.T) {
	workouts := twoAPIWorkouts()
	body := mockWorkoutResponse(t, workouts, 2)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/workouts" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(body) //nolint:errcheck
	}))
	defer server.Close()

	client := newClientForTest(server.Client(), server.URL)
	resp, err := client.ListWorkouts(context.Background(), 1, 30, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("ListWorkouts: %v", err)
	}

	if len(resp.Workouts) != 2 {
		t.Fatalf("expected 2 workouts, got %d", len(resp.Workouts))
	}
	if resp.Total != 2 {
		t.Errorf("Total = %d, want 2", resp.Total)
	}

	w := resp.Workouts[0]
	if w.ID != 1001 {
		t.Errorf("ID = %d, want 1001", w.ID)
	}
	if w.Summary == nil {
		t.Fatal("expected non-nil Summary")
	}
	if w.Summary.PowerAvg != flexFloat64(180.0) {
		t.Errorf("PowerAvg = %f, want 180.0", w.Summary.PowerAvg)
	}
}

func TestClient_ListWorkouts_NonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	client := newClientForTest(server.Client(), server.URL)
	_, err := client.ListWorkouts(context.Background(), 1, 30, time.Time{}, time.Time{})
	if err == nil {
		t.Fatal("expected error for non-OK status")
	}
}

func TestClient_ListWorkouts_BearerTokenPassedThrough(t *testing.T) {
	var receivedAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(WorkoutListResponse{}) //nolint:errcheck
	}))
	defer server.Close()

	// Build a client that injects a Bearer token via a custom RoundTripper.
	transport := &bearerTransport{token: "test-access-token", base: server.Client().Transport}
	authClient := &http.Client{Transport: transport}

	client := newClientForTest(authClient, server.URL)
	client.ListWorkouts(context.Background(), 1, 30, time.Time{}, time.Time{}) //nolint:errcheck

	if receivedAuth != "Bearer test-access-token" {
		t.Errorf("Authorization = %q, want %q", receivedAuth, "Bearer test-access-token")
	}
}

func TestClient_DownloadFIT(t *testing.T) {
	fitContent := []byte("FIT_FILE_BINARY_DATA")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(fitContent) //nolint:errcheck
	}))
	defer server.Close()

	destPath := t.TempDir() + "/test.fit"
	client := newClientForTest(server.Client(), server.URL)

	if err := client.DownloadFIT(context.Background(), server.URL+"/fit/1234.fit", destPath); err != nil {
		t.Fatalf("DownloadFIT: %v", err)
	}

	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(fitContent) {
		t.Errorf("file content = %q, want %q", got, fitContent)
	}
}

// bearerTransport is a test RoundTripper that adds an Authorization header.
type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r2 := req.Clone(req.Context())
	r2.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(r2)
}

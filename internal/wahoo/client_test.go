package wahoo

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
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

	httpClient := &http.Client{Transport: wahooRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/workouts" {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("not found")),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(string(body))),
		}, nil
	})}

	client := newClientForTest(httpClient, "https://example.test")
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
	httpClient := &http.Client{Transport: wahooRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("unauthorized\n")),
		}, nil
	})}

	client := newClientForTest(httpClient, "https://example.test")
	_, err := client.ListWorkouts(context.Background(), 1, 30, time.Time{}, time.Time{})
	if err == nil {
		t.Fatal("expected error for non-OK status")
	}
}

func TestClient_ListWorkouts_BearerTokenPassedThrough(t *testing.T) {
	var receivedAuth string

	baseTransport := wahooRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		receivedAuth = r.Header.Get("Authorization")
		body, _ := json.Marshal(WorkoutListResponse{})
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(string(body))),
		}, nil
	})

	// Build a client that injects a Bearer token via a custom RoundTripper.
	transport := &bearerTransport{token: "test-access-token", base: baseTransport}
	authClient := &http.Client{Transport: transport}

	client := newClientForTest(authClient, "https://example.test")
	client.ListWorkouts(context.Background(), 1, 30, time.Time{}, time.Time{}) //nolint:errcheck

	if receivedAuth != "Bearer test-access-token" {
		t.Errorf("Authorization = %q, want %q", receivedAuth, "Bearer test-access-token")
	}
}

func TestClient_DownloadFIT(t *testing.T) {
	fitContent := []byte("FIT_FILE_BINARY_DATA")

	destPath := t.TempDir() + "/test.fit"
	origDefaultClient := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: wahooRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(string(fitContent))),
		}, nil
	})}
	defer func() { http.DefaultClient = origDefaultClient }()

	client := newClientForTest(&http.Client{}, "https://example.test")

	if err := client.DownloadFIT(context.Background(), "https://example.test/fit/1234.fit", destPath); err != nil {
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

type wahooRoundTripFunc func(*http.Request) (*http.Response, error)

func (f wahooRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

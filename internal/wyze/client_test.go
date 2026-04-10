package wyze

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestClient_QueryScaleRecords(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/scale-records/query" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		respBody, _ := json.Marshal(map[string]any{
			"records": []map[string]any{
				{
					"external_id":    "wyze:scale_record:abc123",
					"measured_at":    "2026-04-08T07:14:22Z",
					"weight_kg":      77.4,
					"body_fat_pct":   18.2,
					"muscle_mass_kg": 36.8,
					"body_water_pct": 55.1,
					"bmr_kcal":       1684,
					"raw_source":     "wyze",
				},
			},
		})
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(string(respBody))),
		}, nil
	})}

	client := NewClientForTest("https://example.test", httpClient)
	got, err := client.QueryScaleRecords(
		context.Background(),
		time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 9, 23, 59, 59, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("QueryScaleRecords: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(got))
	}
	if got[0].ExternalID != "wyze:scale_record:abc123" {
		t.Fatalf("ExternalID = %q", got[0].ExternalID)
	}
	if got[0].BodyWaterPct == nil || *got[0].BodyWaterPct != 55.1 {
		t.Fatalf("BodyWaterPct = %v", got[0].BodyWaterPct)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

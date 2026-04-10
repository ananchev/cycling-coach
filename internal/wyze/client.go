package wyze

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type ScaleRecord struct {
	ExternalID   string `json:"external_id"`
	MeasuredAt   time.Time
	DeviceID     *string  `json:"device_id,omitempty"`
	WeightKG     *float64 `json:"weight_kg,omitempty"`
	BodyFatPct   *float64 `json:"body_fat_pct,omitempty"`
	MuscleMassKG *float64 `json:"muscle_mass_kg,omitempty"`
	BodyWaterPct *float64 `json:"body_water_pct,omitempty"`
	BMRKcal      *float64 `json:"bmr_kcal,omitempty"`
	RawSource    string   `json:"raw_source"`
}

type sidecarRecord struct {
	ExternalID   string   `json:"external_id"`
	MeasuredAt   string   `json:"measured_at"`
	DeviceID     *string  `json:"device_id,omitempty"`
	WeightKG     *float64 `json:"weight_kg,omitempty"`
	BodyFatPct   *float64 `json:"body_fat_pct,omitempty"`
	MuscleMassKG *float64 `json:"muscle_mass_kg,omitempty"`
	BodyWaterPct *float64 `json:"body_water_pct,omitempty"`
	BMRKcal      *float64 `json:"bmr_kcal,omitempty"`
	RawSource    string   `json:"raw_source"`
}

type sidecarQueryResponse struct {
	Records []sidecarRecord `json:"records"`
}

type Sidecar interface {
	QueryScaleRecords(ctx context.Context, from, to time.Time) ([]ScaleRecord, error)
}

type Client struct {
	baseURL string
	http    *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

func NewClientForTest(baseURL string, httpClient *http.Client) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    httpClient,
	}
}

func (c *Client) QueryScaleRecords(ctx context.Context, from, to time.Time) ([]ScaleRecord, error) {
	reqBody, err := json.Marshal(map[string]string{
		"from": from.Format(time.RFC3339),
		"to":   to.Format(time.RFC3339),
	})
	if err != nil {
		return nil, fmt.Errorf("wyze.Client.QueryScaleRecords: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/scale-records/query", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("wyze.Client.QueryScaleRecords: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("wyze.Client.QueryScaleRecords: do: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("wyze.Client.QueryScaleRecords: read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wyze.Client.QueryScaleRecords: status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBytes)))
	}

	var out sidecarQueryResponse
	if err := json.Unmarshal(respBytes, &out); err != nil {
		return nil, fmt.Errorf("wyze.Client.QueryScaleRecords: unmarshal: %w", err)
	}

	records := make([]ScaleRecord, 0, len(out.Records))
	for _, rec := range out.Records {
		measuredAt, err := time.Parse(time.RFC3339, rec.MeasuredAt)
		if err != nil {
			return nil, fmt.Errorf("wyze.Client.QueryScaleRecords: parse measured_at: %w", err)
		}
		records = append(records, ScaleRecord{
			ExternalID:   rec.ExternalID,
			MeasuredAt:   measuredAt,
			DeviceID:     rec.DeviceID,
			WeightKG:     rec.WeightKG,
			BodyFatPct:   rec.BodyFatPct,
			MuscleMassKG: rec.MuscleMassKG,
			BodyWaterPct: rec.BodyWaterPct,
			BMRKcal:      rec.BMRKcal,
			RawSource:    rec.RawSource,
		})
	}
	return records, nil
}

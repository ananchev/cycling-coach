package wahoo

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/oauth2"

	"cycling-coach/internal/config"
)

const wahooBaseURL = "https://api.wahooligan.com"

// Client calls the Wahoo Cloud API with automatic token refresh.
type Client struct {
	http    *http.Client
	baseURL string
}

// NewClient creates a Client whose requests are authenticated with a DB-backed token.
// The token is refreshed automatically when it expires.
func NewClient(db *sql.DB, cfg *config.Config) *Client {
	conf := newOAuth2Config(cfg)
	ts := oauth2.ReuseTokenSource(nil, &dbTokenSource{db: db, conf: conf})
	return &Client{
		http:    oauth2.NewClient(context.Background(), ts),
		baseURL: wahooBaseURL,
	}
}

// newClientForTest creates a Client with a custom http.Client and base URL.
// Used in tests to point the client at a mock HTTP server.
func newClientForTest(httpClient *http.Client, baseURL string) *Client {
	return &Client{
		http:    httpClient,
		baseURL: baseURL,
	}
}

// ListWorkouts fetches one page of workouts from GET /v1/workouts.
// page is 1-based; perPage must be ≤ 30 (Wahoo sandbox limit).
// from/to are optional date filters (zero value = no filter).
// Sandbox rate limit: 25 req / 5 min — callers must throttle for bulk fetches.
func (c *Client) ListWorkouts(ctx context.Context, page, perPage int, from, to time.Time) (*WorkoutListResponse, error) {
	url := fmt.Sprintf("%s/v1/workouts?page=%d&per_page=%d", c.baseURL, page, perPage)
	if !from.IsZero() {
		url += "&start_date=" + from.UTC().Format(time.RFC3339)
	}
	if !to.IsZero() {
		url += "&end_date=" + to.UTC().Format(time.RFC3339)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("wahoo.Client.ListWorkouts: build request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("wahoo.Client.ListWorkouts: do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("wahoo.Client.ListWorkouts: status %d: %s", resp.StatusCode, body)
	}

	var out WorkoutListResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("wahoo.Client.ListWorkouts: decode: %w", err)
	}

	slog.Debug("wahoo: fetched workouts page",
		"page", page,
		"count", len(out.Workouts),
		"total", out.Total,
	)
	return &out, nil
}

// DownloadFIT downloads a FIT file from the Wahoo CDN to destPath.
// CDN files do not require an Authorization header.
// The parent directory of destPath is created if it does not exist.
func (c *Client) DownloadFIT(ctx context.Context, fileURL, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("wahoo.Client.DownloadFIT: mkdir: %w", err)
	}

	// FIT files are served from the Wahoo CDN — no Bearer token needed.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return fmt.Errorf("wahoo.Client.DownloadFIT: build request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("wahoo.Client.DownloadFIT: do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("wahoo.Client.DownloadFIT: status %d", resp.StatusCode)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("wahoo.Client.DownloadFIT: create: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("wahoo.Client.DownloadFIT: write: %w", err)
	}
	return nil
}

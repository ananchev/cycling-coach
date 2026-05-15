package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"cycling-coach/internal/storage"
)

// mcpTestProfilePath points to the fixture athlete profile in testdata/.
const mcpTestProfilePath = "../../testdata/athlete-profile.md"

// openMCPTestDB copies testdata/cycling.db to a temp dir and opens it so tests
// run against real fixture data without modifying the source file.
func openMCPTestDB(t *testing.T) *storage.DB {
	t.Helper()
	src := "../../testdata/cycling.db"
	tmp := t.TempDir()
	for _, suffix := range []string{"", "-shm", "-wal"} {
		data, err := os.ReadFile(src + suffix)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) && suffix != "" {
				continue // WAL companion files may be absent
			}
			t.Fatalf("copy testdata/cycling.db%s: %v", suffix, err)
		}
		if err := os.WriteFile(filepath.Join(tmp, "cycling.db"+suffix), data, 0644); err != nil {
			t.Fatalf("write cycling.db%s: %v", suffix, err)
		}
	}
	store, err := storage.Open(filepath.Join(tmp, "cycling.db"))
	if err != nil {
		t.Fatalf("storage.Open(testdata/cycling.db): %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// ── Exclusion guard ───────────────────────────────────────────────────────────

// TestMCPDTOs_NoExcludedColumnNames walks every MCP DTO type via reflection
// and asserts that no JSON field name matches a §8-excluded column. If this
// test fails, a DTO has leaked a forbidden field into the contract surface.
func TestMCPDTOs_NoExcludedColumnNames(t *testing.T) {
	excluded := []string{
		"system_prompt", "user_prompt", "full_html",
		"raw_payload_json", "access_token", "refresh_token",
	}
	types := []any{
		mcpProfileResponse{},
		mcpZoneConfigResponse{},
		mcpWorkoutListItem{},
		mcpWorkoutsListResponse{},
		mcpZonePcts{},
		mcpCadenceBands{},
		mcpWorkoutDetailResponse{},
		mcpPeriod{},
		mcpBlockContextResponse{},
		mcpProgressRange{},
		mcpProgressMetric{},
		mcpProgressKPIs{},
		mcpWeeklyLoadPoint{},
		mcpProgressResponse{},
		mcpNoteItem{},
		mcpNotesListResponse{},
		mcpBodyMetricItem{},
		mcpBodyMetricDeltas{},
		mcpBodyMetricsResponse{},
		mcpReportListItem{},
		mcpReportsListResponse{},
		mcpReportDetailResponse{},
		mcpErrorResponse{},
	}
	for _, v := range types {
		assertNoExcludedFields(t, reflect.TypeOf(v), excluded)
	}
}

func assertNoExcludedFields(t *testing.T, rt reflect.Type, excluded []string) {
	t.Helper()
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		tag := strings.Split(f.Tag.Get("json"), ",")[0]
		for _, ex := range excluded {
			if tag == ex {
				t.Errorf("DTO %s.%s exposes excluded JSON field %q", rt.Name(), f.Name, ex)
			}
		}
		ft := f.Type
		if ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Struct {
			assertNoExcludedFields(t, ft, excluded)
		}
	}
}

// ── Shape tests ───────────────────────────────────────────────────────────────

func TestMCPEndpoints_Return200WithContractShape(t *testing.T) {
	store := openMCPTestDB(t)
	h := &mcpHandlers{db: store.DB(), profilePath: mcpTestProfilePath}

	t.Run("profile", func(t *testing.T) {
		rr := mcpGET(h.profile, "/api/mcp/v1/profile", "")
		assertMCPOK(t, rr)
		var resp mcpProfileResponse
		mustDecode(t, rr, &resp)
	})

	t.Run("zone-config", func(t *testing.T) {
		rr := mcpGET(h.zoneConfig, "/api/mcp/v1/zone-config", "")
		assertMCPOK(t, rr)
		var resp mcpZoneConfigResponse
		mustDecode(t, rr, &resp)
		if len(resp.HRZoneBounds) != 4 {
			t.Errorf("hr_zone_bounds len = %d, want 4", len(resp.HRZoneBounds))
		}
		if len(resp.PowerZoneBounds) != 4 {
			t.Errorf("power_zone_bounds len = %d, want 4", len(resp.PowerZoneBounds))
		}
	})

	t.Run("workouts-list", func(t *testing.T) {
		rr := mcpGET(h.listWorkouts, "/api/mcp/v1/workouts", "")
		assertMCPOK(t, rr)
		var resp mcpWorkoutsListResponse
		mustDecode(t, rr, &resp)
		if resp.Items == nil {
			t.Error("items must not be null")
		}
	})

	t.Run("workout-detail", func(t *testing.T) {
		rr := mcpGETWithID(h.getWorkout, "/api/mcp/v1/workouts/1", "id", "1")
		assertMCPOK(t, rr)
		var resp mcpWorkoutDetailResponse
		mustDecode(t, rr, &resp)
	})

	t.Run("block-context", func(t *testing.T) {
		rr := mcpGET(h.blockContext, "/api/mcp/v1/block-context", "")
		assertMCPOK(t, rr)
		var resp mcpBlockContextResponse
		mustDecode(t, rr, &resp)
	})

	t.Run("block-context-current", func(t *testing.T) {
		rr := mcpGET(h.blockContext, "/api/mcp/v1/block-context", "block=current")
		assertMCPOK(t, rr)
		var resp mcpBlockContextResponse
		mustDecode(t, rr, &resp)
	})

	t.Run("progress", func(t *testing.T) {
		rr := mcpGET(h.progress, "/api/mcp/v1/progress", "")
		assertMCPOK(t, rr)
		var resp mcpProgressResponse
		mustDecode(t, rr, &resp)
		if resp.WeeklyLoad == nil {
			t.Error("weekly_load must not be null")
		}
		assertProgressKPITrend(t, resp.KPIs.AerobicEfficiency.Trend)
		assertProgressKPITrend(t, resp.KPIs.CumulativeTSS.Trend)
	})

	t.Run("notes-list", func(t *testing.T) {
		rr := mcpGET(h.listNotes, "/api/mcp/v1/notes", "")
		assertMCPOK(t, rr)
		var resp mcpNotesListResponse
		mustDecode(t, rr, &resp)
		if resp.Items == nil {
			t.Error("items must not be null")
		}
	})

	t.Run("body-metrics", func(t *testing.T) {
		rr := mcpGET(h.bodyMetrics, "/api/mcp/v1/body-metrics", "")
		assertMCPOK(t, rr)
		var resp mcpBodyMetricsResponse
		mustDecode(t, rr, &resp)
		if resp.Items == nil {
			t.Error("items must not be null")
		}
	})

	t.Run("reports-list", func(t *testing.T) {
		rr := mcpGET(h.listReports, "/api/mcp/v1/reports", "")
		assertMCPOK(t, rr)
		var resp mcpReportsListResponse
		mustDecode(t, rr, &resp)
		if resp.Items == nil {
			t.Error("items must not be null")
		}
	})

	t.Run("report-detail", func(t *testing.T) {
		rr := mcpGETWithID(h.getReport, "/api/mcp/v1/reports/1", "id", "1")
		assertMCPOK(t, rr)
		var resp mcpReportDetailResponse
		mustDecode(t, rr, &resp)
	})
}

// ── Bad-param tests ───────────────────────────────────────────────────────────

func TestMCPEndpoints_BadParams_Return400(t *testing.T) {
	store := openMCPTestDB(t)
	h := &mcpHandlers{db: store.DB(), profilePath: mcpTestProfilePath}

	cases := []struct {
		name    string
		handler http.HandlerFunc
		query   string
	}{
		{"workouts bad from", h.listWorkouts, "from=not-a-date"},
		{"workouts bad to", h.listWorkouts, "to=not-a-date"},
		{"workouts to before from", h.listWorkouts, "from=2025-11-10&to=2025-11-01"},
		{"workouts bad last_days", h.listWorkouts, "last_days=abc"},
		{"workouts bad limit", h.listWorkouts, "limit=abc"},
		{"block-context bad from", h.blockContext, "from=not-a-date"},
		{"progress bad from", h.progress, "from=not-a-date"},
		{"notes bad from", h.listNotes, "from=not-a-date"},
		{"notes bad limit", h.listNotes, "limit=-5"},
		{"body-metrics bad to", h.bodyMetrics, "to=not-a-date"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := mcpGET(tc.handler, "/api/mcp/v1/x", tc.query)
			if rr.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 (body: %s)", rr.Code, rr.Body.String())
			}
			var errResp mcpErrorResponse
			if err := json.NewDecoder(rr.Body).Decode(&errResp); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			if errResp.Error == "" {
				t.Error("expected non-empty error message")
			}
		})
	}

	// Non-integer id → 400
	for _, name := range []string{"workout bad id", "report bad id"} {
		t.Run(name, func(t *testing.T) {
			var handler http.HandlerFunc
			var paramKey string
			if strings.HasPrefix(name, "workout") {
				handler = h.getWorkout
				paramKey = "id"
			} else {
				handler = h.getReport
				paramKey = "id"
			}
			rr := mcpGETWithID(handler, "/api/mcp/v1/x/abc", paramKey, "abc")
			if rr.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rr.Code)
			}
		})
	}
}

// ── Router mount test ─────────────────────────────────────────────────────────

func TestMCPRoutes_AllMounted(t *testing.T) {
	store := openMCPTestDB(t)
	r := chi.NewRouter()
	mountMCPRoutes(r, store.DB(), mcpTestProfilePath)

	paths := []string{
		"/api/mcp/v1/profile",
		"/api/mcp/v1/zone-config",
		"/api/mcp/v1/workouts",
		"/api/mcp/v1/workouts/1",
		"/api/mcp/v1/block-context",
		"/api/mcp/v1/progress",
		"/api/mcp/v1/notes",
		"/api/mcp/v1/body-metrics",
		"/api/mcp/v1/reports",
		"/api/mcp/v1/reports/1",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)
			if rr.Code == http.StatusNotFound {
				t.Errorf("route %s returned 404 — not mounted", path)
			}
		})
	}
}

// ── Test helpers ──────────────────────────────────────────────────────────────

// mcpGET fires a GET request to handler with optional query string.
func mcpGET(handler http.HandlerFunc, path, query string) *httptest.ResponseRecorder {
	url := path
	if query != "" {
		url += "?" + query
	}
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rr := httptest.NewRecorder()
	handler(rr, req)
	return rr
}

// mcpGETWithID fires a GET request to handler with a chi URL param set.
func mcpGETWithID(handler http.HandlerFunc, path, paramKey, paramVal string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(paramKey, paramVal)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()
	handler(rr, req)
	return rr
}

func assertMCPOK(t *testing.T, rr *httptest.ResponseRecorder) {
	t.Helper()
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rr.Code, rr.Body.String())
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func mustDecode(t *testing.T, rr *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.NewDecoder(rr.Body).Decode(v); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func assertProgressKPITrend(t *testing.T, trend string) {
	t.Helper()
	switch trend {
	case "up", "down", "steady":
		// valid
	default:
		t.Errorf("trend = %q, want up/down/steady", trend)
	}
}

package web

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

// Budget constants — overridable via env in WP8 deployment wiring.
const (
	mcpDefaultWindowDays    = 28
	mcpMaxRows              = 200
	mcpBlockContextMaxChars = 60_000
	mcpTruncHint            = "Narrow the date range to see fewer results."
)

// ── DTO types ─────────────────────────────────────────────────────────────────
// All types must be free of excluded columns (§8): system_prompt, user_prompt,
// full_html, raw_payload_json, access_token, refresh_token. The exclusion test
// in mcp_test.go asserts this via reflection on every type below.

type mcpProfileResponse struct {
	Markdown string `json:"markdown"`
}

type mcpZoneConfigResponse struct {
	FTPW            int   `json:"ftp_w"`
	HRMaxBPM        int   `json:"hr_max_bpm"`
	HRZoneBounds    []int `json:"hr_zone_bounds"`
	PowerZoneBounds []int `json:"power_zone_bounds"`
}

type mcpWorkoutListItem struct {
	ID          int64    `json:"id"`
	WahooID     string   `json:"wahoo_id"`
	Date        string   `json:"date"`
	Type        *string  `json:"type"`
	DurationMin *float64 `json:"duration_min"`
	AvgPowerW   *float64 `json:"avg_power_w"`
	NPW         *float64 `json:"np_w"`
	IF          *float64 `json:"if"`
	VI          *float64 `json:"vi"`
	AvgHR       *float64 `json:"avg_hr"`
	TSS         *float64 `json:"tss"`
	Processed   bool     `json:"processed"`
}

type mcpWorkoutsListResponse struct {
	Items     []mcpWorkoutListItem `json:"items"`
	Truncated bool                 `json:"truncated"`
	Hint      *string              `json:"hint"`
}

type mcpZonePcts struct {
	Z1Pct *float64 `json:"z1_pct"`
	Z2Pct *float64 `json:"z2_pct"`
	Z3Pct *float64 `json:"z3_pct"`
	Z4Pct *float64 `json:"z4_pct"`
	Z5Pct *float64 `json:"z5_pct"`
}

type mcpCadenceBands struct {
	LT70Pct     *float64 `json:"lt70_pct"`
	Z70to85Pct  *float64 `json:"z70_85_pct"`
	Z85to100Pct *float64 `json:"z85_100_pct"`
	GE100Pct    *float64 `json:"ge100_pct"`
}

type mcpWorkoutDetailResponse struct {
	ID               int64           `json:"id"`
	WahooID          string          `json:"wahoo_id"`
	Date             string          `json:"date"`
	Type             *string         `json:"type"`
	DurationMin      *float64        `json:"duration_min"`
	AvgPowerW        *float64        `json:"avg_power_w"`
	NormalizedPowerW *float64        `json:"normalized_power_w"`
	IntensityFactor  *float64        `json:"intensity_factor"`
	VariabilityIndex *float64        `json:"variability_index"`
	AvgHR            *float64        `json:"avg_hr"`
	MaxHR            *float64        `json:"max_hr"`
	AvgCadenceRPM    *float64        `json:"avg_cadence_rpm"`
	TSS              *float64        `json:"tss"`
	EfficiencyFactor *float64        `json:"efficiency_factor"`
	HRDriftPct       *float64        `json:"hr_drift_pct"`
	Processed        bool            `json:"processed"`
	PowerZones       mcpZonePcts     `json:"power_zones"`
	HRZones          mcpZonePcts     `json:"hr_zones"`
	CadenceBands     mcpCadenceBands `json:"cadence_bands"`
	ZoneDetailMD     *string         `json:"zone_detail_markdown"`
	RideNotes        *string         `json:"ride_notes"`
	GeneralNotes     *string         `json:"general_notes"`
}

type mcpPeriod struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type mcpBlockContextResponse struct {
	Period         mcpPeriod `json:"period"`
	Markdown       string    `json:"markdown"`
	TruncatedChars bool      `json:"truncated_chars"`
}

type mcpProgressRange struct {
	From string `json:"from"`
	To   string `json:"to"`
	Days int    `json:"days"`
}

type mcpProgressMetric struct {
	Current  *float64 `json:"current"`
	Prior    *float64 `json:"prior"`
	Delta    *float64 `json:"delta"`
	DeltaPct *float64 `json:"delta_pct"`
	Trend    string   `json:"trend"`
}

type mcpProgressKPIs struct {
	AerobicEfficiency   mcpProgressMetric `json:"aerobic_efficiency"`
	EnduranceDurability mcpProgressMetric `json:"endurance_durability"`
	CumulativeTSS       mcpProgressMetric `json:"cumulative_tss"`
	CumulativeTRIMP     mcpProgressMetric `json:"cumulative_trimp"`
	AverageIF           mcpProgressMetric `json:"average_if"`
	CompletionRate      mcpProgressMetric `json:"completion_rate"`
	AverageWeightKG     mcpProgressMetric `json:"average_weight_kg"`
}

type mcpWeeklyLoadPoint struct {
	WeekStart string  `json:"week_start"`
	TSS       float64 `json:"tss"`
	TRIMP     float64 `json:"trimp"`
}

type mcpProgressResponse struct {
	SelectedRange  mcpProgressRange     `json:"selected_range"`
	PriorRange     mcpProgressRange     `json:"prior_range"`
	AerobicOnlyEF  bool                 `json:"aerobic_only_ef"`
	KPIs           mcpProgressKPIs      `json:"kpis"`
	WeeklyLoad     []mcpWeeklyLoadPoint `json:"weekly_load"`
	SavedNarrative *string              `json:"saved_narrative"`
}

type mcpNoteItem struct {
	Timestamp    string   `json:"timestamp"`
	Type         string   `json:"type"`
	RPE          *int64   `json:"rpe"`
	Text         *string  `json:"text"`
	WeightKG     *float64 `json:"weight_kg"`
	BodyFatPct   *float64 `json:"body_fat_pct"`
	MuscleMassKG *float64 `json:"muscle_mass_kg"`
	BodyWaterPct *float64 `json:"body_water_pct"`
	BMRKcal      *float64 `json:"bmr_kcal"`
	WorkoutID    *int64   `json:"workout_id"`
}

type mcpNotesListResponse struct {
	Items     []mcpNoteItem `json:"items"`
	Truncated bool          `json:"truncated"`
	Hint      *string       `json:"hint"`
}

type mcpBodyMetricItem struct {
	Date         string   `json:"date"`
	WeightKG     *float64 `json:"weight_kg"`
	BodyFatPct   *float64 `json:"body_fat_pct"`
	MuscleMassKG *float64 `json:"muscle_mass_kg"`
	BodyWaterPct *float64 `json:"body_water_pct"`
	BMRKcal      *float64 `json:"bmr_kcal"`
}

type mcpBodyMetricDeltas struct {
	WeightKG     *float64 `json:"weight_kg"`
	BodyFatPct   *float64 `json:"body_fat_pct"`
	MuscleMassKG *float64 `json:"muscle_mass_kg"`
	BodyWaterPct *float64 `json:"body_water_pct"`
	BMRKcal      *float64 `json:"bmr_kcal"`
}

type mcpBodyMetricsResponse struct {
	Items     []mcpBodyMetricItem `json:"items"`
	Deltas    mcpBodyMetricDeltas `json:"deltas"`
	Truncated bool                `json:"truncated"`
}

type mcpReportListItem struct {
	ID             int64   `json:"id"`
	Type           string  `json:"type"`
	WeekStart      string  `json:"week_start"`
	WeekEnd        string  `json:"week_end"`
	CreatedAt      string  `json:"created_at"`
	HasSummary     bool    `json:"has_summary"`
	HasNarrative   bool    `json:"has_narrative"`
	DeliveryStatus *string `json:"delivery_status"`
}

type mcpReportsListResponse struct {
	Items []mcpReportListItem `json:"items"`
}

type mcpReportDetailResponse struct {
	ID                int64   `json:"id"`
	Type              string  `json:"type"`
	WeekStart         string  `json:"week_start"`
	WeekEnd           string  `json:"week_end"`
	CreatedAt         string  `json:"created_at"`
	SummaryMarkdown   *string `json:"summary_markdown"`
	NarrativeMarkdown *string `json:"narrative_markdown"`
}

type mcpErrorResponse struct {
	Error string `json:"error"`
}

// ── Handler struct ─────────────────────────────────────────────────────────────

type mcpHandlers struct {
	db          *sql.DB
	profilePath string
}

// mountMCPRoutes registers all /api/mcp/v1/* routes on r.
func mountMCPRoutes(r chi.Router, db *sql.DB, profilePath string) {
	h := &mcpHandlers{db: db, profilePath: profilePath}
	r.Route("/api/mcp/v1", func(r chi.Router) {
		r.Get("/profile", h.profile)
		r.Get("/zone-config", h.zoneConfig)
		r.Get("/workouts", h.listWorkouts)
		r.Get("/workouts/{id}", h.getWorkout)
		r.Get("/block-context", h.blockContext)
		r.Get("/progress", h.progress)
		r.Get("/notes", h.listNotes)
		r.Get("/body-metrics", h.bodyMetrics)
		r.Get("/reports", h.listReports)
		r.Get("/reports/{id}", h.getReport)
	})
}

// ── Stub handlers (WP1) ───────────────────────────────────────────────────────
// Each stub parses and validates params, then returns a contract-shaped empty
// response. WP2 replaces the body of each stub with real storage calls.

func (h *mcpHandlers) profile(w http.ResponseWriter, r *http.Request) {
	mcpWriteJSON(w, mcpProfileResponse{})
}

func (h *mcpHandlers) zoneConfig(w http.ResponseWriter, r *http.Request) {
	mcpWriteJSON(w, mcpZoneConfigResponse{
		HRZoneBounds:    []int{0, 0, 0, 0},
		PowerZoneBounds: []int{0, 0, 0, 0},
	})
}

func (h *mcpHandlers) listWorkouts(w http.ResponseWriter, r *http.Request) {
	if _, err := parseMCPWindow(r); err != nil {
		writeMCPError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := parseMCPLimit(r); err != nil {
		writeMCPError(w, http.StatusBadRequest, err.Error())
		return
	}
	mcpWriteJSON(w, mcpWorkoutsListResponse{Items: []mcpWorkoutListItem{}, Hint: nil})
}

func (h *mcpHandlers) getWorkout(w http.ResponseWriter, r *http.Request) {
	if _, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64); err != nil {
		writeMCPError(w, http.StatusBadRequest, "id must be an integer")
		return
	}
	mcpWriteJSON(w, mcpWorkoutDetailResponse{
		PowerZones:   mcpZonePcts{},
		HRZones:      mcpZonePcts{},
		CadenceBands: mcpCadenceBands{},
	})
}

func (h *mcpHandlers) blockContext(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("block") != "current" {
		if _, err := parseMCPWindow(r); err != nil {
			writeMCPError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	mcpWriteJSON(w, mcpBlockContextResponse{Period: mcpPeriod{}})
}

func (h *mcpHandlers) progress(w http.ResponseWriter, r *http.Request) {
	if fromStr := r.URL.Query().Get("from"); fromStr != "" {
		if _, err := time.Parse("2006-01-02", fromStr); err != nil {
			writeMCPError(w, http.StatusBadRequest, "from must be YYYY-MM-DD")
			return
		}
	}
	mcpWriteJSON(w, mcpProgressResponse{
		KPIs: mcpProgressKPIs{
			AerobicEfficiency:   mcpProgressMetric{Trend: "steady"},
			EnduranceDurability: mcpProgressMetric{Trend: "steady"},
			CumulativeTSS:       mcpProgressMetric{Trend: "steady"},
			CumulativeTRIMP:     mcpProgressMetric{Trend: "steady"},
			AverageIF:           mcpProgressMetric{Trend: "steady"},
			CompletionRate:      mcpProgressMetric{Trend: "steady"},
			AverageWeightKG:     mcpProgressMetric{Trend: "steady"},
		},
		WeeklyLoad: []mcpWeeklyLoadPoint{},
	})
}

func (h *mcpHandlers) listNotes(w http.ResponseWriter, r *http.Request) {
	if _, err := parseMCPWindow(r); err != nil {
		writeMCPError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := parseMCPLimit(r); err != nil {
		writeMCPError(w, http.StatusBadRequest, err.Error())
		return
	}
	mcpWriteJSON(w, mcpNotesListResponse{Items: []mcpNoteItem{}, Hint: nil})
}

func (h *mcpHandlers) bodyMetrics(w http.ResponseWriter, r *http.Request) {
	if _, err := parseMCPWindow(r); err != nil {
		writeMCPError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := parseMCPLimit(r); err != nil {
		writeMCPError(w, http.StatusBadRequest, err.Error())
		return
	}
	mcpWriteJSON(w, mcpBodyMetricsResponse{Items: []mcpBodyMetricItem{}})
}

func (h *mcpHandlers) listReports(w http.ResponseWriter, r *http.Request) {
	mcpWriteJSON(w, mcpReportsListResponse{Items: []mcpReportListItem{}})
}

func (h *mcpHandlers) getReport(w http.ResponseWriter, r *http.Request) {
	if _, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64); err != nil {
		writeMCPError(w, http.StatusBadRequest, "id must be an integer")
		return
	}
	mcpWriteJSON(w, mcpReportDetailResponse{})
}

// ── Budget / param helpers ─────────────────────────────────────────────────────

// mcpWindow holds the resolved date window after parsing query params.
type mcpWindow struct {
	From time.Time
	To   time.Time
}

// parseMCPWindow resolves the date window from query params. Priority:
//  1. last_days  → from = today − last_days, to = today
//  2. from / to  → parsed from YYYY-MM-DD
//  3. default    → from = today − mcpDefaultWindowDays, to = today
func parseMCPWindow(r *http.Request) (mcpWindow, error) {
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	if ldStr := r.URL.Query().Get("last_days"); ldStr != "" {
		ld, err := strconv.Atoi(ldStr)
		if err != nil || ld <= 0 {
			return mcpWindow{}, fmt.Errorf("last_days must be a positive integer")
		}
		return mcpWindow{From: today.AddDate(0, 0, -ld), To: today}, nil
	}

	from := today.AddDate(0, 0, -mcpDefaultWindowDays)
	to := today

	if fromStr := r.URL.Query().Get("from"); fromStr != "" {
		t, err := time.Parse("2006-01-02", fromStr)
		if err != nil {
			return mcpWindow{}, fmt.Errorf("from must be YYYY-MM-DD")
		}
		from = t
	}
	if toStr := r.URL.Query().Get("to"); toStr != "" {
		t, err := time.Parse("2006-01-02", toStr)
		if err != nil {
			return mcpWindow{}, fmt.Errorf("to must be YYYY-MM-DD")
		}
		to = t
	}
	if to.Before(from) {
		return mcpWindow{}, fmt.Errorf("to must be on or after from")
	}
	return mcpWindow{From: from, To: to}, nil
}

// parseMCPLimit returns the effective row limit from the query param, capped at mcpMaxRows.
func parseMCPLimit(r *http.Request) (int, error) {
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		l, err := strconv.Atoi(lStr)
		if err != nil || l <= 0 {
			return 0, fmt.Errorf("limit must be a positive integer")
		}
		if l > mcpMaxRows {
			return mcpMaxRows, nil
		}
		return l, nil
	}
	return mcpMaxRows, nil
}

// mcpHintPtr returns the truncation hint pointer when truncated is true, nil otherwise.
func mcpHintPtr(truncated bool) *string {
	if truncated {
		s := mcpTruncHint
		return &s
	}
	return nil
}

// ── JSON helpers ───────────────────────────────────────────────────────────────

func mcpWriteJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("mcp: encode response", "err", err)
	}
}

func writeMCPError(w http.ResponseWriter, statusCode int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(mcpErrorResponse{Error: msg})
}

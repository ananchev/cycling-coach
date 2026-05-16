package web

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"cycling-coach/internal/analysis"
	"cycling-coach/internal/reporting"
	"cycling-coach/internal/storage"
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
// When apiKey is non-empty, requests must carry "Authorization: Bearer <apiKey>".
// Empty apiKey disables the check (local development).
func mountMCPRoutes(r chi.Router, db *sql.DB, profilePath, apiKey string) {
	h := &mcpHandlers{db: db, profilePath: profilePath}
	r.Route("/api/mcp/v1", func(r chi.Router) {
		r.Use(mcpBearerAuth(apiKey))
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

// mcpBearerAuth returns a middleware that enforces a static Bearer token on
// every request. When key is empty the middleware is a no-op (local dev).
func mcpBearerAuth(key string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}
			auth := r.Header.Get("Authorization")
			token, ok := strings.CutPrefix(auth, "Bearer ")
			if !ok || token != key {
				writeMCPError(w, http.StatusUnauthorized, "invalid or missing API key")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ── Handlers ──────────────────────────────────────────────────────────────────

func (h *mcpHandlers) profile(w http.ResponseWriter, r *http.Request) {
	data, err := os.ReadFile(h.profilePath)
	if err != nil {
		writeMCPError(w, http.StatusInternalServerError, "profile unavailable")
		return
	}
	mcpWriteJSON(w, mcpProfileResponse{Markdown: string(data)})
}

func (h *mcpHandlers) zoneConfig(w http.ResponseWriter, r *http.Request) {
	zc, err := analysis.LoadZoneConfig(h.db)
	if err != nil {
		slog.Error("mcp: zoneConfig", "err", err)
		writeMCPError(w, http.StatusInternalServerError, "zone config unavailable")
		return
	}
	mcpWriteJSON(w, mcpZoneConfigResponse{
		FTPW:            zc.FTPWatts,
		HRMaxBPM:        zc.HRMax,
		HRZoneBounds:    []int{zc.HRZ1Max, zc.HRZ2Max, zc.HRZ3Max, zc.HRZ4Max},
		PowerZoneBounds: []int{zc.PwrZ1Max, zc.PwrZ2Max, zc.PwrZ3Max, zc.PwrZ4Max},
	})
}

func (h *mcpHandlers) listWorkouts(w http.ResponseWriter, r *http.Request) {
	win, err := parseMCPWindow(r)
	if err != nil {
		writeMCPError(w, http.StatusBadRequest, err.Error())
		return
	}
	limit, err := parseMCPLimit(r)
	if err != nil {
		writeMCPError(w, http.StatusBadRequest, err.Error())
		return
	}

	f := storage.WorkoutFilter{
		From:    win.From,
		To:      win.To,
		Type:    r.URL.Query().Get("type"),
		WahooID: r.URL.Query().Get("wahoo_id"),
	}

	rows, err := storage.ListWorkoutsForMCP(h.db, f, limit+1)
	if err != nil {
		slog.Error("mcp: listWorkouts", "err", err)
		writeMCPError(w, http.StatusInternalServerError, "database error")
		return
	}

	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}

	items := make([]mcpWorkoutListItem, len(rows))
	for i, wm := range rows {
		items[i] = workoutToListItem(wm)
	}

	mcpWriteJSON(w, mcpWorkoutsListResponse{
		Items:     items,
		Truncated: truncated,
		Hint:      mcpHintPtr(truncated),
	})
}

func (h *mcpHandlers) getWorkout(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeMCPError(w, http.StatusBadRequest, "id must be an integer")
		return
	}

	wm, err := storage.GetWorkoutWithMetricsByID(h.db, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeMCPError(w, http.StatusNotFound, "workout not found")
		} else {
			slog.Error("mcp: getWorkout", "err", err)
			writeMCPError(w, http.StatusInternalServerError, "database error")
		}
		return
	}

	// Fetch full RideMetrics for EfficiencyFactor and MaxHR — not in WorkoutWithMetrics join.
	var ef, maxHR *float64
	if rm, err := storage.GetRideMetrics(h.db, id); err == nil {
		ef = rm.EfficiencyFactor
		maxHR = rm.MaxHR
	}

	var durationMin *float64
	if wm.DurationSec != nil {
		d := float64(*wm.DurationSec) / 60.0
		durationMin = &d
	}

	var zoneDetailPtr *string
	if md := reporting.FormatWorkoutZoneDetail(wm); md != "" {
		zoneDetailPtr = &md
	}

	mcpWriteJSON(w, mcpWorkoutDetailResponse{
		ID:               wm.ID,
		WahooID:          wm.WahooID,
		Date:             wm.StartedAt.Format("2006-01-02"),
		Type:             wm.WorkoutType,
		DurationMin:      durationMin,
		AvgPowerW:        wm.AvgPower,
		NormalizedPowerW: wm.NormalizedPower,
		IntensityFactor:  wm.IntensityFactor,
		VariabilityIndex: wm.VariabilityIndex,
		AvgHR:            wm.AvgHR,
		MaxHR:            maxHR,
		AvgCadenceRPM:    wm.AvgCadence,
		TSS:              wm.TSS,
		EfficiencyFactor: ef,
		HRDriftPct:       wm.HRDriftPct,
		Processed:        wm.Processed,
		PowerZones: mcpZonePcts{
			Z1Pct: wm.PwrZ1Pct,
			Z2Pct: wm.PwrZ2Pct,
			Z3Pct: wm.PwrZ3Pct,
			Z4Pct: wm.PwrZ4Pct,
			Z5Pct: wm.PwrZ5Pct,
		},
		HRZones: mcpZonePcts{
			Z1Pct: wm.HRZ1Pct,
			Z2Pct: wm.HRZ2Pct,
			Z3Pct: wm.HRZ3Pct,
			Z4Pct: wm.HRZ4Pct,
			Z5Pct: wm.HRZ5Pct,
		},
		CadenceBands: mcpCadenceBands{
			LT70Pct:     wm.CadLT70Pct,
			Z70to85Pct:  wm.Cad70To85Pct,
			Z85to100Pct: wm.Cad85To100Pct,
			GE100Pct:    wm.CadGE100Pct,
		},
		ZoneDetailMD: zoneDetailPtr,
		RideNotes:    wm.RideNotes,
		GeneralNotes: wm.GeneralNotes,
	})
}

func (h *mcpHandlers) blockContext(w http.ResponseWriter, r *http.Request) {
	var from, to time.Time

	if r.URL.Query().Get("block") == "current" {
		now := time.Now().UTC()
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

		latest, err := storage.GetLatestReport(h.db, storage.ReportTypeWeeklyReport)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				from = today.AddDate(0, 0, -mcpDefaultWindowDays)
			} else {
				slog.Error("mcp: blockContext: get latest report", "err", err)
				writeMCPError(w, http.StatusInternalServerError, "database error")
				return
			}
		} else {
			next := latest.WeekEnd.AddDate(0, 0, 1)
			from = time.Date(next.Year(), next.Month(), next.Day(), 0, 0, 0, 0, time.UTC)
		}
		to = today
	} else {
		win, err := parseMCPWindow(r)
		if err != nil {
			writeMCPError(w, http.StatusBadRequest, err.Error())
			return
		}
		from = win.From
		to = win.To
	}

	input, err := reporting.AssembleInput(r.Context(), h.db, h.profilePath, storage.ReportTypeWeeklyReport, from, to)
	if err != nil {
		slog.Error("mcp: blockContext: assemble", "err", err)
		writeMCPError(w, http.StatusInternalServerError, "context assembly failed")
		return
	}

	prompt := reporting.BuildPrompt(input)
	truncated := len(prompt) > mcpBlockContextMaxChars
	if truncated {
		prompt = prompt[:mcpBlockContextMaxChars]
	}

	mcpWriteJSON(w, mcpBlockContextResponse{
		Period:         mcpPeriod{From: from.Format("2006-01-02"), To: to.Format("2006-01-02")},
		Markdown:       prompt,
		TruncatedChars: truncated,
	})
}

func (h *mcpHandlers) progress(w http.ResponseWriter, r *http.Request) {
	win, err := parseMCPWindow(r)
	if err != nil {
		writeMCPError(w, http.StatusBadRequest, err.Error())
		return
	}

	aerobicOnlyEF := r.URL.Query().Get("aerobic_only_ef") == "true"

	snap, err := storage.BuildProgressSnapshot(h.db, win.From, win.To, aerobicOnlyEF)
	if err != nil {
		slog.Error("mcp: progress: snapshot", "err", err)
		writeMCPError(w, http.StatusInternalServerError, "database error")
		return
	}

	var savedNarrative *string
	if pa, err := storage.GetProgressAnalysis(h.db); err == nil {
		savedNarrative = &pa.Narrative
	}

	weekly := make([]mcpWeeklyLoadPoint, len(snap.WeeklyLoad))
	for i, wl := range snap.WeeklyLoad {
		weekly[i] = mcpWeeklyLoadPoint{
			WeekStart: wl.WeekStart.Format("2006-01-02"),
			TSS:       wl.TSS,
			TRIMP:     wl.TRIMP,
		}
	}

	mcpWriteJSON(w, mcpProgressResponse{
		SelectedRange: mcpProgressRange{
			From: snap.SelectedRange.From.Format("2006-01-02"),
			To:   snap.SelectedRange.To.Format("2006-01-02"),
			Days: snap.SelectedRange.Days,
		},
		PriorRange: mcpProgressRange{
			From: snap.PriorRange.From.Format("2006-01-02"),
			To:   snap.PriorRange.To.Format("2006-01-02"),
			Days: snap.PriorRange.Days,
		},
		AerobicOnlyEF: snap.AerobicOnlyEF,
		KPIs: mcpProgressKPIs{
			AerobicEfficiency:   progressMetricToMCP(snap.AerobicEfficiency),
			EnduranceDurability: progressMetricToMCP(snap.EnduranceDurability),
			CumulativeTSS:       progressMetricToMCP(snap.CumulativeTSS),
			CumulativeTRIMP:     progressMetricToMCP(snap.CumulativeTRIMP),
			AverageIF:           progressMetricToMCP(snap.AverageIF),
			CompletionRate:      progressMetricToMCP(snap.CompletionRate),
			AverageWeightKG:     progressMetricToMCP(snap.AverageWeightKG),
		},
		WeeklyLoad:     weekly,
		SavedNarrative: savedNarrative,
	})
}

func (h *mcpHandlers) listNotes(w http.ResponseWriter, r *http.Request) {
	win, err := parseMCPWindow(r)
	if err != nil {
		writeMCPError(w, http.StatusBadRequest, err.Error())
		return
	}
	limit, err := parseMCPLimit(r)
	if err != nil {
		writeMCPError(w, http.StatusBadRequest, err.Error())
		return
	}

	f := storage.NoteFilter{
		From: win.From,
		To:   win.To,
		Type: r.URL.Query().Get("type"),
	}

	notes, err := storage.ListNotesFiltered(h.db, f, limit+1)
	if err != nil {
		slog.Error("mcp: listNotes", "err", err)
		writeMCPError(w, http.StatusInternalServerError, "database error")
		return
	}

	truncated := len(notes) > limit
	if truncated {
		notes = notes[:limit]
	}

	items := make([]mcpNoteItem, len(notes))
	for i, n := range notes {
		items[i] = mcpNoteItem{
			Timestamp:    n.Timestamp.Format(time.RFC3339),
			Type:         string(n.Type),
			RPE:          n.RPE,
			Text:         n.Note,
			WeightKG:     n.WeightKG,
			BodyFatPct:   n.BodyFatPct,
			MuscleMassKG: n.MuscleMassKG,
			BodyWaterPct: n.BodyWaterPct,
			BMRKcal:      n.BMRKcal,
			WorkoutID:    n.WorkoutID,
		}
	}

	mcpWriteJSON(w, mcpNotesListResponse{
		Items:     items,
		Truncated: truncated,
		Hint:      mcpHintPtr(truncated),
	})
}

func (h *mcpHandlers) bodyMetrics(w http.ResponseWriter, r *http.Request) {
	win, err := parseMCPWindow(r)
	if err != nil {
		writeMCPError(w, http.StatusBadRequest, err.Error())
		return
	}
	limit, err := parseMCPLimit(r)
	if err != nil {
		writeMCPError(w, http.StatusBadRequest, err.Error())
		return
	}

	rows, err := storage.ListBodyMetrics(h.db, win.From, win.To, limit+1)
	if err != nil {
		slog.Error("mcp: bodyMetrics", "err", err)
		writeMCPError(w, http.StatusInternalServerError, "database error")
		return
	}

	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}

	items := make([]mcpBodyMetricItem, len(rows))
	for i, n := range rows {
		items[i] = mcpBodyMetricItem{
			Date:         n.Timestamp.Format("2006-01-02"),
			WeightKG:     n.WeightKG,
			BodyFatPct:   n.BodyFatPct,
			MuscleMassKG: n.MuscleMassKG,
			BodyWaterPct: n.BodyWaterPct,
			BMRKcal:      n.BMRKcal,
		}
	}

	mcpWriteJSON(w, mcpBodyMetricsResponse{
		Items:     items,
		Deltas:    computeBodyMetricDeltas(rows),
		Truncated: truncated,
	})
}

func (h *mcpHandlers) listReports(w http.ResponseWriter, r *http.Request) {
	typeFilter := r.URL.Query().Get("type")

	rwds, err := storage.ListReportsWithDelivery(h.db, time.Time{}, time.Time{}, 0)
	if err != nil {
		slog.Error("mcp: listReports", "err", err)
		writeMCPError(w, http.StatusInternalServerError, "database error")
		return
	}

	items := make([]mcpReportListItem, 0, len(rwds))
	for _, rwd := range rwds {
		if typeFilter != "" && string(rwd.Type) != typeFilter {
			continue
		}
		items = append(items, mcpReportListItem{
			ID:             rwd.ID,
			Type:           string(rwd.Type),
			WeekStart:      rwd.WeekStart.Format("2006-01-02"),
			WeekEnd:        rwd.WeekEnd.Format("2006-01-02"),
			CreatedAt:      rwd.CreatedAt.Format(time.RFC3339),
			HasSummary:     rwd.SummaryText != nil && *rwd.SummaryText != "",
			HasNarrative:   rwd.HasNarrative,
			DeliveryStatus: rwd.DeliveryStatus,
		})
	}

	mcpWriteJSON(w, mcpReportsListResponse{Items: items})
}

func (h *mcpHandlers) getReport(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeMCPError(w, http.StatusBadRequest, "id must be an integer")
		return
	}

	rep, err := storage.GetReportByID(h.db, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeMCPError(w, http.StatusNotFound, "report not found")
		} else {
			slog.Error("mcp: getReport", "err", err)
			writeMCPError(w, http.StatusInternalServerError, "database error")
		}
		return
	}

	mcpWriteJSON(w, mcpReportDetailResponse{
		ID:                rep.ID,
		Type:              string(rep.Type),
		WeekStart:         rep.WeekStart.Format("2006-01-02"),
		WeekEnd:           rep.WeekEnd.Format("2006-01-02"),
		CreatedAt:         rep.CreatedAt.Format(time.RFC3339),
		SummaryMarkdown:   rep.SummaryText,
		NarrativeMarkdown: rep.NarrativeText,
	})
}

// ── Conversion helpers ─────────────────────────────────────────────────────────

func workoutToListItem(wm storage.WorkoutWithMetrics) mcpWorkoutListItem {
	var durationMin *float64
	if wm.DurationSec != nil {
		d := float64(*wm.DurationSec) / 60.0
		durationMin = &d
	}
	return mcpWorkoutListItem{
		ID:          wm.ID,
		WahooID:     wm.WahooID,
		Date:        wm.StartedAt.Format("2006-01-02"),
		Type:        wm.WorkoutType,
		DurationMin: durationMin,
		AvgPowerW:   wm.AvgPower,
		NPW:         wm.NormalizedPower,
		IF:          wm.IntensityFactor,
		VI:          wm.VariabilityIndex,
		AvgHR:       wm.AvgHR,
		TSS:         wm.TSS,
		Processed:   wm.Processed,
	}
}

func progressMetricToMCP(pm storage.ProgressMetric) mcpProgressMetric {
	return mcpProgressMetric{
		Current:  pm.Current,
		Prior:    pm.Prior,
		Delta:    pm.Delta,
		DeltaPct: pm.DeltaPct,
		Trend:    string(pm.Trend),
	}
}

func computeBodyMetricDeltas(rows []storage.AthleteNote) mcpBodyMetricDeltas {
	if len(rows) < 2 {
		return mcpBodyMetricDeltas{}
	}
	first := rows[0]
	last := rows[len(rows)-1]
	return mcpBodyMetricDeltas{
		WeightKG:     floatDeltaPtr(first.WeightKG, last.WeightKG),
		BodyFatPct:   floatDeltaPtr(first.BodyFatPct, last.BodyFatPct),
		MuscleMassKG: floatDeltaPtr(first.MuscleMassKG, last.MuscleMassKG),
		BodyWaterPct: floatDeltaPtr(first.BodyWaterPct, last.BodyWaterPct),
		BMRKcal:      floatDeltaPtr(first.BMRKcal, last.BMRKcal),
	}
}

func floatDeltaPtr(first, last *float64) *float64 {
	if first == nil || last == nil {
		return nil
	}
	d := *last - *first
	return &d
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

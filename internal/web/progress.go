package web

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"cycling-coach/internal/reporting"
	"cycling-coach/internal/storage"
)

const progressDefaultDays = 56

type progressKPIResponse struct {
	Key         string                 `json:"key"`
	Title       string                 `json:"title"`
	Explanation string                 `json:"explanation"`
	Current     *float64               `json:"current,omitempty"`
	Prior       *float64               `json:"prior,omitempty"`
	Delta       *float64               `json:"delta,omitempty"`
	DeltaPct    *float64               `json:"delta_pct,omitempty"`
	Trend       storage.ProgressTrend  `json:"trend"`
}

// progressHandler returns the current progress dashboard snapshot for
// selected `from` through today, plus the currently saved AI interpretation.
// GET /api/progress?from=YYYY-MM-DD&aerobic_only_ef=1
func progressHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		now := time.Now().UTC()
		from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -progressDefaultDays+1)
		if fromStr := r.URL.Query().Get("from"); fromStr != "" {
			parsed, err := time.Parse("2006-01-02", fromStr)
			if err != nil {
				http.Error(w, "from must be YYYY-MM-DD", http.StatusBadRequest)
				return
			}
			from = parsed
		}
		aerobicOnly := r.URL.Query().Get("aerobic_only_ef") != "0"

		to := now
		snapshot, err := storage.BuildProgressSnapshot(db, from, to, aerobicOnly)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var saved *storage.ProgressAnalysis
		saved, err = storage.GetProgressAnalysis(db)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			slog.Error("progressHandler: saved analysis", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if errors.Is(err, sql.ErrNoRows) {
			saved = nil
		}

		type rangeJSON struct {
			From string `json:"from"`
			To   string `json:"to"`
			Days int    `json:"days"`
		}
		type weeklyLoadJSON struct {
			WeekStart string  `json:"week_start"`
			TSS       float64 `json:"tss"`
			TRIMP     float64 `json:"trimp"`
		}
		type savedJSON struct {
			From      string `json:"from"`
			To        string `json:"to"`
			Narrative string `json:"narrative"`
			HTML      string `json:"html"`
			System    string `json:"system_prompt"`
			User      string `json:"user_prompt"`
			UpdatedAt string `json:"updated_at"`
		}

		resp := struct {
			SelectedRange   rangeJSON             `json:"selected_range"`
			PriorRange      rangeJSON             `json:"prior_range"`
			AerobicOnlyEF   bool                  `json:"aerobic_only_ef"`
			KPIs            []progressKPIResponse `json:"kpis"`
			WeeklyLoad      []weeklyLoadJSON      `json:"weekly_load"`
			PriorWeeklyLoad []weeklyLoadJSON      `json:"prior_weekly_load"`
			SavedAnalysis   *savedJSON            `json:"saved_analysis,omitempty"`
		}{
			SelectedRange: rangeJSON{
				From: snapshot.SelectedRange.From.Format("2006-01-02"),
				To:   snapshot.SelectedRange.To.Format("2006-01-02"),
				Days: snapshot.SelectedRange.Days,
			},
			PriorRange: rangeJSON{
				From: snapshot.PriorRange.From.Format("2006-01-02"),
				To:   snapshot.PriorRange.To.Format("2006-01-02"),
				Days: snapshot.PriorRange.Days,
			},
			AerobicOnlyEF: snapshot.AerobicOnlyEF,
			KPIs: []progressKPIResponse{
				kpiResponse(reporting.ProgressKPIDefinitions()[0], snapshot.AerobicEfficiency),
				kpiResponse(reporting.ProgressKPIDefinitions()[1], snapshot.EnduranceDurability),
				kpiResponse(reporting.ProgressKPIDefinitions()[2], snapshot.ActiveCalories),
				kpiResponse(reporting.ProgressKPIDefinitions()[3], snapshot.CumulativeTSS),
				kpiResponse(reporting.ProgressKPIDefinitions()[4], snapshot.CumulativeTRIMP),
				kpiResponse(reporting.ProgressKPIDefinitions()[5], snapshot.AverageIF),
				kpiResponse(reporting.ProgressKPIDefinitions()[6], snapshot.CompletionRate),
				kpiResponse(reporting.ProgressKPIDefinitions()[7], snapshot.AverageWeightKG),
			},
		}

		for _, point := range snapshot.WeeklyLoad {
			resp.WeeklyLoad = append(resp.WeeklyLoad, weeklyLoadJSON{
				WeekStart: point.WeekStart.Format("2006-01-02"),
				TSS:       point.TSS,
				TRIMP:     point.TRIMP,
			})
		}
		for _, point := range snapshot.PriorWeeklyLoad {
			resp.PriorWeeklyLoad = append(resp.PriorWeeklyLoad, weeklyLoadJSON{
				WeekStart: point.WeekStart.Format("2006-01-02"),
				TSS:       point.TSS,
				TRIMP:     point.TRIMP,
			})
		}
		if saved != nil {
			resp.SavedAnalysis = &savedJSON{
				From:      saved.PeriodFrom.Format("2006-01-02"),
				To:        saved.PeriodTo.Format("2006-01-02"),
				Narrative: saved.Narrative,
				HTML:      reporting.RenderMarkdownFragment(saved.Narrative),
				System:    saved.SystemPrompt,
				User:      saved.UserPrompt,
				UpdatedAt: saved.UpdatedAt.Format(time.RFC3339),
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}
}

// progressInterpretHandler regenerates the single saved progress interpretation
// from the selected `from` date through today.
// POST /api/progress/interpret  body: {"from":"YYYY-MM-DD","aerobic_only_ef":true}
func progressInterpretHandler(orch *reporting.Orchestrator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			From          string `json:"from"`
			AerobicOnlyEF *bool  `json:"aerobic_only_ef"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.From == "" {
			http.Error(w, "from is required", http.StatusBadRequest)
			return
		}
		from, err := time.Parse("2006-01-02", req.From)
		if err != nil {
			http.Error(w, "from must be YYYY-MM-DD", http.StatusBadRequest)
			return
		}
		aerobicOnly := true
		if req.AerobicOnlyEF != nil {
			aerobicOnly = *req.AerobicOnlyEF
		}

		saved, err := orch.GenerateProgressAnalysis(r.Context(), from, time.Now().UTC(), aerobicOnly)
		if err != nil {
			slog.Error("progressInterpretHandler", "err", err)
			http.Error(w, "interpretation failed: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
			"status":     "saved",
			"from":       saved.PeriodFrom.Format("2006-01-02"),
			"to":         saved.PeriodTo.Format("2006-01-02"),
			"updated_at": saved.UpdatedAt.Format(time.RFC3339),
		})
	}
}

func kpiResponse(def reporting.ProgressKPIDefinition, metric storage.ProgressMetric) progressKPIResponse {
	return progressKPIResponse{
		Key:         def.Key,
		Title:       def.Title,
		Explanation: def.Explanation,
		Current:     metric.Current,
		Prior:       metric.Prior,
		Delta:       metric.Delta,
		DeltaPct:    metric.DeltaPct,
		Trend:       metric.Trend,
	}
}

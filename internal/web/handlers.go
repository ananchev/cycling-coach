package web

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"cycling-coach/internal/analysis"
	fitpkg "cycling-coach/internal/fit"
	"cycling-coach/internal/reporting"
	"cycling-coach/internal/storage"
	"cycling-coach/internal/wahoo"
)

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
}

// syncHandler returns an http.HandlerFunc that triggers a Wahoo sync and reports stats.
// Optional JSON body: {"from":"2026-01-01","to":"2026-03-31"} — both fields optional.
func syncHandler(syncer *wahoo.Syncer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var opts wahoo.SyncOptions
		if r.ContentLength != 0 {
			var req struct {
				From string `json:"from"`
				To   string `json:"to"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
				return
			}
			if req.From != "" {
				t, err := time.Parse("2006-01-02", req.From)
				if err != nil {
					http.Error(w, "from must be YYYY-MM-DD: "+err.Error(), http.StatusBadRequest)
					return
				}
				opts.From = t
			}
			if req.To != "" {
				t, err := time.Parse("2006-01-02", req.To)
				if err != nil {
					http.Error(w, "to must be YYYY-MM-DD: "+err.Error(), http.StatusBadRequest)
					return
				}
				opts.To = t
			}
		}

		result, err := syncer.Sync(r.Context(), opts)
		if err != nil {
			slog.Error("sync failed", "err", err)
			http.Error(w, "sync failed: "+err.Error(), http.StatusInternalServerError)
			return
		}

		type response struct {
			Inserted int      `json:"inserted"`
			Skipped  int      `json:"skipped"`
			Errors   []string `json:"errors,omitempty"`
		}
		resp := response{
			Inserted: result.Inserted,
			Skipped:  result.Skipped,
		}
		for _, e := range result.Errors {
			resp.Errors = append(resp.Errors, e.Error())
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}
}

// reportHandler returns an http.HandlerFunc that triggers report generation.
// Request body: {"type":"weekly_report","week_start":"2026-03-09"}
// week_end defaults to week_start + 7 days if omitted.
func reportHandler(orch *reporting.Orchestrator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Type       string `json:"type"`
			WeekStart  string `json:"week_start"`
			WeekEnd    string `json:"week_end"`
			UserPrompt string `json:"user_prompt"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		reportType := storage.ReportType(req.Type)
		if reportType != storage.ReportTypeWeeklyReport && reportType != storage.ReportTypeWeeklyPlan {
			http.Error(w, "type must be weekly_report or weekly_plan", http.StatusBadRequest)
			return
		}

		weekStart, err := time.Parse("2006-01-02", req.WeekStart)
		if err != nil {
			http.Error(w, "week_start must be YYYY-MM-DD: "+err.Error(), http.StatusBadRequest)
			return
		}

		var weekEnd time.Time
		if req.WeekEnd != "" {
			weekEnd, err = time.Parse("2006-01-02", req.WeekEnd)
			if err != nil {
				http.Error(w, "week_end must be YYYY-MM-DD: "+err.Error(), http.StatusBadRequest)
				return
			}
		} else {
			weekEnd = weekStart.Add(7 * 24 * time.Hour)
		}

		id, err := orch.Generate(r.Context(), reportType, weekStart, weekEnd, req.UserPrompt)
		if err != nil {
			slog.Error("report generation failed", "err", err)
			http.Error(w, "report generation failed: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int64{"id": id}) //nolint:errcheck
	}
}

// sendReportHandler sends a specific report via Telegram.
// Request body: {"report_id": 42}
func sendReportHandler(delivery *reporting.DeliveryService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ReportID int64 `json:"report_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.ReportID <= 0 {
			http.Error(w, "report_id must be a positive integer", http.StatusBadRequest)
			return
		}

		if delivery == nil {
			http.Error(w, "Telegram delivery not configured", http.StatusServiceUnavailable)
			return
		}

		if err := delivery.Send(r.Context(), req.ReportID); err != nil {
			slog.Error("send report failed", "report_id", req.ReportID, "err", err)
			http.Error(w, "send failed: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "sent"}) //nolint:errcheck
	}
}

// processHandler triggers FIT file processing.
// Optional JSON body fields:
//   - (no body)             → process all unprocessed workouts
//   - {"from":"YYYY-MM-DD","to":"YYYY-MM-DD"} → reprocess workouts in date range
//   - {"reprocess_all":true} → reprocess every workout regardless of processed flag
func processHandler(proc *analysis.Processor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var opts analysis.ProcessOptions
		if r.ContentLength != 0 {
			var req struct {
				From         string `json:"from"`
				To           string `json:"to"`
				ReprocessAll bool   `json:"reprocess_all"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
				return
			}
			if req.ReprocessAll {
				opts.ReprocessAll = true
			} else if req.From != "" || req.To != "" {
				if req.From != "" {
					t, err := time.Parse("2006-01-02", req.From)
					if err != nil {
						http.Error(w, "from must be YYYY-MM-DD", http.StatusBadRequest)
						return
					}
					opts.From = t
				}
				if req.To != "" {
					t, err := time.Parse("2006-01-02", req.To)
					if err != nil {
						http.Error(w, "to must be YYYY-MM-DD", http.StatusBadRequest)
						return
					}
					opts.To = t
				}
			}
		}

		result := proc.Process(r.Context(), opts)

		type parseErr struct {
			WahooID string `json:"wahoo_id"`
			Error   string `json:"error"`
		}
		type response struct {
			Processed    int        `json:"processed"`
			SkippedNoFIT int        `json:"skipped_no_fit"`
			ParseErrors  []parseErr `json:"parse_errors,omitempty"`
			Errors       []string   `json:"errors,omitempty"`
		}
		resp := response{
			Processed:    result.Processed,
			SkippedNoFIT: result.SkippedNoFIT,
		}
		for _, pe := range result.ParseErrors {
			resp.ParseErrors = append(resp.ParseErrors, parseErr{WahooID: pe.WahooID, Error: pe.Err.Error()})
		}
		for _, e := range result.Errors {
			resp.Errors = append(resp.Errors, e.Error())
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}
}

// resetFITHandler deletes the FIT file from disk and resets processed=false
// so the workout will be re-synced and re-processed.
// POST /api/workout/reset-fit  body: {"wahoo_id":"..."}
func resetFITHandler(proc *analysis.Processor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			WahooID string `json:"wahoo_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.WahooID == "" {
			http.Error(w, "wahoo_id required", http.StatusBadRequest)
			return
		}
		if err := proc.ResetFIT(r.Context(), req.WahooID); err != nil {
			slog.Error("resetFITHandler", "wahoo_id", req.WahooID, "err", err)
			http.Error(w, "reset failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		// Re-fetch to return the workout date for UI guidance.
		workout, err := storage.GetWorkoutByWahooID(proc.DB(), req.WahooID)
		if err != nil {
			// Non-fatal: reset succeeded, just can't return the date.
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "reset", "wahoo_id": req.WahooID}) //nolint:errcheck
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
			"status":     "reset",
			"wahoo_id":   req.WahooID,
			"started_at": workout.StartedAt.Format("2006-01-02"),
		})
	}
}

// deleteReportHandler deletes a report and its delivery record by ID.
// DELETE /api/report/{id}
func deleteReportHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || id <= 0 {
			http.Error(w, "invalid report id", http.StatusBadRequest)
			return
		}
		if err := storage.DeleteReport(db, id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			slog.Error("deleteReportHandler", "id", id, "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "deleted"}) //nolint:errcheck
	}
}

// ignoreFITHandler marks a workout as processed without requiring a valid FIT file.
// Use when the FIT file is permanently corrupt on the server and cannot be recovered.
// POST /api/workout/ignore  body: {"wahoo_id":"..."}
func ignoreFITHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			WahooID string `json:"wahoo_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.WahooID == "" {
			http.Error(w, "wahoo_id required", http.StatusBadRequest)
			return
		}
		workout, err := storage.GetWorkoutByWahooID(db, req.WahooID)
		if err != nil {
			http.Error(w, "workout not found: "+err.Error(), http.StatusNotFound)
			return
		}
		if err := storage.MarkWorkoutProcessed(db, workout.ID); err != nil {
			slog.Error("ignoreFITHandler", "wahoo_id", req.WahooID, "err", err)
			http.Error(w, "ignore failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		slog.Info("analysis: workout ignored (corrupt FIT, permanent)", "wahoo_id", req.WahooID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
			"status":   "ignored",
			"wahoo_id": req.WahooID,
		})
	}
}

// evolveProfileHandler regenerates the athlete profile using recent weekly reports.
// POST /api/profile/evolve  body: {"last_n": 8}  (last_n defaults to 8 when omitted)
func evolveProfileHandler(orch *reporting.Orchestrator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			LastN int `json:"last_n"`
		}
		req.LastN = 8
		if r.ContentLength != 0 {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
				return
			}
		}
		if req.LastN <= 0 {
			req.LastN = 8
		}

		result, err := orch.EvolveProfile(r.Context(), req.LastN)
		if err != nil {
			// Validation failure: Claude produced output but it was structurally invalid.
			// Return 422 with the rejected filename so the UI can guide the user.
			var ve *reporting.EvolveValidationError
			if errors.As(err, &ve) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnprocessableEntity)
				json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
					"status":   "validation_failed",
					"reason":   ve.Reason,
					"rejected": filepath.Base(ve.RejectedPath),
				})
				return
			}
			slog.Error("evolveProfileHandler", "err", err)
			http.Error(w, "evolve failed: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
			"status": "updated",
			"backup": filepath.Base(result.BackupPath),
		})
	}
}

// bodyMetricsHandler returns body composition data points for charting.
// GET /api/body-metrics
func bodyMetricsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var from, to time.Time
		if fromStr := r.URL.Query().Get("from"); fromStr != "" {
			t, err := time.Parse("2006-01-02", fromStr)
			if err != nil {
				http.Error(w, "from must be YYYY-MM-DD", http.StatusBadRequest)
				return
			}
			from = t
		}
		if toStr := r.URL.Query().Get("to"); toStr != "" {
			t, err := time.Parse("2006-01-02", toStr)
			if err != nil {
				http.Error(w, "to must be YYYY-MM-DD", http.StatusBadRequest)
				return
			}
			// Make the upper bound inclusive for the whole selected day.
			to = t.Add(24*time.Hour - time.Nanosecond)
		}

		notes, err := storage.ListBodyMetrics(db, from, to, 1000)
		if err != nil {
			slog.Error("bodyMetricsHandler", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		type dataPoint struct {
			Date         string   `json:"date"`
			WeightKG     *float64 `json:"weight_kg,omitempty"`
			BodyFatPct   *float64 `json:"body_fat_pct,omitempty"`
			MuscleMassKG *float64 `json:"muscle_mass_kg,omitempty"`
		}

		out := make([]dataPoint, 0, len(notes))
		for _, n := range notes {
			out = append(out, dataPoint{
				Date:         n.Timestamp.Format("2006-01-02"),
				WeightKG:     n.WeightKG,
				BodyFatPct:   n.BodyFatPct,
				MuscleMassKG: n.MuscleMassKG,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out) //nolint:errcheck
	}
}

// createNoteHandler creates a note, optionally linked to a workout.
// POST /api/notes
func createNoteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Type         string   `json:"type"`
			RPE          *int64   `json:"rpe"`
			WeightKG     *float64 `json:"weight_kg"`
			BodyFatPct   *float64 `json:"body_fat_pct"`
			MuscleMassKG *float64 `json:"muscle_mass_kg"`
			Note         *string  `json:"note"`
			WorkoutID    *int64   `json:"workout_id"`
			Timestamp    string   `json:"timestamp"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		noteType := storage.NoteType(req.Type)
		if noteType != storage.NoteTypeRide && noteType != storage.NoteTypeNote && noteType != storage.NoteTypeWeight {
			http.Error(w, "type must be ride, note, or weight", http.StatusBadRequest)
			return
		}

		ts := time.Now()
		if req.Timestamp != "" {
			parsed, err := time.Parse(time.RFC3339, req.Timestamp)
			if err != nil {
				http.Error(w, "timestamp must be RFC3339", http.StatusBadRequest)
				return
			}
			ts = parsed
		} else if req.WorkoutID != nil {
			workout, err := storage.GetWorkoutWithMetricsByID(db, *req.WorkoutID)
			if err == nil {
				ts = workout.StartedAt
			}
		}

		id, err := storage.InsertNote(db, &storage.AthleteNote{
			Timestamp:    ts,
			Type:         noteType,
			RPE:          req.RPE,
			WeightKG:     req.WeightKG,
			BodyFatPct:   req.BodyFatPct,
			MuscleMassKG: req.MuscleMassKG,
			Note:         req.Note,
			WorkoutID:    req.WorkoutID,
		})
		if err != nil {
			slog.Error("createNoteHandler", "err", err)
			http.Error(w, "create failed: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"status": "created", "id": id}) //nolint:errcheck
	}
}

// listNotesHandler returns athlete notes as JSON for the admin UI.
// GET /api/notes?limit=200
// GET /api/notes?workout_id=42  (returns notes linked to a specific workout)
func listNotesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var notes []storage.AthleteNote
		var err error

		noteType := r.URL.Query().Get("type") // optional: "ride" or "note"

		if wid := r.URL.Query().Get("workout_id"); wid != "" {
			workoutID, parseErr := strconv.ParseInt(wid, 10, 64)
			if parseErr != nil || workoutID <= 0 {
				http.Error(w, "invalid workout_id", http.StatusBadRequest)
				return
			}
			notes, err = storage.ListNotesByWorkout(db, workoutID, noteType)
		} else {
			limit := 500
			if l := r.URL.Query().Get("limit"); l != "" {
				if v, parseErr := strconv.Atoi(l); parseErr == nil && v > 0 && v <= 2000 {
					limit = v
				}
			}
			notes, err = storage.ListAllNotes(db, limit)
		}
		if err != nil {
			slog.Error("listNotesHandler", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		type noteJSON struct {
			ID           int64    `json:"id"`
			Timestamp    string   `json:"timestamp"`
			Type         string   `json:"type"`
			RPE          *int64   `json:"rpe,omitempty"`
			WeightKG     *float64 `json:"weight_kg,omitempty"`
			BodyFatPct   *float64 `json:"body_fat_pct,omitempty"`
			MuscleMassKG *float64 `json:"muscle_mass_kg,omitempty"`
			Note         *string  `json:"note,omitempty"`
			WorkoutID    *int64   `json:"workout_id,omitempty"`
		}

		out := make([]noteJSON, 0, len(notes))
		for _, n := range notes {
			out = append(out, noteJSON{
				ID:           n.ID,
				Timestamp:    n.Timestamp.Format("2006-01-02 15:04"),
				Type:         string(n.Type),
				RPE:          n.RPE,
				WeightKG:     n.WeightKG,
				BodyFatPct:   n.BodyFatPct,
				MuscleMassKG: n.MuscleMassKG,
				Note:         n.Note,
				WorkoutID:    n.WorkoutID,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out) //nolint:errcheck
	}
}

// updateNoteHandler updates mutable fields of a note.
// PUT /api/notes/{id}
func updateNoteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || id <= 0 {
			http.Error(w, "invalid note id", http.StatusBadRequest)
			return
		}

		var req struct {
			Type         string   `json:"type"`
			RPE          *int64   `json:"rpe"`
			WeightKG     *float64 `json:"weight_kg"`
			BodyFatPct   *float64 `json:"body_fat_pct"`
			MuscleMassKG *float64 `json:"muscle_mass_kg"`
			Note         *string  `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		noteType := storage.NoteType(req.Type)
		if noteType != storage.NoteTypeRide && noteType != storage.NoteTypeNote && noteType != storage.NoteTypeWeight {
			http.Error(w, "type must be ride, note, or weight", http.StatusBadRequest)
			return
		}

		note := &storage.AthleteNote{
			ID:           id,
			Type:         noteType,
			RPE:          req.RPE,
			WeightKG:     req.WeightKG,
			BodyFatPct:   req.BodyFatPct,
			MuscleMassKG: req.MuscleMassKG,
			Note:         req.Note,
		}

		if err := storage.UpdateNote(db, note); err != nil {
			slog.Error("updateNoteHandler", "id", id, "err", err)
			http.Error(w, "update failed: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "updated"}) //nolint:errcheck
	}
}

// deleteNoteHandler deletes a note by ID.
// DELETE /api/notes/{id}
func deleteNoteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || id <= 0 {
			http.Error(w, "invalid note id", http.StatusBadRequest)
			return
		}

		if err := storage.DeleteNote(db, id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			slog.Error("deleteNoteHandler", "id", id, "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "deleted"}) //nolint:errcheck
	}
}

// workoutDataHandler returns formatted per-workout data blocks for the admin UI.
// GET /api/workouts/{id}/data
func workoutDataHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || id <= 0 {
			http.Error(w, "invalid workout id", http.StatusBadRequest)
			return
		}

		workout, err := storage.GetWorkoutWithMetricsByID(db, id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			slog.Error("workoutDataHandler", "id", id, "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		resp := map[string]string{
			"summary_table": reporting.FormatWorkoutSummaryTable(workout),
			"zone_detail":   reporting.FormatWorkoutZoneDetail(workout),
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}
}

// workoutTimeSeriesHandler downloads parsed FIT records as CSV for a workout.
// GET /api/workouts/{id}/timeseries.csv
func workoutTimeSeriesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || id <= 0 {
			http.Error(w, "invalid workout id", http.StatusBadRequest)
			return
		}

		workout, err := storage.GetWorkoutWithMetricsByID(db, id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			slog.Error("workoutTimeSeriesHandler", "id", id, "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if workout.FITFilePath == nil || *workout.FITFilePath == "" {
			http.Error(w, "no FIT file available", http.StatusNotFound)
			return
		}

		parsed, err := fitpkg.ParseFile(*workout.FITFilePath)
		if err != nil {
			slog.Error("workoutTimeSeriesHandler: parse fit", "id", id, "err", err)
			http.Error(w, "failed to parse FIT file", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=\"workout-"+workout.WahooID+"-timeseries.csv\"")

		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"timestamp", "heart_rate_bpm", "power_watts", "cadence_rpm", "distance_m", "speed_ms"})
		for _, rec := range parsed.Records {
			_ = cw.Write([]string{
				rec.Timestamp.Format(time.RFC3339),
				strconv.Itoa(int(rec.HeartRate)),
				strconv.Itoa(int(rec.Power)),
				strconv.Itoa(int(rec.Cadence)),
				strconv.FormatFloat(rec.DistanceM, 'f', 2, 64),
				strconv.FormatFloat(rec.SpeedMS, 'f', 3, 64),
			})
		}
		cw.Flush()
	}
}

// reportPageHandler serves the stored HTML for a report or plan by its numeric ID.
// Used for GET /reports/{id} and GET /plans/{id}.
func reportPageHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || id <= 0 {
			http.Error(w, "invalid report id", http.StatusBadRequest)
			return
		}

		report, err := storage.GetReportByID(db, id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			slog.Error("reportPageHandler: get report", "id", id, "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		if report.FullHTML == nil || *report.FullHTML == "" {
			http.Error(w, "report has no HTML content", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(*report.FullHTML)) //nolint:errcheck
	}
}

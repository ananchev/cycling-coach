package web

import (
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"

	"cycling-coach/internal/analysis"
	"cycling-coach/internal/config"
	"cycling-coach/internal/reporting"
	"cycling-coach/internal/wahoo"
)

// NewRouter constructs the HTTP router with all middleware and routes wired.
func NewRouter(cfg *config.Config, db *sql.DB, auth *wahoo.AuthHandler, syncer *wahoo.Syncer, webhookHandler *wahoo.WebhookHandler, orch *reporting.Orchestrator, delivery *reporting.DeliveryService, proc *analysis.Processor, logs *LogBroadcaster) http.Handler {
	r := chi.NewRouter()

	r.Use(recoveryMiddleware)
	r.Use(loggingMiddleware)

	r.Get("/health", healthHandler)
	r.Get("/admin", adminHandler(db))

	// Wahoo OAuth2 flow — bypassed by Cloudflare Access (see ARCHITECTURE.md §16).
	r.Get("/wahoo/authorize", auth.Authorize)
	r.Get("/wahoo/callback", auth.Callback)

	// Wahoo webhook — bypassed by Cloudflare Access (Wahoo must reach this).
	r.Post("/wahoo/webhook", webhookHandler.Handle)

	// Report web views — protected by Cloudflare Access in production.
	r.Get("/reports/{id}", reportPageHandler(db))
	r.Get("/plans/{id}", reportPageHandler(db))

	// Admin endpoints — protected by Cloudflare Access in production.
	r.Post("/api/sync", syncHandler(syncer))
	r.Post("/api/process", processHandler(proc))
	r.Post("/api/workout/reset-fit", resetFITHandler(proc))
	r.Post("/api/workout/ignore", ignoreFITHandler(db))
	r.Post("/api/report", reportHandler(orch))
	r.Post("/api/report/send", sendReportHandler(delivery))
	r.Delete("/api/report/{id}", deleteReportHandler(db))
	r.Post("/api/profile/evolve", evolveProfileHandler(orch))

	// Body metrics & note management.
	r.Get("/api/body-metrics", bodyMetricsHandler(db))
	r.Get("/api/notes", listNotesHandler(db))
	r.Put("/api/notes/{id}", updateNoteHandler(db))
	r.Delete("/api/notes/{id}", deleteNoteHandler(db))

	// Live log stream — Server-Sent Events endpoint for the admin log panel.
	r.Get("/api/logs/stream", logStreamHandler(logs))

	return r
}

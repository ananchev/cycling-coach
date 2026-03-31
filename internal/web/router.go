package web

import (
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"

	"cycling-coach/internal/config"
)

// NewRouter constructs the HTTP router with all middleware and routes wired.
func NewRouter(cfg *config.Config, db *sql.DB) http.Handler {
	r := chi.NewRouter()

	r.Use(recoveryMiddleware)
	r.Use(loggingMiddleware)

	r.Get("/health", healthHandler)

	return r
}

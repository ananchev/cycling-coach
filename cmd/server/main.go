package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"cycling-coach/internal/config"
	"cycling-coach/internal/storage"
	"cycling-coach/internal/web"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config.Load", "err", err)
		os.Exit(1)
	}

	db, err := storage.Open(cfg.DatabasePath)
	if err != nil {
		slog.Error("storage.Open", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := bootstrapAthleteProfile(cfg); err != nil {
		slog.Warn("bootstrapAthleteProfile", "err", err)
	}

	if err := os.MkdirAll(cfg.FITFilesPath, 0755); err != nil {
		slog.Warn("create fit files dir", "path", cfg.FITFilesPath, "err", err)
	}

	router := web.NewRouter(cfg, db.DB())

	slog.Info("server starting", "addr", cfg.ServerAddr)
	if err := http.ListenAndServe(cfg.ServerAddr, router); err != nil {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

// bootstrapAthleteProfile copies the seed profile to the data volume on first run.
func bootstrapAthleteProfile(cfg *config.Config) error {
	if _, err := os.Stat(cfg.AthleteProfilePath); err == nil {
		return nil // already exists
	}

	if err := os.MkdirAll(filepath.Dir(cfg.AthleteProfilePath), 0755); err != nil {
		return fmt.Errorf("bootstrapAthleteProfile: mkdir: %w", err)
	}

	data, err := os.ReadFile("config/athlete-profile.default.md")
	if err != nil {
		return fmt.Errorf("bootstrapAthleteProfile: read seed: %w", err)
	}

	if err := os.WriteFile(cfg.AthleteProfilePath, data, 0644); err != nil {
		return fmt.Errorf("bootstrapAthleteProfile: write: %w", err)
	}

	slog.Info("athlete profile bootstrapped", "path", cfg.AthleteProfilePath)
	return nil
}

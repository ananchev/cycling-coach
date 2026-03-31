package storage

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

// DB wraps a *sql.DB and owns the connection lifecycle.
type DB struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database at the given path, applies pragmas,
// and runs all schema migrations.
func Open(path string) (*DB, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("storage.Open: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("storage.Open: ping: %w", err)
	}

	// Single-writer system; WAL improves concurrent reads.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, fmt.Errorf("storage.Open: WAL pragma: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		return nil, fmt.Errorf("storage.Open: foreign_keys pragma: %w", err)
	}

	if err := migrate(db); err != nil {
		return nil, fmt.Errorf("storage.Open: migrate: %w", err)
	}

	return &DB{db: db}, nil
}

// DB returns the underlying *sql.DB for use by other packages.
func (d *DB) DB() *sql.DB {
	return d.db
}

// Close closes the underlying database connection.
func (d *DB) Close() error {
	return d.db.Close()
}

// migrate runs all schema migrations idempotently.
func migrate(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("migrate: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	for _, stmt := range migrations {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("migrate: exec: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migrate: commit: %w", err)
	}
	return nil
}

var migrations = []string{
	`CREATE TABLE IF NOT EXISTS wahoo_tokens (
		id            INTEGER PRIMARY KEY,
		access_token  TEXT NOT NULL,
		refresh_token TEXT NOT NULL,
		expires_at    DATETIME NOT NULL,
		updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP
	)`,

	`CREATE TABLE IF NOT EXISTS workouts (
		id           INTEGER PRIMARY KEY AUTOINCREMENT,
		wahoo_id     TEXT UNIQUE NOT NULL,
		started_at   DATETIME NOT NULL,
		duration_sec INTEGER,
		distance_m   REAL,
		calories     INTEGER,
		avg_hr       INTEGER,
		max_hr       INTEGER,
		avg_power    REAL,
		max_power    REAL,
		avg_cadence  REAL,
		workout_type TEXT,
		fit_file_path TEXT,
		source       TEXT DEFAULT 'api' CHECK(source IN ('api','csv','manual')),
		processed    BOOLEAN DEFAULT 0,
		created_at   DATETIME DEFAULT CURRENT_TIMESTAMP
	)`,

	`CREATE TABLE IF NOT EXISTS ride_metrics (
		id                INTEGER PRIMARY KEY AUTOINCREMENT,
		workout_id        INTEGER UNIQUE REFERENCES workouts(id),
		duration_min      REAL,
		avg_hr            REAL,
		max_hr            REAL,
		avg_power         REAL,
		max_power         REAL,
		avg_cadence       REAL,
		normalized_power  REAL,
		intensity_factor  REAL,
		tss               REAL,
		trimp             REAL,
		efficiency_factor REAL,
		hr_drift_pct      REAL,
		decoupling_pct    REAL,
		hr_z1_pct REAL, hr_z2_pct REAL, hr_z3_pct REAL, hr_z4_pct REAL, hr_z5_pct REAL,
		pwr_z1_pct REAL, pwr_z2_pct REAL, pwr_z3_pct REAL, pwr_z4_pct REAL, pwr_z5_pct REAL,
		created_at        DATETIME DEFAULT CURRENT_TIMESTAMP
	)`,

	`CREATE TABLE IF NOT EXISTS athlete_notes (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp  DATETIME NOT NULL,
		type       TEXT NOT NULL CHECK(type IN ('ride','note','weight')),
		rpe        INTEGER CHECK(rpe BETWEEN 1 AND 10),
		weight_kg  REAL,
		note       TEXT,
		workout_id INTEGER REFERENCES workouts(id),
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`,

	`CREATE TABLE IF NOT EXISTS athlete_config (
		key        TEXT PRIMARY KEY,
		value      TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`,

	`CREATE TABLE IF NOT EXISTS reports (
		id           INTEGER PRIMARY KEY AUTOINCREMENT,
		type         TEXT NOT NULL CHECK(type IN ('weekly_report','weekly_plan')),
		week_start   DATE NOT NULL,
		week_end     DATE NOT NULL,
		summary_text TEXT,
		full_html    TEXT,
		created_at   DATETIME DEFAULT CURRENT_TIMESTAMP
	)`,
}

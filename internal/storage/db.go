package storage

import (
	"database/sql"
	"fmt"
	"strings"

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
			// ALTER TABLE ADD COLUMN fails if the column already exists.
			// SQLite has no IF NOT EXISTS variant, so we tolerate duplicate-column errors.
			if strings.Contains(err.Error(), "duplicate column name") {
				continue
			}
			return fmt.Errorf("migrate: exec: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migrate: commit: %w", err)
	}
	return nil
}

const seedWorkoutTypes = `INSERT OR IGNORE INTO workout_types(id, description, location, family) VALUES
(0,'Biking','outdoor','biking'),
(1,'Running','outdoor','running'),
(2,'FE','indoor',''),
(3,'Running Track','outdoor','track'),
(4,'Running Trail','outdoor','trail'),
(5,'Running Treadmill','indoor','running'),
(6,'Walking','outdoor','walking'),
(7,'Walking Speed','outdoor','walking'),
(8,'Walking Nordic','outdoor','walking'),
(9,'Hiking','outdoor','walking'),
(10,'Mountaineering','outdoor','walking'),
(11,'Biking Cyclocross','outdoor','biking'),
(12,'Biking Indoor','indoor','biking'),
(13,'Biking Mountain','outdoor','biking'),
(14,'Biking Recumbent','outdoor','biking'),
(15,'Biking Road','outdoor','biking'),
(16,'Biking Track','outdoor','biking'),
(17,'Biking Motorcycling','outdoor','biking'),
(18,'FE General','indoor',''),
(19,'FE Treadmill','indoor',''),
(20,'FE Elliptical','indoor','gym'),
(21,'FE Bike','indoor',''),
(22,'FE Rower','indoor','gym'),
(23,'FE Climber','indoor',''),
(25,'Swimming Lap','indoor','swimming'),
(26,'Swimming Open Water','outdoor','swimming'),
(27,'Snowboarding','outdoor','snow_sport'),
(28,'Skiing','outdoor','snow_sport'),
(29,'Skiing Downhill','outdoor','snow_sport'),
(30,'Skiing Cross Country','outdoor','snow_sport'),
(31,'Skating','outdoor','skating'),
(32,'Skating Ice','indoor','skating'),
(33,'Skating Inline','indoor','skating'),
(34,'Long Boarding','outdoor','skating'),
(35,'Sailing','outdoor','water_sports'),
(36,'Windsurfing','outdoor','water_sports'),
(37,'Canoeing','outdoor','water_sports'),
(38,'Kayaking','outdoor','water_sports'),
(39,'Rowing','outdoor','water_sports'),
(40,'Kiteboarding','outdoor','water_sports'),
(41,'Stand Up Paddle Board','outdoor','water_sports'),
(42,'Workout','indoor','gym'),
(43,'Cardio Class','indoor','gym'),
(44,'Stair Climber','indoor','gym'),
(45,'Wheelchair','outdoor','other'),
(46,'Golfing','outdoor','other'),
(47,'Other','outdoor','other'),
(49,'Biking Indoor Cycling Class','indoor','biking'),
(56,'Walking Treadmill','indoor','walking'),
(61,'Biking Indoor Trainer','indoor','biking'),
(62,'Multisport','outdoor',''),
(63,'Transition','outdoor',''),
(64,'E-Biking','outdoor','biking'),
(65,'TICKR Offline','outdoor',''),
(66,'Yoga','indoor','gym'),
(67,'Running Race','outdoor','running'),
(68,'Biking Indoor Virtual','indoor','biking'),
(69,'Mental Strength','indoor','other'),
(70,'Handcycling','outdoor','biking'),
(71,'Running Indoor Virtual','indoor','running'),
(255,'Unknown','','')
`

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
		cad_lt70_pct REAL, cad_70_85_pct REAL, cad_85_100_pct REAL, cad_ge100_pct REAL,
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

	`CREATE TABLE IF NOT EXISTS wyze_scale_imports (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		wyze_record_id  TEXT NOT NULL UNIQUE,
		athlete_note_id INTEGER NOT NULL REFERENCES athlete_notes(id),
		measured_at     DATETIME NOT NULL,
		payload_hash    TEXT NOT NULL,
		raw_payload_json TEXT,
		created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_seen_at    DATETIME DEFAULT CURRENT_TIMESTAMP
	)`,

	`CREATE TABLE IF NOT EXISTS wyze_scale_conflicts (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		wyze_record_id  TEXT NOT NULL UNIQUE,
		manual_note_id  INTEGER NOT NULL REFERENCES athlete_notes(id),
		wyze_note_id    INTEGER NOT NULL REFERENCES athlete_notes(id),
		conflict_type   TEXT NOT NULL DEFAULT 'conflict_with_manual',
		created_at      DATETIME DEFAULT CURRENT_TIMESTAMP
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
		system_prompt TEXT NOT NULL DEFAULT '',
		user_prompt   TEXT NOT NULL DEFAULT '',
		full_html    TEXT,
		created_at   DATETIME DEFAULT CURRENT_TIMESTAMP
	)`,

	`CREATE TABLE IF NOT EXISTS progress_analyses (
		id             INTEGER PRIMARY KEY CHECK(id = 1),
		period_from    DATE NOT NULL,
		period_to      DATE NOT NULL,
		snapshot_json  TEXT NOT NULL,
		system_prompt  TEXT NOT NULL DEFAULT '',
		user_prompt    TEXT NOT NULL DEFAULT '',
		narrative_text TEXT NOT NULL,
		created_at     DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at     DATETIME DEFAULT CURRENT_TIMESTAMP
	)`,

	// Indexes for common query patterns.
	`CREATE INDEX IF NOT EXISTS idx_workouts_started_at ON workouts(started_at)`,
	`CREATE INDEX IF NOT EXISTS idx_workouts_processed  ON workouts(processed)`,
	`CREATE INDEX IF NOT EXISTS idx_notes_timestamp     ON athlete_notes(timestamp)`,
	`CREATE INDEX IF NOT EXISTS idx_notes_workout_id    ON athlete_notes(workout_id)`,
	`CREATE INDEX IF NOT EXISTS idx_wyze_imports_measured_at ON wyze_scale_imports(measured_at)`,
	`CREATE INDEX IF NOT EXISTS idx_wyze_conflicts_manual_note_id ON wyze_scale_conflicts(manual_note_id)`,
	`CREATE INDEX IF NOT EXISTS idx_wyze_conflicts_wyze_note_id ON wyze_scale_conflicts(wyze_note_id)`,
	`CREATE INDEX IF NOT EXISTS idx_reports_week_start  ON reports(week_start)`,
	`CREATE INDEX IF NOT EXISTS idx_progress_period_from ON progress_analyses(period_from)`,

	// Unique constraint on reports(type, week_start) required for ON CONFLICT upserts.
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_reports_type_week ON reports(type, week_start)`,

	// report_deliveries tracks outbound delivery attempts per report per channel.
	// UNIQUE(report_id, channel) enforces one delivery record per report, enabling
	// idempotent send logic: INSERT OR IGNORE to claim, then check status before sending.
	`CREATE TABLE IF NOT EXISTS report_deliveries (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		report_id       INTEGER NOT NULL REFERENCES reports(id),
		channel         TEXT NOT NULL DEFAULT 'telegram',
		status          TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','sent','failed')),
		telegram_msg_id INTEGER,
		attempted_at    DATETIME,
		sent_at         DATETIME,
		error           TEXT,
		created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(report_id, channel)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_deliveries_status ON report_deliveries(status)`,

	// Migration: add narrative_text column to reports for full plan comparison.
	`ALTER TABLE reports ADD COLUMN narrative_text TEXT`,
	`ALTER TABLE reports ADD COLUMN system_prompt TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE reports ADD COLUMN user_prompt TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE progress_analyses ADD COLUMN system_prompt TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE progress_analyses ADD COLUMN user_prompt TEXT NOT NULL DEFAULT ''`,

	// Workout types reference table (Wahoo master data).
	`CREATE TABLE IF NOT EXISTS workout_types (
		id          INTEGER PRIMARY KEY,
		description TEXT NOT NULL,
		location    TEXT NOT NULL DEFAULT '',
		family      TEXT NOT NULL DEFAULT ''
	)`,

	// Add workout_type_id integer column to workouts for proper FK to workout_types.
	`ALTER TABLE workouts ADD COLUMN workout_type_id INTEGER`,

	// Seed workout types master data.
	seedWorkoutTypes,

	// Backfill workout_type_id from existing workout_type text where possible.
	// Handles "wahoo_type_<id>" format and known legacy names.
	`UPDATE workouts SET workout_type_id = CAST(REPLACE(workout_type, 'wahoo_type_', '') AS INTEGER)
	 WHERE workout_type_id IS NULL AND workout_type LIKE 'wahoo_type_%'`,

	// Migration: add power zone timeline JSON to ride_metrics.
	`ALTER TABLE ride_metrics ADD COLUMN zone_timeline TEXT`,
	`ALTER TABLE ride_metrics ADD COLUMN hr_zone_timeline TEXT`,
	`ALTER TABLE ride_metrics ADD COLUMN cad_lt70_pct REAL`,
	`ALTER TABLE ride_metrics ADD COLUMN cad_70_85_pct REAL`,
	`ALTER TABLE ride_metrics ADD COLUMN cad_85_100_pct REAL`,
	`ALTER TABLE ride_metrics ADD COLUMN cad_ge100_pct REAL`,

	// Migration: add body composition columns to athlete_notes.
	`ALTER TABLE athlete_notes ADD COLUMN body_fat_pct REAL`,
	`ALTER TABLE athlete_notes ADD COLUMN muscle_mass_kg REAL`,
	`ALTER TABLE athlete_notes ADD COLUMN body_water_pct REAL`,
	`ALTER TABLE athlete_notes ADD COLUMN bmr_kcal REAL`,
}

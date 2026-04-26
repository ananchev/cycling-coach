# Cycling Coach — Implementation-Aligned Architecture

This document summarizes the architecture currently implemented in the repository. It is intentionally narrower than the older design document it replaces.

If this document drifts from the code, the code wins.

## 1. System Overview

```text
Wahoo OAuth + API + webhook
        |
        v
  internal/wahoo
        |
        v
   SQLite workouts table  <---- Telegram notes/body metrics
        |                           |
        v                           v
   FIT files on disk          athlete_notes table
                                   ^
                                   |
                           Wyze Python sidecar
                                   ^
                                   |
                              internal/wyze
        |
        v
  internal/fit + internal/analysis
        |
        v
    ride_metrics table
        |
        v
 internal/reporting assembler
        |
        v
  Claude API (report / plan / progress generation)
        |
        v
 reports table (summary, narrative, HTML)
        |
        +--> /reports/{id}, /plans/{id}
        |
        +--> Telegram delivery + report_deliveries
        |
        +--> saved system/user prompts

progress_analyses table (single saved interpretation + prompts)
```

## 2. Startup Flow

`cmd/server/main.go` performs the runtime wiring in this order:

1. load config from environment
2. configure slog plus the in-memory log broadcaster used by `/api/logs/stream`
3. open SQLite and run migrations
4. bootstrap the athlete profile file if it does not exist
5. create the FIT files directory
6. build Wahoo auth/client/sync/webhook components
7. seed default `athlete_config` values if keys are missing
8. build the FIT processor
9. build the Claude provider and report orchestrator
10. optionally enable Telegram delivery and the inbound Telegram bot
11. register scheduler jobs if cron env vars are set
12. start the HTTP server

## 3. Packages And Responsibilities

### `internal/config`

- loads runtime environment variables
- provides optional cron expressions for the scheduler

### `internal/storage`

- opens SQLite
- applies schema migrations on startup
- owns CRUD helpers for tokens, workouts, metrics, notes, Wyze imports/conflicts/body-metric records, reports, progress analyses, deliveries, and athlete config
- owns placeholder-workout reconciliation for no-training days

### `internal/wyze`

- calls the local Wyze sidecar
- normalizes the sidecar response into Go records
- imports Wyze body metrics into `athlete_notes`
- tracks idempotency with `wyze_scale_imports`
- tracks explicit manual-vs-Wyze conflicts in `wyze_scale_conflicts`

### `internal/wahoo`

- handles OAuth authorize/callback flow
- stores and refreshes OAuth tokens
- lists workouts through the Wahoo API
- downloads FIT files
- ingests workouts via sync or webhook
- uses separate payload mapping for the polling API shape and the documented webhook shape

### `internal/fit`

- parses FIT activity files into session summaries plus per-record streams

### `internal/analysis`

- loads zone configuration from `athlete_config`
- computes ride metrics
- processes unprocessed workouts or explicit ranges
- supports FIT reset for re-download and reprocessing workflows

### `internal/reporting`

- assembles report input from profile, workouts, metrics, and notes
- builds Claude prompts
- calls the Anthropic Messages API
- renders report HTML
- stores generated reports
- stores saved system/user prompts for reports, plans, and progress interpretations
- sends Telegram deliveries
- patches the athlete profile after each block close (recent-weeks table, milestone statuses, last-updated date)
- evolves the athlete profile from recent report history
- formats admin-only workout summary/zone-detail previews
- generates progress interpretations from aggregated KPI snapshots

### `internal/telegram`

- runs the inbound bot in long-polling mode
- saves notes and body metrics
- serves the current athlete profile or replaces it from an uploaded markdown file
- provides the outbound sender abstraction used by report delivery

### `internal/web`

- registers routes
- exposes the admin page and JSON APIs
- serves stored report HTML
- provides request logging, recovery, and live log streaming
- serves workout data preview and FIT time-series download endpoints
- serves progress snapshot / interpretation endpoints and saved prompt views
- serves Wyze sync, records, and conflict endpoints

### `internal/scheduler`

- registers cron jobs only when env vars are present
- always registers a fixed daily placeholder-workout job at 23:50 Europe/Amsterdam
- runs Wahoo sync
- runs FIT processing
- runs scheduled report/plan generation and then delivery

## 4. Current Runtime Features

### Wahoo integration

- OAuth2 authorize/callback flow
- DB-backed token refresh through `oauth2.ReuseTokenSource`
- manual sync via `/api/sync`
- webhook ingestion via `/wahoo/webhook`
- idempotent workout insert keyed by `wahoo_id`
- optional FIT download when the workout payload contains a file URL
- webhook-specific mapping from:
  - `workout_summary.workout.id`
  - `workout_summary.workout.starts`
  - `workout_summary.workout.workout_type_id`
  - `workout_summary.file.url`

### Workout analysis

- parses FIT files from disk
- marks workouts with missing FIT files as processed without metrics
- leaves corrupt FIT files unprocessed so they can be reset and retried
- can export parsed FIT record streams as CSV through the admin/API layer
- computes:
  - duration
  - average/max HR
  - average/max power
  - average cadence
  - normalized power
  - intensity factor
  - TSS
  - TRIMP
  - efficiency factor
  - HR drift
  - decoupling
  - HR zone percentages
  - power zone percentages
  - cadence distribution percentages in four bands: `<70`, `70-85`, `85-100`, `100+`
  - power zone timeline JSON
  - HR zone timeline JSON

### Reporting

- report periods and plan periods share the same generation pipeline
- the main admin workflow is now “close block -> generate report -> patch profile -> generate next 7-day plan”
- after the report is generated, a lightweight profile patch updates three structured sections: the recent-weeks rolling table, Stelvio readiness milestone statuses, and the last-updated date
- the patch runs automatically as part of the close-block flow so the plan benefits from the freshest profile context
- patch failure is non-fatal — logged as a warning, plan generation continues regardless
- the patch step only runs when the provider is a `*ClaudeProvider` (skipped in tests with stub providers)
- reports optionally include the prior plan narrative for plan-vs-actual comparison
- plan generation receives continuity context: up to the 3 most recent weekly_report narratives are attached to the prompt (oldest first, with the just-ended period marked) so Claude extends recent recommendations rather than restarting progression at lower volumes
- both the close-block flow and the scheduled weekly cron path apply this continuity context to plan generation; report generation never receives it
- when execution extends beyond the originally planned 7-day block, the prompt explicitly tells Claude to interpret that drift
- the Claude prompt now includes a structured body-metrics block for the selected period, including weight, body fat, muscle mass, body water, and BMR when available
- output is expected from Claude as JSON with:
  - `summary`
  - `narrative`
- HTML is rendered and stored in the `reports` table
- saved system/user prompts are stored with each report row

### Wyze integration

- the Go app never talks to Wyze directly; it calls a local Python sidecar
- the sidecar uses the Wyze SDK and returns normalized scale records
- sync can be triggered manually from the admin/API layer or on a cron schedule
- imports are idempotent through `wyze_scale_imports`
- explicit overlapping manual-vs-Wyze imports are tracked in `wyze_scale_conflicts`
- split manual rows that match a same-day Wyze row are inferred as duplicates in the admin view even when no explicit conflict row exists
- body-metric charts suppress manual rows that duplicate same-day Wyze imports

### Progress

- the admin UI includes a dedicated Progress page
- selected period = chosen `from` date through today
- prior period = immediately preceding equal-length window
- a single saved interpretation is persisted in `progress_analyses`
- saved system/user prompts for that interpretation are viewable from the admin UI

### Delivery

- Telegram delivery is optional
- idempotency is tracked in `report_deliveries`
- failed sends are persisted and can be retried
- `SendAllUndelivered` sends all reports whose delivery is missing or not marked `sent`

### Admin and operations

- admin UI at `/admin`
- JSON endpoints for sync, process, report generation, delivery, note creation/editing, body metrics, workout data preview, FIT CSV download, and log streaming
- report/plan HTML served directly from the database
- body metrics support date filtering in the UI and backend
- workout rows expose note-state icons plus summary/zone/timeseries actions
- the Reports & Plans table is grouped by period: each row pairs the plan and the report for the same `[week_start, week_end]` window side-by-side, with `—` shown when one side hasn't been generated yet

### Placeholder workout behavior

- at 23:50 Europe/Amsterdam, the scheduler creates a manual placeholder workout for the day if no workout exists yet
- placeholder workouts are marked processed immediately and do not expect FIT files
- notes can be attached to the placeholder day
- if a real Wahoo workout later arrives for that same day, the placeholder is deleted and its notes are moved to the real workout

## 5. Current Data Model

The schema is defined in [`internal/storage/db.go`](/Users/ananchev/Development/cycling-coach/internal/storage/db.go).

Main tables:

- `wahoo_tokens`
- `workouts`
- `ride_metrics`
- `athlete_notes`
- `wyze_scale_imports`
- `wyze_scale_conflicts`
- `athlete_config`
- `reports`
- `progress_analyses`
- `report_deliveries`
- `workout_types`

Notable schema details:

- `reports` is unique on `(type, week_start)`
- `reports` stores the exact saved `system_prompt` and `user_prompt` used for generation
- `progress_analyses` is a single-row table keyed to `id = 1`
- `report_deliveries` is unique on `(report_id, channel)`
- `workouts.processed` drives the default FIT-processing queue
- `ride_metrics.zone_timeline` stores the power-zone timeline JSON generated by analysis
- `ride_metrics.hr_zone_timeline` stores the HR-zone timeline JSON generated by analysis
- `ride_metrics` also stores cadence-distribution percentages used in the Claude per-ride detail and admin workout modal
- `athlete_notes` stores body metrics under `type='weight'`, including `body_water_pct` and `bmr_kcal`
- `wyze_scale_imports` stores the stable mapping from a Wyze record to an `athlete_notes` row
- `wyze_scale_conflicts` stores explicit manual-vs-Wyze conflicts when both were imported as separate rows

## 6. Scheduler Behavior

The scheduler is present in code but inactive by default unless env vars are provided.

Implemented cron env vars:

- `CRON_SYNC`
- `CRON_FIT_PROCESSING`
- `CRON_WEEKLY_REPORT`
- `CRON_WYZE_SCALE_SYNC`

Behavior:

- no cron string means that job is not registered
- if all are empty, the scheduler starts with no jobs
- all jobs use the `Europe/Amsterdam` timezone
- one additional fixed job always exists: `23:50` daily placeholder workout creation

## 7. Athlete Profile Behavior

Current implementation:

- the runtime markdown file is created from `config/athlete-profile.default.md` on first startup if missing
- report and plan generation send the raw markdown file to Claude as the system prompt
- Telegram `/profile` returns the file
- Telegram `/profile set` replaces the file with an uploaded markdown attachment
- `/api/profile/evolve` rewrites the profile using recent reports and validates required sections
- automatic profile patch on block close updates three structured sections without a full rewrite
- both the patcher and evolver validate that all 8 protected section headings survive every write
- the profile is backed up with a timestamp suffix before every patch or evolution write

Current limitation:

- there is no implemented profile parser/sync path from markdown back into `athlete_config`
- the analysis engine reads numeric values from `athlete_config`
- startup seeds `athlete_config` with defaults in `cmd/server/main.go`

## 8. Active HTTP Surface

### Public/integration routes

- `GET /health`
- `GET /wahoo/authorize`
- `GET /wahoo/callback`
- `POST /wahoo/webhook`

### Report pages

- `GET /reports/{id}`
- `GET /plans/{id}`

### Admin/API routes

- `GET /admin`
- `POST /api/sync`
- `POST /api/wyze/sync`
- `POST /api/process`
- `POST /api/workout/reset-fit`
- `POST /api/workout/ignore`
- `POST /api/report`
- `POST /api/report/close-block`
- `POST /api/report/send`
- `DELETE /api/report/{id}`
- `GET /api/report/{id}/prompts`
- `POST /api/profile/evolve`
- `GET /api/progress`
- `POST /api/progress/interpret`
- `GET /api/body-metrics`
- `GET /api/wyze/conflicts`
- `GET /api/wyze/records`
- `DELETE /api/wyze/conflicts/{id}`
- `DELETE /api/wyze/records/{id}`
- `GET /api/workouts/{id}/data`
- `GET /api/workouts/{id}/timeseries.csv`
- `POST /api/notes`
- `GET /api/notes`
- `PUT /api/notes/{id}`
- `DELETE /api/notes/{id}`
- `GET /api/logs/stream`

## 9. Active Telegram Surface

Implemented commands:

- `/help`
- `/start`
- `/ride`
- `/note`
- `/weight`
- `/bodyfat`
- `/muscle`
- `/profile`
- `/profile set`

Older design-doc commands such as `/status`, `/week`, and `/plan` do not exist in the current implementation.

## 10. Rendering Path

The active report rendering path is [`internal/reporting/renderer.go`](/Users/ananchev/Development/cycling-coach/internal/reporting/renderer.go), which renders HTML from an inline Go template.

Rendered titles and headers now speak in terms of `Training Report`, `Training Plan`, and explicit `Period:` ranges rather than assuming a fixed calendar week.

The admin UI also has a separate display-only formatting path in [`internal/reporting/ride_view.go`](/Users/ananchev/Development/cycling-coach/internal/reporting/ride_view.go) for:

- workout summary-row preview
- per-ride zone-detail preview

That path is for admin inspection only and does not alter the main Claude prompt assembly.

## 11. Known Doc Drift Resolved Here

The current codebase does not implement the following older design elements:

- `GET /api/profile`
- `POST /api/profile`
- `POST /api/profile/reload`
- markdown parsing/sync into `athlete_config`
- `cmd/backfill`
- `cmd/import-csv`
- `cmd/reset`
- Telegram `/status`
- Telegram `/week`
- Telegram `/plan`
- template-file-based report rendering
- separate visible admin cards for manual standalone report generation; the backend capability still exists, but the UI now centers the close-block workflow

Future work can add any of those later, but they should not be described as current behavior until they exist in code.

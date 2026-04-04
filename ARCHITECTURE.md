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
  Claude API (report / plan generation)
        |
        v
 reports table (summary, narrative, HTML)
        |
        +--> /reports/{id}, /plans/{id}
        |
        +--> Telegram delivery + report_deliveries
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
- owns CRUD helpers for tokens, workouts, metrics, notes, reports, deliveries, and athlete config

### `internal/wahoo`

- handles OAuth authorize/callback flow
- stores and refreshes OAuth tokens
- lists workouts through the Wahoo API
- downloads FIT files
- ingests workouts via sync or webhook

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
- sends Telegram deliveries
- evolves the athlete profile from recent report history

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

### `internal/scheduler`

- registers cron jobs only when env vars are present
- runs Wahoo sync
- runs FIT processing
- runs weekly report/plan generation and then delivery

## 4. Current Runtime Features

### Wahoo integration

- OAuth2 authorize/callback flow
- DB-backed token refresh through `oauth2.ReuseTokenSource`
- manual sync via `/api/sync`
- webhook ingestion via `/wahoo/webhook`
- idempotent workout insert keyed by `wahoo_id`
- optional FIT download when the workout payload contains a file URL

### Workout analysis

- parses FIT files from disk
- marks workouts with missing FIT files as processed without metrics
- leaves corrupt FIT files unprocessed so they can be reset and retried
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
  - power zone timeline JSON

### Reporting

- weekly reports and weekly plans share the same generation pipeline
- weekly reports optionally include the prior plan narrative for plan-vs-actual comparison
- output is expected from Claude as JSON with:
  - `summary`
  - `narrative`
- HTML is rendered and stored in the `reports` table

### Delivery

- Telegram delivery is optional
- idempotency is tracked in `report_deliveries`
- failed sends are persisted and can be retried
- `SendAllUndelivered` sends all reports whose delivery is missing or not marked `sent`

### Admin and operations

- admin UI at `/admin`
- JSON endpoints for sync, process, report generation, delivery, note editing, body metrics, and log streaming
- report/plan HTML served directly from the database

## 5. Current Data Model

The schema is defined in [`internal/storage/db.go`](/Users/ananchev/Development/cycling-coach/internal/storage/db.go).

Main tables:

- `wahoo_tokens`
- `workouts`
- `ride_metrics`
- `athlete_notes`
- `athlete_config`
- `reports`
- `report_deliveries`
- `workout_types`

Notable schema details:

- `reports` is unique on `(type, week_start)`
- `report_deliveries` is unique on `(report_id, channel)`
- `workouts.processed` drives the default FIT-processing queue
- `ride_metrics.zone_timeline` stores JSON generated by analysis
- `athlete_notes` currently also stores body metrics under `type='weight'`

## 6. Scheduler Behavior

The scheduler is present in code but inactive by default unless env vars are provided.

Implemented cron env vars:

- `CRON_SYNC`
- `CRON_FIT_PROCESSING`
- `CRON_WEEKLY_REPORT`

Behavior:

- no cron string means that job is not registered
- if all are empty, the scheduler starts with no jobs
- all jobs use the `Europe/Amsterdam` timezone

## 7. Athlete Profile Behavior

Current implementation:

- the runtime markdown file is created from `config/athlete-profile.default.md` on first startup if missing
- report and plan generation send the raw markdown file to Claude as the system prompt
- Telegram `/profile` returns the file
- Telegram `/profile set` replaces the file with an uploaded markdown attachment
- `/api/profile/evolve` rewrites the profile using recent weekly reports and validates required sections

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
- `POST /api/process`
- `POST /api/workout/reset-fit`
- `POST /api/workout/ignore`
- `POST /api/report`
- `POST /api/report/send`
- `DELETE /api/report/{id}`
- `POST /api/profile/evolve`
- `GET /api/body-metrics`
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

Inactive-but-present assets:

- [`templates/report.html`](/Users/ananchev/Development/cycling-coach/templates/report.html)
- [`templates/plan.html`](/Users/ananchev/Development/cycling-coach/templates/plan.html)
- [`static/style.css`](/Users/ananchev/Development/cycling-coach/static/style.css)

These assets are still copied into the Docker image but are not the active renderer used at runtime.

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

Future work can add any of those later, but they should not be described as current behavior until they exist in code.

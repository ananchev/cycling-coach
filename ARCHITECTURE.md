# Cycling Training Workflow — Architecture Design Document

**Version:** 3.1  
**Date:** 2026-03-30  
**Stack:** Go · Docker · SQLite · Telegram · Wahoo Cloud API · Claude API  
**Dev tool:** Claude Code in VS Code  
**Deployment:** Docker on homelab, Cloudflare + Nginx Proxy Manager  

---

## 1. System Overview

```
                                        ┌─────────────────────────────────────────┐
                                        │         Docker (homelab)                │
                                        │                                         │
 ┌──────────────┐   webhook (HTTPS)     │  ┌───────────────────────────────────┐  │
 │ Wahoo Cloud  │ ────────────────────▶ │  │        cycling-coach (Go)         │  │
 │  API         │ ◀──── poll fallback── │  │                                   │  │
 └──────────────┘                       │  │  HTTP server (:8080)              │  │
                                        │  │    /wahoo/authorize               │  │
                                        │  │    /wahoo/callback                │  │
 ┌──────────────┐   Telegram Bot API    │  │    /wahoo/webhook                 │  │
 │  Telegram    │ ◀───────────────────▶ │  │    /reports/{id}  (web view)      │  │
 │  (you)       │                       │  │    /plans/{id}    (web view)      │  │
 └──────────────┘                       │  │    /api/profile   (config)        │  │
                                        │  │                                   │  │
 ┌──────────────┐                       │  │  Telegram bot (long-polling)      │  │
 │ Claude API   │ ◀───────────────────▶ │  │  Scheduler (cron jobs)            │  │
 │ (reports)    │                       │  │  Analysis engine                   │  │
 └──────────────┘                       │  │  FIT parser                       │  │
                                        │  └──────────┬────────────────────────┘  │
 ┌──────────────┐                       │             │                           │
 │ Browser      │ ◀──── report links ── │  ┌──────────▼──────────┐               │
 │ (you)        │  (Cloudflare Access)  │  │   /data (volume)    │               │
 └──────────────┘                       │  │   cycling.db         │               │
                                        │  │   fit_files/         │               │
                                        │  │   athlete-profile.md │               │
                                        │  └─────────────────────┘               │
                                        └─────────────────────────────────────────┘
                                                         ▲
                                                         │ reverse proxy
                                        ┌────────────────┴───────────────────────┐
                                        │  Nginx Proxy Manager                   │
                                        │  coach.tonio.cc → cycling-coach:8080   │
                                        └────────────────┬───────────────────────┘
                                                         │
                                        ┌────────────────┴───────────────────────┐
                                        │  Cloudflare (DNS + TLS + Access)       │
                                        │  coach.tonio.cc → homelab IP           │
                                        └────────────────────────────────────────┘
```

---

## 2. Athlete Profile: Runtime Configuration

The athlete profile (training zones, FTP, coaching philosophy, training history) is **not part of the codebase**. It is runtime configuration that lives in the data volume and can be updated without rebuilding or redeploying.

### How it works

| Aspect | Detail |
|---|---|
| **File location** | `/data/athlete-profile.md` (inside Docker volume) |
| **Env var** | `ATHLETE_PROFILE_PATH` (default: `/data/athlete-profile.md`) |
| **Seed file** | `config/athlete-profile.default.md` ships with the repo. Copied to data volume on first startup only. |
| **Used by** | Claude API calls (as system prompt for report/plan generation) |
| **Structured values** | FTP, HRmax, weight, zone boundaries extracted and stored in `athlete_config` DB table |
| **Analysis engine reads** | `athlete_config` table (never parses the markdown for numbers) |

### Update paths

| Method | How |
|---|---|
| **Telegram** | `/profile` → bot sends current profile as document attachment. `/profile set` → send a markdown file as attachment, bot saves it and reloads config. |
| **Web API** | `GET /api/profile` → returns current markdown. `POST /api/profile` → accepts new markdown body, saves + reloads. `POST /api/profile/reload` → re-reads from disk + re-syncs DB. All protected by Cloudflare Access. |
| **Direct edit** | SSH into server, edit `/data/athlete-profile.md`, then `curl -X POST https://coach.tonio.cc/api/profile/reload` |

### Config sync flow

```
Profile markdown updated (any path)
        │
        ▼
  Save to /data/athlete-profile.md
        │
        ▼
  Parse structured values from markdown:
    - FTP, HRmax, weight
    - HR zone boundaries
    - Power zone boundaries
        │
        ▼
  Upsert into athlete_config table
        │
        ▼
  Analysis engine picks up new values on next run
```

The parser should look for the known table/list structure in the markdown (zone tables, FTP line, etc.) and extract values. If parsing fails, the app logs a warning but keeps the previous config values — never silently use stale data without flagging it.

---

## 3. Report Delivery Strategy

Telegram is the input channel and alert channel. Anything longer than a few lines gets rendered as a web page behind Cloudflare Access.

### What goes where

| Content | Channel | Format |
|---|---|---|
| **Post-ride summary** (1-2 lines) | Telegram message | `58min · 139W · 121bpm · drift 3.1% ✓` |
| **Daily notes input** (RPE, weight) | Telegram commands | `/ride 6 legs heavy` |
| **Alerts / flags** | Telegram message | `⚠️ drift 11% — check hydration` |
| **Weekly report** | Telegram summary (5 lines) + link | Full at `coach.tonio.cc/reports/2026-w14` |
| **Weekly plan** | Telegram compact (7 lines) + link | Full at `coach.tonio.cc/plans/2026-w15` |

### Telegram message format

```
📊 Week 14 Summary
Sessions: 5 · Volume: 6.2h · Avg Z2: 139W @ 120bpm
Drift: 2.8% avg (✓) · Weight: 90.4→90.1 (↓)
⚠️ Wed drift spike 9% — matched "bad sleep" note
🔗 https://coach.tonio.cc/reports/2026-w14

📋 Week 15 Plan
  Mon: Recovery 45min ≤120W
  Tue: Tempo 60min (2×12min @ 155W)
  Wed: Z2 60min 135-140W
  Thu: Z2 60min 135-140W
  Fri: Rest
  Sat: Long Z2 90min 135-140W
  Sun: Long Z2+Tempo 100min
🔗 https://coach.tonio.cc/plans/2026-w15
```

### Web report pages

- Static HTML rendered once, stored in DB (`reports.full_html`)
- Mobile-friendly, chart.js via CDN for trend charts
- Protected by Cloudflare Access
- Report: session table, HR drift chart (4-8 weeks), Z2 power trend, weight, notes timeline
- Plan: daily breakdown with watt targets, HR caps, cadence, rationale

---

## 4. Wahoo Cloud API Integration

### Registration & OAuth

1. Register at `https://developers.wahooligan.com`
2. Create app → `client_id` + `client_secret`
3. Redirect URI: `https://coach.tonio.cc/wahoo/callback`
4. Scopes: `user_read`, `workouts_read`
5. Start sandbox, upgrade to production after approval

### OAuth2 flow (standard server-side)

1. Visit `https://coach.tonio.cc/wahoo/authorize`
2. Redirects to Wahoo login → grant access
3. Wahoo redirects to `/wahoo/callback` with auth code
4. Server exchanges code for tokens, stores in DB
5. Auto-refresh before API calls (tokens expire every 2h)

Token rules: max 10 unrevoked per user; previous token only revoked after successful call with new token.

### Data ingestion

| Strategy | How | When |
|---|---|---|
| **Webhook** (primary) | Wahoo POSTs to `/wahoo/webhook` | Seconds after ride ends |
| **Polling** (fallback) | Cron: `GET /v1/workouts` every 4h | Catches missed webhooks |
| **Backfill** (one-time) | CLI fetches all historical workouts | Dev + go-live |

### Key endpoints

| Endpoint | Method | Purpose |
|---|---|---|
| `/oauth/authorize` | GET | Start OAuth flow |
| `/oauth/token` | POST | Exchange code / refresh |
| `/v1/workouts` | GET | List workouts (paginated) |
| `/v1/workouts/:id/workout_summary` | GET/POST | Summary + FIT file URL |

### Rate limits

| Interval | Sandbox | Production |
|---|---|---|
| Per 5 min | 25 | 200 |
| Per hour | 100 | 1,000 |
| Per day | 250 | 5,000 |

FIT file downloads (CDN) don't count.

---

## 5. Historical Data & Migration Strategy

### During development

1. **CSV import** (`cmd/import-csv/`): reads existing Wahoo CSV exports → workout + ride_metrics records. Primary dev/test data source.
2. **API backfill** (`cmd/backfill/`): once OAuth works, fetches historical workouts from API. Rate-limited with built-in throttling.

### Go-live

`cmd/reset/` performs:
1. Back up current DB
2. Drop and recreate all tables
3. API backfill: fetch ALL historical workouts + FIT files
4. Process all FIT files through analysis engine
5. Generate baseline status report

Then: enable webhook + scheduled jobs for production.

### CLI commands

```
cycling-coach serve          # Run server (default)
cycling-coach import-csv     # Import Wahoo CSV exports
cycling-coach backfill       # Fetch all from Wahoo API
cycling-coach reset          # Backup, wipe, reimport, reprocess
cycling-coach process        # Reprocess unprocessed FIT files
cycling-coach report         # Generate report for a given week
```

---

## 6. Telegram Bot

### Commands

| Command | Example | Effect |
|---|---|---|
| `/ride <RPE> [note]` | `/ride 6 legs heavy` | Stores RPE + note |
| `/note [text]` | `/note bad sleep` | Stores context note |
| `/weight <kg>` | `/weight 90.3` | Stores weight |
| `/status` | `/status` | → last ride + week stats |
| `/week` | `/week` | → summary + report link |
| `/plan` | `/plan` | → compact plan + detail link |
| `/profile` | `/profile` | → sends current profile as document |
| `/profile set` | (attach .md file) | → updates athlete profile |

### Behavior

- Passive: no unsolicited messages except post-ride prompt and Sunday report
- Auto-links notes to nearest workout (±2h window)
- Only responds to `TELEGRAM_CHAT_ID`

---

## 7. FIT File Parsing

**Library:** `github.com/muktihari/fit` — actively maintained, FIT Protocol V2, faster than alternatives.

**Extracted per file:**
- Record messages (per-second): timestamp, HR, power, cadence, speed, distance
- Session messages (summary): sport, duration, distance, avg/max HR, avg/max power, cadence, calories

---

## 8. Analysis Engine

### Per-ride metrics

| Metric | Computation |
|---|---|
| HR zone distribution | % time in Z1-Z5 (from `athlete_config`) |
| Power zone distribution | % time in Z1-Z5 (from `athlete_config`) |
| HR drift | Avg HR 2nd half ÷ 1st half at matched power → % change |
| Decoupling | EF drift between halves |
| Normalized Power | 30s rolling avg → 4th power → avg → 4th root |
| Intensity Factor | NP / FTP (from `athlete_config`) |
| TSS | (duration_s × NP × IF) / (FTP × 3600) × 100 |
| TRIMP | Banister: duration × ΔHR_ratio × e^(coeff × ΔHR_ratio) |
| Efficiency Factor | NP / avg HR |

All zone boundaries and FTP read from `athlete_config` table at processing time.

### Weekly aggregation

Total volume (hours, TSS, TRIMP), Z2 power trend (4-week rolling), HR drift trend, weight trend, session compliance, subjective overlay (RPE vs drift).

### Claude API integration

Weekly data + athlete profile markdown (from disk) as system prompt → Claude returns narrative report + plan → rendered to HTML + compact Telegram summary.

---

## 9. Data Storage (SQLite)

```sql
CREATE TABLE wahoo_tokens (
    id            INTEGER PRIMARY KEY,
    access_token  TEXT NOT NULL,
    refresh_token TEXT NOT NULL,
    expires_at    DATETIME NOT NULL,
    updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE workouts (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    wahoo_id        TEXT UNIQUE NOT NULL,
    started_at      DATETIME NOT NULL,
    duration_sec    INTEGER,
    distance_m      REAL,
    calories        INTEGER,
    avg_hr          INTEGER,
    max_hr          INTEGER,
    avg_power       REAL,
    max_power       REAL,
    avg_cadence     REAL,
    workout_type    TEXT,
    fit_file_path   TEXT,
    source          TEXT DEFAULT 'api' CHECK(source IN ('api','csv','manual')),
    processed       BOOLEAN DEFAULT 0,
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE ride_metrics (
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
);

CREATE TABLE athlete_notes (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp   DATETIME NOT NULL,
    type        TEXT NOT NULL CHECK(type IN ('ride','note','weight')),
    rpe         INTEGER CHECK(rpe BETWEEN 1 AND 10),
    weight_kg   REAL,
    note        TEXT,
    workout_id  INTEGER REFERENCES workouts(id),
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE athlete_config (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE reports (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    type          TEXT NOT NULL CHECK(type IN ('weekly_report','weekly_plan')),
    week_start    DATE NOT NULL,
    week_end      DATE NOT NULL,
    summary_text  TEXT,
    full_html     TEXT,
    created_at    DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

---

## 10. Networking & Infrastructure

### DNS + TLS + Access

```
coach.tonio.cc → Cloudflare DNS (proxied) → Cloudflare Access → homelab → NPM → cycling-coach:8080
```

**Cloudflare Access rules:**
- `/wahoo/*` → bypass (Wahoo must reach callback + webhook)
- `/health` → bypass
- Everything else → require Cloudflare Access authentication

### Docker

```yaml
# docker-compose.yml
services:
  cycling-coach:
    build: .
    container_name: cycling-coach
    restart: unless-stopped
    ports:
      - "8080:8080"
    volumes:
      - coach-data:/data
    env_file:
      - .env

volumes:
  coach-data:
```

```dockerfile
# Dockerfile
FROM golang:1.22-alpine AS builder
RUN apk add --no-cache gcc musl-dev
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o cycling-coach ./cmd/server

FROM alpine:3.19
RUN apk add --no-cache sqlite-libs ca-certificates tzdata
WORKDIR /app
COPY --from=builder /app/cycling-coach .
COPY --from=builder /app/templates ./templates
COPY --from=builder /app/static ./static
COPY --from=builder /app/config ./config
EXPOSE 8080
CMD ["./cycling-coach"]
```

Note: `config/athlete-profile.default.md` is copied into the image as a seed file. On first startup, the app copies it to the data volume if no profile exists.

---

## 11. Project Structure

```
cycling-coach/
├── cmd/
│   ├── server/
│   │   └── main.go                 # Entry point
│   ├── backfill/
│   │   └── main.go                 # Fetch all historical workouts
│   ├── import-csv/
│   │   └── main.go                 # Import Wahoo CSV exports
│   └── reset/
│       └── main.go                 # Backup, wipe, reimport
│
├── internal/
│   ├── config/
│   │   └── config.go              # Env var loading
│   │
│   ├── wahoo/
│   │   ├── auth.go                # OAuth2 flow
│   │   ├── client.go              # API client
│   │   ├── webhook.go             # Webhook handler
│   │   └── models.go              # API response types
│   │
│   ├── telegram/
│   │   ├── bot.go                 # Bot init + long-polling
│   │   ├── handlers.go            # Command handlers
│   │   └── formatter.go           # Message formatting
│   │
│   ├── fit/
│   │   └── parser.go              # FIT → records
│   │
│   ├── analysis/
│   │   ├── metrics.go             # Per-ride metrics
│   │   ├── trends.go              # Multi-ride trends
│   │   └── processor.go           # Processing pipeline
│   │
│   ├── profile/
│   │   ├── profile.go             # Load, save, reload athlete profile
│   │   └── parser.go              # Extract structured config from markdown
│   │
│   ├── reporting/
│   │   ├── claude.go              # Claude API integration
│   │   ├── renderer.go            # HTML rendering
│   │   └── delivery.go            # Telegram + web delivery
│   │
│   ├── scheduler/
│   │   └── scheduler.go           # Cron jobs
│   │
│   ├── storage/
│   │   ├── db.go                  # SQLite connection + migrations
│   │   ├── workouts.go            # Workout CRUD
│   │   ├── metrics.go             # Ride metrics CRUD
│   │   ├── notes.go               # Athlete notes CRUD
│   │   ├── reports.go             # Report CRUD
│   │   ├── tokens.go              # Wahoo token storage
│   │   └── athlete_config.go      # Athlete config key-value CRUD
│   │
│   └── web/
│       ├── router.go              # HTTP routes
│       ├── handlers.go            # HTTP handlers
│       └── middleware.go          # Logging, recovery
│
├── config/
│   └── athlete-profile.default.md # Seed file (copied to data vol on first run)
│
├── templates/
│   ├── report.html
│   └── plan.html
│
├── static/
│   └── style.css
│
├── testdata/
│   └── sample.fit
│
├── CLAUDE.md                      # Claude Code project instructions
├── ARCHITECTURE.md                # This document
├── .env.example
├── .gitignore
├── Dockerfile
├── docker-compose.yml
├── Makefile
├── go.mod
├── go.sum
└── README.md
```

---

## 12. Go Dependencies

| Package | Purpose |
|---|---|
| `github.com/muktihari/fit` | FIT file decoding |
| `github.com/go-telegram-bot-api/telegram-bot-api/v5` | Telegram bot |
| `github.com/mattn/go-sqlite3` | SQLite driver (CGO) |
| `github.com/go-chi/chi/v5` | HTTP router |
| `github.com/robfig/cron/v3` | Cron scheduler |
| `golang.org/x/oauth2` | OAuth2 for Wahoo |
| `log/slog` | Structured logging (stdlib) |

Claude API: raw `net/http` (simple JSON POST, no SDK needed).

---

## 13. Scheduled Jobs

| Job | Schedule | What |
|---|---|---|
| Token refresh | Every 90 min | Refresh Wahoo access token |
| Workout poll | Every 4 hours | Check for missed workouts |
| FIT processing | Every 15 min | Process unprocessed workouts |
| Weekly report | Sunday 20:00 CET | Generate report + plan, send via Telegram |

---

## 14. Build Phases

Each phase is a self-contained Claude Code session. Commit after each.

### Phase 1: Skeleton + Config + DB
Runnable Docker container with HTTP server and SQLite.

- `go mod init`, add dependencies
- `internal/config/config.go` — env loading (including `ATHLETE_PROFILE_PATH`)
- `internal/storage/db.go` — SQLite init + all migrations (including `athlete_config`)
- `cmd/server/main.go` — chi router, `/health` endpoint
- `Dockerfile`, `docker-compose.yml`, `Makefile`
- First-run logic: copy `config/athlete-profile.default.md` to data volume if missing
- **Verify:** `docker compose up` → `curl localhost:8080/health` → 200

### Phase 2: Athlete Profile Management
Profile loading, parsing, and update paths.

- `internal/profile/profile.go` — load from disk, save, reload
- `internal/profile/parser.go` — extract FTP, HRmax, weight, zones from markdown
- `internal/storage/athlete_config.go` — key-value CRUD
- `internal/web/handlers.go` — `GET/POST /api/profile`, `POST /api/profile/reload`
- Seed `athlete_config` from default profile on first run
- **Verify:** update profile via API → `athlete_config` rows updated

### Phase 3: Telegram Bot
Bot responds to commands, stores notes.

- `internal/telegram/bot.go` — init, long-polling goroutine
- `internal/telegram/handlers.go` — `/ride`, `/note`, `/weight`, `/profile`
- `internal/storage/notes.go` — athlete_notes CRUD
- Wire into `main.go`
- **Verify:** `/ride 6 felt good` → row in DB; `/profile` → receive document

### Phase 4: Wahoo OAuth
Complete OAuth flow, tokens stored.

- `internal/wahoo/auth.go` — authorize, callback, refresh
- `internal/wahoo/models.go` — API types
- `internal/storage/tokens.go` — token CRUD
- Mount `/wahoo/authorize`, `/wahoo/callback`
- **Verify:** complete OAuth in browser → tokens in DB

### Phase 5: Workout Ingestion
Rides appear in DB with FIT files.

- `internal/wahoo/client.go` — list workouts, get summary, download FIT
- `internal/wahoo/webhook.go` — webhook handler
- `internal/storage/workouts.go` — workout CRUD
- **Verify:** ride → webhook → workout + FIT in DB + disk

### Phase 6: FIT Parsing + Analysis
Every ride gets metrics computed.

- `internal/fit/parser.go` — FIT decode → records
- `internal/analysis/metrics.go` — per-ride metrics (reads zones from `athlete_config`)
- `internal/analysis/processor.go` — pipeline
- `internal/storage/metrics.go` — ride_metrics CRUD
- **Verify:** process a FIT file → correct metrics

### Phase 7: Historical Import
CSV import + API backfill + reset.

- `cmd/import-csv/main.go`
- `cmd/backfill/main.go` (rate-limited)
- `cmd/reset/main.go` (backup, wipe, reimport, reprocess)
- **Verify:** import CSVs → metrics match previous analysis

### Phase 8: Reporting + Delivery
Weekly reports and plans.

- `internal/analysis/trends.go` — multi-ride trends
- `internal/reporting/claude.go` — Claude API (loads profile from `ATHLETE_PROFILE_PATH`)
- `internal/reporting/renderer.go` — HTML rendering
- `internal/reporting/delivery.go` — Telegram + web
- `internal/web/handlers.go` — `/reports/{id}`, `/plans/{id}`
- `templates/report.html`, `templates/plan.html`
- **Verify:** trigger report → HTML page → Telegram summary

### Phase 9: Scheduler + Integration
Full automation.

- `internal/scheduler/scheduler.go`
- `/status`, `/week`, `/plan` Telegram commands
- Post-ride notification
- Auto-link notes to workouts
- Error handling, graceful shutdown
- **Verify:** full unattended week

---

## 15. Environment Variables

```env
# .env.example

# Wahoo Cloud API
WAHOO_CLIENT_ID=
WAHOO_CLIENT_SECRET=
WAHOO_REDIRECT_URI=https://coach.tonio.cc/wahoo/callback
WAHOO_WEBHOOK_SECRET=

# Telegram
TELEGRAM_BOT_TOKEN=
TELEGRAM_CHAT_ID=

# Anthropic (Claude API)
ANTHROPIC_API_KEY=

# Server
SERVER_ADDR=:8080
BASE_URL=https://coach.tonio.cc
DATABASE_PATH=/data/cycling.db
FIT_FILES_PATH=/data/fit_files/
ATHLETE_PROFILE_PATH=/data/athlete-profile.md
TZ=Europe/Amsterdam
```

---

## 16. Cloudflare Access Configuration

Add `coach.tonio.cc` to existing Cloudflare Access setup.

**Application:** `Cycling Coach` · Domain: `coach.tonio.cc` · Session: 24h+

**Policy:** Allow your email / identity group.

**Bypass rules:**
- `/wahoo/*` → bypass (Wahoo callback + webhook)
- `/health` → bypass

---

## 17. Infrastructure Setup Checklist

Before writing any code:

- [ ] Register Wahoo developer app at `developers.wahooligan.com`
- [ ] Create Telegram bot via @BotFather → get token
- [ ] Get your Telegram chat ID
- [ ] Get Anthropic API key
- [ ] Create Cloudflare DNS: `coach.tonio.cc` → homelab IP (proxied)
- [ ] Add `coach.tonio.cc` to Cloudflare Access with bypass rules
- [ ] Configure Nginx Proxy Manager: `coach.tonio.cc` → `cycling-coach:8080`
- [ ] Populate `.env` from `.env.example`

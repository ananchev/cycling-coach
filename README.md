# cycling-coach

Personal cycling training assistant. Ingests ride data from Wahoo, collects subjective notes via Telegram, analyzes every ride, and generates weekly reports and training plans.

## How it works

1. **Wahoo KICKR** → ride data flows via webhook/API → FIT files stored and analyzed
2. **Telegram bot** → you log RPE, weight, sleep/stress notes with zero friction
3. **Analysis engine** → computes HR drift, power zones, NP, TSS, TRIMP per ride
4. **Claude API** → generates narrative weekly reports and structured training plans
5. **Delivery** → compact summaries in Telegram, full reports as web pages

## Quick start

```bash
cp .env.example .env
# Fill in your Wahoo, Telegram, and Anthropic credentials

docker compose up -d
```

On first run, the default athlete profile (`config/athlete-profile.default.md`) is copied to the data volume. Update it anytime via Telegram (`/profile set`) or the web API.

Then visit `https://coach.tonio.cc/wahoo/authorize` to connect your Wahoo account.

## Architecture

See [ARCHITECTURE.md](ARCHITECTURE.md) for the full system design.

## Key files

| File | Purpose |
|---|---|
| `CLAUDE.md` | Claude Code dev instructions (read by Claude Code automatically) |
| `ARCHITECTURE.md` | Full system design, API docs, build phases |
| `config/athlete-profile.default.md` | Seed athlete profile (training zones, FTP, coaching philosophy) |
| `.env.example` | Environment variable template |

The athlete profile is runtime config — it lives in the data volume (`/data/athlete-profile.md`), not in the codebase. The seed file provides the initial version.

## Development

```bash
make dev     # Run locally
make test    # Run tests
make lint    # Run linter
make build   # Build Docker image
make run     # Start with docker compose
```

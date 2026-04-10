## Open Brain Dashboard (Go)

Single static binary dashboard for the self-hosted Open Brain stack. Talks
directly to PostgreSQL + pgvector for reads and writes, and to Ollama for
semantic-search query embeddings. No Node.js, no bundler, no edge functions —
one binary plus a TOML config.

Intended to pair with [`integrations/docker-compose-deployment/`](../../integrations/docker-compose-deployment/).

## Status

v0 complete. Home, detail, browse, and search all work against a real
Open Brain database.

## What It Does

| Page | State | Purpose |
|------|-------|---------|
| Home | **working** | Stats overview (total, this week, by type, top topics) plus the most recent captures |
| Detail | **working** | Single-thought view with full metadata, action items with priority chips, dates mentioned, and a raw-JSON disclosure |
| Browse | **working** | Paginated filtered list — type chip row, topic/person/content-substring inputs, time window dropdown, pagination with filter preservation |
| Search | **working** | Semantic (pgvector cosine) and text (ILIKE) modes with a mode-toggle switch, similarity badges on semantic results, and a graceful text-mode fallback when semantic embedding fails |

v1 adds inline edit and delete. v2 adds duplicates, audit, and ingestion
queue — features that require their own new tables but never alter the
existing `thoughts` table. Eventual candidate for a Gmail-style
master/detail two-pane layout refactor once bulk operations arrive.

## Home Page Data Model

The home page reads directly from the `thoughts` table using jsonb paths
rather than column promotions. Counts-by-type use `metadata->>'type'` with
`coalesce` so missing values show up as `unknown`. Top-topics uses
`jsonb_array_elements_text(metadata->'topics')` with a `jsonb_typeof = 'array'`
guard so malformed metadata skips silently instead of erroring. This is
deliberate: it means the dashboard runs against the stock
`integrations/docker-compose-deployment/` schema without any migrations.

Four queries run serially on each page load (total count, week count,
types, topics, recent list). Against a few thousand thoughts on a local
Postgres this is sub-50ms end-to-end. If the corpus grows large enough to
matter, add `CREATE INDEX ON thoughts ((metadata->>'type'))` and a GIN
index on `metadata->'topics'` for array containment.

## Prerequisites

- A working Open Brain stack from `integrations/docker-compose-deployment/`
  (or any Postgres + pgvector + Ollama setup that speaks the same schema)
- Go 1.25+
- [templ](https://templ.guide) — install once with
  `go install github.com/a-h/templ/cmd/templ@latest`
- `curl` (used once by `build.sh` to vendor `htmx.min.js`)

## Quick Start

```bash
cd dashboards/open-brain-dashboard-go

# Generate a default config, then edit the database URL + Ollama URL to
# point at your stack:
./build.sh build
./open-brain-dashboard-go gen-config

# Edit open-brain-dashboard-go.toml — set database.url and ollama.url
$EDITOR open-brain-dashboard-go.toml

# Run it
./open-brain-dashboard-go serve --config open-brain-dashboard-go.toml
# → listening on http://127.0.0.1:8080
```

Or use `./build.sh run` to build and launch in one step.

## Config

```toml
[server]
listen = "127.0.0.1:8080"

[database]
url = "postgres://openbrain:YOUR_PASSWORD@home-server:5432/openbrain?sslmode=disable"

[ollama]
url = "http://home-server:11434"
embedding_model = "mxbai-embed-large"
```

Every value can be overridden via CLI flag — run
`./open-brain-dashboard-go serve --help` for the full list. CLI flags take
precedence over config file values so you can keep a single config checked
in and override just the listen address when running multiple instances.

## Deployment Modes

**Dev (desktop):** build and run on your workstation, pointing at
`home-server:5432` and `home-server:11434`. Fast iteration loop — edit
`.go` or `.templ`, `./build.sh run`, refresh browser. Requires the Ollama
port to be exposed on the LAN in your docker-compose stack (see the main
`docker-compose.yml` comment above the `ports:` block on the `ollama`
service).

**Prod (node-0):** runs as a fourth docker-compose service alongside
`db`, `ollama`, and `mcp-server`, bound to `127.0.0.1:8080` behind Caddy at
`http://open-brain-ui/` (or equivalent). The compose wiring for this lands
alongside the real feature handlers, not in the v0 scaffold.

## Layout

```
main.go               # go-flags entry point, serve + gen-config subcommands
config.go             # TOML config types + Load/Write/Default helpers
db.go                 # pgx connection pool setup
ollama.go             # OpenAI-compatible /v1/embeddings client
server.go             # chi router + handler dispatch + static file mount
templates/
  layout.templ        # minimal base (head + body slot, no header baked in)
  home.templ          # placeholder Home page
static/
  tokens.css          # design tokens (dark theme)
  app.css             # component styles
  vendor/htmx.min.js  # vendored by build.sh (gitignored)
build.sh              # templ generate + go build wrapper (always use this)
.air.toml             # live-reload config for `air` during dev
```

## Why this exists

The existing dashboards in `dashboards/` (`open-brain-dashboard` and
`open-brain-dashboard-next`) both assume a hosted-Supabase deployment
model — one uses Supabase Auth for its own login gate, and the other
requires a separate `open-brain-rest` Edge Function that isn't in this
repo. Neither runs against a fully self-hosted `integrations/docker-compose-deployment/`
stack without substantial modification.

This dashboard targets the self-hosted deployment model directly: direct
Postgres over the LAN, direct Ollama for embeddings, no external gateway,
no session-cookie framework, no JS toolchain.

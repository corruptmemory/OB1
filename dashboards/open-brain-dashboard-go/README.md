## Open Brain Dashboard (Go)

Single static binary dashboard for the self-hosted Open Brain stack. Talks
directly to PostgreSQL + pgvector for reads and writes, and to Ollama for
semantic-search query embeddings. No Node.js, no bundler, no edge functions —
one binary plus a TOML config.

Intended to pair with [`integrations/docker-compose-deployment/`](../../integrations/docker-compose-deployment/).

## Status

v1.1 complete. All CRUD + search is working against a real Open Brain
database: home, detail, browse, and search on the read side; create,
edit (including action items), and delete on the write side. The dev
build runs on a workstation against a remote `home-server:5432` and
`home-server:11434`.

**Next resume point:** deploy the binary to node-0 as a fourth
docker-compose service behind Caddy at `http://open-brain-ui/` (or
similar). See "Deployment Modes" below for the target layout.

## What It Does

| Page / Action | State | Purpose |
|---------------|-------|---------|
| Home | **working** | Stats overview (total, this week, by type, top topics), most recent captures, and an inline collapsible quick-capture form at the top of the page |
| Capture | **working** | `<details>`-collapsible form on the home page. Writes a new thought via `ollama.Embed` + DB insert. Fields match the edit form. No LLM-based auto-extraction — manual metadata is fast and the user is already engaged. `metadata.source = "dashboard"` for provenance. |
| Detail | **working** | Single-thought view with full metadata, action items with priority chips, dates mentioned, and a raw-JSON disclosure. Shows "edited X ago" when the thought has been modified via the dashboard. |
| Browse | **working** | Paginated filtered list — type chip row, topic/person/content-substring inputs, time window dropdown, pagination with filter preservation |
| Search | **working** | Semantic (pgvector cosine) and text (ILIKE) modes with a mode-toggle switch, similarity badges on semantic results, and a graceful text-mode fallback when semantic embedding fails |
| Edit | **working** | Dedicated edit form at `/thought/{id}/edit` for content (textarea), type (select), topics and people (comma-separated), and action items (dynamic rows with description + priority). Content changes trigger a sync re-embed via Ollama; metadata-only edits skip Ollama entirely. Update timestamps are stored at `metadata.updated_at`. Action items are always written in the normalized `{description, priority}` object shape, forward-migrating any string-shape items on first edit. |
| Delete | **working** | `POST /thought/{id}/delete` with browser-native confirm. 303-redirects to the home page and decrements the total count. |

v2 adds duplicates, audit, and ingestion queue — features that require
their own new tables but never alter the existing `thoughts` table.
Eventual candidate for a Gmail-style master/detail two-pane layout
refactor once bulk operations arrive.

## URL Surface

```
GET  /                              home (stats + recent + capture form)
POST /capture                       quick-capture from home form
GET  /browse[?type=&topic=&person=&q=&days=&page=]
GET  /search[?q=&mode=semantic|text&page=]
GET  /thought/{id}                  readonly detail
GET  /thought/{id}/edit             edit form
POST /thought/{id}/edit             update handler (content + metadata + action items)
POST /thought/{id}/delete           delete handler
GET  /partials/action-item-row      htmx fragment: blank action-item row
```

Capture, update, and delete are classic POST-redirect-GET flows — the
dashboard's write paths work with JavaScript disabled. The only feature
that requires JavaScript is the "+ add action item" button in the edit
form, which uses htmx to append a row fragment. Removing an action item
is plain inline JS (`this.closest('.action-item-row').remove();`) and
the edit form's submit drops empty rows server-side, so "remove" works
without JS too — just clear the description and save.

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

## Open Brain Dashboard (Go)

Single static binary dashboard for the self-hosted Open Brain stack. Talks
directly to PostgreSQL + pgvector for reads and writes, and to Ollama for
semantic-search query embeddings. No Node.js, no bundler, no edge functions —
one binary plus a TOML config.

Intended to pair with [`integrations/docker-compose-deployment/`](../../integrations/docker-compose-deployment/).

## Status

**v1.5 complete.** The dashboard is a three-pane master/detail shell
(Gmail-style) at `/`, driven entirely by URL query parameters. The
v1.1 flat pages (home / browse / search / detail) are gone — their
URLs redirect into the unified `/?…` shape so bookmarks keep working.
All CRUD is wired end-to-end against the live Open Brain database,
plus a single bulk-ops lane (bulk delete) so the capability is
proven rather than scaffolded.

See `docs/plans/2026-04-11-dashboard-v1.5-master-detail-design.md`
for the full design rationale and `CLAUDE.md` in this directory for
the binding design principles.

**Next resume point:** deploy the binary to node-0 as a fourth
docker-compose service behind Caddy at `http://open-brain-ui/` (or
similar). See "Deployment Modes" below for the target layout. A
latent issue to address before deployment: `server.go` serves static
assets via `http.FileServer(http.Dir("static"))`, which means the
binary must be run from the dashboard directory. For containerized
deployment, either mount the `static` directory as a volume or add
a `//go:embed static` directive so the assets ship inside the
binary.

## What It Does

The v1.5 shell replaces every v1.1 page with a single route `/` that
renders three panes: filter sidebar, thought list, detail. Every
interaction is a partial htmx swap targeting one pane, URL-pushed
so bookmarks and back/forward work.

| Area | State | Purpose |
|---|---|---|
| Shell | **working** | CSS-grid three-pane layout (`sidebar` · `list` · `detail`) sized in `fr` + fixed widths. Mobile falls back to a single column below 1216 px. Dark is default; light palette ships alongside and toggles via the sidebar footer's theme button (localStorage-backed). |
| Filter sidebar | **working** | Type chips with counts, time-window chips (All / 7d / 30d / 90d), top-10 topics, top-10 people. Clicking a chip pushes the filter into the URL, clicking an active chip clears it. Every anchor has a real `href` for progressive-enhancement — JS-off users still get filtered shell loads. |
| List pane | **working** | Dense two-line rows (~50 per 1080p screen) with type chip, relative time, id, and truncated content. Row click loads the thought into the detail pane. Semantic search is one box at the top with automatic text-fallback when Ollama is unavailable or returns no matches. An Ollama health dot next to the search box reflects the last embed outcome (green / amber / grey). Pagination is bookmarkable. |
| Detail pane: read | **working** | Full content, topic/person tags linked back to filter URLs, action items with priority chips, raw metadata disclosure, Edit and Delete buttons. Shows "captured X ago" and "edited X ago" when applicable. |
| Detail pane: edit | **working** | Right-pane swap (not a modal): textarea + type select + CSV topics/people + dynamic action-item rows. Save POSTs to `/thought/{id}/edit`; on htmx the response is a fragment + `HX-Trigger: refresh-row-N` so the corresponding list row re-fetches itself. Cancel restores the read view. JS-off fallback is the same POST with 303-to-read. Ollama embed is recomputed only when content text changes. |
| Compose panel | **working** | Gmail-style floating `<dialog>` sibling of the shell. Opens bottom-right in compact mode (non-blocking), can expand to modal (dimmed backdrop) and back. Minimize collapses to the title bar via `<details>`. Esc and backdrop-click both contract to compact — your draft survives anything except explicit close. Save clears the slot and fires `HX-Trigger: refresh-list, focus-thought-{id}` so the list refreshes and the new thought lands in the detail pane. Error path re-renders the panel with typed input preserved. |
| Bulk delete | **working** | Row checkboxes with a three-state toolbar (normal / bulk / confirm) switched via CSS `:has()` — no client state tracking. Clicking Delete selected swaps to an inline confirm ("Delete N thoughts? This is permanent."); Yes, delete POSTs `ids[]` to `/bulk-delete` via `hx-include="#list-form"`. Deletion is immediate and permanent — no undo window. If the currently-open thought is in the deleted set, the response emits `HX-Trigger: clear-detail` and `HX-Push-Url` to strip `?id=` from the URL. |
| Legacy redirects | **working** | v1.1 URLs (`/home`, `/browse?…`, `/search?q=…&mode=…`, `/thought/{id}`, GET `/thought/{id}/edit`) all 303-redirect into the new `/?…` shape. The `mode=` parameter is dropped silently — v1.5 has no mode toggle. POST `/thought/{id}/edit` and POST `/thought/{id}/delete` stay as write endpoints. |

**v2 candidates** (not in v1.5): "select all matching" beyond the
visible page, bulk re-embed, bulk tag/type edit, search operators
(`"quoted"` substring), compose draft persistence, undo window,
keyboard shortcuts, similarity threshold for the semantic/text
fallback path, phone-specific layout refinements, numbered
pagination with ellipsis elision.

## URL Surface

```
GET  /                                 shell (query params drive state)
     ?type=&topic=&person=&days=&q=&id=&page=

GET  /partials/list?…                  list pane fragment
GET  /partials/detail/{id}?…           detail pane read fragment
GET  /partials/detail/{id}/edit?…      detail pane edit form fragment
GET  /partials/detail/empty            empty detail placeholder
GET  /partials/row/{id}?…              single list-row fragment (for refresh-row-N trigger)
GET  /partials/compose                 floating compose dialog fragment
DEL  /partials/compose                 clear the compose slot
GET  /partials/action-item-row         blank action-item row fragment

POST /capture                          save a new thought from compose
POST /thought/{id}/edit                update handler (content + metadata + action items)
POST /thought/{id}/delete              delete handler
POST /bulk-delete                      batch delete from selected ids

GET  /home                             303 → /
GET  /browse?…                         303 → /?…
GET  /search?q=…&mode=…                303 → /?q=… (mode dropped)
GET  /thought/{id}                     303 → /?id={id}
GET  /thought/{id}/edit                303 → /?id={id}&mode=edit
```

Every write path works with JavaScript disabled: forms have real
`action=` attributes, POSTs return 303 redirects on non-htmx
requests. htmx is progressive enhancement over POST-redirect-GET,
not a prerequisite.

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

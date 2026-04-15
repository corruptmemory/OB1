## Open Brain Dashboard (Go)

Single static binary dashboard for the self-hosted Open Brain stack. Talks
directly to PostgreSQL + pgvector for reads and writes, and to Ollama for
semantic-search query embeddings. No Node.js, no bundler, no edge functions —
one binary plus a TOML config.

Intended to pair with [`integrations/docker-compose-deployment/`](../../integrations/docker-compose-deployment/).

## Status

**v2.0 complete.** The dashboard is a three-pane master/detail shell
(Gmail-style) at `/`, driven entirely by URL query parameters. The
v1.1 flat pages (home / browse / search / detail) are gone — their
URLs redirect into the unified `/?…` shape so bookmarks keep working.
All CRUD is wired end-to-end against the live Open Brain database,
plus bulk delete and coalesce.

**v2.0 adds a fully integrated Go MCP server** — same binary, same
DB pool, same Ollama client. The `system` subcommand runs both the
dashboard HTTP server and the MCP server concurrently. The Deno/Docker
MCP container is retired; this binary takes over port 8000 directly.
Seven tools: `capture_thought`, `search_thoughts`, `list_thoughts`,
`thought_stats`, `delete_thought`, `find_similar_thoughts`, `coalesce_thoughts`.

Deployed to node-0 as a single systemd unit (`open-brain.service`)
under `/home/open-brain/bin/`. Dashboard on port 8082 fronted by
`open-brain-dashboard:80` in Caddy; MCP on port 8000 fronted by
`open-brain:80`.

See `docs/plans/2026-04-11-dashboard-v1.5-master-detail-design.md`
for the v1.5 design rationale and `CLAUDE.md` in this directory for
the binding design principles.

## What It Does

The v1.5 shell replaces every v1.1 page with a single route `/` that
renders three panes: filter sidebar, thought list, detail. Every
interaction is a partial htmx swap targeting one pane, URL-pushed
so bookmarks and back/forward work.

| Area | State | Purpose |
|---|---|---|
| Activity bar | **working** | VS Code-style vertical icon strip (48px) to the left of the sidebar. Three view icons: Thought catalogue (active), Dashboard (placeholder), Organize (placeholder). Theme toggle pinned to the bottom. Active view gets an accent left-border highlight. Hidden on mobile (<1216px). View selection is URL-driven (`?view=catalogue\|dashboard\|organize`). |
| Shell | **working** | CSS-grid four-pane layout (`activity` · `sidebar` · `list` · `handle` · `detail`) sized in `fr` + fixed widths. Mobile falls back to a single column below 1216 px. Dark is default; light palette ships alongside and toggles via the activity bar's theme button (localStorage-backed). |
| Filter sidebar | **working** | Type chips with counts, time-window chips (All / 7d / 30d / 90d), top-50 topics, top-50 people. Topics and people sections have client-side filter inputs (prefix matches sort above contains matches) and scroll independently when the list overflows. Clicking a chip pushes the filter into the URL, clicking an active chip clears it. Every anchor has a real `href` for progressive-enhancement — JS-off users still get filtered shell loads. |
| List pane | **working** | Dense two-line rows (~50 per 1080p screen) with type chip, relevance score (when searching), relative time, id, and truncated content (`text-overflow: ellipsis`). Row click loads the thought into the detail pane. A draggable resize handle between list and detail panes persists widths to localStorage. Search is submit-on-Enter with a ⌕ button — semantic results are filtered by a 0.5 cosine similarity threshold so irrelevant matches don't appear. Automatic text-fallback when Ollama is unavailable or returns no matches. An Ollama health dot next to the search box reflects the last embed outcome (green / amber / grey). Pagination is bookmarkable. |
| Detail pane: read | **working** | Full content, topic/person tags linked back to filter URLs, action items with priority chips, raw metadata disclosure, Edit and Delete buttons. Shows "captured X ago" and "edited X ago" when applicable. |
| Detail pane: edit | **working** | Right-pane swap (not a modal): textarea + type select + CSV topics/people + dynamic action-item rows. Save POSTs to `/thought/{id}/edit`; on htmx the response is a fragment + `HX-Trigger: refresh-row-N` so the corresponding list row re-fetches itself. Cancel restores the read view. JS-off fallback is the same POST with 303-to-read. Ollama embed is recomputed only when content text changes. |
| Compose panel | **working** | Gmail-style floating `<dialog>` sibling of the shell. Opens bottom-right in compact mode (non-blocking), can expand to modal (dimmed backdrop) and back. Minimize collapses to the title bar via `<details>`. Esc and backdrop-click both contract to compact — your draft survives anything except explicit close. Save runs AI metadata extraction via Ollama chat (same prompt as the MCP server) to auto-populate type, topics, people, action_items, and dates_mentioned — user-entered form fields override AI results. Save clears the slot and fires `HX-Trigger: refresh-list, focus-thought-{id}` so the list refreshes and the new thought lands in the detail pane. Error path re-renders the panel with typed input preserved. |
| Bulk delete | **working** | Row checkboxes with a three-state toolbar (normal / bulk / confirm) switched via CSS `:has()` — no client state tracking. Clicking Delete selected swaps to an inline confirm ("Delete N thoughts? This is permanent."); Yes, delete POSTs `ids[]` to `/bulk-delete` via `hx-include="#list-form"`. Deletion is immediate and permanent — no undo window. If the currently-open thought is in the deleted set, the response emits `HX-Trigger: clear-detail` and `HX-Push-Url` to strip `?id=` from the URL. |
| Coalesce thoughts | **working** | When 2+ thoughts are selected, a "Coalesce" button appears in the bulk toolbar. Clicking it POSTs to `/coalesce`, which fetches the selected thoughts' content, calls Ollama chat to synthesize a merged note, then extracts metadata via the same AI pipeline as compose. The result renders in a compose-like floating dialog pre-filled with the AI synthesis. The user can edit everything, then "Replace N thoughts" atomically creates the new thought and deletes the originals in a single transaction. Falls back to raw concatenation with a warning when Ollama is unavailable. A "synthesizing…" indicator shows during the AI call. |
| Legacy redirects | **working** | v1.1 URLs (`/home`, `/browse?…`, `/search?q=…&mode=…`, `/thought/{id}`, GET `/thought/{id}/edit`) all 303-redirect into the new `/?…` shape. The `mode=` parameter is dropped silently — v1.5 has no mode toggle. POST `/thought/{id}/edit` and POST `/thought/{id}/delete` stay as write endpoints. |

**v2 candidates** (not in v1.5): "select all matching" beyond the
visible page, bulk re-embed, bulk tag/type edit, search operators
(`"quoted"` substring), compose draft persistence, undo window,
keyboard shortcuts, similarity threshold slider (currently baked at
0.5), phone-specific layout refinements, numbered pagination with
ellipsis elision, dashboard view (graphs/stats), organize view
(duplicate detection via cosine distance).

## URL Surface

```
GET  /                                 shell (query params drive state)
     ?view=&type=&topic=&person=&days=&q=&id=&page=

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
POST /coalesce                         AI synthesis of selected thoughts → compose panel
POST /coalesce/confirm                 replace originals with coalesced thought (tx)

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

## Subcommands

| Subcommand | What it runs |
|---|---|
| `serve` | Dashboard HTTP server only (port 8082 by default) |
| `mcp` | MCP server only (port 8001 by default) |
| `system` | Both servers concurrently — this is the production mode |
| `gen-config` | Write a default TOML config to disk and exit |

## Quick Start

```bash
cd dashboards/open-brain-dashboard-go

# Generate a default config, then edit the database URL + Ollama URL:
./build.sh build
./open-brain-dashboard-go gen-config

# Edit the config — at minimum set database.url and ollama.url
$EDITOR open-brain-dashboard-go.toml

# Dashboard only:
./open-brain-dashboard-go serve --config open-brain-dashboard-go.toml

# MCP server only:
./open-brain-dashboard-go mcp --config open-brain-dashboard-go.toml

# Both (production mode):
./open-brain-dashboard-go system --config open-brain-dashboard-go.toml
```

Or use `./build.sh run` to build and launch the dashboard in one step.

## Live Reload (Dev)

[air](https://github.com/air-verse/air) watches `.go`, `.templ`, `.css`,
`.js`, and `.toml` files and rebuilds + restarts the server automatically on
every change. Because all static assets are embedded via `//go:embed`, each
rebuild produces a fresh binary with the latest CSS/JS baked in — there's no
"just refresh" shortcut; the binary must be rebuilt.

```bash
# Install air (once)
go install github.com/air-verse/air@latest

# Run from the dashboard directory (air reads .air.toml from cwd)
cd dashboards/open-brain-dashboard-go
air
```

The `.air.toml` in this directory is pre-configured: it calls `./build.sh
build` as the build command, runs the binary with `serve --config
open-brain-dashboard-go.toml`, and excludes generated `*_templ.go` files to
prevent rebuild loops (templ generate creates `.go` files that air watches).

**Gotchas:**
- Run `air` from this directory, not from the repo root — otherwise air
  watches the entire monorepo and chokes on unrelated directories.
- `air` passes arguments to the binary via `args_bin` in `.air.toml`, not
  as part of `bin`. If `bin` contains spaces (e.g.
  `"./binary serve --config foo"`), air treats the whole string as a single
  executable path and fails with "No such file or directory."
- Kill air (`Ctrl-C` or `pkill air`) before doing manual builds — two
  processes fighting over the same port will confuse both.

## Config

```toml
[server]
listen = "127.0.0.1:8082"

[database]
url = "postgres://openbrain:YOUR_PASSWORD@home-server:5432/openbrain?sslmode=disable"

[ollama]
url = "http://home-server:11434"
embedding_model = "mxbai-embed-large"
chat_model = "qwen2.5:3b"

[mcp]
listen = "127.0.0.1:8001"
access_key = ""   # leave empty to disable auth; set to a random hex string in production
```

`chat_model` enables AI metadata extraction on compose and MCP capture
(type, topics, people, action_items, dates_mentioned) plus Ollama-powered
synthesis during coalesce. If omitted, extraction is silently skipped.

`mcp.access_key` is checked against the `x-brain-key` request header (or
`?key=` query param for clients that can't set headers). Leave blank during
local dev; set to the same key your AI clients send in production.

Every value can be overridden via CLI flag — run
`./open-brain-dashboard-go serve --help` for the full list.

## Deployment Modes

**Dev (desktop):** build and run on your workstation, pointing at
`home-server:5432` and `home-server:11434`. Fast iteration loop — edit
`.go` or `.templ`, `./build.sh run`, refresh browser. Requires the Ollama
port to be exposed on the LAN in your docker-compose stack (see the main
`docker-compose.yml` comment above the `ports:` block on the `ollama`
service).

**Prod (node-0):** the binary ships as a single self-contained
executable with every static asset embedded via `//go:embed static`,
so deployment is a plain systemd unit — no Docker ceremony required.

Actual deployed layout:

```
/home/open-brain/bin/
  open-brain-dashboard-go          # scp'd from a desktop cross-compile
  config.toml                      # db/ollama/mcp URLs → 127.0.0.1 loopback
```

```ini
# /etc/systemd/system/open-brain.service
[Unit]
Description=Open Brain (dashboard + MCP server)
After=network.target docker.service
Wants=docker.service

[Service]
Type=simple
User=jim
WorkingDirectory=/home/open-brain/bin
ExecStart=/home/open-brain/bin/open-brain-dashboard-go system --config /home/open-brain/bin/config.toml
Restart=on-failure
RestartSec=5s
SyslogIdentifier=open-brain

[Install]
WantedBy=multi-user.target
```

Caddy routes on node-0:
- `open-brain:80` → `localhost:8000` (MCP, replaces the old Deno container)
- `open-brain-dashboard:80` → `localhost:8082` (dashboard)

The config points `database.url` and `ollama.url` at `127.0.0.1`
because the open-brain docker-compose stack exposes Postgres on `5432`
and Ollama on `11434` on loopback. Only the MCP and dashboard containers
are retired — the DB and Ollama containers keep running.

Updates: cross-compile on desktop, `scp` to `/tmp/`, `systemctl stop
open-brain`, `cp /tmp/binary /home/open-brain/bin/`, `systemctl start
open-brain`. No image builds, no registry, no volume mounts.

## Layout

```
main.go               # go-flags entry point: serve / mcp / system / gen-config
config.go             # TOML config types + Load/Write/Default helpers (server, db, ollama, mcp)
db.go                 # pgx connection pool setup
ollama.go             # Embed (with truncateForEmbed guard), Extract, Synthesize
thoughts.go           # DB methods: Create, Update, Delete, Search, BulkDelete,
                      #   CoalesceThoughts, FetchContents, FindSimilar, SidebarCounts, ThoughtByID
thoughts_query.go     # SQL builders for list/count/bulk-delete queries
server.go             # chi router + all dashboard HTTP handlers
mcp.go                # MCP server: 7 tools via mark3labs/mcp-go, auth + CORS middleware
templates/            # templ components (.templ source; *_templ.go are generated)
static/
  tokens.css          # design tokens (dark + light palettes)
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

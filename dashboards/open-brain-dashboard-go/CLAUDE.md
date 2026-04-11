# CLAUDE.md — open-brain-dashboard-go

Agent instructions scoped to this dashboard. The repo-root `CLAUDE.md`
covers contributor rules for the whole Open Brain monorepo; this file
adds the dashboard's stack conventions and the Design Context that
should guide every UI decision here.

## Stack and conventions

- **Language:** Go 1.25+, single static binary, no CGO.
- **Router:** `github.com/go-chi/chi/v5`.
- **Templates:** `github.com/a-h/templ` — edit `.templ` files only, never
  the generated `*_templ.go`. `./build.sh` runs `templ generate` for you.
- **Frontend interactivity:** htmx, vendored at
  `static/vendor/htmx.min.js`. No npm, no bundler, no CDN.
- **CSS:** Design tokens in `static/tokens.css`, component styles in
  `static/app.css`. No frameworks. See Design Context below for the
  visual direction those tokens should express.
- **CLI parsing:** `github.com/jessevdk/go-flags` with `serve` and
  `gen-config` subcommands.
- **Config:** TOML via `BurntSushi/toml`, CLI flags override every field.
  The config file path itself must be a CLI flag.
- **Database:** `pgx/v5` pool talking directly to Postgres — no ORM,
  no migration framework. The dashboard must run against the stock
  `integrations/docker-compose-deployment/` schema without any DDL.
- **Embeddings:** Ollama `/v1/embeddings` over HTTP, model
  `mxbai-embed-large`.
- **Build:** Always `./build.sh` — never invoke `go build`, `go test`,
  or `templ generate` directly. Live reload uses `air` with `.air.toml`.

## Guard rails specific to this dashboard

- **Never add schema migrations.** Read and write metadata through jsonb
  paths. Adding indexes on jsonb expressions is fine; altering
  `thoughts` columns is not.
- **Every write path must work with JavaScript disabled.** The only JS
  in the page is vendored htmx. Any feature that can't POST-redirect-GET
  without htmx needs a plain-HTML fallback.
- **No client-side state framework.** The URL is the source of truth
  for which pane is active, what filters are applied, and what's
  selected. If you're reaching for localStorage or a JS state object
  to answer "what's on the screen right now," stop and put it in the
  query string instead.
- **No hover-only interactions.** Every interaction must work with a
  tap (phone) and a focus ring (keyboard).

## Design Context

### Users

A single user — Jim — using this as a personal memory dashboard on his
home LAN. No multi-tenancy, no onboarding flow, no auth surface. The
primary context of use is a desktop workstation running Arch Linux at
1440p+; a secondary context is a phone on the same Tailscale network.
The job to be done is "quickly find, review, edit, and maintain
thoughts captured by AI clients across Jim's fleet," not "demo a
product."

### Brand personality

**Utilitarian. Direct. Quiet.** The dashboard is a tool in the unix
sense — it does one thing well and gets out of the way. The voice
(in copy, error messages, empty states) is terse and peer-to-peer, not
instructional. No exclamation points. No "Oops!" No marketing gloss.
Short labels, definite articles, sentence case.

### Aesthetic direction

- **Primary reference:** Gmail + `~/projects/mail-processor`. A
  three-pane master/detail shell with a header toolbar, dense lists,
  and inline read/edit. The dashboard should feel like a sibling of
  mail-processor, built by the same hand.
- **Anti-references:** No gradient-heavy "AI dashboards," no Notion-
  card aesthetics, no drop shadows on drop shadows, no skeuomorphism,
  no decorative illustrations in empty states, no marketing color
  accents (no purple-to-pink gradients). Do not invent a logo.
- **Theme:** Dark by default (GitHub-dark palette already in
  `tokens.css`), with a light-mode toggle modeled on mail-processor's
  `localStorage`-backed switch. Tokens must come in both palettes so a
  future light palette is one file away, not a refactor.
- **Density:** Dense. The middle-pane thought list uses two-line rows
  (meta+snippet), targeting ~40–50 rows per 1080p screen. Comfortable
  is a trap — the whole point of a master/detail shell is to see a lot
  of context at once.
- **Typography:** System fonts only — `system-ui` for the sans stack,
  `ui-monospace` for code and raw-JSON disclosures. No web fonts, no
  CDN, no downloads.
- **Motion:** Minimal. htmx `hx-swap` default transitions are fine;
  anything richer (cross-fades, slide-ins, animated highlights) must
  earn its place by resolving a real affordance problem. Respect
  `prefers-reduced-motion`.

### Responsive strategy

**Desktop first, mobile viable.** Build the three-pane shell to spec
at 1280px+, but honor three rules so a phone variant is cheap to add
later:

1. CSS grid with named areas sized in `fr` and `min-content` — never
   fixed pixel pane widths.
2. The URL is the source of truth for the active pane (`/` list,
   `/?id=N` detail, `/?mode=new` compose). A phone variant only has
   to show one pane at a time based on these same URLs.
3. No JS that reads pane widths or assumes neighbors exist. htmx swaps
   must render correctly even if their target is the whole viewport.

Under these rules, the phone variant is ~40 lines of media queries
plus a collapsible sidebar drawer — not a rewrite.

### Design principles

1. **The URL is the UI state.** Every filter, every selection, every
   mode is a bookmarkable query string. Back button must work.
2. **Dense over pretty.** See more thoughts per screen. Whitespace
   that doesn't earn its keep is noise.
3. **Read and edit on the same surface.** The right pane swaps in
   place between read and edit. New thoughts use a Gmail-style
   compose panel (compact by default, modal on expand), because
   creating is contextually unbound. No **blocking** modals.
4. **Every interaction works with the keyboard and with JS off.**
   htmx is a progressive enhancement over POST-redirect-GET, not a
   prerequisite.
5. **Never decorate.** If a chip, badge, icon, shadow, or gradient
   doesn't encode information, it doesn't ship.

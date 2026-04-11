# Dashboard v1.5 — Master/Detail Refactor (Design)

**Date:** 2026-04-11
**Branch:** `self-hosted-ui-v1.5` (off `self-hosted-ui`)
**Supersedes:** v1.1's flat home/browse/search/detail layout

## Goal

Collapse the four flat screens (`/`, `/browse`, `/search`, `/thought/{id}`)
into a single Gmail-style master/detail shell so every navigation is a
partial pane swap, every interaction is URL-encoded, and the interface
stops feeling "jumpy." Along the way, wire one real bulk operation
(bulk delete) end-to-end so the shell's bulk-ops capability is proven
rather than scaffolded.

## Scope

**In:** master/detail shell, URL-driven state machine, unified search
with auto-fallback, Gmail-style compose panel with compact+modal modes,
bulk delete of the visible selection, dark+light theme toggle, legacy
route 303 redirects for backward-compat.

**Out (explicitly deferred to v2):** bulk re-embed, bulk tag-edit, bulk
set-type, "select all matching" beyond the visible page, search operators
(`"quoted"` substring), compose draft persistence, undo window on delete,
keyboard shortcut layer, phone-specific layout. All of these are possible
under the v1.5 shape and none are rebuild-blocked by omitting them.

## Design Principles (binding)

The five principles in `CLAUDE.md` (§Design Context) are the source of
truth. Restated here for convenience:

1. **The URL is the UI state.** Every filter, selection, and mode is a
   bookmarkable query string.
2. **Dense over pretty.** ~40–50 rows per 1080p screen.
3. **Read and edit on the same surface.** New thoughts use a Gmail-style
   compose panel instead; no blocking modals.
4. **Every interaction works with keyboard and with JS off.** htmx is
   progressive enhancement over POST-redirect-GET.
5. **Never decorate.** If a chip/icon/shadow doesn't encode information,
   it doesn't ship.

Principle #3 supersedes the absolutist "no modals" wording from the
first draft of CLAUDE.md. See §Compose below for the rationale.

## Routing and URL state

Everything renders at `/`. The query string fully encodes what's on the
screen. There are no other top-level routes.

| URL                                  | What renders                                           |
|--------------------------------------|--------------------------------------------------------|
| `/`                                  | Recent-50 list, empty detail pane                      |
| `/?type=task&days=7`                 | Filtered list, empty detail pane                       |
| `/?id=142`                           | Default list, detail pane shows thought 142 (read)     |
| `/?id=142&mode=edit`                 | Same list, detail pane shows 142 in edit form          |
| `/?q=deployment+pipeline`            | List re-ranked by semantic similarity to *q*           |
| `/?type=idea&q=Vulkan&id=99`         | Filtered+ranked list, detail pane shows 99             |

The compose panel is **not** in the URL. It's ephemeral draft state,
not navigable state — see §Compose below.

### Legacy redirects (all 303)

```
/home                 → /
/browse?...           → /?...
/search?q=...         → /?q=...           (mode= dropped)
/thought/{id}         → /?id={id}
/thought/{id}/edit    → /?id={id}&mode=edit
```

### htmx targeting

| Action                  | Request                                                | Target           | URL push |
|-------------------------|--------------------------------------------------------|------------------|----------|
| Filter chip click       | `GET /partials/list?...`                               | `#list-pane`     | yes      |
| Row click               | `GET /partials/detail/{id}?...filters`                 | `#detail-pane`   | yes      |
| Edit button             | `GET /partials/detail/{id}/edit?...filters`            | `#detail-pane`   | yes      |
| Save edit               | `POST /thought/{id}/edit` → fragment + `HX-Trigger`    | `#detail-pane`   | yes      |
| Cancel edit             | `GET /partials/detail/{id}?...filters`                 | `#detail-pane`   | yes      |
| Topic/person chip in detail | `GET /partials/list?topic=X`                       | `#list-pane`     | yes      |
| Open compose            | `GET /partials/compose`                                | `#compose-slot`  | no       |
| Close compose           | `DELETE /partials/compose` (returns empty)             | `#compose-slot`  | no       |
| Save new                | `POST /capture` → fragment + `HX-Trigger` triggers     | multiple         | yes      |
| Bulk delete             | `POST /bulk-delete` with `ids[]` from `#list-form`     | `#list-pane`     | no       |
| Row refresh after edit  | `GET /partials/row/{id}` on `HX-Trigger: refresh-row-{id}` | single row   | no       |

The shell page (`GET /`) renders the whole layout including whichever
detail-pane state matches the URL. Every partial endpoint also knows
how to render as a full page (just call the shell template with the
partial content slotted in), so direct navigation and back/forward work
against any URL.

## Filter sidebar (left rail)

Top to bottom:

1. **`+ New thought`** — primary-accent button. Fires
   `hx-get="/partials/compose"` targeting `#compose-slot`.
2. **Type chips** — `All · observation · task · idea · reference ·
   person_note`. Each chip shows its count trailing (`task (58)`).
   Single-select, URL param `type`. Active chip has filled background.
3. **Time window** — `All · 7d · 30d · 90d`. Single-select, URL param
   `days`.
4. **Topics** — top 10 topics by count, plain list. Click to filter
   (single-select, URL param `topic`). Overflow behind a `Show all` link
   that expands in place.
5. **People** — top 10 people, same pattern as topics, URL param `person`.
6. **Footer** — `142 thoughts · 12 this week` (the only stats), theme
   toggle `[☾]`, settings gear `[⚙]` (no-op placeholder).

All sidebar interactions target `#list-pane` and push URL. Combining
filters AND-combines them. Clicking an already-active single-select
chip clears it.

The sidebar is collapsible via a header toggle (same as mail-processor).
Collapse state is the one thing kept in `localStorage` — it's a
user-preference, not session state.

## List pane (middle)

### Toolbar (top of pane)

```
[☐ all]  [search …………………………]  [● ollama]     [Captured ▾ · Updated · Type]
```

- **Left** — select-all checkbox. CSS toggles the toolbar into bulk mode
  whenever any `.row-checkbox:checked` exists.
- **Center** — search box (`hx-trigger="keyup changed delay:300ms,
  search"` → `hx-get="/?q=..." hx-target="#list-pane" hx-push-url="true"`).
- **Ollama health dot** — adjacent to search. See §Ollama Health.
- **Right** — sort pills: `Captured · Updated · Type`. Single-select,
  arrow indicates direction, click the active one to reverse.

### Row shape

Two lines, fixed height, dense:

```
[☐]  observation · 2d                                       142
     First 120 chars of content, rune-clipped to one line…
```

- Type is a filled chip, color-coded by canonical type (the one place
  we use color to encode information).
- Timestamp: relative for < 7d (`2d`), absolute for older (`2026-03-14`).
- ID right-aligned in muted color for cross-reference convenience.
- Content snippet muted, single-line, ~120 runes (rune-aware helper).
- **No topic/person chips in rows.** They blow out row height; they live
  in the detail pane.
- Selected row (`?id=N`) has a left accent border and subtle bg tint.
- Click anywhere except the checkbox opens detail. Checkbox click is
  `stopPropagation`'d.

### Pagination

50 per page (up from v1.1's 25). Numbered links with ellipsis elision
at page 11+. No infinite scroll — pages are bookmarkable.

### Empty state

`"No thoughts match these filters."` + `[Clear filters]` link.

## Detail pane (right)

Three states, URL-driven, all swapped into `#detail-pane`.

```
         ┌───────────────┐
  /      │   (empty)     │       no id, no mode
         └───────────────┘
                 │
                 ▼
         ┌───────────────┐
  ?id=N  │     read      │       Edit button → mode=edit
         └───────────────┘       Delete → 303 to /
                 │
                 ▼
?id=N    ┌───────────────┐
&mode=   │     edit      │       Save → 303 to ?id=N  (read)
edit     └───────────────┘       Cancel → hx-get /?id=N (read)
```

Note that **new** is *not* a detail-pane state. It lives in the compose
panel — see §Compose below.

### Read state (`?id=N`)

```
[Edit]  [Delete]
──────────────────────────────
observation · captured 2 days ago
edited 3 hours ago

Full content, pre-wrap, line-length
capped around 72ch for readability.

Topics:  [work]  [open-brain]
People:  [@Alex]

Action items
  [high]  Follow up on rollback plan
  [med]   Check canary coverage

▸ Raw metadata
```

- Topic/person chips are `hx-get` links to `/?topic=X` / `/?person=X`.
- Action items are read-only here.
- `edited X ago` only renders when `metadata.updated_at` exists.
- Raw metadata `<details>` disclosure, collapsed by default.

### Edit state (`?id=N&mode=edit`)

Same fields as v1.1's `detail_edit.templ`, reused and adapted as a
templ component inside `detail_pane.templ`. Toolbar becomes `[Save]
[Cancel]`.

- Save → `POST /thought/{id}/edit` → 303 to `/?id=N&...filters`.
- Response fragment renders the read view into `#detail-pane` and sets
  header `HX-Trigger: refresh-row-{id}` so the corresponding list row
  re-fetches itself via `hx-trigger="refresh-row-{id} from:body"`.
- Cancel → plain `hx-get` to the read URL.
- Ollama embedding rules are unchanged from v1.1: recomputed only when
  content text changed, graceful-degrade re-renders edit form with an
  error banner on Ollama failure.

### Empty state

`"Select a thought or create a new one."` + secondary `+ New` button
that fires the compose panel. Shows when no `?id` is in the URL.

## Compose panel (floating, dual-mode)

New thoughts live in a floating compose panel, *not* in the detail pane,
because creating is contextually unbound and should be visually distinct
from editing an existing selection. Modeled on Gmail compose.

### Two modes

**Compact (default):** non-blocking floating panel, `position: fixed;
bottom: 0; right: var(--space-5)`, ~540×540 sized in rem. List and
detail panes remain fully interactive behind it. Title bar shows
`[↕ expand] [– minimize] [× close]`.

**Modal (on expand):** full-focus writing surface. Dimmed backdrop,
centered panel around ~760px wide. Title bar shows `[↘ contract] [×
close]`. **No minimize** — the whole point of modal mode is to be the
only thing on screen.

### Transitions

- `+ New thought` in sidebar → compose opens in compact.
- Compact `[↕]` → modal.
- Modal `[↘]` → compact (preserves draft).
- Click dimmed backdrop in modal → contracts to compact (preserves
  draft — does *not* close).
- `Esc` in modal → contracts to compact.
- `Esc` in compact → minimizes to title bar via `<details>` toggle.
- `[×]` in either mode → closes and discards draft.
- Minimize (`[–]`) → `<details>` collapse to just the title bar. Zero JS.

### Save path

- `POST /capture` with form data.
- Response fragment:
  - Clears `#compose-slot` via `hx-swap-oob` (empty target).
  - Sets `HX-Trigger: refresh-list, focus-thought-{new_id}`.
- `refresh-list` handler re-fetches `#list-pane`.
- `focus-thought-{new_id}` handler `hx-get`s `/partials/detail/{new_id}`
  into `#detail-pane` and `HX-Push-Url`s `/?id={new_id}`.
- Single user click → list refreshes, detail pane shows the new
  thought, URL settles. One action, everything in sync.

### JS-off fallback

The compose form has a real `action="/capture" method="POST"`. Without
htmx, it POSTs and the server responds with 303 to `/?id={new_id}` —
the shell renders fully, new thought shown, no dialog overlay. Progressive
enhancement intact.

### Implementation

Renders as a single `<dialog>` element. `dialog.show()` for compact
(non-modal), `dialog.showModal()` for modal-with-backdrop. Mode toggle
buttons are tiny inline `onclick` handlers calling `.close()` and the
appropriate opener — same class of "single-purpose event handler" as
v1.1's action-item remove button. No framework, no state outside the
DOM.

### Mobile degrade

Single media query below `~860px`: compact becomes full-width
bottom-sheet, modal stays centered but grows to fill ~95% of viewport.

## Search: one box, semantic-by-default, auto-fallback

No mode toggle in v1.5. Single search input in the list-pane toolbar,
URL param `?q=...`. The server picks the strategy based on Ollama state
and result count:

| Ollama state   | Semantic above threshold | Strategy            | Banner                                      |
|----------------|--------------------------|---------------------|---------------------------------------------|
| Up             | ≥ 1                      | Semantic ranking    | none                                        |
| Up             | 0                        | `ILIKE '%q%'` fallback | `"no semantic matches — showing text"`   |
| Down (fail)    | —                        | `ILIKE '%q%'` fallback | `"text-only — embeddings unavailable"` |

Threshold logic carries over from v1.1. Banners are muted, single-line,
above the list — not error styling.

Clear button (`×`) in the input fires `hx-get="/?..."` without the `q`
param, URL-pushing. Pressing Enter fires immediately (bypassing the
keyup debounce).

No search operators in v1.5. `"quoted substring"` is a v2 idea.

## Bulk delete: inline confirm, visible-page only

The list pane is wrapped in `<form id="list-form">` so all checkbox
state is form-state. Every htmx button that needs the current selection
includes `hx-include="#list-form"` to gather selected IDs.

### Toolbar states

Three states rendered into the same slot, CSS selects which shows using
`:has()`:

```
Normal:     [☐ all]  [search …………]  [● ollama]  [Captured ▾ · Updated · Type]
Bulk:       [☑ all]  3 selected  [Delete selected]  [Clear]
Confirm:    [☑ all]  Delete 3 thoughts? This is permanent.  [Yes, delete]  [Cancel]
```

### State transitions

- **Normal → Bulk**: checkbox check. Pure CSS —
  `.list-pane:has(.row-checkbox:checked) .toolbar-normal { display: none; }`
  and its mirror. Zero JS.
- **Bulk → Confirm**: click `[Delete selected]`. Tiny inline `onclick`
  sets `data-confirming` on the toolbar element. CSS uses the attribute
  to show confirm / hide bulk.
- **Confirm → Bulk**: click `[Cancel]`. Clears `data-confirming`. No
  server roundtrip. Selections preserved.
- **Confirm → Done**: click `[Yes, delete]`. htmx
  `hx-post="/bulk-delete" hx-include="#list-form" hx-target="#list-pane"`.
  Server runs a single `DELETE FROM thoughts WHERE id = ANY($1)`,
  returns re-rendered list pane. All checkboxes gone → toolbar back to
  Normal automatically via `:has()`.
- **Clear**: single-line inline JS —
  `document.querySelectorAll('.row-checkbox').forEach(c => c.checked = false)`.

### Detail pane side-effect

If the currently-open `?id=N` was in the deleted set, the server
detects this during re-render and emits `HX-Push-Url: /` (sans id) and
`HX-Trigger: clear-detail`. The handler `hx-get`s the empty state into
`#detail-pane`. Stale selection → empty pane, no 404.

### Out of scope for v1.5

- Select all matching beyond the visible page (v2, mirror of
  mail-processor's pattern)
- Undo window — deletion is immediate and permanent, consistent with
  v1.1's single-row delete
- Any other bulk op

## Stats and Ollama health

### Sidebar footer

```
142 thoughts · 12 this week
[☾]  [⚙]
```

Total and week counts. Theme toggle persists to `localStorage` and sets
`data-theme` on `<html>`. Gear is a reserved placeholder (no-op in
v1.5).

Type and topic distributions are encoded as chip counts inside the
sidebar itself (`task (58)`, `open-brain (6)`). No additional
visualization — the home page's type/topic distribution blocks are
deleted.

### Ollama health indicator

Small dot next to the search box:

- **Green**: last embed succeeded within 5 minutes
- **Amber**: last embed failed or > 5 minutes stale
- **Grey**: never called this session

Tooltip: `"embeddings: up at 12:04:33"` or `"embeddings: unavailable
(last checked 12:04:33)"`. Updates on actual embed calls (capture,
content-edit, search). No polling, no background heartbeat.

State lives on the server (in the actor that owns Ollama health) and is
sent to the client via the shell render + `HX-Trigger: ollama-state-{up|down}`
on every embed call. A tiny client handler updates the dot's class.

## Transition plan

### Branch

`self-hosted-ui-v1.5` off `self-hosted-ui`. Build incrementally —
every increment should run and serve pages, even if some panes are
placeholder. Delete old v1.1 templ files only at the end, once
everything is wired.

### File inventory

**Stays unchanged:**
- `main.go`, `config.go`, `db.go`, `ollama.go`

**Augmented:**
- `thoughts.go` — add `BulkDelete(ctx, ids)` and a unified
  `Search(ctx, filters, q, page)` that internally picks
  semantic/text/fallback
- `helpers.go` — toolbar state helpers, URL builders for the unified
  route

**Rewritten:**
- `server.go` — routes collapse to `/` + `/partials/*` + legacy
  redirects. Single shell handler dispatches to read/edit/empty based
  on query params.
- `layout.templ` — becomes the three-pane grid shell with named areas
  + `#compose-slot` sibling
- `app.css` — near-full rewrite. v1.1 styles assumed full-page flows;
  v1.5 needs grid shell, pane CSS, dense list rows, detail pane
  states, compose dialog styles. Cleaner to rewrite than patch.

**Deleted at the end:**
- `home.templ`, `browse.templ`, `search.templ`, `detail.templ`,
  `detail_edit.templ`, `thought_card.templ`, `partials.templ` and
  their generated `*_templ.go` counterparts

**New:**
- `shell.templ` — three-pane grid layout
- `sidebar.templ` — filter rail
- `list.templ` — middle pane with toolbar (normal/bulk/confirm), row
  template, pagination, empty state
- `detail_pane.templ` — `DetailRead`, `DetailEdit`, `DetailEmpty` templ
  functions; `ActionItemRow` moves here
- `compose.templ` — `<dialog>` compose panel with dual-mode styling
- `partials_row.templ` — single-row render for `refresh-row-{N}`
  triggers
- `static/js/app.js` — < 50 lines total. Theme toggle, sidebar
  collapse, compose mode switchers, clear-selection helper,
  action-item add/remove (the last one carried over from v1.1).

### Token additions

`tokens.css` gets a light palette alongside the existing dark. Toggled
via `[data-theme="light"]` on `<html>`. Both palettes must ship in
v1.5 even though the default stays dark.

### CLAUDE.md update

Principle #3 relaxes to:

> **Read and edit on the same surface.** The right pane swaps in place
> between read and edit. New thoughts use a Gmail-style compose panel
> (compact by default, modal on expand), because creating is
> contextually unbound. No **blocking** modals.

Committed as part of the design doc PR.

## Out of scope for v1.5 (recap)

- Bulk re-embed, bulk tag-edit, bulk set-type
- Select-all-matching beyond the visible page
- Search operators (`"quoted substring"`)
- Compose draft persistence
- Undo window on delete
- Keyboard shortcut layer (`/` to focus search, `c` to open compose)
- Phone-specific layout refinements beyond the single collapse media
  query
- Node-0 deployment — that lands *after* v1.5 is merged

## Next step

Hand off to the `writing-plans` skill to produce a step-by-step
implementation plan from this design.

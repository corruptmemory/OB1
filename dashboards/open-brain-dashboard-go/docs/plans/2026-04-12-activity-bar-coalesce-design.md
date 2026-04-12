# Activity Bar + Coalesce Thoughts — Design

Date: 2026-04-12

## Activity Bar

### Grid Change

The `.app` grid gains a leading 48px column for a vertical icon strip:

```
activity | sidebar | list | handle | detail
 48px      18rem    1fr    4px     var(--detail-width)
```

### Markup

A `<nav class="activity-bar">` before the sidebar in the shell template.
Each icon is an `<a>` with `href="/?view=X"` and htmx attributes for
partial swap. Active view gets a left-border accent highlight.

### Views (initial)

| Icon | `?view=` | Status |
|------|----------|--------|
| Catalogue (list icon) | `catalogue` (default) | Active |
| Dashboard (chart icon) | `dashboard` | Placeholder |
| Organize (merge icon) | `organize` | Placeholder |

Theme toggle moves from sidebar footer to activity bar bottom (it's
app-global, not view-specific).

### Server Side

`handleShell` reads `?view=` param. `catalogue` (or empty) renders the
current shell. Unimplemented views render a placeholder. No new routes.

### Mobile

Activity bar hidden below 1216px. URL `?view=` still works via direct
navigation.

## Coalesce Thoughts

### Trigger

"Coalesce" button in the bulk toolbar, next to "Delete selected" and
"Clear". Server rejects requests with fewer than 2 IDs.

### Flow

1. User selects N thoughts, clicks "Coalesce"
2. `POST /coalesce` with `ids[]` (htmx `hx-include="#list-form"`)
3. Server fetches all N thoughts' content from DB
4. Server calls `Synthesize()` — Ollama chat with a merging prompt
5. Server calls `Extract()` on the synthesized content for metadata
6. Response renders a compose-like panel in `#compose-slot`:
   - AI-synthesized content (editable textarea)
   - AI-extracted metadata (type, topics, people, action items)
   - "Replace N thoughts" submit button
   - Hidden `ids[]` fields carrying the originals
   - Cancel dismisses
7. Submit `POST /coalesce/confirm` with edited content + metadata + ids[]
8. Server: `BEGIN` → insert new thought with embedding → bulk delete
   originals → `COMMIT`
9. Response: refresh list + focus new thought in detail pane

### New Ollama Method

`Synthesize(ctx, contents []string) (string, error)` — calls `/api/chat`
with a consolidation prompt. Returns merged text. Falls back to
concatenated content if Ollama is unavailable.

### New Endpoints

- `POST /coalesce` — read-only synthesis, returns pre-filled compose panel
- `POST /coalesce/confirm` — destructive replace (insert + delete in tx)

### Error Handling

- Ollama down: render compose panel with concatenated raw content as
  fallback, plus a warning banner
- < 2 IDs: 400 error
- Transaction failure: error in compose panel, originals untouched

### JS-off Fallback

- `POST /coalesce` returns full page with compose form
- `POST /coalesce/confirm` returns 303 → `/?id={newId}`

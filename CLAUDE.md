# CLAUDE.md — Agent Instructions for Open Brain

This file helps AI coding tools (Claude Code, Codex, Cursor, etc.) work effectively in this repo.

## What This Repo Is

Open Brain is a persistent AI memory system — one database (Supabase + pgvector), one MCP protocol, any AI client. This repo contains the extensions, recipes, schemas, dashboards, integrations, and skills that the community builds on top of the core Open Brain setup.

**License:** FSL-1.1-MIT. No commercial derivative works. Keep this in mind when generating code or suggesting dependencies.

## Repo Structure

```
extensions/     — Curated, ordered learning path (6 builds). Do NOT add without maintainer approval.
primitives/     — Reusable concept guides (must be referenced by 2+ extensions). Curated.
recipes/        — Standalone capability builds. Open for community contributions.
schemas/        — Database table extensions. Open.
dashboards/     — Frontend templates (Vercel/Netlify). Open.
integrations/   — MCP extensions, webhooks, capture sources. Open.
skills/         — Reusable AI client skills and prompt packs. Open.
docs/           — Setup guides, FAQ, companion prompts.
resources/      — Official companion files and packaged exports.
server/         — Reference MCP server (Deno + Hono + Supabase Edge Function).
                  Single file: server/index.ts. The canonical implementation
                  of the "remote MCP only" guard rail. Deno import map is in
                  server/deno.json; no test or build tasks — deployed via
                  `supabase functions deploy`.
```

Every contribution lives in its own subfolder under the right category and must include `README.md` + `metadata.json`. New contributions start by copying `recipes/_template/` (or `primitives/_template/`), never by creating a folder from scratch — the templates carry the exact `metadata.json` shape the `ob1-gate.yml` validator expects.

## Reference MCP Server Architecture

The `server/` directory contains the canonical Open Brain MCP server — a single `index.ts` file (~400 lines) that demonstrates every architectural rule in this repo. Read it before building a new MCP extension; recipes and integrations that add tools should mirror its shape.

- **Stack:** Deno runtime, Hono HTTP framework, `@modelcontextprotocol/sdk` with `StreamableHTTPTransport` from `@hono/mcp`, `@supabase/supabase-js` for DB access. No build step — Deno runs the TypeScript directly; `server/deno.json` is an import map only (no tasks).
- **Tools exposed:** `search_thoughts` (vector search via `match_thoughts` RPC), `list_thoughts` (filtered fetch with `type`/`topic`/`person`/`days`), `thought_stats` (aggregate counts), `capture_thought` (embedding + metadata extraction + `upsert_thought` RPC, then a follow-up embedding UPDATE).
- **Data path:** Input text → OpenRouter embeddings (`text-embedding-3-small`) + OpenRouter metadata extraction (`gpt-4o-mini` in JSON mode) → `thoughts` table via the `upsert_thought` RPC → second write sets the embedding column. Retrieval uses the `match_thoughts` RPC with a similarity threshold.
- **Auth:** Custom `x-brain-key` header, also accepted as `?key=` query param for clients that can't set headers. Validated against the `MCP_ACCESS_KEY` env var before any MCP dispatch.
- **Claude Desktop compatibility quirk:** Claude Desktop connectors don't send the `Accept: text/event-stream` header that `StreamableHTTPTransport` requires. `server/index.ts` patches the incoming request to add it (search for the block referencing `NateBJones-Projects/OB1#33`). Do not remove this workaround without testing against Claude Desktop.
- **Required env vars:** `SUPABASE_URL`, `SUPABASE_SERVICE_ROLE_KEY`, `OPENROUTER_API_KEY`, `MCP_ACCESS_KEY`.

## Build, Test, Deploy

There is no root-level build, test, or lint harness. This repo is a collection of drop-in contributions, not a single application:

- **server/** — Deno + Supabase Edge Function. No tests. Deployed with `supabase functions deploy <name>` from the user's own Supabase project. Deno handles TypeScript directly; no compile step.
- **recipes/**, **integrations/**, **schemas/** — each folder's `README.md` is the source of truth for how to install and verify that specific contribution. There is no aggregate test suite.
- **dashboards/** — each dashboard has its own package manager (npm/pnpm) and its own dev/build/deploy commands in its own README.
- **PR validation** runs in CI via `.github/workflows/ob1-gate.yml` and `.github/workflows/claude-review.yml`. Both run against any PR without local setup.

## Guard Rails

- **Never modify the core `thoughts` table structure.** Adding columns is fine; altering or dropping existing ones is not.
- **No credentials, API keys, or secrets in any file.** Use environment variables.
- **No binary blobs** over 1MB. No `.exe`, `.dmg`, `.zip`, `.tar.gz`.
- **No `DROP TABLE`, `DROP DATABASE`, `TRUNCATE`, or unqualified `DELETE FROM`** in SQL files.
- **MCP servers must be remote (Supabase Edge Functions), not local.** Never use `claude_desktop_config.json`, `StdioServerTransport`, or local Node.js servers. All extensions deploy as Edge Functions and connect via Claude Desktop's custom connectors UI (Settings → Connectors → Add custom connector → paste URL). See `docs/01-getting-started.md` Step 7 for the pattern.

## PR Standards

- **Title format:** `[category] Short description` (e.g., `[recipes] Email history import via Gmail API`, `[skills] Panning for Gold standalone skill pack`)
- **Branch convention:** `contrib/<github-username>/<short-description>`
- **Commit prefixes:** `[category]` matching the contribution type
- Every PR must pass the automated gate in `.github/workflows/ob1-gate.yml` and the AI review in `.github/workflows/claude-review.yml` before human review
- See `CONTRIBUTING.md` for the full review process, metadata.json template, and README requirements

## Key Files

- `CONTRIBUTING.md` — Source of truth for contribution rules, metadata format, and the review process
- `.github/workflows/ob1-gate.yml` — Mechanical PR gate (structure, secrets, SQL safety, deps)
- `.github/workflows/claude-review.yml` — AI-powered substantive PR review
- `.github/metadata.schema.json` — JSON schema for metadata.json validation
- `.github/PULL_REQUEST_TEMPLATE.md` — PR description template
- `LICENSE.md` — FSL-1.1-MIT terms

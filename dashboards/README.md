# Dashboards

https://github.com/user-attachments/assets/9454662f-2648-4928-8723-f7d52e94e9b8

Frontend templates for your Open Brain. Three community-built options today,
covering both the hosted-Supabase and self-hosted deployment models.

| Dashboard | Stack | Deployment | Best For |
|-----------|-------|------------|----------|
| [`open-brain-dashboard`](open-brain-dashboard/) | SvelteKit + Tailwind + Supabase Auth | Vercel / Netlify | Hosted Supabase setups, minimal dashboard with MCP proxy |
| [`open-brain-dashboard-next`](open-brain-dashboard-next/) | Next.js 16 + React 19 + Tailwind + iron-session | Vercel / Netlify / any Node.js host | Hosted Supabase setups, full-featured 8-page UI with smart ingest and quality auditing |
| [`open-brain-dashboard-go`](open-brain-dashboard-go/) | Go + chi + templ + htmx + pgx + pgvector | Single static binary (host, systemd, or docker-compose sidecar) | Self-hosted stacks, talks directly to Postgres + Ollama, pairs with [`integrations/docker-compose-deployment`](../integrations/docker-compose-deployment/) |

The two hosted-Supabase dashboards assume a deployed `open-brain-rest` Edge
Function and the Supabase auth context. The Go dashboard assumes a local
Postgres + pgvector + Ollama reachable over the network and does not
require any edge-function intermediary. Pick the one that matches your
deployment, not the framework you happen to know.

## Ideas for new dashboards

- Weekly review view — summarize the week's captures by type and topic
- Mobile-friendly capture UI — big text area, single tap to submit
- Knowledge-graph visualizer — thoughts as nodes, shared topics as edges
- Calendar heatmap of capture activity — GitHub-contributions-style grid
- Action-items kanban — pulled from `metadata.action_items` across thoughts

## Contributing

Dashboards are open for community contributions. See [CONTRIBUTING.md](../CONTRIBUTING.md) for details. Start by copying [`_template/`](_template/) and replacing the contents with your dashboard's README and metadata — the template carries the exact `metadata.json` shape the automated PR gate expects.

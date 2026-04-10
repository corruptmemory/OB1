# Docker Compose Self-Hosted Deployment

> Deploy Open Brain with a single `docker-compose.yml` on any Linux host. Runs PostgreSQL + pgvector, the MCP server, and a local Ollama for embeddings and metadata extraction. No Supabase, no OpenRouter, no cloud dependencies.

This is a companion to the [Kubernetes integration](../kubernetes-deployment/) for people who want self-hosted Open Brain but don't need the operational complexity of Kubernetes. It targets single-host deployments — a home server, a VPS, a laptop in a basement — where `docker compose up -d` is the right level of infrastructure.

## What it runs

Three containers on an internal `open-brain` network:

| Container | Image | Purpose | Host port |
| --- | --- | --- | --- |
| `open-brain-db` | `ankane/pgvector:v0.5.1` | PostgreSQL 16 + pgvector, bootstrapped from `init.sql` on first start | `5432` (LAN-visible) |
| `open-brain-ollama` | `ollama/ollama:latest` | Local Ollama serving embedding and chat models via the OpenAI-compatible `/v1` API | not published (internal only) |
| `open-brain-mcp` | built from `Dockerfile` | Deno + Hono MCP server exposing four tools over authenticated HTTP | `8000` (localhost only — front with a reverse proxy for LAN access) |

The MCP server reaches Ollama over the internal Docker network at `http://ollama:11434/v1` using the standard OpenAI request shape. The `Dockerfile` and `deno.json` are unmodified copies from the Kubernetes integration — this deployment differs in three places: the data-plane wiring (compose instead of k8s manifests), the choice of embedding model (`mxbai-embed-large`, 1024-dim, open-weight) instead of OpenAI's `text-embedding-3-small`, and a small `index.ts` patch (see "Divergence from the Kubernetes integration" below).

## Divergence from the Kubernetes integration

The `index.ts` in this directory is forked from `integrations/kubernetes-deployment/index.ts` with exactly **one source-level change**: the authenticated request handler patches the incoming `Accept` header to include `text/event-stream` if the client didn't send it. This workaround is required because Claude Desktop connectors and many HTTP clients (including `curl` by default) don't send the `Accept: application/json, text/event-stream` header that the MCP `StreamableHTTPTransport` requires, and without the patch those clients receive a `Not Acceptable` error on every call.

The same fix exists in `server/index.ts` (the Supabase-flavor reference server) with a comment referencing [NateBJones-Projects/OB1#33](https://github.com/NateBJones-Projects/OB1/issues/33). The Kubernetes integration was forked from an older version of `server/index.ts` that predated the fix and never got it backported. We ported it here because testing confirmed that without it, no interactive MCP client worked. Consider this a candidate upstream improvement for the Kubernetes integration as well.

## Prerequisites

- Docker 24+ with the Compose plugin (`docker compose`, not legacy `docker-compose`)
- A Linux host with at least 4 GB free RAM (Ollama keeps ~3 GB of models in memory with `OLLAMA_KEEP_ALIVE=-1`)
- ~5 GB free disk space for models and Postgres data
- A non-root user in the `docker` group (or willingness to `sudo docker compose ...`)
- Internet access on first boot for image pulls and the initial Ollama model downloads — after that, zero internet dependency

No ROCm, no CUDA, no GPU passthrough, no BIOS changes. This runs on CPU.

## Credential tracker

Copy this block into a text editor and fill it in as you go. Both values should be strong random strings — see the generation commands in the Quick Start section below.

```text
DOCKER COMPOSE DEPLOYMENT -- CREDENTIAL TRACKER
-----------------------------------------------

POSTGRESQL
  Password:         ____________

MCP SERVER
  Access key:       ____________

DEPLOY HOST
  Hostname / IP:    ____________
  psql endpoint:    ____________:5432
  MCP endpoint:     http://____________:8000

-----------------------------------------------
```

## Quick start

All commands run from `integrations/docker-compose-deployment/`.

```bash
# 1. Create the .env file from the template
cp .env.example .env
chmod 600 .env

# 2. Generate strong secrets and paste them into .env
#    (manually edit .env and replace the "replace-me-with-..." placeholders)
echo "POSTGRES_PASSWORD=$(openssl rand -base64 32 | tr -d '=+/' | cut -c1-32)"
echo "MCP_ACCESS_KEY=$(openssl rand -hex 32)"

# 3. Bring up the stack (first run downloads images and builds the MCP server)
docker compose up -d

# 4. Pull the Ollama models (~2.7 GB total, one-time download)
bin/pull-models.sh

# 5. Verify the MCP endpoint responds with the four tools
curl -s http://localhost:8000 \
  -H "x-brain-key: $(grep '^MCP_ACCESS_KEY=' .env | cut -d= -f2-)" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"tools/list","id":1}' | jq
```

The last command should return a JSON-RPC response listing `search_thoughts`, `list_thoughts`, `thought_stats`, and `capture_thought`. If you see that, the stack is working.

First-time model downloads take a few minutes depending on your connection. Subsequent `docker compose up -d` calls are fast because the bind mount at `./data/ollama` preserves the model files.

## Exposing the MCP server to other hosts

The default `docker-compose.yml` binds the MCP server to **`127.0.0.1:8000`** — it's reachable from the host it runs on, but **not** from other machines on your network. This is the secure default. Before external clients (Claude Desktop on another computer, Claude Code on a laptop, etc.) can reach it, you need one of these:

**Option A (recommended): Front it with a reverse proxy on the host.** The host presumably already runs a reverse proxy like Caddy, nginx, or Traefik if it hosts any other services. Add a hostname-based route that forwards to `localhost:8000`. Example Caddyfile block:

```caddyfile
open-brain:80 {
    reverse_proxy localhost:8000
}
```

Then set up DNS so `open-brain` resolves to the host, and clients can use `http://open-brain/` as the endpoint. The reverse proxy handles port-multiplexing (multiple services on one port), optional TLS, and any additional access-control layers. The MCP server's own `x-brain-key` check remains as a second line of defense.

**Option B (simpler, less defense-in-depth): Bind the container directly to the LAN.** Change the port line in `docker-compose.yml` from `"127.0.0.1:8000:8000"` to `"0.0.0.0:8000:8000"` and `docker compose up -d mcp-server`. Clients then reach it directly at `http://<host>:8000/`. The `x-brain-key` header is the only thing between a LAN attacker and your MCP endpoint; this is fine on a trusted home network but worse than a reverse proxy with additional auth layers.

## Connecting MCP clients

The examples below assume you're using Option A with a reverse proxy front door at `http://open-brain/`. Substitute `http://<host>:8000/` if you're using Option B.

### Claude Desktop

Settings → Connectors → Add custom connector → paste the URL, then add a custom header with key `x-brain-key` and the value of `MCP_ACCESS_KEY` from `.env`. The four tools show up in the tool list on the next chat restart.

### Claude Code

```bash
claude mcp add open-brain --transport http http://open-brain/ \
  --header "x-brain-key: $(grep '^MCP_ACCESS_KEY=' .env | cut -d= -f2-)"
```

Check the exact flag names with `claude mcp add --help` if your Claude Code version differs — the HTTP transport and custom headers are both supported but flag spelling has evolved.

### Direct psql access

Since the `db` service publishes port 5432 to `0.0.0.0`, anyone on the host network can connect directly with:

```bash
psql -h <host> -U openbrain -d openbrain
# password: value of POSTGRES_PASSWORD from .env
```

This is useful for ad-hoc queries, schema inspection, backups, and bulk imports. If you don't want the Postgres port on the LAN, change the port mapping in `docker-compose.yml` from `0.0.0.0:5432:5432` to `127.0.0.1:5432:5432`.

## Performance expectations

`capture_thought` does two Ollama calls in parallel: embedding (fast) and metadata extraction (slower). On a Ryzen 7 PRO 5850U-class CPU, expect roughly:

| Operation | Model | Typical latency |
| --- | --- | --- |
| `getEmbedding` | `mxbai-embed-large` | 50–200 ms |
| `extractMetadata` | `qwen2.5:3b` | 10–18 s (first call of the session may be slower) |
| `capture_thought` total | both | dominated by metadata extraction |
| `search_thoughts` | `mxbai-embed-large` + pgvector | under 500 ms |
| `list_thoughts` | pgvector only | under 100 ms |
| `thought_stats` | pgvector only | under 100 ms |

The metadata extraction cost is the main latency. It's tolerable for interactive use because the MCP client treats the call as async — you fire it and keep working. For bulk imports, plan on several seconds per thought.

If 10–18 seconds per capture feels too slow, switch to a smaller chat model:

```bash
# Pull a 1.5B-parameter alternative (~half the latency, modest quality drop)
docker exec open-brain-ollama ollama pull qwen2.5:1.5b

# Edit .env to use it
echo "CHAT_MODEL=qwen2.5:1.5b" >> .env

# Restart just the MCP server (ollama keeps running)
docker compose up -d mcp-server
```

The even smaller `qwen2.5:0.5b` and `llama3.2:1b` are much faster but produce lower-quality structured output and may hallucinate metadata fields.

`OLLAMA_KEEP_ALIVE=-1` is set in `docker-compose.yml` to pin loaded models in RAM permanently, so the first capture after an idle period doesn't pay the ~2 GB load-from-disk cost. This uses ~3 GB of resident memory continuously in exchange for consistent latency across the day. If your host has tight RAM, change it to a duration like `30m` and accept the occasional cold-start spike.

## GPU acceleration

The default configuration is **CPU-only and should not be changed without measuring first**. For integrated AMD graphics (Vega, RDNA1/2 iGPUs), GPU acceleration does not provide a meaningful speedup for LLM inference because transformer decode is memory-bandwidth-bound, not compute-bound, and the integrated GPU shares the same system RAM bus as the CPU. See the "Direction 2 — Vulkan backend" section of the design discussion that produced this integration for the full analysis.

If you have a discrete GPU (NVIDIA CUDA, or AMD RDNA3+ Radeon with ROCm support), Ollama can use it. You'll need to:

1. Install the NVIDIA Container Toolkit or the ROCm device plugin for Docker on the host.
2. Add a `deploy.resources.reservations.devices` block (Swarm) or the legacy `runtime: nvidia` + `--gpus all` equivalent to the `ollama` service in `docker-compose.yml`.
3. Verify with `docker exec open-brain-ollama ollama ps` that the loaded model shows a GPU layer count greater than 0.

This is out of scope for the default integration and intentionally not wired up here.

## Troubleshooting

**`docker compose up` fails with "POSTGRES_PASSWORD is required"**
You haven't created `.env` from the template. Run `cp .env.example .env` and fill in the two required values.

**`docker compose logs mcp-server` shows `ConnectionRefused` errors right after startup**
This is cosmetic noise for the first ~10 seconds, not a real failure. The MCP server's Postgres pool eagerly tries to pre-warm connections on boot, and `depends_on` only waits for the `db` container to start (not for Postgres inside to accept connections). Deno logs the unhandled promise rejection but keeps `Deno.serve` listening on port 8000, so the container stays up throughout. Once Postgres is ready (typically within 10 seconds of first boot), subsequent requests work normally. If the errors persist longer than 30 seconds or the container actually restarts, something else is wrong — check `docker compose logs db` to see if Postgres is even starting.

**`bin/pull-models.sh` fails with "container not running"**
Run `docker compose ps` and confirm `open-brain-ollama` shows Status "Up". If it's not, check `docker compose logs ollama` for the reason.

**`capture_thought` returns metadata with only `topics: ["uncategorized"]`**
This is the fallback path in `extractMetadata` — it triggers when the chat model's response fails to parse as JSON. Usually this means Ollama's OpenAI-compatible layer did not honor the `response_format: { type: "json_object" }` directive, and the model returned plain prose. The default configuration (`qwen2.5:3b` + Ollama 0.20.5) has been verified end-to-end — JSON mode works and metadata is extracted correctly. If you see the fallback path with a different chat model, that model likely ignores JSON mode regardless of what Ollama passes through. Workarounds: switch to a model that's known to honor `response_format` (the Qwen 2.5 family is a reliable default), or tweak the extraction prompt in `index.ts` to force JSON output through instruction alone.

**`psql: FATAL: role "openbrain" does not exist`**
The database didn't initialize. This usually means `./data/postgres` already contains a database from a previous run with different credentials. Stop the stack (`docker compose down`), delete the directory (`rm -rf data/postgres`), and start again (`docker compose up -d`). **This destroys any captured thoughts.** For an in-place password change, connect to the existing database with the old password and run `ALTER USER openbrain WITH PASSWORD '<new>';` manually.

**`Embedding API failed: 404`**
The embedding model hasn't been pulled into the Ollama container yet. Run `bin/pull-models.sh` — it's a post-`docker compose up` step that the compose file cannot automate.

**Vector dimension mismatch errors on capture**
You changed `EMBEDDING_MODEL` to a model with a different output dimension without also updating `init.sql` and recreating the `thoughts` table. See the comment at the top of `init.sql` for what to change. You cannot migrate between embedding dimensions in place — existing vectors have to be dropped or re-embedded.

## Updating

```bash
docker compose pull                      # pull new db and ollama images
docker compose build mcp-server          # rebuild the Deno server image
docker compose up -d                     # recreate containers with new images
```

The bind mounts at `./data/postgres` and `./data/ollama` persist across recreations, so you don't lose data or re-download models during an update.

## Uninstall

```bash
docker compose down                      # stop and remove containers
rm -rf data/                             # DESTROYS all captured thoughts and downloaded models
rm .env                                  # remove local secrets
```

## Directory layout

```
docker-compose-deployment/
├── README.md              — this file
├── metadata.json          — Open Brain contribution metadata
├── docker-compose.yml     — service definitions
├── Dockerfile             — MCP server container build (unchanged from kubernetes-deployment/)
├── index.ts               — MCP server source, forked from kubernetes-deployment/ with Accept-header patch
├── deno.json              — Deno import map (unchanged from kubernetes-deployment/)
├── init.sql               — Postgres bootstrap, forked with vector(1024) for mxbai-embed-large
├── .env.example           — committed template with placeholders
├── .env                   — gitignored, user-created, contains real secrets
├── .gitignore             — ignores .env and data/
├── bin/
│   └── pull-models.sh     — post-deploy Ollama model pull
└── data/                  — gitignored, created on first `docker compose up`
    ├── postgres/          — Postgres cluster state
    └── ollama/            — Downloaded Ollama models
```

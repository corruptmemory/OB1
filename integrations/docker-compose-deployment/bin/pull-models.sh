#!/usr/bin/env bash
#
# Pull the Ollama models required by Open Brain into the running ollama
# container. Run this once after the first `docker compose up -d`. Models
# persist in the ./data/ollama bind mount across container restarts, so
# this is a one-time download.
#
# Usage:
#   bin/pull-models.sh               # pull both defaults
#   EMBEDDING_MODEL=nomic-embed-text bin/pull-models.sh   # override
#
# The container must be running (`docker compose ps` shows open-brain-ollama
# as "Up") before this script can succeed.

set -euo pipefail

CONTAINER="${OLLAMA_CONTAINER:-open-brain-ollama}"
EMBEDDING_MODEL="${EMBEDDING_MODEL:-mxbai-embed-large}"
CHAT_MODEL="${CHAT_MODEL:-qwen2.5:3b}"

# Use -it flags only when we actually have a TTY, so the script works from
# both interactive shells (pretty progress bars) and non-interactive contexts
# like CI or agent-driven deploys (plain streamed output).
if [ -t 0 ] && [ -t 1 ]; then
    DOCKER_TTY_FLAGS="-it"
else
    DOCKER_TTY_FLAGS=""
fi

if ! docker ps --format '{{.Names}}' | grep -qx "${CONTAINER}"; then
    echo "error: container '${CONTAINER}' is not running"
    echo "       run 'docker compose up -d' first"
    exit 1
fi

echo "==> pulling embedding model: ${EMBEDDING_MODEL}"
docker exec ${DOCKER_TTY_FLAGS} "${CONTAINER}" ollama pull "${EMBEDDING_MODEL}"

echo "==> pulling chat model: ${CHAT_MODEL}"
docker exec ${DOCKER_TTY_FLAGS} "${CONTAINER}" ollama pull "${CHAT_MODEL}"

echo
echo "==> done. models available in ${CONTAINER}:"
docker exec "${CONTAINER}" ollama list

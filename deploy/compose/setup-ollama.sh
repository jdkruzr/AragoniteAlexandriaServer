#!/bin/sh
# Start the optional local provider and download a model. This does not change
# application settings or queue any indexing/recognition work.
set -eu
cd "$(dirname "$0")"
. ./common.sh
model=${1:-nomic-embed-text:v1.5}
compose --profile embeddings up -d --no-deps ollama
attempt=0
until compose --profile embeddings exec -T ollama ollama list >/dev/null 2>&1; do
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 60 ]; then
        echo 'Ollama did not become ready; inspect the ollama service logs.' >&2
        exit 1
    fi
    sleep 1
done
compose --profile embeddings exec -T ollama ollama pull "$model"
printf '\nOllama is available at http://ollama:11434 with model %s.\n' "$model"
echo 'Save that URL/model in Settings. Enabling semantic search and building its index are separate actions.'
echo 'Set ALEXANDRIA_OLLAMA_ENABLED=true in .env to include it in installer/backup lifecycle commands.'

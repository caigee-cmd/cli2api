#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${ROOT_DIR}/deploy/.env"
COMPOSE=(docker compose --env-file "${ENV_FILE}" -f "${ROOT_DIR}/deploy/docker-compose.yml")

if [[ ! -f "${ENV_FILE}" ]]; then
  cp "${ROOT_DIR}/deploy/.env.example" "${ENV_FILE}"
  echo "Created ${ENV_FILE}; SQLite will generate the API key on first startup."
fi

started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
if ! "${COMPOSE[@]}" pull; then
  echo "Published image unavailable; building the image locally."
  "${COMPOSE[@]}" up -d --build
else
  "${COMPOSE[@]}" up -d
fi

for _ in {1..60}; do
  if curl -fsS http://127.0.0.1:3010/health >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

if ! curl -fsS http://127.0.0.1:3010/health >/dev/null 2>&1; then
  "${COMPOSE[@]}" ps
  "${COMPOSE[@]}" logs --no-color --tail=100 qoder-api-proxy
  exit 1
fi

echo "CLI2API is running at http://127.0.0.1:3010"
new_key="$("${COMPOSE[@]}" logs --no-color --since "${started_at}" qoder-api-proxy 2>/dev/null | grep -F 'initialized API key' || true)"
if [[ -n "${new_key}" ]]; then
  printf '%s\n' "${new_key}"
else
  echo "The API key is already stored in the SQLite database."
fi

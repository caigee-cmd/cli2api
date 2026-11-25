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

# Let the console pull and apply new images itself. On Linux this needs the host
# updater once; install it here so a plain ./scripts/start.sh deployment is
# self-updating. Opt out with CLI2API_SKIP_UPDATER_INSTALL=1.
if [[ "$(uname -s)" == "Linux" ]] && command -v systemctl >/dev/null 2>&1 \
  && [[ ! -f /etc/systemd/system/cli2api-updater.service ]] \
  && [[ "${CLI2API_SKIP_UPDATER_INSTALL:-0}" != "1" ]]; then
  echo "Installing the managed updater so the console can update this stack itself."
  if [[ "${EUID}" -eq 0 ]]; then
    "${ROOT_DIR}/deploy/install-updater.sh" || echo "Managed updater install failed; run sudo ./deploy/install-updater.sh manually." >&2
  elif command -v sudo >/dev/null 2>&1 && sudo -n true >/dev/null 2>&1; then
    sudo -n "${ROOT_DIR}/deploy/install-updater.sh" || echo "Managed updater install failed; run sudo ./deploy/install-updater.sh manually." >&2
  else
    echo "Run sudo ./deploy/install-updater.sh once to enable one-click updates in the console."
  fi
  "${COMPOSE[@]}" up -d >/dev/null 2>&1 || true
fi


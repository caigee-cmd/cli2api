#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${ROOT_DIR}/deploy/.env"
COMPOSE_FILE="${ROOT_DIR}/deploy/docker-compose.yml"
CONTAINER_NAME="qoder-api-proxy"
SERVICE_NAME="qoder-api-proxy"
IMAGE_REPOSITORY="ghcr.io/caigee-cmd/cli2api"
GITHUB_REPOSITORY="caigee-cmd/cli2api"

require_commands() {
  for command in "$@"; do
    command -v "${command}" >/dev/null 2>&1 || { echo "Missing command: ${command}" >&2; exit 1; }
  done
}

ensure_env_file() {
  if [[ ! -f "${ENV_FILE}" ]]; then
    cp "${ROOT_DIR}/deploy/.env.example" "${ENV_FILE}"
    chmod 0600 "${ENV_FILE}"
    if [[ "${EUID}" -eq 0 && -n "${SUDO_UID:-}" && -n "${SUDO_GID:-}" ]]; then
      chown "${SUDO_UID}:${SUDO_GID}" "${ENV_FILE}"
    fi
  fi
}

read_env_value() {
  local key="$1"
  awk -v key="${key}" 'index($0, key "=") == 1 { print substr($0, length(key) + 2); exit }' "${ENV_FILE}"
}

set_env_value() {
  local key="$1"
  local value="$2"
  local temp_file owner_group=""
  if [[ "${EUID}" -eq 0 ]]; then
    owner_group="$(stat -c '%u:%g' "${ENV_FILE}" 2>/dev/null || true)"
  fi
  temp_file="$(mktemp "${ENV_FILE}.XXXXXX")"
  awk -v key="${key}" -v value="${value}" '
    BEGIN { found = 0 }
    index($0, key "=") == 1 {
      if (!found) print key "=" value
      found = 1
      next
    }
    { print }
    END { if (!found) print key "=" value }
  ' "${ENV_FILE}" > "${temp_file}"
  chmod 0600 "${temp_file}"
  if [[ -n "${owner_group}" ]]; then
    chown "${owner_group}" "${temp_file}"
  fi
  mv "${temp_file}" "${ENV_FILE}"
}

xml_escape() {
  printf '%s' "$1" | sed \
    -e 's/&/\&amp;/g' \
    -e 's/</\&lt;/g' \
    -e 's/>/\&gt;/g' \
    -e 's/"/\&quot;/g'
}


running_release_version() {
  local response version
  response="$(curl --silent --fail --max-time 3 http://127.0.0.1:3010/health 2>/dev/null || true)"
  version="$(printf '%s' "${response}" | sed -nE 's/.*"version"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/p')"
  if [[ "${version}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    printf '%s' "${version}"
    return 0
  fi
  if [[ "${version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    printf 'v%s' "${version}"
    return 0
  fi
  return 1
}

updater_asset_name() {
  local os_name="$1"
  local machine arch
  machine="$(uname -m)"
  case "${machine}" in
    x86_64|amd64) arch="amd64" ;;
    arm64|aarch64) arch="arm64" ;;
    *)
      echo "Unsupported host architecture: ${machine}" >&2
      return 1
      ;;
  esac
  printf 'cli2api-updater_%s_%s' "${os_name}" "${arch}"
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
    return
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
    return
  fi
  echo "Missing command: sha256sum or shasum" >&2
  return 1
}

install_released_updater() {
  local destination="$1"
  local os_name="$2"
  local version asset temp_dir checksum_file expected actual base_url source_label
  version="$(running_release_version)" || return 1
  asset="$(updater_asset_name "${os_name}")" || return 2
  temp_dir="$(mktemp -d)"
  checksum_file="${temp_dir}/cli2api-updater_checksums.txt"

  for source_label in "${version}" latest; do
    if [[ "${source_label}" == "latest" ]]; then
      base_url="https://github.com/${GITHUB_REPOSITORY}/releases/latest/download"
    else
      base_url="https://github.com/${GITHUB_REPOSITORY}/releases/download/${source_label}"
    fi
    if curl --silent --show-error --fail --location \
      "${base_url}/${asset}" --output "${temp_dir}/${asset}"; then
      if ! curl --silent --show-error --fail --location \
        "${base_url}/cli2api-updater_checksums.txt" --output "${checksum_file}"; then
        rm -rf "${temp_dir}"
        echo "Updater checksum file is unavailable from ${source_label}." >&2
        return 2
      fi

      expected="$(awk -v asset="${asset}" '$2 == asset { print $1; exit }' "${checksum_file}")"
      actual="$(sha256_file "${temp_dir}/${asset}")"
      if [[ -z "${expected}" || "${actual}" != "${expected}" ]]; then
        rm -rf "${temp_dir}"
        echo "Updater checksum verification failed for ${asset}." >&2
        return 2
      fi

      mv "${temp_dir}/${asset}" "${destination}"
      chmod 0755 "${destination}"
      rm -rf "${temp_dir}"
      echo "Installed updater asset ${asset} from ${source_label}."
      return 0
    fi
  done

  rm -rf "${temp_dir}"
  return 1
}

install_linux() {
  if [[ "${EUID}" -ne 0 ]]; then
    echo "Run with sudo: sudo ./deploy/install-updater.sh" >&2
    exit 1
  fi
  require_commands docker systemctl install mktemp awk curl stat chown
  ensure_env_file
  set_env_value CLI2API_UPDATER_SOCKET_DIR /run/cli2api-updater
  set_env_value UPDATE_AGENT_URL ""
  set_env_value UPDATE_AGENT_TOKEN ""

  if ! docker container inspect "${CONTAINER_NAME}" >/dev/null 2>&1; then
    echo "Start ${CONTAINER_NAME} before installing the managed updater." >&2
    exit 1
  fi

  local temp_dir status
  temp_dir="$(mktemp -d)"
  if docker cp "${CONTAINER_NAME}:/app/cli2api-updater" "${temp_dir}/cli2api-updater" >/dev/null 2>&1; then
    install -m 0755 "${temp_dir}/cli2api-updater" /usr/local/bin/cli2api-updater
    echo "Installed updater from the running container."
  elif install_released_updater /usr/local/bin/cli2api-updater linux; then
    :
  else
    status=$?
    if [[ "${status}" -ne 1 ]]; then
      rm -rf "${temp_dir}"
      exit "${status}"
    fi
    if ! command -v go >/dev/null 2>&1; then
      rm -rf "${temp_dir}"
      echo "No updater binary is available. Install Go 1.25.6+ to build it locally." >&2
      exit 1
    fi
    echo "Released updater asset unavailable; building from the checked-out source."
    (cd "${ROOT_DIR}" && go build -trimpath -o "${temp_dir}/cli2api-updater" ./cmd/updater)
    install -m 0755 "${temp_dir}/cli2api-updater" /usr/local/bin/cli2api-updater
  fi
  rm -rf "${temp_dir}"

  install -d -m 0750 /run/cli2api-updater
  install -d -m 0700 /var/lib/cli2api-updater
  cat > /etc/systemd/system/cli2api-updater.service <<UNIT
[Unit]
Description=CLI2API managed update daemon
After=docker.service network-online.target
Requires=docker.service

[Service]
Type=simple
User=root
Group=root
UMask=0077
ExecStart=/usr/local/bin/cli2api-updater \\
  --socket /run/cli2api-updater/updater.sock \\
  --status-file /var/lib/cli2api-updater/status.json \\
  --compose-file "${COMPOSE_FILE}" \\
  --env-file "${ENV_FILE}" \\
  --service ${SERVICE_NAME} \\
  --container ${CONTAINER_NAME} \\
  --image-repository ${IMAGE_REPOSITORY} \\
  --health-url http://127.0.0.1:3010/health
Restart=always
RestartSec=3
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
UNIT

  systemctl daemon-reload
  systemctl enable cli2api-updater
  systemctl restart cli2api-updater
  systemctl --no-pager --full status cli2api-updater
}

install_macos() {
  if [[ "${EUID}" -eq 0 ]]; then
    echo "Run without sudo on macOS: ./deploy/install-updater.sh" >&2
    exit 1
  fi
  require_commands docker launchctl openssl curl mktemp awk sed shasum uname
  if ! docker info >/dev/null 2>&1; then
    echo "Start Docker Desktop before installing the managed updater." >&2
    exit 1
  fi
  ensure_env_file

  local token
  token="$(read_env_value UPDATE_AGENT_TOKEN)"
  if [[ -z "${token}" ]]; then
    token="$(openssl rand -hex 32)"
  fi
  set_env_value CLI2API_UPDATER_SOCKET_DIR /tmp/cli2api-updater
  set_env_value UPDATE_AGENT_URL http://host.docker.internal:3011
  set_env_value UPDATE_AGENT_TOKEN "${token}"

  local install_dir support_dir log_dir binary wrapper token_file status_file plist label path_value
  install_dir="${HOME}/.local/bin"
  support_dir="${HOME}/Library/Application Support/cli2api-updater"
  log_dir="${HOME}/Library/Logs/cli2api-updater"
  binary="${install_dir}/cli2api-updater"
  wrapper="${support_dir}/run.sh"
  token_file="${support_dir}/token"
  status_file="${support_dir}/status.json"
  plist="${HOME}/Library/LaunchAgents/com.caigee.cli2api-updater.plist"
  label="com.caigee.cli2api-updater"
  path_value="$(dirname "$(command -v docker)"):/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin"

  mkdir -p "${install_dir}" "${support_dir}" "${log_dir}" "$(dirname "${plist}")"
  if install_released_updater "${binary}" darwin; then
    :
  else
    status=$?
    if [[ "${status}" -ne 1 ]]; then
      exit "${status}"
    fi
    if ! command -v go >/dev/null 2>&1; then
      echo "No updater asset exists for the running version. Install Go 1.25.6+ to build it locally." >&2
      exit 1
    fi
    echo "Released updater asset unavailable; building from the checked-out source."
    (cd "${ROOT_DIR}" && go build -trimpath -o "${binary}" ./cmd/updater)
    chmod 0755 "${binary}"
  fi
  printf '%s\n' "${token}" > "${token_file}"
  chmod 0600 "${token_file}"

  {
    echo '#!/usr/bin/env bash'
    printf 'export HOME=%q\n' "${HOME}"
    printf 'export PATH=%q\n' "${path_value}"
    printf 'exec %q' "${binary}"
    printf ' %q' --listen 127.0.0.1:3011
    printf ' %q' --auth-token-file "${token_file}"
    printf ' %q' --status-file "${status_file}"
    printf ' %q' --compose-file "${COMPOSE_FILE}"
    printf ' %q' --env-file "${ENV_FILE}"
    printf ' %q' --service "${SERVICE_NAME}"
    printf ' %q' --container "${CONTAINER_NAME}"
    printf ' %q' --image-repository "${IMAGE_REPOSITORY}"
    printf ' %q' --health-url http://127.0.0.1:3010/health
    printf '\n'
  } > "${wrapper}"
  chmod 0700 "${wrapper}"

  local wrapper_xml stdout_xml stderr_xml
  wrapper_xml="$(xml_escape "${wrapper}")"
  stdout_xml="$(xml_escape "${log_dir}/stdout.log")"
  stderr_xml="$(xml_escape "${log_dir}/stderr.log")"
  cat > "${plist}" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>${label}</string>
  <key>ProgramArguments</key>
  <array>
    <string>${wrapper_xml}</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>${stdout_xml}</string>
  <key>StandardErrorPath</key>
  <string>${stderr_xml}</string>
</dict>
</plist>
PLIST

  launchctl bootout "gui/${UID}" "${plist}" >/dev/null 2>&1 || true
  launchctl bootstrap "gui/${UID}" "${plist}"
  launchctl kickstart -k "gui/${UID}/${label}"

  local ready=0
  for _ in {1..30}; do
    if curl --silent --fail --header "Authorization: Bearer ${token}" http://127.0.0.1:3011/health >/dev/null; then
      ready=1
      break
    fi
    sleep 1
  done
  if [[ "${ready}" -ne 1 ]]; then
    echo "Updater did not become ready. Check ${log_dir}/stderr.log" >&2
    exit 1
  fi
}

case "$(uname -s)" in
  Linux)
    install_linux
    ;;
  Darwin)
    install_macos
    ;;
  *)
    echo "Use install-updater.sh on Linux/macOS or install-updater.ps1 on Windows." >&2
    exit 1
    ;;
esac

echo "Managed updater installed. Recreate qoder-api-proxy once so it can use the updater channel:"
echo "docker compose --env-file ${ENV_FILE} -f ${COMPOSE_FILE} up -d --force-recreate qoder-api-proxy"

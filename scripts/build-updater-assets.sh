#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUTPUT_DIR="${1:-${ROOT_DIR}/dist/updater}"

mkdir -p "${OUTPUT_DIR}"

for target in \
  linux/amd64 linux/arm64 \
  darwin/amd64 darwin/arm64 \
  windows/amd64 windows/arm64; do
  goos="${target%/*}"
  goarch="${target#*/}"
  suffix=""
  if [[ "${goos}" == "windows" ]]; then
    suffix=".exe"
  fi
  output="${OUTPUT_DIR}/cli2api-updater_${goos}_${goarch}${suffix}"
  CGO_ENABLED=0 GOOS="${goos}" GOARCH="${goarch}" \
    go build -trimpath -ldflags="-s -w" -o "${output}" "${ROOT_DIR}/cmd/updater"
done

checksum_file="${OUTPUT_DIR}/cli2api-updater_checksums.txt"
: > "${checksum_file}"
for asset in "${OUTPUT_DIR}"/cli2api-updater_*; do
  if [[ "${asset}" == "${checksum_file}" ]]; then
    continue
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    hash="$(sha256sum "${asset}" | awk '{print $1}')"
  else
    hash="$(shasum -a 256 "${asset}" | awk '{print $1}')"
  fi
  printf '%s  %s\n' "${hash}" "$(basename "${asset}")" >> "${checksum_file}"
done

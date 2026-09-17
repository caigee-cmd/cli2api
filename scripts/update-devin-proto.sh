#!/usr/bin/env bash
# Download Desktop, extract descriptors and generate Go bindings. Requires Go,
# protoc 34.1 and protoc-gen-go v1.36.10 on PATH:
#   go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.10
#   bash scripts/update-devin-proto.sh                   # latest stable
#   bash scripts/update-devin-proto.sh --version 3.10.31 # pinned release
# Only .pb.go files are written to the checkout; downloads/descriptors are temporary.
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root"
exec go run ./scripts/devin-proto "$@"

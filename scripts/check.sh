#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

export GOTOOLCHAIN="${GOTOOLCHAIN:-auto}"
export GOMODCACHE="${GOMODCACHE:-$ROOT_DIR/.gomodcache}"
mkdir -p "$GOMODCACHE"

go fmt ./...
go vet ./...
go test ./...

test -f docker-compose/docker-compose.yml
test -f proto/meshserver/session/v1/session.proto
test -n "$(find migrations -maxdepth 1 -type f | head -n 1)"

echo "check completed"


#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PROTO_FILE="$ROOT_DIR/proto/meshserver/session/v1/session.proto"
OUT_DIR="$ROOT_DIR/internal/gen/proto"

command -v protoc >/dev/null 2>&1 || {
  echo "protoc is required but not installed" >&2
  exit 1
}

command -v protoc-gen-go >/dev/null 2>&1 || {
  echo "protoc-gen-go is required but not installed" >&2
  exit 1
}

if ! command -v protoc-gen-go-grpc >/dev/null 2>&1; then
  echo "warning: protoc-gen-go-grpc not found; continuing because gRPC stubs are not required for this project" >&2
fi

mkdir -p "$OUT_DIR"

protoc \
  -I "$ROOT_DIR/proto" \
  --go_out="$OUT_DIR" \
  --go_opt=paths=source_relative \
  "$PROTO_FILE"

echo "generated protobuf Go files under $OUT_DIR"


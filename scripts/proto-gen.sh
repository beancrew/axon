#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
PROTO_DIR="${ROOT_DIR}/proto"
OUT_DIR="${ROOT_DIR}/gen/proto"

# ─── Check dependencies ───

check_cmd() {
  if ! command -v "$1" &>/dev/null; then
    echo "ERROR: '$1' not found. Please install it." >&2
    case "$1" in
      protoc)
        echo "  brew install protobuf  OR  apt install -y protobuf-compiler" >&2
        ;;
      protoc-gen-go)
        echo "  go install google.golang.org/protobuf/cmd/protoc-gen-go@latest" >&2
        ;;
      protoc-gen-go-grpc)
        echo "  go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest" >&2
        ;;
    esac
    exit 1
  fi
}

check_cmd protoc
check_cmd protoc-gen-go
check_cmd protoc-gen-go-grpc

# ─── Generate ───

mkdir -p "${OUT_DIR}/control"
mkdir -p "${OUT_DIR}/operations"
mkdir -p "${OUT_DIR}/management"

echo "Generating control.proto..."
protoc \
  --proto_path="${PROTO_DIR}" \
  --go_out="${OUT_DIR}/control" \
  --go_opt=paths=source_relative \
  --go-grpc_out="${OUT_DIR}/control" \
  --go-grpc_opt=paths=source_relative \
  control.proto

echo "Generating operations.proto..."
protoc \
  --proto_path="${PROTO_DIR}" \
  --go_out="${OUT_DIR}/operations" \
  --go_opt=paths=source_relative \
  --go-grpc_out="${OUT_DIR}/operations" \
  --go-grpc_opt=paths=source_relative \
  operations.proto

echo "Generating management.proto..."
protoc \
  --proto_path="${PROTO_DIR}" \
  --go_out="${OUT_DIR}/management" \
  --go_opt=paths=source_relative \
  --go-grpc_out="${OUT_DIR}/management" \
  --go-grpc_opt=paths=source_relative \
  management.proto

echo "Proto generation complete. Output: ${OUT_DIR}"

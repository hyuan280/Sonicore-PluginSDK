#!/usr/bin/env bash
# Regenerates the generated protocol stubs from proto/ into each language
# SDK directory. The toolchain is pinned:
#   - protoc           v29.3   (downloaded into .tools/ when missing)
#   - protoc-gen-go    via `go run` from go/tools.go (protobuf-go v1.36.12)
#   - protoc-gen-go-grpc via `go run` from go/tools.go (v1.6.2)
# Run `make regen-check` in CI to assert committed stubs stay in sync.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PROTOC_VERSION="29.3"
PROTOC_DIR="$ROOT/.tools/protoc-$PROTOC_VERSION"
PROTOC_BIN="${PROTOC_BIN:-$PROTOC_DIR/bin/protoc}"

if [ ! -x "$PROTOC_BIN" ]; then
  case "$(uname -s)-$(uname -m)" in
    Linux-x86_64)  PROTOC_ARCH="linux-x86_64" ;;
    Linux-aarch64) PROTOC_ARCH="linux-aarch_64" ;;
    Darwin-x86_64) PROTOC_ARCH="osx-x86_64" ;;
    Darwin-arm64)  PROTOC_ARCH="osx-aarch_64" ;;
    *)
      echo "unsupported platform $(uname -s)-$(uname -m); install protoc $PROTOC_VERSION manually and set PROTOC_BIN" >&2
      exit 1
      ;;
  esac
  mkdir -p "$PROTOC_DIR"
  ZIP="$PROTOC_DIR/protoc.zip"
  URL="https://github.com/protocolbuffers/protobuf/releases/download/v$PROTOC_VERSION/protoc-$PROTOC_VERSION-$PROTOC_ARCH.zip"
  echo "downloading protoc $PROTOC_VERSION ($PROTOC_ARCH)..."
  curl -fsSL --retry 3 --retry-all-errors -o "$ZIP" "$URL"
  unzip -oq "$ZIP" -d "$PROTOC_DIR"
  rm -f "$ZIP"
fi

# go run wrappers so protoc can invoke the pinned Go plugins.
PLUGINS="$(mktemp -d)"
trap 'rm -rf "$PLUGINS"' EXIT
cat > "$PLUGINS/protoc-gen-go" <<'EOF'
#!/bin/sh
exec go run google.golang.org/protobuf/cmd/protoc-gen-go "$@"
EOF
cat > "$PLUGINS/protoc-gen-go-grpc" <<'EOF'
#!/bin/sh
exec go run google.golang.org/grpc/cmd/protoc-gen-go-grpc "$@"
EOF
chmod +x "$PLUGINS/protoc-gen-go" "$PLUGINS/protoc-gen-go-grpc"

GOGEN="$ROOT/go/gen"
mkdir -p "$GOGEN"
(cd "$ROOT/go" && "$PROTOC_BIN" \
  -I "$ROOT/proto" \
  --plugin=protoc-gen-go="$PLUGINS/protoc-gen-go" \
  --plugin=protoc-gen-go-grpc="$PLUGINS/protoc-gen-go-grpc" \
  --go_out="$GOGEN" \
  --go_opt=paths=source_relative \
  --go-grpc_out="$GOGEN" \
  --go-grpc_opt=paths=source_relative \
  "$ROOT"/proto/*.proto)

echo "generated Go stubs in $GOGEN"

# Normalize generated output with goimports so regen is idempotent and the
# committed stubs pass `make fmt-check`.
if command -v goimports >/dev/null 2>&1; then
  GOIMPORTS="$(command -v goimports)"
else
  GOIMPORTS="$(go env GOPATH)/bin/goimports"
fi
if [ ! -x "$GOIMPORTS" ]; then
  echo "goimports not found: go install golang.org/x/tools/cmd/goimports@latest" >&2
  exit 1
fi
"$GOIMPORTS" -w -local github.com/hyuan280/Sonicore-PluginSDK "$GOGEN"

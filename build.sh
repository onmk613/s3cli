#!/usr/bin/env bash
set -euo pipefail

gofmt -w .
go mod vendor
rm -rf bin/ vendor/

VERSION=${VERSION:-$(git describe --tags --always 2>/dev/null || echo "dev")}
COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS="-s -w \
  -X 's3cli/pkg/cmd.Version=${VERSION}' \
  -X 's3cli/pkg/cmd.Commit=${COMMIT}' \
  -X 's3cli/pkg/cmd.BuildDate=${DATE}'"

ENTRY=./cmd/s3cli

go mod tidy

build_one() {
  local os=$1 arch=$2 out=$3
  echo ">>> building ${os}/${arch} -> ${out}"
  mkdir -p "$(dirname "${out}")"
  env CGO_ENABLED=0 GOOS=${os} GOARCH=${arch} \
    go build -trimpath -ldflags "${LDFLAGS}" -o "${out}" "${ENTRY}"
}

if [ "${1:-}" = "all" ]; then
  echo "=== building all platforms ==="
  build_one linux   amd64   bin/s3cli-linux-amd64
  build_one linux   arm64   bin/s3cli-linux-arm64
  build_one darwin  amd64   bin/s3cli-darwin-amd64
  build_one darwin  arm64   bin/s3cli-darwin-arm64
  build_one windows amd64   bin/s3cli-windows-amd64.exe
  echo "=== done, binaries in bin/ ==="
else
  # 默认：编译当前平台到当前目录
  OS=$(uname -s | tr '[:upper:]' '[:lower:]')
  ARCH=$(uname -m)
  case "${ARCH}" in
    x86_64)  ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
    arm64)   ARCH="arm64" ;;
  esac
  OUT="s3cli"
  [ "${OS}" = "windows" ] || true  # windows 不走这里（uname 在 msys 下可能不同）
  echo "=== building ${OS}/${ARCH} -> ${OUT} ==="
  build_one "${OS}" "${ARCH}" "${OUT}"
  echo "=== done: ${OUT} ==="
fi

rm -rf vendor

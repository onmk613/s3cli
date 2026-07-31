#!/usr/bin/env bash
set -euo pipefail

# 用法:
#   ./build.sh                编译当前平台 (默认后端 s3api)
#   ./build.sh aws            编译当前平台, 官方 SDK 后端 (-tags aws)
#   ./build.sh s3api          编译当前平台, 自建请求后端 (与默认等价)
#   ./build.sh aws all        全平台交叉编译 (后端同样可选, 缺省 s3api)
#
# 后端在编译期通过 build tag 选定, 二进制只包含一种实现:
#   - s3api (默认): 自建 HTTP + SigV4, 零 SDK 依赖
#   - aws:          官方 aws-sdk-go-v2

gofmt -w .
rm -rf bin/

BACKEND="s3api"
ALL=""
for arg in "$@"; do
  case "${arg}" in
    aws)    BACKEND="aws" ;;
    s3api)  BACKEND="s3api" ;;
    all)    ALL="1" ;;
    *)
      echo "unknown argument: ${arg} (expected: s3api | aws | all)" >&2
      exit 1
      ;;
  esac
done

TAGS=""
if [ "${BACKEND}" = "aws" ]; then
  TAGS="-tags aws"
fi

VERSION=${VERSION:-$(git describe --tags --always 2>/dev/null || echo "dev")}
COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
GOVERSION=$(go version | awk '{print $3}')

# 版本变量位于 internal/cmd/root.go（曾为 pkg/cmd，包迁移后此路径失效，
# 链接器对不存在的符号静默忽略，导致版本信息从未真正注入）。
LDFLAGS="-s -w \
  -X 's3cli/internal/cmd.Version=${VERSION}' \
  -X 's3cli/internal/cmd.Commit=${COMMIT}' \
  -X 's3cli/internal/cmd.BuildDate=${DATE}' \
  -X 's3cli/internal/cmd.GoVersion=${GOVERSION}'"

ENTRY=.

go mod tidy

build_one() {
  local os=$1 arch=$2 out=$3
  echo ">>> building ${os}/${arch} [backend=${BACKEND}] -> ${out}"
  mkdir -p "$(dirname "${out}")"
  env CGO_ENABLED=0 GOOS=${os} GOARCH=${arch} \
    go build -trimpath ${TAGS} -ldflags "${LDFLAGS}" -o "${out}" "${ENTRY}"
}

if [ -n "${ALL}" ]; then
  echo "=== building all platforms [backend=${BACKEND}] ==="
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
  echo "=== building ${OS}/${ARCH} [backend=${BACKEND}] -> ${OUT} ==="
  build_one "${OS}" "${ARCH}" "${OUT}"
  echo "=== done: ${OUT} ==="
fi

#!/usr/bin/env bash
# e2e-s3.sh 对真实 S3 兼容服务端跑一组 CLI 黑盒用例。
#
# 与 main_test.go 的内存假 S3 不同, 这里验证的是 SigV4、真实 HTTP、分页、
# 分段上传、服务端 CopyObject 等与外部实现的互操作性。CI 用 LocalStack;
# 本地/其它环境只要提供 endpoint 即可:
#
#   S3CLI_E2E_ENDPOINT=http://127.0.0.1:4566 \
#   S3CLI_E2E_ACCESS_KEY=test S3CLI_E2E_SECRET_KEY=test \
#   ./scripts/e2e-s3.sh
#
# 可选:
#   S3CLI_BIN=./s3cli                  待测二进制 (默认 ./s3cli)
#   S3CLI_E2E_SKIP_MULTIPART=1         跳过 65MiB 分段上传用例

set -euo pipefail

BIN="${S3CLI_BIN:-./s3cli}"
ENDPOINT="${S3CLI_E2E_ENDPOINT:-}"
AK="${S3CLI_E2E_ACCESS_KEY:-test}"
SK="${S3CLI_E2E_SECRET_KEY:-test}"
REGION="${S3CLI_E2E_REGION:-us-east-1}"

if [ -z "$ENDPOINT" ]; then
  echo "S3CLI_E2E_ENDPOINT is required (e.g. http://127.0.0.1:4566)" >&2
  exit 77
fi
if [ ! -x "$BIN" ]; then
  echo "s3cli binary not found or not executable: $BIN" >&2
  exit 1
fi

TMP="$(mktemp -d)"
CONF="$TMP/s3cli.conf"
BUCKET="e2e-$(date +%s)-$$"
DST_BUCKET="$BUCKET-dst"

fail() {
  echo "E2E FAIL: $*" >&2
  exit 1
}

cleanup() {
  "$BIN" -f "$CONF" --lang en --no-color rm -r --force "e2e:$BUCKET/" >/dev/null 2>&1 || true
  "$BIN" -f "$CONF" --lang en --no-color rm -r --force "e2e:$DST_BUCKET/" >/dev/null 2>&1 || true
  "$BIN" -f "$CONF" --lang en --no-color bucket remove --force --yes "e2e:$BUCKET" >/dev/null 2>&1 || true
  "$BIN" -f "$CONF" --lang en --no-color bucket remove --force --yes "e2e:$DST_BUCKET" >/dev/null 2>&1 || true
  rm -rf "$TMP"
}
trap cleanup EXIT

run() {
  "$BIN" -f "$CONF" --lang en --no-color "$@"
}

# 等待服务端可连接 (不要求 2xx; 403/404 也说明 HTTP 已就绪)。
ready=0
for _ in $(seq 1 60); do
  if curl -s --connect-timeout 2 --max-time 5 -o /dev/null "$ENDPOINT" 2>/dev/null; then
    ready=1
    break
  fi
  sleep 1
done
[ "$ready" = 1 ] || fail "endpoint did not become reachable: $ENDPOINT"

cat >"$CONF" <<CONF
[e2e]
host_base = "$ENDPOINT"
access_key = "$AK"
secret_key = "$SK"
region = "$REGION"
bucket_lookup = "path"
CONF
chmod 600 "$CONF"

echo "==> alias list / bucket create"
run alias list >/dev/null
run bucket create "e2e:$BUCKET"
run bucket create "e2e:$DST_BUCKET"

echo "==> put / get / ls / stat / du / tree"
printf 'hello-s3cli-e2e\n' >"$TMP/small.txt"
run put "$TMP/small.txt" "e2e:$BUCKET/small.txt" >/dev/null
run get "e2e:$BUCKET/small.txt" "$TMP/small.got" >/dev/null
cmp -s "$TMP/small.txt" "$TMP/small.got" || fail "get content mismatch"

run ls --json "e2e:$BUCKET" | grep -q "\"path\":\"e2e:$BUCKET/small.txt\"" || fail "ls --json missing object"
run stat --json "e2e:$BUCKET/small.txt" | grep -q '"name":"small.txt"' || fail "stat --json missing object"
run du --json "e2e:$BUCKET" | grep -q '"fileNum":1' || fail "du --json file count"
run tree --json "e2e:$BUCKET" | grep -q '"files":1' || fail "tree --json file count"

echo "==> checksum"
if ! run put --checksum SHA256 "$TMP/small.txt" "e2e:$BUCKET/checksum.txt" >/dev/null 2>"$TMP/put-checksum.err"; then
  if grep -qi "checksum" "$TMP/put-checksum.err"; then
    echo "SKIP checksum: endpoint does not support additional checksums"
  else
    cat "$TMP/put-checksum.err" >&2
    fail "put --checksum failed"
  fi
else
  rm -f "$TMP/checksum.got"
  if ! run get --checksum SHA256 "e2e:$BUCKET/checksum.txt" "$TMP/checksum.got" >/dev/null 2>"$TMP/checksum.err"; then
    # 少数兼容实现不实现 x-amz-checksum-mode; 这属于服务端能力缺口,
    # 不应让整个 e2e 失败 (put 侧已证明标准请求头被接受)。
    if grep -q "x-amz-checksum-mode\|returned no" "$TMP/checksum.err"; then
      echo "SKIP checksum verification: endpoint does not expose checksums"
    else
      cat "$TMP/checksum.err" >&2
      fail "get --checksum failed"
    fi
  else
    cmp -s "$TMP/small.txt" "$TMP/checksum.got" || fail "checksum get content mismatch"
  fi
fi

echo "==> tag"
run tag set "e2e:$BUCKET" --tag env=e2e >/dev/null
run tag list --json "e2e:$BUCKET" | grep -q '"key":"env"' || fail "tag list missing tag"

echo "==> cp / mv / rm"
run cp "e2e:$BUCKET/small.txt" "e2e:$BUCKET/copied.txt" >/dev/null
run stat "e2e:$BUCKET/copied.txt" >/dev/null || fail "cp did not create target"
run mv "e2e:$BUCKET/copied.txt" "e2e:$BUCKET/moved.txt" >/dev/null
if run stat "e2e:$BUCKET/copied.txt" >/dev/null 2>&1; then fail "mv left source behind"; fi
run stat "e2e:$BUCKET/moved.txt" >/dev/null || fail "mv did not create target"
run rm "e2e:$BUCKET/moved.txt" >/dev/null
if run stat "e2e:$BUCKET/moved.txt" >/dev/null 2>&1; then fail "rm did not delete object"; fi

if [ "${S3CLI_E2E_SKIP_MULTIPART:-0}" != "1" ]; then
  echo "==> multipart upload (65MiB)"
  dd if=/dev/zero of="$TMP/big.bin" bs=1M count=65 2>/dev/null
  run put --part-size 5 "$TMP/big.bin" "e2e:$BUCKET/big.bin" >/dev/null
  rm -f "$TMP/big.got"
  run get "e2e:$BUCKET/big.bin" "$TMP/big.got" >/dev/null
  cmp -s "$TMP/big.bin" "$TMP/big.got" || fail "multipart get content mismatch"
fi

echo "==> mirror (same endpoint, server-side copy) + --remove"
run mirror -q "e2e:$BUCKET" "e2e:$DST_BUCKET"
run stat "e2e:$DST_BUCKET/small.txt" >/dev/null || fail "mirror did not copy object"
run put "$TMP/small.txt" "e2e:$DST_BUCKET/extra.txt" >/dev/null
run mirror -q "e2e:$BUCKET" "e2e:$DST_BUCKET" --remove --force
if run stat "e2e:$DST_BUCKET/extra.txt" >/dev/null 2>&1; then fail "mirror --remove left extra object"; fi

echo "==> recursive rm / bucket remove"
run rm -r --force "e2e:$BUCKET/" >/dev/null
run bucket remove --force --yes "e2e:$BUCKET"
run rm -r --force "e2e:$DST_BUCKET/" >/dev/null
run bucket remove --force --yes "e2e:$DST_BUCKET"

echo "E2E PASS"

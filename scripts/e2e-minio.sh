#!/usr/bin/env bash
# e2e-minio.sh —— 启动本地 MinIO, 对 s3cli 全部子命令及其参数做端到端测试,
# 测试结束后彻底清理 (进程/数据目录/临时配置/状态文件), 零残留。
#
# 用法:
#   scripts/e2e-minio.sh          # 自动下载 MinIO、构建 s3cli、跑全量矩阵
#   scripts/e2e-minio.sh --keep   # 失败排查用: 保留临时目录与进程
#
# 环境变量:
#   S3CLI_BIN   指定 s3cli 二进制 (默认 <仓库根>/s3cli, 缺失时自动构建)
#   MINIO_BIN   指定 minio 二进制 (默认自动下载到临时目录)
#
# 隔离设计: 所有 s3cli 调用统一注入 HOME=<临时目录> 与 -f <临时配置>,
# 不触碰用户真实的 ~/.s3cli 与 ~/.s3cli-mpu。
set -u

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
KEEP=0
[ "${1:-}" = "--keep" ] && KEEP=1

S3CLI_BIN="${S3CLI_BIN:-$ROOT/s3cli}"
MINIO_BIN="${MINIO_BIN:-}"
MINIO_USER=minioadmin
MINIO_PASS=minioadmin

WORK="$(mktemp -d "$ROOT/.e2e-tmp.XXXXXX")"
HOME_DIR="$WORK/home"
CONF="$HOME_DIR/.s3cli"
MPU_DIR="$HOME_DIR/.s3cli-mpu"
D1="$WORK/data1"
D2="$WORK/data2"
FX="$WORK/fx"
MINIO_PID1=""
MINIO_PID2=""
EVENT_PID=""
PORT1=""
PORT2=""
EVENT_PORT=""
ADDR1=""
ADDR2=""

pass=0
fail=0
failures=""

# ---------- 基础工具 ----------

die() { printf 'FATAL: %s\n' "$*" >&2; cleanup; exit 1; }

t()  { printf '%s\n' "$1"; }
ok() { pass=$((pass + 1)); printf '  ok  %s\n' "$1"; }
bad() {
  fail=$((fail + 1))
  failures="$failures
  FAIL $1"
  printf '  FAIL %s\n' "$1"
}

# run: 执行命令, 结果写入 OUT / ERR / CODE。
# OUT/ERR 会剥离 ANSI 色码便于断言; RAWOUT/RAWERR 保留原始输出 (供无颜色断言)。
strip_ansi() { sed $'s/\x1b\[[0-9;]*m//g'; }

run() {
  RAWOUT="$("$@" 2>"$WORK/.stderr")"
  CODE=$?
  RAWERR="$(cat "$WORK/.stderr" 2>/dev/null || true)"
  OUT="$(printf '%s' "$RAWOUT" | strip_ansi)"
  ERR="$(printf '%s' "$RAWERR" | strip_ansi)"
}

# run_stdin: 从变量喂 stdin 后执行
run_stdin() {
  local input="$1"
  shift
  RAWOUT="$(printf '%s' "$input" | "$@" 2>"$WORK/.stderr")"
  CODE=$?
  RAWERR="$(cat "$WORK/.stderr" 2>/dev/null || true)"
  OUT="$(printf '%s' "$RAWOUT" | strip_ansi)"
  ERR="$(printf '%s' "$RAWERR" | strip_ansi)"
}

# s3: 统一注入 HOME 隔离、测试配置与英文语言, 所有测试命令经此调用。
# 断言以英文输出为准 (确定性); 中文输出的正确性另在 test_global_flags 中验证。
s3() { env HOME="$HOME_DIR" CLI_LANG=en "$S3CLI_BIN" -f "$CONF" "$@"; }

contains() { printf '%s' "$1" | grep -qF -- "$2"; }

# json_ok <名称> <python表达式(d=已加载json)> <命令...>
# 表达式为多行时用 $'\n' 拼接传入; 断言失败即 FAIL
json_ok() {
  local name="$1" py="$2"
  shift 2
  run "$@"
  if [ "$CODE" -ne 0 ]; then
    bad "$name (exit=$CODE: $(printf '%s' "$ERR" | head -c 150))"
    return
  fi
  if printf '%s' "$OUT" | python3 -c "import json,sys
d=json.load(sys.stdin)
$py" >/dev/null 2>&1; then
    ok "$name"
  else
    bad "$name (json 断言失败: $(printf '%s' "$OUT" | head -c 150))"
  fi
}

# jsonl_ok <名称> <python表达式(rows=每行json对象列表)> <命令...>
jsonl_ok() {
  local name="$1" py="$2"
  shift 2
  run "$@"
  if [ "$CODE" -ne 0 ]; then
    bad "$name (exit=$CODE: $(printf '%s' "$ERR" | head -c 150))"
    return
  fi
  if printf '%s' "$OUT" | python3 -c "import json,sys
rows=[json.loads(l) for l in sys.stdin if l.strip()]
$py" >/dev/null 2>&1; then
    ok "$name"
  else
    bad "$name (jsonl 断言失败: $(printf '%s' "$OUT" | head -c 150))"
  fi
}

expect_ok() {
  local name="$1"
  shift
  run "$@"
  if [ "$CODE" -eq 0 ]; then ok "$name"; else bad "$name (exit=$CODE: $(printf '%s' "$ERR" | head -c 150))"; fi
}

expect_code() {
  local name="$1" want="$2"
  shift 2
  run "$@"
  if [ "$CODE" -eq "$want" ]; then ok "$name"; else bad "$name (exit=$CODE, 期望 $want: $(printf '%s' "$ERR" | head -c 150))"; fi
}

expect_out() {
  local name="$1" needle="$2"
  shift 2
  run "$@"
  if [ "$CODE" -eq 0 ] && contains "$OUT" "$needle"; then ok "$name"; else bad "$name (输出缺少 '$needle': $(printf '%s' "$OUT" | head -c 150))"; fi
}

expect_out_code() {
  local name="$1" want="$2" needle="$3"
  shift 3
  run "$@"
  if [ "$CODE" -eq "$want" ] && contains "$OUT$ERR" "$needle"; then ok "$name"; else bad "$name (exit=$CODE 期望 $want / 缺少 '$needle': $(printf '%s' "$OUT$ERR" | head -c 150))"; fi
}

expect_not_out() {
  local name="$1" needle="$2"
  shift 2
  run "$@"
  if [ "$CODE" -eq 0 ] && ! contains "$OUT" "$needle"; then ok "$name"; else bad "$name (输出不应含 '$needle')"; fi
}

expect_err_out() {
  local name="$1" needle="$2"
  shift 2
  run "$@"
  if [ "$CODE" -ne 0 ] && contains "$OUT$ERR" "$needle"; then ok "$name"; else bad "$name (exit=$CODE, 错误信息缺少 '$needle': $(printf '%s' "$OUT$ERR" | head -c 150))"; fi
}

# ---------- 环境准备 ----------

free_port() { python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1]);s.close()'; }

# 优先复用本机已有的 MinIO 二进制 (避免重复下载); 找不到时从官方源下载。
download_minio() {
  local plat
  case "$(uname -s)-$(uname -m)" in
    Darwin-arm64)  plat=darwin-arm64 ;;
    Darwin-x86_64) plat=darwin-amd64 ;;
    Linux-x86_64)  plat=linux-amd64 ;;
    Linux-aarch64) plat=linux-arm64 ;;
    *) die "不支持的平台 $(uname -sm)" ;;
  esac

  # 本地候选: 项目调试目录与 ~/Downloads 下已下载的 minio 二进制
  local candidate
  for candidate in "$ROOT/.e2e-debug/bin/minio" "$HOME"/Downloads/minio*; do
    if [ -x "$candidate" ]; then
      # macOS: 浏览器下载的二进制带 com.apple.quarantine, 后台执行会被 SIGKILL, 先行剥离
      xattr -d com.apple.quarantine "$candidate" 2>/dev/null || true
      MINIO_BIN="$candidate"
      printf '  复用本机 MinIO: %s\n' "$MINIO_BIN"
      return 0
    fi
  done

  MINIO_BIN="$WORK/minio"
  # 固定版本 + sha256 校验: 滚动拉 latest 会让 CI 随上游 release 变化而变红
  # (不可复现), 且无完整性校验。sha256sum 与二进制同源分发, 主要防止传输损坏,
  # 版本可复现性由 pin 保证; 需要换版本时覆盖 MINIO_VERSION 环境变量。
  local ver="${MINIO_VERSION:-RELEASE.2025-09-07T16-13-09Z}"
  local base="https://dl.min.io/server/minio/release/$plat/archive"
  printf '  下载 MinIO %s (%s) ...\n' "$ver" "$plat"
  curl -fsSL --retry 3 -o "$MINIO_BIN" "$base/minio.$ver" || die "MinIO 下载失败"
  local expect_sha got_sha
  expect_sha=$(curl -fsSL --retry 3 "$base/minio.$ver.sha256sum" | awk '{print $1}') || die "MinIO sha256sum 获取失败"
  if command -v sha256sum >/dev/null 2>&1; then
    got_sha=$(sha256sum "$MINIO_BIN" | awk '{print $1}')
  else
    got_sha=$(shasum -a 256 "$MINIO_BIN" | awk '{print $1}')
  fi
  if [ "$got_sha" != "$expect_sha" ]; then
    die "MinIO sha256 校验失败 (got $got_sha, want $expect_sha)"
  fi
  chmod +x "$MINIO_BIN"
}

start_minio() {
  local bin="$1" data="$2" addr="$3" console="$4" notify_env="$5" pid_var="$6"
  env MINIO_ROOT_USER="$MINIO_USER" MINIO_ROOT_PASSWORD="$MINIO_PASS" \
      MINIO_KMS_SECRET_KEY="${MINIO_KMS_SECRET_KEY:-}" \
      $notify_env \
      "$bin" server "$data" --address "$addr" --console-address "$console" \
      >"$WORK/.minio-$(basename "$data").log" 2>&1 &
  eval "$pid_var=\$!"
}

wait_healthy() {
  local addr="$1" i
  for i in $(seq 1 100); do
    curl -fsS "$addr/minio/health/live" >/dev/null 2>&1 && return 0
    sleep 0.2
  done
  return 1
}

setup() {
  mkdir -p "$HOME_DIR" "$D1" "$D2" "$FX"
  [ -x "$MINIO_BIN" ] || download_minio

  # 构建 s3cli (不存在或不可执行时)
  if [ ! -x "$S3CLI_BIN" ]; then
    printf '  构建 s3cli ...\n'
    ( cd "$ROOT" && go build -o "$S3CLI_BIN" . ) || die "s3cli 构建失败"
  fi

  # 随机 KMS 密钥 (供 SSE-KMS 用例; MinIO 单密钥模式)
  export MINIO_KMS_SECRET_KEY="e2e-key:$(python3 -c 'import base64,os;print(base64.b64encode(os.urandom(32)).decode())')"

  PORT1="$(free_port)"; PORT2="$(free_port)"; EVENT_PORT="$(free_port)"
  ADDR1="http://127.0.0.1:$PORT1"
  ADDR2="http://127.0.0.1:$PORT2"

  # 事件通知 webhook 监听器 (记录投递到文件)
  cat > "$WORK/eventd.py" <<'PYEOF'
import http.server, sys, threading
lock = threading.Lock()
class H(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        n = int(self.headers.get('Content-Length', 0) or 0)
        body = self.rfile.read(n).decode('utf-8', 'replace')
        with lock:
            with open(sys.argv[2], 'a') as f:
                f.write(body + '\n')
        self.send_response(200)
        self.end_headers()
    def log_message(self, *a):
        pass
http.server.HTTPServer(('127.0.0.1', int(sys.argv[1])), H).serve_forever()
PYEOF
  python3 "$WORK/eventd.py" "$EVENT_PORT" "$WORK/events.log" >/dev/null 2>&1 &
  EVENT_PID=$!

  # 实例1: 事件通知 webhook 目标; 实例2: 跨端点 mirror 目标
  start_minio "$MINIO_BIN" "$D1" "127.0.0.1:$PORT1" "127.0.0.1:$(free_port)" \
    "MINIO_NOTIFY_WEBHOOK_ENDPOINT_events=http://127.0.0.1:$EVENT_PORT/events MINIO_NOTIFY_WEBHOOK_ENABLE_events=on" MINIO_PID1
  start_minio "$MINIO_BIN" "$D2" "127.0.0.1:$PORT2" "127.0.0.1:$(free_port)" "" MINIO_PID2

  if ! wait_healthy "$ADDR1"; then
    printf '%s\n' '--- minio-data1 日志 ---'; tail -30 "$WORK/.minio-data1.log" 2>/dev/null || true
    die "MinIO 实例1 启动失败 (日志: $WORK/.minio-data1.log)"
  fi
  if ! wait_healthy "$ADDR2"; then
    printf '%s\n' '--- minio-data2 日志 ---'; tail -30 "$WORK/.minio-data2.log" 2>/dev/null || true
    die "MinIO 实例2 启动失败 (日志: $WORK/.minio-data2.log)"
  fi

  s3 alias add minio "$ADDR1" "$MINIO_USER" "$MINIO_PASS" || die "alias add 失败"
  s3 alias add minio2 "$ADDR2" "$MINIO_USER" "$MINIO_PASS" || die "alias add minio2 失败"
  s3 alias add minio1 "$ADDR1" "$MINIO_USER" "$MINIO_PASS" || die "alias add minio1 失败"

  # fixture
  printf 'hello world\n' > "$FX/hello.txt"
  printf 'line one\nline two\nline three\n' > "$FX/lines.txt"
  mkdir -p "$FX/tree/sub/deep"
  printf 'a\n' > "$FX/tree/a.txt"
  printf 'b\n' > "$FX/tree/sub/b.txt"
  printf 'c\n' > "$FX/tree/sub/deep/c.txt"
  printf 'name,age\nAlice,30\nBob,25\nCarol,35\n' > "$FX/data.csv"
  printf '{"id":1,"name":"Alice"}\n{"id":2,"name":"Bob"}\n{"id":3,"name":"Carol"}\n' > "$FX/data.jsonl"
  printf 'name,age\nEve,40\n' | gzip > "$FX/data.csv.gz" 2>/dev/null || {
    # 无 gzip 时用 python 生成
    python3 -c 'import gzip,sys;gzip.open(sys.argv[1],"wb").write(b"name,age\nEve,40\n")' "$FX/data.csv.gz"
  }
  dd if=/dev/zero of="$FX/big.bin" bs=1M count=256 2>/dev/null
  printf 'cors-setup' > "$FX/.done"
}

cleanup() {
  # 优雅终止 → 短暂等待 → 强杀兜底; 等待子进程退出避免其继续写入数据目录
  [ -n "$MINIO_PID1" ] && kill "$MINIO_PID1" 2>/dev/null
  [ -n "$MINIO_PID2" ] && kill "$MINIO_PID2" 2>/dev/null
  [ -n "$EVENT_PID" ] && kill "$EVENT_PID" 2>/dev/null
  [ -n "${PUT_PID:-}" ] && kill "$PUT_PID" 2>/dev/null
  for i in $(seq 1 25); do
    kill -0 "${MINIO_PID1:-0}" 2>/dev/null || break
    sleep 0.2
  done
  [ -n "$MINIO_PID1" ] && kill -9 "$MINIO_PID1" 2>/dev/null
  [ -n "$MINIO_PID2" ] && kill -9 "$MINIO_PID2" 2>/dev/null
  [ -n "$EVENT_PID" ] && kill -9 "$EVENT_PID" 2>/dev/null
  wait 2>/dev/null
  if [ "$KEEP" -eq 0 ]; then
    rm -rf "$WORK"
    if [ -e "$WORK" ]; then
      printf '警告: 清理失败, 残留 %s\n' "$WORK" >&2
    else
      printf '清理完成: 临时目录/进程/数据均已移除\n'
    fi
  else
    printf '保留现场 (--keep): %s\n' "$WORK"
  fi
}

# ---------- 测试分组 ----------

test_global_flags() {
  t '=== 全局参数与环境 ==='

  run env HOME="$HOME_DIR" "$S3CLI_BIN" --help
  [ "$CODE" -eq 0 ] && contains "$OUT" "s3cli" && ok "--help 输出" || bad "--help 输出"

  run env HOME="$HOME_DIR" "$S3CLI_BIN" --lang zh --help
  [ "$CODE" -eq 0 ] && contains "$OUT" "轻量级 S3 命令行客户端" && ok "--lang zh 中文帮助" || bad "--lang zh"
  expect_out "--lang zh 中文子命令帮助" "管理存储桶生命周期" s3 --lang zh bucket lifecycle --help
  expect_out "--lang zh 中文子命令帮助 (mpu)" "管理进行中" s3 --lang zh mpu --help

  run env HOME="$HOME_DIR" CLI_LANG=zh "$S3CLI_BIN" --help
  [ "$CODE" -eq 0 ] && contains "$OUT" "轻量级" && ok "CLI_LANG=zh 环境变量" || bad "CLI_LANG=zh"

  run env HOME="$HOME_DIR" "$S3CLI_BIN" --version
  [ "$CODE" -eq 0 ] && contains "$OUT" "golang" && ok "--version" || bad "--version"

  # 非 TTY 输出天然无颜色; 显式 --no-color 与 CLI_NO_COLOR=1 同样无 ANSI
  run s3 --no-color ls minio
  contains "$RAWOUT" "$(printf '\033')" && bad "--no-color 输出含 ANSI" || ok "--no-color 无 ANSI"
  run env CLI_NO_COLOR=1 HOME="$HOME_DIR" "$S3CLI_BIN" -f "$CONF" ls minio
  if [ "$CODE" -eq 0 ] && ! contains "$RAWOUT" "$(printf '\033')"; then ok "CLI_NO_COLOR=1 无 ANSI"; else bad "CLI_NO_COLOR=1"; fi

  expect_out "--debug 打印请求摘要" "GET" s3 --debug ls minio
  expect_out "-H 自定义头生效 (debug 可见)" "X-E2e-Test" s3 --debug -H "X-E2e-Test: 1" ls minio
  expect_out "--user-agent 覆盖生效 (debug 可见)" "s3cli-e2e-ua" s3 --debug --user-agent "s3cli-e2e-ua" ls minio

  expect_ok "--host-base 指向实际端点" s3 --host-base "$ADDR1" ls minio
  expect_err_out "未知别名报错" "not found" s3 ls ghost-alias:bucket
  expect_err_out "无参数 ls 报用法错误" "requires at least 1 arg" s3 ls
}

test_alias() {
  t '=== alias 管理 ==='

  expect_ok "alias list" s3 alias list
  expect_out "alias list 默认脱敏密钥" "minio" s3 alias list
  s3 alias add sec "$ADDR1" ak-sec "e2e-super-secret" >/dev/null 2>&1
  run s3 alias list
  { [ "$CODE" -eq 0 ] && contains "$OUT" "sec" && ! contains "$OUT" "e2e-super-secret"; } \
    && ok "alias list 脱敏 (无明文 secret)" || bad "alias list 脱敏失败"
  expect_out "alias list --show-secret 显示明文" "e2e-super-secret" s3 alias list --show-secret

  expect_ok "alias add 5 参 (带 session token)" s3 alias add tok "$ADDR1" ak sk mytoken
  expect_ok "alias add 别名 a/set 可用" s3 alias set tok2 "$ADDR1" ak2 sk2
  expect_out "alias list 包含新别名" "tok2" s3 alias list

  # 交互式 edit: 8 个字段 (host/ak/sk/token/region/bucket_lookup/no_verify/chunk)
  run_stdin "$(printf '\n\n\n\n\npath\nfalse\n15\n')" s3 alias edit tok2
  [ "$CODE" -eq 0 ] && ok "alias edit 交互式 (管道输入)" || bad "alias edit (exit=$CODE: $ERR)"
  expect_out "edit 后别名仍存在" "tok2" s3 alias list

  # '-' 清空 session token
  run_stdin "$(printf '\n\n\n-\n\npath\nfalse\n15\n')" s3 alias edit tok
  [ "$CODE" -eq 0 ] && ok "alias edit 清空 token" || bad "alias edit 清空 token (exit=$CODE: $ERR)"
  grep -q "session_token" "$CONF" && bad "token 清除后配置仍含 session_token" || ok "token 已从配置移除"

  expect_ok "alias del" s3 alias del tok2
  run s3 alias list
  contains "$OUT" "tok2" && bad "del 后不应再列出 tok2" || ok "del 后列表无 tok2"
}

test_bucket() {
  t '=== bucket 桶管理 ==='

  printf '{"CORSRules":[{"ID":"cors0","AllowedOrigins":["*"],"AllowedMethods":["GET"]}]}\n' > "$WORK/cors.json"
  printf '{"Rules":[{"ID":"lc0","Status":"Enabled","Filter":{"Prefix":"x/"},"Expiration":{"Days":1}}]}\n' > "$WORK/life.json"
  sed "s/BUCKET/$B4/" > "$WORK/pol.json" <<'POLOF'
{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":["*"]},"Action":["s3:GetObject"],"Resource":["arn:aws:s3:::BUCKET/*"]}]}
POLOF

  expect_ok "bucket create" s3 bucket create "minio:$B1"
  expect_err_out "重复 create 报错 (无 -p)" "Already" s3 bucket create "minio:$B1"
  expect_ok "create -p 忽略已存在" s3 bucket create -p "minio:$B1"
  expect_ok "create --region" s3 bucket create "minio:$B_R" --region us-east-1
  expect_ok "create --versioning" s3 bucket create "minio:$B2" --versioning
  expect_ok "create --with-lock" s3 bucket create -l "minio:$B3"
  expect_ok "create --set-lifecycle/--set-policy 一步配置" \
    s3 bucket create "minio:$B4" --set-lifecycle "$WORK/life.json" --set-policy "$WORK/pol.json"
  # MinIO 的 PutBucketCors 为 dummy handler, 恒返回 NotImplemented; 属服务端限制, 负向断言优雅报错
  expect_err_out "create --set-cors 在 MinIO 优雅报错 (NotImplemented)" "NotImplemented" \
    s3 bucket create "minio:e2e-cneg" --set-cors "$WORK/cors.json"
  expect_ok "create e2e-event" s3 bucket create "minio:$B5"
  expect_ok "create e2e-enc" s3 bucket create "minio:$B6"
  expect_ok "create e2e-life" s3 bucket create "minio:$B7"
  expect_out "ls 列出全部桶" "$B1" s3 ls minio

  expect_ok "remove 测试专用桶 (含对象)" s3 bucket create "minio:e2e-full"
  s3 put "$FX/hello.txt" "minio:e2e-full/obj.txt" >/dev/null 2>&1
  expect_err_out "remove 非空桶无 --force 报错" "not empty" s3 bucket remove "minio:e2e-full"
  expect_ok "remove --force 删除非空桶" s3 bucket remove --force "minio:e2e-full"
  expect_ok "remove 空桶" s3 bucket remove "minio:$B_R"
  run s3 ls minio
  contains "$OUT" "$B_R" && bad "remove 后 ls 仍包含 $B_R" || ok "remove 后 ls 不再包含"

  # CORS: MinIO 的 CORS API 是 dummy handler (恒 501 NotImplemented),
  # 此处验证客户端对服务端限制的优雅处理 (解析并展示错误, 不崩溃不误报成功)。
  t '  bucket cors'
  expect_err_out "cors set (flags) 报 NotImplemented (服务端限制)" "NotImplemented" \
    s3 bucket cors set "minio:$B1" --origin "https://example.com" --method GET --max-age 600 --id cors1
  printf '{"CORSRules":[{"ID":"cors2","AllowedOrigins":["https://file.example.com"],"AllowedMethods":["GET"]}]}\n' > "$WORK/cors2.json"
  expect_err_out "cors set --from-file 同样优雅报错" "NotImplemented" s3 bucket cors set "minio:$B1" --from-file "$WORK/cors2.json"
  expect_err_out "cors get 报 NoSuchCORSConfiguration" "NoSuchCORSConfiguration" s3 bucket cors get "minio:$B1"
  run s3 bucket cors remove "minio:$B1"
  { [ "$CODE" -eq 0 ] || contains "$ERR$OUT" "NotImplemented"; } \
    && ok "cors remove 幂等 (成功或 NotImplemented)" || bad "cors remove 异常 (exit=$CODE)"

  # encryption
  t '  bucket encryption'
  expect_ok "encryption set --algorithm AES256 (SSE-S3)" s3 bucket encryption set "minio:$B6" --algorithm AES256
  expect_out "encryption get 含 AES256" "AES256" s3 bucket encryption get "minio:$B6"
  expect_ok "encryption set aws:kms (MinIO KES-less 单密钥)" s3 bucket encryption set "minio:$B6" --algorithm aws:kms --kms-key-id e2e-key
  expect_out "encryption get 含 aws:kms" "aws:kms" s3 bucket encryption get "minio:$B6"
  printf '{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"AES256"}}]}\n' > "$WORK/enc.json"
  expect_ok "encryption set --from-file" s3 bucket encryption set "minio:$B6" --from-file "$WORK/enc.json"
  expect_ok "encryption del" s3 bucket encryption del "minio:$B6"
  run s3 bucket encryption get "minio:$B6"
  { [ "$CODE" -ne 0 ] || ! contains "$OUT" "AES256"; } && ok "encryption del 后为空" || bad "encryption del 后仍残留"

  # event (webhook 端到端投递)
  t '  bucket event'
  cat > "$WORK/events.json" <<EOF
{
  "QueueConfigurations": [
    {
      "Id": "e2e-events",
      "QueueArn": "arn:minio:sqs::events:webhook",
      "Events": ["s3:ObjectCreated:*"]
    }
  ]
}
EOF
  expect_ok "event set (AWS CLI 兼容 JSON)" s3 bucket event set "$WORK/events.json" "minio:$B5"
  expect_out "event get 含规则" "e2e-events" s3 bucket event get "minio:$B5"
  expect_ok "上传对象" s3 put "$FX/hello.txt" "minio:$B5/event-obj.txt"
  # webhook 投递轮询
  delivered=0
  for i in $(seq 1 50); do
    grep -q "event-obj.txt" "$WORK/events.log" 2>/dev/null && { delivered=1; break; }
    sleep 0.2
  done
  [ "$delivered" -eq 1 ] && ok "event 实际投递到 webhook" || bad "event 未投递 (日志: $(cat "$WORK/events.log" 2>/dev/null | head -c 200))"
  expect_ok "event del" s3 bucket event del "minio:$B5"
  run s3 bucket event get "minio:$B5"
  { [ "$CODE" -ne 0 ] || ! contains "$OUT" "e2e-events"; } && ok "event del 后为空" || bad "event del 后仍残留"

  # lifecycle
  t '  bucket lifecycle'
  expect_ok "lifecycle set (flags)" s3 bucket lifecycle set "minio:$B7" --id lc1 --prefix "logs/" --expire-days 30
  expect_out "lifecycle list 含 ID" "lc1" s3 bucket lifecycle list "minio:$B7"
  run s3 bucket lifecycle list "minio:$B7" --json
  if [ "$CODE" -eq 0 ] && printf '%s' "$OUT" | sed -n '/^{/,$p' | python3 -c "import json,sys;d=json.load(sys.stdin);assert any(r.get('ID')=='lc1' for r in d.get('Rules',[]))" 2>/dev/null; then
    ok "lifecycle list --json 含规则"
  else
    bad "lifecycle list --json 断言失败 (out: $(printf '%s' "$OUT" | head -c 120))"
  fi
  expect_out "lifecycle list --expiry 视图" "lc1" s3 bucket lifecycle list "minio:$B7" --expiry
  expect_ok "lifecycle list --transition 视图 (无 transition 规则为空表)" s3 bucket lifecycle list "minio:$B7" --transition
  expect_ok "lifecycle set --disable" s3 bucket lifecycle set "minio:$B7" --id lc2 --prefix "tmp/" --expire-days 1 --disable
  expect_out "lifecycle list 含第二条" "lc2" s3 bucket lifecycle list "minio:$B7"
  expect_ok "lifecycle remove --id" s3 bucket lifecycle remove "minio:$B7" --id lc2
  printf '{"Rules":[{"ID":"fromfile","Status":"Enabled","Filter":{"Prefix":"arch/"},"Expiration":{"Days":7}}]}\n' > "$WORK/life.json"
  expect_ok "lifecycle set --from-file (整体替换)" s3 bucket lifecycle set "minio:$B7" --from-file "$WORK/life.json"
  expect_out "lifecycle list 含 from-file 规则" "fromfile" s3 bucket lifecycle list "minio:$B7"
  expect_ok "lifecycle remove --id from-file 规则" s3 bucket lifecycle remove "minio:$B7" --id fromfile
  expect_ok "lifecycle remove --all --force" s3 bucket lifecycle remove "minio:$B7" --all --force
  run s3 bucket lifecycle list "minio:$B7"
  { [ "$CODE" -eq 0 ] && ! contains "$OUT" "lc1"; } && ok "lifecycle 清空后无规则" || bad "lifecycle 清空后仍残留"

  # policy (mc anonymous 兼容)
  t '  bucket policy'
  expect_ok "policy set --type download" s3 bucket policy set "minio:$B4" --type download
  expect_out "policy get 显示 download" "download" s3 bucket policy get "minio:$B4"
  json_ok "policy get --json 输出原始策略" "assert 'Statement' in d" s3 bucket policy get "minio:$B4" --json
  expect_ok "policy set --type upload" s3 bucket policy set "minio:$B4" --type upload
  expect_out "policy get --json 含 PutObject (MinIO 归一化后显示 custom 属正常)" "s3:PutObject" s3 bucket policy get "minio:$B4" --json
  expect_ok "policy set --type public" s3 bucket policy set "minio:$B4" --type public
  expect_ok "policy set --type private (还原)" s3 bucket policy set "minio:$B4" --type private
  sed "s/BUCKET/$B4/" > "$WORK/pol.json" <<'EOF'
{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":["*"]},"Action":["s3:GetObject"],"Resource":["arn:aws:s3:::BUCKET/*"]}]}
EOF
  expect_ok "policy set --from-file" s3 bucket policy set "minio:$B4" --from-file "$WORK/pol.json"
  expect_out "policy get --json 含 GetObject" "GetObject" s3 bucket policy get "minio:$B4" --json
  expect_ok "policy del" s3 bucket policy del "minio:$B4"
  run s3 bucket policy get "minio:$B4"
  { [ "$CODE" -ne 0 ] || ! contains "$OUT" "download"; } && ok "policy del 后为空" || bad "policy del 后仍残留"

  # versioning
  t '  bucket versioning'
  expect_out "versioning info (已启用)" "Enabled" s3 bucket versioning info "minio:$B2"
  expect_ok "versioning set --status Suspended" s3 bucket versioning set "minio:$B2" --status Suspended
  expect_out "versioning info 显示 Suspended" "Suspended" s3 bucket versioning info "minio:$B2"
  expect_ok "versioning set --status Enabled" s3 bucket versioning set "minio:$B2" --status Enabled
  expect_out "versioning info 恢复 Enabled" "Enabled" s3 bucket versioning info "minio:$B2"
}

test_read_commands() {
  t '=== 读命令 (ls/du/stat/info/find/tree) ==='

  # 造数
  s3 put "$FX/hello.txt" "minio:$B1/read/hello.txt" >/dev/null 2>&1
  s3 put "$FX/lines.txt" "minio:$B1/read/lines.txt" >/dev/null 2>&1
  s3 put -r "$FX/tree/" "minio:$B1/read/tree" >/dev/null 2>&1
  s3 put "$FX/data.csv" "minio:$B1/read/data.csv" >/dev/null 2>&1
  s3 put "$FX/hello.txt" "minio:$B1/read/z.log" >/dev/null 2>&1
  : > "$FX/empty"
  DMURL="$(s3 share upload "minio:$B1/dirmark/" | strip_ansi | grep -oE 'https?://[^ ]+' | head -1)"
  curl -fsS -X PUT --data-binary '' "$DMURL" >/dev/null 2>&1 || true

  expect_out "ls 列对象" "hello.txt" s3 ls "minio:$B1/read/"
  expect_out "ls 别名 list 可用" "hello.txt" s3 list "minio:$B1/read/"
  expect_out "ls -r 递归" "deep/c.txt" s3 ls -r "minio:$B1/read/tree/"
  expect_out "ls --include glob" "data.csv" s3 ls -r "minio:$B1/read/" --include "*.csv"
  expect_not_out "ls --exclude glob" "data.csv" s3 ls -r "minio:$B1/read/" --exclude "*.csv"
  expect_out "ls --summarize" "object(s)" s3 ls --summarize -r "minio:$B1/read/"
  jsonl_ok "ls --json 结构化输出" "assert any(r.get('path','').endswith('hello.txt') for r in rows)" \
    s3 ls --json -r "minio:$B1/read/"

  expect_ok "du 前缀" s3 du "minio:$B1/read/"
  expect_ok "du -r 按前缀总计" s3 du -r "minio:$B1/read/"
  expect_ok "du -d 深度限制" s3 du -r -d 1 "minio:$B1/read/"
  json_ok "du --json" "assert isinstance(d, dict) or isinstance(d, list)" s3 du --json "minio:$B1/read/"

  expect_out "stat 对象" "hello.txt" s3 stat "minio:$B1/read/hello.txt"
  expect_out "stat 桶" "$B1" s3 stat "minio:$B1"
  expect_ok "stat -r 递归" s3 stat -r "minio:$B1/read/"
  json_ok "stat --json" "assert isinstance(d, dict) or isinstance(d, list)" s3 stat --json "minio:$B1/read/hello.txt"

  expect_out "info 对象 (JSON)" "hello.txt" s3 info "minio:$B1/read/hello.txt"
  expect_ok "info -r" s3 info -r "minio:$B1/read/"

  # find
  expect_out "find --name glob" "z.log" s3 find "minio:$B1/read/" --name "*.log"
  expect_out "find --regex" "data.csv" s3 find "minio:$B1/read/" --regex ".*\.csv"
  expect_out "find --path 目录名" "a.txt" s3 find "minio:$B1/read/" --path "tree"
  expect_out "find --larger" "lines.txt" s3 find "minio:$B1/read/" --larger 10B
  expect_out "find --smaller" "hello.txt" s3 find "minio:$B1/read/" --smaller 1KB
  expect_out "find --newer-than 1h" "hello.txt" s3 find "minio:$B1/read/" --newer-than 1h
  expect_out "find --older-than 1h 空结果" "no objects matched" s3 find "minio:$B1/read/" --older-than 1h
  expect_out "find --max-depth 1" "hello.txt" s3 find "minio:$B1/read/" --max-depth 1
  expect_out "find --min-depth 2" "c.txt" s3 find "minio:$B1/read/tree/" --min-depth 2
  jsonl_ok "find --limit 2" "assert len(rows)==2" s3 find "minio:$B1/read/" --limit 2 --json
  expect_out "find --ignore 排除" "data.csv" s3 find "minio:$B1/read/" --ignore "*.log"
  run s3 find "minio:$B1/read/" --ignore "*.log"
  contains "$OUT" "z.log" && bad "find --ignore 应排除 z.log" || ok "find --ignore 排除生效"
  expect_out "find --include 仅匹配" "data.csv" s3 find "minio:$B1/read/" --include "*.csv"
  expect_out "find --print 模板" "read/hello.txt" s3 find "minio:$B1/read/" --name "hello.txt" --print "{path}"
  expect_out "find --sort size" "hello.txt" s3 find "minio:$B1/read/" --sort size
  expect_ok "find --sort -size 降序" s3 find "minio:$B1/read/" --sort -size
  expect_ok "find --reverse" s3 find "minio:$B1/read/" --sort name --reverse
  expect_out "find --type file" "hello.txt" s3 find "minio:$B1/" --type file
  expect_out "find --type dir 目录标记" "dirmark" s3 find "minio:$B1/" --type dir
  expect_ok "find --storage-class STANDARD" s3 find "minio:$B1/read/" --storage-class STANDARD
  jsonl_ok "find --json" "assert any('hello.txt' in r.get('path','') for r in rows)" \
    s3 find "minio:$B1/read/" --name "hello.txt" --json

  # tree
  expect_out "tree 目录视图" "read" s3 tree "minio:$B1/"
  expect_out "tree --files 含文件" "hello.txt" s3 tree "minio:$B1/read/" --files
  expect_ok "tree -d 深度" s3 tree "minio:$B1/" -d 1
  expect_out "tree -s 显示大小" "B" s3 tree "minio:$B1/read/" --files -s
  json_ok "tree --json" "assert isinstance(d, dict) or isinstance(d, list)" s3 tree --json "minio:$B1/read/"
}

test_transfer() {
  t '=== 传输 (put/get/cp/mv/rm) ==='

  expect_ok "put 单文件" s3 put "$FX/hello.txt" "minio:$B1/tx/hello.txt"
  expect_out "put 后 ls 可见" "tx/hello.txt" s3 ls -r "minio:$B1/tx/"
  s3 put "$FX/lines.txt" "minio:$B1/tx/over.txt" >/dev/null 2>&1
  expect_out "put 已存在默认跳过" "already exists" s3 put "$FX/hello.txt" "minio:$B1/tx/over.txt"
  expect_ok "put --overwrite 覆盖" s3 put --overwrite "$FX/lines.txt" "minio:$B1/tx/over.txt"
  run s3 get "minio:$B1/tx/over.txt" -
  contains "$OUT" "line one" && ok "put --overwrite 后内容已替换" || bad "put --overwrite 内容未替换"
  expect_out "put --content-type" "text/csv" s3 put --content-type text/csv "$FX/data.csv" "minio:$B1/tx/ct.csv"
  expect_ok "put --metadata 上传" s3 put --metadata k1=v1 "$FX/hello.txt" "minio:$B1/tx/meta.txt"
  run s3 stat "minio:$B1/tx/meta.txt"
  { contains "$OUT" "x-amz-meta-K1" || contains "$OUT" "x-amz-meta-k1"; } \
    && ok "put --metadata 元数据已写入 (stat Metadata 段可见)" || bad "put --metadata 元数据缺失 (stat: $(printf '%s' "$OUT" | head -c 150))"
  expect_ok "put --tags 上传" s3 put --tags "env=e2e" "$FX/hello.txt" "minio:$B1/tx/tagged.txt"
  expect_out "put --tags 标签可查" "env" s3 tag list "minio:$B1/tx/tagged.txt"
  expect_ok "put --storage-class REDUCED_REDUNDANCY" s3 put --storage-class REDUCED_REDUNDANCY "$FX/hello.txt" "minio:$B1/tx/sc.txt"
  expect_ok "put --sc 别名" s3 put --sc REDUCED_REDUNDANCY "$FX/hello.txt" "minio:$B1/tx/sc2.txt"
  expect_ok "put -r 目录" s3 put -r "$FX/tree/" "minio:$B1/tx/tree"
  expect_out "put -r 递归完整" "deep/c.txt" s3 ls -r "minio:$B1/tx/tree/"
  expect_ok "put --concurrency 2" s3 put --concurrency 2 -r "$FX/tree/" "minio:$B1/tx/tree2"
  expect_ok "put -q 静默" s3 put -q "$FX/hello.txt" "minio:$B1/tx/q.txt"

  # stdin 上传 (单 PUT 路径)
  run_stdin "from-stdin-data" s3 put - "minio:$B1/tx/stdin.txt"
  [ "$CODE" -eq 0 ] && ok "put - stdin 上传 (小)" || bad "put - stdin (exit=$CODE: $ERR)"
  expect_out "stdin 上传内容可读回" "from-stdin-data" s3 get "minio:$B1/tx/stdin.txt" -

  # stdin 上传 (≥64MiB → 分片路径)
  run bash -c "head -c 73400320 /dev/zero | env HOME=\"$HOME_DIR\" \"$S3CLI_BIN\" -f \"$CONF\" put - \"minio:$B1/tx/stdin-big.bin\""
  [ "$CODE" -eq 0 ] && ok "put - stdin 分片上传 (70MiB)" || bad "put - stdin 分片 (exit=$CODE: $ERR)"
  run s3 find "minio:$B1/tx/stdin-big.bin" --print '{size}'
  contains "$OUT" "73400320" && ok "stdin 分片上传大小正确 (73400320)" || bad "stdin 分片大小 (out: $(printf '%s' "$OUT" | head -c 120))"

  # 大文件分片 (可续传路径)
  expect_ok "put 256MiB 大文件 (自动分片)" s3 put -q "$FX/big.bin" "minio:$B1/tx/big.bin"

  expect_ok "get 单文件" s3 get "minio:$B1/tx/hello.txt" "$WORK/got.txt"
  grep -qF "hello world" "$WORK/got.txt" && ok "get 下载内容一致" || bad "get 下载内容不符"
  expect_ok "get -r 目录" s3 get -r "minio:$B1/tx/tree/" "$WORK/got-tree"
  grep -qF "c" "$WORK/got-tree/sub/deep/c.txt" && ok "get -r 目录内容一致" || bad "get -r 目录内容不符"
  expect_ok "get --concurrency 2" s3 get --concurrency 2 -r "minio:$B1/tx/tree2/" "$WORK/got-tree2"
  expect_out "get 已存在默认跳过" "already exists" s3 get "minio:$B1/tx/hello.txt" "$WORK/got.txt"
  expect_ok "get --overwrite" s3 get --overwrite "minio:$B1/tx/hello.txt" "$WORK/got.txt"
  expect_ok "get --range 字节区间" s3 get --range "bytes=0-4" "minio:$B1/tx/hello.txt" "$WORK/got-range.txt"
  [ "$(cat "$WORK/got-range.txt")" = "hello" ] && ok "get --range 内容为 hello" || bad "get --range 内容: $(cat "$WORK/got-range.txt" 2>/dev/null)"
  expect_out "get - 流式到 stdout" "hello world" s3 get "minio:$B1/tx/hello.txt" -
  expect_out "get - -o 偏移" "world" s3 get -o 6 "minio:$B1/tx/hello.txt" -
  expect_out "get - -n 行数" "hello world" s3 get -n 1 "minio:$B1/tx/hello.txt" -
  expect_out "get - -t 尾部字节" "orld" s3 get -t 5 "minio:$B1/tx/hello.txt" -

  expect_ok "cp 单对象" s3 cp "minio:$B1/tx/hello.txt" "minio:$B1/tx/cp-hello.txt"
  expect_out "cp 后目标存在" "cp-hello.txt" s3 ls "minio:$B1/tx/"
  expect_ok "cp -r 目录" s3 cp -r "minio:$B1/tx/tree/" "minio:$B1/tx/cp-tree/"
  expect_out "cp -r 结果完整" "deep/c.txt" s3 ls -r "minio:$B1/tx/cp-tree/"
  expect_ok "cp --storage-class/--sc" s3 cp --sc REDUCED_REDUNDANCY "minio:$B1/tx/hello.txt" "minio:$B1/tx/cp-sc.txt"
  expect_ok "cp --metadata 替换元数据" s3 cp --metadata k2=v2 "minio:$B1/tx/meta.txt" "minio:$B1/tx/cp-meta.txt"
  expect_ok "cp --tags" s3 cp --tags "cp=e2e" "minio:$B1/tx/hello.txt" "minio:$B1/tx/cp-tagged.txt"
  expect_ok "cp -q 静默" s3 cp -q "minio:$B1/tx/hello.txt" "minio:$B1/tx/cp-q.txt"

  expect_ok "mv 单对象" s3 mv "minio:$B1/tx/cp-hello.txt" "minio:$B1/tx/mv-hello.txt"
  expect_out "mv 后目标存在" "mv-hello.txt" s3 ls "minio:$B1/tx/"
  run s3 stat "minio:$B1/tx/cp-hello.txt"
  [ "$CODE" -ne 0 ] && ok "mv 后源不存在" || bad "mv 后源仍存在"
  expect_ok "mv -r 目录" s3 mv -r "minio:$B1/tx/cp-tree/" "minio:$B1/tx/mv-tree/"
  expect_out "mv -r 结果完整" "deep/c.txt" s3 ls -r "minio:$B1/tx/mv-tree/"
  expect_ok "mv --metadata/--tags/--sc" s3 mv --metadata m1=x --tags "mv=e2e" --sc REDUCED_REDUNDANCY "minio:$B1/tx/mv-hello.txt" "minio:$B1/tx/mv2.txt"
  expect_ok "mv -q 静默" s3 mv -q "minio:$B1/tx/mv2.txt" "minio:$B1/tx/mv3.txt"

  # rm
  expect_out "rm --dry-run 不真删" "a.txt" s3 rm --dry-run -r --force "minio:$B1/tx/tree2/"
  expect_out "rm --dry-run 后仍存在" "deep/c.txt" s3 ls -r "minio:$B1/tx/tree2/"
  expect_err_out "rm -r 无 --force 拒绝" "force" s3 rm -r "minio:$B1/tx/tree2/"
  expect_ok "rm -r --force 递归删除" s3 rm -r --force "minio:$B1/tx/tree2/"
  run s3 ls -r "minio:$B1/tx/tree2/"
  { [ "$CODE" -eq 0 ] && [ -z "$OUT" ]; } && ok "rm -r 后目录为空" || bad "rm -r 后仍残留"
  expect_ok "rm --include/--exclude 过滤" s3 rm -r --force "minio:$B1/tx/tree/" --include "sub/*" --exclude "deep/*"
  run s3 ls -r "minio:$B1/tx/tree/"
  { [ "$CODE" -eq 0 ] && contains "$OUT" "a.txt" && ! contains "$OUT" "b.txt"; } \
    && ok "rm --include 只删 sub/* (a.txt 保留, b.txt 已删)" || bad "rm --include 结果不符: $(printf '%s' "$OUT" | head -c 150)"
  expect_ok "rm --older-than/--newer-than" s3 rm -r --force --older-than 1h "minio:$B1/tx/tree/"
  run_stdin "$(printf 'tx/q.txt\ntx/sc2.txt\n')" s3 rm "minio:$B1" --stdin
  [ "$CODE" -eq 0 ] && ok "rm --stdin 逐行删除" || bad "rm --stdin (exit=$CODE: $ERR)"
  run s3 stat "minio:$B1/tx/q.txt"
  [ "$CODE" -ne 0 ] && ok "rm --stdin 后对象不存在" || bad "rm --stdin 后仍存在"

  # 版本控制下的 rm
  s3 put "$FX/hello.txt" "minio:$B2/vkey.txt" >/dev/null 2>&1
  s3 put --overwrite "$FX/lines.txt" "minio:$B2/vkey.txt" >/dev/null 2>&1
  # find --versions 输出所有版本; 取第一行的 version-id 用于定向删除
  V1="$(s3 find --versions "minio:$B2/vkey.txt" --print '{version-id}' 2>/dev/null | head -1)"
  expect_ok "rm --version-id 删指定版本" s3 rm --version-id "$V1" "minio:$B2/vkey.txt"
  expect_ok "rm 普通删除 (版本桶产生删除标记)" s3 rm "minio:$B2/vkey.txt"
  expect_out "rm 无版本号产生删除标记" "DEL" s3 ls --versions "minio:$B2/vkey.txt"
  expect_ok "rm --versions 删所有版本+标记" s3 rm --versions "minio:$B2/vkey.txt"
  run s3 ls --versions "minio:$B2/vkey.txt"
  { [ "$CODE" -eq 0 ] && ! contains "$OUT" "vkey"; } && ok "rm --versions 后完全清除" || bad "rm --versions 后仍残留 ($OUT)"

  # 非当前版本删除
  s3 put "$FX/hello.txt" "minio:$B2/nc.txt" >/dev/null 2>&1
  s3 put --overwrite "$FX/lines.txt" "minio:$B2/nc.txt" >/dev/null 2>&1
  expect_ok "rm --non-current 删旧版本" s3 rm --non-current "minio:$B2/nc.txt"
  expect_out "当前版本仍在 (rm --non-current 保留最新版)" "line one" s3 get "minio:$B2/nc.txt" -

  run s3 restore --days 2 --tier Standard "minio:$B1/tx/hello.txt"
  [ "$CODE" -ne 0 ] && [ -n "$ERR$OUT" ] && ok "restore 在 MinIO 优雅报错 (Glacier 未配置, exit=$CODE)" \
    || bad "restore 负向用例异常 (exit=$CODE: $(printf '%s' "$ERR$OUT" | head -c 150))"
}

test_sql_tag() {
  t '=== S3 Select (sql) 与 tag ==='

  s3 put "$FX/data.csv" "minio:$B1/sql/data.csv" >/dev/null 2>&1
  s3 put "$FX/data.jsonl" "minio:$B1/sql/data.jsonl" >/dev/null 2>&1
  s3 put "$FX/data.csv.gz" "minio:$B1/sql/data.csv.gz" >/dev/null 2>&1
  s3 put "$FX/data.csv" "minio:$B1/sql/multi/a.csv" >/dev/null 2>&1
  s3 put "$FX/data.csv" "minio:$B1/sql/multi/b.csv" >/dev/null 2>&1

  expect_out "sql 默认查询 CSV" "Alice" s3 sql "minio:$B1/sql/data.csv" -e "select * from S3Object"
  expect_out "sql 条件过滤" "30" s3 sql "minio:$B1/sql/data.csv" -e "select * from S3Object s where s._2 = '30'"
  expect_out "sql --csv-input 自定义分隔符" "Alice" s3 sql "minio:$B1/sql/data.csv" \
    --csv-input "rd=\n,fh=USE" -e "select * from S3Object"
  expect_out "sql --csv-output-header" "name" s3 sql "minio:$B1/sql/data.csv" \
    --csv-output-header "name,age" -e "select * from S3Object"
  expect_out "sql JSON lines 输入" "Alice" s3 sql "minio:$B1/sql/data.jsonl" \
    --json-input "type=LINES" -e "select * from S3Object"
  expect_out "sql --compression GZIP" "Eve" s3 sql "minio:$B1/sql/data.csv.gz" \
    --compression GZIP -e "select * from S3Object"
  expect_out "sql -r 递归前缀" "Carol" s3 sql -r "minio:$B1/sql/multi/" -e "select * from S3Object"

  expect_out "tag set 对象" "Tag set" s3 tag set "minio:$B1/tx/tagged.txt" --tag "env=e2e"
  expect_out "tag list 对象" "env" s3 tag list "minio:$B1/tx/tagged.txt"
  json_ok "tag list --json" "assert isinstance(d, dict)" s3 tag list "minio:$B1/tx/tagged.txt" --json
  expect_ok "tag remove 对象" s3 tag remove "minio:$B1/tx/tagged.txt"
  run s3 tag list "minio:$B1/tx/tagged.txt"
  { [ "$CODE" -eq 0 ] && ! contains "$OUT" "env"; } && ok "tag remove 后为空" || bad "tag remove 后仍残留"

  expect_out "tag set 桶级" "Tag set" s3 tag set "minio:$B1" --tag "bkt=e2e"
  expect_out "tag list 桶级" "bkt" s3 tag list "minio:$B1"
  expect_ok "tag remove 桶级" s3 tag remove "minio:$B1"
}

test_mirror() {
  t '=== mirror 镜像同步 ==='

  # 源数据
  s3 put -r "$FX/tree/" "minio:$B1/mir/src" >/dev/null 2>&1
  s3 put "$FX/data.csv" "minio:$B1/mir/src/data.csv" >/dev/null 2>&1

  expect_out "mirror --dry-run 计划" "COPY" s3 mirror --dry-run "minio:$B1/mir/src/" "minio1:$B1/mir/dst/"
  run s3 ls -r "minio1:$B1/mir/dst/"
  { [ "$CODE" -eq 0 ] && [ -z "$OUT" ]; } && ok "dry-run 不产生目标对象" || bad "dry-run 不应写目标 ($OUT)"

  expect_ok "mirror 同端点复制 (服务端 CopyObject)" s3 mirror -q "minio:$B1/mir/src/" "minio1:$B1/mir/dst/"
  expect_out "mirror 后目标完整" "deep/c.txt" s3 ls -r "minio1:$B1/mir/dst/"

  # 增量: 目标已有且未变 → 无操作; 源新增 → 只复制新增
  s3 put "$FX/hello.txt" "minio:$B1/mir/src/new.txt" >/dev/null 2>&1
  expect_out "mirror 增量同步新对象" "new.txt" s3 mirror "minio:$B1/mir/src/" "minio1:$B1/mir/dst/"
  expect_out "增量后目标含 new.txt" "new.txt" s3 ls -r "minio1:$B1/mir/dst/"

  # --include/--exclude
  expect_ok "mirror --include 过滤" s3 mirror -q --include "*.csv" "minio:$B1/mir/src/" "minio1:$B1/mir/only-csv/"
  expect_out "mirror --include 结果" "data.csv" s3 ls -r "minio1:$B1/mir/only-csv/"
  expect_ok "mirror --exclude 过滤" s3 mirror -q --exclude "*.csv" "minio:$B1/mir/src/" "minio1:$B1/mir/no-csv/"
  run s3 ls -r "minio1:$B1/mir/no-csv/"
  contains "$OUT" "data.csv" && bad "mirror --exclude 应排除 csv" || ok "mirror --exclude 生效"

  # --remove 删除目标多余对象 + --max-delete 保护
  s3 put "$FX/hello.txt" "minio:$B1/mir/rm-dst/extra.txt" >/dev/null 2>&1
  expect_out "mirror --remove 删除多余" "deleted=1" s3 mirror --remove -q "minio:$B1/mir/src/" "minio1:$B1/mir/rm-dst/"
  run s3 stat "minio:$B1/mir/rm-dst/extra.txt"
  [ "$CODE" -ne 0 ] && ok "mirror --remove 后多余对象已删" || bad "mirror --remove 未删除多余对象"
  s3 put "$FX/hello.txt" "minio:$B1/mir/rm-dst/extra.txt" >/dev/null 2>&1
  s3 put "$FX/hello.txt" "minio:$B1/mir/rm-dst/extra2.txt" >/dev/null 2>&1
  expect_err_out "mirror --max-delete 拦截" "max-delete" s3 mirror --remove --max-delete 1 -q "minio:$B1/mir/src/" "minio1:$B1/mir/rm-dst/"

  # --manifest + --resume 断点
  expect_ok "mirror --manifest 记录" s3 mirror -q --manifest "$WORK/mir.manifest" "minio:$B1/mir/src/" "minio1:$B1/mir/man-dst/"
  expect_out "manifest 文件记录 key" "a.txt" sh -c "cat '$WORK/mir.manifest'"
  expect_out "mirror --resume 跳过已完成" "skipped" s3 mirror --resume -q --manifest "$WORK/mir.manifest" "minio:$B1/mir/src/" "minio1:$B1/mir/man-dst/"
  expect_err_out "mirror --resume 无 --manifest 报错" "requires --manifest" s3 mirror --resume -q "minio:$B1/mir/src/" "minio1:$B1/mir/man-dst/"

  # --size-limit
  expect_ok "mirror --size-limit 跳过超大对象" s3 mirror -q --size-limit 10 "minio:$B1/mir/src/" "minio1:$B1/mir/small/"

  # --overwrite 内容变化后覆盖
  s3 put --overwrite "$FX/hello.txt" "minio:$B1/mir/src/data.csv" >/dev/null 2>&1
  expect_out "mirror --overwrite 覆盖变更对象" "data.csv" s3 mirror --overwrite -q "minio:$B1/mir/src/" "minio1:$B1/mir/dst/"
  expect_out "覆盖后内容一致" "hello world" s3 get "minio:$B1/mir/dst/data.csv" -

  # --storage-class / --sc
  expect_ok "mirror --storage-class" s3 mirror -q --storage-class REDUCED_REDUNDANCY "minio:$B1/mir/src/" "minio1:$B1/mir/sc-dst/"
  expect_ok "mirror --sc 别名" s3 mirror -q --sc REDUCED_REDUNDANCY "minio:$B1/mir/src/" "minio1:$B1/mir/sc2-dst/"

  # 同桶前缀重叠防护 (负向)
  expect_err_out "mirror 同桶前缀包含被拒" "overlap" s3 mirror -q "minio:$B1/mir/" "minio1:$B1/mir/src/"

  # 跨端点 (minio2)
  s3 bucket create "minio2:e2e-x" >/dev/null 2>&1
  expect_ok "mirror 跨端点 (下载+上传)" s3 mirror -q --part-size 5 "minio:$B1/mir/src/" "minio2:e2e-x/mir-x/"
  expect_out "跨端点后目标完整" "deep/c.txt" s3 ls -r "minio2:e2e-x/mir-x/"
}

test_diff() {
  t '=== diff 差异比对 ==='

  s3 put -r "$FX/tree/" "minio:$B1/diff/s3a" >/dev/null 2>&1
  s3 put -r "$FX/tree/" "minio:$B1/diff/s3b" >/dev/null 2>&1
  mkdir -p "$WORK/local-tree" "$WORK/local-tree-diff"
  cp -R "$FX/tree/." "$WORK/local-tree/"
  cp -R "$FX/tree/." "$WORK/local-tree-diff/"
  printf 'x\n' > "$WORK/local-tree-diff/sub/b.txt"   # 与 b.txt 同尺寸 (2B), 内容不同

  expect_out "diff 相同目录 (s3↔s3)" "OK" s3 diff "minio:$B1/diff/s3a/" "minio:$B1/diff/s3b/"
  expect_code "diff 相同目录退出码 0" 0 s3 diff "minio:$B1/diff/s3a/" "minio:$B1/diff/s3b/"

  expect_out "diff 本地↔s3 一致" "OK" s3 diff "$WORK/local-tree" "minio:$B1/diff/s3a/"
  expect_code "diff 一致退出码 0" 0 s3 diff "$WORK/local-tree" "minio:$B1/diff/s3a/"

  expect_code "diff 发现差异退出码 6" 6 s3 diff "$WORK/local-tree-diff" "minio:$B1/diff/s3a/"
  expect_out_code "diff --brief 只输出差异" 6 "b.txt" s3 diff --brief "$WORK/local-tree-diff" "minio:$B1/diff/s3a/"
  run s3 diff --brief "$WORK/local-tree-diff" "minio:$B1/diff/s3a/"
  contains "$OUT" "OK" && bad "diff --brief 不应输出 OK" || ok "diff --brief 隐藏相同项"

  expect_code "diff --check size 同尺寸判同 (内容差异被忽略)" 0 s3 diff --check size "$WORK/local-tree-diff" "minio:$B1/diff/s3a/"
  # quick 比较 mtime: 本地 mtime 与 S3 LastModified 是两个时钟 (后者=上传时刻),
  # 跨来源比较必然误报 —— 新语义跳过 mtime (同尺寸判同); mtime 检出改用同源目录验证
  touch -t 202001010000 "$WORK/local-tree-diff/sub/b.txt"
  expect_code "diff --check quick 本地↔s3 跳过不可比的 mtime" 0 s3 diff --check quick "$WORK/local-tree-diff" "minio:$B1/diff/s3a/"
  expect_code "diff --check quick 同源目录检出 mtime 差异" 6 s3 diff --check quick "$WORK/local-tree" "$WORK/local-tree-diff"
  expect_code "diff --check md5 检出内容差异" 6 s3 diff --check md5 "$WORK/local-tree-diff" "minio:$B1/diff/s3a/"
  expect_ok "diff --concurrency 并发比对" s3 diff --concurrency 2 "$WORK/local-tree" "minio:$B1/diff/s3a/"
  json_ok "diff --json 结构化输出" "assert isinstance(d, dict) or isinstance(d, list)" \
    s3 diff --json "$WORK/local-tree" "minio:$B1/diff/s3a/"
  s3 put "$FX/hello.txt" "minio:$B1/diff/one.txt" >/dev/null 2>&1
  expect_code "diff 单文件一致" 0 s3 diff "$FX/hello.txt" "minio:$B1/diff/one.txt"
}

test_mpu() {
  t '=== 分片上传管理 (mpu) 与断点续传 ==='

  # 中断一次本地大文件上传: 产生服务端 in-progress + 本地状态文件
  # 直接后台运行 s3cli: SIGTERM 触发 NotifyContext 优雅取消 (退出码 130),
  # 服务端分片上传保持 in-progress, 本地状态文件保留 —— 供 ls -I/mpu/续传测试。
  start_interrupted() {
    # 清理上一轮可能已完成的对象 (put 默认跳过已存在对象, 会拿不到状态文件)
    s3 rm "minio:$B1/mpu/big.bin" >/dev/null 2>&1 || true
    env HOME="$HOME_DIR" "$S3CLI_BIN" -f "$CONF" put -q --part-size 5 "$FX/big.bin" "minio:$B1/mpu/big.bin" \
      >"$WORK/.put.log" 2>&1 &
    PUT_PID=$!
    local waited=0
    while [ -z "$(ls "$MPU_DIR"/*.json 2>/dev/null | head -1)" ] && kill -0 "$PUT_PID" 2>/dev/null && [ "$waited" -lt 400 ]; do
      sleep 0.02; waited=$((waited + 1))
    done
    kill "$PUT_PID" 2>/dev/null
    wait "$PUT_PID" 2>/dev/null
    PUT_CODE=$?
    [ -z "$(ls "$MPU_DIR"/*.json 2>/dev/null | head -1)" ] && return 1
    return 0
  }

  if start_interrupted; then
    ok "中断上传产生本地状态文件"
  else
    bad "中断上传未产生本地状态 (日志: $(cat "$WORK/.put.log" 2>/dev/null | head -c 200))"
    return
  fi
  [ "$PUT_CODE" -eq 130 ] && ok "被中断的 put 退出码 130" || bad "被中断的 put 退出码 $PUT_CODE (期望 130)"

  # MinIO 对 ListMultipartUploads 的 prefix 过滤不可靠, 这里用桶级列举
  expect_out "ls -I 显示进行中上传" "big.bin" s3 ls -I "minio:$B1"
  expect_out "mpu list 显示进行中上传" "big.bin" s3 mpu list "minio:$B1"
  UPLOAD_ID="$(s3 mpu list --json "minio:$B1" 2>/dev/null | python3 -c '
import json,sys
rows=[json.loads(l) for l in sys.stdin if l.strip()]
assert rows, "no uploads"
print(rows[0].get("upload_id") or rows[0].get("UploadID") or rows[0].get("uploadId",""))')"
  jsonl_ok "mpu list --json" "assert any('big.bin' in str(r) for r in rows)" \
    s3 mpu list --json "minio:$B1"

  expect_out "mpu local-list 显示本地状态" "big.bin" s3 mpu local-list
  STATE_PATH="$(s3 mpu local-list --json 2>/dev/null | python3 -c '
import json,sys
d=json.load(sys.stdin)
assert d, "no states"
print(d[0].get("state_path") or d[0].get("StatePath",""))')"
  json_ok "mpu local-list --json" "assert isinstance(d, list) and len(d) >= 1" s3 mpu local-list --json

  expect_ok "mpu abort --upload-id 自动解析 key" s3 mpu abort "minio:$B1" --upload-id "$UPLOAD_ID"
  run s3 mpu list "minio:$B1"
  contains "$OUT" "big.bin" && bad "mpu abort 后仍存在" || ok "mpu abort 后服务端无上传"

  # 本地状态仍存在 (服务端已中止) → 续传自愈 (NoSuchUpload → 重建)
  expect_ok "put 断点自愈 (服务端已中止) 完成上传" s3 put -q --part-size 5 "$FX/big.bin" "minio:$B1/mpu/big.bin"
  run ls "$MPU_DIR"/*.json 2>/dev/null
  [ -z "$(ls "$MPU_DIR"/*.json 2>/dev/null)" ] && ok "自愈完成后本地状态已清除" || bad "自愈完成后状态仍残留"
  expect_out "自愈后对象完整" "big.bin" s3 ls "minio:$B1/mpu/"

  # 真正断点续传: 中断 → 不中止 → 重跑 put → ListParts 续传 → 完成
  if start_interrupted; then
    ok "第二次中断上传"
  else
    bad "第二次中断上传失败"
    return
  fi
  expect_ok "put 续传剩余分片并完成" s3 put -q --part-size 5 "$FX/big.bin" "minio:$B1/mpu/big.bin"
  run ls "$MPU_DIR"/*.json 2>/dev/null
  [ -z "$(ls "$MPU_DIR"/*.json 2>/dev/null)" ] && ok "续传完成后本地状态已清除" || bad "续传后状态仍残留"

  # rm -I 中止进行中上传
  if start_interrupted; then
    ok "第三次中断上传"
  else
    bad "第三次中断上传失败"
    return
  fi
  expect_out "rm -I 中止分片上传" "aborted 1" s3 rm -I -r --force "minio:$B1"
  run s3 mpu list "minio:$B1"
  contains "$OUT" "big.bin" && bad "rm -I 后服务端仍有上传" || ok "rm -I 后服务端无上传"

  # mpu abort 全前缀 (无 --upload-id)
  if start_interrupted; then
    ok "第四次中断上传"
  else
    bad "第四次中断上传失败"
    return
  fi
  expect_ok "mpu abort 全桶" s3 mpu abort "minio:$B1"
  run s3 mpu list "minio:$B1"
  contains "$OUT" "big.bin" && bad "mpu abort 全桶后仍存在" || ok "mpu abort 全桶后服务端无上传"

  # local-clear 精确清理
  if start_interrupted; then
    ok "第五次中断上传"
  else
    bad "第五次中断上传失败"
    return
  fi
  STATE_PATH="$(s3 mpu local-list --json 2>/dev/null | python3 -c '
import json,sys
d=json.load(sys.stdin)
assert d
print(d[0].get("state_path") or d[0].get("StatePath",""))')"
  expect_ok "mpu local-clear 清理指定状态" s3 mpu local-clear "$STATE_PATH"
  run ls "$MPU_DIR"/*.json 2>/dev/null
  [ -z "$(ls "$MPU_DIR"/*.json 2>/dev/null)" ] && ok "local-clear 后状态目录为空" || bad "local-clear 后仍残留"
  run s3 mpu local-clear "$STATE_PATH"
  [ "$CODE" -ne 0 ] && ok "mpu local-clear 重复清理报错" || bad "mpu local-clear 重复清理应报错"
  expect_err_out "mpu local-clear 越界路径拒绝" "refusing" s3 mpu local-clear "/tmp/not-a-state.json"
  # 清理这次中断的服务端残留
  s3 mpu abort "minio:$B1" >/dev/null 2>&1
}

test_share_completion() {
  t '=== share 预签名与 completion 补全 ==='

  s3 put "$FX/hello.txt" "minio:$B1/share/hello.txt" >/dev/null 2>&1

  expect_out "share download 生成 URL" "http" s3 share download "minio:$B1/share/hello.txt"
  URL="$(s3 share download "minio:$B1/share/hello.txt" | strip_ansi | grep -oE 'https?://[^ ]+' | head -1)"
  run curl -fsS "$URL"
  contains "$OUT" "hello world" && ok "预签名 URL 可下载且内容一致" || bad "预签名下载 (exit=$CODE: $ERR)"
  expect_out "share download --expire 生效" "X-Amz-Expires=3600" s3 share download --expire 3600 "minio:$B1/share/hello.txt"
  expect_ok "share download --v2 (SigV2)" s3 share download --v2 "minio:$B1/share/hello.txt"

  expect_out "share upload 生成 PUT URL" "X-Amz-Signature" s3 share upload "minio:$B1/share/up.txt"
  PURL="$(s3 share upload "minio:$B1/share/up.txt" | strip_ansi | grep -oE 'https?://[^ ]+' | head -1)"
  run curl -fsS -X PUT --upload-file "$FX/hello.txt" "$PURL"
  [ "$CODE" -eq 0 ] && ok "预签名 URL 可上传" || bad "预签名上传 (exit=$CODE: $ERR)"
  expect_out "上传后对象可读" "hello world" s3 get "minio:$B1/share/up.txt" -

  expect_ok "completion bash" s3 completion bash
  run s3 completion bash
  printf '%s' "$OUT" > "$WORK/s3cli.bash"
  run bash -n "$WORK/s3cli.bash"
  [ "$CODE" -eq 0 ] && ok "completion bash 语法正确" || bad "completion bash 语法错误"
  expect_out "completion bash --no-descriptions" "_s3cli" s3 completion bash --no-descriptions
  expect_out "completion zsh" "#compdef s3cli" s3 completion zsh
  expect_ok "completion zsh --no-descriptions" s3 completion zsh --no-descriptions
  expect_out "completion fish" "complete" s3 completion fish
  expect_ok "completion powershell" s3 completion powershell
  expect_out "completion powershell 内容" "Register-ArgumentCompleter" s3 completion powershell
}

# ---------- 主流程 ----------

main() {
  B1="e2e-b1"; B_R="e2e-region"; B2="e2e-ver"; B3="e2e-lock"; B4="e2e-policy"
  B5="e2e-event"; B6="e2e-enc"; B7="e2e-life"

  printf 's3cli e2e 测试 (MinIO)\n'
  printf '  s3cli: %s\n  work:  %s\n' "$S3CLI_BIN" "$WORK"
  setup

  test_global_flags
  test_alias
  test_bucket
  test_read_commands
  test_transfer
  test_sql_tag
  test_mirror
  test_diff
  test_mpu
  test_share_completion

  printf '\n==============================\n'
  printf '通过: %d   失败: %d\n' "$pass" "$fail"
  if [ "$fail" -gt 0 ]; then
    printf '失败列表:%s\n' "$failures"
    exit 1
  fi
  printf '全部通过 ✅\n'
  exit 0
}

trap cleanup EXIT INT TERM
main

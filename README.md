# s3cli

轻量、高性能、简单的 S3 命令行客户端

中文 · [English](README.en.md)

## 特性

- 零 AWS SDK 依赖：基于 `net/http` 自实现 S3 REST API 与 SigV4 签名，兼容任何 S3 服务
- 真正实现多断点管理（本地状态文件 + 服务端 `ListParts` 对账式续传）
- 支持 Path-style、DNS、自定义模板（`%(bucket)`/`%(region)`，含区域探测）三种桶寻址方式
- 上传 / 下载 / 复制 / 移动 / 删除，支持递归整个目录树；stdin 管道上传（`put -`）、stdout 流式输出（`get -`）
- 实时进度条（速率/ETA/终端宽度自适应）、分片上传下载
- 桶配置管理：CORS、生命周期、策略、加密、版本控制、事件通知、标签、ACL、Object Lock、复制
- 跨端点镜像同步（同端零拷贝 / 跨端流式，manifest 断点续传）、文件差异对比（size/quick/md5）
- 查找（find）、树形展示（tree）、磁盘占用（du）、S3 Select（sql）、归档恢复（restore）、预签名 URL（share）
- 结构化 JSON 输出、中英双语帮助、Bash / Zsh / Fish / PowerShell 自动补全

## 安装

```bash
# Go Version 1.26+
git clone https://github.com/onmk613/s3cli.git
cd s3cli && bash build.sh
mv ./s3cli /usr/local/bin/
s3cli help
```

## 快速开始

```bash
# 1. 配置端点（别名）
s3cli alias add my-s3 https://s3.example.com AKIA... SECRET

# 2. 常用操作
s3cli ls my-s3:                             # 列出所有桶
s3cli ls my-s3:my-bucket/                   # 列出桶内对象
s3cli put ./data my-s3:my-bucket/backup/    # 上传目录（递归）
s3cli get my-s3:my-bucket/backup ./out/     # 下载目录
s3cli mirror my-s3:prod my-s3:backup        # 同步（同端点服务端复制）
cat access.log | s3cli put - my-s3:logs/access.log   # stdin 管道上传
s3cli sql my-s3:my-bucket/data.csv -e "select * from S3Object limit 10"
s3cli share download my-s3:my-bucket/file --expire 24h
```

## Help

```txt
s3cli is a fast, dependency-free CLI for any S3-compatible object storage.

Usage:
  s3cli [flags]
  s3cli [command]

Endpoint Management
  alias       Manage aliases (S3 endpoint configurations)

Bucket Commands
  bucket      Bucket management and configuration

Read Commands
  diff        Compare files/directories between s3 and/or local paths
  du          Show disk usage of bucket or paths
  find        Search objects by name pattern, size and modification time
  info        Show object/bucket metadata as JSON
  ls          List objects or bucket
  stat        Show metadata about bucket or object (mc stat compatible)
  tree        Display objects as a tree of directories

Object Operations
  cp          Copy object within the same S3 endpoint
  get         Download object(s) from S3
  mpu         Manage in-progress multipart uploads
  mv          Move object within the same S3 endpoint
  put         Upload file(s) to S3
  restore     Restore archived objects (Glacier / Deep Archive)
  rm          Delete object(s) from S3
  sql         Run SQL queries against objects (text output only)
  tag         Manage tags for buckets and objects

Synchronization
  mirror      Synchronize objects from source to target (one-way sync)

Tools
  share       Generate URL for temporary access to an object

Additional Commands:
  completion  Generate the autocompletion script for the specified shell
  help        Help about any command

Flags:
  -f, --conf string                Path to configuration file (default ~/.s3cli)
      --debug                      Print summarized S3 requests
  -H, --header stringArray         Add a custom HTTP header (key:value), can repeat
  -h, --help                       help for s3cli
      --host-base string           Override the endpoint host for all aliases
      --lang string                Help language: auto (detect from timezone/locale) | en | zh (default "auto")
      --no-color                   Disable color output
      --no-verify-ssl              Skip TLS certificate verification
      --user-agent string          Override the HTTP User-Agent header
      --user-agent-suffix string   Append extra content to the HTTP User-Agent header
  -v, --version                    version for s3cli
```

> `--json` 与 `--quiet` 是隐藏的全局参数：`--json` 仅在 ls、du、stat、info、find、
> tree、diff、`bucket lifecycle list`、`tag list`、`mpu list`、`mpu local-list` 的 help 中显示；
> `--quiet` 仅在 get/put/cp/mv/mirror（有进度条的命令）的 help 中显示。
> 二者在其它子命令中仍可正常使用（行为不变），只是不在 help 中出现。
> `--show-secret` 仅 `alias list` 子命令可用。

## 退出码

脚本友好：按错误类型返回语义化退出码。

| 退出码 | 含义 |
| --- | --- |
| 0 | 成功 |
| 1 | 通用错误 |
| 4 | 对象/桶不存在（404 / NoSuchKey） |
| 5 | 无权限（403 / AccessDenied） |
| 6 | `diff` 发现差异（非错误，供脚本判断） |
| 130 | 被 SIGINT 中断（Ctrl+C） |

## JSON 输出

`--json` 为支持的命令输出结构化结果（JSON lines 或单文档）。支持的命令：`ls`、
`du`、`stat`、`info`、`find`、`tree`、`bucket lifecycle list`、`tag list`、
`mpu list`、`mpu local-list`、`diff`。其余命令在 `--help` 中标注
`(text output only)`。

## 配置

```bash
s3cli alias help
```

> 路径格式统一为 `别名:桶/路径`，例如 `my-s3:my-bucket/dir/file.txt`。

配置文件（`~/.s3cli`，TOML）：

```toml
[my-s3]
host_base = "https://s3.example.com"
access_key = "AKIA..."
secret_key = "secret"
session_token = ""          # 临时凭证（STS），可省略

# bucket 寻址: "path" (默认) / "dns" / 自定义模板 (含 %(bucket)、可选 %(region))
# %(region) 会探测 bucket 实际区域
bucket_lookup = "path"
# bucket_lookup = "dns"
# bucket_lookup = "https://www.%(bucket).example.com"

# 可选调优
region = "us-east-1"
no_verify_ssl = false
default_mime_type = "application/octet-stream"
multipart_chunk_size_mb = 15
max_retries = 3
tls_min_version = "1.2"     # 1.0 / 1.1 / 1.2 / 1.3，缺省 1.2；老式端点可放宽到 1.0/1.1
```

## S3 后端

底层操作统一抽象为 `pkg/s3iface.S3Operations` 接口，由自建请求客户端 `s3api.Client`
（HTTP + SigV4 签名，零 SDK 依赖，兼容任何 S3 服务）实现。

```bash
./build.sh                # 编译当前平台
./build.sh all            # 全平台交叉编译
go test ./...             # 单元测试（自包含，无需真实 S3）
bash scripts/e2e-minio.sh # MinIO 端到端子命令测试（295 个断言，结束后自动清理）
```

## 许可证

MIT License，详见 [LICENSE](LICENSE)。

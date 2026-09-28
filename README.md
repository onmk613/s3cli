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
- 桶/对象子资源：CORS、生命周期、策略、加密、版本控制、事件通知、标签、**ACL**、
  **Object Lock（桶默认保留策略 + 对象 retention / legal hold）**、**复制**、**公共访问阻断**
- 完整性：`put --checksum`（CRC32/CRC32C/SHA1/SHA256 附加校验和）、`get --checksum`（下载后校验）
- 安全自检：`put`/`get`/`cp`/`mv`/`rm`/`mirror`/`bucket remove` 支持 `--dry-run`
- 单对象分片并行上传（每文件 4 并发分片）
- 结构化 JSON 输出（全局 `--json`）、中英双语帮助、Bash / Zsh / Fish / PowerShell 自动补全

## 安装

```bash
# Go Version 1.27+
git clone https://github.com/onmk613/s3cli.git
cd s3cli && bash build.sh
mv ./bin/s3cli /usr/local/bin/
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
轻量级 S3 命令行客户端。

通过别名（alias）管理多个 S3 兼容存储端的存储桶与对象。

Usage:
  s3cli [flags]
  s3cli [command]

Endpoint Management
  alias       管理别名（S3 endpoint 配置）

Bucket Commands
  bucket      存储桶管理与配置

Read Commands
  diff        比较 S3 与/或本地路径之间的文件/目录差异
  du          显示存储桶或路径的磁盘占用
  find        按名称模式、大小和修改时间搜索对象
  info        以 JSON 展示对象/存储桶元信息
  ls          列出对象或存储桶
  stat        显示存储桶或对象的元信息
  tree        以目录树形式展示对象

Object Operations
  cp          在同一 S3 endpoint 内复制对象
  get         从 S3 下载对象
  mpu         管理进行中的分段上传任务
  mv          在同一 S3 endpoint 内移动对象
  object      管理对象子资源（ACL / 对象锁）
  put         上传文件到 S3
  restore     恢复归档对象（Glacier / Deep Archive）
  rm          删除 S3 对象
  sql         对对象执行 SQL 查询（仅文本输出）
  tag         管理存储桶与对象的标签

Synchronization
  mirror      把对象从源单向同步到目标

Tools
  share       生成对象的临时访问 URL

Additional Commands:
  completion  Generate the autocompletion script for the specified shell
  help        Help about any command

Flags:
  -f, --conf string                配置文件路径
      --debug                      打印精简后的 S3 请求信息
  -H, --header stringArray         添加自定义 HTTP 请求头（key:value），可重复使用
  -h, --help                       help for s3cli
      --host-base string           覆盖所有别名使用的 endpoint 主机地址
      --json                       输出结构化 JSON（仅部分命令支持，其余命令忽略此参数）
      --lang string                帮助语言：auto（按时区/环境自动检测）| en | zh (default "zh")
      --no-color                   禁用彩色输出
      --no-verify-ssl              跳过 TLS 证书校验
      --user-agent string          覆盖 HTTP User-Agent 请求头
      --user-agent-suffix string   向 HTTP User-Agent 请求头追加额外内容
  -v, --version                    version for s3cli

Use "s3cli [command] --help" for more information about a command.
```

> `--json` 是真正的全局参数：支持结构化输出的命令读取它，其余命令静默忽略。
> 因此脚本可以无条件加 `--json`，不必逐命令判断。支持的命令见下节。
> `--show-secret` 仅 `alias list` 子命令可用。
>
> **数值参数上界**：`--concurrency` 允许 1–1024，`--part-size` /
> `multipart_chunk_size_mb` 允许 5–1024 (MB)。超出会直接报错而不是崩溃或
> 尝试分配超大缓冲区（每个在途分片都持有一份完整内存缓冲）。

## 退出码

脚本友好：按错误类型返回语义化退出码。

| 退出码 | 含义 |
| --- | --- |
| 0 | 成功 |
| 1 | 通用错误 |
| 4 | 对象/桶不存在（404 / NoSuchKey / NoSuchBucket / NoSuchUpload） |
| 5 | 无权限（403 / AccessDenied） |
| 6 | `diff` 发现差异（非错误，供脚本判断） |
| 130 | 被 SIGINT 中断（Ctrl+C） |

## JSON 输出

`--json` 是全局参数，为支持的命令输出结构化结果（JSON lines 或单文档）。
支持的命令：`ls`、`du`、`stat`、`find`、`tree`、`diff`、
`bucket lifecycle list`、`bucket acl get`、`object acl get`、`tag list`、
`mpu list`、`mpu local-list`。其余命令继续输出文本（`--json` 被忽略）。

> `info` 不在上表内：它本来就**始终**输出 JSON（兼容旧用法），不读取也不
> 需要 `--json`，加不加该参数行为一致。
>
> 各命令的字段 schema 见 [docs/OUTPUT_SCHEMA.md](docs/OUTPUT_SCHEMA.md)。

`policy get --raw` 是独立的本地参数（打印原始策略 JSON 而非分类结果），
与全局 `--json` 语义不同，故意不复用同名。

## 配置

```bash
s3cli alias help
```

> 路径格式统一为 `别名:桶/路径`，例如 `my-s3:my-bucket/dir/file.txt`。
>
> `host_base` **必须带显式协议前缀**（`https://` 或 `http://`）。漏写会直接报错，
> 不再隐式补 `http://` —— 那会让一次少打几个字符的输入把凭证与对象数据
> 以明文发出。用 `http://`（如局域网 MinIO/Ceph）仍被允许，但会打印告警。

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
multipart_chunk_size_mb = 15  # 分片大小，5–1024
max_retries = 3
tls_min_version = "1.2"     # 1.0 / 1.1 / 1.2 / 1.3，缺省 1.2；老式端点可放宽到 1.0/1.1

# 厂商差异开关（可选，缺省即严格 AWS 语义，仅供对接特定 S3 兼容实现时使用）
# lifecycle_root_element = "LifecycleConfiguration"  # 少数厂商要求 BucketLifecycleConfiguration
# lambda_notification_element = "cloud"              # cloud = <CloudFunctionConfiguration>（默认）/ lambda
# disable_region_redirect = false                    # true = 不按 X-Amz-Bucket-Region 重签重发
# disable_region_probe = false                       # true = %(region) 模板不探测 ?location
# force_unsigned_payload = false                     # true = x-amz-content-sha256: UNSIGNED-PAYLOAD
# xmlns = "http://s3.amazonaws.com/doc/2006-03-01/"  # 覆盖 CORS / 生命周期的命名空间
```

## S3 后端

底层操作统一抽象为 `internal/api` 的 `S3Operations` 接口（`operations.go`），由同包的自建客户端
`api.Client`（HTTP + SigV4 签名，零 SDK 依赖，兼容任何 S3 服务）实现。

对接不同 S3 兼容厂商时，优先用上面的**厂商差异开关**适配，而不是引入另一套 SDK：差异写进
别名配置即可生效，零值保持严格 AWS 语义，升级不会改变既有请求。

```bash
./build.sh       # 编译当前平台（产物在 bin/）
./build.sh all   # 全平台交叉编译
go test ./...    # 测试（自包含，无需真实 S3 服务）
go vet ./...     # 静态检查
```

## 测试命名约定

- 单元测试与被测源文件同名：`bucket-cors.go` → `bucket-cors_test.go`；
- 场景 / 回归测试（跨多个文件的端到端语义锁定）用下划线：`mirror_flow_test.go`、
  `resume_integrity_test.go`。

## 许可证

MIT License，详见 [LICENSE](LICENSE)。

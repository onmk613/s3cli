# s3cli 超详细技术文档

> 本文是 s3cli 项目的完整技术参考，面向希望深入理解架构、功能、实现细节的核心贡献者，也面向第一次接触本项目、希望快速上手的小白开发者。
>
> 阅读建议：
> - **小白开发者**：先读「第一章 项目概览」和「第二章 快速上手」，再按需阅读「第三章 架构总览」；「实现细节」章节可后续查阅。
> - **核心贡献者**：直接从「第三章 架构总览」开始，重点阅读「第五章 核心实现深度剖析」与「第七章 设计权衡与陷阱」。

---

## 目录

- [第一章 项目概览](#第一章-项目概览)
- [第二章 快速上手（小白入门）](#第二章-快速上手小白入门)
- [第三章 架构总览](#第三章-架构总览)
- [第四章 各层职责详解](#第四章-各层职责详解)
- [第五章 核心实现深度剖析](#第五章-核心实现深度剖析)
- [第六章 命令体系全解](#第六章-命令体系全解)
- [第七章 设计权衡与陷阱](#第七章-设计权衡与陷阱)
- [第八章 测试与持续集成](#第八章-测试与持续集成)
- [第九章 构建与发布](#第九章-构建与发布)
- [第十章 术语表与索引](#第十章-术语表与索引)

---

## 修订记录（2026 全面重构）

本轮重构是破坏性内部调整，用户可见行为基本不变，但**目录结构与内部契约有变动**，
阅读下文时请以下列说明为准：

| 变更 | 说明 |
| --- | --- |
| 包结构统一为 `internal/` | 原 `pkg/{api,fmtutil,i18n,kvcache,progress}` 全部移入 `internal/`。本项目交付物是单个 CLI 二进制，不提供可被第三方 import 的库 API。 |
| 新增 `internal/action/render` | 原散落在 action 各文件的**表现层**（JSON 输出、列举表格、树形绘制、S3 路径格式、版本标记、时间格式）集中到此包。它只依赖 `api`/`fmtutil`/`i18n`，不依赖 `action`，因此无 import 环且可脱离 S3 客户端单测。 |
| 统一错误包装 | 全代码库的 S3 错误包装统一用 `%w`；`action.FormatAPIError` 已删除，改为 `*api.ErrorResponse.Error()` 直接返回人类可读的 `"Code: Message"`。**这才是退出码 4/5 能真正生效的前提**（旧实现用 `%s` 包装，类型链断裂，退出码恒为 1）。 |
| 统一 S3 错误判定 | 新增 `api.IsNotFound` / `api.IsAccessDenied` / `api.HasCode` / `api.ErrorOf` / `api.CodeOf`，替换原先 5 处各写一遍的 `errors.As` + 字符串嗅探。 |
| 分页与批处理收敛 | 新增 `forEachObject/forEachObjectPage/forEachVersion/forEachVersionPage`；批量删除收敛为 `deleteObjectsInBatches`（唯一批大小常量 `s3DeleteBatchSize`，且不再只报 `Errors[0]`）。 |
| 分片并行上传 | 新增 `internal/action/multipart-parts.go`：单个对象内部 4 路并发分片。此前是「读一片传一片」的串行循环。 |
| 附加校验和 | 新增 `internal/api/checksum.go`：`put --checksum` / `get --checksum`（CRC32/CRC32C/SHA1/SHA256），并贯通分片上传的每片校验和与 `CompleteMultipartUpload` 回传。 |
| 全局 `--json` | `--json` 由 12 处逐命令声明改为根命令的持久 flag；支持的命令读 `config.G.F.JSON`，其余静默忽略。`policy get --json` 因语义不同改名为 `--raw`。**注意**：改持久 flag 后，每个命令都必须在自己的 `RunE` 里显式 `opt.JSON = config.G.F.JSON`；`find` / `diff` / `tag list` 曾漏掉这一句，导致 action 层的 JSON 分支永远走不到（`cmd/json_wiring_test.go` 现已锁定）。 |
| 新增子资源命令 | `bucket acl`、`bucket object-lock`、`bucket replication`、`bucket public-access-block`、`object acl`、`object retention`、`object legal-hold` —— 这些后端能力此前已实现却无 CLI 入口。 |
| 删除 e2e | `scripts/e2e-minio.sh` 与 CI 的 `e2e` job 已移除（MinIO 归档开源版并停止分发社区二进制，所有下载路径统一 `410 Gone`）。CI 工作流由 `e2e.yml` 更名为 `ci.yml`，只保留单元测试 / 静态检查 / 交叉编译三个 job。 |
| Go 1.27 | `go.mod` 升到 `go 1.27`；CI 与 release 工作流的 `go-version` 同步为 `1.27`。所有直接依赖经 `go get -u ./...` + `go mod tidy` 核对，均已是最新版。 |

---

## 第一章 项目概览

### 1.1 这是什么

**s3cli** 是一个轻量、高性能、零 AWS SDK 依赖的 S3 命令行客户端，使用 Go 编写。

它兼容任何 S3 协议的对象存储（AWS S3、MinIO、Ceph RGW、阿里云 OSS、腾讯云 COS、华为 OBS、Backblaze B2 等），底层只依赖 Go 标准库 `net/http`，自行实现：

- **S3 REST API 客户端**（`internal/api`）：纯 HTTP 请求，零 SDK 依赖
- **AWS Signature V4 签名**（`internal/api/signer.go`）：纯标准库 HMAC-SHA256
- **断点续传的分片上传**（`internal/action/multipart-*.go`）：本地状态 + 服务端对账
- **流式镜像同步**（`internal/action/object-mirror.go`）：双端有序归并，内存恒定

### 1.2 核心特性一览

| 特性 | 说明 |
| --- | --- |
| 零 SDK 依赖 | 基于 `net/http` 自实现 S3 REST API 与 SigV4 签名 |
| 多别名管理 | 一个配置文件管理多个 S3 端点（`~/.s3cli`，TOML 格式） |
| 三种桶寻址 | Path-style / DNS virtual-host / 自定义模板（含区域探测） |
| 断点续传 | 本地状态文件 + 服务端 `ListParts` 对账式续传 |
| 流式传输 | 上传/下载/复制/移动，支持递归整目录；stdin 管道上传、stdout 流式输出 |
| 实时进度条 | 速率 / ETA / 终端宽度自适应 |
| 桶配置管理 | CORS、生命周期、策略、加密、版本控制、事件通知、标签、ACL、Object Lock、复制 |
| 镜像同步 | 同端零拷贝（服务端 CopyObject）/ 跨端流式，manifest 断点续传 |
| 文件差异对比 | size / quick / md5 三种模式 |
| 结构化输出 | `--json` 对受支持命令输出结构化 JSON |
| 中英双语 | `--lang auto\|en\|zh`，auto 按时区/locale 自动检测 |
| Shell 补全 | Bash / Zsh / Fish / PowerShell |
| 脚本友好 | 语义化退出码（0/1/4/5/6/130） |

### 1.3 项目规模

- **代码量**：约 22,300 行 Go 代码（不含测试），约 13,900 行测试代码
- **Go 版本**：1.27（go.mod 声明 `go 1.27`，与 CI 的 `go-version: "1.27"` 一致）
- **外部依赖**：仅 6 个直接依赖（cobra、pflag、BurntSushi/toml、mattn/go-runewidth、golang.org/x/{sys,term}），极其精简

### 1.4 目录结构总览

```
s3cli/
├── main.go                  # 入口，仅调用 cmd.NewRootCmd()
├── build.sh                 # 构建脚本（单平台 / 全平台交叉编译）
├── go.mod / go.sum
├── README.md
│
├── cmd/                     # 【命令层】cobra 命令定义 + RunE 工厂
│   ├── root.go              #   根命令、全局 flag、注册表、退出码
│   ├── common.go            #   RunE 工厂（NewRunE / NewRunEWithMode ...）
│   ├── error.go             #   错误展示、语义化退出码映射
│   ├── alias.go             #   alias 命令组
│   ├── ls.go                #   ls / du / stat / info
│   ├── transfer.go          #   get / put / rm / restore
│   ├── mirror.go            #   mirror
│   ├── diff.go              #   diff
│   ├── bucket.go            #   bucket 命令组入口
│   ├── cors.go / lifecycle.go / policy.go / encryption.go
│   ├── versioning.go / event.go / tag.go
│   ├── mpu.go / signurl.go / sql.go / find.go / completion.go
│   └── *_test.go            #   命令层测试
│
├── internal/                # 【内部层】不对外暴露
│   ├── config/              #   配置管理（TOML 读写、别名 CRUD）
│   │   ├── config.go        #     Config / Static / Flags 结构、默认值
│   │   ├── readconf.go      #     读取配置文件
│   │   ├── saveconf.go      #     保存配置文件
│   │   ├── setconf.go       #   alias set / edit（交互式）
│   │   ├── listconf.go      #   alias list
│   │   └── delconf.go       #   alias del
│   │
│   ├── s3path/              #   S3 路径解析 "alias:bucket/key"
│   │   └── s3path.go
│   │
│   ├── client/              #   客户端构造与缓存
│   │   ├── client.go        #     newS3Client（HTTP Transport、TLS、User-Agent）
│   │   ├── parse-path.go    #     ParsePathAndNewClient + 客户端缓存
│   │   ├── lookup.go        #     CustomBucketLookup（自定义模板寻址）
│   │   ├── header.go        #     自定义 HTTP header 注入 RoundTripper
│   │   ├── user-agent.go    #     User-Agent 改写 RoundTripper
│   │   └── dump.go          #     --debug 请求 dump RoundTripper
│   │
│   └── action/              #   【业务逻辑层】面向命令的原子操作
│       ├── common.go        #     Action 核心、存在性/目录探测、对象遍历
│       ├── utils.go         #     IsCanceled / FormatBytes / MIME 注册
│       ├── stream.go        #     RunStream 流式传输框架
│       ├── multipart-*.go   #     分片上传与断点续传
│       ├── object-*.go      #     对象级操作（put/get/cp/mv/rm/ls/...）
│       ├── bucket-*.go      #     桶级操作（make/remove/cors/lifecycle/...）
│       ├── object-mirror.go #     mirror 主流程
│       ├── mirror-stream.go #     流式列举 + 归并差异
│       ├── mirror-copy.go   #     同端/跨端复制 + 批量删除
│       ├── mirror-manifest.go #   manifest 断点续传
│       ├── diff.go          #     文件差异对比
│       ├── share.go / info.go / stat.go / ...
│       └── *_test.go
│
├── internal/                # 【内部实现层】全部包均不对外暴露
│   ├── api/                 #   S3 客户端：契约 + 实现 + 厂商差异开关（单一包）
│   │   ├── api.go           #     Client / Options / New / Do（请求生命周期）
│   │   ├── operations.go    #     S3Operations 接口 + 分页器接口（上层契约）
│   │   ├── types.go         #     操作级 DTO（列举/对象/分片/预签名/错误）
│   │   ├── bucket-types.go  #     桶子资源 DTO（CORS/加密/生命周期/通知/...）
│   │   ├── object-select-types.go # S3 Select DTO
│   │   ├── quirks.go        #     厂商差异开关（零值 = 严格 AWS 语义）
│   │   ├── signer.go        #     AWS SigV4 签名
│   │   ├── bucket-lookup.go #     三种桶寻址 + 区域探测
│   │   ├── error.go         #     S3 错误响应解析
│   │   ├── utils.go         #     哈希、编码、XML 辅助
│   │   ├── list.go          #     ListObjectsV2 / Versions + 分页器
│   │   ├── object-*.go      #     对象 CRUD
│   │   ├── bucket-*.go      #     桶子资源 CRUD
│   │   ├── multipart-upload.go  # 分片上传 API
│   │   └── presigned.go     #     预签名 URL
│   │
│   ├── fmtutil/             #   格式化输出（表格、颜色、字节单位）
│   ├── i18n/                #   国际化（中英双语）
│   ├── progress/            #   进度条（终端宽度自适应、ETA）
│   └── kvcache/             #   泛型 KV 缓存（客户端缓存、区域缓存）
│
└── docs/                    #   本技术文档

```

---

## 第二章 快速上手（小白入门）

### 2.1 环境准备

```bash
# 需要 Go 1.27+
go version
```

### 2.2 编译安装

```bash
git clone https://github.com/onmk613/s3cli.git
cd s3cli
bash build.sh            # 编译当前平台，生成 ./s3cli
# 或
bash build.sh all        # 全平台交叉编译，输出到 bin/

# 移动到 PATH
mv ./s3cli /usr/local/bin/
s3cli --version
```

`build.sh` 会通过 `-ldflags` 把版本号、Git commit、构建时间注入到二进制（见 `cmd/root.go` 的 `Version/Commit/BuildDate/GoVersion` 变量）。

### 2.3 配置第一个端点

s3cli 用「别名（alias）」管理多个 S3 端点。配置文件默认在 `~/.s3cli`（TOML 格式）。

**方式一：交互式配置（推荐新手）**

```bash
s3cli alias set my-s3
# 按提示输入 URL、AccessKey、SecretKey 等
```

**方式二：一行命令配置**

```bash
s3cli alias set my-s3 https://s3.example.com AKIA... SECRET
```

**方式三：手动编辑配置文件**

```toml
# ~/.s3cli
[my-s3]
host_base = "https://s3.example.com"
access_key = "AKIA..."
secret_key = "secret"
session_token = ""              # 临时凭证（STS），可省略
bucket_lookup = "path"          # path(默认) / dns / 自定义模板
region = "us-east-1"
multipart_chunk_size_mb = 15
max_retries = 3
tls_min_version = "1.2"
```

查看已配置的别名：

```bash
s3cli alias list
s3cli alias list my-s3          # 查看单个别名的详细配置
s3cli alias list -s             # 显示完整 secret key（默认脱敏）
```

### 2.4 路径格式

s3cli 的 S3 路径统一为 **`别名:桶/路径`**：

```
my-s3:                         # 仅别名 → 列出所有桶
my-s3:my-bucket                # 别名:桶 → 列出桶内对象（顶层）
my-s3:my-bucket/               # 尾斜杠 → 目录语义
my-s3:my-bucket/dir/file.txt   # 完整对象路径
```

> ⚠️ 别名和桶之间用**冒号** `:` 分隔，因此别名和桶名都不允许包含冒号。

### 2.5 最常用操作

```bash
# 列出
s3cli ls my-s3:                         # 列出所有桶
s3cli ls my-s3:my-bucket/               # 列出桶内对象（顶层）
s3cli ls my-s3:my-bucket/ -r            # 递归列出所有对象
s3cli ls my-s3:my-bucket/ --summarize   # 带汇总（对象数、总大小）

# 上传
s3cli put ./file.txt my-s3:my-bucket/          # 上传单文件
s3cli put ./data my-s3:my-bucket/backup/ -r    # 递归上传目录
cat access.log | s3cli put - my-s3:logs/access.log  # stdin 管道上传

# 下载
s3cli get my-s3:my-bucket/file.txt ./          # 下载单文件
s3cli get my-s3:my-bucket/backup ./out/ -r     # 递归下载目录
s3cli get my-s3:my-bucket/file -               # 输出到 stdout（替代 cat）

# 删除
s3cli rm my-s3:my-bucket/file.txt
s3cli rm my-s3:my-bucket/old-data/ -r --force  # 递归删除（需 --force 确认）

# 复制 / 移动（同 endpoint 内）
s3cli cp my-s3:bucket/a.txt my-s3:bucket/b.txt
s3cli mv my-s3:bucket/a.txt my-s3:bucket/b.txt

# 镜像同步（可跨 endpoint）
s3cli mirror my-s3:prod my-s3:backup           # 同端点（服务端零拷贝）
s3cli mirror prod:bucket backup:bucket --remove # 删除目标多余对象
```

### 2.6 退出码（脚本友好）

| 退出码 | 含义 |
| --- | --- |
| 0 | 成功 |
| 1 | 通用错误 |
| 4 | 对象/桶不存在（404 / NoSuchKey） |
| 5 | 无权限（403 / AccessDenied） |
| 6 | `diff` 发现差异（**非错误**，供脚本判断） |
| 130 | 被 SIGINT 中断（Ctrl+C） |

```bash
s3cli diff my-s3:bucket/file ./local-file
case $? in
  0) echo "完全一致" ;;
  6) echo "存在差异" ;;
  *) echo "发生错误" ;;
esac
```

### 2.7 开发工作流

```bash
# 测试（自包含，无需真实 S3 服务，也不下载任何二进制）
go test ./...

# 带竞态检测（CI 跑的就是这个）
go test -race ./...

# 覆盖率
go test -cover ./...

# 格式与静态检查
gofmt -l main.go cmd internal
go vet ./...
golangci-lint run ./...
govulncheck ./...

# 全平台交叉编译
bash build.sh all
```

---

## 第三章 架构总览

### 3.1 分层架构

s3cli 采用严格的**四层分层架构**，依赖方向自上而下单向流动：

```
┌─────────────────────────────────────────────────────────┐
│  main.go  →  cmd.NewRootCmd()                           │  入口
├─────────────────────────────────────────────────────────┤
│  cmd/          命令层（CLI 框架、参数解析、RunE 工厂）     │  第 1 层
│   │  依赖: cobra/pflag, action, config, i18n             │
│   ↓  不直接构造 api.Client                                │
├─────────────────────────────────────────────────────────┤
│  internal/action/   业务逻辑层（面向命令的原子操作）       │  第 2 层
│   │  依赖: api.S3Operations 接口（不感知具体实现）         │
│   ↓  通过 client 包注入实现                               │
├─────────────────────────────────────────────────────────┤
│  internal/client/   客户端构造层（组装 api.Client）        │  第 3 层
│   │  依赖: api, config                                    │
│   ↓  返回 api.S3Operations；别名配置 → api.Quirks          │
├─────────────────────────────────────────────────────────┤
│  internal/api/         S3 客户端：契约 + 实现 + 厂商差异开关    │  第 4 层
│   │  operations.go 定义 S3Operations（契约）              │
│   │  Client 实现该契约（HTTP + SigV4，纯标准库）           │
│   │  quirks.go 承载厂商差异（零值 = 严格 AWS 语义）         │
└─────────────────────────────────────────────────────────┘

横向支撑库:
  internal/fmtutil        表格/颜色/字节单位
  internal/i18n           中英双语
  internal/progress       进度条
  internal/kvcache        泛型 KV 缓存
  internal/config         配置管理
  internal/s3path         路径解析
  internal/action/render  表现层（JSON/表格/树/S3 路径格式）
```

**关键设计原则**：

1. **接口分层而非分包分层**：`action` 层面向 `api.S3Operations` 接口编程，接口与默认实现（`api.Client`）同包。这样做的收益是**测试替身**与**装配点收窄**（全进程只有 `internal/client` 构造具体客户端），而不是"将来换后端"——真出现第二个后端时，加逻辑到现有 `api` 通常比引入另一套 SDK 更划算（厂商差异只能自己写 shim，SDK 并不代劳）。
2. **厂商差异走配置而非代码**：`api.Quirks`（`internal/api/quirks.go`）把已知的厂商分歧（生命周期根元素、Lambda 通知元素命名、region 重定向/探测、payload 哈希、XMLNS）收敛为开关，经别名配置直通；零值严格等于 AWS 语义，因此新增开关不会改变既有行为。
3. **内部分层**：全部实现包统一放在 `internal/` 下，保证对外 API 面为零——本项目交付物是单个 CLI 二进制，不以可被第三方 import 的库为目标。
4. **表现与逻辑分离**：`internal/action/render` 只做"怎么显示"，`action` 只做"做什么"。渲染包不依赖 `action`（因此不会成环），需要 S3 类型的地方接收最小视图结构（`render.LsRow` / `render.TreeNode`），而不是把 `api.ObjectInfo` 搬进去。这一刀之所以值得切，是因为它让渲染可脱离 S3 客户端单测，而按业务域拆 `action` 只会产生互相 import 的子包。

### 3.2 一次命令的完整生命周期

以 `s3cli ls my-s3:my-bucket/` 为例，追踪从敲下回车到输出结果的完整流程：

```
用户输入: s3cli ls my-s3:my-bucket/
    │
    ▼
① main.go: cmd.NewRootCmd()
    │
    ▼
② cmd/root.go: NewRootCmd()
   - resolveLangPref(os.Args) → 解析 --lang（在 cobra 解析前）
   - i18n.Resolve(langPref)   → 全局设置语言
   - signal.NotifyContext()   → 注册 SIGINT/SIGTERM → 可取消 ctx
   - 构造 rootCmd，注册全局 flag（--conf/--debug/--no-color/...）
   - 遍历 cmdRegistry，AddGroup + AddCommand（带重名校验）
   - rootCmd.Execute()
    │
    ▼
③ cobra 解析 flag + 路由到 ls 命令
   - PersistentPreRunE:
     · bindEnv()        → 环境变量 CLI_* 绑定到 flag
     · shouldSkipConfLoad? → 否
     · config.LoadConf()  → 读取 ~/.s3cli 到 config.G.S
    │
    ▼
④ cmd/ls.go 的 RunE（由 NewRunEAllowAliasOnly 工厂生成）
   - splitArgs() → 参数解析（ls 允许仅别名）
   - 对每个 S3 路径参数:
     · parseClient(arg)
       - client.ParsePathAndNewClient(arg)
         · s3path.Parse("my-s3:my-bucket/")
           → &Path{Alias:"my-s3", Bucket:"my-bucket", Key:"", TrailingSlash:true}
         · config.G.S["my-s3"] 查找别名配置 → Static
         · client.NewClient("my-s3", static)
           · 命中 S3Clients 缓存? → 返回缓存
           · 否则 newS3Client(static, flags)
             · 构造 http.Transport（TLS、连接池、超时）
             · 套 RoundTripper 链：dump → user-agent → header → transport
             · 解析 bucket_lookup → BucketLookupType + CustomBucketLookup
             · api.New(opts) → *api.Client
           · 缓存到 S3Clients
       - 返回 action.Action{S3: client, Alias: "my-s3", Ctx: ctx}
     · 调用 fn(S3, sp) → S3.ListObjects(opt, "my-bucket", "")
    │
    ▼
⑤ internal/action/object-list.go: ListObjects()
   - 校验、构造 ListOptions
   - ListObjectsV2Paginator 分页循环:
     · paginator.NextPage(ctx)
       - api.Client.ListObjectsV2()
         · 构造 query: list-type=2&prefix=...&delimiter=/
         · c.Do(ctx, GET, meta)
    │
    ▼
⑥ internal/api/api.go: Client.Do()
   - 检测 body 是否可 Seek（用于重试回卷）
   - 查询 bucketLocCache 获取 bucket region
   - 重试循环（attempt = 0..maxRetries）:
     · newRequest(ctx, method, meta, signingRegion)
       - resolveURL() → 三种寻址之一生成 URL
       - 设置 header（X-Amz-Content-Sha256、X-Amz-Security-Token）
       - signV4() → SigV4 签名，写入 Authorization
     · httpClient.Do(req)
     · region 重定向（301/307/400 + X-Amz-Bucket-Region）?
       → 更新 signingRegion + 写缓存 + 重签重发（不消耗重试预算）
     · 2xx → 返回 resp
     · 5xx/429/SlowDown → 退避重试
     · 4xx → 解析错误，返回 *ErrorResponse
    │
    ▼
⑦ 回到 ListObjectsV2: xmlDecoder 解析响应
   - 返回 *ListObjectsV2Output{Contents: [...], CommonPrefixes: [...]}
    │
    ▼
⑧ 回到 action.ListObjects: 格式化输出
   - --json? → printJSONLine()
   - 否则 → fmtutil.Table 渲染（终端宽度自适应、CJK 对齐）
    │
    ▼
⑨ cmd/root.go: Execute() 返回
   - err == nil → os.Exit(0)
   - err != nil → exitCodeForError(err) → os.Exit(code)
```

### 3.3 关键数据流：依赖注入

`api.S3Operations` 接口是整个架构的**核心契约**。它的注入路径：

```
cmd/common.go: parseClient()
    │
    ├── client.ParsePathAndNewClient(arg)
    │       │
    │       ├── s3path.Parse(arg) → *s3path.Path
    │       ├── config.G.S[alias] → config.Static
    │       └── client.NewClient(alias, static)
    │               │
    │               ├── (缓存命中) → 返回缓存的 api.S3Operations
    │               └── (缓存未命中) → newBackendClient(static, flags)
    │                       │
    │                       └── newS3Client(static, flags) → api.New(opts)
    │                               │
    │                               └── *api.Client（实现 api.S3Operations）
    │
    └── 返回 action.Action{S3: <api.Client>, Alias, Ctx}
            │
            └── action 层的所有方法通过 Action.S3 调用底层操作
```

`action.Action` 结构体是业务逻辑层的核心载体：

```go
type Action struct {
    S3    api.S3Operations  // 底层 S3 操作接口（api.Client 实现）
    Alias string                // 别名（用于日志显示）
    Ctx   context.Context       // 上下文（可取消）
}
```

这种设计的好处：**全进程只有 `internal/client` 一个地方构造具体客户端**，action 层面向
`api.S3Operations` 接口编程，测试可以注入替身（HTTP 层替身见 `internal/action/backend_switch_mock_test.go`），
完全不依赖真实 S3。

---

## 第四章 各层职责详解

### 4.1 命令层（`cmd/`）

#### 4.1.1 职责

- 定义所有 CLI 命令（cobra Command）
- 解析命令行参数（flag）
- 把参数转换成对 `action` 层的调用
- 统一错误展示与退出码

**命令层不做业务逻辑**，它只是「参数 → action 调用」的薄封装。

#### 4.1.2 命令注册机制

s3cli 不在 `NewRootCmd` 里硬编码子命令列表，而是用**注册表模式**：每个命令文件在 `init()` 中调用 `Register()` 把自己注册到 `cmdRegistry`。

```go
// cmd/ls.go
func init() {
    Register("read", "Read Commands", NewLsCmd)
    Register("read", "Read Commands", NewDuCmd)
    // ...
}
```

```go
// cmd/root.go
func Register(groupID, title string, fn func() *cobra.Command) {
    registryMu.Lock()
    defer registryMu.Unlock()
    for i := range cmdRegistry {
        if cmdRegistry[i].ID == groupID {
            cmdRegistry[i].Commands = append(cmdRegistry[i].Commands, fn)
            return
        }
    }
    cmdRegistry = append(cmdRegistry, cmdGroup{ID: groupID, Title: title, Commands: []func() *cobra.Command{fn}})
}
```

`NewRootCmd` 遍历注册表，按 `GroupID` 分组显示，并做**顶层命令名/别名冲突检测**（fail-fast，防止 `rm` 被误路由到删桶命令这种危险情况）。

命令分组（与 `s3cli help` 输出一致）：

| GroupID | Title | 命令 |
| --- | --- | --- |
| `alias` | Endpoint Management | alias |
| `bucket` | Bucket Commands | bucket |
| `read` | Read Commands | ls, du, stat, info, find, tree, diff |
| `object` | Object Operations | get, put, cp, mv, rm, restore, sql, tag, mpu |
| `sync` | Synchronization | mirror |
| `tools` | Tools | share |

#### 4.1.3 RunE 工厂（核心抽象）

命令层最精妙的设计是 **RunE 工厂模式**（`cmd/common.go`）。不同命令的参数模式不同：

- `ls`/`rm`/`stat`：所有参数都是 S3 路径
- `put`：第一个参数是本地文件，第二个是 S3 路径
- `get`：第一个是 S3 路径，最后一个可能是本地目录
- `cp`/`mv`/`mirror`：两个 S3 路径
- `diff`：两个参数，各自可能是本地或 S3

为此，`common.go` 提供了一组工厂函数，封装「参数解析 → 客户端构造 → action 调用 → 错误处理」的通用流程：

| 工厂 | 用途 | 示例命令 |
| --- | --- | --- |
| `NewRunE(fn)` | 所有参数都是 S3 路径 | `ls`, `rm`, `stat` |
| `NewRunEWithMode(fn)` | 有一个参数不是 S3 路径 | `put`, `get` |
| `NewRunEAllowAliasOnly(fn)` | 允许仅别名参数 | `ls`（列出所有桶） |
| `NewRunETwoPaths(fn)` | 两个 S3 路径 | `cp`, `mv` |
| `NewRunEMixedPair(fn)` | 两个参数各自可能是本地/S3 | `diff` |
| `NewRunELocal(fn)` | 不解析 S3 路径的本地命令 | `mpu local-list` |

参数解析模式通过 `cmd.Annotations[AnnoArgParseMode]` 标注：

```go
// cmd/common.go
const (
    AnnoArgParseMode = "ArgParseMode"
    OnlyS3Path           // 所有参数都是 S3 路径
    FirstLocalFileOrPath // 首参是本地路径（put）
    LastLocalFileOrPath  // 末参是本地路径（get）
)
```

```go
// cmd/transfer.go: get 命令
cmd := &cobra.Command{
    Use:         "get [alias:bucket/path] [local-path]",
    Annotations: LastLocalFileOrPathMode,  // 标注：末参是本地路径
    RunE: NewRunEWithMode(func(S3 action.Action, dst *s3path.Path, opts ArgParseMode) error {
        return S3.GetObject(getOpt, dst.Bucket, dst.Key, opts[LocalFileOrPath])
    }),
}
```

#### 4.1.4 错误处理与退出码

命令层用**双重 `%w` 包装**技巧同时实现两个目标：

```go
// cmd/common.go
func wrapErrs(errs []error) error {
    if len(errs) == 0 {
        return nil
    }
    return fmt.Errorf("%w: %w", errAlreadyDisplayed, errs[0])
}
```

- `errors.Is(err, errAlreadyDisplayed)` → 抑制上层重复打印（错误已在 RunE 内通过 `displayError` 输出）
- `errors.As(err, &apiErr)` → 穿透到原始 `*api.ErrorResponse`，供 `exitCodeForError` 还原语义化退出码

```go
// cmd/error.go
func exitCodeForError(err error) int {
    if action.IsCanceled(err) {
        return exitCanceled  // 130
    }
    if apiErr, ok := errors.AsType[*api.ErrorResponse](err); ok {
        switch {
        case apiErr.StatusCode == 404 || strings.Contains(apiErr.Code, "NoSuch"):
            return exitNotFound  // 4
        case apiErr.StatusCode == 403 || strings.Contains(apiErr.Code, "AccessDenied"):
            return exitForbidden  // 5
        }
    }
    if action.IsDifferErr(err) {
        return exitDiffer  // 6
    }
    return exitGeneric  // 1
}
```

#### 4.1.5 全局 Flag 与环境变量

全局 Flag 定义在 `cmd/root.go`，绑定到 `config.G.F`（`config.Flags`）：

| Flag | 环境变量 | 作用 |
| --- | --- | --- |
| `-f, --conf` | `CLI_CONF` | 配置文件路径（默认 `~/.s3cli`） |
| `--debug` | `CLI_DEBUG` | 打印精简后的 S3 请求 |
| `--no-color` | `CLI_NO_COLOR` | 禁用彩色输出 |
| `--host-base` | `CLI_HOST_BASE` | 覆盖所有别名的 endpoint host |
| `--no-verify-ssl` | `CLI_NO_VERIFY_SSL` | 跳过 TLS 证书校验 |
| `--user-agent` | `CLI_USER_AGENT` | 覆盖 User-Agent |
| `--user-agent-suffix` | `CLI_USER_AGENT_SUFFIX` | 追加 User-Agent |
| `-H, --header` | `CLI_HEADER` | 自定义 HTTP header（可重复） |
| `--lang` | `CLI_LANG` | 帮助语言：auto/en/zh |

**优先级**：命令行 > 环境变量 > 默认值（由 `bindEnv` 实现）。

隐藏 flag（`--json`/`--quiet`）：这两个 flag 是全局的，但只在相关命令的 help 中显示，避免污染其他命令的帮助输出。详见 README 的说明。

### 4.2 业务逻辑层（`internal/action/`）

#### 4.2.1 职责

- 在 `api.S3Operations` 之上组织面向命令的原子操作
- 处理多对象递归、并发、进度跟踪
- 大文件分片上传与断点续传
- 镜像同步、差异对比

#### 4.2.2 Action 核心结构

```go
// internal/action/common.go
type Action struct {
    S3    api.S3Operations
    Alias string
    Ctx   context.Context
}
```

`Action` 的方法对应一个完整的命令语义，例如：

- `ListObjects(opt, bucket, key)` ← `ls`
- `PutObject(opt, bucket, prefix, localPath, isS3Dir)` ← `put`
- `GetObject(opt, bucket, key, localPath)` ← `get`
- `DeleteObjects(bucket, key, opt)` ← `rm`
- `Mirror(cfg)` ← `mirror`（注意：`Mirror` 是包级函数，不是方法，因为它需要两个 Action）

#### 4.2.3 关键辅助方法

```go
// 判断 S3 路径是文件还是目录（或不存在）
func (c *Action) IsS3File(bucket, key string) (bool, error)

// 判断目标 key 当前状态：文件/目录/不存在（用于 cp/mv/mirror 计算目标 key）
func (c *Action) DestStateOf(bucket, key string) (s3path.DestState, error)

// 遍历 bucket/prefix 下所有对象（自动翻页）
func (c *Action) forEachObject(ctx, bucket, prefix, fn) error
```

这些方法统一处理了「S3 没有真正的目录」这一核心复杂性：通过 `HeadObject` + `ListObjectsV2(prefix=key/, delimiter=/, maxKeys=1)` 探测来区分文件/目录/不存在。

### 4.3 客户端构造层（`internal/client/`）

#### 4.3.1 职责

- 把 `config.Static`（别名配置）+ `config.Flags`（全局开关）组装成 `*api.Client`
- 管理客户端缓存（同一进程内同一别名复用）
- 实现 RoundTripper 链（debug dump / user-agent / header 注入）

#### 4.3.2 客户端缓存

```go
// internal/client/parse-path.go
var S3Clients = &kvcache.Cache[string, cachedBackend]{}

func NewClient(alias string, static config.Static) (api.S3Operations, error) {
    if cached, ok := S3Clients.Get(alias); ok && reflect.DeepEqual(cached.static, static) {
        return cached.client, nil  // 配置未变，命中缓存
    }
    s3Client, err := newBackendClient(static, config.G.F)
    // ...
    S3Clients.Set(alias, cachedBackend{client: s3Client, static: static})
    return s3Client, nil
}
```

缓存会比对静态配置（`reflect.DeepEqual`），如果同一进程内别名配置发生变化（例如未来的常驻模式），会自动重建客户端，不会返回陈旧凭证。

#### 4.3.3 HTTP Transport 构造

`newS3Client`（`internal/client/client.go`）构造 HTTP Transport 的关键点：

```go
transport := &http.Transport{
    TLSClientConfig:       &tls.Config{MinVersion: minTLS, InsecureSkipVerify: cfg.NoVerifySSL},
    DialContext:           (&net.Dialer{Timeout: 10s, KeepAlive: 30s}).DialContext,
    TLSHandshakeTimeout:   10 * time.Second,
    ResponseHeaderTimeout: 30 * time.Second,
    IdleConnTimeout:       90 * time.Second,
    MaxIdleConns:          100,
    MaxIdleConnsPerHost:   10,
}
```

然后按需套上 RoundTripper 链（**顺序很重要**，外层先执行）：

```
请求流出方向 →

[header 注入] → [user-agent 改写] → [debug dump] → [实际 transport]
   最外层                                    最内层

放最外层的理由：让 --debug 看到的是最终请求头（user-agent 和 header 已改写）。
```

- `--header`：`newHeaderTransport`（注入自定义 header）
- `--user-agent`/`--user-agent-suffix`：`newUserAgentTransport`
- `--debug`：`newDumper`（打印精简后的请求摘要）

### 4.4 S3 API 客户端（`internal/api/`）

这是整个项目的技术核心，详见「第五章 核心实现深度剖析」。

### 4.5 契约层与厂商差异开关（`internal/api/`）

#### 4.5.1 职责

`internal/api` 是**单一包**，同时承载三件事：

1. **契约**（`operations.go`）：`S3Operations` 接口与两个分页器接口；
2. **数据类型**（`types.go` / `bucket-types.go` / `object-select-types.go`）：各操作的输入输出 DTO；
3. **实现**（`api.go` / `signer.go` / `bucket-*.go` / `object-*.go` / ...）：HTTP + SigV4 的具体实现，以及**厂商差异开关**（`quirks.go`）。

> 📌 历史沿革：接口与 DTO 曾经独立为 `internal/s3iface` 包（依赖倒置 + "将来可换 AWS SDK"）。实际落地后，接口始终只有一个实现、且每加一个 DTO 都要在两处各起一次名字（一个 110 行的纯别名文件），维护成本每天都在付而抽象收益是纸面的。因此已合并回 `internal/api`：**接口保留**（用于测试替身与装配点收窄），**独立包取消**。

#### 4.5.2 S3Operations 接口

`operations.go` 定义了 `S3Operations` 接口，涵盖：

- **桶基础操作**：ListBuckets / CreateBucket / DeleteBucket / GetBucketLocation
- **桶子资源**：CORS / 加密 / 生命周期 / 事件通知 / 标签 / 版本控制 / 公共访问阻断 / Object Lock / 复制 / 策略 / ACL
- **对象操作**：列举 / 元数据 / 下载 / 上传 / 复制 / 删除 / 标签 / S3 Select / 归档恢复
- **分片上传**：Create / UploadPart / UploadPartCopy / Complete / Abort / List
- **预签名 URL**：PresignedURL（V4）/ PresignV2
- **分页器工厂**：NewListObjectsV2Paginator / NewListObjectVersionsPaginator
- **客户端元数据**：AccessKey / SecretKey / SessionToken / Endpoint（用于 mirror/diff 判断同 endpoint）

> 💡 这组客户端元数据方法（`AccessKey()` 等）不是标准 S3 操作，而是为了 `sameEndpoint()` 判断服务，属于接口的「实用扩展」。

接口满足性由 `operations.go` 末尾的编译期断言 `var _ S3Operations = (*Client)(nil)` 保证：任何方法签名漂移都会在 `internal/api` 内直接报错，而不是等到上层装配点才暴露。

#### 4.5.3 Quirks：厂商差异的落点

不同 S3 兼容实现在线协议细节上有分歧。这些分歧**不写死在 66 个操作方法里**，而是收敛到 `quirks.go` 的一个结构体，每个开关只有一个落点，并经别名配置直通：

```go
// internal/api/quirks.go（节选）
type Quirks struct {
    LifecycleRootElement      string // 落点: SetBucketLifecycle -> marshalLifecycleXML
    LambdaNotificationElement string // 落点: SetBucketNotification -> marshalNotificationXML
    DisableRegionRedirect     bool   // 落点: Do 的 region 重定向分支
    DisableRegionProbe        bool   // 落点: resolveBucketRegion
    ForceUnsignedPayload      bool   // 落点: newRequest 的 x-amz-content-sha256
    XMLNS                     string // 落点: marshalCorsXML / marshalLifecycleXML
}
```

对应别名配置键（`~/.s3cli`）：

| 配置键 | 取值 | 缺省（严格 AWS 语义） |
|---|---|---|
| `lifecycle_root_element` | 元素名 | `LifecycleConfiguration` |
| `lambda_notification_element` | `cloud` / `lambda` | `cloud` |
| `disable_region_redirect` | true/false | false（按 `X-Amz-Bucket-Region` 重签重发） |
| `disable_region_probe` | true/false | false（`%(region)` 模板会探测 `?location`） |
| `force_unsigned_payload` | true/false | false（真实 SHA256） |
| `xmlns` | 命名空间 | `http://s3.amazonaws.com/doc/2006-03-01/` |

约定（新增厂商差异时请沿用）：

- **零值 = 严格语义**，任何开关不设置都不会改变发出的请求，升级不会悄悄改变线上行为；
- **一个开关一个落点**，新增差异 = 加一个字段 + 落点加一个判断，不散落到多个操作文件；
- **读取方向尽量无条件放宽**（如 `NotificationConfiguration.UnmarshalXML` 始终同时接受 `CloudFunctionConfiguration` 与 `LambdaFunctionConfiguration` 两种命名），因为多接受一种输入不改变对外行为。

不需要新开关的既有逃生口：寻址方式（`Options.BucketLookup` / `BucketLookupViaURL` 自定义模板）、TLS 版本与校验（`Transport`，由 `internal/client` 按别名配置构造）、重试次数（`MaxRetries`）、预签名版本（`PresignedURL` / `PresignV2`）、任意 HTTP 头（`Transport` 装饰器）。

> ✅ 回归网：`internal/client/quirks_test.go` 验证「别名配置 → `api.Client` → 实际 HTTP 请求」整条链路；`internal/api/quirks_test.go` 验证每个开关的行为与"零值等于既有行为"。

#### 4.5.4 ErrorResponse 类型

```go
// internal/api/types.go（部分）
type ErrorResponse struct {
    XMLName    xml.Name `xml:"Error"`
    Code       string
    Message    string
    BucketName string
    Key        string
    RequestID  string
    HostID     string
    StatusCode int  // HTTP 状态码（非 XML 字段，由 parseErrorResponse 填充）
}

func (e *ErrorResponse) Error() string { ... }  // 实现 error 接口
```

`ErrorResponse` 实现 `error` 接口，可通过 `errors.As(err, &apiErr)` 提取，是退出码映射、错误格式化的基础。

### 4.6 配置层（`internal/config/`）

#### 4.6.1 配置模型

```go
// internal/config/config.go
var G = &Config{}  // 进程级全局配置

type Config struct {
    S map[string]Static  // 别名 → 端点配置
    F Flags              // 全局 CLI 开关
    C string             // 配置文件路径
}

type Static struct {
    AccessKey, SecretKey, SessionToken string
    HostBase    string
    Region      string
    NoVerifySSL bool
    BucketLookup string  // path / dns / 自定义模板
    DefaultMimeType      string
    MultipartChunkSizeMb int
    MaxRetries           int
    TLSMinVersion        string  // "1.0"/"1.1"/"1.2"/"1.3"
}

type Flags struct {
    Debug, NoColor, ShowSecret, NoVerifySSL bool
    UserAgent, UserAgentSuffix, HostBase    string
    Headers                                 []string
}
```

#### 4.6.2 bucket_lookup 解析

`Static.ResolveBucketLookup()` 是配置层的一个重要方法，把字符串配置解析为结构化结果：

```go
func (c *Static) ResolveBucketLookup() (mode string, tpl string, err error) {
    switch strings.ToLower(c.BucketLookup) {
    case "", "path": return BucketLookupPath, "", nil
    case "dns":      return BucketLookupDNS, "", nil
    }
    if validateCustomTemplate(c.BucketLookup) {
        return BucketLookupCustom, c.BucketLookup, nil
    }
    return "", "", fmt.Errorf("invalid bucket_lookup ...")
}
```

自定义模板的合法性校验（`validateCustomTemplate`）：
- `%(bucket)` 必须存在、不能在最末尾、仅一次
- `%(region)` 可选，最多一次
- 替换为测试值后须为合法 URL，host 非空且不含 `..`

#### 4.6.3 配置文件格式

TOML 格式，每个别名是一个 table：

```toml
[my-s3]
host_base = "https://s3.example.com"
access_key = "AKIA..."
secret_key = "..."
bucket_lookup = "path"
```

读取用 `BurntSushi/toml`，写入也是 TOML（`saveconf.go` 手工序列化以保证字段顺序和注释）。

### 4.7 路径解析（`internal/s3path/`）

#### 4.7.1 Path 结构

```go
type Path struct {
    Alias         string  // 别名（必填）
    Bucket        string  // 桶名（必填，仅别名时为空）
    Key           string  // 对象 key（可空；目录时以 "/" 结尾）
    TrailingSlash bool    // 原始输入是否以 "/" 结尾
}
```

#### 4.7.2 解析规则

`s3path.Parse(s)` 的核心逻辑：

1. 找第一个 `:` → 左边是 alias，右边是 `bucket/key`
2. 没有 `:` → 仅 alias，返回 `ErrAliasOnly`（调用方可特殊处理，如 `ls` 列出所有桶）
3. 校验 alias 名（正则 `^[A-Za-z0-9]([a-zA-Z0-9_\-.]{0,62}[A-Za-z0-9])?$`）
4. 按第一个 `/` 切分 bucket 和 key
5. 校验 bucket 名
6. 处理尾斜杠：`TrailingSlash` 记录原始语义，key 去掉尾部 `/` 后再加回（确保前缀匹配精确）

> ⚠️ **为什么尾斜杠很重要**：`my-bucket/dir` 和 `my-bucket/dir/` 语义不同。前者可能是一个对象，后者是目录前缀。`TrailingSlash` 字段保留这一语义，供 put/get/cp/mv 计算目标 key 时使用。

#### 4.7.3 目标 key 解析

`ResolveFileDest` 和 `ResolveDirDestPrefix` 处理「源 → 目标」的 key 映射，模拟 `cp -r` 的行为。规则较复杂，涉及：源是否有尾斜杠、目标是否有尾斜杠、目标当前状态（文件/目录/不存在）。详见代码注释。

### 4.8 公共支撑库（`internal/`）

#### 4.8.1 fmtutil（格式化输出）

- `Table`：轻量文本表格，支持 CJK 全角字符宽度计算（中文表头对齐不偏移）、单元格着色、行数超限自动降级为流式 TSV
- `Color`：ANSI 颜色封装，遵循 `--no-color` 和非终端自动禁用
- `FormatBytes`：字节单位换算（B/KB/MB/GB/...）

#### 4.8.2 i18n（国际化）

双语机制的核心是 `T(en, zh string) string` 函数：

```go
func T(en, zh string) string {
    if currentLang() == Zh {
        return zh
    }
    return en
}
```

语言解析（`Resolve`）：
- `--lang zh` / `--lang en`：直接采用
- `--lang auto`（默认）：按时区 + locale 自动检测
  - 时区命中中国时区（`Asia/Shanghai` 等）+ 终端支持中文编码 → 中文
  - 否则 → 英文

**关键约束**：语言必须在 cobra 命令**构建之前**解析，因为帮助/描述文案在构造时就渲染了。所以 `NewRootCmd` 在构造 rootCmd 之前先调用 `resolveLangPref(os.Args)` 手动解析 `--lang`（绕过 cobra）。

#### 4.8.3 progress（进度条）

`Tracker` 实现：
- 终端宽度自适应（`term.GetSize`，定时刷新）
- 速率 / ETA 计算
- 静默模式（`--quiet` 或非终端场景自动启用）
- 并发安全（统计数据用 `atomic`，显示节奏用 `sync.Mutex`）
- 幂等 `Stop()`，调用后各 `Add*` 不再渲染

#### 4.8.4 kvcache（泛型 KV 缓存）

`Cache[K, V]` 是一个简单的并发安全泛型缓存，用于：
- `client.S3Clients`：按 alias 缓存 S3 客户端
- `api.Client.bucketLocCache`：按 bucket 缓存 region

---

## 第五章 核心实现深度剖析

### 5.1 AWS SigV4 签名（`internal/api/signer.go`）

这是「零 SDK 依赖」的技术基石。SigV4 签名的完整流程：

#### 5.1.1 签名步骤

```
① 构建规范请求（Canonical Request）
   = HTTPMethod + "\n"
   + CanonicalURI + "\n"
   + CanonicalQueryString + "\n"
   + CanonicalHeaders + "\n"
   + SignedHeaders + "\n"
   + HashedPayload

② 构建待签字符串（String to Sign）
   = "AWS4-HMAC-SHA256" + "\n"
   + ISO8601DateTime + "\n"
   + Scope(YYYYMMDD/region/s3/aws4_request) + "\n"
   + SHA256(CanonicalRequest)

③ 派生签名密钥（Signing Key）
   kDate    = HMAC-SHA256("AWS4" + SecretKey, YYYYMMDD)
   kRegion  = HMAC-SHA256(kDate,    region)
   kService = HMAC-SHA256(kRegion,  "s3")
   kSigning = HMAC-SHA256(kService, "aws4_request")

④ 计算签名
   signature = HEX(HMAC-SHA256(kSigning, StringToSign))

⑤ 构造 Authorization 头
   Authorization: AWS4-HMAC-SHA256
     Credential=AK/yyyymmdd/region/s3/aws4_request,
     SignedHeaders=host;x-amz-content-sha256;x-amz-date,
     Signature=...
```

#### 5.1.2 关键实现细节

**规范头处理**：
- host 始终参与签名（即使没显式设置）
- `Content-Length` 存于 `req.ContentLength` 而非 Header，需单独纳入
- `Authorization`/`User-Agent`/`Accept-Encoding` 不参与签名（随网络层变化）
- 头名小写，值去首尾空白，按键排序

**查询串规范**：键值均百分号编码，按键排序，同键多值再按值排序

**x-amz-content-sha256**（payload 摘要）的三种情况：
- body 为 nil → 空串的 SHA256（`e3b0c442...`）
- body 是 `io.ReadSeeker` → 计算实际 SHA256 并回卷
- body 不可 Seek → `UNSIGNED-PAYLOAD`（流式上传场景）

```go
// internal/api/api.go: newRequest
switch {
case meta.contentBody == nil:
    shaHex = emptySHA256Hex
default:
    if s, ok := meta.contentBody.(io.ReadSeeker); ok {
        shaHex, err = hashSHA256Seeker(s)  // 计算后回卷
    } else {
        shaHex = unsignedPayload
    }
}
```

### 5.2 桶寻址（`internal/api/bucket-lookup.go`）

#### 5.2.1 三种寻址方式

| 方式 | URL 格式 | 适用场景 |
| --- | --- | --- |
| Path-style | `http://host/bucket/key` | 默认，兼容性最好（MinIO、自建 S3） |
| DNS (virtual-host) | `http://bucket.host/key` | AWS S3 标准 |
| 自定义模板 | 模板决定 | 特殊端点（如 `https://%(bucket).s3.%(region).example.com`） |

#### 5.2.2 自定义模板的区域探测

当模板含 `%(region)` 占位符时，需要先知道 bucket 的实际 region。这会触发「先有鸡还是先有蛋」问题：需要 region 才能寻址，但寻址才能拿 region。

**解决方案**：`probeBucketLocation` 强制用 path-style 发送 `GET /bucket?location` 请求，打破引导环：

```go
func (c *Client) probeBucketLocation(ctx context.Context, bucket string) (string, error) {
    resp, err := c.Do(ctx, http.MethodGet, requestMetadata{
        bucketName:     bucket,
        queryValues:    urlValues{"location": ""},
        forcePathStyle: true,  // ← 关键：强制 path-style，跳过自定义模板
    })
    // ...
}
```

探测结果缓存在 `bucketLocCache`，每个 bucket 在进程内最多探测一次。

#### 5.2.3 运行期二次校验

配置期的 `validateCustomTemplate` 只用固定测试值（`test-bucket`）校验模板。真实 bucket 名（如含 `..`）替换进模板后可能拼出非法 host。因此 `validateResolvedCustomEndpoint` 在构造请求 URL 前再做一次校验，防止路径穿越。

### 5.3 请求生命周期（`internal/api/api.go: Do`）

`Client.Do` 是所有 S3 操作的统一入口，处理签名、发送、重试、重定向、错误解析。

#### 5.3.1 重试机制

```go
attempts := c.maxRetries + 1  // 默认 4 次（1 + 3 重试）
for attempt := 0; attempt < attempts; attempt++ {
    if attempt > 0 {
        // 回卷 body（不可回卷则无法安全重试）
        if seeker == nil && meta.contentBody != nil { break }
        if seeker != nil { seeker.Seek(bodyStart, SeekStart) }
        time.Sleep(retryBackoff(attempt))  // 指数退避 + 抖动
    }
    // ... 发送请求 ...
}
```

**可重试条件**（`isRetryable`）：
- HTTP 状态码：429 / 500 / 502 / 503 / 504
- 错误码：SlowDown / RequestTimeout / InternalError / ServiceUnavailable

**退避策略**（`retryBackoff`）：
- 基础 200ms，左移 attempt 位（指数增长）
- 上限 10s
- 加入 0~50% 抖动，避免惊群
- attempt 钳制到 25 以内，防止极大 MaxRetries 时移位溢出

#### 5.3.2 Region 重定向（精妙设计）

S3 可能要求 bucket 特定 region，通过以下方式告知：
- 301 / 307 重定向 + `X-Amz-Bucket-Region` 头
- 400 + `X-Amz-Bucket-Region` 头

s3cli 的处理有**两层循环**：

```
外层循环：attempt（消耗重试预算）
  └─ 内层循环：region 重定向（不消耗重试预算）
       ├─ newRequest(当前 signingRegion) → 签名
       ├─ httpClient.Do(req)
       ├─ 收到 301/307/400 + X-Amz-Bucket-Region?
       │   └─ 是 → 更新 signingRegion + 写 bucketLocCache + 回卷 body + continue（内层）
       └─ 否 → break（交给成功/错误处理）
```

**关键设计决策**：
1. **禁止自动跟随重定向**（`CheckRedirect: ErrUseLastResponse`）：S3 的 region 重定向需要重签（region 变化），默认跟随会跨 host 转发且丢失 Authorization 头。
2. **region 缓存**：发现的 region 写入 `bucketLocCache`，后续该 bucket 的请求直接命中正确 region，无需再次重定向。
3. **重定向不消耗重试预算**：把完整的重试次数留给真正的网络/5xx 错误。

#### 5.3.3 零字节 body 的特殊处理

```go
// 0 字节 body（如空对象上传）: 显式替换为 http.NoBody
if meta.contentLength > 0 {
    req.ContentLength = meta.contentLength
} else if meta.contentBody != nil {
    req.Body = http.NoBody
    req.ContentLength = 0
}
```

原因：`http.NewRequest` 只对实现了 `Len()` 的空 reader（如 `bytes.Reader`）自动做此转换。其他类型（如 `PutObjectStream` 传入的 0 长度 `*os.File`）的 `ContentLength` 为 0 时会被 transport 视为长度未知而走 chunked，严格服务端会拒绝。

### 5.4 流式传输框架（`internal/action/stream.go: RunStream`）

`RunStream` 统一支撑 put/get/cp/mv 的批量并发传输，采用**生产者-消费者**模式：

```
┌─────────────┐     ┌──────────────────┐     ┌─────────────────┐
│ Count 协程  │     │ Scan 协程        │     │ Work 协程池     │
│ (预统计)    │     │ (扫描任务)       │     │ (并发处理)      │
│             │     │                  │     │                 │
│ 遍历数据源  │     │ 遍历数据源       │     │ for job := range jobs │
│ add(n,size) │     │ jobs <- StreamJob│     │   Work(job)     │
│             │     │                  │     │   report(n)     │
└─────────────┘     └──────────────────┘     └─────────────────┘
       │                    │                        │
       └─── AddTotal/Size ──┴─── AddTotal/Size ──────┘
                            │
                     ┌──────▼──────┐
                     │  Tracker    │
                     │  (进度条)   │
                     └─────────────┘
```

#### 5.4.1 预统计（Count）

`Count` 是可选的预统计协程：在独立 goroutine 中提前快速遍历数据源（S3 用 `ListObjectsV2Paginator`，本地用 `filepath.Walk`），增量上报对象数和字节数，使进度条的 total 尽早接近真实总量。

- 提供 `Count` 时，`Scan` 阶段不再累加 total（避免重复计数）
- `Count` 失败时退化为旧行为（`Scan` 边派发边累加）

#### 5.4.2 取消与泄漏防护

`RunStream` 对取消和协程泄漏做了细致处理：

- `ctx.Done()` 时立即停止扫描和处理
- 取消时排空 relay channel，避免内部 Scan 协程永远阻塞
- `scanWg` 纳入外层转发、内层 Scan、drain 协程，`RunStream` 返回前统一等待
- `countWg` + `cancelCount` 保证 Count 协程在 Stop() 后不再渲染

#### 5.4.3 进度对账

```go
// 成功：对账，把进度精确补齐到 job.Size
if diff := j.Size - reported; diff != 0 {
    pt.AddTotalSizeDone(diff)
}
// 失败：回退本任务已上报的字节
if reported != 0 {
    pt.AddTotalSizeDone(-reported)
}
```

`reported` 记录本任务已通过 `report` 累加到进度条的字节数。适配两种场景：
- 有分片进度的操作（如分片上传）：`report` 被多次调用，`reported` 累加
- 无分片进度的操作（如服务端 CopyObject）：`report` 未被调用，成功后按 `job.Size` 补齐

### 5.5 分片上传与断点续传

#### 5.5.1 三种上传路径

```
uploadFile(ctx, opt, mimeType, bucket, key, filePath, report)
    │
    ├── 文件 < 64 MiB → S3.PutObjectStream（单次 PUT）
    │
    └── 文件 >= 64 MiB → uploadMultipartFile（分片 + 断点续传）

putStdin (put -) → uploadUnknownSize
    │
    ├── 首片读不满 → S3.PutObject（单次 PUT）
    └── 首片读满 → uploadMultipart（分片，无断点续传）
```

关键常量：
```go
minMultipartPartSize = 5 * 1024 * 1024   // 最小分片 5MB（S3 限制）
defaultMultipartSize = 15 * 1024 * 1024  // 默认分片 15MB
multipartThreshold   = 64 * 1024 * 1024  // 触发分片的阈值 64MB
maxMultipartParts    = 10000             // 最大分片数（S3 限制）
```

`multipartPartSize` 会根据文件大小自动放大分片，确保不超过 10000 个分片上限。

#### 5.5.2 断点续传机制（uploadMultipartFile）

这是 s3cli 最有价值的高级特性之一。流程：

```
① 加载本地状态文件（loadMultipartState）
   - 按 (本地路径, bucket, key) 的 SHA256 哈希命名
   - 校验：Version / UploadID / Bucket / Key / TotalSize / ModTime 必须全部匹配
   - 不匹配或不存在 → 返回 nil（新建上传）

② 若有有效本地状态：
   - ListParts 向服务端查询已上传的分片
   - NoSuchUpload?（服务端 upload 已被 Abort/过期）
     → 自愈：放弃旧 uploadID，重新 CreateMultipartUpload
   - ListParts 成功：
     → 对账：服务端返回的分片序号必须连续（1,2,3,...），否则重建
     → 收集已完成的 parts

③ 若无有效状态：
   - CreateMultipartUpload
   - 保存状态文件（saveMultipartState）
     · 原子写：临时文件 + fsync + rename
     · 权限 0600

④ 从 offset = len(parts) * partSize 处续传剩余分片

⑤ CompleteMultipartUpload

⑥ 删除状态文件
```

**核心安全保证**：服务端 `ListParts` 响应是权威，本地状态文件从不作为权威。即使本地状态文件被篡改/编辑，也只会影响「从哪里开始续传」，不会导致未验证的分片被 Complete（Complete 必须使用服务端返回的真实 ETag）。

#### 5.5.3 状态文件格式

位置：`~/.s3cli-mpu/<sha256>.json`（注意：不是 `~/.s3cli/mpu`，因为 `~/.s3cli` 是文件不是目录）

```json
{
  "version": 1,
  "upload_id": "abc123...",
  "bucket": "my-bucket",
  "key": "path/to/file",
  "local_path": "/abs/path/to/local/file",
  "part_size": 15728640,
  "total_size": 104857600,
  "mod_time_unix_ns": 1692000000000000000,
  "created_at": "2026-08-14T..."
}
```

管理命令：
- `s3cli mpu local-list`：列出本地状态
- `s3cli mpu local-clear <path>`：删除指定状态文件

#### 5.5.4 清理与取消

任何分片或完成失败都会中止服务端 upload（释放空间）。关键：清理用 `context.WithoutCancel(ctx)`，避免用户 Ctrl+C 后连 AbortMultipartUpload 一起取消，导致服务端残留分片。

### 5.6 镜像同步（mirror）

mirror 是最复杂的命令，涉及双端流式列举、归并差异、并发复制/删除、断点续传。

#### 5.6.1 整体架构

```
┌──────────────┐     ┌──────────────┐
│ streamObjects│     │ streamObjects│
│ (源端列举)   │     │ (目标端列举) │
└──────┬───────┘     └──────┬───────┘
       │ srcCh              │ tgtCh
       ▼                    ▼
┌──────────────────────────────────┐
│ filterObjects (include/exclude)  │
└──────┬───────────────────────────┘
       ▼
┌──────────────────────────────────┐
│ streamDiff (有序归并)            │
│ merge-join 两个有序流            │
└──────┬───────────────────────────┘
       ▼ actions (diffAction)
┌──────────────────────────────────┐
│ copyAndDelete                    │
│ - worker 池并发复制              │
│ - 收集 delete 列表               │
│ - 批量删除（1000/批）            │
└──────────────────────────────────┘
```

#### 5.6.2 流式归并差异（streamDiff）

因为 S3 `ListObjectsV2` 保证 key 字典序递增，源/目标两个流都是有序的，可以做经典的 **merge-join**：

```go
for srcOK && tgtOK {
    switch {
    case src.Key < tgt.Key:
        // src 独有 → COPY
        actions <- diffAction{rel: src.Key, size: src.Size}
        src, srcOK = <-srcCh
    case src.Key > tgt.Key:
        // tgt 独有 → DELETE（目标多余）
        actions <- diffAction{rel: tgt.Key, delete: true}
        tgt, tgtOK = <-tgtCh
    default: // 相等
        if overwrite && needsUpdate(src, tgt) {
            actions <- diffAction{rel: src.Key, size: src.Size}
        }
        src, srcOK = <-srcCh
        tgt, tgtOK = <-tgtCh
    }
}
```

**内存占用 O(1)**：仅各持有一个待比较对象，不依赖全集装载。

#### 5.6.3 同端 vs 跨端复制

`sameEndpoint` 判断源/目标是否同一 endpoint（规范化后比较 endpoint URL）：

- **同端**：`copyObjectSameEndpoint` → 服务端 `CopyObject`（零拷贝，最快）
- **跨端**：`copyObjectCrossEndpoint` → download + upload
  - 小文件：整体下载到内存再 PUT
  - 大文件：Range 分片下载 + UploadPart（内存上限为一个分片）

#### 5.6.4 needsUpdate 判断

```go
func needsUpdate(src, tgt ObjectInfo) bool {
    // MPU 上传的 ETag 形如 "xxx-N"，两端不可比
    if !strings.Contains(src.ETag, "-") && !strings.Contains(tgt.ETag, "-") &&
        src.ETag != "" && tgt.ETag != "" {
        return src.ETag != tgt.ETag  // ETag 可比，优先用 ETag
    }
    if src.Size != tgt.Size {
        return true
    }
    return src.LastModified.After(tgt.LastModified)  // 退化到 size + mtime
}
```

**已知局限**：S3 的 LastModified 只有秒级精度。若源/目标同一秒内先后写入，即使内容不同也可能被判定为「无需更新」。这是 S3 元数据精度限制，需要 ETag 可比（非 MPU 上传）或加大同步间隔来规避。

#### 5.6.5 安全守卫

- **前缀重叠检测**：同 endpoint + 同 bucket 时，禁止源/目标前缀互相包含。否则 `--remove` 会删自己的数据，或不加 `--remove` 会级联复制。
- **前缀规范化**：`normalizeMirrorPrefix` 把非空前缀补上尾斜杠，避免裸前缀的前缀碰撞（`dir` 误匹配 `dir2/x`）。
- **--max-delete**：限制删除数量，防止误删大量数据。
- **删除失败必须上报**：目标端仍有应清理的对象，不能以成功退出。

#### 5.6.6 manifest 断点续传

`--manifest <file>` + `--resume`：

- 成功复制的相对 key 追加写入 manifest 文件
- `--resume` 时读入并跳过这些 key
- manifest 只是加速提示，服务端 ListParts 仍是续传权威
- 非 resume 模式必须截断 manifest（`O_TRUNC`），避免历史 key 污染

### 5.7 文件差异对比（diff）

支持三种模式：

| 模式 | 判断依据 | 速度 | 准确性 |
| --- | --- | --- | --- |
| `--mode size` | 仅比较大小 | 最快 | 可能漏掉同大小的不同内容 |
| `--mode quick` | 大小 + mtime | 快 | mtime 精度有限 |
| `--mode md5`（默认） | 大小相同时流式 MD5 | 较慢 | 最准 |

目录模式：递归列举两端，按相对路径建立索引，并发比对（MD5 模式并发下载计算）。

发现差异时返回 `errDiffer` 哨兵错误，映射为退出码 6（非错误，供脚本判断）。

### 5.8 S3 Select（`internal/api/object-select.go`）

自行解析 S3 Select 的**事件流（event stream）**二进制格式：

```
每条消息的帧格式:
  prelude (12B): TotalLength(4,BE) | HeadersLength(4,BE) | PreludeCRC(4,BE,CRC32)
  headers: NameLen(1) | Name | ValueType(1) | ValueLen(2,BE) | Value
  payload: 变长
  message CRC (4,BE, CRC32)

header ":message-type" = event | error
header "event-type"    = Records | Progress | Stats | End | Continuation
```

这是纯手写的二进制解析，因为标准库和依赖库都不提供 S3 Select 的事件流解码器。`onRecord` 回调逐条输出 Records 事件的内容。

---

## 第六章 命令体系全解

### 6.1 端点管理

#### `alias` - 管理别名

```bash
s3cli alias add <name> [url] [ak] [sk] [token]   # 添加（1参数交互式，4-5参数非交互）
s3cli alias edit <name>                          # 交互式编辑（空输入保留原值）
s3cli alias list [name] [-s]                     # 列出（-s 显示完整 secret）
s3cli alias del <name>                           # 删除
```

支持 tab 补全（`ValidArgsFunction: AutoCompleteAlias`）。

### 6.2 桶管理

#### `bucket` - 桶管理与配置

```bash
s3cli bucket make <alias:name>                   # 创建桶
s3cli bucket remove <alias:name>                 # 删除桶
```

桶子资源配置（每个都有 `set`/`get`/`del` 子命令）：

| 子命令组 | 管理的配置 | 文件 |
| --- | --- | --- |
| `bucket cors` | CORS | `cmd/cors.go`, `internal/action/bucket-cors.go` |
| `bucket lifecycle` | 生命周期规则 | `cmd/lifecycle.go`, `internal/action/bucket-lifecycle.go` |
| `bucket policy` | 桶策略 | `cmd/policy.go`, `internal/action/bucket-policy.go` |
| `bucket encryption` | 服务端加密 | `cmd/encryption.go`, `internal/action/bucket-encryption.go` |
| `bucket versioning` | 版本控制 | `cmd/versioning.go`, `internal/action/bucket-versioning.go` |
| `bucket event` | 事件通知 | `cmd/event.go`, `internal/action/bucket-notification.go` |
| `bucket tag` | 桶标签 | `cmd/tag.go`, `internal/action/tag.go` |
| `bucket acl` | 桶 ACL（canned + grant 头） | `cmd/bucketacl.go`, `internal/action/bucket-acl.go` |
| `bucket object-lock` | Object Lock 默认保留策略 | `cmd/objectlock.go`, `internal/action/object-lock.go` |
| `bucket replication` | 跨区域复制 | `cmd/replication.go`, `internal/action/bucket-replication.go` |
| `bucket public-access-block` | 公共访问阻断 | `cmd/publicaccess.go`, `internal/action/bucket-public-access.go` |

> 桶配置的 `--from-file` / `set` 输入 JSON 统一采用 **AWS CLI / SDK 字段名**：
> 通知是 `TopicArn` / `QueueArn` / `LambdaFunctionArn` + `Filter.Key.FilterRules`，CORS 是
> `CORSRules` 与复数形式 `AllowedOrigins` / `AllowedMethods` / `AllowedHeaders` / `ExposeHeaders`。
> 因此 `aws s3api` 的对应输出可直接使用，`s3cli bucket <cfg> get` 的输出也可原样回灌
> `set --from-file`。`bucket event set --help` 里有完整示例。

### 6.3 读取命令

| 命令 | 作用 | 关键 flag |
| --- | --- | --- |
| `ls` | 列出桶/对象 | `-r`, `--versions`, `-I`(未完成上传), `--summarize`, `--include`/`--exclude` |
| `du` | 磁盘占用 | `-r`, `-d N`(深度) |
| `stat` | 对象/桶元信息 | `-r`, `--version-id`/`--vid` |
| `info` | 元信息（JSON） | `-r`, `--version-id`/`--vid` |
| `find` | 按名称/大小/时间查找 | `--name`, `--larger-than`, `--smaller-than`, `--newer-than`, `--older-than` |
| `tree` | 树形展示 | `-d N`(深度) |
| `diff` | 差异对比 | `--mode size/quick/md5`, `-r`, `--json` |

### 6.4 对象操作

| 命令 | 作用 | 关键 flag |
| --- | --- | --- |
| `put` | 上传 | `-r`, `--content-type`, `--storage-class`/`--sc`, `--metadata k=v`, `--tags`, `--overwrite`, `--dry-run`, `--include`/`--exclude`, `--checksum`, `-q` |
| `get` | 下载 | `-r`, `--concurrency`, `--range`, `--overwrite`, `--version-id`, `-o`, `-t`, `-n`, `--dry-run`, `--include`/`--exclude`, `--checksum`, `-q` |
| `cp` | 复制（同 endpoint） | `-r`, `--storage-class`, `--concurrency`, `--dry-run`, `--include`/`--exclude` |
| `mv` | 移动（同 endpoint） | `-r`, `--storage-class`, `--concurrency`, `--dry-run`, `--include`/`--exclude` |
| `rm` | 删除 | `-r --force`, `--versions`, `-I`(未完成上传), `--dry-run`, `--older-than`, `--newer-than`, `--stdin`, `--non-current` |
| `restore` | 恢复归档对象 | `--days`, `--tier`, `--version-id` |
| `sql` | S3 Select | `-e`(表达式), `--input-format`, `--output-format` |
| `tag` | 对象/桶标签 | `set`/`get`/`del` |
| `object acl` | 对象 ACL | `get`/`set`，`--version-id`/`--vid` |
| `object retention` | 对象保留策略 | `get`/`set`，`--mode`, `--retain-until` |
| `object legal-hold` | 对象法定保留 | `get`/`set`，`--status ON/OFF` |

### 6.5 同步

#### `mirror` - 单向镜像同步

```bash
s3cli mirror <src> <tgt> [flags]
```

关键 flag：

| Flag | 作用 |
| --- | --- |
| `--remove` | 删除目标端多余对象 |
| `--overwrite` | 已存在时依据 ETag/Size 覆盖 |
| `--dry-run` | 仅打印计划，不执行 |
| `--concurrency` | 并发数（默认 10） |
| `--part-size` | 分片大小 MB |
| `--storage-class` | 目标存储类别 |
| `--size-limit` | 单对象大小上限 |
| `--max-delete` | 删除数量上限 |
| `--include`/`--exclude` | glob 过滤（可重复） |
| `--manifest` + `--resume` | 断点续传 |
| `-q` | 禁用进度条 |

### 6.6 分片上传管理

#### `mpu` - 管理进行中的分片上传

```bash
s3cli mpu list <alias:bucket>             # 列出服务端进行中的分片上传
s3cli mpu abort <alias:bucket/key> <id>   # 中止指定上传
s3cli mpu local-list                      # 列出本地断点续传状态文件
s3cli mpu local-clear <state-path>        # 删除本地状态文件
```

### 6.7 工具

#### `share` - 生成预签名 URL

```bash
s3cli share download <alias:bucket/key> --expire 24h   # 下载 URL
s3cli share upload <alias:bucket/key> --expire 1h      # 上传 URL
```

---

## 第七章 设计权衡与陷阱

这一章记录了代码中那些「不显然但很重要」的设计决策，是深入理解和安全修改代码的关键。每条都附带了代码注释中记录的踩坑历史。

### 7.1 退出码的语义化

**问题**：最初所有错误都 `os.Exit(1)`，脚本无法区分「对象不存在」和「权限错误」。

**解决**：`exitCodeForError` 按错误类型还原语义化退出码（4/5/6/130）。这要求 `wrapErrs` 用双重 `%w` 包装，既抑制重复打印（`errAlreadyDisplayed`），又穿透到原始 `*ErrorResponse`。

### 7.2 命令名冲突的 fail-fast

**问题**：cobra 的命令命中顺序依赖注册次序，曾导致 `rm`（删对象）被误路由到删桶命令的别名 `rm`。

**解决**：`NewRootCmd` 中遍历注册表时做 token 冲突检测，`panic` 而非静默路由（这是删数据级别的危险 bug）。

### 7.3 AllowAliasOnly 的作用域

**问题**：曾经用包级变量 `AllowAliasOnly` 控制是否容忍「仅别名」参数，导致跨命令泄漏（一个命令的设置影响另一个）。

**解决**：改为闭包捕获 `allowAliasOnly` 参数（`newRunEWithMode`），作用域限定在单个命令构建时。

### 7.4 region 重定向不消耗重试预算

**问题**：如果 region 重定向消耗重试预算，真正的网络错误可能无预算可用。

**解决**：双层循环，内层处理 region 重定向（不消耗预算），外层处理网络/5xx 错误。

### 7.5 bucket 寻址的引导环

**问题**：自定义模板含 `%(region)` 时，需要 region 才能寻址，但寻址才能拿 region。

**解决**：`probeBucketLocation` 用 `forcePathStyle` 强制 path-style，打破引导环。结果缓存在 `bucketLocCache`，每个 bucket 最多探测一次。

### 7.6 断点续传的权威

**问题**：本地状态文件可能被篡改、编辑、陈旧。

**解决**：服务端 `ListParts` 响应永远是权威。本地状态只用于「找到续传点」，Complete 必须用服务端返回的真实 ETag。NoSuchUpload 时自愈重建。

### 7.7 清理用 WithoutCancel

**问题**：用户 Ctrl+C 后 ctx 被取消，连 AbortMultipartUpload 一起取消，导致服务端残留分片。

**解决**：清理用 `context.WithoutCancel(ctx)`，确保中止请求不受用户取消影响。

### 7.8 前缀碰撞

**问题**：裸前缀（如 `dir`）做 ListObjectsV2 会误匹配 `dir2/x`、`dir-old/y`；目标端同理会在 `--remove` 时误删。

**解决**：`normalizeMirrorPrefix` 把非空前缀补上尾斜杠（`dir/`）。

### 7.9 mirror 的前缀重叠守卫

**问题**：同 endpoint + 同 bucket 时，源/目标前缀互相包含会导致：
- `--remove` 删自己的数据
- 不加 `--remove` 边列举边复制，新写入的对象被源分页器再次列出，级联复制

**解决**：`resolveMirrorPlan` 检测前缀重叠，直接报错拒绝执行。

### 7.10 S3 LastModified 精度限制

**问题**：S3 的 LastModified 只有秒级精度，`needsUpdate` 可能误判。

**解决**：ETag 可比时优先用 ETag；文档中明确记录这一局限，建议依赖 ETag 或加大同步间隔。

### 7.11 0 字节 body 的 chunked 编码

**问题**：0 长度 `*os.File` 的 ContentLength 为 0 时会被 transport 视为长度未知而走 chunked，严格服务端拒绝。

**解决**：显式替换为 `http.NoBody`，保证发送 `Content-Length: 0`。

### 7.12 目录探测的裸前缀陷阱

**问题**：曾经有一层「裸 key 前缀」兜底探测（`Prefix=key, MaxKeys=1`），会把同名的其他对象误判为目录（如 `key="report"` 误命中 `report-2023.pdf`）。

**解决**：整体移除裸前缀兜底，只保留 `key + "/"` 探测。

### 7.13 进度条的协程泄漏

**问题**：`RunStream` 返回后，Count/Scan 协程可能仍在运行，导致终端错乱和协程泄漏。

**解决**：`countWg`/`scanWg` 纳入所有后台协程（含取消时排空 relay 的 drain 协程），`RunStream` 返回前统一等待。

### 7.14 mirror manifest 的并发安全

**问题**：主 goroutine 调 `has()` 时，worker goroutine 可能正在 `mark()` 写同一个 map，触发 `concurrent map read and map write`。

**解决**：`has()` 和 `mark()` 都持 `mu` 锁。

### 7.15 语言解析时机

**问题**：帮助/描述文案在 cobra 命令构造时就渲染，但 cobra 的 flag 解析在构造之后。

**解决**：`resolveLangPref(os.Args)` 在 cobra 解析前手动扫描 `--lang`，`i18n.Resolve()` 全局生效后再构造命令。

### 7.16 TLS 版本可配置

**问题**：老式自建 S3 端点可能只支持 TLS 1.0/1.1，Go 默认最低 TLS 1.2 会连接失败。

**解决**：`tls_min_version` 配置项支持 1.0/1.1/1.2/1.3，缺省 1.2。注意 `no_verify_ssl` 不降低协议版本。

### 7.17 同一 DTO 承载 XML 与 JSON 两套字段名

**问题**：桶子资源 DTO 同时要服务两种线格式，而两者的字段名并不一致：

- **XML（S3 线协议）**：历史命名，如通知的 `<CloudFunctionConfiguration>` / `<CloudFunction>`、过滤条件 `<S3Key><FilterRule>`；CORS 的列表是逐个单数元素 `<AllowedOrigin>`；
- **JSON（AWS CLI / SDK 与本地配置文件）**：如 `LambdaFunctionArn` / `QueueArn`，过滤条件 `Filter.Key.FilterRules`；CORS 的列表是复数键 `AllowedOrigins` / `AllowedMethods`。

早期 CORS 的 json tag 直接照搬了 xml 元素名（单数），通知则完全没有 json tag，后果都是**静默丢字段**：`bucket cors set --from-file` 会把 AWS 形状的规则解析成只剩 `ID`/`MaxAgeSeconds` 的空壳，`bucket event set` 则报成"未找到通知配置"。

**解决**：在 DTO 上并列标注两套 tag（`xml:"CloudFunction" json:"LambdaFunctionArn"`、`xml:"AllowedOrigin" json:"AllowedOrigins"`），并给 `XMLName` 加 `json:"-"`，避免 `{"Space":"","Local":""}` 噪声混进 `bucket event get` / `bucket encryption get` 的输出。配套三条约定：

1. **写方向**按厂商线协议（由 `Quirks` 决定元素命名），**读方向**始终宽容（`NotificationConfiguration.UnmarshalXML` 接受两种元素命名）；
2. JSON 解析对未识别键是宽容的（`unmarshalAWS` 先严格后降级），因此 `internal/action` 在发请求前校验必需字段是否齐备——键名拼错只会留下空字段，本地报"哪一类、哪一条配置缺什么"远好过服务端回一个 `MalformedXML`；
3. 每个 `--from-file` 配置都要有"JSON → XML → 服务端 → 回读"的回归测试，单测 JSON 形状不够——`xml` tag 写错时 JSON 侧测试仍然是绿的。

回归测试：`internal/api/bucket-notification-json_test.go`、`internal/api/bucket-cors-json_test.go`（JSON 形状 / 往返幂等）+ `internal/action/bucket-notification_test.go`、`internal/action/bucket-cors_test.go`（JSON → XML → 服务端 → DTO 全链路）。

---

## 第八章 测试与持续集成

### 8.1 测试分层

| 层级 | 范围 | 特点 |
| --- | --- | --- |
| 单元测试 | `go test ./...` | 自包含，无需真实 S3，用 mock 和 httptest |

测试全部自包含：不依赖任何外部 S3 服务、不下载任何二进制，因此可在无网络环境下
完整运行（含 `-race`）。

> **关于端到端测试的移除**：项目曾有一个基于 MinIO 的 e2e 脚本
> （`scripts/e2e-minio.sh`）与对应的 CI job。2025-10 起 MinIO 归档了开源版
> Server/MC/KES 并停止从 `dl.min.io` 分发社区二进制（所有路径统一 `410 Gone`），
> 该脚本的自动下载不再可用，CI job 必然失败。与其保留一个长期飘红、需要人工
> 提供二进制的 job，不如直接删除：单元测试 + `httptest` 已经覆盖了线协议层
> （签名、请求构造、错误解析、分页、厂商差异开关），e2e 的边际收益不足以抵消
> 对外部二进制的强依赖。
>
> 若将来需要恢复端到端验证，建议改用可持续获取的后端（SeaweedFS / Garage /
> Ceph RGW，或固定 digest 的容器镜像），而不是重新引入对单一厂商下载站的依赖。

### 8.2 单元测试的关键 mock

action 层测试不依赖真实 S3，靠的是两层替身：

1. **HTTP 层替身**：`internal/action/backend_switch_mock_test.go` 的 `mockS3Server` 是一个**内存版最小 S3 兼容服务端**（只实现 `ServeHTTP`），用 `httptest.NewServer` 挂起来，再包成 `api.S3Operations` 交给 action 层（见 `internal/action/backend_switch_test.go`）。它验证的是「HTTP 线协议 + action 语义」的一致性，而不是接口的另一种实现。

```go
// 大致结构（简化）
type mockS3Server struct {
    mu      sync.Mutex
    objects map[string]string  // key → body
    // ... 只实现 ServeHTTP：/bucket/key 的 GET/PUT/DELETE + list-type=2 分页
}
```

2. **接口替身**：由于 `api.S3Operations` 现在与 `api.Client` 同包，上层仍可面向接口注入自定义实现；`internal/action` 的测试通过 `api.S3Operations(builtin)` 显式转换来固定这一契约。

`internal/api/*_test.go` 用 `httptest.Server` 模拟 S3 HTTP 端点，测试签名、请求构造、错误解析；`internal/api/quirks_test.go` 逐个验证厂商差异开关，`internal/client/quirks_test.go` 验证「别名配置 → 客户端 → 实际请求」的完整链路。

### 8.3 CI（`.github/workflows/ci.yml`）

三个 job，全部在 `ubuntu-latest` + Go 1.27 上运行：

1. **单元测试**（`unit`）：
   - `gofmt -l main.go cmd internal` 检查格式
   - `go vet ./...` 静态检查
   - `go test -race ./...` 带竞态检测的测试

2. **静态检查**（`lint`）：
   - `golangci-lint`（标准五件套：errcheck / govet / ineffassign / staticcheck / unused）
   - `govulncheck` 依赖漏洞扫描

3. **交叉编译冒烟**（`cross-build`）：
   - `bash build.sh all` 全平台编译，并校验五个产物均存在

另有 `.github/workflows/release.yml`：打 `v*` tag 时先跑 test + vet，再全平台
交叉编译、打包、生成 SHA256SUMS 并创建 GitHub Release（用 `SOURCE_DATE_EPOCH`
保证可复现构建）。

触发条件：push 到 main + 所有 PR（release 仅 tag）。

---

## 第九章 构建与发布

### 9.1 build.sh

```bash
./build.sh          # 编译当前平台 → ./s3cli
./build.sh all      # 全平台交叉编译 → bin/
```

构建过程：
1. `rm -rf bin/`
2. 通过 `-ldflags` 注入版本信息：

> **构建脚本刻意不产生副作用**：不做 `gofmt -w`（改写源码）也不做 `go mod tidy`
> （改写依赖清单）。格式化由开发者/CI 的 `gofmt -l` 检查，依赖清单由
> `go mod tidy -diff` 检查 —— 构建只负责产出二进制，见 `build.sh` 顶部注释。

```bash
LDFLAGS="-s -w \
  -X 's3cli/cmd.Version=${VERSION}' \
  -X 's3cli/cmd.Commit=${COMMIT}' \
  -X 's3cli/cmd.BuildDate=${DATE}' \
  -X 's3cli/cmd.GoVersion=${GOVERSION}'"
```

> ⚠️ **历史坑**：包路径曾误写为 `internal/cmd`，链接器对不存在的符号静默忽略，导致版本信息从未注入。现已修正为 `s3cli/cmd`。

`-trimpath` 移除编译机器的路径信息，保证可复现构建。

`CGO_ENABLED=0` 纯静态编译，交叉编译友好。

### 9.2 支持的平台

| OS | Arch | 输出 |
| --- | --- | --- |
| linux | amd64 | `bin/s3cli-linux-amd64` |
| linux | arm64 | `bin/s3cli-linux-arm64` |
| darwin | amd64 | `bin/s3cli-darwin-amd64` |
| darwin | arm64 | `bin/s3cli-darwin-arm64` |
| windows | amd64 | `bin/s3cli-windows-amd64.exe` |

### 9.3 外部依赖

```
github.com/spf13/cobra        v1.10.2   # CLI 框架
github.com/spf13/pflag         v1.0.10   # flag 解析
github.com/BurntSushi/toml     v1.6.0    # TOML 配置解析
github.com/mattn/go-runewidth  v0.0.30   # 表格的东亚宽字符宽度
golang.org/x/term              v0.46.0   # 终端宽度/密码输入
golang.org/x/sys               v0.48.0   # 系统调用（term 的依赖）
```

极其精简，没有任何 AWS SDK 或重型依赖。所有 S3 协议逻辑都是自实现的。

---

## 第十章 术语表与索引

### 10.1 术语表

| 术语 | 含义 |
| --- | --- |
| **alias（别名）** | 一个 S3 端点的命名配置（host + 凭证 + 寻址方式等） |
| **endpoint** | S3 服务的访问入口 URL，如 `https://s3.example.com` |
| **bucket（桶）** | S3 的顶层容器 |
| **key（对象键）** | 对象在桶内的路径 |
| **SigV4** | AWS Signature Version 4，S3 请求签名算法 |
| **path-style** | 桶寻址方式：`http://host/bucket/key` |
| **DNS / virtual-host style** | 桶寻址方式：`http://bucket.host/key` |
| **multipart upload（分片上传）** | 大文件分多次上传的 S3 机制 |
| ** MPU** | Multipart Upload 的缩写 |
| **断点续传** | 上传中断后从断点继续，不重传已完成部分 |
| **manifest** | mirror 的清单文件，记录已成功复制的 key |
| **STS** | Security Token Service，临时凭证 |
| **Glacier / Deep Archive** | S3 归档存储类别，需 restore 才能访问 |
| **S3 Select** | 用 SQL 查询对象内容的 S3 功能 |
| **SSE** | Server-Side Encryption，服务端加密 |
| **Object Lock** | S3 的 WORM（写一次读多次）合规保留功能 |

### 10.2 关键文件索引

按「想了解 X，看哪个文件」组织：

| 想了解 | 看这里 |
| --- | --- |
| 项目入口 | `main.go` → `cmd/root.go: NewRootCmd` |
| 命令注册机制 | `cmd/root.go: Register / cmdRegistry` |
| RunE 工厂 | `cmd/common.go: NewRunE / NewRunEWithMode / ...` |
| 退出码映射 | `cmd/error.go: exitCodeForError` |
| S3 路径解析 | `internal/s3path/s3path.go: Parse` |
| 配置结构 | `internal/config/config.go: Config / Static / Flags` |
| bucket_lookup 解析 | `internal/config/config.go: ResolveBucketLookup` |
| 客户端构造 | `internal/client/client.go: newS3Client` |
| 客户端缓存 | `internal/client/parse-path.go: NewClient / S3Clients` |
| 自定义桶寻址模板 | `internal/client/lookup.go: CustomBucketLookup` |
| Action 核心 | `internal/action/common.go: Action` |
| 流式传输框架 | `internal/action/stream.go: RunStream` |
| 分片上传断点续传 | `internal/action/multipart-transfer.go: uploadMultipartFile` |
| 本地状态文件 | `internal/action/multipart-state.go` |
| mirror 主流程 | `internal/action/object-mirror.go: Mirror` |
| mirror 流式归并 | `internal/action/mirror-stream.go: streamDiff` |
| mirror 复制/删除 | `internal/action/mirror-copy.go` |
| mirror manifest | `internal/action/mirror-manifest.go` |
| diff 实现 | `internal/action/diff.go: Diff` |
| S3 客户端核心 | `internal/api/api.go: Client / Do` |
| SigV4 签名 | `internal/api/signer.go: signV4` |
| 桶寻址 URL 生成 | `internal/api/bucket-lookup.go: resolveURL / buildURL` |
| 请求重试 | `internal/api/api.go: Do / retryBackoff / isRetryable` |
| 错误解析 | `internal/api/error.go: parseErrorResponse` |
| S3 Select 事件流 | `internal/api/object-select.go` |
| S3Operations 接口 | `internal/api/operations.go` |
| 操作级 DTO 类型 | `internal/api/types.go` |
| 桶子资源 DTO | `internal/api/bucket-types.go` |
| 厂商差异开关 | `internal/api/quirks.go` |
| 双语机制 | `internal/i18n/i18n.go: T / Resolve` |
| 进度条 | `internal/progress/progress.go: Tracker` |
| 表格渲染 | `internal/fmtutil/table.go: Table` |
| CI 配置 | `.github/workflows/ci.yml` |
| 发布工作流 | `.github/workflows/release.yml` |
| 构建脚本 | `build.sh` |

### 10.3 扩展指南

#### 新增一个 S3 操作（API 层）

1. 在 `internal/api/operations.go` 的 `S3Operations` 接口添加方法签名（末尾的 `var _ S3Operations = (*Client)(nil)` 会立即校验实现是否齐备）
2. 在 `internal/api/types.go` 添加输入输出 DTO（桶子资源 DTO 放 `bucket-types.go`）
3. 在 `internal/api/` 新建文件实现该方法（构造 `requestMetadata` → `c.Do` → 解析响应）
4. 编写单元测试（`httptest.Server` 模拟）

> 若该操作在不同厂商间存在线协议差异，**不要写死**：在 `internal/api/quirks.go` 加一个开关（零值 = 严格 AWS 语义），在落点加一处判断，并在 `internal/config` 与 `internal/client` 贯通同名配置键。

#### 适配一个新的 S3 兼容厂商

1. 先在 `~/.s3cli` 的别名里试既有开关（`bucket_lookup` / `disable_region_redirect` / `lambda_notification_element` / `lifecycle_root_element` / `xmlns` / `force_unsigned_payload`）；
2. 仍不适配则在 `internal/api/quirks.go` 新增字段（保持零值 = 严格语义），落点只加一处判断；
3. 在 `internal/config/config.go` 加 TOML 键、`saveconf.go` 加"非默认才写"、`internal/client/client.go` 直通；
4. 补两个测试：`internal/api/quirks_test.go`（开关行为 + 零值不变）与 `internal/client/quirks_test.go`（配置到线上请求的贯通）。

#### 新增一个命令

1. 在 `cmd/` 新建文件，`init()` 中调用 `Register(groupID, title, NewXxxCmd)`
2. 构造 `*cobra.Command`，选择合适的 RunE 工厂（`NewRunE`/`NewRunEWithMode`/...）
3. 在 `internal/action/` 新建文件实现业务逻辑（作为 `Action` 的方法或包级函数）
4. 所有用户可见文案用 `i18n.T(en, zh)` 双语化
5. 添加 `--json` 支持时，用 `printJSONLine` 输出
6. 编写测试（mock `api.S3Operations`）
7. 若改动涉及线协议，在对应的 `internal/api/*_test.go` 里用 `httptest` 补断言

#### 新增一个桶子资源配置

参考现有的 `bucket cors` 实现：
- `cmd/cors.go`：命令定义（`set`/`get`/`del` 子命令）
- `internal/action/bucket-cors.go`：业务逻辑
- `internal/api/bucket-cors.go`：API 层
- `internal/api/bucket-types.go`：DTO 类型

---

> **文档版本**：对应 s3cli 主分支（2026-08-14）。如发现文档与代码不一致，以代码为准，并欢迎提交 PR 修正。

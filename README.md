# s3cli

轻量级、高性能的 S3 命令行客户端，兼容所有 S3 协议的对象存储（AWS S3、MinIO、Ceph RGW 等）。

## 特性

- **多端点支持** — 通过命名别名管理多个 S3 服务
- **灵活的桶寻址方式** — 支持 Path-style、DNS、自定义模板三种模式
- **递归操作** — 上传、下载、复制、移动、删除整个目录树
- **进度条** — 实时显示传输进度，支持滚动日志行
- **分片上传/下载** — 可配置分片大小和并发数
- **预签名 URL** — 生成限时访问链接（支持 v2 和 v4 签名）
- **对象版本管理** — 列出版本和删除标记
- **桶配置管理** — CORS、生命周期、策略、加密、版本控制、事件通知
- **跨端点镜像** — 不同端点间的单向同步
- **文件对比** — S3 之间、S3 与本地、本地与本地，支持按大小/ETag/MD5 比较
- **查找与树形展示** — 按名称/大小/时间搜索对象，可视化目录结构
- **管道上传** — 从标准输入流式上传到 S3 对象
- **Shell 自动补全** — Bash / Zsh / Fish

## 安装

### 下载预编译二进制

| 平台 | 文件名 |
|------|--------|
| Linux (amd64) | `s3cli-linux-amd64` |
| Linux (arm64) | `s3cli-linux-arm64` |
| macOS (amd64) | `s3cli-darwin-amd64` |
| macOS (arm64) | `s3cli-darwin-arm64` |
| Windows (amd64) | `s3cli-windows-amd64.exe` |

### 从源码编译

```bash
# 需要 Go 1.23+
git clone https://github.com/your-org/s3cli.git
cd s3cli
bash build.sh
# 编译产物在: ./output/s3cli-<系统>-<架构>
```

## 快速开始

### 1. 创建配置文件

默认路径为 `~/.s3cli`（可通过 `-f` 指定其他路径）。

```ini
[my-s3]
host_base = s3.amazonaws.com
access_key = AKIAIOSFODNN7EXAMPLE
secret_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
session_token =
# 桶寻址方式: path | dns | https://www.%(bucket).example.com
bucket_lookup = dns
region = us-east-1
verify_ssl = True
default_mime_type = binary/octet-stream
multipart_chunk_size_mb = 15
```

> **说明**：`bucket_lookup` 支持三种模式：
> - `path` — 路径风格：`host_base/bucket/key`
> - `dns` — 虚拟主机风格：`bucket.host_base/key`
> - `https://www.%(bucket).example.com` — 自定义模板，`%(bucket)` 为占位符

### 2. 列出桶

```bash
s3cli ls my-s3
```

输出示例：
```
2026-04-24 09:55   my-s3:my-bucket
2026-04-27 09:10   my-s3:another-bucket
```

### 3. 列出对象

```bash
# 列出顶层对象和"目录"
s3cli ls my-s3:my-bucket

# 递归列出所有对象
s3cli ls my-s3:my-bucket --all

# 列出指定前缀下的内容
s3cli ls my-s3:my-bucket/path/to/dir/
```

输出示例：
```
my-s3       2026-04-23 13:56:02       889786  FILE  my-bucket/data/report.pdf
my-s3       2026-04-23 13:56:13     11140665  FILE  my-bucket/data/archive.zip
my-s3                                      -  DIR   my-bucket/logs/
```

> 输出格式：`别名  时间  大小  类型  桶/路径`，别名列自动对齐。

## 命令参考

### 读取类命令

| 命令 | 说明 |
|------|------|
| `ls` | 列出桶或对象 |
| `du` | 查看磁盘占用（总大小 + 对象数量） |
| `info` | 查看桶或对象的元信息（ACL、标签、加密等） |
| `lsv` | 列出对象版本（包括删除标记） |
| `tree` | 以目录树形式展示对象 |
| `find` | 按名称、大小、时间搜索对象 |

#### 示例

```bash
# 查看磁盘占用
s3cli du my-s3:my-bucket/logs/

# 查看对象元信息（JSON 格式）
s3cli info my-s3:my-bucket/report.pdf --json

# 列出所有版本
s3cli lsv my-s3:my-bucket

# 树形展示（限制深度 + 显示文件大小）
s3cli tree my-s3:my-bucket -L 3 --size

# 按名称查找对象
s3cli find my-s3:my-bucket --name "*.log" --min-size 1024 --newer-than "2026-01-01"
```

### 对象操作命令

| 命令 | 说明 |
|------|------|
| `get` | 从 S3 下载对象 |
| `put` | 上传文件到 S3 |
| `cat` | 将对象内容输出到标准输出 |
| `pipe` | 从标准输入上传到 S3 |
| `cp` | 同一端点内复制对象 |
| `mv` | 同一端点内移动（重命名）对象 |
| `rm` | 删除对象 |
| `mirror` | 跨端点单向同步 |
| `diff` | 比较 S3/本地路径间的文件差异 |
| `mpu` | 管理进行中的分片上传 |
| `tag` | 管理桶和对象的标签 |

#### 示例

```bash
# 下载单个文件
s3cli get my-s3:my-bucket/report.pdf ./report.pdf

# 递归下载目录
s3cli get my-s3:my-bucket/logs/ ./logs/ -r

# 上传文件
s3cli put ./report.pdf my-s3:my-bucket/

# 递归上传目录
s3cli put ./data/ my-s3:my-bucket/backup/ -r

# 上传时指定 MIME 类型、存储级别和自定义元数据
s3cli put ./video.mp4 my-s3:my-bucket/ \
  --content-type video/mp4 \
  --storage-class STANDARD_IA \
  --metadata "author=张三,project=demo"

# 输出对象内容到终端
s3cli cat my-s3:my-bucket/config.json

# 从管道上传
tar czf - ./data/ | s3cli pipe my-s3:my-bucket/backup.tar.gz

# 同端点复制
s3cli cp my-s3:my-bucket/src/ my-s3:my-bucket/dst/ -r

# 同端点移动（重命名）
s3cli mv my-s3:my-bucket/old-name my-s3:my-bucket/new-name -r

# 删除
s3cli rm my-s3:my-bucket/tmp/ -r

# 跨端点镜像同步（覆盖差异 + 清理多余文件）
s3cli mirror src-s3:my-bucket/ dst-s3:my-bucket/ --overwrite --remove

# 比较两个目录的差异
s3cli diff my-s3:my-bucket/prod/ my-s3:my-bucket/staging/ --check md5

# 列出进行中的分片上传
s3cli mpu list my-s3:my-bucket

# 中止分片上传
s3cli mpu abort my-s3:my-bucket --upload-id "xxx"

# 设置标签
s3cli tag set --tag "env=prod,team=platform" my-s3:my-bucket/report.pdf

# 查看标签
s3cli tag get my-s3:my-bucket/report.pdf

# 删除标签
s3cli tag del my-s3:my-bucket/report.pdf
```

### 桶管理命令

| 命令 | 说明 |
|------|------|
| `mb` | 创建桶 |
| `rb` | 删除桶 |

#### 示例

```bash
# 创建桶
s3cli mb my-s3:new-bucket

# 创建桶并同时设置 CORS 和生命周期规则
s3cli mb my-s3:new-bucket --set-cors cors.json --set-lifecycle lifecycle.json

# 删除桶（--force 先清空所有对象）
s3cli rb my-s3:old-bucket --force
```

### 桶配置命令

| 命令 | 说明 |
|------|------|
| `cors` | 管理 CORS 跨域规则 |
| `encryption` | 设置默认加密（SSE-S3 / SSE-KMS） |
| `event` | 管理事件通知（SQS/SNS/Lambda） |
| `lifecycle` | 管理生命周期规则 |
| `policy` | 管理桶策略 |
| `version` | 管理版本控制（启用/暂停/查看） |

#### 示例

```bash
# CORS 跨域配置
s3cli cors set cors.json my-s3:my-bucket
s3cli cors get my-s3:my-bucket
s3cli cors del my-s3:my-bucket

# 默认加密
s3cli encryption set my-s3:my-bucket --algorithm AES256
s3cli encryption set my-s3:my-bucket --algorithm aws:kms --kms-key-id "arn:aws:kms:..."
s3cli encryption get my-s3:my-bucket
s3cli encryption del my-s3:my-bucket

# 生命周期
s3cli lifecycle set lifecycle.json my-s3:my-bucket
s3cli lifecycle get my-s3:my-bucket
s3cli lifecycle del my-s3:my-bucket

# 桶策略
s3cli policy set policy.json my-s3:my-bucket
s3cli policy get my-s3:my-bucket
s3cli policy del my-s3:my-bucket

# 版本控制
s3cli version enabled my-s3:my-bucket
s3cli version suspended my-s3:my-bucket
s3cli version info my-s3:my-bucket

# 事件通知
s3cli event set notification.json my-s3:my-bucket
s3cli event get my-s3:my-bucket
s3cli event del my-s3:my-bucket
```

### 工具类命令

| 命令 | 说明 |
|------|------|
| `signurl` | 生成预签名 URL |
| `alias` | 管理端点别名配置 |

#### 示例

```bash
# 生成预签名下载 URL（默认 7 天有效）
s3cli signurl my-s3:my-bucket/report.pdf

# 生成预签名上传 URL，1 小时有效
s3cli signurl my-s3:my-bucket/upload.zip -m PUT -e 3600

# 使用 v2 签名
s3cli signurl my-s3:my-bucket/file.txt --v2

# 列出所有别名
s3cli alias ls

# 添加新别名
s3cli alias set prod-s3 host_base=s3.amazonaws.com access_key=xxx secret_key=yyy region=us-east-1

# 删除别名
s3cli alias rm prod-s3
```

> **注意**：v4 签名最长有效期为 7 天（604800 秒）。如果 `--expire` 超过此值，会输出黄色警告但 URL 仍会正常生成。

## 全局参数

| 参数 | 说明 |
|------|------|
| `-f, --conf` | 配置文件路径（默认 `~/.s3cli`） |
| `--debug` | 输出 HTTP 请求/响应头信息 |
| `--no-color` | 关闭彩色输出 |
| `--logfile` | 输出到文件而不是标准输出 |
| `--scroll` | 进度条滚动行数，0 为全部显示（默认 5） |
| `-v, --version` | 查看版本号 |
| `-h, --help` | 查看帮助 |

## 配置文件参考

```ini
[别名]
host_base = s3.amazonaws.com           # S3 端点地址（host:port）
access_key = 你的_ACCESS_KEY            # 访问密钥 ID
secret_key = 你的_SECRET_KEY            # 秘密访问密钥
session_token =                         # 临时会话令牌（可选）
bucket_lookup = dns                     # 桶寻址方式: path | dns | 自定义模板
region = us-east-1                      # 区域，MinIO 等可用 "auto"
verify_ssl = True                       # 是否验证 SSL 证书
default_mime_type = binary/octet-stream # 默认 MIME 类型（无法自动识别时使用）
multipart_chunk_size_mb = 15            # 分片上传块大小（MB）
```

### 桶寻址方式详解

| 模式 | 配置值 | 实际请求格式 |
|------|--------|-------------|
| 路径风格 | `path` | `host_base/bucket/key` |
| 虚拟主机风格 | `dns` | `bucket.host_base/key` |
| 自定义模板 | `https://www.%(bucket).example.com` | `https://www.mydata.example.com/key` |

## 项目结构

```
s3cli/
├── cmd/s3cli/main.go          # 程序入口
├── pkg/
│   ├── action/                # 业务逻辑层（S3 操作）
│   │   ├── common.go          # S3Client 结构体、S3Path 路径格式化
│   │   ├── interface.go       # S3 操作接口定义（支持 mock 测试）
│   │   ├── stream.go          # 通用流式任务框架（生产者-消费者模式）
│   │   ├── object-*.go        # 对象操作（get/put/list/cat/pipe 等）
│   │   ├── bucket-*.go        # 桶操作（创建/删除/CORS 等）
│   │   ├── diff.go            # 文件对比引擎
│   │   └── utils.go           # API 错误格式化、MIME 类型注册
│   ├── client/                # S3 客户端工厂 + 缓存
│   │   ├── client.go          # NewS3Client 构造函数
│   │   ├── lookup-path.go     # 自定义桶端点解析器
│   │   └── utils.go           # 路径解析 + 客户端缓存
│   ├── cmd/                   # CLI 命令定义（cobra 框架）
│   │   ├── root.go            # 根命令 + 命令自注册机制
│   │   ├── common.go          # NewRunE / NewRunETwoPaths + 共享类型
│   │   └── *.go               # 各命令文件
│   ├── config/                # 配置加载
│   │   ├── config.go          # 配置结构体 + 桶寻址解析
│   │   └── loadconf.go        # INI 文件解析
│   ├── fmtutil/               # 输出格式化
│   │   ├── color.go           # ANSI 颜色 + 多目标输出
│   │   ├── print.go           # Printf/Println 封装
│   │   ├── progress.go        # 终端进度条
│   │   └── loger.go           # 调试/警告/错误日志
│   ├── httptracer/            # HTTP 请求/响应调试输出
│   ├── kvcache/               # 泛型内存缓存
│   └── utils/                 # 路径解析、文件读写工具
├── s3cli.ini                  # 示例配置文件
├── build.sh                   # 跨平台编译脚本
└── go.mod                     # Go 模块定义
```

## 开发指南：新增命令

s3cli 使用**命令自注册**模式。新增命令只需创建一个文件，无需修改 `root.go`。

在 `pkg/cmd/` 下新建 `mycommand.go`：

```go
package cmd

import (
    "s3cli/pkg/action"
    "s3cli/pkg/utils"
    "github.com/spf13/cobra"
)

// init 中注册命令，第一个参数是分组 ID，第二个是分组标题
func init() {
    Register("object", "Object Operations", NewMyCmd)
}

func NewMyCmd() *cobra.Command {
    var myOpt string                          // 命令专属参数
    opts := newCmdContext()                   // 创建上下文
    cmd := &cobra.Command{
        Use:   "mycmd [alias:bucket/key]",
        Short: "我的自定义操作",
        Args:  cobra.MinimumNArgs(1),
        RunE: NewRunE(ActionFunc(func(S3 action.S3Client, _ *CmdContext, s3path *utils.S3Path) error {
            // 通过 s3path.Bucket、s3path.Key 获取路径信息
            // 通过 myOpt 获取命令行参数
            return nil
        }), &opts),
    }
    cmd.Flags().StringVar(&myOpt, "my-flag", "", "参数说明")
    return cmd
}
```

编译后命令自动出现在 `--help` 中，无需其他任何修改。

## Shell 自动补全

```bash
# Bash
source <(s3cli completion bash)

# Zsh
source <(s3cli completion zsh)

# Fish
s3cli completion fish | source
```

## 使用 Ctrl+C 中断

所有命令支持 `Ctrl+C` 安全中断。中断时会静默退出，不会打印错误信息。

## 许可证

Apache 2.0 LICENSE


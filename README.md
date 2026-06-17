# s3cli

轻量、高性能的 S3 命令行客户端，兼容各类 S3 对象存储（AWS S3、MinIO、Ceph RGW 等）。

## 特性

- 通过命名别名管理多个 S3 端点
- 支持 Path-style、DNS、自定义模板三种桶寻址方式
- 上传 / 下载 / 复制 / 移动 / 删除，支持递归整个目录树
- 实时进度条、分片上传下载
- 预签名 URL（v2 / v4 签名）
- 桶配置管理：CORS、生命周期、策略、加密、版本控制、事件通知
- 跨端点镜像同步、文件差异对比
- 查找、树形展示、管道上传
- Bash / Zsh / Fish 自动补全

## 安装

从源码编译（需要 Go 1.25+）：

```bash
git clone https://github.com/your-org/s3cli.git
cd s3cli
bash build.sh
# 产物位于 ./output/s3cli-<系统>-<架构>
```

## 配置

默认读取 `~/.s3cli`，也可用 `-f` 指定其他路径。

```ini
[my-s3]
host_base = s3.amazonaws.com
access_key = AKIAIOSFODNN7EXAMPLE
secret_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
bucket_lookup = dns          # 桶寻址方式: path | dns | 自定义模板
region = us-east-1
verify_ssl = True
multipart_chunk_size_mb = 15
```

`bucket_lookup` 三种模式：

| 模式 | 配置值 | 请求格式 |
|------|--------|----------|
| 路径风格 | `path` | `host_base/bucket/key` |
| 虚拟主机风格 | `dns` | `bucket.host_base/key` |
| 自定义模板 | `https://www.%(bucket).example.com` | `https://www.mybucket.example.com/key` |

也可以用 `alias` 命令直接管理：

```bash
s3cli alias ls
s3cli alias set prod host_base=s3.amazonaws.com access_key=xxx secret_key=yyy region=us-east-1
s3cli alias rm prod
```

> 路径格式统一为 `别名:桶/路径`，例如 `my-s3:my-bucket/dir/file.txt`。

## 快速开始

```bash
s3cli ls my-s3                          # 列出所有桶
s3cli ls my-s3:my-bucket                # 列出对象
s3cli ls my-s3:my-bucket --all          # 递归列出
s3cli get my-s3:my-bucket/a.pdf ./      # 下载
s3cli put ./a.pdf my-s3:my-bucket/      # 上传
```

## 命令一览

### 读取

| 命令 | 说明 |
|------|------|
| `ls` | 列出桶或对象 |
| `du` | 查看磁盘占用 |
| `info` | 查看桶/对象元信息 |
| `lsv` | 列出对象版本（含删除标记） |
| `tree` | 树形展示对象 |
| `find` | 按名称/大小/时间搜索 |
| `diff` | 比较路径间文件差异 |

### 对象操作

| 命令 | 说明 |
|------|------|
| `get` / `put` | 下载 / 上传，支持 `-r` 递归 |
| `cp` / `mv` / `rm` | 同端点复制 / 移动 / 删除 |
| `cat` / `pipe` | 输出到标准输出 / 从标准输入上传 |
| `mpu` | 管理进行中的分片上传 |
| `tag` | 管理桶和对象标签 |
| `mirror` | 跨端点单向同步 |

### 桶管理与配置

| 命令 | 说明 |
|------|------|
| `mb` / `rb` | 创建 / 删除桶 |
| `cors` | CORS 跨域规则 |
| `encryption` | 默认加密（SSE-S3 / SSE-KMS） |
| `lifecycle` | 生命周期规则 |
| `policy` | 桶策略 |
| `version` | 版本控制 |
| `event` | 事件通知 |

### 工具

| 命令 | 说明 |
|------|------|
| `signurl` | 生成预签名 URL |
| `alias` | 管理端点别名 |

## 常用示例

```bash
# 递归上传 / 下载目录
s3cli put ./data/ my-s3:my-bucket/backup/ -r
s3cli get my-s3:my-bucket/logs/ ./logs/ -r

# 上传时指定类型与元数据
s3cli put ./video.mp4 my-s3:my-bucket/ --content-type video/mp4 --metadata "author=tom"

# 管道上传
tar czf - ./data/ | s3cli pipe my-s3:my-bucket/backup.tar.gz

# 跨端点镜像（覆盖差异并清理多余文件）
s3cli mirror src:bucket/ dst:bucket/ --overwrite --remove

# 生成预签名 URL（默认 7 天）
s3cli signurl my-s3:my-bucket/a.pdf
s3cli signurl my-s3:my-bucket/up.zip -m PUT -e 3600

# 桶配置（多数支持 set / get / del 子命令）
s3cli cors set cors.json my-s3:my-bucket
s3cli lifecycle get my-s3:my-bucket
s3cli version enabled my-s3:my-bucket
```

## 全局参数

| 参数 | 说明 |
|------|------|
| `-f, --conf` | 配置文件路径（默认 `~/.s3cli`） |
| `--debug` | 输出 HTTP 请求摘要 |
| `--no-color` | 关闭彩色输出 |
| `-H, --header` | 添加自定义 HTTP 头（可重复） |
| `-v, --version` | 查看版本 |
| `-h, --help` | 查看帮助 |

## 自动补全

```bash
source <(s3cli completion bash)   # Bash
source <(s3cli completion zsh)    # Zsh
s3cli completion fish | source    # Fish
```

所有命令均支持 `Ctrl+C` 安全中断。

## 许可证

MIT License，详见 [LICENSE](LICENSE)。

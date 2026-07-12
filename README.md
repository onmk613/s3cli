# s3cli

轻量、高性能的 S3 命令行客户端，兼容各类 S3 对象存储（AWS S3、MinIO、Ceph RGW 等）。

## 特性

- 通过命名别名管理多个 S3 端点
- 支持 ConfPath-style、DNS、自定义模板三种桶寻址方式
- 上传 / 下载 / 复制 / 移动 / 删除，支持递归整个目录树
- 实时进度条、分片上传下载
- 桶配置管理：CORS、生命周期、策略、加密、版本控制、事件通知
- 跨端点镜像同步、文件差异对比
- 查找、树形展示、管道上传
- Bash / Zsh / Fish 自动补全

## 安装
```bash
# Go Version 1.25+
git clone https://github.com/onmk613/s3cli.git
cd s3cli && bash build.sh
mv ./s3cli /usr/local/bin/
s3cli help
```

## Help
```txt
s3cli is a fast, dependency-free CLI for any S3-compatible object storage.
Usage:
  s3cli [flags]
  s3cli [command]

Endpoint Management
  alias       Manage aliases (S3 endpoint configurations)

Bucket Management
  mb          Create new bucket(s)
  rb          Remove bucket(s)

Bucket Configuration
  cors        Manage CORS configuration for bucket(s)
  encryption  Manage bucket(s) default encryption (SSE-S3 / SSE-KMS)
  event       Manage object notifications
  lifecycle   Manage lifecycle rules
  policy      Manage bucket policy
  versioning  Manage bucket versioning

Read Commands
  diff        Compare files/directories between s3 and/or local paths
  du          Show disk usage of buckets or paths
  find        Search objects by name pattern, size and modification time
  info        Show information about bucket(s) or object(s)
  ls          List objects or buckets
  lsv         List object versions (including delete markers)
  tree        Display objects as a tree of directories

Object Operations
  cat         Print object contents to stdout
  cp          Copy object(s) within the same S3 endpoint
  get         Download object(s) from S3
  mpu         Manage in-progress multipart uploads
  mv          Move object(s) within the same S3 endpoint
  pipe        Upload data from stdin to an S3 object
  put         Upload file(s) to S3
  rm          Delete object(s) from S3
  tag         Manage tags for buckets and objects

Synchronization
  mirror      Synchronize objects from source to target (one-way sync)

Tools
  signurl     Print pre-signed S3 URLs

Additional Commands:
  help        Help about any command

Flags:
  -f, --conf string                ConfPath to configuration file (default ~/.s3cli)
      --debug                      Print summarized S3 requests
  -H, --header stringArray         Add a custom HTTP header (key:value), can repeat
  -h, --help                       help for s3cli
      --no-color                   Disable color output
      --user-agent string          Override the HTTP User-Agent header
      --user-agent-suffix string   Append extra content to the HTTP User-Agent header
  -v, --version                    version for s3cli
```

## 配置
```bash
s3cli alias help
```

> 路径格式统一为 `别名:桶/路径`，例如 `my-s3:my-bucket/dir/file.txt`。

大于等于 64 MiB 的本地文件会自动使用 Multipart Upload。传输中断后，保持本地文件内容和修改时间不变，再次执行相同的 `put` 命令会从 `$HOME/.s3cli/mpu/` 中保存的安全状态恢复；服务端已上传分片会重新校验后再继续。

`mirror --remove` 可配合以下保护项使用：

```bash
s3cli --request-timeout 10m mirror source:bucket/ target:bucket/ \
  --include 'logs/*' --exclude '*.tmp' --max-delete 100
```

## 测试

```bash
make test
make test-race
make coverage
make build
```

使用 MinIO 运行 S3 兼容性冒烟测试：

```bash
docker compose -f docker-compose.integration.yml up -d minio
S3_TEST_ENDPOINT=http://127.0.0.1:9000 \
S3_TEST_ACCESS_KEY=minioadmin \
S3_TEST_SECRET_KEY=minioadmin \
go test -tags=integration ./tests/integration
docker compose -f docker-compose.integration.yml down
```

同一套集成测试也可用于 Ceph RGW 或 AWS S3；设置对应 endpoint、access key、secret key，必要时设置 `S3_TEST_REGION`。测试会创建并清理一个唯一 bucket，因此只应使用专用的测试账号。

## 许可证

MIT License，详见 [LICENSE](LICENSE)。

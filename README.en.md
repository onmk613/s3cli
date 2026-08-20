# s3cli

A lightweight, high-performance, simple S3 command-line client

English · [中文](README.md)

## Features

- Zero AWS SDK dependency: implements the S3 REST API and SigV4 signing from scratch on top of `net/http`; compatible with any S3 service
- Real multipart resume management (local state file + server-side `ListParts` reconciliation-based resume)
- Three bucket addressing styles: path-style, DNS, and custom templates (`%(bucket)`/`%(region)`, with region detection)
- Upload / download / copy / move / delete, with support for recursing entire directory trees; stdin pipe upload (`put -`), stdout streaming output (`get -`)
- Real-time progress bar (rate/ETA, terminal-width adaptive), multipart upload and download
- Bucket configuration management: CORS, lifecycle, policy, encryption, versioning, event notifications, tags, ACL, Object Lock, replication
- Cross-endpoint mirror sync (zero-copy within the same endpoint / streaming across endpoints, manifest-based resume), file diffing (size/quick/md5)
- Find (`find`), tree display (`tree`), disk usage (`du`), S3 Select (`sql`), archive restore (`restore`), presigned URLs (`share`)
- Structured JSON output, bilingual (Chinese/English) help, Bash / Zsh / Fish / PowerShell autocompletion

## Installation

```bash
# Go Version 1.26+
git clone https://github.com/onmk613/s3cli.git
cd s3cli && bash build.sh
mv ./s3cli /usr/local/bin/
s3cli help
```

## Quick Start

```bash
# 1. Configure an endpoint (alias)
s3cli alias add my-s3 https://s3.example.com AKIA... SECRET

# 2. Common operations
s3cli ls my-s3:                             # List all buckets
s3cli ls my-s3:my-bucket/                   # List objects in a bucket
s3cli put ./data my-s3:my-bucket/backup/    # Upload a directory (recursive)
s3cli get my-s3:my-bucket/backup ./out/     # Download a directory
s3cli mirror my-s3:prod my-s3:backup        # Sync (server-side copy within the same endpoint)
cat access.log | s3cli put - my-s3:logs/access.log   # Upload via stdin pipe
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

> `--json` and `--quiet` are hidden global flags: `--json` only appears in the help
> of `ls`, `du`, `stat`, `info`, `find`, `tree`, `diff`, `bucket lifecycle list`,
> `tag list`, `mpu list`, and `mpu local-list`;
> `--quiet` only appears in the help of get/put/cp/mv/mirror (commands with a
> progress bar).
> Both flags still work on every other subcommand (behavior unchanged); they are
> simply not shown in the help output.
> `--show-secret` is available on the `alias list` subcommand only.

## Exit Codes

Script-friendly: returns semantic exit codes by error type.

| Exit code | Meaning |
| --- | --- |
| 0 | Success |
| 1 | Generic error |
| 4 | Object/bucket does not exist (404 / NoSuchKey) |
| 5 | Permission denied (403 / AccessDenied) |
| 6 | `diff` found differences (not an error; for script logic) |
| 130 | Interrupted by SIGINT (Ctrl+C) |

## JSON Output

`--json` prints structured results (JSON lines or a single document) for supported
commands: `ls`, `du`, `stat`, `info`, `find`, `tree`, `bucket lifecycle list`,
`tag list`, `mpu list`, `mpu local-list`, and `diff`. All other commands are
marked `(text output only)` in their `--help`.

## Configuration

```bash
s3cli alias help
```

> Paths use the unified `alias:bucket/path` format, e.g.
> `my-s3:my-bucket/dir/file.txt`.

Configuration file (`~/.s3cli`, TOML):

```toml
[my-s3]
host_base = "https://s3.example.com"
access_key = "AKIA..."
secret_key = "secret"
session_token = ""          # Temporary credentials (STS); optional

# Bucket addressing: "path" (default) / "dns" / custom template (with %(bucket), optional %(region))
# %(region) triggers detection of the bucket's actual region
bucket_lookup = "path"
# bucket_lookup = "dns"
# bucket_lookup = "https://www.%(bucket).example.com"

# Optional tuning
region = "us-east-1"
no_verify_ssl = false
default_mime_type = "application/octet-stream"
multipart_chunk_size_mb = 15
max_retries = 3
tls_min_version = "1.2"     # 1.0 / 1.1 / 1.2 / 1.3, default 1.2; relax to 1.0/1.1 for legacy endpoints
```

## S3 Backend

All underlying operations are abstracted behind the `pkg/s3iface.S3Operations`
interface, implemented by the self-built request client `s3api.Client`
(HTTP + SigV4 signing, zero SDK dependency, compatible with any S3 service).

```bash
./build.sh                # Build for the current platform
./build.sh all            # Cross-compile for all platforms
go test ./...             # Unit tests (self-contained, no real S3 needed)
bash scripts/e2e-minio.sh # MinIO end-to-end subcommand tests (295 assertions, auto-cleanup)
```

## License

MIT License; see [LICENSE](LICENSE).

# s3cli

A lightweight, high-performance, simple S3 command-line client

English · [中文](README.md)

## Features

- Zero AWS SDK dependency: implements the S3 REST API and SigV4 signing from scratch on top of `net/http`; compatible with any S3 service
- Real multipart resume management (local state file + server-side `ListParts` reconciliation-based resume)
- Three bucket addressing styles: path-style, DNS, and custom templates (`%(bucket)`/`%(region)`, with region detection)
- Upload / download / copy / move / delete, with support for recursing entire directory trees; stdin pipe upload (`put -`), stdout streaming output (`get -`)
- Real-time progress bar (rate/ETA, terminal-width adaptive), multipart upload and download
- Cross-endpoint mirror sync (zero-copy within the same endpoint / streaming across endpoints, manifest-based resume), file diffing (size/quick/md5)
- Find (`find`), tree display (`tree`), disk usage (`du`), S3 Select (`sql`), archive restore (`restore`), presigned URLs (`share`)
- Bucket/object subresources: CORS, lifecycle, policy, encryption, versioning, notifications, tags,
  **ACL**, **Object Lock (bucket default retention + object retention / legal hold)**, **replication**,
  **public access block**
- Integrity: `put --checksum` (CRC32/CRC32C/SHA1/SHA256 additional checksums), `get --checksum`
  (verify after download)
- Safety preview: `--dry-run` on `put` / `get` / `cp` / `mv` / `rm` / `mirror` / `bucket remove`
- Parallel per-object multipart upload (4 concurrent parts per file)
- Structured JSON output (global `--json`), bilingual (Chinese/English) help, Bash / Zsh / Fish / PowerShell autocompletion

## Installation

```bash
# Go Version 1.27+
git clone https://github.com/onmk613/s3cli.git
cd s3cli && bash build.sh
mv ./bin/s3cli /usr/local/bin/
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
A lightweight S3 command-line client.

Manage buckets and objects across multiple S3-compatible endpoints via aliases.

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
  stat        Show metadata about bucket or object
  tree        Display objects as a tree of directories

Object Operations
  cp          Copy object within the same S3 endpoint
  get         Download object(s) from S3
  mpu         Manage in-progress multipart uploads
  mv          Move object within the same S3 endpoint
  object      Manage object subresources (ACL / Object Lock)
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
  -f, --conf string                Path to configuration file
      --debug                      Print summarized S3 requests
  -H, --header stringArray         Add a custom HTTP header (key:value), can repeat
  -h, --help                       help for s3cli
      --host-base string           Override the endpoint host for all aliases
      --json                       Output structured JSON (supported commands only; ignored elsewhere)
      --lang string                Help language: auto (detect from timezone/locale) | en | zh (default "en")
      --no-color                   Disable color output
      --no-verify-ssl              Skip TLS certificate verification
      --user-agent string          Override the HTTP User-Agent header
      --user-agent-suffix string   Append extra content to the HTTP User-Agent header
  -v, --version                    version for s3cli

Use "s3cli [command] --help" for more information about a command.
```

> `--json` is a true global flag: commands that support structured output read it, and
> the rest ignore it silently, so scripts can always pass `--json` without per-command
> checks. See the JSON output section below for the list of supported commands.
> `--show-secret` is available on `alias list` only.
>
> **Numeric bounds**: `--concurrency` accepts 1-1024; `--part-size` /
> `multipart_chunk_size_mb` accept 5-1024 (MB). Out-of-range values fail fast
> instead of crashing or allocating an enormous buffer (every in-flight part
> holds a full in-memory buffer).

## Exit Codes

Script-friendly: returns semantic exit codes by error type.

| Exit code | Meaning |
| --- | --- |
| 0 | Success |
| 1 | Generic error |
| 4 | Object/bucket does not exist (404 / NoSuchKey / NoSuchBucket / NoSuchUpload) |
| 5 | Permission denied (403 / AccessDenied) |
| 6 | `diff` found differences (not an error; for script logic) |
| 130 | Interrupted by SIGINT (Ctrl+C) |

## JSON Output

`--json` is a global flag that prints structured results (JSON lines or a single
document) for supported commands: `ls`, `du`, `stat`, `find`, `tree`,
`diff`, `bucket lifecycle list`, `bucket acl get`, `object acl get`, `tag list`,
`mpu list`, and `mpu local-list`. All other commands keep printing text and
silently ignore `--json`.

> `info` is not in the list above: it **always** prints JSON (legacy behaviour),
> so it neither reads nor needs `--json`.
>
> Per-command field schemas live in [docs/OUTPUT_SCHEMA.md](docs/OUTPUT_SCHEMA.md).

`policy get --raw` is a separate local flag (print the raw policy JSON instead of
the classified type). It deliberately does not reuse the name `--json`.

## Configuration

```bash
s3cli alias help
```

> Paths use the unified `alias:bucket/path` format, e.g.
> `my-s3:my-bucket/dir/file.txt`.
>
> `host_base` **must include an explicit scheme** (`https://` or `http://`).
> Omitting it is now an error instead of silently defaulting to plaintext
> `http://`, which would send credentials and object data unencrypted.
> `http://` is still allowed (e.g. MinIO/Ceph on a LAN) but prints a warning.

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
multipart_chunk_size_mb = 15  # part size, 5-1024
max_retries = 3
tls_min_version = "1.2"     # 1.0 / 1.1 / 1.2 / 1.3, default 1.2; relax to 1.0/1.1 for legacy endpoints

# Vendor-difference switches (optional; defaults are strict AWS semantics,
# only needed for specific S3-compatible implementations)
# lifecycle_root_element = "LifecycleConfiguration"  # some vendors want BucketLifecycleConfiguration
# lambda_notification_element = "cloud"              # cloud = <CloudFunctionConfiguration> (default) / lambda
# disable_region_redirect = false                    # true = ignore X-Amz-Bucket-Region re-signing
# disable_region_probe = false                       # true = do not probe ?location for %(region) templates
# force_unsigned_payload = false                     # true = x-amz-content-sha256: UNSIGNED-PAYLOAD
# xmlns = "http://s3.amazonaws.com/doc/2006-03-01/"  # override namespace for CORS / lifecycle XML
```

## S3 Backend

All underlying operations are abstracted behind the `S3Operations` interface in
`internal/api` (`operations.go`), implemented by `api.Client` in the same package
(HTTP + SigV4 signing, zero SDK dependency, compatible with any S3 service).

To support a particular S3-compatible vendor, prefer the **vendor-difference
switches** above over pulling in another SDK: a difference becomes an alias
config entry, defaults keep strict AWS semantics, and upgrades never silently
change the requests you send.

```bash
./build.sh       # Build for the current platform (output in bin/)
./build.sh all   # Cross-compile for all platforms
go test ./...    # Tests (self-contained, no real S3 endpoint needed)
go vet ./...     # Static checks
```

## Test naming convention

- Unit tests mirror the source file: `bucket-cors.go` → `bucket-cors_test.go`.
- Scenario / regression tests (end-to-end semantics spanning several files) use
  underscores: `mirror_flow_test.go`, `resume_integrity_test.go`.

## License

MIT License; see [LICENSE](LICENSE).

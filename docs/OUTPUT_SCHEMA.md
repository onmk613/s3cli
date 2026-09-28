# `--json` 输出契约

> 本文是 `s3cli --json` 的输出 schema 参考。`internal/action/render`、`diff.go`、
> `json_output_test.go` 的注释都指向本文 —— 它是脚本消费方可依赖的稳定契约。

## 1. 总则

`--json` 是**根命令的持久 flag**，对所有命令可见：

- 支持结构化输出的命令读取它并把结果写进各自的 Options；
- 不支持的命令**静默忽略**它，继续输出文本。

因此脚本可以无条件加 `--json`，不必逐命令判断是否支持。

```bash
s3cli ls --json a:b/bucket/            # JSON lines
s3cli --json find a:b/bucket/prefix    # 全局位置同样有效
s3cli --json put ./f a:b/k             # 不支持 -> 忽略, 输出文本
```

**实现约束**：`--json` 已从"每命令各自声明"改为全局 flag，因此每个支持的命令
都**必须**在自己的 `RunE` 里显式执行一次 `opt.JSON = config.G.F.JSON`。漏掉这一句
不会编译报错、action 层单测也照样全绿（它们直接构造 `Options{JSON: true}`），
但 CLI 上 `--json` 会被静默忽略。`cmd/json_wiring_test.go` 专门锁定这一点。

## 2. 两种输出形态

| 形态 | 说明 | 适用 |
| --- | --- | --- |
| **JSON lines** | 每行一个独立、紧凑的 JSON 对象，无外层数组 | 逐条记录（对象、版本、目录、标签…） |
| **JSON document** | 单个缩进多行 JSON 文档 | 整体结果（info、资源子配置、diff 汇总） |

JSON lines 便于流式消费，且**内存恒定**（不需要把全部结果先攒成数组）：

```bash
s3cli ls -r --json a:b/bucket/ | while read -r line; do jq -r .path <<<"$line"; done
```

## 3. 支持的命令与 schema

所有记录都带一个 `kind` 或 `type` 判别字段（部分命令省略），用于在多形态输出中
区分行类型。路径统一为 `alias:bucket/key`（与命令行写法一致）。

### `ls`

| 场景 | 字段 |
| --- | --- |
| 列出桶 | `kind`,`name`,`creationDate` |
| 列出对象 | `kind`,`path`,`size`,`lastModified`,`storageClass`,`etag` |
| 目录项 | `kind`,`path` |
| `--summarize` | `kind`,`path`,`count`,`totalSize` |
| `--incomplete` | `kind`,`path`,`initiated`,`uploadId` |
| `--versions` | `kind`,`path`,`versionId`,`isLatest`,`isDeleteMarker`,`size`,`lastModified` |

### `du`

`kind`,`path`,`fileNum`,`size`；`-r` 时每行额外带 `dir`。

### `find`

`path`,`size`,`etag`,`type`,`storageClass`,`lastModified`,`versionId`,`isDeleteMarker`

`type` 取 `file`；`--versions` 时区分版本与删除标记。

### `tree`

汇总行：`path`,`directories`,`files`,`totalSize`；树体在 `tree` 字段中。

### `stat`

对象：`status`,`name`,`lastModified`,`size`,`etag`,`type`,`metadata`

桶：`status`,`name`,`createdAt`,`type`,`versioning`,`location`,`anonymous`,`ilm`,`usage`,`totalSize`

### `diff`

单个 JSON 文档：

```json
{
  "mode": "md5", "a": "...", "b": "...",
  "differ": [], "onlyA": ["f1.txt"], "onlyB": ["f2.txt"], "identical": [],
  "failed": 0, "differCount": 0, "onlyACount": 1, "onlyBCount": 1,
  "identicalCount": 0
}
```

### `tag list`

`path`,`tags`（`tags` 为 `[{"key":…,"value":…}]`）

### `bucket lifecycle list`

生命周期规则数组（见 `internal/action/bucket-lifecycle.go` 的 JSON 分支）。

### `bucket acl get`

`{"acl": "<原始 XML 字符串>"}` —— ACL 的表示就是 XML，不做二次建模。

### `object acl get`

同上，`acl` 为原始 XML。

### `mpu list`

`path`,`bucket`,`key`,`uploadId`,`initiated`

### `mpu local-list`

单个 JSON 文档：本地断点上传状态数组（`upload_id`,`bucket`,`key`,`local_path`,
`total_size`,`created_at`,`state_path`）。

## 4. 退出码与 JSON 无关

结构化输出不改变退出码语义（见 README 的退出码表）。特别注意：

- `diff` 发现差异时以 **6** 退出 —— 这是**正常结果**而非错误，JSON 文档照常输出；
- `ls`/`find` 等**没有匹配项**时退出码为 **0**，JSON lines 为空（零行）。

## 5. 稳定性

- 字段**只增不改不删**；新增字段不视为破坏性变更。
- 字段名固定为 camelCase（`lastModified`）或语义化小写（`path`、`tags`），
  与 Go 结构的 JSON tag 一一对应，不做重命名。
- `internal/action/render.JSONLine` / `JSONDoc` 是唯一出口；任何绕过它们的
  手写 `json.Marshal` 都会被 `internal/action/json_output_test.go` 的
  "每行必须是合法 JSON" 断言拦下。

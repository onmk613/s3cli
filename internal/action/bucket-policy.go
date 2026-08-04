// bucket-policy.go 实现桶访问策略 (Bucket Policy) 管理, 对齐 mc 的 `mc anonymous`:
// SetPolicy (permission 参数直接生成策略, 不依赖文件) / GetPolicy / DelPolicy.
//
// permission 取值与 mc 一致: private / download / upload / public.
// 兼容旧名: none -> private, public-read -> download, public-write -> upload,
// public-read-write -> public. 策略语句结构对齐 minio-go (mc 底层):
//
//	download: GetBucketLocation + ListBucket + GetObject
//	upload:   GetBucketLocation + ListBucketMultipartUploads + Put/Delete/Abort/ListParts
//	public:   以上并集
//	private:  删除策略

package action

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/s3iface"
)

// PolicyOptions 控制 SetPolicy 的参数.
//
// 三选一: 指定 ConfigFile 用自定义 JSON 文件覆盖;
// 或指定 Permission 套用预定义策略 (mc 语义); 否则报错.
// Prefix 仅对预定义策略生效, 把策略限定到某个 key 前缀.
type PolicyOptions struct {
	Permission string // private / download / upload / public (+ 兼容旧名)
	Prefix     string // 限定预定义策略生效的 key 前缀
	ConfigFile string // 自定义策略 JSON 文件; 非空时覆盖 Permission/Prefix
}

// SetPolicy 设置桶策略: ConfigFile 非空时从自定义 JSON 文件加载,
// 否则按 Permission 套用预定义 (canned) 策略. 两者必须给出其一.
func (c *Action) SetPolicy(opt PolicyOptions, bucket string) error {
	if opt.ConfigFile != "" {
		return c.setPolicyFromFile(opt.ConfigFile, bucket)
	}
	if opt.Permission == "" {
		return fmt.Errorf("policy set: either --permission/-p PERMISSION or -f/--from-file is required")
	}
	return c.applyCannedPolicy(opt.Permission, bucket, opt.Prefix)
}

// setPolicyFromFile 从本地 JSON 文件读取策略并设置到桶上.
func (c *Action) setPolicyFromFile(policyFile, bucket string) error {
	data, _, err := loadAWSConfigFile(policyFile)
	if err != nil {
		return err
	}
	if err := validateJSON(data); err != nil {
		return err
	}
	if err := c.S3.SetBucketPolicy(c.Ctx, bucket, data); err != nil {
		return FormatAPIError(err)
	}

	myprint.PrintfBoldGreen("Policy set for %s\n", c.S3Path(bucket, ""))
	return nil
}

// GetPolicy 读取并以 pretty JSON 打印桶的访问策略.
func (c *Action) GetPolicy(bucket string) error {
	raw, err := c.S3.GetBucketPolicy(c.Ctx, bucket)
	if err != nil {
		return FormatAPIError(err)
	}
	myprint.PrintfBoldBlue("# %s policy:\n", c.S3Path(bucket, ""))

	var pretty bytes.Buffer
	if err := json.Indent(&pretty, raw, "", "  "); err == nil {
		myprint.PrintlnGreen(pretty.String())
	} else {
		myprint.PrintlnGreen(string(raw))
	}
	return nil
}

// DelPolicy 删除桶的访问策略.
func (c *Action) DelPolicy(bucket string) error {
	if err := c.S3.DeleteBucketPolicy(c.Ctx, bucket); err != nil {
		return FormatAPIError(err)
	}

	myprint.PrintfBoldGreen("Policy deleted for %s: success\n", c.S3Path(bucket, ""))
	return nil
}

// ----------------------------------------------------------------------------
// 预定义策略 (mc anonymous 语义)
// ----------------------------------------------------------------------------

// normalizePermission 把权限名归一化: 旧名映射到 mc 语义.
func normalizePermission(name string) (string, error) {
	switch name {
	case "private", "none":
		return "private", nil
	case "download", "public-read":
		return "download", nil
	case "upload", "public-write":
		return "upload", nil
	case "public", "public-read-write":
		return "public", nil
	}
	return "", fmt.Errorf("unknown permission %q: allowed values are [private, download, upload, public]", name)
}

// anonStatement / anonPolicy 按 minio-go 的 JSON 结构构造策略.
type anonPrincipal struct {
	AWS []string `json:"AWS"`
}

type anonCondition struct {
	StringEquals map[string][]string `json:"StringEquals,omitempty"`
}

type anonStatement struct {
	Action    []string       `json:"Action"`
	Condition *anonCondition `json:"Condition,omitempty"`
	Effect    string         `json:"Effect"`
	Principal anonPrincipal  `json:"Principal"`
	Resource  []string       `json:"Resource"`
	Sid       string         `json:"Sid,omitempty"`
}

type anonPolicy struct {
	Version    string          `json:"Version"`
	Statements []anonStatement `json:"Statement"`
}

const (
	actionGetBucketLocation   = "s3:GetBucketLocation"
	actionListBucket          = "s3:ListBucket"
	actionListBucketMultipart = "s3:ListBucketMultipartUploads"
	actionGetObject           = "s3:GetObject"
	actionPutObject           = "s3:PutObject"
	actionDeleteObject        = "s3:DeleteObject"
	actionAbortMultipart      = "s3:AbortMultipartUpload"
	actionListMultipartParts  = "s3:ListMultipartUploadParts"
)

// buildAnonymousPolicy 按 mc (minio-go) 语义构造匿名访问策略 JSON, 语句结构与 mc
// 完全一致 (含 GetBucketLocation+ListBucketMultipartUploads 合并语句与
// ListBucket 的 StringEquals s3:prefix 条件).
// prefix 为空时作用于整个桶, 否则仅作用于该前缀下的对象.
func buildAnonymousPolicy(perm, bucket, prefix string) ([]byte, error) {
	bucketRes := "arn:aws:s3:::" + bucket
	objRes := bucketRes + "/" + prefix + "*"

	allow := "Allow"
	principal := anonPrincipal{AWS: []string{"*"}}

	// 桶级语句: GetBucketLocation 与 ListBucketMultipartUploads 合并为一条 (同 mc).
	common := anonStatement{
		Action:    []string{actionGetBucketLocation, actionListBucketMultipart},
		Effect:    allow,
		Principal: principal,
		Resource:  []string{bucketRes},
	}
	if perm == "download" {
		common.Action = []string{actionGetBucketLocation}
	}
	readBucket := anonStatement{
		Action:    []string{actionListBucket},
		Effect:    allow,
		Principal: principal,
		Resource:  []string{bucketRes},
	}
	if prefix != "" {
		readBucket.Condition = &anonCondition{StringEquals: map[string][]string{"s3:prefix": {prefix}}}
	}

	readObj := anonStatement{
		Action:    []string{actionGetObject},
		Effect:    allow,
		Principal: principal,
		Resource:  []string{objRes},
	}
	writeObj := anonStatement{
		Action:    []string{actionAbortMultipart, actionDeleteObject, actionListMultipartParts, actionPutObject},
		Effect:    allow,
		Principal: principal,
		Resource:  []string{objRes},
	}
	rwObj := anonStatement{
		Action:    []string{actionAbortMultipart, actionDeleteObject, actionGetObject, actionListMultipartParts, actionPutObject},
		Effect:    allow,
		Principal: principal,
		Resource:  []string{objRes},
	}

	var stmts []anonStatement
	switch perm {
	case "download":
		stmts = []anonStatement{common, readBucket, readObj}
	case "upload":
		stmts = []anonStatement{common, writeObj}
	case "public":
		stmts = []anonStatement{common, readBucket, rwObj}
	default:
		return nil, fmt.Errorf("unknown permission %q", perm)
	}

	return json.Marshal(anonPolicy{Version: "2012-10-17", Statements: stmts})
}

// applyCannedPolicy 应用预定义策略. private 删除策略恢复私有 (无策略时幂等);
// 其余写入对应的匿名访问授权, prefix 非空时仅对该 key 前缀下的对象生效.
func (c *Action) applyCannedPolicy(name, bucket, prefix string) error {
	perm, err := normalizePermission(name)
	if err != nil {
		return err
	}

	if perm == "private" {
		if err := c.S3.DeleteBucketPolicy(c.Ctx, bucket); err != nil {
			// 无策略 (已私有) 视为成功, 保持幂等。
			var apiErr *s3iface.ErrorResponse
			if !(errors.As(err, &apiErr) && (apiErr.Code == "NoSuchBucketPolicy" || apiErr.Code == "NoSuchBucket" || apiErr.Code == "404")) {
				return FormatAPIError(err)
			}
		}
		myprint.PrintfBoldGreen("Policy removed (private) for %s\n", c.S3Path(bucket, ""))
		return nil
	}

	data, err := buildAnonymousPolicy(perm, bucket, prefix)
	if err != nil {
		return err
	}
	if err := c.S3.SetBucketPolicy(c.Ctx, bucket, data); err != nil {
		return FormatAPIError(err)
	}
	scope := "whole bucket"
	if prefix != "" {
		scope = "prefix " + prefix
	}
	myprint.PrintfBoldGreen("Policy %s set for %s (%s)\n", perm, c.S3Path(bucket, ""), scope)
	return nil
}

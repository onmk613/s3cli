// bucket-policy.go 实现桶访问策略 (Bucket Policy) 管理:
// SetPolicy (permission 参数直接生成策略, 不依赖文件) / GetPolicy / DelPolicy.
//
// permission 取值: private / download / upload / public.
// 兼容旧名: none -> private, public-read -> download, public-write -> upload,
// public-read-write -> public. 预定义策略语句结构:
//
//	download: GetBucketLocation + ListBucket + GetObject
//	upload:   GetBucketLocation + ListBucketMultipartUploads + Put/Delete/Abort/ListParts
//	public:   以上并集
//	private:  删除策略

package action

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/i18n"
	"s3cli/pkg/s3iface"
)

// PolicyOptions 控制 SetPolicy 的参数.
//
// 三选一: 指定 ConfigFile 用自定义 JSON 文件覆盖;
// 或指定 Permission 套用预定义策略; 否则报错.
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
		return errors.New(i18n.T("policy set: either --type TYPE or --from-file FILE is required", "设置策略：必须指定 --type TYPE 或 --from-file FILE"))
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

	myprint.PrintfBoldGreen(i18n.T("Policy set for %s\n", "已为 %s 设置策略\n"), c.S3Path(bucket, ""))
	return nil
}

// GetPolicyOptions 控制 GetPolicy 的输出方式.
type GetPolicyOptions struct {
	JSON bool // --json: 直接输出服务器返回的原始策略 JSON
}

// GetPolicy 读取桶策略: 默认解析策略 JSON 并输出策略类型
// (private/download/upload/public/custom); --json 时直接输出原始策略 JSON.
func (c *Action) GetPolicy(opt GetPolicyOptions, bucket string) error {
	raw, err := c.S3.GetBucketPolicy(c.Ctx, bucket)
	if err != nil {
		// 无策略等价于 private, 默认输出直接展示类型; --json 无原始 JSON 可输出, 保持报错.
		var apiErr *s3iface.ErrorResponse
		if !opt.JSON && errors.As(err, &apiErr) && apiErr.Code == "NoSuchBucketPolicy" {
			myprint.PrintfBoldBlue(i18n.T("# %s policy:\n", "# %s 策略：\n"), c.S3Path(bucket, ""))
			myprint.PrintlnGreen(i18n.T("type: private", "类型：private"))
			return nil
		}
		return FormatAPIError(err)
	}

	if opt.JSON {
		out := string(raw)
		if !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		_, err := fmt.Fprint(os.Stdout, out)
		return err
	}

	typ, err := classifyPolicyType(raw, bucket)
	if err != nil {
		return err
	}
	myprint.PrintfBoldBlue(i18n.T("# %s policy:\n", "# %s 策略：\n"), c.S3Path(bucket, ""))
	myprint.PrintfGreen(i18n.T("type: %s\n", "类型：%s\n"), typ)
	return nil
}

// DelPolicy 删除桶的访问策略.
func (c *Action) DelPolicy(bucket string) error {
	if err := c.S3.DeleteBucketPolicy(c.Ctx, bucket); err != nil {
		return FormatAPIError(err)
	}

	myprint.PrintfBoldGreen(i18n.T("Policy deleted for %s: success\n", "已删除 %s 的策略：成功\n"), c.S3Path(bucket, ""))
	return nil
}

// ----------------------------------------------------------------------------
// 预定义匿名访问策略
// ----------------------------------------------------------------------------

// normalizePermission 把权限名归一化: 旧名映射到标准语义.
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
	return "", fmt.Errorf(i18n.T("unknown permission %q: allowed values are [private, download, upload, public]", "未知权限 %q：允许的值有 [private, download, upload, public]"), name)
}

// anonStatement / anonPolicy 描述匿名访问策略的 JSON 结构.
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

// buildAnonymousPolicy 构造匿名访问策略 JSON, 语句结构为标准的预定义策略,
// 含 GetBucketLocation+ListBucketMultipartUploads 合并语句与
// ListBucket 的 StringEquals s3:prefix 条件.
// prefix 为空时作用于整个桶, 否则仅作用于该前缀下的对象.
func buildAnonymousPolicy(perm, bucket, prefix string) ([]byte, error) {
	bucketRes := "arn:aws:s3:::" + bucket
	objRes := bucketRes + "/" + prefix + "*"

	allow := "Allow"
	principal := anonPrincipal{AWS: []string{"*"}}

	// 桶级语句: GetBucketLocation 与 ListBucketMultipartUploads 合并为一条.
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
		return nil, fmt.Errorf(i18n.T("unknown permission %q", "未知权限 %q"), perm)
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
		myprint.PrintfBoldGreen(i18n.T("Policy removed (private) for %s\n", "已移除 %s 的策略（private）\n"), c.S3Path(bucket, ""))
		return nil
	}

	data, err := buildAnonymousPolicy(perm, bucket, prefix)
	if err != nil {
		return err
	}
	if err := c.S3.SetBucketPolicy(c.Ctx, bucket, data); err != nil {
		return FormatAPIError(err)
	}
	scope := i18n.T("whole bucket", "整个存储桶")
	if prefix != "" {
		scope = fmt.Sprintf(i18n.T("prefix %s", "前缀 %s"), prefix)
	}
	myprint.PrintfBoldGreen(i18n.T("Policy %s set for %s (%s)\n", "策略 %s 已设置于 %s（%s）\n"), perm, c.S3Path(bucket, ""), scope)
	return nil
}

// ----------------------------------------------------------------------------
// 策略类型识别 (PolicyGetCmd 默认输出)
// ----------------------------------------------------------------------------

// policyStatement / policyDocument 仅用于类型识别: 宽松解析 Action/Resource
// (S3 策略允许单个字符串或字符串数组), 忽略 Sid 等不影响识别的字段.
type policyStatement struct {
	Action    any            `json:"Action"`
	Effect    string         `json:"Effect"`
	Principal any            `json:"Principal"`
	Resource  any            `json:"Resource"`
	Condition map[string]any `json:"Condition"`
}

type policyDocument struct {
	Version   string          `json:"Version"`
	Statement json.RawMessage `json:"Statement"`
}

// policySigPart 是单条策略语句的规范化签名, 用于和预定义策略做精确比对.
type policySigPart struct {
	Actions   []string `json:"a"`
	Resources []string `json:"r"`
	Prefix    string   `json:"p,omitempty"`
}

// classifyPolicyType 解析桶策略 JSON 并识别匿名访问类型: download / upload /
// public 与 SetPolicy 的预定义策略一一对应; 合法 JSON 但无法对应任何预定义
// 策略时返回 custom.
func classifyPolicyType(raw []byte, bucket string) (string, error) {
	var doc policyDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", fmt.Errorf("policy get: invalid policy JSON: %w", err)
	}
	if doc.Version != "2012-10-17" || len(doc.Statement) == 0 {
		return "custom", nil
	}

	stmts, err := parsePolicyStatements(doc.Statement)
	if err != nil {
		return "custom", nil
	}

	var sigs []string
	prefixes := map[string]bool{}
	for _, st := range stmts {
		sig, condPrefix, ok := policyStatementSignature(st)
		if !ok {
			return "custom", nil
		}
		sigs = append(sigs, sig)
		if condPrefix != "" {
			prefixes[condPrefix] = true
		}
		resources, ok := normalizeStringSet(st.Resource)
		if !ok {
			return "custom", nil
		}
		for _, res := range resources {
			p, isObj := objectResourcePrefix(bucket, res)
			if isObj {
				prefixes[p] = true
			}
		}
	}
	if len(prefixes) > 1 {
		return "custom", nil
	}
	prefix := ""
	for p := range prefixes {
		prefix = p
	}
	slices.Sort(sigs)

	for _, perm := range []string{"download", "upload", "public"} {
		expected := cannedPolicySignatures(perm, bucket, prefix)
		if slices.Equal(sigs, expected) {
			return perm, nil
		}
	}
	return "custom", nil
}

// parsePolicyStatements 兼容 Statement 为单个对象或对象数组两种写法.
func parsePolicyStatements(raw json.RawMessage) ([]policyStatement, error) {
	var list []policyStatement
	if err := json.Unmarshal(raw, &list); err == nil {
		return list, nil
	}
	var one policyStatement
	if err := json.Unmarshal(raw, &one); err == nil {
		return []policyStatement{one}, nil
	}
	return nil, fmt.Errorf("policy get: invalid Statement")
}

// policyStatementSignature 把单条语句归一化为可比较签名; 任何不属于预定义
// 匿名策略的写法 (Deny、非匿名 Principal、额外 Condition 等) 返回 ok=false.
func policyStatementSignature(st policyStatement) (sig, condPrefix string, ok bool) {
	if st.Effect != "Allow" || !isAnonymousPrincipal(st.Principal) {
		return "", "", false
	}
	actions, ok := normalizeStringSet(st.Action)
	if !ok {
		return "", "", false
	}
	resources, ok := normalizeStringSet(st.Resource)
	if !ok {
		return "", "", false
	}
	condPrefix, ok = normalizePolicyCondition(st.Condition)
	if !ok {
		return "", "", false
	}
	b, err := json.Marshal(policySigPart{
		Actions:   actions,
		Resources: resources,
		Prefix:    condPrefix,
	})
	if err != nil {
		return "", "", false
	}
	return string(b), condPrefix, true
}

// isAnonymousPrincipal 判断 Principal 是否为匿名主体 "*" (字符串或 AWS 数组形式).
func isAnonymousPrincipal(v any) bool {
	switch p := v.(type) {
	case string:
		return p == "*"
	case map[string]any:
		aws, ok := p["AWS"]
		if !ok {
			return false
		}
		switch a := aws.(type) {
		case string:
			return a == "*"
		case []any:
			return len(a) == 1 && a[0] == "*"
		}
	}
	return false
}

// normalizeStringSet 把单个字符串或字符串数组归一化为非空字符串集合.
func normalizeStringSet(v any) ([]string, bool) {
	var out []string
	switch t := v.(type) {
	case string:
		if t == "" {
			return nil, false
		}
		out = []string{t}
	case []any:
		for _, e := range t {
			s, ok := e.(string)
			if !ok || s == "" {
				return nil, false
			}
			out = append(out, s)
		}
	default:
		return nil, false
	}
	return out, len(out) > 0
}

// normalizePolicyCondition 只接受预定义策略使用的 StringEquals s3:prefix 条件,
// 返回前缀值; 无条件返回 "".
func normalizePolicyCondition(cond map[string]any) (string, bool) {
	if len(cond) == 0 {
		return "", true
	}
	if len(cond) != 1 {
		return "", false
	}
	seq, ok := cond["StringEquals"].(map[string]any)
	if !ok || len(seq) != 1 {
		return "", false
	}
	val, ok := seq["s3:prefix"]
	if !ok {
		return "", false
	}
	var prefix string
	switch v := val.(type) {
	case string:
		prefix = v
	case []any:
		if len(v) != 1 {
			return "", false
		}
		s, ok := v[0].(string)
		if !ok {
			return "", false
		}
		prefix = s
	default:
		return "", false
	}
	if prefix == "" {
		return "", false
	}
	return prefix, true
}

// objectResourcePrefix 从对象资源 ARN 中提取策略前缀:
// arn:aws:s3:::bucket/prefix* -> prefix; 非对象资源返回 ok=false.
func objectResourcePrefix(bucket, res string) (prefix string, ok bool) {
	bucketARN := "arn:aws:s3:::" + bucket
	if !strings.HasPrefix(res, bucketARN+"/") || !strings.HasSuffix(res, "*") {
		return "", false
	}
	return strings.TrimSuffix(strings.TrimPrefix(res, bucketARN+"/"), "*"), true
}

// cannedPolicySignatures 生成预定义策略的规范化语句签名集合 (已排序),
// 与 buildAnonymousPolicy 的结构一一对应.
func cannedPolicySignatures(perm, bucket, prefix string) []string {
	bucketRes := "arn:aws:s3:::" + bucket
	objRes := bucketRes + "/" + prefix + "*"

	sig := func(actions, resources []string, condPrefix string) string {
		b, _ := json.Marshal(policySigPart{
			Actions:   actions,
			Resources: resources,
			Prefix:    condPrefix,
		})
		return string(b)
	}

	var sigs []string
	switch perm {
	case "download":
		sigs = []string{
			sig([]string{actionGetBucketLocation}, []string{bucketRes}, ""),
			sig([]string{actionListBucket}, []string{bucketRes}, prefix),
			sig([]string{actionGetObject}, []string{objRes}, ""),
		}
	case "upload":
		sigs = []string{
			sig([]string{actionGetBucketLocation, actionListBucketMultipart}, []string{bucketRes}, ""),
			sig([]string{actionAbortMultipart, actionDeleteObject, actionListMultipartParts, actionPutObject}, []string{objRes}, ""),
		}
	case "public":
		sigs = []string{
			sig([]string{actionGetBucketLocation, actionListBucketMultipart}, []string{bucketRes}, ""),
			sig([]string{actionListBucket}, []string{bucketRes}, prefix),
			sig([]string{actionAbortMultipart, actionDeleteObject, actionGetObject, actionListMultipartParts, actionPutObject}, []string{objRes}, ""),
		}
	}
	slices.Sort(sigs)
	return sigs
}

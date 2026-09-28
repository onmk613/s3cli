// bucket-acl.go 实现桶级 ACL 的读取与设置: Get/SetBucketACL.
//
// GET ?acl 返回原始 XML, 直接展示 (可缩进) 或以 --json 包装为 {"acl": "<xml>"};
// PUT ?acl 只支持 canned ACL 头 (x-amz-acl) 与 grant 头 (x-amz-grant-*), 见 api.ACLOptions.

package action

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"

	"s3cli/internal/action/render"
	"s3cli/internal/api"
	myprint "s3cli/internal/fmtutil"
	"s3cli/internal/i18n"
)

// ACLGetOptions 控制 `bucket acl get` / `object acl get` 的参数.
type ACLGetOptions struct {
	JSON bool // --json: 以 {"acl": "<xml>"} 输出, 便于脚本消费
}

// ACLSetOptions 控制 `bucket acl set` / `object acl set` 的参数.
//
// ACL 为 canned ACL (x-amz-acl); 其余字段为可重复的 grantee 列表,
// 每个元素形如 "<id>" / "id=<id>" / "uri=<uri>" / "emailAddress=<email>"。
type ACLSetOptions struct {
	ACL              string
	GrantFullControl []string
	GrantRead        []string
	GrantReadACP     []string
	GrantWrite       []string
	GrantWriteACP    []string
}

// cannedACLs 是允许的 canned ACL 取值 (api.ACLOptions 注释中列出的集合).
var cannedACLs = map[string]bool{
	"private":                   true,
	"public-read":               true,
	"public-read-write":         true,
	"authenticated-read":        true,
	"bucket-owner-read":         true,
	"bucket-owner-full-control": true,
	"log-delivery-write":        true,
}

// GetBucketACL 读取并打印桶 ACL.
func (c *Action) GetBucketACL(opt ACLGetOptions, bucket string) error {
	data, err := c.S3.GetBucketACL(c.Ctx, bucket)
	if err != nil {
		return fmt.Errorf("get bucket acl %s: %w", bucket, err)
	}
	return printACL(opt, c.S3Path(bucket, ""), data)
}

// SetBucketACL 设置桶 ACL (canned ACL 与 grant 头至少给出一项).
func (c *Action) SetBucketACL(opt ACLSetOptions, bucket string) error {
	apiOpt, err := buildACLOptions(opt)
	if err != nil {
		return err
	}
	if err := c.S3.PutBucketACL(c.Ctx, bucket, apiOpt); err != nil {
		return fmt.Errorf("set bucket acl %s: %w", bucket, err)
	}
	myprint.PrintfBoldGreen(i18n.T("ACL set for %s (%s)\n", "已为 %s 设置 ACL（%s）\n"), c.S3Path(bucket, ""), describeACLOptions(apiOpt))
	return nil
}

// buildACLOptions 把命令参数收敛为 api.ACLOptions, 并做本地校验:
// 未给出任何 ACL 参数时直接报错, 避免发出一个"什么都不改"的空 PUT。
func buildACLOptions(opt ACLSetOptions) (*api.ACLOptions, error) {
	out := &api.ACLOptions{ACL: strings.ToLower(strings.TrimSpace(opt.ACL))}
	if out.ACL != "" && !cannedACLs[out.ACL] {
		return nil, fmt.Errorf(i18n.T("acl set: invalid canned ACL %q (expected private / public-read / public-read-write / authenticated-read / bucket-owner-read / bucket-owner-full-control / log-delivery-write)", "设置 ACL：canned ACL %q 无效（应为 private / public-read / public-read-write / authenticated-read / bucket-owner-read / bucket-owner-full-control / log-delivery-write）"), opt.ACL)
	}

	var err error
	if out.GrantFullControl, err = buildGrantHeader(opt.GrantFullControl); err != nil {
		return nil, err
	}
	if out.GrantRead, err = buildGrantHeader(opt.GrantRead); err != nil {
		return nil, err
	}
	if out.GrantReadACP, err = buildGrantHeader(opt.GrantReadACP); err != nil {
		return nil, err
	}
	if out.GrantWrite, err = buildGrantHeader(opt.GrantWrite); err != nil {
		return nil, err
	}
	if out.GrantWriteACP, err = buildGrantHeader(opt.GrantWriteACP); err != nil {
		return nil, err
	}

	if out.ACL == "" && out.GrantFullControl == "" && out.GrantRead == "" &&
		out.GrantReadACP == "" && out.GrantWrite == "" && out.GrantWriteACP == "" {
		return nil, errors.New(i18n.T(
			"acl set: at least one of --acl / --grant-read / --grant-write / --grant-read-acp / --grant-write-acp / --grant-full-control is required",
			"设置 ACL：至少需要 --acl / --grant-read / --grant-write / --grant-read-acp / --grant-write-acp / --grant-full-control 之一"))
	}
	return out, nil
}

// buildGrantHeader 把一个或多个 grantee 参数收敛为 x-amz-grant-* 头的值.
// 返回空串表示该项未设置。
func buildGrantHeader(values []string) (string, error) {
	parts := make([]string, 0, len(values))
	for _, raw := range values {
		g, err := normalizeGrantee(raw)
		if err != nil {
			return "", err
		}
		parts = append(parts, g)
	}
	return strings.Join(parts, ", "), nil
}

// normalizeGrantee 把单个 grantee 参数转成 S3 grant 头语法 `<type>="<value>"`。
//
//   - "id=<id>" / "uri=<uri>" / "emailAddress=<email>" 显式指定类型 (大小写不敏感);
//   - "http(s)://..." 裸值按 uri 处理, 含 "@" 的裸值按 emailAddress 处理,
//     其余裸值按 CanonicalUser id 处理。
func normalizeGrantee(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", errors.New(i18n.T("grantee must not be empty", "grantee 不能为空"))
	}

	kind, val := "id", v
	if before, after, ok := strings.Cut(v, "="); ok {
		switch strings.ToLower(strings.TrimSpace(before)) {
		case "id":
			kind, val = "id", strings.TrimSpace(after)
		case "uri":
			kind, val = "uri", strings.TrimSpace(after)
		case "emailaddress":
			kind, val = "emailAddress", strings.TrimSpace(after)
		default:
			return "", fmt.Errorf(i18n.T("invalid grantee %q: expected <id>, id=<id>, uri=<uri> or emailAddress=<email>", "grantee %q 无效：应为 <id>、id=<id>、uri=<uri> 或 emailAddress=<email>"), raw)
		}
	} else if strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://") {
		kind = "uri"
	} else if strings.Contains(v, "@") {
		// CanonicalUser ID 是定长十六进制串, 不会含 "@"; 裸邮箱按 emailAddress 处理。
		kind = "emailAddress"
	}

	if val == "" {
		return "", fmt.Errorf(i18n.T("invalid grantee %q: value is empty", "grantee %q 无效：取值为空"), raw)
	}
	// 引号/逗号会破坏 grant 头的 `type="value", type="value"` 语法。
	if strings.ContainsAny(val, "\",\r\n") {
		return "", fmt.Errorf(i18n.T("invalid grantee %q: value must not contain quotes, commas or line breaks", "grantee %q 无效：取值不能包含引号、逗号或换行"), raw)
	}
	return kind + `="` + val + `"`, nil
}

// describeACLOptions 生成写入确认信息中的参数摘要 (只列出实际设置的部分).
func describeACLOptions(opt *api.ACLOptions) string {
	var parts []string
	add := func(name, v string) {
		if v != "" {
			parts = append(parts, name+"="+v)
		}
	}
	add("acl", opt.ACL)
	add("grant-full-control", opt.GrantFullControl)
	add("grant-read", opt.GrantRead)
	add("grant-read-acp", opt.GrantReadACP)
	add("grant-write", opt.GrantWrite)
	add("grant-write-acp", opt.GrantWriteACP)
	return strings.Join(parts, " ")
}

// printACL 打印原始 ACL XML: --json 时包装为 {"acl": "<xml>"}, 否则尽量缩进展示。
func printACL(opt ACLGetOptions, path string, data []byte) error {
	if opt.JSON {
		return render.JSONLine(map[string]any{"acl": string(data)})
	}
	myprint.PrintfBoldBlue(i18n.T("# %s ACL:\n", "# %s ACL：\n"), path)
	if pretty, ok := prettyXML(data); ok {
		myprint.PrintlnGreen(string(pretty))
		return nil
	}
	// 不是合法 XML (个别兼容实现返回空体或纯文本) 时原样输出。
	myprint.PrintlnGreen(strings.TrimRight(string(data), "\n"))
	return nil
}

// prettyXML 按元素层级缩进 XML 文本, 解析失败时返回 (nil, false) 交由调用方回退原样输出。
//
// 用 RawToken 而不是 Token: 前者保留原始命名空间前缀 (如 xsi:type),
// 后者会把前缀解析成命名空间 URL, 再由编码器重写, 输出与原始文档差异较大。
func prettyXML(data []byte) ([]byte, bool) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var buf bytes.Buffer
	var stack []string
	depth := 0
	wroteElement := false
	// pending 暂存当前元素的文本内容: 只有文本的元素 (如 <ID>abc</ID>) 保持单行,
	// 需要另起一行时才写出。
	var pending string

	indent := func() {
		if buf.Len() > 0 {
			buf.WriteByte('\n')
		}
		buf.WriteString(strings.Repeat("  ", depth))
	}
	escaped := func(s string) string {
		var b bytes.Buffer
		_ = xml.EscapeText(&b, []byte(s))
		return b.String()
	}
	flushPending := func() {
		if pending != "" {
			indent()
			buf.WriteString(escaped(pending))
			pending = ""
		}
	}
	// name 还原 "prefix:local" 形式 (RawToken 把前缀放在 Space 中)。
	name := func(n xml.Name) string {
		if n.Space == "" {
			return n.Local
		}
		return n.Space + ":" + n.Local
	}

	for {
		tok, err := dec.RawToken()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, false
		}
		switch t := tok.(type) {
		case xml.StartElement:
			flushPending()
			indent()
			buf.WriteByte('<')
			buf.WriteString(name(t.Name))
			for _, a := range t.Attr {
				buf.WriteByte(' ')
				buf.WriteString(name(a.Name))
				buf.WriteString(`="`)
				buf.WriteString(escaped(a.Value))
				buf.WriteByte('"')
			}
			buf.WriteByte('>')
			stack = append(stack, name(t.Name))
			wroteElement = true
			depth++
		case xml.EndElement:
			// encoding/xml 对不匹配的结束标签很宽容, 这里自行校验元素配对,
			// 保证只有格式良好的 XML 才会被"美化"输出。
			if len(stack) == 0 || stack[len(stack)-1] != name(t.Name) {
				return nil, false
			}
			stack = stack[:len(stack)-1]
			depth--
			if pending != "" {
				// 纯文本元素: <ID>abc</ID> 保持单行
				buf.WriteString(escaped(pending))
				pending = ""
			} else {
				indent()
			}
			buf.WriteString("</")
			buf.WriteString(name(t.Name))
			buf.WriteByte('>')
		case xml.CharData:
			if text := strings.TrimSpace(string(t)); text != "" {
				pending += text
			}
		case xml.Comment:
			flushPending()
			indent()
			buf.WriteString("<!--")
			buf.Write(t)
			buf.WriteString("-->")
		case xml.ProcInst:
			flushPending()
			indent()
			buf.WriteString("<?")
			buf.WriteString(t.Target)
			buf.WriteByte(' ')
			buf.Write(t.Inst)
			buf.WriteString("?>")
		case xml.Directive:
			flushPending()
			indent()
			buf.WriteString("<!")
			buf.Write(t)
			buf.WriteByte('>')
		}
	}
	if !wroteElement || len(stack) != 0 {
		return nil, false
	}
	return buf.Bytes(), true
}

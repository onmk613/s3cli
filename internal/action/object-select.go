// object-select.go 实现 S3 Select SQL 查询 (sql 命令).
//
// 支持对 CSV / JSON / Parquet 对象执行 SQL, 序列化选项用
// "rd=\\n,fh=USE,fd=;" 键值串描述 (缩写: rd/fd/fh/qc/qec/cc/qf, 详见 parseSelectOpts).

package action

import (
	"errors"
	"fmt"
	"os"
	"strings"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/i18n"
	"s3cli/pkg/s3iface"
)

// SelectOptions sql 命令参数.
type SelectOptions struct {
	Query           string // SQL 表达式
	Recursive       bool   // -r: 对前缀下所有对象执行查询
	Compression     string // 输入压缩: NONE / GZIP / BZIP2
	CSVInput        string // CSV 输入序列化选项, 如 "rd=\n,fh=USE,fd=;"
	JSONInput       string // JSON 输入序列化选项, 如 "type=LINES"
	CSVOutput       string // CSV 输出序列化选项
	JSONOutput      string // JSON 输出序列化选项
	CSVOutputHeader string // 可选 CSV 输出表头 (逗号分隔)
}

// SelectObjects 对对象执行 SQL 查询并输出结果记录.
func (c *Action) SelectObjects(opt SelectOptions, bucket, prefix string) error {
	if opt.Query == "" {
		opt.Query = "select * from S3Object"
	}
	if opt.Recursive {
		return c.selectRecursive(opt, bucket, prefix)
	}
	if prefix == "" {
		return errors.New(i18n.T("sql requires an object key or a prefix with -r", "sql 需要指定对象 key 或带 -r 的前缀"))
	}
	return c.selectOne(opt, bucket, prefix)
}

// selectRecursive 对前缀下所有对象执行查询 (-r).
func (c *Action) selectRecursive(opt SelectOptions, bucket, prefix string) error {
	var count int
	err := c.forEachObject(c.Ctx, bucket, prefix, func(obj s3iface.ObjectInfo) error {
		if strings.HasSuffix(obj.Key, "/") && obj.Size == 0 {
			return nil // 目录标记对象
		}
		count++
		return c.selectOne(opt, bucket, obj.Key)
	})
	if err != nil {
		return err
	}
	if count == 0 {
		myprint.PrintfBoldYellow(i18n.T("%s: no objects to query\n", "%s：没有可查询的对象\n"), c.S3Path(bucket, prefix))
	}
	return nil
}

// selectOne 对单个对象执行查询.
func (c *Action) selectOne(opt SelectOptions, bucket, key string) error {
	in, out, err := buildSelectSerializations(opt, key)
	if err != nil {
		return err
	}

	// CSV 输出表头: 每个对象开头输出一次
	var headerPrinted bool
	stats, err := c.S3.SelectObjectContent(c.Ctx, bucket, key, &s3iface.SelectObjectInput{
		Expression:          opt.Query,
		InputSerialization:  in,
		OutputSerialization: out,
		RequestProgress:     false,
	}, func(payload []byte) error {
		if !headerPrinted && opt.CSVOutputHeader != "" {
			if _, err := fmt.Fprintln(os.Stdout, opt.CSVOutputHeader); err != nil {
				return err
			}
			headerPrinted = true
		}
		_, werr := os.Stdout.Write(payload)
		return werr
	})
	if err != nil {
		return fmt.Errorf("sql %s: %w", c.S3Path(bucket, key), FormatAPIError(err))
	}

	// 统计信息输出到 stderr, 不污染 stdout 的查询结果
	if stats != nil {
		fmt.Fprintf(os.Stderr, i18n.T("%s: scanned %d B, processed %d B, returned %d B\n", "%s：扫描 %d B，处理 %d B，返回 %d B\n"),
			c.S3Path(bucket, key), stats.BytesScanned, stats.BytesProcessed, stats.BytesReturned)
	}
	return nil
}

// buildSelectSerializations 由选项串构造输入/输出序列化描述:
// 未指定输入格式时按文件扩展名推断 (csv/json/parquet), CSV 默认首行为表头 (FileHeaderInfo=USE);
// 未指定 --compression 时按扩展名推断 (.gz -> GZIP, .bz/.bz2 -> BZIP2).
func buildSelectSerializations(opt SelectOptions, key string) (*s3iface.SelectSerialization, *s3iface.SelectSerialization, error) {
	if opt.CSVInput != "" && opt.JSONInput != "" {
		return nil, nil, errors.New(i18n.T("only one of --csv-input or --json-input can be specified", "--csv-input 与 --json-input 只能指定一个"))
	}
	if opt.CSVOutput != "" && opt.JSONOutput != "" {
		return nil, nil, errors.New(i18n.T("only one of --csv-output or --json-output can be specified", "--csv-output 与 --json-output 只能指定一个"))
	}

	lowerKey := strings.ToLower(key)
	trimmed := lowerKey
	trimmed = strings.TrimSuffix(strings.TrimSuffix(trimmed, ".gz"), ".bz")
	trimmed = strings.TrimSuffix(trimmed, ".bz2")

	// 输入格式: 显式 flag > 扩展名推断 > 默认 CSV
	format := "CSV"
	switch {
	case opt.CSVInput != "":
		format = "CSV"
	case opt.JSONInput != "":
		format = "JSON"
	case strings.HasSuffix(trimmed, ".parquet"):
		format = "PARQUET"
	case strings.HasSuffix(trimmed, ".json"):
		format = "JSON"
	}

	in := &s3iface.SelectSerialization{
		Format: format,
	}
	// 压缩类型: 显式 flag > 扩展名推断
	switch {
	case opt.Compression != "":
		in.CompressionType = strings.ToUpper(opt.Compression)
	case strings.HasSuffix(lowerKey, ".gz"):
		in.CompressionType = "GZIP"
	case strings.HasSuffix(lowerKey, ".bz") || strings.HasSuffix(lowerKey, ".bz2"):
		in.CompressionType = "BZIP2"
	default:
		in.CompressionType = "NONE"
	}

	var err error
	switch format {
	case "CSV":
		if opt.CSVInput != "" {
			err = applySelectCSVInput(in, opt.CSVInput)
		} else {
			// 默认首行为表头
			in.FileHeaderInfo = "USE"
		}
	case "JSON":
		if opt.JSONInput != "" {
			err = applySelectJSONInput(in, opt.JSONInput)
		} else {
			in.JSONType = "LINES"
		}
	}
	if err != nil {
		return nil, nil, err
	}

	// 输出格式: 默认 CSV
	out := &s3iface.SelectSerialization{Format: "CSV"}
	switch {
	case opt.JSONOutput != "":
		out.Format = "JSON"
		err = applySelectJSONOutput(out, opt.JSONOutput)
	case opt.CSVOutput != "":
		err = applySelectCSVOutput(out, opt.CSVOutput)
	default:
		out.RecordDelimiter = "\n"
	}
	if err != nil {
		return nil, nil, err
	}
	return in, out, nil
}

// ----------------------------------------------------------------------------
// 序列化选项解析
// ----------------------------------------------------------------------------

// parseSelectOpts 解析 "k=v,k2=v2" 选项串, 展开缩写并校验合法键.
func parseSelectOpts(inp string, validKeys map[string]string) (map[string]string, error) {
	out := map[string]string{}
	if strings.TrimSpace(inp) == "" {
		return out, nil
	}
	for _, pair := range splitOptPairs(inp) {
		eq := strings.Index(pair, "=")
		if eq < 0 {
			return nil, fmt.Errorf(i18n.T("serialization options should be of the form key=value,... (got %q)", "序列化选项应为 key=value,... 形式（当前为 %q）"), pair)
		}
		key := strings.TrimSpace(pair[:eq])
		val := strings.TrimSpace(pair[eq+1:])
		if key == "" {
			return nil, fmt.Errorf(i18n.T("empty option key in %q", "%q 中存在空的选项键"), pair)
		}
		// 展开缩写 (rd -> RecordDelimiter 等)
		long, ok := validKeys[key]
		if !ok {
			return nil, fmt.Errorf(i18n.T("invalid serialization key %q (valid: %s)", "无效的序列化键 %q（合法：%s）"), key, strings.Join(optKeys(validKeys), ", "))
		}
		if _, dup := out[long]; dup {
			return nil, fmt.Errorf(i18n.T("more than one key=value found for %s", "%s 出现多次 key=value"), key)
		}
		out[long] = unescapeOptValue(val)
	}
	return out, nil
}

// splitOptPairs 按逗号切分 k=v 对 (值中的逗号视为普通字符).
func splitOptPairs(inp string) []string {
	var pairs []string
	var cur strings.Builder
	for i := 0; i < len(inp); i++ {
		if inp[i] == ',' {
			pairs = append(pairs, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(inp[i])
	}
	if cur.Len() > 0 || len(pairs) > 0 {
		pairs = append(pairs, cur.String())
	}
	return pairs
}

// unescapeOptValue 还原 \n \r \t 转义.
func unescapeOptValue(v string) string {
	return strings.NewReplacer(`\n`, "\n", `\t`, "\t", `\r`, "\r").Replace(v)
}

// optKeys 返回合法键列表.
func optKeys(m map[string]string) []string {
	seen := map[string]bool{}
	var keys []string
	for _, long := range m {
		if !seen[long] {
			seen[long] = true
			keys = append(keys, long)
		}
	}
	return keys
}

// validSelectKeys 合法键: 缩写 -> 长名.
var validSelectKeys = map[string]string{
	"cc":                    "Comments",
	"fh":                    "FileHeader",
	"qrd":                   "QuotedRecordDelimiter",
	"rd":                    "RecordDelimiter",
	"fd":                    "FieldDelimiter",
	"qc":                    "QuoteChar",
	"qec":                   "QuoteEscChar",
	"qf":                    "QuoteFields",
	"type":                  "Type",
	"comments":              "Comments",
	"fileheader":            "FileHeader",
	"quotedrecorddelimiter": "QuotedRecordDelimiter",
	"recorddelimiter":       "RecordDelimiter",
	"fielddelimiter":        "FieldDelimiter",
	"quotechar":             "QuoteChar",
	"quoteescchar":          "QuoteEscChar",
	"quotefields":           "QuoteFields",
}

// applySelectCSVInput 应用 CSV 输入选项.
func applySelectCSVInput(ins *s3iface.SelectSerialization, opts string) error {
	kv, err := parseSelectOpts(opts, validSelectKeys)
	if err != nil {
		return fmt.Errorf(i18n.T("--csv-input: %w", "--csv-input：%w"), err)
	}
	for k, v := range kv {
		switch k {
		case "FieldDelimiter":
			ins.FieldDelimiter = v
		case "RecordDelimiter":
			ins.RecordDelimiter = v
		case "QuoteChar":
			ins.QuoteCharacter = v
		case "QuoteEscChar":
			ins.QuoteEscapeCharacter = v
		case "Comments":
			ins.CommentCharacter = v
		case "FileHeader":
			ins.FileHeaderInfo = strings.ToUpper(v)
		default:
			// QuotedRecordDelimiter 是服务端扩展, 标准 S3 无此元素, 忽略
		}
	}
	return nil
}

// applySelectJSONInput 应用 JSON 输入选项.
func applySelectJSONInput(ins *s3iface.SelectSerialization, opts string) error {
	kv, err := parseSelectOpts(opts, validSelectKeys)
	if err != nil {
		return fmt.Errorf(i18n.T("--json-input: %w", "--json-input：%w"), err)
	}
	for k, v := range kv {
		if k == "Type" {
			ins.JSONType = strings.ToUpper(v)
		}
	}
	if ins.JSONType == "" {
		ins.JSONType = "LINES"
	}
	return nil
}

// applySelectCSVOutput 应用 CSV 输出选项.
func applySelectCSVOutput(outs *s3iface.SelectSerialization, opts string) error {
	kv, err := parseSelectOpts(opts, validSelectKeys)
	if err != nil {
		return fmt.Errorf(i18n.T("--csv-output: %w", "--csv-output：%w"), err)
	}
	for k, v := range kv {
		switch k {
		case "RecordDelimiter":
			outs.RecordDelimiter = v
		case "FieldDelimiter":
			outs.FieldDelimiter = v
		case "QuoteChar":
			outs.QuoteCharacter = v
		case "QuoteEscChar":
			outs.QuoteEscapeCharacter = v
		case "QuoteFields":
			outs.QuoteFields = strings.ToUpper(v)
		}
	}
	if outs.RecordDelimiter == "" {
		outs.RecordDelimiter = "\n"
	}
	return nil
}

// applySelectJSONOutput 应用 JSON 输出选项.
func applySelectJSONOutput(outs *s3iface.SelectSerialization, opts string) error {
	kv, err := parseSelectOpts(opts, validSelectKeys)
	if err != nil {
		return fmt.Errorf(i18n.T("--json-output: %w", "--json-output：%w"), err)
	}
	for k, v := range kv {
		if k == "RecordDelimiter" {
			outs.RecordDelimiter = v
		}
	}
	if outs.RecordDelimiter == "" {
		outs.RecordDelimiter = "\n"
	}
	return nil
}

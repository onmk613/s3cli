package cmd

import (
	"s3cli/internal/action"
	"s3cli/internal/s3path"
	"s3cli/pkg/i18n"

	"github.com/spf13/cobra"
)

func init() { Register("object", "Object Operations", NewSQLCmd) }

// NewSQLCmd 对对象执行 SQL 查询.
func NewSQLCmd() *cobra.Command {
	var opt action.SelectOptions
	cmd := &cobra.Command{
		Use:               "sql [alias:bucket/path] ...",
		Short:             i18n.T("Run SQL queries against objects (text output only)", "对对象执行 SQL 查询（仅文本输出）"),
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: AutoCompletePath,
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.SelectObjects(opt, dst.Bucket, dst.Key)
		}),
	}
	f := cmd.Flags()
	f.StringVarP(&opt.Query, "query", "e", "select * from S3Object", i18n.T("SQL query expression", "SQL 查询表达式"))
	f.BoolVarP(&opt.Recursive, "recursive", "r", false, i18n.T("Run the query recursively on all objects under the prefix", "对前缀下的所有对象递归执行查询"))
	f.StringVar(&opt.Compression, "compression", "", i18n.T("Input compression type: NONE / GZIP / BZIP2", "输入压缩类型：NONE / GZIP / BZIP2"))
	f.StringVar(&opt.CSVInput, "csv-input", "", i18n.T("CSV input serialization options, e.g. 'rd=\\n,fh=USE,fd=;'", "CSV 输入序列化选项，如 'rd=\\n,fh=USE,fd=;'"))
	f.StringVar(&opt.JSONInput, "json-input", "", i18n.T("JSON input serialization options, e.g. 'type=LINES'", "JSON 输入序列化选项，如 'type=LINES'"))
	f.StringVar(&opt.CSVOutput, "csv-output", "", i18n.T("CSV output serialization options, e.g. 'rd=\\n'", "CSV 输出序列化选项，如 'rd=\\n'"))
	f.StringVar(&opt.JSONOutput, "json-output", "", i18n.T("JSON output serialization options, e.g. 'rd=\\n'", "JSON 输出序列化选项，如 'rd=\\n'"))
	f.StringVar(&opt.CSVOutputHeader, "csv-output-header", "", i18n.T("Optional CSV output header, comma separated", "可选的 CSV 输出表头，逗号分隔"))
	cmd.ValidArgsFunction = AutoCompletePath
	return cmd
}

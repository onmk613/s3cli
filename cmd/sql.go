package cmd

import (
	"s3cli/internal/action"
	"s3cli/internal/s3path"

	"github.com/spf13/cobra"
)

func init() { Register("object", "Object Operations", NewSqlCmd) }

// NewSqlCmd 对对象执行 SQL 查询 (mc sql 对齐).
func NewSqlCmd() *cobra.Command {
	var opt action.SelectOptions
	cmd := &cobra.Command{
		Use:               "sql [alias:bucket/path] ...",
		Long:              "Run SQL queries against objects (text output only)",
		Short:             "Run SQL queries on objects (mc sql compatible)",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: AutoCompletePath,
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.SelectObjects(opt, dst.Bucket, dst.Key)
		}),
	}
	f := cmd.Flags()
	f.StringVarP(&opt.Query, "query", "e", "select * from S3Object", "SQL query expression")
	f.BoolVarP(&opt.Recursive, "recursive", "r", false, "Run the query recursively on all objects under the prefix")
	f.StringVar(&opt.Compression, "compression", "", "Input compression type: NONE / GZIP / BZIP2")
	f.StringVar(&opt.CSVInput, "csv-input", "", "CSV input serialization options, e.g. 'rd=\\n,fh=USE,fd=;'")
	f.StringVar(&opt.JSONInput, "json-input", "", "JSON input serialization options, e.g. 'type=LINES'")
	f.StringVar(&opt.CSVOutput, "csv-output", "", "CSV output serialization options, e.g. 'rd=\\n'")
	f.StringVar(&opt.JSONOutput, "json-output", "", "JSON output serialization options, e.g. 'rd=\\n'")
	f.StringVar(&opt.CSVOutputHeader, "csv-output-header", "", "Optional CSV output header, comma separated")
	cmd.ValidArgsFunction = AutoCompletePath
	return cmd
}

// select.go 定义 S3 Select (SelectObjectContent) 的 DTO 类型.
//
// S3 Select 允许用 SQL 直接查询对象内容 (CSV/JSON/Parquet), 响应为事件流
// (Records/Progress/Stats/End). 本文件只描述请求参数与统计结果, 与后端无关.

package s3iface

// SelectSerialization 描述 S3 Select 输入/输出的序列化格式与选项.
type SelectSerialization struct {
	Format               string // "CSV" / "JSON" / "Parquet"
	CompressionType      string // 输入压缩: "NONE" / "GZIP" / "BZIP2"
	FileHeaderInfo       string // CSV 输入: "USE" / "IGNORE" / "NONE"
	FieldDelimiter       string
	RecordDelimiter      string
	QuoteCharacter       string
	QuoteEscapeCharacter string
	CommentCharacter     string // CSV 输入注释符 (S3 元素名 Comments)
	JSONType             string // JSON: "DOCUMENT" / "LINES"
	QuoteFields          string // CSV 输出: "ALWAYS" / "ASNEEDED"
}

// SelectObjectInput 是 SelectObjectContent 的请求参数.
type SelectObjectInput struct {
	Expression          string
	InputSerialization  *SelectSerialization
	OutputSerialization *SelectSerialization
	RequestProgress     bool // 是否请求进度事件
}

// SelectObjectStats 是 SelectObjectContent 的统计信息.
type SelectObjectStats struct {
	BytesScanned   int64
	BytesProcessed int64
	BytesReturned  int64
}

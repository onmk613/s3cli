package render

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// TimeLayout 是列举类输出统一的秒级时间格式
const TimeLayout = "2006-01-02 15:04:05"

// TimeLayoutMinute 是精确到分钟的时间格式 (桶列表的创建时间)
const TimeLayoutMinute = "2006-01-02 15:04"

// S3Path 把 (alias, bucket, key) 格式化为命令行风格的 "alias:bucket/key"
// key 为空时省略斜杠, 得到 "alias:bucket"
func S3Path(alias, bucket, key string) string {
	if key == "" {
		return alias + ":" + bucket
	}
	return alias + ":" + bucket + "/" + key
}

// JSONLine 以单行 JSON (JSON lines) 输出一条结构化结果
func JSONLine(v any) error {
	return JSONLineTo(os.Stdout, v)
}

// JSONLineTo 是 JSONLine 的显式 writer 版本
func JSONLineTo(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}
	_, err = fmt.Fprintln(w, string(b))
	return err
}

// JSONDoc 以缩进多行 JSON 输出一份完整文档
func JSONDoc(v any) error {
	return JSONDocTo(os.Stdout, v)
}

// JSONDocTo 是 JSONDoc 的显式 writer 版本
func JSONDocTo(w io.Writer, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}
	_, err = fmt.Fprintln(w, string(b))
	return err
}

// Package render 集中 action 层的终端表现原语: 结构化 JSON 输出、S3 路径
// 展示格式、时间格式、列举表格与树形绘制。
//
// 分包的目的不是"多一层", 而是把**怎么显示**与**做什么**分开:
//   - 本包不依赖 action 包 (只依赖 api / fmtutil / i18n / s3path), 因此不会
//     产生 import 环, 也让渲染逻辑可以脱离 S3 客户端单独测试;
//   - action 负责取数与决策, 把结果交给本包渲染。
//
// 约定: 需要 S3 类型的地方一律接收本包定义的最小视图结构 (如 LsRow /
// TreeNode), 而不是把 api.ObjectInfo 直接搬进来 —— 这样渲染不需要知道
// 分页、版本、目录标记等业务细节。
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"s3cli/internal/fmtutil"
)

// TimeLayout 是列举类输出统一的秒级时间格式。
const TimeLayout = "2006-01-02 15:04:05"

// TimeLayoutMinute 是精确到分钟的时间格式 (桶列表的创建时间)。
const TimeLayoutMinute = "2006-01-02 15:04"

// S3Path 把 (alias, bucket, key) 格式化为命令行风格的 "alias:bucket/key"。
// key 为空时省略斜杠, 得到 "alias:bucket"。
func S3Path(alias, bucket, key string) string {
	if key == "" {
		return alias + ":" + bucket
	}
	return alias + ":" + bucket + "/" + key
}

// JSONLine 以单行 JSON (JSON lines) 输出一条结构化结果。
//
// 各命令 --json 模式统一经此输出, 保证 schema 稳定 (完整字段表见
// docs/OUTPUT_SCHEMA.md); 所有 JSON 输出 (ls / du / stat / find / tree / diff /
// bucket config / acl / tag / mpu …) 都必须走这里, 否则格式会随实现漂移。
func JSONLine(v any) error {
	return JSONLineTo(os.Stdout, v)
}

// JSONLineTo 是 JSONLine 的显式 writer 版本 (测试用)。
func JSONLineTo(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}
	_, err = fmt.Fprintln(w, string(b))
	return err
}

// JSONDoc 以缩进多行 JSON 输出一份完整文档 (桶子资源配置、info 等)。
func JSONDoc(v any) error {
	return JSONDocTo(os.Stdout, v)
}

// JSONDocTo 是 JSONDoc 的显式 writer 版本 (测试用)。
func JSONDocTo(w io.Writer, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}
	_, err = fmt.Fprintln(w, string(b))
	return err
}

// PrintJSONDoc 以高亮色输出缩进 JSON 文档 (info / bucket xxx get 的风格)。
func PrintJSONDoc(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}
	fmtutil.PrintlnGreen(string(b))
	return nil
}

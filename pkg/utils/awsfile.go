package utils

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"strings"
)

// 读取 AWS 配置文件 (CORS/Lifecycle/Policy 等)
// 同时支持 JSON / XML，并处理 BOM。返回 (data, format)，format 为 "json" 或 "xml"
func LoadAWSConfigFile(path string) ([]byte, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read %s: %w", path, err)
	}

	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, "", fmt.Errorf("file %s is empty", path)
	}

	switch trimmed[0] {
	case '{', '[':
		return data, "json", nil
	case '<':
		return data, "xml", nil
	}
	return data, "", fmt.Errorf("unsupported format in %s: must be JSON or XML", path)
}

// UnmarshalAWS 自动按 JSON 或 XML 反序列化到任意 SDK 类型
func UnmarshalAWS(data []byte, format string, v any) error {
	switch format {
	case "json":
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(v); err != nil {
			if err2 := json.Unmarshal(data, v); err2 != nil {
				return fmt.Errorf("parse json: %w", err)
			}
		}
		return nil
	case "xml":
		if err := xml.Unmarshal(data, v); err != nil {
			return fmt.Errorf("parse xml: %w", err)
		}
		return nil
	}
	return fmt.Errorf("unknown format %q", format)
}

// ValidateJSON 仅检查内容是否合法 JSON
func ValidateJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(bytes.TrimSpace(data)))
	var v any
	if err := dec.Decode(&v); err != nil {
		preview := string(data)
		if len(preview) > 80 {
			preview = preview[:80] + "..."
		}
		return fmt.Errorf("invalid json (%s): %w", strings.TrimSpace(preview), err)
	}
	return nil
}

// bucket-cors-json_test.go 锁定 CORS 配置的 JSON 契约: 字段名与 AWS CLI / SDK 一致
// (CORSRules / AllowedOrigins / AllowedMethods / AllowedHeaders / ExposeHeaders)。
//
// 回归背景: json tag 曾是单数形式 (AllowedOrigin / AllowedMethod), 与 xml 元素名一致但与
// AWS CLI 不一致, 于是 aws s3api get-bucket-cors 的输出、以及手写的
// `{"CORSRules":[{"AllowedOrigins":[...],"AllowedMethods":[...]}]}` 都会把规则解析成空壳。

package api

import (
	"encoding/json"
	"strings"
	"testing"
)

const awsCLICorsJSON = `{
  "CORSRules": [
    {
      "ID": "rule1",
      "AllowedOrigins": ["https://example.com"],
      "AllowedMethods": ["GET", "PUT"],
      "AllowedHeaders": ["Authorization"],
      "ExposeHeaders": ["ETag"],
      "MaxAgeSeconds": 3000
    }
  ]
}`

func TestCorsJSONAcceptsAWSCLIShape(t *testing.T) {
	var cfg CorsConfig
	dec := json.NewDecoder(strings.NewReader(awsCLICorsJSON))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		t.Fatalf("AWS CLI 形状的 CORS JSON 未通过严格解析: %v", err)
	}
	if len(cfg.CORSRules) != 1 {
		t.Fatalf("应解析出 1 条规则, 得到 %d", len(cfg.CORSRules))
	}
	r := cfg.CORSRules[0]
	if len(r.AllowedOrigin) != 1 || r.AllowedOrigin[0] != "https://example.com" {
		t.Errorf("AllowedOrigins 未解析: %+v", r.AllowedOrigin)
	}
	if len(r.AllowedMethod) != 2 || r.AllowedMethod[0] != "GET" {
		t.Errorf("AllowedMethods 未解析: %+v", r.AllowedMethod)
	}
	if len(r.AllowedHeader) != 1 || r.AllowedHeader[0] != "Authorization" {
		t.Errorf("AllowedHeaders 未解析: %+v", r.AllowedHeader)
	}
	if len(r.ExposeHeader) != 1 || r.ExposeHeader[0] != "ETag" {
		t.Errorf("ExposeHeaders 未解析: %+v", r.ExposeHeader)
	}
	if r.ID != "rule1" || r.MaxAgeSeconds != 3000 {
		t.Errorf("ID/MaxAgeSeconds 未解析: %+v", r)
	}
}

func TestCorsJSONMarshalShape(t *testing.T) {
	var cfg CorsConfig
	if err := json.Unmarshal([]byte(awsCLICorsJSON), &cfg); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	for _, want := range []string{
		`"CORSRules"`, `"AllowedOrigins"`, `"AllowedMethods"`, `"AllowedHeaders"`, `"ExposeHeaders"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("JSON 输出缺少 %s: %s", want, got)
		}
	}
	for _, bad := range []string{"XMLName", "XMLNS", `"AllowedOrigin"`, `"AllowedMethod"`} {
		if strings.Contains(got, bad) {
			t.Errorf("JSON 输出泄漏 %s: %s", bad, got)
		}
	}
}

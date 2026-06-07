package utils

import (
	"encoding/xml"
	"testing"
)

// ── LoadAWSConfigFile ──────────────────────────────────────────────

func TestLoadAWSConfigFile_JSON(t *testing.T) {
	data, format, err := LoadAWSConfigFile("testdata/cors.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if format != "json" {
		t.Errorf("format = %q, want json", format)
	}
	if len(data) == 0 {
		t.Error("expected non-empty data")
	}
}

func TestLoadAWSConfigFile_XML(t *testing.T) {
	_, format, err := LoadAWSConfigFile("testdata/cors.xml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if format != "xml" {
		t.Errorf("format = %q, want xml", format)
	}
}

func TestLoadAWSConfigFile_BOM(t *testing.T) {
	// UTF-8 BOM (0xEF 0xBB 0xBF) 前缀应被自动剔除
	data, format, err := LoadAWSConfigFile("testdata/bom.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if format != "json" {
		t.Errorf("format = %q, want json", format)
	}
	// BOM 不应出现在数据开头
	if len(data) > 0 && data[0] == 0xEF {
		t.Error("BOM prefix was not stripped")
	}
	// 内容应该是合法 JSON
	if err := ValidateJSON(data); err != nil {
		t.Errorf("BOM-stripped data is not valid JSON: %v", err)
	}
}

func TestLoadAWSConfigFile_Array(t *testing.T) {
	_, format, err := LoadAWSConfigFile("testdata/array.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if format != "json" {
		t.Errorf("format = %q, want json", format)
	}
}

func TestLoadAWSConfigFile_NotFound(t *testing.T) {
	_, _, err := LoadAWSConfigFile("testdata/does_not_exist.json")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestLoadAWSConfigFile_Empty(t *testing.T) {
	_, _, err := LoadAWSConfigFile("testdata/empty.json")
	if err == nil {
		t.Error("expected error for empty file")
	}
}

func TestLoadAWSConfigFile_UnsupportedFormat(t *testing.T) {
	_, _, err := LoadAWSConfigFile("testdata/plain.txt")
	if err == nil {
		t.Error("expected error for unsupported format (plain text)")
	}
}

// ── ValidateJSON ───────────────────────────────────────────────────

func TestValidateJSON_Valid(t *testing.T) {
	tests := []string{
		"testdata/cors.json",
		"testdata/lifecycle.json",
		"testdata/array.json",
		"testdata/bom.json",
	}
	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			data, _, err := LoadAWSConfigFile(path)
			if err != nil {
				t.Fatalf("load failed: %v", err)
			}
			if err := ValidateJSON(data); err != nil {
				t.Errorf("ValidateJSON(%s) = %v, want nil", path, err)
			}
		})
	}
}

func TestValidateJSON_Invalid(t *testing.T) {
	data, _, err := LoadAWSConfigFile("testdata/invalid.json")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if err := ValidateJSON(data); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestValidateJSON_LongError(t *testing.T) {
	// 超过 80 字节的非 JSON 内容，验证错误信息截断
	long := make([]byte, 200)
	for i := range long {
		long[i] = 'x'
	}
	err := ValidateJSON(long)
	if err == nil {
		t.Error("expected error for non-JSON input")
	}
}

// ── UnmarshalAWS ───────────────────────────────────────────────────

func TestUnmarshalAWS_JSON(t *testing.T) {
	type Policy struct {
		Rules []struct {
			ID     string `json:"ID"`
			Status string `json:"Status"`
		} `json:"Rules"`
	}

	data, _, err := LoadAWSConfigFile("testdata/lifecycle.json")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	var p Policy
	if err := UnmarshalAWS(data, "json", &p); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(p.Rules) != 1 {
		t.Errorf("Rules count = %d, want 1", len(p.Rules))
	}
	if p.Rules[0].ID != "expire-logs" {
		t.Errorf("Rule ID = %q, want expire-logs", p.Rules[0].ID)
	}
}

func TestUnmarshalAWS_XML(t *testing.T) {
	type CorsRule struct {
		XMLName        xml.Name `xml:"CORSRule"`
		AllowedOrigins []string `xml:"AllowedOrigin"`
		AllowedMethods []string `xml:"AllowedMethod"`
	}
	type CORSConfig struct {
		XMLName xml.Name   `xml:"CORSConfiguration"`
		Rules   []CorsRule `xml:"CORSRule"`
	}

	data, _, err := LoadAWSConfigFile("testdata/cors.xml")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	var cfg CORSConfig
	if err := UnmarshalAWS(data, "xml", &cfg); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(cfg.Rules) != 1 {
		t.Errorf("Rules count = %d, want 1", len(cfg.Rules))
	}
}

func TestUnmarshalAWS_UnknownFormat(t *testing.T) {
	var v any
	err := UnmarshalAWS([]byte(`{}`), "yaml", &v)
	if err == nil {
		t.Error("expected error for unknown format")
	}
}

func TestUnmarshalAWS_InvalidJSON(t *testing.T) {
	data, _, _ := LoadAWSConfigFile("testdata/invalid.json")
	var v any
	err := UnmarshalAWS(data, "json", &v)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// ── 集成测试：JSON/XML 往返 ──────────────────────────────────────

func TestRoundTrip_LifecycleJSON(t *testing.T) {
	type LifecycleConfig struct {
		Rules []struct {
			ID         string            `json:"ID"`
			Status     string            `json:"Status"`
			Filter     map[string]string `json:"Filter"`
			Expiration map[string]int    `json:"Expiration"`
		} `json:"Rules"`
	}

	data, _, err := LoadAWSConfigFile("testdata/lifecycle.json")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	var cfg LifecycleConfig
	if err := UnmarshalAWS(data, "json", &cfg); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if cfg.Rules[0].Status != "Enabled" {
		t.Errorf("Status = %q, want Enabled", cfg.Rules[0].Status)
	}
	if cfg.Rules[0].Expiration["Days"] != 30 {
		t.Errorf("Expiration.Days = %d, want 30", cfg.Rules[0].Expiration["Days"])
	}
}

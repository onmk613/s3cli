package action

import (
	"os"
	"path/filepath"
	"testing"
)

type testConfig struct {
	Name  string `json:"name"`
	Value string `xml:"Value"`
}

func TestLoadAWSConfigFileAndUnmarshal(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(jsonPath, []byte("\xef\xbb\xbf{\"name\":\"value\"}"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, format, err := loadAWSConfigFile(jsonPath)
	if err != nil || format != "json" {
		t.Fatalf("format=%q err=%v", format, err)
	}
	var jsonValue testConfig
	if err := unmarshalAWS(data, format, &jsonValue); err != nil || jsonValue.Name != "value" {
		t.Fatalf("json=%#v err=%v", jsonValue, err)
	}

	xmlPath := filepath.Join(dir, "config.xml")
	if err := os.WriteFile(xmlPath, []byte("<Config><Value>x</Value></Config>"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, format, err = loadAWSConfigFile(xmlPath)
	if err != nil || format != "xml" {
		t.Fatalf("format=%q err=%v", format, err)
	}
	var xmlValue testConfig
	if err := unmarshalAWS(data, format, &xmlValue); err != nil || xmlValue.Value != "x" {
		t.Fatalf("xml=%#v err=%v", xmlValue, err)
	}
	if err := validateJSON([]byte("{")); err == nil {
		t.Fatal("expected malformed json error")
	}
}

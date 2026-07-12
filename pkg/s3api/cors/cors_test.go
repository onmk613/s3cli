package cors

import (
	"strings"
	"testing"
)

func TestCORSRoundTripNormalizesMethods(t *testing.T) {
	cfg, err := ParseBucketCorsConfig(strings.NewReader(`<CORSConfiguration><CORSRule><AllowedMethod>get</AllowedMethod><AllowedOrigin>*</AllowedOrigin></CORSRule></CORSConfiguration>`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.XMLNS == "" || cfg.CORSRules[0].AllowedMethod[0] != "GET" {
		t.Fatalf("config = %#v", cfg)
	}
	data, err := cfg.ToXML()
	if err != nil || !strings.Contains(string(data), "CORSConfiguration") {
		t.Fatalf("xml = %q, %v", data, err)
	}
}

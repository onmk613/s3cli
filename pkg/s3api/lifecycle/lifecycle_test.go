package lifecycle

import (
	"strings"
	"testing"
)

func TestLifecycleRoundTrip(t *testing.T) {
	cfg, err := ParseBucketLifecycleConfig(strings.NewReader(`<LifecycleConfiguration><Rule><ID>expire</ID><Status>Enabled</Status><Filter><Prefix>logs/</Prefix></Filter><Expiration><Days>30</Days></Expiration></Rule></LifecycleConfiguration>`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.XMLNS == "" || len(cfg.Rules) != 1 || cfg.Rules[0].Expiration == nil {
		t.Fatalf("config = %#v", cfg)
	}
	data, err := cfg.ToXML()
	if err != nil || !strings.Contains(string(data), "LifecycleConfiguration") {
		t.Fatalf("xml = %q, %v", data, err)
	}
}

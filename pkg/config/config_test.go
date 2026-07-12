package config

import "testing"

func TestResolveBucketLookup(t *testing.T) {
	cases := []struct {
		raw, mode, template string
		wantErr             bool
	}{
		{"", BucketLookupPath, "", false},
		{"virtual-hosted-style", BucketLookupDNS, "", false},
		{"https://%(bucket).objects.example.test", BucketLookupCustom, "https://%(bucket).objects.example.test", false},
		{"not-a-mode", "", "", true},
	}
	for _, tc := range cases {
		gotMode, gotTemplate, err := (&Static{BucketLookup: tc.raw}).ResolveBucketLookup()
		if tc.wantErr {
			if err == nil {
				t.Fatalf("%q: expected error", tc.raw)
			}
			continue
		}
		if err != nil || gotMode != tc.mode || gotTemplate != tc.template {
			t.Fatalf("%q: got (%q,%q,%v)", tc.raw, gotMode, gotTemplate, err)
		}
	}
}

func TestStaticDefaultsAndTrimming(t *testing.T) {
	c := Static{Region: " eu-west-1 ", AccessKey: " key ", SecretKey: " secret ", MaxRetries: 7}
	if c.GetRegion() != "eu-west-1" || c.GetAccessKey() != "key" || c.GetSecretKey() != "secret" || c.GetMaxRetries() != 7 {
		t.Fatalf("unexpected static getters: %#v", c)
	}
	if (&Static{}).GetRegion() != DefaultRegion || (&Static{}).GetMaxRetries() != 3 {
		t.Fatal("defaults were not applied")
	}
}

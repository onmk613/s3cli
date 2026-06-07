package config

import "testing"

func TestValidateCustomTemplate(t *testing.T) {
	ph := BucketPlaceholder // "%(bucket)"
	tests := []struct {
		name  string
		tpl   string
		valid bool
	}{
		{"valid https template", "https://www.%(bucket).example.com", true},
		{"valid http template", "http://%(bucket).s3.example.com", true},
		{"no placeholder", "https://www.example.com", false},
		{"placeholder at end", "https://%(bucket)", false},
		{"double placeholder", "https://%(bucket).%(bucket).com", false},
		{"result has no host", "%(bucket)/path", false},
		{"consecutive dots in host", "https://www..example.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateCustomTemplate(tt.tpl, ph)
			if got != tt.valid {
				t.Errorf("validateCustomTemplate(%q) = %v, want %v", tt.tpl, got, tt.valid)
			}
		})
	}
}

func TestResolveBucketLookup(t *testing.T) {
	tests := []struct {
		name   string
		lookup string
		want   string
	}{
		{"empty defaults to path", "", BucketLookupPath},
		{"path", "path", BucketLookupPath},
		{"path-style alias", "path-style", BucketLookupPath},
		{"dns", "dns", BucketLookupDNS},
		{"virtual alias", "virtual", BucketLookupDNS},
		{"vhost alias", "vhost", BucketLookupDNS},
		{"subdomain alias", "subdomain", BucketLookupDNS},
		{"custom template", "https://www.%(bucket).example.com", BucketLookupCustom},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Static{BucketLookup: tt.lookup}
			mode, _, err := c.ResolveBucketLookup()
			if err != nil {
				t.Errorf("ResolveBucketLookup(%q) unexpected error: %v", tt.lookup, err)
				return
			}
			if mode != tt.want {
				t.Errorf("ResolveBucketLookup(%q) = %q, want %q", tt.lookup, mode, tt.want)
			}
		})
	}
}

func TestResolveBucketLookupInvalid(t *testing.T) {
	invalid := []string{"invalid", "ftp://example.com", "something_else"}
	for _, v := range invalid {
		c := Static{BucketLookup: v}
		_, _, err := c.ResolveBucketLookup()
		if err == nil {
			t.Errorf("ResolveBucketLookup(%q) expected error, got nil", v)
		}
	}
}

func TestGetRegion(t *testing.T) {
	c := Static{}
	if c.GetRegion() != DefaultRegion {
		t.Errorf("empty region should default to %q", DefaultRegion)
	}

	c.Region = "us-west-2"
	if c.GetRegion() != "us-west-2" {
		t.Errorf("explicit region should be preserved")
	}
}

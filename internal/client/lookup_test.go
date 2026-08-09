package client

import "testing"

func TestCustomBucketLookupNeedsRegion(t *testing.T) {
	withRegion := &CustomBucketLookup{
		Template:          "https://%(bucket).s3.%(region).example.com",
		BucketPlaceholder: "%(bucket)",
		RegionPlaceholder: "%(region)",
	}
	if !withRegion.NeedsRegion() {
		t.Error("should need region")
	}
	withoutRegion := &CustomBucketLookup{
		Template:          "https://%(bucket).s3.example.com",
		BucketPlaceholder: "%(bucket)",
	}
	if withoutRegion.NeedsRegion() {
		t.Error("should not need region")
	}
	missing := &CustomBucketLookup{
		Template:          "https://%(bucket).s3.example.com",
		BucketPlaceholder: "%(bucket)",
		RegionPlaceholder: "%(region)",
	}
	if missing.NeedsRegion() {
		t.Error("placeholder set but absent -> false")
	}
}

func TestResolveCustomEndpoint(t *testing.T) {
	c := &CustomBucketLookup{
		Template:          "https://%(bucket).s3.%(region).example.com",
		BucketPlaceholder: "%(bucket)",
		RegionPlaceholder: "%(region)",
	}
	u, err := c.ResolveCustomEndpoint("mybucket", "us-west-2")
	if err != nil {
		t.Fatal(err)
	}
	if u.String() != "https://mybucket.s3.us-west-2.example.com" {
		t.Errorf("got %s", u)
	}

	c2 := &CustomBucketLookup{
		Template:          "https://%(bucket).s3.example.com",
		BucketPlaceholder: "%(bucket)",
	}
	u2, _ := c2.ResolveCustomEndpoint("bk", "ignored")
	if u2.Host != "bk.s3.example.com" {
		t.Errorf("got %s", u2)
	}

	if _, err := (&CustomBucketLookup{}).ResolveCustomEndpoint("b", ""); err == nil {
		t.Error("expected error for empty template")
	}
	if _, err := c.ResolveCustomEndpoint("", ""); err == nil {
		t.Error("expected error for empty bucket")
	}
}

func TestResolveCustomEndpointErrors(t *testing.T) {
	// 替换后不是合法 URL -> url.Parse 报错
	badURL := &CustomBucketLookup{
		Template:          "https://%(bucket).[bad",
		BucketPlaceholder: "%(bucket)",
	}
	if _, err := badURL.ResolveCustomEndpoint("b", ""); err == nil {
		t.Error("expected error for unparseable URL")
	}

	// 模板无 host -> empty host 错误
	noHost := &CustomBucketLookup{
		Template:          "plain-%(bucket)",
		BucketPlaceholder: "%(bucket)",
	}
	if _, err := noHost.ResolveCustomEndpoint("b", ""); err == nil {
		t.Error("expected error for empty host")
	}
}

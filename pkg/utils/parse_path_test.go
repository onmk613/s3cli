package utils

import (
	"errors"
	"testing"
)

func TestParseS3Path(t *testing.T) {
	cases := []struct {
		input                  string
		alias, bucket, key     string
		trailing               bool
		wantAliasOnly, wantErr bool
	}{
		{"prod:bucket", "prod", "bucket", "", false, false, false},
		{"prod:bucket/object.txt", "prod", "bucket", "object.txt", false, false, false},
		{"prod:bucket/prefix/", "prod", "bucket", "prefix/", true, false, false},
		{"prod", "prod", "", "", false, true, false},
		{"prod:", "", "", "", false, false, true},
		{"bad alias:bucket", "", "", "", false, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := ParseS3Path(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if tc.wantAliasOnly {
				if !errors.Is(err, ErrAliasOnly) {
					t.Fatalf("error = %v, want ErrAliasOnly", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if got.Alias != tc.alias || got.Bucket != tc.bucket || got.Key != tc.key || got.TrailingSlash != tc.trailing {
				t.Fatalf("parsed = %#v", got)
			}
		})
	}
}

func TestResolveDirDestPrefix(t *testing.T) {
	cases := []struct {
		src         string
		srcTrailing bool
		dst         string
		dstTrailing bool
		state       DestState
		want        string
		appendRel   bool
	}{
		{"source", false, "target/", true, DestNone, "target/source", true},
		{"source/", true, "target/", true, DestNone, "target", true},
		{"source/", true, "target", false, DestNone, "target", false},
		{"source", false, "target", false, DestDir, "target", true},
		{"source", false, "target", false, DestFile, "target", false},
	}
	for _, tc := range cases {
		got, appendRel := ResolveDirDestPrefix(tc.src, tc.srcTrailing, tc.dst, tc.dstTrailing, tc.state)
		if got != tc.want || appendRel != tc.appendRel {
			t.Fatalf("ResolveDirDestPrefix(%q,%q) = (%q,%v), want (%q,%v)", tc.src, tc.dst, got, appendRel, tc.want, tc.appendRel)
		}
	}
}

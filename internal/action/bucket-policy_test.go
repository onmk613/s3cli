package action

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildCannedPolicy(t *testing.T) {
	cases := []struct {
		name       string
		bucket     string
		prefix     string
		wantAction interface{} // string 或 []string
		wantRes    string
	}{
		{"public-read", "mybucket", "", "s3:GetObject", "arn:aws:s3:::mybucket/*"},
		{"public-read", "mybucket", "logs/", "s3:GetObject", "arn:aws:s3:::mybucket/logs/*"},
		{"public-read-write", "mybucket", "", []string{"s3:GetObject", "s3:PutObject", "s3:DeleteObject"}, "arn:aws:s3:::mybucket/*"},
		{"public-read-write", "mybucket", "data/", []string{"s3:GetObject", "s3:PutObject", "s3:DeleteObject"}, "arn:aws:s3:::mybucket/data/*"},
		{"public-read", "mybucket", "img", "s3:GetObject", "arn:aws:s3:::mybucket/img*"},
	}
	for _, tc := range cases {
		raw, err := buildCannedPolicy(tc.name, tc.bucket, tc.prefix)
		if err != nil {
			t.Fatalf("%s/%s: unexpected error: %v", tc.name, tc.prefix, err)
		}
		var doc struct {
			Version   string `json:"Version"`
			Statement []struct {
				Effect    string          `json:"Effect"`
				Principal string          `json:"Principal"`
				Action    json.RawMessage `json:"Action"`
				Resource  string          `json:"Resource"`
			} `json:"Statement"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("%s/%s: invalid json: %v\n%s", tc.name, tc.prefix, err, raw)
		}
		if doc.Version != "2012-10-17" || len(doc.Statement) != 1 {
			t.Fatalf("%s/%s: bad doc structure: %s", tc.name, tc.prefix, raw)
		}
		s := doc.Statement[0]
		if s.Effect != "Allow" || s.Principal != "*" || s.Resource != tc.wantRes {
			t.Fatalf("%s/%s: got Effect=%q Principal=%q Resource=%q, want Allow/*/ %q",
				tc.name, tc.prefix, s.Effect, s.Principal, s.Resource, tc.wantRes)
		}
		switch want := tc.wantAction.(type) {
		case string:
			var got string
			if err := json.Unmarshal(s.Action, &got); err != nil || got != want {
				t.Fatalf("%s/%s: action = %s, want %q", tc.name, tc.prefix, s.Action, want)
			}
		case []string:
			var got []string
			if err := json.Unmarshal(s.Action, &got); err != nil || strings.Join(got, ",") != strings.Join(want, ",") {
				t.Fatalf("%s/%s: action = %s, want %v", tc.name, tc.prefix, s.Action, want)
			}
		}
	}

	if _, err := buildCannedPolicy("bogus", "b", ""); err == nil {
		t.Fatal("expected error for unknown canned policy")
	}
}

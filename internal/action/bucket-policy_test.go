package action

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

func TestBuildAnonymousPolicy(t *testing.T) {
	cases := []struct {
		perm    string
		bucket  string
		prefix  string
		want    map[string]string // resource -> actions 集合
		wantStm int
	}{
		{"download", "mybucket", "", map[string]string{
			"arn:aws:s3:::mybucket":   "s3:GetBucketLocation,s3:ListBucket",
			"arn:aws:s3:::mybucket/*": "s3:GetObject",
		}, 3},
		{"download", "mybucket", "logs/", map[string]string{
			"arn:aws:s3:::mybucket":        "s3:GetBucketLocation,s3:ListBucket",
			"arn:aws:s3:::mybucket/logs/*": "s3:GetObject",
		}, 3},
		{"upload", "mybucket", "", map[string]string{
			"arn:aws:s3:::mybucket":   "s3:GetBucketLocation,s3:ListBucketMultipartUploads",
			"arn:aws:s3:::mybucket/*": "s3:AbortMultipartUpload,s3:DeleteObject,s3:ListMultipartUploadParts,s3:PutObject",
		}, 2},
		{"public", "mybucket", "img", map[string]string{
			"arn:aws:s3:::mybucket":      "s3:GetBucketLocation,s3:ListBucket,s3:ListBucketMultipartUploads",
			"arn:aws:s3:::mybucket/img*": "s3:AbortMultipartUpload,s3:DeleteObject,s3:GetObject,s3:ListMultipartUploadParts,s3:PutObject",
		}, 3},
	}
	for _, tc := range cases {
		raw, err := buildAnonymousPolicy(tc.perm, tc.bucket, tc.prefix)
		if err != nil {
			t.Fatalf("%s/%s: unexpected error: %v", tc.perm, tc.prefix, err)
		}
		var doc struct {
			Version    string `json:"Version"`
			Statements []struct {
				Action    []string       `json:"Action"`
				Condition map[string]any `json:"Condition"`
				Effect    string         `json:"Effect"`
				Principal map[string]any `json:"Principal"`
				Resource  []string       `json:"Resource"`
			} `json:"Statement"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("%s/%s: invalid json: %v\n%s", tc.perm, tc.prefix, err, raw)
		}
		if doc.Version != "2012-10-17" || len(doc.Statements) != tc.wantStm {
			t.Fatalf("%s/%s: bad doc: version=%s stmts=%d\n%s", tc.perm, tc.prefix, doc.Version, len(doc.Statements), raw)
		}
		got := map[string][]string{}
		for _, s := range doc.Statements {
			if s.Effect != "Allow" {
				t.Fatalf("%s/%s: effect=%s", tc.perm, tc.prefix, s.Effect)
			}
			if s.Principal["AWS"] == nil {
				t.Fatalf("%s/%s: principal=%v", tc.perm, tc.prefix, s.Principal)
			}
			if len(s.Resource) != 1 {
				t.Fatalf("%s/%s: resources=%v", tc.perm, tc.prefix, s.Resource)
			}
			got[s.Resource[0]] = append(got[s.Resource[0]], s.Action...)
		}
		for res, wantActions := range tc.want {
			actions := got[res]
			if actions == nil {
				t.Fatalf("%s/%s: missing resource %s in %s", tc.perm, tc.prefix, res, raw)
			}
			sort.Strings(actions)
			if strings.Join(actions, ",") != wantActions {
				t.Fatalf("%s/%s: resource %s actions = %v, want %s", tc.perm, tc.prefix, res, actions, wantActions)
			}
		}
		// 前缀场景: ListBucket 必须带 StringEquals s3:prefix 条件
		if tc.prefix != "" {
			found := false
			for _, s := range doc.Statements {
				if len(s.Resource) == 1 && s.Resource[0] == "arn:aws:s3:::"+tc.bucket && s.Condition != nil {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s/%s: ListBucket statement lacks condition:\n%s", tc.perm, tc.prefix, raw)
			}
		}
	}

	if _, err := buildAnonymousPolicy("bogus", "b", ""); err == nil {
		t.Fatal("expected error for unknown permission")
	}
}

func TestNormalizePermission(t *testing.T) {
	cases := map[string]string{
		"private": "private", "none": "private",
		"download": "download", "public-read": "download",
		"upload": "upload", "public-write": "upload",
		"public": "public", "public-read-write": "public",
	}
	for in, want := range cases {
		got, err := normalizePermission(in)
		if err != nil || got != want {
			t.Errorf("normalizePermission(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := normalizePermission("bogus"); err == nil {
		t.Error("expected error for unknown permission")
	}
}

func TestClassifyPolicyType(t *testing.T) {
	cases := []struct {
		name   string
		perm   string
		bucket string
		prefix string
	}{
		{"download", "download", "mybucket", ""},
		{"download-prefix", "download", "mybucket", "logs/"},
		{"upload", "upload", "mybucket", ""},
		{"upload-prefix", "upload", "mybucket", "incoming/"},
		{"public", "public", "mybucket", ""},
		{"public-prefix", "public", "mybucket", "img"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := buildAnonymousPolicy(tc.perm, tc.bucket, tc.prefix)
			if err != nil {
				t.Fatal(err)
			}
			got, err := classifyPolicyType(raw, tc.bucket)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.perm {
				t.Fatalf("classifyPolicyType = %q, want %q", got, tc.perm)
			}
		})
	}

	// 合法自定义策略 (单对象 Statement / 单字符串 Action / Sid) 识别为 custom
	custom := []byte(`{"Version":"2012-10-17","Statement":{"Effect":"Allow","Principal":{"AWS":"*"},"Action":"s3:GetObject","Resource":"arn:aws:s3:::mybucket/*","Sid":"custom"}}`)
	if got, err := classifyPolicyType(custom, "mybucket"); err != nil || got != "custom" {
		t.Fatalf("custom policy = %q, %v; want custom", got, err)
	}

	// Deny 语句不是匿名预定义策略
	deny := []byte(`{"Version":"2012-10-17","Statement":[{"Action":["s3:GetObject"],"Effect":"Deny","Principal":{"AWS":["*"]},"Resource":["arn:aws:s3:::mybucket/*"]}]}`)
	if got, _ := classifyPolicyType(deny, "mybucket"); got != "custom" {
		t.Fatalf("deny policy = %q, want custom", got)
	}

	// 预定义策略放到另一个桶名下识别为 custom (资源 ARN 不匹配)
	raw, _ := buildAnonymousPolicy("download", "otherbucket", "")
	if got, _ := classifyPolicyType(raw, "mybucket"); got != "custom" {
		t.Fatalf("mismatched bucket = %q, want custom", got)
	}

	// 非法 JSON 报错
	if _, err := classifyPolicyType([]byte(`{`), "mybucket"); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestGetPolicyDefaultOutputsType(t *testing.T) {
	c := newJSONTestClient(t)
	out := captureStdout(t, func() {
		if err := c.GetPolicy(GetPolicyOptions{}, "mybucket"); err != nil {
			t.Error(err)
		}
	})
	if !strings.Contains(out, "type: download") {
		t.Fatalf("default policy output missing type: %q", out)
	}
	if strings.Contains(out, `"Version"`) {
		t.Fatalf("default policy output should not include policy JSON: %q", out)
	}
}

func TestGetPolicyJSONOutputsRaw(t *testing.T) {
	c := newJSONTestClient(t)
	out := captureStdout(t, func() {
		if err := c.GetPolicy(GetPolicyOptions{JSON: true}, "mybucket"); err != nil {
			t.Error(err)
		}
	})
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("--json output is not valid JSON: %v\n%q", err, out)
	}
	if m["Version"] != "2012-10-17" {
		t.Fatalf("--json output = %v", m)
	}
}

func TestGetPolicyPrivate(t *testing.T) {
	c := newJSONTestClient(t)
	if err := c.S3.DeleteBucketPolicy(c.Ctx, "mybucket"); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if err := c.GetPolicy(GetPolicyOptions{}, "mybucket"); err != nil {
			t.Error(err)
		}
	})
	if !strings.Contains(out, "type: private") {
		t.Fatalf("private policy output = %q", out)
	}
	if err := c.GetPolicy(GetPolicyOptions{JSON: true}, "mybucket"); err == nil {
		t.Fatal("--json on a bucket without policy should fail")
	}
}

// object_lock_test.go 覆盖对象级 Object Lock (retention / legal-hold)。
//
// 此前 object-lock.go 全部为 0% 覆盖, 而它是 `object retention` /
// `object legal-hold` 两个已发布命令的唯一后端。Object Lock 是合规特性:
// 请求构造错了 (子资源名、versionId、XML 根元素) 会得到一个"看起来成功"
// 但其实没有施加保留的结果, 或者一个无法解除的错误保留。

package api

import (
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestObjectLockQuery(t *testing.T) {
	q := objectLockQuery("retention", "")
	if _, ok := q["retention"]; !ok {
		t.Fatal("subresource must be present")
	}
	if got := q.Get("retention"); got != "" {
		t.Fatalf("subresource value = %q, want empty (?retention form)", got)
	}
	if q.Has("versionId") {
		t.Fatal("versionId must be omitted when empty")
	}

	q = objectLockQuery("legal-hold", "v-123")
	if got := q.Get("versionId"); got != "v-123" {
		t.Fatalf("versionId = %q, want v-123", got)
	}
	if _, ok := q["legal-hold"]; !ok {
		t.Fatal("legal-hold subresource missing")
	}
}

// TestObjectLockEndpointsAndXML 覆盖四个操作的 HTTP 形状:
// 方法、子资源查询参数、versionId 透传、请求体 XML 根元素。
func TestObjectLockEndpointsAndXML(t *testing.T) {
	type captured struct {
		method   string
		query    string
		body     string
		hasMD5   bool
		ctHeader string
	}

	cases := []struct {
		name         string
		versionID    string
		call         func(*Client, context.Context) error
		wantMethod   string
		wantSubres   string
		wantBodyPart string // 请求体里必须出现的片段 (空 = 不应有请求体)
	}{
		{
			name:      "GetObjectRetention",
			versionID: "v1",
			call: func(c *Client, ctx context.Context) error {
				got, err := c.GetObjectRetention(ctx, "mybucket", "k.txt", "v1")
				if err != nil {
					return err
				}
				if got.Mode != "GOVERNANCE" || got.RetainUntilDate != "2027-01-01T00:00:00Z" {
					t.Errorf("parsed retention = %+v", got)
				}
				return nil
			},
			wantMethod: http.MethodGet, wantSubres: "retention",
		},
		{
			name:      "PutObjectRetention",
			versionID: "v2",
			call: func(c *Client, ctx context.Context) error {
				return c.PutObjectRetention(ctx, "mybucket", "k.txt", "v2", &ObjectLockRetention{
					Mode: "COMPLIANCE", RetainUntilDate: "2030-06-01T12:00:00Z",
				})
			},
			wantMethod: http.MethodPut, wantSubres: "retention",
			wantBodyPart: "<Retention>",
		},
		{
			name:      "GetObjectLegalHold",
			versionID: "",
			call: func(c *Client, ctx context.Context) error {
				got, err := c.GetObjectLegalHold(ctx, "mybucket", "k.txt", "")
				if err != nil {
					return err
				}
				if got.Status != "ON" {
					t.Errorf("parsed legal hold = %+v", got)
				}
				return nil
			},
			wantMethod: http.MethodGet, wantSubres: "legal-hold",
		},
		{
			name:      "PutObjectLegalHold",
			versionID: "v3",
			call: func(c *Client, ctx context.Context) error {
				return c.PutObjectLegalHold(ctx, "mybucket", "k.txt", "v3", &ObjectLockLegalHold{Status: "OFF"})
			},
			wantMethod: http.MethodPut, wantSubres: "legal-hold",
			wantBodyPart: "<LegalHold>",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var seen captured
			c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seen.method = r.Method
				seen.query = r.URL.RawQuery
				seen.hasMD5 = r.Header.Get("Content-MD5") != ""
				seen.ctHeader = r.Header.Get("Content-Type")
				if b, _ := io.ReadAll(r.Body); len(b) > 0 {
					seen.body = string(b)
				}
				switch {
				case strings.Contains(r.URL.RawQuery, "legal-hold"):
					_, _ = io.WriteString(w, `<LegalHold><Status>ON</Status></LegalHold>`)
				default:
					_, _ = io.WriteString(w, `<Retention><Mode>GOVERNANCE</Mode><RetainUntilDate>2027-01-01T00:00:00Z</RetainUntilDate></Retention>`)
				}
			}))

			if err := tc.call(c, context.Background()); err != nil {
				t.Fatal(err)
			}

			if seen.method != tc.wantMethod {
				t.Errorf("method = %s, want %s", seen.method, tc.wantMethod)
			}
			// 子资源必须以裸参数形式出现 (?retention / ?legal-hold),
			// 写成 ?retention= 之外的形式服务端不会识别为子资源请求。
			if !strings.Contains(seen.query, tc.wantSubres) {
				t.Errorf("query %q does not carry the %s subresource", seen.query, tc.wantSubres)
			}
			if tc.versionID != "" && !strings.Contains(seen.query, "versionId="+tc.versionID) {
				t.Errorf("query %q does not carry versionId=%s", seen.query, tc.versionID)
			}
			if tc.versionID == "" && strings.Contains(seen.query, "versionId") {
				t.Errorf("query %q must not carry versionId when empty", seen.query)
			}

			if tc.wantBodyPart != "" {
				if !strings.Contains(seen.body, tc.wantBodyPart) {
					t.Errorf("body %q does not contain %q", seen.body, tc.wantBodyPart)
				}
				// XML 根元素必须能被服务端按 Object Lock schema 解析。
				if err := xml.Unmarshal([]byte(seen.body), new(any)); err != nil {
					t.Errorf("body is not well-formed XML: %v (%q)", err, seen.body)
				}
				if !seen.hasMD5 {
					t.Error("PUT Object Lock must send Content-MD5 (S3 requires it for these subresources)")
				}
				if seen.ctHeader != "application/xml" {
					t.Errorf("Content-Type = %q, want application/xml", seen.ctHeader)
				}
			} else if seen.body != "" {
				t.Errorf("GET must not send a body, got %q", seen.body)
			}
		})
	}
}

// TestObjectLockParsesServerError 服务端拒绝 (如桶未启用 Object Lock) 时必须
// 上抛带错误码的错误, 而不是静默成功 —— 合规场景下"以为设上了"代价很高。
func TestObjectLockParsesServerError(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `<Error><Code>InvalidRequest</Code><Message>Bucket is missing Object Lock Configuration</Message></Error>`)
	}))

	_, err := c.GetObjectRetention(context.Background(), "mybucket", "k", "")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !HasCode(err, "InvalidRequest") {
		t.Fatalf("error code not preserved: %v", err)
	}

	err = c.PutObjectLegalHold(context.Background(), "mybucket", "k", "", &ObjectLockLegalHold{Status: "ON"})
	if err == nil {
		t.Fatal("expected an error from PutObjectLegalHold")
	}
	if !HasCode(err, "InvalidRequest") {
		t.Fatalf("error code not preserved: %v", err)
	}
}

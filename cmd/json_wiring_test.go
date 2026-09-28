package cmd

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"s3cli/internal/config"
	myprint "s3cli/internal/fmtutil"
)

// jsonWiringServer 是 --json 接线测试用的最小 S3 服务端:
// 支持 ListObjectsV2 (find) 与 GetObjectTagging (tag list)。
func jsonWiringServer(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case q.Has("tagging"):
			_, _ = io.WriteString(w, `<Tagging><TagSet><Tag><Key>env</Key><Value>prod</Value></Tag></TagSet></Tagging>`)
		case q.Get("list-type") == "2":
			_, _ = io.WriteString(w, `<ListBucketResult><IsTruncated>false</IsTruncated>`+
				`<Contents><Key>data/a.csv</Key><Size>9</Size></Contents>`+
				`</ListBucketResult>`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

// withJSONWiringEnv 安装一份指向 mock 服务端的别名配置, 并在测试结束后还原。
// alias 必须逐用例唯一: client.S3Clients 是按别名缓存的后端。
func withJSONWiringEnv(t *testing.T, alias, endpoint string, jsonFlag bool) {
	t.Helper()
	oldS, oldC, oldJSON := config.G.S, config.G.C, config.G.F.JSON
	t.Cleanup(func() {
		config.G.S, config.G.C, config.G.F.JSON = oldS, oldC, oldJSON
	})
	config.G.S = map[string]config.Static{
		alias: {HostBase: endpoint, AccessKey: "access", SecretKey: "secret", MaxRetries: 1},
	}
	config.G.C = "<test>"
	config.G.F.JSON = jsonFlag
}

// captureCmdStdout 捕获命令执行期间写入 os.Stdout 与全局 printer 的内容。
func captureCmdStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	myprint.SetWriter(w)
	defer func() {
		os.Stdout = old
		myprint.SetWriter(old)
	}()

	runErr := fn()
	_ = w.Close()
	b, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatal(readErr)
	}
	return string(b), runErr
}

// TestGlobalJSONFlagIsForwardedToCommands 是"flag 未接线"整类缺陷的回归测试。
//
// --json 在重构中从"每命令声明"改成根命令的持久 flag, 于是每个命令都必须在
// RunE 里显式读一次 config.G.F.JSON 并写入自己的 Options。find / diff / tag list
// 当时漏了这一步: action 层的 JSON 分支实现完整、单元测试也覆盖 (它们直接构造
// Options{JSON:true}), 但 CLI 永远走不到 —— --json 被静默忽略, README 却把三者
// 列为支持 JSON 的命令。这类缺陷只有"从命令层走一遍"才抓得到。
func TestGlobalJSONFlagIsForwardedToCommands(t *testing.T) {
	server := jsonWiringServer(t)

	localA := t.TempDir()
	localB := t.TempDir()
	if err := os.WriteFile(filepath.Join(localA, "only-a.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localB, "only-b.txt"), []byte("y"), 0o600); err != nil {
		t.Fatal(err)
	}

	// 每个用例一个独立别名, 避免 client.S3Clients 的别名级缓存串味。
	cases := []struct {
		name  string
		alias string
		build func() (func(args []string) error, error)
		args  []string
	}{
		{
			name:  "find",
			alias: "jsonfind",
			args:  []string{"jsonfind:bucket/data"},
			build: func() (func([]string) error, error) {
				c := NewFindCmd()
				c.SetContext(context.Background())
				return func(a []string) error { return c.RunE(c, a) }, nil
			},
		},
		{
			name:  "tag-list",
			alias: "jsontag",
			args:  []string{"jsontag:bucket/k.txt"},
			build: func() (func([]string) error, error) {
				c := NewListTagCmd()
				c.SetContext(context.Background())
				return func(a []string) error { return c.RunE(c, a) }, nil
			},
		},
		{
			name:  "diff",
			alias: "jsondiff",
			args:  []string{localA, localB},
			build: func() (func([]string) error, error) {
				c := NewDiffCmd()
				c.SetContext(context.Background())
				return func(a []string) error { return c.RunE(c, a) }, nil
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name+" with --json emits JSON", func(t *testing.T) {
			withJSONWiringEnv(t, tc.alias, server.URL, true)
			run, err := tc.build()
			if err != nil {
				t.Fatal(err)
			}
			out, _ := captureCmdStdout(t, func() error { return run(tc.args) })

			line := firstNonEmptyLine(out)
			if line == "" {
				t.Fatalf("no output; want JSON (output=%q)", out)
			}
			var v any
			if err := json.Unmarshal([]byte(line), &v); err != nil {
				t.Fatalf("--json output is not valid JSON: %v\nline=%q\nfull=%q", err, line, out)
			}
		})

		t.Run(tc.name+" without --json emits text", func(t *testing.T) {
			withJSONWiringEnv(t, tc.alias, server.URL, false)
			run, err := tc.build()
			if err != nil {
				t.Fatal(err)
			}
			out, _ := captureCmdStdout(t, func() error { return run(tc.args) })
			line := firstNonEmptyLine(out)
			if line == "" {
				t.Fatal("no output at all")
			}
			// 反向断言: 未开 --json 时不应是 JSON 文档/JSON lines。
			// (否则上面的正向用例可能因为"永远输出 JSON"而假通过。)
			var v any
			if err := json.Unmarshal([]byte(line), &v); err == nil {
				t.Fatalf("without --json the output should be text, got JSON: %q", line)
			}
		})
	}
}

func firstNonEmptyLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

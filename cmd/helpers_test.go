package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"s3cli/internal/action"
	"s3cli/internal/config"
	"s3cli/internal/s3path"
	"s3cli/pkg/s3iface"

	"github.com/spf13/cobra"
)

func snapshotConfig(t *testing.T) func() {
	t.Helper()
	oldG, oldPath := config.G, config.G.C
	config.G = &config.Config{}
	config.G.C = ""
	return func() {
		config.G, config.G.C = oldG, oldPath
	}
}

func TestWrapDisplayed(t *testing.T) {
	orig := &s3iface.ErrorResponse{Code: "X", StatusCode: 404}
	wrapped := wrapDisplayed(orig)
	if !errors.Is(wrapped, errAlreadyDisplayed) {
		t.Error("should wrap errAlreadyDisplayed")
	}
	if !errors.Is(wrapped, orig) {
		t.Error("should carry original")
	}
}

func TestExitCodeForError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, exitOK},
		{"generic", errors.New("boom"), exitGeneric},
		{"canceled", context.Canceled, exitCanceled},
		{"status404", &s3iface.ErrorResponse{StatusCode: 404}, exitNotFound},
		{"status403", &s3iface.ErrorResponse{StatusCode: 403}, exitForbidden},
		{"codeNoSuchKey", &s3iface.ErrorResponse{Code: "NoSuchKey"}, exitNotFound},
		{"codeAccessDenied", &s3iface.ErrorResponse{Code: "AccessDenied"}, exitForbidden},
		{"wrappedDisplayed404", fmt.Errorf("%w: %w", errAlreadyDisplayed, &s3iface.ErrorResponse{StatusCode: 404}), exitNotFound},
		{"wrappedGeneric", fmt.Errorf("%w: %w", errAlreadyDisplayed, errors.New("boom")), exitGeneric},
	}
	for _, tc := range cases {
		if got := exitCodeForError(tc.err); got != tc.want {
			t.Errorf("%s: exitCodeForError = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestExitCodeForErrorDiffer 用真实 diff 结果验证 exitDiffer 分支
// (action 包未导出 errDiffer 哨兵, 只能经由 action.Diff 构造)。
func TestExitCodeForErrorDiffer(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	if err := os.WriteFile(filepath.Join(dirA, "a.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirB, "a.txt"), []byte("world"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := action.Diff(action.DiffOptions{
		A:           &action.DiffEndpoint{Path: dirA, Ctx: context.Background()},
		B:           &action.DiffEndpoint{Path: dirB, Ctx: context.Background()},
		Mode:        action.DiffModeMD5,
		Recursive:   true,
		Concurrency: 2,
	})
	if !action.IsDifferErr(err) {
		t.Fatalf("Diff error = %v, want differ", err)
	}
	if got := exitCodeForError(err); got != exitDiffer {
		t.Errorf("exitCodeForError(differ) = %d, want %d", got, exitDiffer)
	}
}

func TestFormatPath(t *testing.T) {
	cases := []struct {
		sp   *s3path.Path
		want string
	}{
		{&s3path.Path{Alias: "a", Bucket: "b"}, "a:b"},
		{&s3path.Path{Alias: "a", Bucket: "b", Key: "k"}, "a:b/k"},
		{&s3path.Path{Alias: "a", Bucket: "b", Key: "k", TrailingSlash: true}, "a:b/k/"},
		{&s3path.Path{Alias: "a", Bucket: "b", Key: "k/", TrailingSlash: true}, "a:b/k/"},
	}
	for _, tc := range cases {
		if got := formatPath(tc.sp); got != tc.want {
			t.Errorf("got %q want %q", got, tc.want)
		}
	}
}

func TestSamePath(t *testing.T) {
	a := &s3path.Path{Alias: "x", Bucket: "b", Key: "k"}
	b := &s3path.Path{Alias: "x", Bucket: "b", Key: "k"}
	if !samePath(a, b) {
		t.Error("identical should match")
	}
	c := &s3path.Path{Alias: "x", Bucket: "b", Key: "k", TrailingSlash: true}
	if !samePath(a, c) {
		t.Error("differing only TrailingSlash should match")
	}
	d := &s3path.Path{Alias: "y", Bucket: "b", Key: "k"}
	if samePath(a, d) {
		t.Error("different alias should not match")
	}
}

func TestVersionString(t *testing.T) {
	oldV, oldC, oldB, oldG := Version, Commit, BuildDate, GoVersion
	Version, Commit, BuildDate, GoVersion = "v1.2.3", "abc", "2024", "go1.22"
	defer func() { Version, Commit, BuildDate, GoVersion = oldV, oldC, oldB, oldG }()
	v := version()
	for _, s := range []string{"v1.2.3", "abc", "2024", "go1.22"} {
		if !strings.Contains(v, s) {
			t.Errorf("version() missing %q: %s", s, v)
		}
	}
}

func TestCompleteAliases(t *testing.T) {
	restore := snapshotConfig(t)
	defer restore()
	config.G.S = map[string]config.Static{
		"alpha": {}, "beta": {}, "prod": {},
	}
	got, dir := completeAliases("al")
	if dir != (cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace) {
		t.Errorf("unexpected directive %v", dir)
	}
	if len(got) != 1 || got[0] != "alpha:" {
		t.Errorf("got %v", got)
	}
	all, _ := completeAliases("")
	if len(all) != 3 {
		t.Errorf("empty prefix should return all, got %d", len(all))
	}
}

func TestCompleteLocalFirstAndLast(t *testing.T) {
	var called bool
	fake := func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		called = true
		return []string{"x"}, cobra.ShellCompDirectiveNoFileComp
	}

	// CompleteLocalFirst: args 空 -> Default, 不调用 fake
	called = false
	first := CompleteLocalFirst(fake)
	_, dir := first(&cobra.Command{}, nil, "")
	if dir != cobra.ShellCompDirectiveDefault || called {
		t.Error("expected Default and fake not called")
	}
	// args 非空 -> 委托
	called = false
	first(&cobra.Command{}, []string{"a"}, "")
	if !called {
		t.Error("expected fake called")
	}

	// CompleteLocalLast: args 数 >= maxS3Args -> Default
	called = false
	last := CompleteLocalLast(fake, 1)
	_, dir = last(&cobra.Command{}, []string{"a"}, "")
	if dir != cobra.ShellCompDirectiveDefault || called {
		t.Error("expected Default when args>=maxS3Args")
	}
	// args 数 < maxS3Args -> 委托
	called = false
	last(&cobra.Command{}, nil, "")
	if !called {
		t.Error("expected fake called when args<maxS3Args")
	}
}

func TestGetClientByAliasUnknown(t *testing.T) {
	restore := snapshotConfig(t)
	defer restore()
	config.G.S = nil
	if c := getClientByAlias(context.Background(), "nope"); c != nil {
		t.Error("unknown alias should return nil")
	}
}

func TestParseExpireSeconds(t *testing.T) {
	cases := []struct {
		in   string
		want int
		err  bool
	}{
		{"3600", 3600, false},
		{"168h", 604800, false},
		{"7d", 604800, false},
		{"30m", 1800, false},
		{"", 0, true},
		{"0", 0, true},
		{"abc", 0, true},
	}
	for _, tc := range cases {
		got, err := parseExpireSeconds(tc.in)
		if tc.err {
			if err == nil {
				t.Errorf("parseExpireSeconds(%q) = %d, want error", tc.in, got)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("parseExpireSeconds(%q) = %d, %v; want %d", tc.in, got, err, tc.want)
		}
	}
}

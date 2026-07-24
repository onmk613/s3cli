package cmd

import (
	"context"
	"errors"
	"strings"
	"testing"

	"s3cli/internal/config"
	"s3cli/internal/s3path"
	"s3cli/pkg/s3api"

	"github.com/spf13/cobra"
)

func snapshotConfig(t *testing.T) func() {
	t.Helper()
	oldG, oldPath := config.G, config.ConfPath
	config.G = &config.Config{}
	config.ConfPath = ""
	return func() {
		config.G, config.ConfPath = oldG, oldPath
	}
}

func TestFormatUserError(t *testing.T) {
	if got := formatUserError(nil); got != "" {
		t.Errorf("nil -> %q", got)
	}
	apiErr := &s3api.ErrorResponse{Code: "AccessDenied", Message: "no perms"}
	if got := formatUserError(apiErr); got != "AccessDenied: no perms" {
		t.Errorf("got %q", got)
	}
	if got := formatUserError(errors.New("boom")); got != "boom" {
		t.Errorf("got %q", got)
	}
}

func TestWrapDisplayed(t *testing.T) {
	orig := &s3api.ErrorResponse{Code: "X", StatusCode: 404}
	wrapped := wrapDisplayed(orig)
	if !errors.Is(wrapped, errAlreadyDisplayed) {
		t.Error("should wrap errAlreadyDisplayed")
	}
	if !errors.Is(wrapped, orig) {
		t.Error("should carry original")
	}
}

func TestContextEnsureInit(t *testing.T) {
	c := &Context{}
	c.ensureInit()
	if c.Global == nil {
		t.Error("Global should be allocated")
	}
	g := &GlobalOptions{}
	c2 := &Context{Global: g}
	c2.ensureInit()
	if c2.Global != g {
		t.Error("existing Global should be preserved")
	}
}

func TestNewCmdContextVariants(t *testing.T) {
	c0 := newCmdContext()
	if c0.Global == nil {
		t.Error("Global nil")
	}
	if c0.ArgParseMode != ParseS3OnlyPath {
		t.Errorf("default mode = %v", c0.ArgParseMode)
	}
	c1 := newCmdContext(ParseArgsAndS3Path)
	if c1.ArgParseMode != ParseArgsAndS3Path {
		t.Error("mode not set")
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

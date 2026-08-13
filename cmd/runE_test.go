package cmd

import (
	"context"
	"errors"
	"testing"

	"s3cli/internal/action"
	"s3cli/internal/config"
	"s3cli/internal/s3path"

	"github.com/spf13/cobra"
)

// TestRunECancelPropagation RunE 工厂在 ctx 已取消时应返回 ctx.Err()
// (root 据此静默退出 130), 而不是返回 nil (此前会错误地退出 0)。
func TestRunECancelPropagation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	runE := newRunEWithMode(false, func(S3 action.Action, sp *s3path.Path, _ ArgParseMode) error {
		t.Fatal("fn should not run when ctx is already canceled")
		return nil
	})
	cmd := &cobra.Command{Use: "x"}
	cmd.SetContext(ctx)

	err := runE(cmd, []string{"alias:bucket"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// TestRunEAllowAliasOnlyGating 仅 ls 允许「仅别名」参数; 其它命令应报错且不执行 fn。
// 回归: AllowAliasOnly 曾是包级全局变量, NewLsCmd 置 true 后泄漏到其后构建的所有命令。
func TestRunEAllowAliasOnlyGating(t *testing.T) {
	oldG := config.G
	defer func() { config.G = oldG }()
	config.G = &config.Config{
		S: map[string]config.Static{"my": {HostBase: "https://s3.example.com", AccessKey: "a", SecretKey: "s"}},
		C: "",
	}

	cmd := &cobra.Command{Use: "x"}
	cmd.SetContext(context.Background())

	t.Run("alias-only rejected by default", func(t *testing.T) {
		called := false
		runE := newRunEWithMode(false, func(S3 action.Action, sp *s3path.Path, _ ArgParseMode) error {
			called = true
			return nil
		})
		err := runE(cmd, []string{"my"})
		if !errors.Is(err, errAlreadyDisplayed) {
			t.Fatalf("err = %v, want errAlreadyDisplayed", err)
		}
		if called {
			t.Fatal("fn should not run for alias-only arg")
		}
	})

	t.Run("alias-only allowed via NewRunEAllowAliasOnly", func(t *testing.T) {
		called := false
		runE := newRunEWithMode(true, func(S3 action.Action, sp *s3path.Path, _ ArgParseMode) error {
			called = true
			if sp.Bucket != "" {
				t.Errorf("bucket = %q, want empty for alias-only", sp.Bucket)
			}
			return nil
		})
		if err := runE(cmd, []string{"my"}); err != nil {
			t.Fatal(err)
		}
		if !called {
			t.Fatal("fn should run for alias-only arg")
		}
	})
}

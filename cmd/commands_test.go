package cmd

import (
	"context"
	"testing"

	"github.com/spf13/cobra"
)

func TestTransferCommandsExposeExpectedFlags(t *testing.T) {
	for _, tc := range []struct {
		name  string
		flags []string
		check func() []string
	}{
		{"get", []string{"recursive", "concurrency", "part-size", "range"}, func() []string {
			c := NewGetCmd()
			var got []string
			for _, f := range []string{"recursive", "concurrency", "part-size", "range"} {
				if c.Flags().Lookup(f) != nil {
					got = append(got, f)
				}
			}
			return got
		}},
		{"put", []string{"recursive", "concurrency", "part-size", "metadata"}, func() []string {
			c := NewPutCmd()
			var got []string
			for _, f := range []string{"recursive", "concurrency", "part-size", "metadata"} {
				if c.Flags().Lookup(f) != nil {
					got = append(got, f)
				}
			}
			return got
		}},
		{"mirror", []string{"remove", "overwrite", "dry-run", "concurrency", "part-size"}, func() []string {
			c := NewMirrorCmd()
			var got []string
			for _, f := range []string{"remove", "overwrite", "dry-run", "concurrency", "part-size"} {
				if c.Flags().Lookup(f) != nil {
					got = append(got, f)
				}
			}
			return got
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.check(); len(got) != len(tc.flags) {
				t.Fatalf("flags = %v, want %v", got, tc.flags)
			}
		})
	}
}

func TestCommandContextsAndCancellation(t *testing.T) {
	ctx := newCmdContext(ParseS3PathAndArgs)
	if ctx.Global == nil || ctx.ArgParseMode != ParseS3PathAndArgs {
		t.Fatalf("context = %#v", ctx)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if !isCanceled(cancelled) {
		t.Fatal("cancelled context should be recognized")
	}
}

// TestShouldSkipConfLoad 回归测试: 曾把 cmd.Root().Name() 放进父链遍历的
// skip 集合, 导致每个命令 (父链终点都是 root) 都跳过 LoadConf, 全工具不可用。
func TestShouldSkipConfLoad(t *testing.T) {
	root := &cobra.Command{Use: "s3cli"}
	ls := &cobra.Command{Use: "ls"}
	alias := &cobra.Command{Use: "alias"}
	aliasSet := &cobra.Command{Use: "set"}
	mpu := &cobra.Command{Use: "mpu"}
	mpuLocalList := &cobra.Command{Use: "local-list"}
	root.AddCommand(ls, alias, mpu)
	alias.AddCommand(aliasSet)
	mpu.AddCommand(mpuLocalList)

	cases := []struct {
		name string
		cmd  *cobra.Command
		want bool
	}{
		{"root itself", root, true},
		{"normal command loads conf", ls, false},
		{"alias skips", alias, true},
		{"alias subcommand skips", aliasSet, true},
		{"mpu local-list skips", mpuLocalList, true},
		{"mpu itself loads conf", mpu, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldSkipConfLoad(tc.cmd); got != tc.want {
				t.Fatalf("shouldSkipConfLoad(%s) = %v, want %v", tc.cmd.Name(), got, tc.want)
			}
		})
	}
}

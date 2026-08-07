package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestTransferCommandsExposeExpectedFlags(t *testing.T) {
	for _, tc := range []struct {
		name  string
		flags []string
		check func() []string
	}{
		{"get", []string{"recursive", "concurrency", "range"}, func() []string {
			c := NewGetCmd()
			var got []string
			for _, f := range []string{"recursive", "concurrency", "range"} {
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

func TestTransferCommandsUseArgParseAnnotations(t *testing.T) {
	if got := NewGetCmd().Annotations[AnnoArgParseMode]; got != ModeS3PathAndArgs {
		t.Errorf("get annotation = %q, want %q", got, ModeS3PathAndArgs)
	}
	if got := NewPutCmd().Annotations[AnnoArgParseMode]; got != ModeArgsAndS3Path {
		t.Errorf("put annotation = %q, want %q", got, ModeArgsAndS3Path)
	}
	if got := EventSetCmd().Annotations[AnnoArgParseMode]; got != ModeArgsAndS3Path {
		t.Errorf("bucket event set annotation = %q, want %q", got, ModeArgsAndS3Path)
	}
}

func TestPolicyGetCmdJSONFlag(t *testing.T) {
	if got := PolicyGetCmd().Flags().Lookup("json"); got == nil {
		t.Fatal("policy get missing --json flag")
	}
}

func TestSplitArgsModes(t *testing.T) {
	cmd := &cobra.Command{}

	cmd.Annotations = ParseArgsAndS3Path
	s3Args, opts, err := splitArgs(cmd, []string{"local-file", "a:b", "c:d"})
	if err != nil {
		t.Fatal(err)
	}
	if opts[AddedArgs] != "local-file" || len(s3Args) != 2 || s3Args[0] != "a:b" || s3Args[1] != "c:d" {
		t.Fatalf("ParseArgsAndS3Path: s3Args=%v opts=%v", s3Args, opts)
	}

	cmd.Annotations = ParseS3PathAndArgs
	s3Args, opts, err = splitArgs(cmd, []string{"a:b", "local-file"})
	if err != nil {
		t.Fatal(err)
	}
	if opts[AddedArgs] != "local-file" || len(s3Args) != 1 || s3Args[0] != "a:b" {
		t.Fatalf("ParseS3PathAndArgs: s3Args=%v opts=%v", s3Args, opts)
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

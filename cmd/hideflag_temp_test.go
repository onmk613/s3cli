package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestHideGlobalFlagPerSubcommandTemp(t *testing.T) {
	var jsonFlag bool

	root := &cobra.Command{Use: "s3cli", RunE: func(c *cobra.Command, _ []string) error { return nil }}
	root.PersistentFlags().BoolVar(&jsonFlag, "json", false, "global json flag")
	root.PersistentFlags().Bool("quiet", false, "global quiet flag")

	get := &cobra.Command{Use: "get", RunE: func(c *cobra.Command, _ []string) error { return nil }}
	shadow := root.PersistentFlags().Lookup("json")
	shadowCopy := *shadow
	shadowCopy.Hidden = true
	get.Flags().AddFlag(&shadowCopy)
	root.AddCommand(get)

	ls := &cobra.Command{Use: "ls", RunE: func(c *cobra.Command, _ []string) error { return nil }}
	root.AddCommand(ls)

	helpOf := func(name string) string {
		buf := &bytes.Buffer{}
		root.SetOut(buf)
		root.SetArgs([]string{name, "--help"})
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}

	getHelp := helpOf("get")
	lsHelp := helpOf("ls")

	t.Logf("--- get help ---\n%s", getHelp)
	t.Logf("--- ls help ---\n%s", lsHelp)

	if strings.Contains(getHelp, "json") {
		t.Errorf("get help should NOT show --json")
	}
	if !strings.Contains(getHelp, "quiet") {
		t.Errorf("get help should still show --quiet")
	}
	if !strings.Contains(lsHelp, "json") {
		t.Errorf("ls help should still show --json")
	}

	root.SetArgs([]string{"get", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !jsonFlag {
		t.Errorf("--json on get should still set the global flag")
	}
}

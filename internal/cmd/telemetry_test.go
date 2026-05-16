package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestResolveCmdForTelemetry(t *testing.T) {
	root := &cobra.Command{Use: "dross"}
	verify := &cobra.Command{Use: "verify", Run: func(*cobra.Command, []string) {}}
	finalize := &cobra.Command{Use: "finalize", Run: func(*cobra.Command, []string) {}}
	verify.AddCommand(finalize)
	root.AddCommand(verify)

	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no args", []string{}, "dross"},
		{"unknown subcommand", []string{"totally-fake"}, "dross"},
		{"known subcommand", []string{"verify"}, "dross verify"},
		{"deeper subcommand", []string{"verify", "finalize"}, "dross verify finalize"},
		{"known + bad flag", []string{"verify", "--no-such-flag"}, "dross verify"},
		{"help flag", []string{"--help"}, "dross"},
		{"version flag", []string{"--version"}, "dross"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ResolveCmdForTelemetry(root, c.args)
			if got == nil {
				t.Fatalf("ResolveCmdForTelemetry returned nil")
			}
			if got.CommandPath() != c.want {
				t.Errorf("CommandPath = %q want %q", got.CommandPath(), c.want)
			}
		})
	}
}

func TestResolveCmdForTelemetryNilRoot(t *testing.T) {
	if got := ResolveCmdForTelemetry(nil, []string{"verify"}); got != nil {
		t.Errorf("expected nil root to return nil, got %v", got)
	}
}

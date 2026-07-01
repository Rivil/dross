package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/telemetry"
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

// telemetryCovEnable isolates telemetry to a throwaway HOME (so events land in
// a temp dir, not the developer's real ~/.claude/dross) and clears the opt-out
// kill-switch so RecordCLIEvent actually writes. A missing defaults.toml under
// the temp HOME resolves to enabled=true (default-ON policy), so events flow.
func telemetryCovEnable(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DROSS_NO_TELEMETRY", "")
}

// TestTelemetryCover_CommandPathBranch exercises telemetry.go:28 (`if c != nil`)
// on both arms: a non-nil command records its CommandPath, a nil command records
// an empty Command. Negating the guard would blank the non-nil path (and panic on
// the nil path), so the recorded Command values distinguish the mutant.
func TestTelemetryCover_CommandPathBranch(t *testing.T) {
	telemetryCovEnable(t)

	root := &cobra.Command{Use: "dross"}
	foo := &cobra.Command{Use: "foo", Run: func(*cobra.Command, []string) {}}
	root.AddCommand(foo)

	RecordCLIEvent(foo, time.Millisecond, nil) // c != nil -> Command = "dross foo"
	RecordCLIEvent(nil, time.Millisecond, nil) // c == nil -> Command = ""

	evs, err := telemetry.Load(telemetryPath())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(evs) != 2 {
		t.Fatalf("want 2 events, got %d", len(evs))
	}
	var haveFoo, haveEmpty bool
	for _, e := range evs {
		switch e.Command {
		case "dross foo":
			haveFoo = true
		case "":
			haveEmpty = true
		}
	}
	if !haveFoo {
		t.Errorf("non-nil command should record CommandPath %q; events=%+v", "dross foo", evs)
	}
	if !haveEmpty {
		t.Errorf("nil command should record an empty Command; events=%+v", evs)
	}
}

// TestTelemetryCover_RepoHashInRepo exercises telemetry.go:32
// (`if root, err := FindRoot(); err == nil`) on the success arm: inside a .dross
// repo FindRoot succeeds and RepoHash is populated. Negating the guard would skip
// the hash and leave RepoHash empty here, so a non-empty RepoHash kills the mutant.
func TestTelemetryCover_RepoHashInRepo(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".dross"), 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, dir)         // cwd now has a .dross so FindRoot resolves
	telemetryCovEnable(t) // temp HOME + clear opt-out (after chdir set it)

	RecordCLIEvent(nil, 0, nil)

	evs, err := telemetry.Load(telemetryPath())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("want 1 event, got %d", len(evs))
	}
	if evs[0].RepoHash == "" {
		t.Errorf("inside a .dross repo RepoHash should be set, got empty")
	}
}

// TestTelemetryCover_RepoHashOutsideRepo exercises telemetry.go:32 on the failure
// arm: with no .dross in the cwd chain FindRoot errors and RepoHash stays empty.
// Negating the guard would hash the (empty) root path and produce a non-empty
// RepoHash, so an empty RepoHash here kills the mutant.
func TestTelemetryCover_RepoHashOutsideRepo(t *testing.T) {
	dir := t.TempDir() // no .dross anywhere in this temp chain
	chdir(t, dir)
	telemetryCovEnable(t)

	RecordCLIEvent(nil, 0, nil)

	evs, err := telemetry.Load(telemetryPath())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("want 1 event, got %d", len(evs))
	}
	if evs[0].RepoHash != "" {
		t.Errorf("outside a .dross repo RepoHash should be empty, got %q", evs[0].RepoHash)
	}
}

// TestTelemetryCover_RunErrBranch exercises telemetry.go:40 (`if runErr != nil`)
// on both arms: a non-nil error records ExitCode 1 plus a classified ErrorClass,
// a nil error records ExitCode 0 and an empty ErrorClass. Negating the guard
// swaps which invocation sets exit=1 and drops the classification, so pairing
// each ExitCode with its expected ErrorClass distinguishes the mutant.
func TestTelemetryCover_RunErrBranch(t *testing.T) {
	telemetryCovEnable(t)

	RecordCLIEvent(nil, 0, errors.New("thing not found")) // classifies to "missing"
	RecordCLIEvent(nil, 0, nil)

	evs, err := telemetry.Load(telemetryPath())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(evs) != 2 {
		t.Fatalf("want 2 events, got %d", len(evs))
	}
	var exit1, exit0 int
	for _, e := range evs {
		if e.ExitCode == 1 {
			exit1++
			if e.ErrorClass != "missing" {
				t.Errorf("exit=1 event should carry the classified error, ErrorClass=%q want %q", e.ErrorClass, "missing")
			}
		} else {
			exit0++
			if e.ErrorClass != "" {
				t.Errorf("exit=0 event should have empty ErrorClass, got %q", e.ErrorClass)
			}
		}
	}
	if exit1 != 1 {
		t.Errorf("want exactly one exit=1 event, got %d", exit1)
	}
	if exit0 != 1 {
		t.Errorf("want exactly one exit=0 event, got %d", exit0)
	}
}

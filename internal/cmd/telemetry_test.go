package cmd

import (
	"errors"
	"fmt"
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
	root := filepath.Join(dir, ".dross")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCompleteRoot(t, root) // FindRoot now requires a COMPLETE root
	chdir(t, dir)              // cwd now has a .dross so FindRoot resolves
	telemetryCovEnable(t)      // temp HOME + clear opt-out (after chdir set it)

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

// TestTelemetryDetailFreeBucketsWriteNoText is the privacy end of the
// detail_allowlist decision, asserted where the write actually happens rather
// than on the allowlist map alone. merge_pending, config_io and env_token
// messages embed a phase id, a config path and a token name respectively; if
// RecordCLIEvent ever stopped gating the detail on CarriesDetail, all three
// would reach the log. The assertions check both the bucket (so a
// misclassified row can't pass vacuously) and the empty err_detail.
func TestTelemetryDetailFreeBucketsWriteNoText(t *testing.T) {
	telemetryCovEnable(t)

	cases := []struct {
		err    error
		class  string
		secret string // the substring that must not reach the log
	}{
		{errors.New("PR #55 for 03-telemetry is not merged upstream — refusing to complete"), "merge_pending", "03-telemetry"},
		{errors.New("decode ~/proj/.dross/project.toml: toml: line 47: expected value"), "config_io", "project.toml"},
		{errors.New("$JIRA_API_TOKEN is not set; run `dross env set JIRA_API_TOKEN` in your shell"), "env_token", "JIRA_API_TOKEN"},
	}
	for _, c := range cases {
		RecordCLIEvent(nil, 0, c.err)
	}

	evs, err := telemetry.Load(telemetryPath())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(evs) != len(cases) {
		t.Fatalf("want %d events, got %d", len(cases), len(evs))
	}
	for i, e := range evs {
		c := cases[i]
		if e.ErrorClass != c.class {
			t.Errorf("event %d: ErrorClass = %q want %q", i, e.ErrorClass, c.class)
		}
		if e.ErrorDetail != "" {
			t.Errorf("event %d (%s): err_detail must be empty, got %q — %q would leak", i, c.class, e.ErrorDetail, c.secret)
		}
	}
}

// TestTelemetryRepoHashOnIncompleteRoot (c-3): a repo with a half-built
// `.dross/` is still this repo, so its failures must stay attributable. Both
// recorders are asserted — fixing telemetry.go alone leaves verify outcome
// events silently unattributed while CLI events keep their hash.
func TestTelemetryRepoHashOnIncompleteRoot(t *testing.T) {
	cases := []struct {
		name   string
		record func()
	}{
		{"cli event", func() { RecordCLIEvent(nil, 0, nil) }},
		{"verify outcome event", func() {
			recordVerifyPhaseOutcome("some-phase", map[string]int{}, nil, map[string]string{"result": "x"})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := realTempDir(t)
			mkRoot(t, dir, "project.toml") // incomplete: no state.json
			chdir(t, dir)
			telemetryCovEnable(t)

			tc.record()

			evs, err := telemetry.Load(telemetryPath())
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if len(evs) != 1 {
				t.Fatalf("want 1 event, got %d", len(evs))
			}
			if evs[0].RepoHash == "" {
				t.Error("an incomplete root should still attribute its events, got an empty RepoHash")
			}
		})
	}
}

// TestClassifyRealIncompleteRootError (c-3) pins the incomplete_root bucket
// against real error *values*, which its counterpart in internal/telemetry
// cannot: that package is imported by cmd, so it can only hand-copy the message
// text. A hand-copied literal keeps passing after root.go's prefix is reworded,
// while real incomplete-root failures quietly fall back into the opaque `other`
// bucket — the exact regression t-8 existed to prevent.
//
// The FindRoot rows are the load-bearing ones: they classify whatever the
// production path actually returns, not what this test thinks it returns.
// FindRoot is the entry point that mints the error — LocateRoot deliberately
// reports missing files without erroring, which is doctor's seam, not this one.
func TestClassifyRealIncompleteRootError(t *testing.T) {
	// incompleteRoot returns FindRoot's error inside a `.dross/` that exists
	// but is missing state.json.
	incompleteRoot := func(t *testing.T) error {
		t.Helper()
		dir := realTempDir(t)
		mkRoot(t, dir, "project.toml")
		chdir(t, dir)
		_, err := FindRoot()
		if err == nil {
			t.Fatal("FindRoot should reject an incomplete root")
		}
		return err
	}

	cases := []struct {
		name string
		err  func(*testing.T) error
		want string
	}{
		{
			name: "constructed value",
			err: func(*testing.T) error {
				return &IncompleteRootError{
					Root:    "/repo/.dross",
					Missing: []string{".dross/state.json"},
				}
			},
			want: "incomplete_root",
		},
		{
			name: "value FindRoot really returns",
			err:  incompleteRoot,
			want: "incomplete_root",
		},
		{
			// Telemetry never sees a bare sentinel — commands wrap first.
			name: "wrapped by a caller",
			err: func(t *testing.T) error {
				return fmt.Errorf("state show: %w", incompleteRoot(t))
			},
			want: "incomplete_root",
		},
		{
			// The absent-root case stays its own tier: different friction,
			// different fix (`dross init`, not `dross onboard`).
			name: "no .dross at all",
			err: func(t *testing.T) error {
				chdir(t, realTempDir(t))
				_, err := FindRoot()
				if err == nil {
					t.Fatal("FindRoot should reject a bare directory")
				}
				return err
			},
			want: "no_root",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.err(t)
			if got := telemetry.ClassifyError(err); got != tc.want {
				t.Errorf("ClassifyError(%v) = %q, want %q", err, got, tc.want)
			}
		})
	}
}

package telemetry

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppendAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), File)
	t.Setenv(OptOutEnv, "") // ensure not opted out

	in := []Event{
		{Kind: "cli", Command: "init", DurationMS: 12, ExitCode: 0},
		{Kind: "cli", Command: "milestone create", DurationMS: 4, ExitCode: 0},
		{Kind: "outcome", Command: "verify", Tags: map[string]string{"verdict": "pass"}, Numbers: map[string]float64{"mutation_score": 0.83}},
	}
	for _, ev := range in {
		if err := Append(path, ev); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(in) {
		t.Fatalf("len: got %d want %d", len(got), len(in))
	}
	if got[0].Command != "init" || got[2].Tags["verdict"] != "pass" {
		t.Errorf("round-trip drift: %+v", got)
	}
	if got[0].Schema != SchemaVersion {
		t.Errorf("schema not stamped: %d", got[0].Schema)
	}
	if got[0].Timestamp.IsZero() {
		t.Error("timestamp not stamped")
	}
}

func TestAppendRespectsOptOut(t *testing.T) {
	path := filepath.Join(t.TempDir(), File)
	t.Setenv(OptOutEnv, "1")
	if err := Append(path, Event{Kind: "cli", Command: "init"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("file should not exist when opted out, got err=%v", err)
	}
}

func TestAppendRotatesAtMaxBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, File)
	// Pre-seed a fat file just over MaxBytes.
	big := strings.Repeat("x", MaxBytes+1)
	if err := os.WriteFile(path, []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Append(path, Event{Kind: "cli", Command: "init"}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var sawRotated, sawCurrent bool
	for _, e := range entries {
		if e.Name() == File {
			sawCurrent = true
		} else if strings.HasPrefix(e.Name(), File+".") {
			sawRotated = true
		}
	}
	if !sawRotated || !sawCurrent {
		t.Errorf("expected rotated + current file: %v", entries)
	}
	// Load must read both files together.
	all, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	// Rotated file is junk text (no JSON) so only the new event survives.
	if len(all) != 1 || all[0].Command != "init" {
		t.Errorf("loaded events drift: %+v", all)
	}
}

func TestLoadTolerantOfCorruptLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), File)
	body := `{"schema":1,"ts":"2026-05-04T00:00:00Z","kind":"cli","cmd":"init"}
not json
{"schema":1,"ts":"2026-05-04T00:01:00Z","kind":"cli","cmd":"verify"}
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 events (corrupt line skipped), got %d", len(got))
	}
}

func TestLoadOrdersByTimestamp(t *testing.T) {
	path := filepath.Join(t.TempDir(), File)
	// Append intentionally out of order via timestamp override.
	for _, ev := range []Event{
		{Kind: "cli", Command: "later", Timestamp: time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)},
		{Kind: "cli", Command: "earlier", Timestamp: time.Date(2026, 5, 4, 8, 0, 0, 0, time.UTC)},
	} {
		if err := Append(path, ev); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Command != "earlier" {
		t.Errorf("order drift: %+v", got)
	}
}

func TestHashRepoStable(t *testing.T) {
	a := HashRepo("/a/b/c")
	b := HashRepo("/a/b/c")
	if a != b || len(a) != 12 {
		t.Errorf("hash unstable or wrong length: %q vs %q", a, b)
	}
	if HashRepo("/a/b/d") == a {
		t.Error("different paths should hash differently")
	}
	if HashRepo("") != "" {
		t.Error("empty path should hash empty")
	}
}

func TestClassifyError(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{nil, ""},
		{errors.New("no .dross directory found"), "no_root"},
		{errors.New("milestone v0.1 already exists"), "already_exists"},
		{errors.New("file not found"), "missing"},
		{errors.New("validate failed: missing field"), "invalid"},
		{errors.New("permission denied"), "permission"},
		{errors.New("git push failed"), "git"},
		{errors.New("http 500"), "network"},

		// phase / plan / spec state
		{errors.New("no phase id given and state has no current_phase"), "no_phase"},
		{errors.New("PhaseID is required"), "no_phase"},
		{errors.New("read spec spec.toml: open: no such file"), "no_spec"},
		{errors.New("decode plan plan.toml: bad toml"), "no_plan"},
		{errors.New("no version given and state has no current_milestone; run `dross milestone list` to see options"), "no_milestone"},
		{errors.New("load milestone .dross/milestones/v0.1.toml: open: no such file"), "no_milestone"},

		// phase-complete pre-flight: dirty tree, ff-only refusing because
		// the upstream merge hasn't happened. Both used to land in "other".
		{errors.New("working tree is dirty; commit or stash before completing"), "dirty_tree"},
		{errors.New("origin/main hasn't advanced past phase/04-x's base — has the PR actually merged upstream?"), "merge_pending"},
		{errors.New("fast-forward of main from origin failed: exit 1"), "merge_pending"},

		// phase-create pre-flight: refused because we're not on main (still
		// on a previous phase branch). Used to land in "other".
		{errors.New("must be on main to start a phase (currently on phase/03-x); switch back or use --no-branch"), "wrong_branch"},

		// state.json persistence failures — used to land in "other".
		{errors.New("save state: write .dross/state.json: permission denied"), "state_io"},
		{errors.New("unmarshal state: bad json"), "state_io"},
		{errors.New("marshal state: cycle"), "state_io"},

		// verify / mutation pipeline
		{errors.New("verify.toml not found at .dross/phases/01/verify.toml — run `dross verify 01` first"), "verify_state"},
		{errors.New("verify verdict is \"\" — fill in pass | partial | fail"), "verify_state"},
		{errors.New("load verify (run /dross-verify first?): open: ENOENT"), "verify_state"},
		{errors.New("stryker invocation failed: exit 1 (is stryker installed?)"), "mutation"},
		{errors.New("gremlins invocation failed: exec: \"gremlins\": not found"), "mutation"},
		{errors.New("mutation adapter not yet implemented"), "mutation"},
		{errors.New("ast-grep run: exec: \"ast-grep\": executable not found"), "mutation"},

		// provider
		{errors.New("forgejo backend needs APIBase (set [remote].api_base)"), "provider"},
		{errors.New("gitlab backend needs APIBase (set [remote].api_base)"), "provider"},
		{errors.New("unsupported provider \"bitbucket\""), "provider"},

		// issue-board sync — operational failures wrapped "board:", and the
		// not-implemented sentinel. Config errors above still read as provider.
		{errors.New("board: create issue: HTTP 422: bad payload"), "board"},
		{errors.New("issue-board sync is not implemented for this provider yet (forgejo/gitea only)"), "board"},

		// CLI surface
		{errors.New("unknown subcommand \"add\" for \"dross phase\""), "unknown_subcommand"},
		{errors.New("unknown field: nonsense"), "unknown_field"},
		{errors.New("unsupported segment \"patch\" (only `internal` is bumpable)"), "unknown_field"},
		{errors.New("not a list field (or unknown): scope.phases"), "unknown_field"},
		{errors.New("--pr is required"), "cli_args"},
		{errors.New("RepoDir is required"), "cli_args"},
		{errors.New("KEY must be non-empty"), "cli_args"},
		{errors.New("comment body is empty"), "cli_args"},

		// cancelled
		{errors.New("aborted: empty value"), "cancelled"},

		// health checks (doctor returns issues found as an error to gate CI)
		{errors.New("3 project-level issue(s) found"), "check_issues"},
		{errors.New("2 issues found"), "check_issues"},
		{errors.New("3 problem(s) found"), "check_issues"},

		{errors.New("something weird"), "other"},
	}
	for _, c := range cases {
		got := ClassifyError(c.err)
		if got != c.want {
			t.Errorf("ClassifyError(%v) = %q want %q", c.err, got, c.want)
		}
	}
}

func TestDetail(t *testing.T) {
	if got := Detail(nil); got != "" {
		t.Errorf("Detail(nil) = %q want empty", got)
	}

	// Plain message passes through, trimmed.
	if got := Detail(errors.New("  something weird happened  ")); got != "something weird happened" {
		t.Errorf("Detail trim = %q", got)
	}

	// Home directory is collapsed to ~ so absolute paths don't leak.
	t.Setenv("HOME", "/Users/someone")
	got := Detail(errors.New("open /Users/someone/Development/proj/.dross/state.json: no such file"))
	if strings.Contains(got, "/Users/someone") {
		t.Errorf("Detail leaked the home dir: %q", got)
	}
	if !strings.Contains(got, "~/Development/proj") {
		t.Errorf("Detail should collapse home to ~: %q", got)
	}

	// Long messages are capped (with an ellipsis) so a pathological wrap
	// can't bloat the log.
	long := Detail(errors.New(strings.Repeat("x", maxDetailLen+50)))
	if r := []rune(long); len(r) > maxDetailLen+1 {
		t.Errorf("Detail not capped: %d runes", len(r))
	}
	if !strings.HasSuffix(long, "…") {
		t.Errorf("capped Detail should end with ellipsis: %q", long)
	}
}

func TestCarriesDetail(t *testing.T) {
	carries := []string{"other", "unknown_subcommand", "unknown_field"}
	for _, c := range carries {
		if !CarriesDetail(c) {
			t.Errorf("CarriesDetail(%q) = false, want true", c)
		}
	}
	// Every other classified bucket must NOT carry text — that's the
	// privacy posture. Spot-check a representative set.
	for _, c := range []string{"", "dirty_tree", "merge_pending", "verify_state", "mutation", "no_root", "provider", "git"} {
		if CarriesDetail(c) {
			t.Errorf("CarriesDetail(%q) = true, want false", c)
		}
	}
}

func TestUnknownSubcommandCarriesRejectedToken(t *testing.T) {
	// The whole point of A4: the rejected token survives into err_detail so
	// the unknown_subcommand bucket reveals WHAT was typed, not just that
	// something was.
	err := errors.New(`unknown subcommand "list" for "dross task"`)
	if got := ClassifyError(err); got != "unknown_subcommand" {
		t.Fatalf("ClassifyError = %q, want unknown_subcommand", got)
	}
	if !CarriesDetail("unknown_subcommand") {
		t.Fatal("unknown_subcommand should carry detail")
	}
	if d := Detail(err); !strings.Contains(d, "list") {
		t.Errorf("detail should preserve the rejected token: %q", d)
	}
}

func TestUnknownFieldCarriesRejectedToken(t *testing.T) {
	err := errors.New("unknown milestone field: milestone.staus")
	if got := ClassifyError(err); got != "unknown_field" {
		t.Fatalf("ClassifyError = %q, want unknown_field", got)
	}
	if !CarriesDetail("unknown_field") {
		t.Fatal("unknown_field should carry detail")
	}
	if d := Detail(err); !strings.Contains(d, "milestone.staus") {
		t.Errorf("detail should preserve the rejected field path: %q", d)
	}
}

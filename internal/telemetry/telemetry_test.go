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
		{errors.New("unsupported provider \"bitbucket\""), "provider"},

		// CLI surface
		{errors.New("unknown field: nonsense"), "unknown_field"},
		{errors.New("unsupported segment \"patch\" (only `internal` is bumpable)"), "unknown_field"},
		{errors.New("--pr is required"), "cli_args"},
		{errors.New("RepoDir is required"), "cli_args"},
		{errors.New("KEY must be non-empty"), "cli_args"},
		{errors.New("comment body is empty"), "cli_args"},

		// cancelled
		{errors.New("aborted: empty value"), "cancelled"},

		{errors.New("something weird"), "other"},
	}
	for _, c := range cases {
		got := ClassifyError(c.err)
		if got != c.want {
			t.Errorf("ClassifyError(%v) = %q want %q", c.err, got, c.want)
		}
	}
}

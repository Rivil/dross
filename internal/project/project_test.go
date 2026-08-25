package project

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	original := &Project{
		Project: ProjectMeta{
			Name:        "feastahead",
			Version:     "1.2.3.4",
			Description: "meal planning",
			Created:     "2026-01-15",
		},
		Stack: Stack{
			Languages:      []string{"typescript", "go"},
			Frameworks:     []string{"sveltekit", "drizzle"},
			PackageManager: "pnpm",
			TestRunner:     "vitest",
			Locked: []LockedChoice{
				{Choice: "sveltekit", Why: "ts ssr", LockedAt: "2026-01-15"},
			},
		},
		Runtime: Runtime{
			Mode:           "docker",
			DevCommand:     "docker compose up app",
			TestCommand:    "docker compose exec app pnpm test",
			MigrateCommand: "docker compose exec app pnpm db:migrate",
			Services: map[string]Service{
				"app": {URL: "http://localhost:5173", Health: "/api/health"},
				"db":  {URL: "postgres://localhost:5432/x", Admin: "psql"},
			},
		},
		Repo: Repo{
			Layout:           "single",
			GitMainBranch:    "main",
			BranchPattern:    "feature/*",
			CommitConvention: "conventional",
			SquashMerge:      true,
		},
		Paths: Paths{Source: "src", Tests: "src", Migrations: "src/db/migrations"},
		Env:   Env{Files: []string{".env", ".env.local"}, SecretsLocation: "1password", Gitignored: true},
		Goals: Goals{
			CoreValue: "meal planning that respects household constraints",
			NonGoals:  []string{"realtime collab"},
		},
		Constraints: map[string]string{"hosting": "self-hosted"},
		Competition: []Competitor{{Name: "mealime", URL: "https://mealime.com", WhatTheyDo: "X"}},
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "project.toml")

	if err := original.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !reflect.DeepEqual(original, loaded) {
		t.Fatalf("round-trip mismatch:\noriginal: %+v\nloaded:   %+v", original, loaded)
	}
}

func TestLoadMissingFileReturnsError(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.toml")); err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

// TestLoadRefusesARemoteHostInTrackedConfig is c-2's tracked-config half.
//
// project.toml is committed, so a remote host set there would be the repo
// authorizing dross to copy the working tree onto a machine of its choosing and
// execute the test suite. The refusal names the key, the value and the verb
// that grants it properly — a bare "invalid config" leaves the user with no
// move.
func TestLoadRefusesARemoteHostInTrackedConfig(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		key  string
		val  string
	}{
		{"host", "[mutation]\n  remote_host = \"helicon\"\n", "mutation.remote_host", "helicon"},
		// Half a config is not a bypass.
		{"workdir alone", "[mutation]\n  remote_workdir = \"/srv/dross\"\n", "mutation.remote_workdir", "/srv/dross"},
		{"both", "[mutation]\n  remote_host = \"helicon\"\n  remote_workdir = \"/srv/dross\"\n", "mutation.remote_host", "helicon"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "project.toml")
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			p, err := Load(path)
			if err == nil {
				t.Fatalf("a committed remote host was accepted: %+v", p)
			}
			// nil, not a partly-usable Project: no caller can proceed on a
			// config dross has just refused to honour, and a non-nil return
			// invites exactly that.
			if p != nil {
				t.Errorf("Load returned %+v alongside its refusal", p)
			}
			for _, want := range []string{tc.key, tc.val, "dross mutation remote grant"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal does not mention %q: %v", want, err)
				}
			}
		})
	}
}

// TestLoadStillDecodesTheRealMutationKeys pins the other side of the trap: the
// refusal is keyed to the trap FIELDS, not to unknown keys generally.
//
// It matters because toml.DecodeFile ignores undecoded keys silently — which is
// both why the trap fields have to be declared at all, and why a refusal
// implemented as "reject anything unrecognised under [mutation]" would be a
// different, much broader change that breaks every forward-compatible config.
func TestLoadStillDecodesTheRealMutationKeys(t *testing.T) {
	body := `[mutation]
  adapters = ["gremlins", "stryker"]

  [mutation.gremlins]
    timeout_coefficient = 30

  [mutation.stryker]
    workdir = "web"
`
	path := filepath.Join(t.TempDir(), "project.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := Load(path)
	if err != nil {
		t.Fatalf("a real mutation config was refused: %v", err)
	}
	if !reflect.DeepEqual(p.Mutation.Adapters, []string{"gremlins", "stryker"}) {
		t.Errorf("adapters = %v", p.Mutation.Adapters)
	}
	if p.Mutation.Gremlins.TimeoutCoefficient != 30 {
		t.Errorf("timeout_coefficient = %d, want 30", p.Mutation.Gremlins.TimeoutCoefficient)
	}
	if p.Mutation.Stryker.Workdir != "web" {
		t.Errorf("stryker workdir = %q, want web", p.Mutation.Stryker.Workdir)
	}
}

// TestRepoOwnProjectTomlStillLoads: the trap is only worth having if it does not
// fire on the config dross itself ships. A repo that cannot load its own
// project.toml has traded one bug for a worse one.
func TestRepoOwnProjectTomlStillLoads(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		dir = filepath.Dir(dir)
	}
	path := filepath.Join(dir, ".dross", "project.toml")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("no repo project.toml at %s", path)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("dross's own project.toml no longer loads: %v", err)
	}
}

func TestSaveDefaultsAreOmittedAndOptionalsRemainEmpty(t *testing.T) {
	// Empty project should serialise without explosion and load back equal.
	p := &Project{}
	dir := t.TempDir()
	path := filepath.Join(dir, "project.toml")
	if err := p.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Project.Name != "" {
		t.Errorf("expected empty name, got %q", loaded.Project.Name)
	}
	if loaded.Repo.SquashMerge {
		t.Error("expected SquashMerge to default false")
	}
}

// TestTestLaneDecodes pins the [[runtime.test_lane]] wire format against
// hand-written toml rather than against a struct this package Saved: the
// document is what a user edits and what `dross test lane add` has to produce,
// so a decoder that only agrees with its own encoder proves nothing about it.
//
// A dropped toml tag or a missing field decodes to a zero-length slice with no
// error at all — toml.Decode ignores keys it has no home for — so the
// length assertion is the one that fails when the schema regresses.
func TestTestLaneDecodes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "project.toml")
	if err := os.WriteFile(path, []byte(`
[project]
name = "x"
version = "1.0.0.0"

[runtime]
mode = "native"
test_command = "go test ./..."

[[runtime.test_lane]]
name = "go"
match = ["internal/**/*.go", "main.go"]
command = "go test -count=1 ./..."
`), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(p.Runtime.TestLane) != 1 {
		t.Fatalf("want 1 lane, got %d — the toml tag or the Runtime field is missing", len(p.Runtime.TestLane))
	}
	lane := p.Runtime.TestLane[0]
	if lane.Name != "go" {
		t.Errorf("name = %q, want go", lane.Name)
	}
	if !reflect.DeepEqual(lane.Match, []string{"internal/**/*.go", "main.go"}) {
		t.Errorf("match = %v, want both globs in order", lane.Match)
	}
	if lane.Command != "go test -count=1 ./..." {
		t.Errorf("command = %q — this is the exact string consent fingerprints", lane.Command)
	}
	// Lanes are additive: declaring one must not disturb the scalar the
	// lane-less path still runs.
	if p.Runtime.TestCommand != "go test ./..." {
		t.Errorf("test_command = %q, want it untouched by the lane block", p.Runtime.TestCommand)
	}
}

// TestNoTestLaneIsAbsentFromTheDocument holds the opt-in half of the schema:
// a project with no lane must not grow a test_lane key on Save. Without
// omitempty the encoder writes an empty array into every project.toml dross
// touches, which turns "this repo has no lanes" into a line the user has to
// read and wonder about.
func TestNoTestLaneIsAbsentFromTheDocument(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "project.toml")
	p := &Project{Project: ProjectMeta{Name: "x", Version: "1.0.0.0"}, Runtime: Runtime{Mode: "native"}}
	if err := p.Save(path); err != nil {
		t.Fatal(err)
	}
	if body := string(mustReadFile(t, path)); strings.Contains(body, "test_lane") {
		t.Errorf("a lane-less project.toml mentions test_lane:\n%s", body)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

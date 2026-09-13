package project

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Every Save test here follows one shape: seed a file, Load it, mutate the
// struct, Save, and assert two things — the set of changed lines is exactly
// the targeted field's, and Load of the result is canonically equal to the
// struct that was saved. The first is c-1/c-2 (nothing else moved); the
// second is the semantics (what moved, moved correctly).

func seed(t *testing.T, src []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "project.toml")
	if err := os.WriteFile(path, src, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustLoad(t *testing.T, path string) *Project {
	t.Helper()
	p, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return p
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// saveAndCheck saves p over path and returns the new bytes after proving
// Load(path) is canonically equal to p.
func saveAndCheck(t *testing.T, p *Project, path string) []byte {
	t.Helper()
	if err := p.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	assertLoadsAs(t, path, p)
	return mustRead(t, path)
}

func assertLoadsAs(t *testing.T, path string, p *Project) {
	t.Helper()
	got, err := encodeFresh(mustLoad(t, path))
	if err != nil {
		t.Fatal(err)
	}
	want, err := encodeFresh(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("Load(saved) is not the struct that was saved\n--- loaded ---\n%s\n--- saved ---\n%s", got, want)
	}
}

func TestSaveRefusesUnverifiedPatch(t *testing.T) {
	src := fixture(t)
	path := seed(t, src)
	p := mustLoad(t, path)
	p.Project.Version = "2.0.0.0"

	// Hand Save an op whose rendered value is the wrong type for the field:
	// the patched text either fails to decode or loads as a different
	// struct, and either way the original bytes must survive.
	orig := planOps
	planOps = func(old, new *Project) ([]op, error) {
		ops, err := diff(old, new)
		if err != nil {
			return nil, err
		}
		ops[0].value = int64(42)
		return ops, nil
	}
	defer func() { planOps = orig }()

	err := p.Save(path)
	if err == nil {
		t.Fatal("a mis-typed patch was written")
	}
	if !strings.Contains(err.Error(), "left untouched") {
		t.Errorf("error does not say the file was left alone: %v", err)
	}
	if got := mustRead(t, path); !bytes.Equal(got, src) {
		t.Errorf("original bytes were replaced:\n%s", got)
	}
}

func TestSaveNeverOverwritesARefusedFile(t *testing.T) {
	src := []byte("[project]\n  name = \"x\"\n\n[mutation]\n  remote_host = \"helicon\"\n")
	path := seed(t, src)
	p := &Project{Project: ProjectMeta{Name: "y"}}
	err := p.Save(path)
	if err == nil {
		t.Fatal("Save over a refused file succeeded")
	}
	if !strings.Contains(err.Error(), "mutation.remote_host") {
		t.Errorf("error is not the refusal: %v", err)
	}
	if got := mustRead(t, path); !bytes.Equal(got, src) {
		t.Errorf("refused file was overwritten:\n%s", got)
	}
}

func TestSaveNoopIsByteIdentical(t *testing.T) {
	src := fixture(t)
	path := seed(t, src)
	p := mustLoad(t, path)
	if err := p.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	if got := mustRead(t, path); !bytes.Equal(got, src) {
		added, removed := changedLines(src, got)
		t.Errorf("Load→Save changed the file: added %q removed %q", added, removed)
	}
}

func TestSaveNoChangeLeavesMtime(t *testing.T) {
	path := seed(t, fixture(t))
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}
	p := mustLoad(t, path)
	if err := p.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !st.ModTime().Equal(past) {
		t.Errorf("mtime moved from %v to %v on a no-change Save", past, st.ModTime())
	}
}

func TestSaveSingleFieldChangesOneLine(t *testing.T) {
	src := fixture(t)
	path := seed(t, src)
	p := mustLoad(t, path)
	p.Project.Version = "1.7.11.0"
	out := saveAndCheck(t, p, path)
	assertChanged(t, src, out,
		[]string{`  version = "1.7.11.0"  # bumped`},
		[]string{`  version = "1.7.5.0"  # bumped`})
}

func TestSaveOmitemptyZeroDeletesLine(t *testing.T) {
	src := fixture(t)
	path := seed(t, src)
	p := mustLoad(t, path)
	p.Board.Enabled = false
	out := saveAndCheck(t, p, path)
	assertChanged(t, src, out, nil, []string{`  enabled = true`})
}

func TestSaveUnsetDeletesLine(t *testing.T) {
	src := fixture(t)
	path := seed(t, src)
	p := mustLoad(t, path)
	p.Stack.Profile = "go"
	withProfile := saveAndCheck(t, p, path)
	assertChanged(t, src, withProfile, []string{`  profile = "go"`}, nil)

	p = mustLoad(t, path)
	p.Stack.Profile = ""
	out := saveAndCheck(t, p, path)
	assertChanged(t, withProfile, out, nil, []string{`  profile = "go"`})
	if !bytes.Equal(out, src) {
		t.Error("set then unset did not restore the original bytes")
	}

	p = mustLoad(t, path)
	p.Repo.SquashMerge = false
	out = saveAndCheck(t, p, path)
	assertChanged(t, src, out, []string{`  squash_merge = false`}, []string{`  squash_merge = true`})
}

func TestSaveListReplacesOneValue(t *testing.T) {
	src := fixture(t)
	path := seed(t, src)
	p := mustLoad(t, path)
	p.Stack.Languages = []string{"go", "sql"}
	out := saveAndCheck(t, p, path)
	assertChanged(t, src, out, []string{`  languages = ["go", "sql"]`}, []string{`  languages = ["go"]`})
}

func TestSaveStateMapPerKey(t *testing.T) {
	// Existing [board.state_map] with two entries: a third is one line.
	src := fixture(t)
	path := seed(t, src)
	p := mustLoad(t, path)
	p.Board.StateMap["verified"] = "Done"
	two := saveAndCheck(t, p, path)
	assertChanged(t, src, two, []string{`    verified = "Done"`}, nil)
	p = mustLoad(t, path)
	p.Board.StateMap["uat"] = "UAT"
	three := saveAndCheck(t, p, path)
	assertChanged(t, two, three, []string{`    uat = "UAT"`}, nil)

	// No such table: header + one line, after [board]'s last key, before
	// the next header.
	src = []byte("[board]\n  provider = \"youtrack\"\n  enabled = true\n\n[paths]\n\n[env]\n")
	path = seed(t, src)
	p = mustLoad(t, path)
	p.Board.StateMap = map[string]string{"planned": "Open"}
	out := saveAndCheck(t, p, path)
	assertChanged(t, src, out, []string{``, `  [board.state_map]`, `    planned = "Open"`}, nil)
	if !bytes.Contains(out, []byte("  enabled = true\n\n  [board.state_map]\n    planned = \"Open\"\n\n[paths]\n")) {
		t.Errorf("placement:\n%s", out)
	}
}

func TestSaveMapTablesAreLeafOps(t *testing.T) {
	src := fixture(t)
	path := seed(t, src)
	p := mustLoad(t, path)
	p.Runtime.Services = map[string]Service{"db": {URL: "postgres://localhost/x", Admin: "psql"}}
	out := saveAndCheck(t, p, path)
	assertChanged(t, src, out, []string{
		``,
		`    [runtime.services.db]`,
		`      url = "postgres://localhost/x"`,
		`      admin = "psql"`,
	}, nil)
	// Inside the runtime region: after the last lane block, before [repo].
	if !bytes.Contains(out, []byte("    command = \"go test ./internal/ship/...\"\n\n    [runtime.services.db]\n      url = \"postgres://localhost/x\"\n      admin = \"psql\"\n\n[repo]\n")) {
		t.Errorf("services placement:\n%s", out)
	}

	p = mustLoad(t, path)
	p.Constraints = map[string]string{"hosting": "self-hosted", "budget": "none"}
	withConstraints := saveAndCheck(t, p, path)
	p = mustLoad(t, path)
	p.Constraints["hosting"] = "cloud"
	out = saveAndCheck(t, p, path)
	assertChanged(t, withConstraints, out, []string{`  hosting = "cloud"`}, []string{`  hosting = "self-hosted"`})
}

func TestSaveArrayOfTablesAlignment(t *testing.T) {
	src := fixture(t)

	t.Run("append adds one block after the third", func(t *testing.T) {
		path := seed(t, src)
		p := mustLoad(t, path)
		p.Runtime.TestLane = append(p.Runtime.TestLane, TestLane{Name: "verify", Match: []string{"internal/verify/**"}, Command: "go test ./internal/verify/..."})
		out := saveAndCheck(t, p, path)
		assertChanged(t, src, out, []string{
			``,
			`  [[runtime.test_lane]]`,
			`    name = "verify"`,
			`    match = ["internal/verify/**"]`,
			`    command = "go test ./internal/verify/..."`,
		}, nil)
		if !bytes.Contains(out, []byte("    command = \"go test ./internal/ship/...\"\n\n  [[runtime.test_lane]]\n    name = \"verify\"\n")) {
			t.Errorf("fourth lane placement:\n%s", out)
		}
	})

	t.Run("edit lane 1 command keeps its comment", func(t *testing.T) {
		path := seed(t, src)
		p := mustLoad(t, path)
		p.Runtime.TestLane[1].Command = "go test -race ./internal/project/..."
		out := saveAndCheck(t, p, path)
		assertChanged(t, src, out,
			[]string{`    command = "go test -race ./internal/project/..."`},
			[]string{`    command = "go test ./internal/project/..."`})
		if !bytes.Contains(out, []byte("  [[runtime.test_lane]]\n    # the project package is the fast lane\n    name = \"project\"\n")) {
			t.Errorf("lane 1's comment did not survive:\n%s", out)
		}
	})

	t.Run("remove lane 0 removes only its block", func(t *testing.T) {
		path := seed(t, src)
		p := mustLoad(t, path)
		p.Runtime.TestLane = p.Runtime.TestLane[1:]
		out := saveAndCheck(t, p, path)
		assertChanged(t, src, out, nil, []string{
			``,
			`  [[runtime.test_lane]]`,
			`    name = "cmd"`,
			`    match = ["internal/cmd/**"]`,
			`    command = "go test ./internal/cmd/..."`,
		})
	})
}

func TestSaveFailureLeavesOriginalBytes(t *testing.T) {
	src := []byte("[project]\n  name = \"x\"\n  version = \"1\"\n  created = \"d\"\n\n[board]\n  provider = \"youtrack\"\n  state_map = { planned = \"Open\" }\n")
	path := seed(t, src)
	p := mustLoad(t, path)
	p.Board.StateMap["uat"] = "UAT"
	err := p.Save(path)
	if err == nil {
		t.Fatal("Save through an inline table succeeded")
	}
	if !strings.Contains(err.Error(), "inline table") {
		t.Errorf("error is not the inline refusal: %v", err)
	}
	if got := mustRead(t, path); !bytes.Equal(got, src) {
		t.Errorf("on-disk bytes changed after a refused patch:\n%s", got)
	}
	// And no temp file was left beside it.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("stray files after a refused Save: %v", names)
	}
}

func TestSaveOntoEmptyFile(t *testing.T) {
	path := seed(t, nil)
	p := &Project{
		Project: ProjectMeta{Name: "fresh", Version: "0.1.0.0", Created: "2026-09-13"},
		Stack:   Stack{Languages: []string{"go"}, Locked: []LockedChoice{{Choice: "go", Why: "static", LockedAt: "2026-09-13"}}},
		Runtime: Runtime{Mode: "native", TestCommand: "go test ./..."},
		Repo:    Repo{Layout: "single", GitMainBranch: "main", SquashMerge: true},
		Board:   Board{Provider: "youtrack", StateMap: map[string]string{"planned": "Open"}},
	}
	out := saveAndCheck(t, p, path)
	if len(out) == 0 {
		t.Fatal("nothing written onto the empty file")
	}
}

func TestSavePreservesFileMode(t *testing.T) {
	path := seed(t, fixture(t))
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	p := mustLoad(t, path)
	p.Project.Version = "9.9.9.9"
	saveAndCheck(t, p, path)
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("mode after Save = %v, want 0600", st.Mode().Perm())
	}
}

// TestSaveRefusesReadOnlyFile: the atomic rename could replace a read-only
// file — only the directory gates a rename — so Save probes the file's own
// write permission first and refuses the way os.Create used to.
func TestSaveRefusesReadOnlyFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions")
	}
	src := fixture(t)
	path := seed(t, src)
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	p := mustLoad(t, path)
	p.Project.Version = "9.9.9.9"
	err := p.Save(path)
	if err == nil {
		t.Fatal("Save replaced a read-only project.toml")
	}
	if !strings.Contains(err.Error(), "project.toml") {
		t.Errorf("error should name the file: %v", err)
	}
	if got := mustRead(t, path); !bytes.Equal(got, src) {
		t.Errorf("read-only file changed:\n%s", got)
	}
}

package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// realTempDir is t.TempDir() with symlinks resolved. On macOS /var is a
// symlink to /private/var, so the path FindRoot derives from the (resolved)
// cwd would never string-match an unresolved fixture path.
func realTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// mkRoot creates <dir>/.dross containing exactly the named files and returns
// the .dross path. Passing no names leaves it bare — an incomplete root.
func mkRoot(t *testing.T, dir string, files ...string) string {
	t.Helper()
	root := filepath.Join(dir, RootDirName)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(""), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// TestFindRootIncompleteIsNoRoot pins c-1: a .dross missing a required file is
// reported as not-a-dross-repo, and the error unwraps to ErrNoRoot so every
// hook target stays silent. Unwrapping the sentinel makes them all go loud.
func TestFindRootIncompleteIsNoRoot(t *testing.T) {
	dir := realTempDir(t)
	mkRoot(t, dir, "project.toml")
	chdir(t, dir)

	_, err := FindRoot()
	if err == nil {
		t.Fatal("FindRoot on an incomplete .dross should error")
	}
	if !errors.Is(err, ErrNoRoot) {
		t.Fatalf("error should unwrap to ErrNoRoot, got %v", err)
	}
}

// TestFindRootIncompleteMessage covers every miss combination: the message
// names each absent path and carries the shared repair hint, and the
// single-miss rows never name the file that is present.
func TestFindRootIncompleteMessage(t *testing.T) {
	cases := []struct {
		name    string
		present []string
		want    []string
		absent  []string
	}{
		{
			name:    "state.json absent",
			present: []string{"project.toml"},
			want:    []string{".dross/state.json"},
			absent:  []string{"project.toml"},
		},
		{
			name:    "project.toml absent",
			present: []string{"state.json"},
			want:    []string{".dross/project.toml"},
			absent:  []string{"state.json"},
		},
		{
			name:    "both absent",
			present: nil,
			want:    []string{".dross/project.toml", ".dross/state.json"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := realTempDir(t)
			mkRoot(t, dir, tc.present...)
			chdir(t, dir)

			_, err := FindRoot()
			if err == nil {
				t.Fatal("expected an error")
			}
			msg := err.Error()
			for _, want := range tc.want {
				if !strings.Contains(msg, want) {
					t.Errorf("message should name %q; got %q", want, msg)
				}
			}
			for _, no := range tc.absent {
				if strings.Contains(msg, no) {
					t.Errorf("message should not name the present file %q; got %q", no, msg)
				}
			}
			if !strings.Contains(msg, RepairHint) {
				t.Errorf("message should contain RepairHint %q; got %q", RepairHint, msg)
			}
		})
	}
}

// TestFindRootStopsAtFirstDross pins the locked walk_stop decision: the walk
// halts at the nearest .dross even when it is incomplete and an ancestor's is
// complete. Continuing the climb would silently bind writes to the parent.
func TestFindRootStopsAtFirstDross(t *testing.T) {
	parent := realTempDir(t)
	mkRoot(t, parent, "project.toml", "state.json")
	child := filepath.Join(parent, "child")
	deep := filepath.Join(child, "deep")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	mkRoot(t, child, "project.toml")
	chdir(t, deep)

	got, err := FindRoot()
	if err == nil {
		t.Fatalf("FindRoot should stop at the incomplete child root, got %q", got)
	}
	if got != "" {
		t.Fatalf("FindRoot returned %q; must never fall through to the parent root", got)
	}
	if !strings.Contains(err.Error(), child) {
		t.Errorf("error should name the child root %q; got %v", child, err)
	}
}

// TestFindRootCompletenessIsExistenceNotParse pins the locked
// completeness_check decision: unparseable content is a real error for
// whoever loads the file, never an uninitialised root. A mutant that decides
// completeness by parsing fails both rows.
func TestFindRootCompletenessIsExistenceNotParse(t *testing.T) {
	t.Run("corrupt state.json still resolves", func(t *testing.T) {
		dir := realTempDir(t)
		root := mkRoot(t, dir, "project.toml")
		if err := os.WriteFile(filepath.Join(root, "state.json"), []byte("{{{"), 0o644); err != nil {
			t.Fatal(err)
		}
		chdir(t, dir)

		got, err := FindRoot()
		if err != nil {
			t.Fatalf("FindRoot should succeed on a corrupt-but-present state.json: %v", err)
		}
		if got != root {
			t.Errorf("root = %q, want %q", got, root)
		}
	})

	t.Run("zero-byte files still resolve", func(t *testing.T) {
		dir := realTempDir(t)
		root := mkRoot(t, dir, "project.toml", "state.json")
		chdir(t, dir)

		got, err := FindRoot()
		if err != nil {
			t.Fatalf("FindRoot should succeed on zero-byte required files: %v", err)
		}
		if got != root {
			t.Errorf("root = %q, want %q", got, root)
		}
	})
}

// TestFindRootDrossAsRegularFile: a plain file named .dross is not a root and
// must not stop the walk — nor panic.
func TestFindRootDrossAsRegularFile(t *testing.T) {
	parent := realTempDir(t)
	mkRoot(t, parent, "project.toml", "state.json")
	child := filepath.Join(parent, "child")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, RootDirName), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	chdir(t, child)

	got, err := FindRoot()
	if err != nil {
		t.Fatalf("a regular file named .dross should be skipped: %v", err)
	}
	if got != filepath.Join(parent, RootDirName) {
		t.Errorf("root = %q, want the parent's root", got)
	}
}

// TestFindRootUnreadableDross: an unreadable .dross is a broken environment,
// not an uninitialised repo — it must not masquerade as ErrNoRoot, or a hook
// target would silently exit 0 on it.
func TestFindRootUnreadableDross(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	dir := realTempDir(t)
	root := mkRoot(t, dir, "project.toml", "state.json")
	if err := os.Chmod(root, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })
	chdir(t, dir)

	_, err := FindRoot()
	if err == nil {
		t.Fatal("an unreadable .dross should error")
	}
	if errors.Is(err, ErrNoRoot) {
		t.Fatalf("an unreadable .dross must not be reported as not-a-dross-repo: %v", err)
	}
}

// TestLocateRootReportsMissing: LocateRoot is the non-erroring surface doctor
// needs — it returns the root plus the misses rather than refusing.
func TestLocateRootReportsMissing(t *testing.T) {
	dir := realTempDir(t)
	root := mkRoot(t, dir, "project.toml")
	chdir(t, dir)

	got, missing, err := LocateRoot()
	if err != nil {
		t.Fatalf("LocateRoot should not error on an incomplete root: %v", err)
	}
	if got != root {
		t.Errorf("root = %q, want %q", got, root)
	}
	if want := []string{filepath.Join(RootDirName, "state.json")}; !reflect.DeepEqual(missing, want) {
		t.Errorf("missing = %v, want %v", missing, want)
	}
}

// TestMissingRootFilesDoesNotWalk pins the seam onboard uses to stay
// cwd-scoped (locked walk_stop): asked about a child .dross whose parent has a
// complete one, it answers about the child and never climbs.
func TestMissingRootFilesDoesNotWalk(t *testing.T) {
	parent := realTempDir(t)
	mkRoot(t, parent, "project.toml", "state.json")
	child := filepath.Join(parent, "child")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	childRoot := mkRoot(t, child, "project.toml")
	chdir(t, child)

	missing, err := MissingRootFiles(childRoot)
	if err != nil {
		t.Fatalf("MissingRootFiles: %v", err)
	}
	want := []string{filepath.Join(RootDirName, "state.json")}
	if !reflect.DeepEqual(missing, want) {
		t.Fatalf("missing = %v, want %v — the parent's complete root must not be consulted", missing, want)
	}
}

// TestShipRecoverResolvesIncompleteRoot: recovery is the escape hatch for a
// half-wiped `.dross/`, so it must reach its real work on an incomplete root
// instead of being turned away. Reverting it to FindRoot makes the run fail
// with the not-a-dross-repo message, which is exactly what this rejects.
func TestShipRecoverResolvesIncompleteRoot(t *testing.T) {
	dir := realTempDir(t)
	mkRoot(t, dir, "project.toml")
	chdir(t, dir)

	err := runCmd(t, Ship(), "recover", "some-phase")
	if err == nil {
		t.Fatal("recover cannot succeed here — there is no git repo; want a post-root-resolution failure")
	}
	if errors.Is(err, ErrNoRoot) || strings.Contains(err.Error(), RepairHint) {
		t.Fatalf("recover stopped at root resolution instead of proceeding: %v", err)
	}
}

// TestRepairHintText pins the literal repair string. Every task that asserts
// on it fails loudly if it drifts, rather than the two copies diverging.
func TestRepairHintText(t *testing.T) {
	const want = "run `dross onboard` to adopt this directory into dross"
	if RepairHint != want {
		t.Errorf("RepairHint = %q, want %q", RepairHint, want)
	}
}

// TestRootCover_MustWriteFile targets root.go:52 CONDITIONALS_NEGATION.
// The success case asserts the file is actually written: the mutant
// (err == nil) would return early before WriteFile, leaving no file.
// The error case forces MkdirAll to fail (parent path is a regular
// file) and asserts a non-nil error is returned.
func TestRootCover_MustWriteFile(t *testing.T) {
	t.Run("success writes file", func(t *testing.T) {
		dir := realTempDir(t)
		path := filepath.Join(dir, "nested", "out.txt")
		want := []byte("payload")
		if err := MustWriteFile(path, want); err != nil {
			t.Fatalf("MustWriteFile returned error: %v", err)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("file was not written: %v", err)
		}
		if string(got) != string(want) {
			t.Fatalf("content = %q, want %q", got, want)
		}
	})

	t.Run("mkdirall failure returns error", func(t *testing.T) {
		dir := realTempDir(t)
		// Create a regular file, then try to write under it so that
		// MkdirAll(filepath.Dir(path)) must fail.
		blocker := filepath.Join(dir, "blocker")
		if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(blocker, "child", "out.txt")
		if err := MustWriteFile(path, []byte("data")); err == nil {
			t.Fatalf("expected error when parent is a file, got nil")
		}
	})
}

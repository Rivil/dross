package survivor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func storePath(t *testing.T) string {
	t.Helper()
	return Path(t.TempDir())
}

func mustAccept(t *testing.T, path string, a Acceptance) {
	t.Helper()
	if err := Accept(path, a); err != nil {
		t.Fatalf("Accept(%s): %v", a.Key, err)
	}
}

// TestAcceptRejectsReasonlessAndWritesNothing is c-2's write half: an
// acceptance submitted without a reason is rejected, and the rejection does not
// touch the file. A store that gained a reasonless entry — or that got rewritten
// on the way to failing — would be suppressing survivors with nothing recorded
// about why.
func TestAcceptRejectsReasonlessAndWritesNothing(t *testing.T) {
	path := storePath(t)

	if err := Accept(path, Acceptance{Key: "k1", File: "a.go", Op: "OP", Text: "return nil"}); err == nil {
		t.Fatal("Accept with no reason and no category succeeded, want error")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected accept left a store behind (stat err = %v)", err)
	}

	// And against an existing store: byte-identical after the rejection.
	mustAccept(t, path, Acceptance{Key: "k0", File: "a.go", Op: "OP", Text: "x := 1", Reason: "pty guard"})
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Accept(path, Acceptance{Key: "k1", File: "a.go", Op: "OP", Text: "return nil"}); err == nil {
		t.Fatal("Accept with no reason succeeded against an existing store, want error")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("store changed on a rejected accept:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestLoadRejectsHandEditedReasonlessEntry is c-2's read half. The store is a
// tracked, hand-editable file, so the write gate is not the only way in: an
// entry whose reason was blanked by hand must fail load rather than load and
// silently suppress.
func TestLoadRejectsHandEditedReasonlessEntry(t *testing.T) {
	path := storePath(t)
	write(t, path, `
[[accepted]]
  key = "k1"
  file = "a.go"
  op = "OP"
  text = "return nil"
  reason = ""
`)
	if _, err := Load(path); err == nil {
		t.Fatal("Load accepted a reasonless entry, want error")
	}
}

// TestCategoryResolution pins the shared-prose path: a category must exist and
// carry prose to be referenced, and a valid reference must resolve to it. A
// category that resolves to nothing is a reasonless acceptance wearing a name.
func TestCategoryResolution(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{
			name: "absent category",
			body: `
[[accepted]]
  key = "k1"
  file = "a.go"
  op = "OP"
  text = "return nil"
  category = "switch-ceiling"
`,
			wantErr: true,
		},
		{
			name: "prose-less category",
			body: `
[[category]]
  name = "switch-ceiling"
  reason = ""

[[accepted]]
  key = "k1"
  file = "a.go"
  op = "OP"
  text = "return nil"
  category = "switch-ceiling"
`,
			wantErr: true,
		},
		{
			name: "valid category",
			body: `
[[category]]
  name = "switch-ceiling"
  reason = "gremlins switch-case attribution ceiling"

[[accepted]]
  key = "k1"
  file = "a.go"
  op = "OP"
  text = "return nil"
  category = "switch-ceiling"
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := storePath(t)
			write(t, path, tc.body)
			s, err := Load(path)
			if tc.wantErr {
				if err == nil {
					t.Fatal("Load succeeded, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			a, ok := s.Get("k1")
			if !ok {
				t.Fatal("entry k1 missing after load")
			}
			reason, err := s.ReasonFor(a)
			if err != nil {
				t.Fatalf("ReasonFor: %v", err)
			}
			if reason != "gremlins switch-case attribution ceiling" {
				t.Errorf("ReasonFor = %q, want the category's prose", reason)
			}
		})
	}
}

// TestKeyUniqueness pins the key as the primary key at both ends: a duplicated
// key fails load, and re-accepting a present key replaces rather than
// duplicates. A store with two rows for one survivor makes staleness and
// suppression ambiguous.
func TestKeyUniqueness(t *testing.T) {
	path := storePath(t)
	write(t, path, `
[[accepted]]
  key = "k1"
  file = "a.go"
  op = "OP"
  text = "return nil"
  reason = "first"

[[accepted]]
  key = "k1"
  file = "a.go"
  op = "OP"
  text = "return nil"
  reason = "second"
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load accepted two entries with the same key, want error")
	}
	if !strings.Contains(err.Error(), "duplicate survivor key") {
		t.Errorf("error = %v, want it to name 'duplicate survivor key'", err)
	}

	fresh := storePath(t)
	mustAccept(t, fresh, Acceptance{Key: "k1", File: "a.go", Op: "OP", Text: "return nil", Reason: "first"})
	mustAccept(t, fresh, Acceptance{Key: "k1", File: "a.go", Op: "OP", Text: "return nil", Reason: "second"})
	s, err := Load(fresh)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(s.Accepted) != 1 {
		t.Fatalf("re-accepting a present key produced %d entries, want 1 (upsert)", len(s.Accepted))
	}
	if s.Accepted[0].Reason != "second" {
		t.Errorf("Reason = %q, want the replacing entry's prose", s.Accepted[0].Reason)
	}
}

// TestSharedCategoryProseEncodedOnce pins acceptance_granularity's payoff: many
// entries, one copy of the prose. If prose starts being copied per entry, a
// bulk drain becomes 76 copy-pasted reasons that drift apart on the first edit.
func TestSharedCategoryProseEncodedOnce(t *testing.T) {
	path := storePath(t)
	const prose = "gremlins switch-case attribution ceiling"
	mustAccept(t, path, Acceptance{Key: "k1", File: "a.go", Op: "OP", Text: "case 1:", Category: "switch-ceiling", Reason: prose})
	mustAccept(t, path, Acceptance{Key: "k2", File: "b.go", Op: "OP", Text: "case 2:", Category: "switch-ceiling", Reason: prose})

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(raw), prose); n != 1 {
		t.Errorf("prose encoded %d times, want exactly 1:\n%s", n, raw)
	}

	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(s.Accepted) != 2 {
		t.Fatalf("round-tripped %d entries, want 2", len(s.Accepted))
	}
	for _, a := range s.Accepted {
		got, err := s.ReasonFor(a)
		if err != nil {
			t.Fatalf("ReasonFor(%s): %v", a.Key, err)
		}
		if got != prose {
			t.Errorf("ReasonFor(%s) = %q, want %q", a.Key, got, prose)
		}
	}
}

// TestLoadMissingStoreIsEmptyNotError pins the first-run path: no
// survivors.toml means no acceptances, not a broken repo. Erroring here would
// make every dross verify in a fresh clone fail on a file nobody created yet.
func TestLoadMissingStoreIsEmptyNotError(t *testing.T) {
	s, err := Load(storePath(t))
	if err != nil {
		t.Fatalf("Load of a missing store returned %v, want nil", err)
	}
	if s == nil {
		t.Fatal("Load returned a nil store")
	}
	if len(s.Accepted) != 0 || len(s.Categories) != 0 {
		t.Errorf("missing store loaded as %+v, want empty", s)
	}
}

// TestLoadCorruptStoreIsAnError is the other half: a garbled file must fail
// loudly. A store that failed open would re-emit the drained backlog and, worse,
// be re-saved without the entries it could not read.
func TestLoadCorruptStoreIsAnError(t *testing.T) {
	path := storePath(t)
	write(t, path, "this is not = = toml\n[[accepted\n")
	if _, err := Load(path); err == nil {
		t.Fatal("Load of a corrupt store succeeded, want error")
	}
}

// TestAcceptPreservesUntouchedEntries pins the read-modify-write: a writer that
// re-saved only what it knew about would silently drop every acceptance made
// before it.
func TestAcceptPreservesUntouchedEntries(t *testing.T) {
	path := storePath(t)
	mustAccept(t, path, Acceptance{Key: "k1", File: "a.go", Op: "OP", Text: "x := 1", Reason: "one"})
	mustAccept(t, path, Acceptance{Key: "k2", File: "b.go", Op: "OP", Text: "y := 2", Reason: "two"})
	mustAccept(t, path, Acceptance{Key: "k3", File: "c.go", Op: "OP", Text: "z := 3", Reason: "three"})

	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, key := range []string{"k1", "k2", "k3"} {
		if _, ok := s.Get(key); !ok {
			t.Errorf("entry %s lost across successive accepts", key)
		}
	}
}

// TestSaveIsAtomicOnFailure pins that a rejected save leaves the prior bytes
// intact rather than a truncated file. os.Create-then-encode would blow the
// store away before discovering the write could not be completed.
func TestSaveIsAtomicOnFailure(t *testing.T) {
	path := storePath(t)
	mustAccept(t, path, Acceptance{Key: "k1", File: "a.go", Op: "OP", Text: "x := 1", Reason: "one"})
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// A store that fails validation must not reach the file at all.
	bad := &Store{Accepted: []Acceptance{{Key: "k2", File: "b.go", Op: "OP", Text: "y := 2"}}}
	if err := Save(path, bad); err == nil {
		t.Fatal("Save of a reasonless store succeeded, want error")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("store unreadable after a failed save: %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("failed save mutated the store:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestLocatePathResolvesRepoRootFromNestedSubdir is c-3's storage half: the
// store is repo-level, so a nested working directory must reach the same file
// the root would. A cwd-relative store forks into one registry per directory
// and acceptances stop being findable from where verify runs.
func TestLocatePathResolvesRepoRootFromNestedSubdir(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, RootDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "internal", "cmd", "testdata")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := LocatePath(nested)
	if err != nil {
		t.Fatalf("LocatePath: %v", err)
	}
	want := filepath.Join(root, RootDirName, StoreFile)
	if resolve(t, got) != resolve(t, want) {
		t.Errorf("LocatePath(nested) = %s, want %s", got, want)
	}
}

// TestLocatePathWithoutRoot pins the no-root case as a clean error rather than
// a path pointing at nothing, so a caller outside a dross repo cannot quietly
// create a stray store.
func TestLocatePathWithoutRoot(t *testing.T) {
	if _, err := LocatePath(t.TempDir()); !errors.Is(err, ErrNoStoreRoot) {
		t.Fatalf("err = %v, want ErrNoStoreRoot", err)
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// resolve canonicalizes a path so a symlinked temp dir (macOS /var -> /private/var)
// doesn't fail an otherwise-correct comparison.
func resolve(t *testing.T, p string) string {
	t.Helper()
	got, err := filepath.EvalSymlinks(filepath.Dir(p))
	if err != nil {
		return p
	}
	return filepath.Join(got, filepath.Base(p))
}

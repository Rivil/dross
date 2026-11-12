package pkgjson

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTopLevelVersionSpanNonObject(t *testing.T) {
	for _, doc := range []string{`["version", "1.0.0"]`, `"1.0.0"`, `{"version": 1}`} {
		if _, _, ok, err := topLevelVersionSpan([]byte(doc)); ok || err != nil {
			t.Errorf("topLevelVersionSpan(%s) = ok %v err %v, want no version and no error", doc, ok, err)
		}
	}
}

// writePkg writes body to a package.json in a fresh dir and returns its path.
func writePkg(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "package.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func readPkg(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestSyncVersionRewritesOnlyTheVersionBytes: every value before the version
// is skipped whole — nested arrays and objects included — and the rewrite
// touches the version's bytes alone, so a later string holding the same old
// value and the file's odd spacing both survive.
func TestSyncVersionRewritesOnlyTheVersionBytes(t *testing.T) {
	const before = `{
  "name": "x",
  "files": ["a", ["b", {"c": [1]}]],
  "scripts": {"t": {"u": "v"}, "w": []},
  "version" :  "1.0.0",
  "after": "1.0.0"
}
`
	const want = `{
  "name": "x",
  "files": ["a", ["b", {"c": [1]}]],
  "scripts": {"t": {"u": "v"}, "w": []},
  "version" :  "1.9.1",
  "after": "1.0.0"
}
`
	path := writePkg(t, before)
	if err := SyncVersion(path, "1.9.1"); err != nil {
		t.Fatalf("SyncVersion: %v", err)
	}
	if got := readPkg(t, path); got != want {
		t.Errorf("rewritten package.json:\n%s\nwant:\n%s", got, want)
	}
}

// TestSyncVersionLeavesFileAlone: the sync keeps a number in step; it never
// adds a version, replaces a non-string one, or rewrites one already at want.
func TestSyncVersionLeavesFileAlone(t *testing.T) {
	for name, body := range map[string]string{
		"no version":      `{"name": "x"}`,
		"not a string":    `{"version": 1}`,
		"already at want": `{"version": "1.9.1"}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := writePkg(t, body)
			if err := SyncVersion(path, "1.9.1"); err != nil {
				t.Fatalf("SyncVersion: %v", err)
			}
			if got := readPkg(t, path); got != body {
				t.Errorf("file changed to %s, want %s", got, body)
			}
		})
	}
}

// TestSyncVersionMissingPathIsNoOp: a package.json gone from the work tree is
// not an error and is not created.
func TestSyncVersionMissingPathIsNoOp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "package.json")
	if err := SyncVersion(path, "1.9.1"); err != nil {
		t.Fatalf("SyncVersion on a missing path: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("a missing package.json was created (stat err %v)", err)
	}
}

// TestSyncVersionStatAndReadErrors: only a missing path is silent — a path
// that cannot be stat'd, or one that stats but cannot be read, is an error.
func TestSyncVersionStatAndReadErrors(t *testing.T) {
	file := writePkg(t, `{"version": "1.0.0"}`)
	if err := SyncVersion(filepath.Join(file, "package.json"), "1.9.1"); err == nil {
		t.Error("a path beneath a regular file stat'd without error")
	}
	if err := SyncVersion(t.TempDir(), "1.9.1"); err == nil {
		t.Error("a directory read as a package.json without error")
	}
}

// TestSyncVersionRefusesEscapedVersion: a version whose raw bytes are not its
// value is refused, and the file is left as it was.
func TestSyncVersionRefusesEscapedVersion(t *testing.T) {
	// The backslash comes from its code point so no editor or tool can fold
	// the escape into the '.' it decodes to: body's raw bytes hold a
	// backslash-u002e escape where the value holds a dot.
	body := `{"version": "1.0` + string(rune(0x5c)) + `u002e0"}`
	path := writePkg(t, body)
	if err := SyncVersion(path, "1.9.1"); err == nil {
		t.Error("an escaped version was not refused")
	}
	if got := readPkg(t, path); got != body {
		t.Errorf("a refused file changed to %s", got)
	}
}

// TestSyncVersionMalformedJSON: a syntax error — at the top, or inside a value
// being skipped on the way to the version — is an error, not a silent no-op.
func TestSyncVersionMalformedJSON(t *testing.T) {
	for name, body := range map[string]string{
		"not json":          `not json`,
		"bad skipped value": `{"name": [1, }, "version": "1.0.0"}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := writePkg(t, body)
			if err := SyncVersion(path, "1.9.1"); err == nil {
				t.Error("malformed JSON synced without error")
			}
			if got := readPkg(t, path); got != body {
				t.Errorf("a malformed file changed to %s", got)
			}
		})
	}
}

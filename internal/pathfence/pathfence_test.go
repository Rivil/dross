package pathfence_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/compilefence"
	"github.com/Rivil/dross/internal/pathfence"
)

// --- construction: the guarantee the type carries -------------------------

// TestContainedCannotBeConstructedOutsidePackage is the load-bearing assertion
// of the whole design: if this stops holding, every "a consumer that skips the
// check fails to build" claim in the phase is false.
//
// The fixture names a REAL unexported field. An invented one fails with
// "unknown field", which it would also do if the field were EXPORTED — so
// asserting on that message would prove nothing about unexported-ness.
func TestContainedCannotBeConstructedOutsidePackage(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go build")
	}
	// Positive control FIRST: if a legitimate import cannot build, the refusal
	// below would be a false positive rather than evidence.
	compilefence.AssertCompiles(t, `package tmpfence

import "github.com/Rivil/dross/internal/pathfence"

var _ = pathfence.InTree
`)

	compilefence.AssertDoesNotCompile(t, `package tmpfence

import "github.com/Rivil/dross/internal/pathfence"

var _ = pathfence.Contained{rel: "x"}
`, "cannot refer to unexported field")
}

// TestSeamRefusesZeroContained closes the hole the compiler does not: Go
// forbids SETTING an unexported field, not writing `Contained{}`. Without this,
// `[]Contained{{}, {}}` would reach the filesystem relative to the working
// directory.
func TestSeamRefusesZeroContained(t *testing.T) {
	var zero pathfence.Contained

	if _, err := pathfence.ReadFile(zero); err == nil {
		t.Error("ReadFile accepted a zero Contained")
	}
	if err := pathfence.WriteFile(zero, []byte("x"), 0o644); err == nil {
		t.Error("WriteFile accepted a zero Contained")
	}
	if _, err := pathfence.Stat(zero); err == nil {
		t.Error("Stat accepted a zero Contained")
	}
}

// --- Contain: refusal and diagnosability ----------------------------------

// TestContainEscapeNamesPathArtifactAndRoot pins c-5: a hand-edited changes.json
// must be fixable from the message alone. Asserted as three separate Contains
// so a reworded message stays passing while a message that DROPS one of the
// three facts fails.
func TestContainEscapeNamesPathArtifactAndRoot(t *testing.T) {
	root := t.TempDir()

	_, err := pathfence.Contain(root, "changes.json", "../x.go")
	if err == nil {
		t.Fatal("Contain accepted \"../x.go\"")
	}
	for _, want := range []string{"../x.go", "changes.json", root} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

// TestContainAbsoluteIsItsOwnSentinel stops a deleted IsAbs branch passing by
// falling through to the escape branch. The two are different diagnoses: an
// absolute path never escapes, it is just one machine's layout.
func TestContainAbsoluteIsItsOwnSentinel(t *testing.T) {
	root := t.TempDir()

	_, err := pathfence.Contain(root, "changes.json", "/etc/passwd")
	if err == nil {
		t.Fatal("Contain accepted an absolute path")
	}
	if !errors.Is(err, pathfence.ErrAbsolute) {
		t.Errorf("want ErrAbsolute, got %v", err)
	}
	if errors.Is(err, pathfence.ErrEscapes) {
		t.Error("an absolute path must NOT report as an escape — the diagnoses are different")
	}
}

func TestContainInteriorDotDotLandingInsideIsNotAnEscape(t *testing.T) {
	root := t.TempDir()

	c, err := pathfence.Contain(root, "changes.json", "a/../b.md")
	if err != nil {
		t.Fatalf("Contain refused an interior \"..\" that lands inside: %v", err)
	}
	if want := filepath.Join(root, "b.md"); c.String() != want {
		t.Errorf("String() = %q, want %q", c.String(), want)
	}

	for _, bad := range []string{"..", "../"} {
		if _, err := pathfence.Contain(root, "changes.json", bad); err == nil {
			t.Errorf("Contain accepted %q", bad)
		}
	}
}

// TestNearMissNamesAreAccepted fails a HasPrefix(p, "..") check that forgot the
// separator test. All three are ordinary filenames.
func TestNearMissNamesAreAccepted(t *testing.T) {
	root := t.TempDir()

	for _, name := range []string{"..foo", "a/..b", "a/b.."} {
		if _, err := pathfence.Contain(root, "changes.json", name); err != nil {
			t.Errorf("Contain refused the ordinary name %q: %v", name, err)
		}
		if got, ok := pathfence.InTree(name); !ok {
			t.Errorf("InTree(%q) = (%q, false), want in-tree", name, got)
		}
	}
}

// TestContainNeverTouchesTheFilesystem — the write path needs to contain paths
// that do not exist yet, so a stat-based implementation would break it.
func TestContainNeverTouchesTheFilesystem(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no", "such", "dir")

	if _, err := pathfence.Contain(missing, "run directory", "report.md"); err != nil {
		t.Fatalf("Contain consulted the filesystem: %v", err)
	}
}

// TestSymlinkUnderRootIsAccepted_symlink_resolution pins the documented limit of
// the symlink_resolution lock. Named for the lock so the next reader finds the
// decision rather than assuming a bug.
func TestSymlinkUnderRootIsAccepted_symlink_resolution(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "victim.md")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.md")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, err := pathfence.Contain(root, "changes.json", "link.md"); err != nil {
		t.Fatalf("lexical containment must ACCEPT a symlink pointing outside "+
			"(symlink_resolution lock): %v", err)
	}
}

// --- the accessor split ---------------------------------------------------

// TestStringIsAbsoluteAndRelIsRelative pins both directions, so swapping the two
// accessors fails here rather than silently writing files to the wrong place.
func TestStringIsAbsoluteAndRelIsRelative(t *testing.T) {
	root := t.TempDir()

	c, err := pathfence.Contain(root, "changes.json", "docs/x.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.String(), root) {
		t.Errorf("String() = %q, want it rooted at %q", c.String(), root)
	}
	if c.Rel() != "docs/x.md" {
		t.Errorf("Rel() = %q, want %q", c.Rel(), "docs/x.md")
	}
	if strings.Contains(c.Rel(), root) {
		t.Errorf("Rel() = %q leaks the root", c.Rel())
	}
}

// TestStringOpensFromAnyWorkingDirectory is what lets a Contained cross into a
// func(path string) callee such as phase.saveTOML beneath WriteScaffoldSpec.
// Rel() under the same chdir must NOT resolve, so the two cannot be confused.
func TestStringOpensFromAnyWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "x.md"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := pathfence.Contain(root, "changes.json", "x.md")
	if err != nil {
		t.Fatal(err)
	}

	t.Chdir(t.TempDir()) // deliberately NOT root

	got, err := os.ReadFile(c.String())
	if err != nil {
		t.Fatalf("String() did not open from another working directory: %v", err)
	}
	if string(got) != "payload" {
		t.Errorf("read %q, want %q", got, "payload")
	}
	if _, err := os.ReadFile(c.Rel()); err == nil {
		t.Error("Rel() resolved from another working directory — it must not, " +
			"or a caller reaching for the wrong accessor silently touches the wrong file")
	}
}

// --- InTree ---------------------------------------------------------------

// TestInTreeReturnsCleanedPathOnFalseBranch — testlane buckets the returned
// string, so returning "" would silently change what lands in its escaped
// bucket.
func TestInTreeReturnsCleanedPathOnFalseBranch(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"../x", "../x"},
		{"/abs/x", "/abs/x"},
	} {
		got, ok := pathfence.InTree(tc.in)
		if ok {
			t.Errorf("InTree(%q) reported in-tree", tc.in)
		}
		if got != tc.want {
			t.Errorf("InTree(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestInTreeTakesNoPositionOnDotOrEmpty keeps each caller's own policy visible
// at its call site rather than buried here. verify treats "." as out-of-tree;
// testlane treats "" as in-tree-and-unmatched. Both stay their own business.
func TestInTreeTakesNoPositionOnDotOrEmpty(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		wantOK   bool
	}{
		{"a/../b", "b", true},
		{".", ".", true},
		{"", "", true},
	} {
		got, ok := pathfence.InTree(tc.in)
		if got != tc.want || ok != tc.wantOK {
			t.Errorf("InTree(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.wantOK)
		}
	}
}

// TestInTreeFoldsBackslashesUnconditionally is the line that fails a
// filepath.ToSlash implementation ON DARWIN, where ToSlash is the identity:
// path.Clean would then see `a\b` as one segment, ".." would pop it, and the
// answer would be "c". Folding first gives "a/c" on every platform.
func TestInTreeFoldsBackslashesUnconditionally(t *testing.T) {
	got, ok := pathfence.InTree(`a\b/../c`)
	if !ok {
		t.Fatalf("InTree(%q) reported out-of-tree", `a\b/../c`)
	}
	if got != "a/c" {
		t.Errorf("InTree(%q) = %q, want %q — a platform-gated fold yields %q here on darwin",
			`a\b/../c`, got, "a/c", "c")
	}
}

// --- Segment --------------------------------------------------------------

func TestSegment(t *testing.T) {
	for _, tc := range []struct {
		id      string
		wantErr bool
		why     string
	}{
		{"run-123", false, "an ordinary run id"},
		{"a/b", true, "contains a separator"},
		{`a\b`, true, "contains a windows separator"},
		{"..", true, "is a traversal"},
		{".", true, "is a traversal"},
		{"", true, "is empty — remote.RunDir's own \"empty run id\" refusal must survive delegation"},
	} {
		err := pathfence.Segment("run id", tc.id)
		if tc.wantErr && err == nil {
			t.Errorf("Segment(%q) = nil, want an error: it %s", tc.id, tc.why)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("Segment(%q) = %v, want nil: it %s", tc.id, err, tc.why)
		}
	}
}

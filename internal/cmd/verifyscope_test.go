package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/pathfence"
	"github.com/Rivil/dross/internal/verify"
)

// scopeRepo builds a temp repo with a `base` branch holding one commit, then
// leaves HEAD on a phase branch forked from it. Returns the repo dir.
func scopeRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitInit(t, dir, "git@github.com:example/x.git")
	writeScopeFile(t, dir, "a.go", strings.Repeat("// line\n", 20))
	writeScopeFile(t, dir, "untouched.go", "package x\n")
	mustGit(t, dir, "add", "a.go", "untouched.go")
	mustGit(t, dir, "commit", "-qm", "base")
	mustGit(t, dir, "branch", "base")
	mustGit(t, dir, "checkout", "-qb", "phase/x")
	return dir
}

func writeScopeFile(t *testing.T, dir, rel, body string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// mustPhaseScope is phaseScope for the cases that must succeed. phaseScope now
// returns an error — the recorded-path containment gate — and every scope built
// here records only in-repo paths, so a failure means the gate misfired and the
// test should stop rather than nil-deref.
func mustPhaseScope(t *testing.T, repoDir, base string, recorded []string) *verify.Scope {
	t.Helper()
	s, err := phaseScope(repoDir, base, recorded)
	if err != nil {
		t.Fatalf("phaseScope(%q, %q, %v): %v", repoDir, base, recorded, err)
	}
	return s
}

// TestPhaseScopeFallsBackWithoutBase: a phase whose changes.json never got a
// base recorded must still produce a usable scope. Failing the run instead
// would turn a bookkeeping gap into a blocked phase — but the gap has to be
// visible, or a narrowed scope reads as a clean pass.
func TestPhaseScopeFallsBackWithoutBase(t *testing.T) {
	dir := scopeRepo(t)
	s := mustPhaseScope(t, dir, "", []string{"a.go"})

	if want := []string{"a.go"}; len(s.Files) != 1 || s.Files[0] != want[0] {
		t.Errorf("files = %v want %v", s.Files, want)
	}
	if s.Source != verify.SourceChangesOnly {
		t.Errorf("source = %q want %q", s.Source, verify.SourceChangesOnly)
	}
	if len(s.Degraded) == 0 || !strings.Contains(strings.Join(s.Degraded, "\n"), "base") {
		t.Errorf("degraded must name the missing source: %v", s.Degraded)
	}
	if s.Base != "" {
		t.Errorf("no base to resolve, got %q", s.Base)
	}
}

// TestPhaseScopeRecordsResolvedSha: the scope records the merge-base SHA, not
// the ref it was derived from. A branch name moves; asserting on the resolved
// sha is what makes a stale-base run diagnosable rather than merely plausible.
func TestPhaseScopeRecordsResolvedSha(t *testing.T) {
	dir := scopeRepo(t)
	writeScopeFile(t, dir, "a.go", strings.Repeat("// line\n", 21))
	mustGit(t, dir, "commit", "-qam", "phase work")

	want := mustGit(t, dir, "rev-parse", "base")
	s := mustPhaseScope(t, dir, "base", nil)

	if s.Base != want {
		t.Errorf("base = %q want the resolved sha %q", s.Base, want)
	}
	if s.Base == "base" {
		t.Error("base recorded as the ref name, not the sha")
	}
}

// TestPhaseScopeSourceProvenance proves the union is symmetric: either side
// alone is enough to produce a scope, and the recorded source says which one
// carried it. A git-gated build would return nothing on the changes-only path.
func TestPhaseScopeSourceProvenance(t *testing.T) {
	dir := scopeRepo(t)
	writeScopeFile(t, dir, "a.go", strings.Repeat("// line\n", 21))
	mustGit(t, dir, "commit", "-qam", "phase work")

	gitOnly := mustPhaseScope(t, dir, "base", nil)
	if gitOnly.Source != verify.SourceGitOnly {
		t.Errorf("git side alone: source = %q want %q", gitOnly.Source, verify.SourceGitOnly)
	}
	if !gitOnly.Contains("a.go") {
		t.Errorf("git-derived file missing from scope: %v", gitOnly.Files)
	}

	// An unresolvable base is the git leg failing; the recorded side carries it.
	changesOnly := mustPhaseScope(t, dir, "no-such-ref", []string{"recorded.go"})
	if changesOnly.Source != verify.SourceChangesOnly {
		t.Errorf("git failed: source = %q want %q", changesOnly.Source, verify.SourceChangesOnly)
	}
	if !changesOnly.Contains("recorded.go") {
		t.Errorf("recorded file missing from scope: %v", changesOnly.Files)
	}
	if len(changesOnly.Degraded) == 0 {
		t.Error("a failed git leg must be recorded as degraded")
	}
}

// TestPhaseScopeRenameKeepsBothSides: --no-renames emits the delete and the
// add separately, so both paths land in scope. The old path matters because a
// mutation report generated before the rename still names it.
func TestPhaseScopeRenameKeepsBothSides(t *testing.T) {
	dir := scopeRepo(t)
	mustGit(t, dir, "mv", "a.go", "b.go")
	mustGit(t, dir, "commit", "-qm", "rename")

	s := mustPhaseScope(t, dir, "base", nil)
	for _, f := range []string{"a.go", "b.go"} {
		if !s.Contains(f) {
			t.Errorf("%s not in scope after rename: %v", f, s.Files)
		}
	}
}

// TestPhaseScopeQuotedPath: git quotes paths with non-ASCII bytes in its
// default output. Left quoted, the scope key is an escaped literal that
// matches no mutant, and every mutant in that file filters out as
// out-of-scope — a vacuous 0/0 wearing the shape of a clean run.
func TestPhaseScopeQuotedPath(t *testing.T) {
	dir := scopeRepo(t)
	const name = "internal/naïve file.go"
	writeScopeFile(t, dir, name, "package x\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-qm", "add awkward name")

	s := mustPhaseScope(t, dir, "base", nil)
	if !s.Contains(name) {
		t.Errorf("mutant path %q not matched by scope %v", name, s.Files)
	}
}

// TestPhaseScopeCollectsHunks: the changed line ranges must survive to the
// Scope, or every in-scope survivor degrades to the weaker inherited tag.
func TestPhaseScopeCollectsHunks(t *testing.T) {
	dir := scopeRepo(t)
	lines := strings.Split(strings.Repeat("// line\n", 20), "\n")
	for i := 9; i <= 11; i++ { // 0-indexed 9..11 == lines 10..12
		lines[i] = "// changed"
	}
	writeScopeFile(t, dir, "a.go", strings.Join(lines, "\n"))
	mustGit(t, dir, "commit", "-qam", "edit 10-12")

	s := mustPhaseScope(t, dir, "base", nil)
	got := s.Hunks["a.go"]
	if len(got) != 1 || got[0] != (verify.Range{Start: 10, End: 12}) {
		t.Fatalf("hunks = %v want [{10 12}]", got)
	}
	if !s.InHunk("a.go", 11) {
		t.Error("line 11 should be in-hunk")
	}
	if s.InHunk("a.go", 13) {
		t.Error("line 13 is outside the hunk")
	}
}

// TestPhaseScopeFencesBaseRef: the base comes out of changes.json, which is a
// file on disk, so it is caller-derived data reaching git's argv. Unfenced,
// `--output=<path>` is not a ref lookup that fails — it is a flag that
// succeeds and writes the file.
//
// Both halves are asserted: the payload had no effect, and every argv that
// carried it put it behind --end-of-options. The second half is what keeps the
// test honest if a future refactor drops the builder — the payload might stop
// working for an unrelated reason, but the ordering cannot pass by accident.
func TestPhaseScopeFencesBaseRef(t *testing.T) {
	dir := scopeRepo(t)
	pwned := filepath.Join(t.TempDir(), "pwned")
	payload := "--output=" + pwned

	var argvs [][]string
	gitArgvRecorder = func(args []string) {
		argvs = append(argvs, append([]string(nil), args...))
	}
	t.Cleanup(func() { gitArgvRecorder = nil })

	s := mustPhaseScope(t, dir, payload, []string{"a.go"})

	if _, err := os.Stat(pwned); err == nil {
		t.Fatalf("injected --output was honoured: %s exists", pwned)
	}
	if s.Source != verify.SourceChangesOnly {
		t.Errorf("a refused base must degrade to changes-only, got %q", s.Source)
	}

	var sawPayload bool
	for _, argv := range argvs {
		sep, pos := -1, -1
		for i, a := range argv {
			if a == endOfOptions && sep == -1 {
				sep = i
			}
			if a == payload && pos == -1 {
				pos = i
			}
		}
		if pos == -1 {
			continue
		}
		sawPayload = true
		if sep == -1 || pos < sep {
			t.Errorf("base ref not fenced behind %s: %q", endOfOptions, argv)
		}
	}
	if !sawPayload {
		t.Fatal("no recorded argv carried the base ref — the tap missed the git calls")
	}
}

// --- t-5: the recorded-path gate and the scope conversion -------------------

// mutationCandidates' signature is pinned at COMPILE time. The usual
// compilefence fixture cannot be used here — mutationCandidates and
// containScope are unexported, so no other module can name them at all — and a
// typed function value is the stronger assertion anyway: it is checked on every
// build of this package, not only when the fence runs.
//
// The PARAMETER is what the phase changes; the RETURNS are pinned deliberately
// unchanged. dispatch feeds the mutation adapters and gone feeds the skip
// report, both as plain repo-relative paths, so a task that also retyped the
// returns would break callers this phase never intended to touch.
var (
	_ func([]pathfence.Contained) ([]string, []string)           = mutationCandidates
	_ func(string, *verify.Scope) ([]pathfence.Contained, error) = containScope
	_ func(string, string, []string) (*verify.Scope, error)      = phaseScope
)

// TestPhaseScopeRefusesEscapingRecordedPath is the gate. It must abort, not
// degrade: the soft lane is where NormalizePath sends "../x.go" TODAY, with the
// run still passing, and that is the live bug (escape_failure_mode lock).
func TestPhaseScopeRefusesEscapingRecordedPath(t *testing.T) {
	dir := scopeRepo(t)

	s, err := phaseScope(dir, "base", []string{"a.go", "../x.go"})
	if err == nil {
		t.Fatal("phaseScope accepted a recorded path escaping the repo")
	}
	if s != nil {
		t.Errorf("a refusal still returned a scope (%+v) — there is no partial scope to inspect", s)
	}
	for _, want := range []string{"../x.go", "changes.json", dir} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not name %q:\n%s", want, err.Error())
		}
	}
	// And specifically NOT the soft lane: a fix that leaves the gate out and
	// relies on NormalizePath's existing rejection lands green everywhere else
	// and must fail here.
	if strings.Contains(err.Error(), "ignored out-of-repo path") {
		t.Error("the escaping recorded path was reported as a degraded entry rather than refused")
	}
}

// TestPhaseScopeDoesNotAbortOnGitPaths keeps the hard rule from being
// over-applied: only the recorded lane is gated, so an ordinary scope built
// with no recorded files still succeeds.
func TestPhaseScopeDoesNotAbortOnGitPaths(t *testing.T) {
	dir := scopeRepo(t)
	writeScopeFile(t, dir, "a.go", strings.Repeat("// changed\n", 20))
	mustGit(t, dir, "commit", "-qam", "phase edit")

	s, err := phaseScope(dir, "base", nil)
	if err != nil {
		t.Fatalf("phaseScope errored on a git-only scope: %v", err)
	}
	if len(s.Files) == 0 {
		t.Fatal("git-only scope came back empty")
	}
}

// TestContainScopeConvertsTheUnion is the line that catches the silent
// narrowing. containScope must convert scope.Files — the recorded-UNION-git set
// — and not the recorded set the gate validated: a file git saw change but no
// task recorded must still reach the mutation adapters, or its survivors could
// gate nothing. An implementation fed ValidateRecorded's input instead passes
// every other assertion in this file and fails this one.
func TestContainScopeConvertsTheUnion(t *testing.T) {
	dir := scopeRepo(t)
	// git sees a.go change; changes.json records only recorded.go.
	writeScopeFile(t, dir, "a.go", strings.Repeat("// changed\n", 20))
	writeScopeFile(t, dir, "recorded.go", "package x\n")
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "-qm", "phase edit")

	s := mustPhaseScope(t, dir, "base", []string{"recorded.go"})
	got, err := containScope(dir, s)
	if err != nil {
		t.Fatalf("containScope: %v", err)
	}

	dispatch, gone := mutationCandidates(got)
	if len(gone) != 0 {
		t.Errorf("nothing should be gone — both files exist: %v", gone)
	}
	if !slices.Contains(dispatch, "a.go") {
		t.Errorf("the git-only file never reached the mutation candidates: %v — "+
			"the conversion narrowed to the recorded set", dispatch)
	}
	if !slices.Contains(dispatch, "recorded.go") {
		t.Errorf("the recorded file is missing from the candidates: %v", dispatch)
	}
}

// TestContainScopeRefusesUncontainedEntry: the conversion is total in practice,
// because every scope.Files entry is already normalised and in-tree. It still
// errors rather than dropping the odd entry, so a future NewScope that admits
// something else surfaces here instead of quietly shrinking the mutation set.
func TestContainScopeRefusesUncontainedEntry(t *testing.T) {
	s := &verify.Scope{Root: "/repo", Files: []string{"a.go", "../x"}}

	got, err := containScope("/repo", s)
	if err == nil {
		t.Fatalf("containScope silently accepted %v", got)
	}
	if !errors.Is(err, pathfence.ErrEscapes) {
		t.Errorf("error is not pathfence.ErrEscapes: %v", err)
	}
	if got != nil {
		t.Errorf("a refusal still returned candidates: %v", got)
	}
}

// TestContainScopeHandlesNilScope: the detached path reconstructs its scope,
// and a nil one must not panic on the way to the error the caller already has.
func TestContainScopeHandlesNilScope(t *testing.T) {
	got, err := containScope("/repo", nil)
	if err != nil || got != nil {
		t.Fatalf("containScope(nil) = (%v, %v), want (nil, nil)", got, err)
	}
}

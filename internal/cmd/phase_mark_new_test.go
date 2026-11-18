package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/facts"
	"github.com/Rivil/dross/internal/phase"
)

// The fixtures reuse factsRepo and writeFactsSpec (phase_facts_test.go): a
// temp repo whose .dross holds one phase, "fx".

// runMarkNew runs `dross phase mark-new` with args, returning stdout and the
// error.
func runMarkNew(t *testing.T, args ...string) (string, error) {
	t.Helper()
	c := Phase()
	c.SetArgs(append([]string{"mark-new"}, args...))
	c.SetOut(new(bytes.Buffer))
	c.SetErr(new(bytes.Buffer))
	var err error
	out := captureStdout(t, func() { err = c.Execute() })
	return out, err
}

// fxSpecPath is fx's spec.toml in the fixture repo at dir.
func fxSpecPath(dir string) string {
	return filepath.Join(dir, ".dross", "phases", "fx", "spec.toml")
}

// readBytes returns path's content.
func readBytes(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// loadFxSpec decodes fx's spec.
func loadFxSpec(t *testing.T, dir string) *phase.Spec {
	t.Helper()
	s, err := phase.LoadSpec(fxSpecPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestPhaseMarkNewNormalisesAndPersists: `foo.go:12.` is stored as foo.go:12,
// and the next facts run over the same spec exits 0.
func TestPhaseMarkNewNormalisesAndPersists(t *testing.T) {
	dir := factsRepo(t)
	writeFactsSpec(t, dir, "the new handler lives at foo.go:12. and nowhere else")
	if _, _, err := runFacts(t, "fx"); err == nil {
		t.Fatal("fixture is wrong: facts reported nothing before mark-new")
	}

	out, err := runMarkNew(t, "fx", "foo.go:12.")
	if err != nil {
		t.Fatalf("mark-new refused an unresolved citation: %v\n%s", err, out)
	}
	if got := loadFxSpec(t, dir).New; !reflect.DeepEqual(got, []string{"foo.go:12"}) {
		t.Errorf("spec new = %q, want [\"foo.go:12\"]", got)
	}
	if !strings.Contains(out, "foo.go:12") {
		t.Errorf("mark-new did not say what it marked:\n%s", out)
	}
	if out, _, err := runFacts(t, "fx"); err != nil {
		t.Errorf("facts still fails after mark-new: %v\n%s", err, out)
	}
}

// TestPhaseMarkNewRefusesNotUnresolved: a citation facts does not report — a
// typo of the cited one, an argument that is no citation — exits non-zero
// naming it and leaves the spec byte-identical, even when another argument in
// the same call is markable.
func TestPhaseMarkNewRefusesNotUnresolved(t *testing.T) {
	dir := factsRepo(t)
	writeFactsSpec(t, dir, "see foo.go for it")
	before := readBytes(t, fxSpecPath(dir))

	for _, args := range [][]string{
		{"fx", "fooo.go"},
		{"fx", "foo.go", "fooo.go"},
		{"fx", "not-a-citation"},
	} {
		_, err := runMarkNew(t, args...)
		if err == nil {
			t.Fatalf("%v exited 0", args)
		}
		if bad := args[len(args)-1]; !strings.Contains(err.Error(), bad) {
			t.Errorf("%v: error does not name %q: %v", args, bad, err)
		}
		if after := readBytes(t, fxSpecPath(dir)); !bytes.Equal(before, after) {
			t.Errorf("%v: refused mark-new changed the spec:\n%s", args, after)
		}
	}
}

// TestPhaseMarkNewDedupes: marking the same citation twice, across calls or
// within one, leaves one entry.
func TestPhaseMarkNewDedupes(t *testing.T) {
	dir := factsRepo(t)
	writeFactsSpec(t, dir, "see foo.go for it")

	if out, err := runMarkNew(t, "fx", "foo.go", "(foo.go),"); err != nil {
		t.Fatalf("first mark-new: %v\n%s", err, out)
	}
	if out, err := runMarkNew(t, "fx", "foo.go"); err != nil {
		t.Fatalf("re-marking a listed citation must be a no-op, got: %v\n%s", err, out)
	}
	if got := loadFxSpec(t, dir).New; !reflect.DeepEqual(got, []string{"foo.go"}) {
		t.Errorf("spec new = %q, want one entry [\"foo.go\"]", got)
	}
}

// richSpec holds every spec section, so a Save that drops any of them shows.
const richSpec = `[phase]
  id = "fx"
  title = "Fx"
  milestone = "v9.9"

[[criteria]]
  id = "c-1"
  text = "see ` + missingCite + ` for the details"
  deferred = ["d1"]

[[criteria]]
  id = "c-2"
  text = "plain prose"

[[decisions]]
  key = "k"
  choice = "use the plain way"
  why = "because"
  locked = true

[[edges]]
  class = "empty"
  outcome = "covered"
  criteria = ["c-2"]

[[edges]]
  class = "encoding"
  outcome = "dismissed"
  reason = "no encoding here"

[[edges]]
  class = "adjacency"
  outcome = "deferred"
  deferred = "d2"

[[deferred]]
  id = "d1"
  text = "absorbed item"
  why = "later"

[[deferred]]
  id = "d2"
  text = "routed item"
  target = "other-phase"
`

// TestPhaseMarkNewPreservesSpec: the written spec carries `new` above
// [phase], and decodes to the original in every field but New.
func TestPhaseMarkNewPreservesSpec(t *testing.T) {
	dir := factsRepo(t)
	mustWrite(t, fxSpecPath(dir), richSpec)
	before := loadFxSpec(t, dir)

	if out, err := runMarkNew(t, "fx", missingCite); err != nil {
		t.Fatalf("mark-new: %v\n%s", err, out)
	}
	text := string(readBytes(t, fxSpecPath(dir)))
	newAt, phaseAt := strings.Index(text, "new = "), strings.Index(text, "[phase]")
	if newAt < 0 || phaseAt < 0 || newAt > phaseAt {
		t.Errorf("written spec must hold `new` above [phase]:\n%s", text)
	}
	after := loadFxSpec(t, dir)
	if !reflect.DeepEqual(after.New, []string{missingCite}) {
		t.Errorf("spec new = %q, want [%q]", after.New, missingCite)
	}
	after.New = nil
	if !reflect.DeepEqual(before, after) {
		t.Errorf("mark-new changed more than new:\nbefore %#v\nafter  %#v", before, after)
	}
}

// TestPhaseMarkNewRefusesMalformedSpec: a spec that does not decode is
// refused and left byte-identical.
func TestPhaseMarkNewRefusesMalformedSpec(t *testing.T) {
	dir := factsRepo(t)
	const bad = "[phase\nid = \"fx\"\n[[criteria]]\ntext = 'see foo.go'\n"
	mustWrite(t, fxSpecPath(dir), bad)

	if _, err := runMarkNew(t, "fx", "foo.go"); err == nil {
		t.Fatal("mark-new on an undecodable spec exited 0")
	}
	if got := string(readBytes(t, fxSpecPath(dir))); got != bad {
		t.Errorf("refused mark-new rewrote the malformed spec:\n%s", got)
	}
}

// TestMarkNewCitation: an argument is read as exactly one citation, with
// wrapping removed and a bare symbol read as a backtick span; its `new` entry
// is the cleaned path, path:line or symbol.
func TestMarkNewCitation(t *testing.T) {
	for _, tc := range []struct{ arg, entry string }{
		{"foo.go:12.", "foo.go:12"},
		{"(./a/b.go),", "a/b.go"},
		{"`Init` — x.go:3", "x.go:3"},
		{"quoted.String()", "quoted.String()"},
		{"`pkg.Name`", "pkg.Name"},
	} {
		c, err := markNewCitation(tc.arg)
		if err != nil {
			t.Errorf("%q: %v", tc.arg, err)
			continue
		}
		got, err := markNewEntry(c)
		if err != nil {
			t.Errorf("%q: entry: %v", tc.arg, err)
			continue
		}
		if got != tc.entry {
			t.Errorf("%q: entry %q, want %q", tc.arg, got, tc.entry)
		}
	}
	for _, arg := range []string{"plain", "a.go b.go"} {
		if _, err := markNewCitation(arg); err == nil {
			t.Errorf("%q was read as one citation", arg)
		}
	}
	if _, err := markNewEntry(facts.Citation{Kind: "future", Raw: "x"}); err == nil {
		t.Error("a citation kind markNewEntry does not know gave an entry, not an error")
	}
}

// TestMarkNewCoversAny: an entry is markable only when it covers a finding by
// the fact check's rule — a path covers a missing path and a line in it, a
// file:line covers only that line, a symbol only that symbol.
func TestMarkNewCoversAny(t *testing.T) {
	findings := []facts.Finding{
		{Kind: facts.KindMissing, Citation: "a.go"},
		{Kind: facts.KindGone, Citation: "`Init` — x.go:3"},
		{Kind: facts.KindMissing, Citation: "m.go:7"},
		{Kind: facts.KindUnresolved, Citation: "quoted.String()"},
	}
	for _, tc := range []struct {
		arg  string
		want bool
	}{
		{"a.go", true},
		{"x.go:3", true},
		{"x.go:4", false},
		{"m.go", true},
		{"quoted.String()", true},
		{"quoted.Other()", false},
		{"b.go", false},
	} {
		c, err := markNewCitation(tc.arg)
		if err != nil {
			t.Fatalf("%q: %v", tc.arg, err)
		}
		got, err := markNewCoversAny(c, findings)
		if err != nil {
			t.Fatalf("%q: %v", tc.arg, err)
		}
		if got != tc.want {
			t.Errorf("%q covers = %v, want %v", tc.arg, got, tc.want)
		}
	}
}

// TestMarkNewCoversAnyBrokenFinding: a finding whose citation does not re-read
// as one citation is an error naming it, never a silent "not unresolved".
func TestMarkNewCoversAnyBrokenFinding(t *testing.T) {
	c, err := markNewCitation("b.go")
	if err != nil {
		t.Fatal(err)
	}
	broken := []facts.Finding{{Field: "criteria c-1", Kind: facts.KindMissing, Citation: "a.go b.go"}}
	if _, err := markNewCoversAny(c, broken); err == nil || !strings.Contains(err.Error(), "a.go b.go") {
		t.Errorf("broken finding: err = %v, want an error naming it", err)
	}
}

// TestPhaseFactsPointsAtMarkNew: plain facts output with findings ends with
// the mark-new hint naming the phase; a clean run does not print it.
func TestPhaseFactsPointsAtMarkNew(t *testing.T) {
	r := buildFactsReport("fx", factsCheckedSpec, facts.Report{Unresolved: []facts.Finding{
		{Field: "criteria c-1", Kind: facts.KindMissing, Citation: "a.go"},
	}})
	out := captureStdout(t, func() { printFacts(r) })
	if !strings.Contains(out, "confirm new with `dross phase mark-new fx") {
		t.Errorf("facts output lacks the mark-new hint:\n%s", out)
	}
	clean := captureStdout(t, func() { printFacts(buildFactsReport("fx", factsCheckedSpec, facts.Report{})) })
	if strings.Contains(clean, "mark-new") {
		t.Errorf("a clean run printed the mark-new hint:\n%s", clean)
	}
}

// TestPhaseMarkNewIsRegistered: the verb resolves under `phase`, and README's
// phase row lists it.
func TestPhaseMarkNewIsRegistered(t *testing.T) {
	root := &cobra.Command{Use: "dross"}
	root.AddCommand(Phase())
	c, _, err := root.Find([]string{"phase", "mark-new"})
	if err != nil {
		t.Fatalf("phase mark-new does not resolve: %v", err)
	}
	if c.CommandPath() != "dross phase mark-new" {
		t.Errorf("resolved %q, want dross phase mark-new", c.CommandPath())
	}
	if !hasVerb(readmePhaseVerbs(t), "mark-new") {
		t.Error("README's phase row does not list the `mark-new` verb")
	}
}

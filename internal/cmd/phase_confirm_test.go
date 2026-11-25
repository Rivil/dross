package cmd

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/phase"
)

// The fixtures reuse factsRepo (phase_facts_test.go) and fxSpecPath,
// readBytes and loadFxSpec (phase_mark_new_test.go): a temp repo whose .dross
// holds one phase, "fx".

// runConfirm runs `dross phase confirm` with args, returning stdout and the
// error.
func runConfirm(t *testing.T, args ...string) (string, error) {
	t.Helper()
	c := Phase()
	c.SetArgs(append([]string{"confirm"}, args...))
	c.SetOut(new(bytes.Buffer))
	c.SetErr(new(bytes.Buffer))
	var err error
	out := captureStdout(t, func() { err = c.Execute() })
	return out, err
}

// doorChoice is the choice of confirmSpec's one locked one-way decision.
const doorChoice = "write the record into spec.toml"

// confirmSpec holds every spec section and one decision of each kind: the
// locked one-way "door" confirm accepts, and the reversible, unlocked and
// untagged ones it refuses.
const confirmSpec = `new = ["foo.go"]

[phase]
  id = "fx"
  title = "Fx"
  milestone = "v9.9"

[[criteria]]
  id = "c-1"
  text = "plain prose"
  deferred = ["d1"]

[[criteria]]
  id = "c-2"
  text = "more prose"

[[decisions]]
  key = "door"
  reversibility = "one-way"
  choice = "` + doorChoice + `"
  why = "because"
  locked = true

[[decisions]]
  key = "rev"
  reversibility = "reversible"
  choice = "flip it later"
  why = "cheap"
  locked = true

[[decisions]]
  key = "loose"
  reversibility = "one-way"
  choice = "still open"
  why = "plan decides"

[[decisions]]
  key = "legacy"
  choice = "predates the tag"
  why = "old"
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

// TestPhaseConfirmWritesHash: confirming the locked one-way decision writes
// DecisionHash(key, choice) onto it, clears it from UnconfirmedOneWay, and
// changes nothing else in the spec.
func TestPhaseConfirmWritesHash(t *testing.T) {
	dir := factsRepo(t)
	mustWrite(t, fxSpecPath(dir), confirmSpec)
	before := loadFxSpec(t, dir)
	if got := phase.UnconfirmedOneWay(before); !reflect.DeepEqual(got, []phase.Unconfirmed{{Key: "door"}}) {
		t.Fatalf("fixture is wrong: unconfirmed before confirm = %+v", got)
	}

	out, err := runConfirm(t, "fx", "door")
	if err != nil {
		t.Fatalf("confirm refused a locked one-way decision: %v\n%s", err, out)
	}
	if !strings.Contains(out, "confirmed: door") {
		t.Errorf("confirm did not say what it confirmed:\n%s", out)
	}
	after := loadFxSpec(t, dir)
	if want := phase.DecisionHash("door", doorChoice); after.Decisions[0].Confirmed != want {
		t.Errorf("confirmed = %q, want %q", after.Decisions[0].Confirmed, want)
	}
	if got := phase.UnconfirmedOneWay(after); len(got) != 0 {
		t.Errorf("unconfirmed after confirm = %+v, want none", got)
	}
	after.Decisions[0].Confirmed = ""
	if !reflect.DeepEqual(before, after) {
		t.Errorf("confirm changed more than the confirmation:\nbefore %#v\nafter  %#v", before, after)
	}
}

// TestPhaseConfirmRefusesWrongKey: an empty or unknown key is refused naming
// it and the spec's keys; a reversible, unlocked or untagged decision is not
// a locked one-way decision; a key two decisions share is ambiguous. Each
// refusal leaves spec.toml byte-identical.
func TestPhaseConfirmRefusesWrongKey(t *testing.T) {
	dir := factsRepo(t)
	mustWrite(t, fxSpecPath(dir), confirmSpec)
	before := readBytes(t, fxSpecPath(dir))

	for _, tc := range []struct {
		key  string
		want []string
	}{
		{"nope", []string{`"nope"`, `"door"`, `"rev"`, `"loose"`, `"legacy"`}},
		{"", []string{"empty", `"door"`, `"legacy"`}},
		{"rev", []string{"not a locked one-way decision", `"reversible"`}},
		{"loose", []string{"not a locked one-way decision", "not locked"}},
		{"legacy", []string{"not a locked one-way decision", "untagged"}},
	} {
		_, err := runConfirm(t, "fx", tc.key)
		if err == nil {
			t.Errorf("key %q: confirm exited 0", tc.key)
			continue
		}
		for _, w := range tc.want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("key %q: error does not name %s: %v", tc.key, w, err)
			}
		}
		if after := readBytes(t, fxSpecPath(dir)); !bytes.Equal(before, after) {
			t.Errorf("key %q: refused confirm changed the spec:\n%s", tc.key, after)
		}
	}

	twin := strings.Replace(confirmSpec, `key = "rev"
  reversibility = "reversible"`, `key = "door"
  reversibility = "one-way"`, 1)
	if twin == confirmSpec {
		t.Fatal("fixture is wrong: the twin rewrite matched nothing")
	}
	mustWrite(t, fxSpecPath(dir), twin)
	_, err := runConfirm(t, "fx", "door")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("a key two decisions carry was not refused as ambiguous: %v", err)
	}
	if got := string(readBytes(t, fxSpecPath(dir))); got != twin {
		t.Errorf("refused ambiguous confirm changed the spec:\n%s", got)
	}
}

// TestPhaseConfirmRefusesBadPhase: an id escaping phases/ is refused before
// anything is read — a spec planted where it points stays untouched — and an
// unknown phase and a phase with no spec are refused naming what is missing.
func TestPhaseConfirmRefusesBadPhase(t *testing.T) {
	dir := factsRepo(t)
	escaped := filepath.Join(dir, ".dross", "x", "spec.toml")
	mustWrite(t, escaped, confirmSpec)

	_, err := runConfirm(t, "../x", "door")
	if err == nil || !strings.Contains(err.Error(), "refused") {
		t.Errorf(`phase id "../x" was not refused: %v`, err)
	}
	if got := string(readBytes(t, escaped)); got != confirmSpec {
		t.Errorf("an escaping id reached the spec it points at:\n%s", got)
	}

	if _, err := runConfirm(t, "nope", "door"); err == nil || !strings.Contains(err.Error(), "unknown phase") {
		t.Errorf("an unknown phase was not refused as unknown: %v", err)
	}

	_, err = runConfirm(t, "fx", "door")
	if err == nil || !strings.Contains(err.Error(), "no spec") || !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a phase with no spec was not refused as missing its spec: %v", err)
	}
	if _, statErr := os.Stat(fxSpecPath(dir)); !errors.Is(statErr, fs.ErrNotExist) {
		t.Errorf("a refused confirm created a spec: %v", statErr)
	}
}

// TestPhaseConfirmIdempotent: confirming a decision whose confirmation is
// current says so and leaves spec.toml byte-identical.
func TestPhaseConfirmIdempotent(t *testing.T) {
	dir := factsRepo(t)
	mustWrite(t, fxSpecPath(dir), confirmSpec)
	if out, err := runConfirm(t, "fx", "door"); err != nil {
		t.Fatalf("first confirm: %v\n%s", err, out)
	}
	before := readBytes(t, fxSpecPath(dir))

	out, err := runConfirm(t, "fx", "door")
	if err != nil {
		t.Fatalf("re-confirming a current confirmation must be a no-op, got: %v\n%s", err, out)
	}
	if !strings.Contains(out, "already confirmed") {
		t.Errorf("re-confirm did not say already confirmed:\n%s", out)
	}
	if after := readBytes(t, fxSpecPath(dir)); !bytes.Equal(before, after) {
		t.Errorf("re-confirm rewrote the spec:\nbefore %s\nafter  %s", before, after)
	}
}

// TestPhaseConfirmReconfirmsEditedChoice is the c-8 path: a choice edited
// after its confirmation leaves a stale hash, and confirm overwrites it with
// the hash of the new choice.
func TestPhaseConfirmReconfirmsEditedChoice(t *testing.T) {
	dir := factsRepo(t)
	mustWrite(t, fxSpecPath(dir), confirmSpec)
	if out, err := runConfirm(t, "fx", "door"); err != nil {
		t.Fatalf("first confirm: %v\n%s", err, out)
	}
	old := loadFxSpec(t, dir).Decisions[0].Confirmed

	const edited = "write the record into a sidecar"
	text := string(readBytes(t, fxSpecPath(dir)))
	if !strings.Contains(text, doorChoice) {
		t.Fatalf("fixture is wrong: saved spec lost the choice:\n%s", text)
	}
	mustWrite(t, fxSpecPath(dir), strings.Replace(text, doorChoice, edited, 1))
	if got := phase.UnconfirmedOneWay(loadFxSpec(t, dir)); !reflect.DeepEqual(got, []phase.Unconfirmed{{Key: "door", Stale: true}}) {
		t.Fatalf("an edited choice did not void the confirmation: %+v", got)
	}

	out, err := runConfirm(t, "fx", "door")
	if err != nil {
		t.Fatalf("re-confirm after an edit: %v\n%s", err, out)
	}
	if !strings.Contains(out, "re-confirmed: door") {
		t.Errorf("re-confirm did not say it replaced a stale confirmation:\n%s", out)
	}
	after := loadFxSpec(t, dir)
	if want := phase.DecisionHash("door", edited); after.Decisions[0].Confirmed != want || want == old {
		t.Errorf("confirmed = %q, want the edited choice's hash %q (old %q)", after.Decisions[0].Confirmed, want, old)
	}
	if got := phase.UnconfirmedOneWay(after); len(got) != 0 {
		t.Errorf("unconfirmed after re-confirm = %+v, want none", got)
	}
}

// TestPhaseConfirmAwkwardChoice: a choice holding backticks, both quote kinds,
// a backslash, TOML's multi-line delimiters and newlines survives Save →
// LoadSpec, and the written hash still matches it.
func TestPhaseConfirmAwkwardChoice(t *testing.T) {
	dir := factsRepo(t)
	choice := "use `dross phase confirm` with \"double\" and 'single' quotes \\ a backslash\n" +
		"line two holds '''literal''' and \"\"\"basic\"\"\" delimiters\n\tand a tab"
	spec := &phase.Spec{
		Phase: phase.SpecPhase{ID: "fx", Title: "Fx"},
		Decisions: []phase.Decision{{
			Key: "door", Reversibility: phase.DoorOneWay, Choice: choice, Why: "w", Locked: true,
		}},
	}
	if err := spec.Save(fxSpecPath(dir)); err != nil {
		t.Fatal(err)
	}

	if out, err := runConfirm(t, "fx", "door"); err != nil {
		t.Fatalf("confirm: %v\n%s", err, out)
	}
	after := loadFxSpec(t, dir)
	d := after.Decisions[0]
	if d.Choice != choice {
		t.Fatalf("choice did not survive the round trip:\ngot  %q\nwant %q", d.Choice, choice)
	}
	if want := phase.DecisionHash("door", choice); d.Confirmed != want {
		t.Errorf("confirmed = %q, want %q", d.Confirmed, want)
	}
	if got := phase.UnconfirmedOneWay(after); len(got) != 0 {
		t.Errorf("unconfirmed after confirm = %+v, want none", got)
	}
	if out, err := runConfirm(t, "fx", "door"); err != nil || !strings.Contains(out, "already confirmed") {
		t.Errorf("the reloaded hash does not read as current: %v\n%s", err, out)
	}
}

// TestPhaseConfirmPassesSecretScan: the hash a confirmation writes is not a
// credential-shaped string, so the artifact secret scan finds nothing in a
// tree holding a confirmed spec.
func TestPhaseConfirmPassesSecretScan(t *testing.T) {
	dir := factsRepo(t)
	mustWrite(t, fxSpecPath(dir), confirmSpec)
	if out, err := runConfirm(t, "fx", "door"); err != nil {
		t.Fatalf("confirm: %v\n%s", err, out)
	}
	if !strings.Contains(string(readBytes(t, fxSpecPath(dir))), phase.DecisionHash("door", doorChoice)) {
		t.Fatal("fixture is wrong: the spec holds no confirmation to scan")
	}
	hits, err := scanDrossArtifacts(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Errorf("secret scan flagged a confirmed spec: %v", hits)
	}
}

// TestReadmePhaseRowListsConfirm: the README's `dross phase {…}` row lists
// confirm, and every verb it lists is a real phase subcommand.
func TestReadmePhaseRowListsConfirm(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRootFromTest(t), "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	var verbs []string
	for _, line := range strings.Split(string(b), "\n") {
		if rest, ok := strings.CutPrefix(line, "| `dross phase {"); ok {
			list, _, _ := strings.Cut(rest, "}")
			verbs = strings.Split(list, ",")
		}
	}
	if len(verbs) == 0 {
		t.Fatal("README has no `dross phase {...}` command row")
	}
	real := map[string]bool{}
	for _, c := range Phase().Commands() {
		real[c.Name()] = true
	}
	listed := false
	for _, v := range verbs {
		if !real[v] {
			t.Errorf("README phase row lists %q, which is no phase subcommand", v)
		}
		listed = listed || v == "confirm"
	}
	if !listed {
		t.Errorf("README phase row does not list confirm: %v", verbs)
	}
}

package verify

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/mutation"
)

// textIdentifier is a fake resolver with the same contract as the real one: a
// key derived from the line's source TEXT (never its position), ambiguity when
// that text appears more than once in the file, and an error when the subject
// is gone. Faking it here is the point of the Identifier seam — classification
// is exercised without a working tree.
type textIdentifier struct {
	// lines maps file -> line -> source text. A missing entry is a gone subject.
	lines map[string]map[int]string
}

var errSubjectGone = errors.New("survivor subject no longer exists")

func (ti textIdentifier) Identify(file string, line int, op string) (string, bool, error) {
	text, ok := ti.lines[file][line]
	if !ok || text == "" {
		return "", false, errSubjectGone
	}
	occurrences := 0
	for _, other := range ti.lines[file] {
		if other == text {
			occurrences++
		}
	}
	return file + "|" + op + "|" + text, occurrences > 1, nil
}

// fixtureIdentifier is the standard four-survivor fixture's resolver.
func fixtureIdentifier() textIdentifier {
	return textIdentifier{lines: map[string]map[int]string{
		"in.go":      {10: "if x > 0 {"},
		"routed.go":  {20: "return err"},
		"accept.go":  {30: "case 1:"},
		"bare.go":    {40: "y := 2"},
		"dup.go":     {50: "return nil", 60: "return nil"},
		"changed.go": {70: "if x >= 0 {"},
	}}
}

func keyOf(t *testing.T, ti textIdentifier, file string, line int, op string) string {
	t.Helper()
	key, _, err := ti.Identify(file, line, op)
	if err != nil {
		t.Fatalf("Identify(%s:%d): %v", file, line, err)
	}
	return key
}

// fourStateFixture builds one survivor per state and the maps that put it there.
func fourStateFixture(t *testing.T) ([]Survivor, map[string]string, map[string]string, textIdentifier) {
	t.Helper()
	ti := fixtureIdentifier()
	survivors := []Survivor{
		{File: "in.go", Line: 10, Op: "OP", Language: "go"},
		{File: "routed.go", Line: 20, Op: "OP", Language: "go", OutOfScope: true},
		{File: "accept.go", Line: 30, Op: "OP", Language: "go", OutOfScope: true},
		{File: "bare.go", Line: 40, Op: "OP", Language: "go", OutOfScope: true},
	}
	accepted := map[string]string{keyOf(t, ti, "accept.go", 30, "OP"): "gremlins switch-case ceiling"}
	routed := map[string]string{keyOf(t, ti, "routed.go", 20, "OP"): "board-sync-truth"}
	return survivors, accepted, routed, ti
}

// TestClassifyPartitionsEverySurvivor is c-1's arithmetic half: the four states
// partition the input. A survivor counted twice inflates the backlog; one
// counted zero times is the silent inheritance this phase exists to end.
func TestClassifyPartitionsEverySurvivor(t *testing.T) {
	survivors, accepted, routed, ti := fourStateFixture(t)
	l := Classify(survivors, accepted, routed, ti)
	if l.Total() != len(survivors) {
		t.Fatalf("classified %d survivors (in-diff %d, routed %d, accepted %d, unclassified %d), want %d",
			l.Total(), len(l.InDiff), len(l.Routed), len(l.Accepted), len(l.Unclassified), len(survivors))
	}
}

// TestClassifyAssignsEachStateOnce is c-1's coverage half: one survivor per
// state, each landing in its own bucket with a non-empty record.
func TestClassifyAssignsEachStateOnce(t *testing.T) {
	survivors, accepted, routed, ti := fourStateFixture(t)
	l := Classify(survivors, accepted, routed, ti)

	buckets := map[string][]Classified{
		LifecycleInDiff:       l.InDiff,
		LifecycleRouted:       l.Routed,
		LifecycleAccepted:     l.Accepted,
		LifecycleUnclassified: l.Unclassified,
	}
	for state, got := range buckets {
		if len(got) != 1 {
			t.Errorf("state %s has %d records, want exactly 1: %+v", state, len(got), got)
			continue
		}
		c := got[0]
		if c.State != state {
			t.Errorf("record in %s bucket carries state %q", state, c.State)
		}
		if c.File == "" || c.Line == 0 || c.Op == "" || c.Key == "" {
			t.Errorf("state %s record is incomplete: %+v", state, c)
		}
	}
	if l.Accepted[0].Note != "gremlins switch-case ceiling" {
		t.Errorf("accepted record should carry the acceptance reason, got %q", l.Accepted[0].Note)
	}
}

// TestRoutedRecordCarriesDestination pins routed_state_source: a routed
// survivor is bucketed AND labelled. A routed record with no destination is
// debt filed under "somewhere", which is how routing stops closing the loop.
func TestRoutedRecordCarriesDestination(t *testing.T) {
	survivors, accepted, routed, ti := fourStateFixture(t)
	l := Classify(survivors, accepted, routed, ti)
	if len(l.Routed) != 1 {
		t.Fatalf("want 1 routed record, got %d", len(l.Routed))
	}
	got := l.Routed[0]
	if got.Target != "board-sync-truth" {
		t.Errorf("Target = %q, want board-sync-truth", got.Target)
	}
	if !strings.Contains(got.Note, "board-sync-truth") {
		t.Errorf("Note = %q, must name the destination — it is what carries it onto the wire", got.Note)
	}
}

// TestAcceptedBeatsRouted pins precedence. A survivor that is both accepted and
// routed must land in one bucket, not two — otherwise Total() over-counts and
// the same debt is reported as both settled and outstanding.
func TestAcceptedBeatsRouted(t *testing.T) {
	ti := fixtureIdentifier()
	key := keyOf(t, ti, "accept.go", 30, "OP")
	survivors := []Survivor{{File: "accept.go", Line: 30, Op: "OP", OutOfScope: true}}

	l := Classify(survivors,
		map[string]string{key: "accepted reason"},
		map[string]string{key: "some-phase"},
		ti)

	if l.Total() != 1 {
		t.Fatalf("survivor landed in %d buckets, want 1", l.Total())
	}
	if len(l.Accepted) != 1 {
		t.Errorf("accepted must win over routed; got in-diff %d routed %d accepted %d unclassified %d",
			len(l.InDiff), len(l.Routed), len(l.Accepted), len(l.Unclassified))
	}
}

// TestAmbiguousAcceptanceDoesNotSuppress pins the locked survivor_identity
// ambiguity rule. The safe failure direction is noise: a lapsed acceptance is
// visible, a false suppression hides a real bug.
func TestAmbiguousAcceptanceDoesNotSuppress(t *testing.T) {
	ti := fixtureIdentifier()
	key := keyOf(t, ti, "dup.go", 50, "OP")
	survivors := []Survivor{{File: "dup.go", Line: 50, Op: "OP"}}

	l := Classify(survivors, map[string]string{key: "accepted reason"}, nil, ti)

	if len(l.Accepted) != 0 {
		t.Fatalf("ambiguous key was suppressed by an acceptance: %+v", l.Accepted)
	}
	if len(l.InDiff) != 1 {
		t.Fatalf("ambiguous in-scope survivor should re-emit as in-diff, got %+v", l)
	}
	if !strings.Contains(l.InDiff[0].Note, "ambiguous") {
		t.Errorf("re-emitted survivor must say why the acceptance was withheld, got note %q", l.InDiff[0].Note)
	}
}

// TestFalseSuppressionGuard is the other half of c-4: a genuinely new survivor
// where an accepted one used to sit must NOT inherit its acceptance. The
// acceptance is keyed on the old text, the survivor resolves to the new one.
func TestFalseSuppressionGuard(t *testing.T) {
	ti := fixtureIdentifier()
	// The acceptance was recorded when line 70 read "if x > 0 {"; the file now
	// reads "if x >= 0 {" at that same file+line+op.
	staleKey := "changed.go|OP|if x > 0 {"
	survivors := []Survivor{{File: "changed.go", Line: 70, Op: "OP"}}

	l := Classify(survivors, map[string]string{staleKey: "accepted reason"}, nil, ti)

	if len(l.Accepted) != 0 {
		t.Fatalf("a changed line inherited the old line's acceptance: %+v", l.Accepted)
	}
	if len(l.InDiff) != 1 {
		t.Fatalf("the new survivor should re-emit, got %+v", l)
	}
}

// TestUnresolvableSubjectIsUnclassified pins the error path: a subject that
// cannot be resolved is unclassified with the reason recorded — never silently
// dropped, never defaulted to in-diff (which would blame this phase for it),
// and never a panic.
func TestUnresolvableSubjectIsUnclassified(t *testing.T) {
	ti := fixtureIdentifier()
	survivors := []Survivor{{File: "deleted.go", Line: 5, Op: "OP"}}

	l := Classify(survivors, nil, nil, ti)

	if l.Total() != 1 {
		t.Fatalf("unresolvable survivor was dropped: %+v", l)
	}
	if len(l.Unclassified) != 1 {
		t.Fatalf("want unclassified, got in-diff %d routed %d accepted %d", len(l.InDiff), len(l.Routed), len(l.Accepted))
	}
	if !strings.Contains(l.Unclassified[0].Note, errSubjectGone.Error()) {
		t.Errorf("note must carry the resolution error, got %q", l.Unclassified[0].Note)
	}
}

// TestClassifyDedupsIdenticalRows: adapters can report one survivor twice.
// Without dedup, Total() over-counts and one acceptance suppresses only one of
// the copies — leaving a phantom survivor nobody can drain.
func TestClassifyDedupsIdenticalRows(t *testing.T) {
	ti := fixtureIdentifier()
	key := keyOf(t, ti, "in.go", 10, "OP")
	survivors := []Survivor{
		{File: "in.go", Line: 10, Op: "OP", Language: "go"},
		{File: "in.go", Line: 10, Op: "OP", Language: "go"},
	}

	l := Classify(survivors, nil, nil, ti)
	if l.Total() != 1 {
		t.Fatalf("duplicate rows produced %d records, want 1", l.Total())
	}

	l = Classify(survivors, map[string]string{key: "reason"}, nil, ti)
	if len(l.Accepted) != 1 || l.Total() != 1 {
		t.Errorf("one acceptance must suppress both duplicate rows, got %+v", l)
	}
}

// TestApplyLifecycleStampsBothRecordTypes: the states must reach the records
// that get persisted, in-scope and out-of-scope alike (c-7). A classification
// that lives only in memory is invisible to the next run.
func TestApplyLifecycleStampsBothRecordTypes(t *testing.T) {
	survivors, accepted, routed, ti := fourStateFixture(t)
	_ = survivors

	tests := &Tests{
		Languages: []LanguageRun{{
			Name: "go", Tool: "gremlins",
			Mutation: &mutation.Report{Surviving: []mutation.Mutant{
				{File: "in.go", Line: 10, Op: "OP"},
			}},
		}},
		OutOfScope: []OutOfScopeMutant{
			{File: "routed.go", Line: 20, Op: "OP", Language: "go"},
			{File: "accept.go", Line: 30, Op: "OP", Language: "go"},
			{File: "bare.go", Line: 40, Op: "OP", Language: "go"},
		},
	}

	l := ApplyLifecycle(tests, accepted, routed, ti)
	if l.Total() != 4 {
		t.Fatalf("classified %d, want 4", l.Total())
	}

	got := tests.Languages[0].Mutation.Surviving[0]
	if got.Lifecycle != LifecycleInDiff || got.Key == "" {
		t.Errorf("in-scope survivor not stamped: %+v", got)
	}
	wantStates := map[string]string{
		"routed.go": LifecycleRouted,
		"accept.go": LifecycleAccepted,
		"bare.go":   LifecycleUnclassified,
	}
	for _, o := range tests.OutOfScope {
		if o.Lifecycle != wantStates[o.File] {
			t.Errorf("%s: Lifecycle = %q, want %q", o.File, o.Lifecycle, wantStates[o.File])
		}
		if o.Key == "" {
			t.Errorf("%s: out-of-scope record has no key", o.File)
		}
		if o.Note == "" {
			t.Errorf("%s: out-of-scope record has no note explaining its state", o.File)
		}
	}
}

// TestNoSurvivorReachesTestsJSONStateless is c-1 at the persistence boundary:
// after classification, an empty state is a bug, not a default.
func TestNoSurvivorReachesTestsJSONStateless(t *testing.T) {
	survivors, accepted, routed, ti := fourStateFixture(t)
	_ = survivors

	tests := &Tests{
		Languages: []LanguageRun{{
			Name: "go", Tool: "gremlins",
			Mutation: &mutation.Report{Surviving: []mutation.Mutant{{File: "in.go", Line: 10, Op: "OP"}}},
		}},
		OutOfScope: []OutOfScopeMutant{
			{File: "routed.go", Line: 20, Op: "OP"},
			{File: "accept.go", Line: 30, Op: "OP"},
			{File: "bare.go", Line: 40, Op: "OP"},
			{File: "deleted.go", Line: 99, Op: "OP"},
		},
	}
	ApplyLifecycle(tests, accepted, routed, ti)

	for _, m := range tests.Languages[0].Mutation.Surviving {
		if m.Lifecycle == "" {
			t.Errorf("in-scope survivor %s:%d reached tests.json with no state", m.File, m.Line)
		}
	}
	for _, o := range tests.OutOfScope {
		if o.Lifecycle == "" {
			t.Errorf("out-of-scope survivor %s:%d reached tests.json with no state", o.File, o.Line)
		}
	}
}

// TestOutOfScopeMutantWireForm pins both the round-trip and the serialised key
// names. The lowercase casing here differs from mutation.Mutant's capitalised
// (tagless) form; that split is pre-existing and deliberately accepted, so both
// shapes are pinned rather than left to drift into each other.
func TestOutOfScopeMutantWireForm(t *testing.T) {
	in := OutOfScopeMutant{
		File: "internal/x.go", Line: 42, Op: "OP", Language: "go",
		Key: "deadbeef", Lifecycle: LifecycleRouted, Note: "routed to board-sync-truth",
	}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal into map: %v", err)
	}
	for _, want := range []string{"key", "lifecycle", "note"} {
		if _, ok := fields[want]; !ok {
			t.Errorf("OutOfScopeMutant did not serialise %q; raw = %s", want, raw)
		}
	}

	var out OutOfScopeMutant
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out != in {
		t.Errorf("round-trip = %+v, want %+v", out, in)
	}
}

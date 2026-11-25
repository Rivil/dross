package phase

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// decisionSpec is a spec holding the given decisions.
func decisionSpec(ds ...Decision) *Spec {
	return &Spec{
		Phase:     SpecPhase{ID: "doors", Title: "Doors"},
		Criteria:  []Criterion{{ID: "c-1", Text: "one"}},
		Decisions: ds,
	}
}

// TestDecisionHashShape pins the hash format: the NUL separator keeps a
// shifted boundary apart, and a fixed input hashes to a fixed literal — a
// re-salted, truncated, unprefixed or separator-less hash fails one of them.
func TestDecisionHashShape(t *testing.T) {
	if DecisionHash("ab", "c") == DecisionHash("a", "bc") {
		t.Errorf("DecisionHash(ab,c) == DecisionHash(a,bc): the key/choice boundary is not separated")
	}
	const want = "sha256:b644798fb6e0bd30c9d84203ee598955ececcfcc0f826bbc3116c27bf66faed2"
	if got := DecisionHash("k", "x"); got != want {
		t.Errorf("DecisionHash(k,x) = %q, want %q", got, want)
	}
}

// TestDecisionHashSensitivity: one changed byte of key or choice changes the
// hash; the phase id is no input, so a renamed phase keeps its confirmation.
func TestDecisionHashSensitivity(t *testing.T) {
	base := DecisionHash("schema", "json v2")
	for _, c := range []struct{ key, choice string }{
		{"schemb", "json v2"},
		{"schema", "json v3"},
		{"schema", "json v2 "},
		{"Schema", "json v2"},
	} {
		if DecisionHash(c.key, c.choice) == base {
			t.Errorf("DecisionHash(%q, %q) equals DecisionHash(schema, json v2)", c.key, c.choice)
		}
	}

	d := Decision{Key: "schema", Choice: "json v2", Locked: true, Reversibility: DoorOneWay, Confirmed: base}
	for _, id := range []string{"doors", "doors-renamed"} {
		s := decisionSpec(d)
		s.Phase.ID = id
		if got := UnconfirmedOneWay(s); len(got) != 0 {
			t.Errorf("phase %q: confirmed decision listed as unconfirmed: %+v", id, got)
		}
	}
}

// TestReversibilityProblemsTable pins every rule ReversibilityProblems
// enforces, in both requireTags modes.
func TestReversibilityProblemsTable(t *testing.T) {
	const want = "want one of reversible, one-way"
	cases := []struct {
		name string
		d    Decision
		// strict and lenient are substrings the single problem line must hold
		// with requireTags true and false; nil means no problem.
		strict, lenient []string
	}{
		{"locked untagged", Decision{Key: "fmt", Locked: true},
			[]string{"decisions[0] (fmt)", "missing", want}, nil},
		{"locked reversible", Decision{Key: "fmt", Locked: true, Reversibility: "reversible"}, nil, nil},
		{"locked one-way", Decision{Key: "fmt", Locked: true, Reversibility: "one-way"}, nil, nil},
		{"capitalised", Decision{Key: "fmt", Locked: true, Reversibility: "One-way"},
			[]string{"decisions[0] (fmt)", `unknown reversibility "One-way"`, want},
			[]string{"decisions[0] (fmt)", `unknown reversibility "One-way"`, want}},
		{"underscore", Decision{Key: "fmt", Locked: true, Reversibility: "one_way"},
			[]string{"decisions[0] (fmt)", `"one_way"`, want},
			[]string{"decisions[0] (fmt)", `"one_way"`, want}},
		{"leading space", Decision{Key: "fmt", Locked: true, Reversibility: " one-way"},
			[]string{"decisions[0] (fmt)", `" one-way"`, want},
			[]string{"decisions[0] (fmt)", `" one-way"`, want}},
		{"no hyphen", Decision{Key: "fmt", Locked: true, Reversibility: "oneway"},
			[]string{"decisions[0] (fmt)", `"oneway"`, want},
			[]string{"decisions[0] (fmt)", `"oneway"`, want}},
		{"unlocked untagged", Decision{Key: "fmt"}, nil, nil},
		{"unlocked unknown", Decision{Key: "fmt", Reversibility: "maybe"},
			[]string{"decisions[0] (fmt)", `unknown reversibility "maybe"`, want},
			[]string{"decisions[0] (fmt)", `unknown reversibility "maybe"`, want}},
		{"unlocked one-way", Decision{Key: "fmt", Reversibility: "one-way"}, nil, nil},
	}
	for _, c := range cases {
		for _, mode := range []struct {
			require bool
			want    []string
		}{{true, c.strict}, {false, c.lenient}} {
			got := ReversibilityProblems(decisionSpec(c.d), mode.require)
			if mode.want == nil {
				if len(got) != 0 {
					t.Errorf("%s (requireTags=%v): want no problem, got %v", c.name, mode.require, got)
				}
				continue
			}
			if len(got) != 1 {
				t.Errorf("%s (requireTags=%v): want one problem, got %v", c.name, mode.require, got)
				continue
			}
			line := got[0].String()
			for _, sub := range mode.want {
				if !strings.Contains(line, sub) {
					t.Errorf("%s (requireTags=%v): %q lacks %q", c.name, mode.require, line, sub)
				}
			}
		}
	}
}

// TestReversibilityProblemsKinds: the missing line is exactly the missing
// kind, the unknown line exactly the unknown kind with its value.
func TestReversibilityProblemsKinds(t *testing.T) {
	got := ReversibilityProblems(decisionSpec(
		Decision{Key: "a", Locked: true},
		Decision{Key: "b", Locked: true, Reversibility: "One-way"},
	), true)
	want := []ReversibilityProblem{
		{Index: 0, Key: "a", Locked: true, Kind: ReversibilityMissing},
		{Index: 1, Key: "b", Locked: true, Kind: ReversibilityUnknown, Value: "One-way"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
	if s := got[0].String(); strings.Contains(s, "unknown") {
		t.Errorf("missing line reads as unknown: %q", s)
	}
	if s := got[1].String(); strings.Contains(s, "missing") {
		t.Errorf("unknown line reads as missing: %q", s)
	}
}

// TestReversibilityProblemsKeepIndex: each problem carries the decision's
// index in the full spec, its key and its lock, so filtering to Locked still
// names the right decisions[i].
func TestReversibilityProblemsKeepIndex(t *testing.T) {
	s := decisionSpec(
		Decision{Key: "ok", Locked: true, Reversibility: DoorReversible},
		Decision{Key: "loose", Reversibility: "nope"},
		Decision{Key: "fine", Locked: true, Reversibility: DoorOneWay},
		Decision{Key: "bad", Locked: true, Reversibility: "One-way"},
	)
	got := ReversibilityProblems(s, true)
	if len(got) != 2 {
		t.Fatalf("want 2 problems, got %v", got)
	}
	if got[0].Index != 1 || got[0].Key != "loose" || got[0].Locked {
		t.Errorf("first problem = %+v, want decisions[1] (loose) unlocked", got[0])
	}
	var locked []ReversibilityProblem
	for _, p := range got {
		if p.Locked {
			locked = append(locked, p)
		}
	}
	if len(locked) != 1 || locked[0].Index != 3 || locked[0].Key != "bad" {
		t.Fatalf("locked problems = %+v, want only decisions[3] (bad)", locked)
	}
	if line := locked[0].String(); !strings.HasPrefix(line, "decisions[3] (bad): ") {
		t.Errorf("filtered line %q does not name decisions[3] (bad)", line)
	}
}

// TestReversibilityProblemsEmpty: a spec with no decisions, or none at all,
// has no problem.
func TestReversibilityProblemsEmpty(t *testing.T) {
	if got := ReversibilityProblems(decisionSpec(), true); len(got) != 0 {
		t.Errorf("no decisions: got %v", got)
	}
	if got := ReversibilityProblems(nil, true); len(got) != 0 {
		t.Errorf("nil spec: got %v", got)
	}
}

// TestUnconfirmedOneWay pins which decisions the plan-lock gate refuses and
// which it calls stale.
func TestUnconfirmedOneWay(t *testing.T) {
	oneWay := func(key, choice, conf string) Decision {
		return Decision{Key: key, Choice: choice, Locked: true, Reversibility: DoorOneWay, Confirmed: conf}
	}
	cases := []struct {
		name string
		ds   []Decision
		want []Unconfirmed
	}{
		{"matching hash", []Decision{oneWay("fmt", "toml", DecisionHash("fmt", "toml"))}, nil},
		{"never confirmed", []Decision{oneWay("fmt", "toml", "")},
			[]Unconfirmed{{Key: "fmt", Stale: false}}},
		{"choice edited after confirm", []Decision{oneWay("fmt", "yaml", DecisionHash("fmt", "toml"))},
			[]Unconfirmed{{Key: "fmt", Stale: true}}},
		{"hash copied from another key", []Decision{oneWay("fmt", "toml", DecisionHash("other", "toml"))},
			[]Unconfirmed{{Key: "fmt", Stale: true}}},
		{"reversible", []Decision{{Key: "fmt", Locked: true, Reversibility: DoorReversible}}, nil},
		{"untagged", []Decision{{Key: "fmt", Locked: true}}, nil},
		{"unlocked one-way", []Decision{{Key: "fmt", Reversibility: DoorOneWay}}, nil},
		{"unknown tag", []Decision{{Key: "fmt", Locked: true, Reversibility: "One-way"}}, nil},
		{"nil decisions", nil, nil},
		{"order kept", []Decision{
			oneWay("b", "x", ""),
			{Key: "skip", Locked: true, Reversibility: DoorReversible},
			oneWay("a", "y", DecisionHash("a", "z")),
		}, []Unconfirmed{{Key: "b"}, {Key: "a", Stale: true}}},
	}
	for _, c := range cases {
		got := UnconfirmedOneWay(decisionSpec(c.ds...))
		if len(got) == 0 && len(c.want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
	if got := UnconfirmedOneWay(nil); len(got) != 0 {
		t.Errorf("nil spec: got %+v", got)
	}
}

// TestDoorLabel: the four renderer labels are distinct, and an unlocked
// one-way decision is never called unconfirmed — no command can confirm it.
func TestDoorLabel(t *testing.T) {
	cases := []struct {
		name string
		d    Decision
		want string
	}{
		{"one-way confirmed", Decision{Key: "k", Choice: "x", Locked: true, Reversibility: DoorOneWay, Confirmed: DecisionHash("k", "x")}, "one-way"},
		{"one-way stale", Decision{Key: "k", Choice: "y", Locked: true, Reversibility: DoorOneWay, Confirmed: DecisionHash("k", "x")}, "one-way, unconfirmed"},
		{"one-way empty", Decision{Key: "k", Choice: "x", Locked: true, Reversibility: DoorOneWay}, "one-way, unconfirmed"},
		{"reversible", Decision{Key: "k", Choice: "x", Locked: true, Reversibility: DoorReversible}, "reversible"},
		{"untagged", Decision{Key: "k", Choice: "x", Locked: true}, ""},
		{"unlocked one-way unconfirmed", Decision{Key: "k", Choice: "x", Reversibility: DoorOneWay}, "one-way"},
		{"unknown tag", Decision{Key: "k", Choice: "x", Locked: true, Reversibility: "One-way"}, ""},
	}
	seen := map[string]bool{}
	for _, c := range cases {
		got := DoorLabel(c.d)
		if got != c.want {
			t.Errorf("%s: DoorLabel = %q, want %q", c.name, got, c.want)
		}
		seen[got] = true
	}
	if len(seen) != 4 {
		t.Errorf("want 4 distinct labels, got %v", seen)
	}
}

// TestDecisionTagsRoundTrip: a tagged decision saves and reloads equal; a
// legacy one saves with neither key, so pre-tag specs stay byte-clean.
func TestDecisionTagsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spec.toml")
	tagged := decisionSpec(
		Decision{Key: "k", Choice: "x", Why: "w", Locked: true, Reversibility: DoorOneWay, Confirmed: DecisionHash("k", "x")},
		Decision{Key: "r", Choice: "y", Why: "w", Locked: true, Reversibility: DoorReversible},
	)
	if err := tagged.Save(path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{`reversibility = "one-way"`, `reversibility = "reversible"`, `confirmed = "` + DecisionHash("k", "x") + `"`} {
		if !strings.Contains(string(raw), sub) {
			t.Errorf("saved spec lacks %s:\n%s", sub, raw)
		}
	}
	loaded, err := LoadSpec(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tagged, loaded) {
		t.Errorf("round-trip drift:\norig: %+v\nload: %+v", tagged, loaded)
	}

	legacy := decisionSpec(Decision{Key: "k", Choice: "x", Why: "w", Locked: true})
	if err := legacy.Save(path); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"reversibility", "confirmed"} {
		if strings.Contains(string(raw), key) {
			t.Errorf("legacy spec wrote a %s key:\n%s", key, raw)
		}
	}
}

// TestDecisionJSONKeys: the JSON keys are lowercase and omitted when empty.
func TestDecisionJSONKeys(t *testing.T) {
	b, err := json.Marshal(Decision{Key: "k", Choice: "x", Locked: true, Reversibility: DoorOneWay, Confirmed: "sha256:ab"})
	if err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{`"reversibility":"one-way"`, `"confirmed":"sha256:ab"`} {
		if !strings.Contains(string(b), sub) {
			t.Errorf("tagged JSON lacks %s: %s", sub, b)
		}
	}
	b, err = json.Marshal(Decision{Key: "k", Choice: "x", Locked: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"reversibility", "Reversibility", "confirmed", "Confirmed"} {
		if strings.Contains(string(b), key) {
			t.Errorf("legacy JSON holds %s: %s", key, b)
		}
	}
}

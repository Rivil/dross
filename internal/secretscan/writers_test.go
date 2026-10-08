package secretscan

import (
	"reflect"
	"strings"
	"testing"
)

// The writer registry's truth — that every file-writing Go file under
// internal/ is declared — is proven by the tree walk in internal/cmd. What
// lives here is the registry's own logic: the validator and the name
// flattening. gremlins mutates one package at a time, so those have to be
// killed by this package's tests or they are never killed at all.

// TestWriterValidationNamesEveryMalformedShape feeds ValidateWriters one
// malformed entry per guard and requires exactly one error naming it.
func TestWriterValidationNamesEveryMalformedShape(t *testing.T) {
	under := &UnderDross{Artifacts: []string{"a"}}
	local := &MachineLocal{Path: "p", IgnoreSeed: "s", Why: "w"}
	outside := &OutsideDross{Why: "w"}
	const f = "internal/x.go"

	cases := []struct {
		name string
		in   []Writer
		want string
	}{
		{"blank File", []Writer{{File: "  ", OutsideDross: outside}}, "empty File"},
		{"no disposition", []Writer{{File: f}}, "no disposition"},
		{"two dispositions", []Writer{{File: f, UnderDross: under, OutsideDross: outside}}, "2 dispositions"},
		{"three dispositions", []Writer{{File: f, UnderDross: under, MachineLocal: local, OutsideDross: outside}}, "3 dispositions"},
		{"under-dross, no artifacts", []Writer{{File: f, UnderDross: &UnderDross{}}}, "no Artifacts"},
		{"artifact with .dross/ prefix", []Writer{{File: f, UnderDross: &UnderDross{Artifacts: []string{".dross/a"}}}}, "must be .dross-relative"},
		{"slash-absolute artifact", []Writer{{File: f, UnderDross: &UnderDross{Artifacts: []string{"/a"}}}}, "must be .dross-relative"},
		{"machine-local, no Path", []Writer{{File: f, MachineLocal: &MachineLocal{Path: " ", IgnoreSeed: "s", Why: "w"}}}, "machine-local with no Path"},
		{"machine-local, blank Also", []Writer{{File: f, MachineLocal: &MachineLocal{Path: "p", Also: []string{" "}, IgnoreSeed: "s", Why: "w"}}}, "blank Also path"},
		{"machine-local, no seed", []Writer{{File: f, MachineLocal: &MachineLocal{Path: "p", Why: "w"}}}, "no IgnoreSeed"},
		{"machine-local, no Why", []Writer{{File: f, MachineLocal: &MachineLocal{Path: "p", IgnoreSeed: "s"}}}, "machine-local with no Why"},
		{"outside-dross, no Why", []Writer{{File: f, OutsideDross: &OutsideDross{Paths: []string{"x"}}}}, "outside-dross with no Why"},
		{"agent-authored not under .dross", []Writer{{File: AgentAuthored, OutsideDross: outside}}, "under .dross by definition"},
		{"declared twice", []Writer{{File: f, OutsideDross: outside}, {File: f, OutsideDross: outside}}, "declared twice"},
	}
	for _, c := range cases {
		errs := ValidateWriters(c.in)
		if len(errs) != 1 {
			t.Errorf("%s: want exactly one error, got %d: %v", c.name, len(errs), errs)
			continue
		}
		if !strings.Contains(errs[0].Error(), c.want) {
			t.Errorf("%s: error %q does not mention %q", c.name, errs[0], c.want)
		}
	}
}

// TestLiveWriterRegistryValidatesInPackage: the shipped registry is well
// formed. Because it holds a good entry of every disposition plus the
// agent-authored one, negating any guard in ValidateWriters makes a good entry
// error here — which is what kills those mutants in this package.
func TestLiveWriterRegistryValidatesInPackage(t *testing.T) {
	live := Writers()
	if errs := ValidateWriters(live); len(errs) != 0 {
		t.Fatalf("the live registry is malformed: %v", errs)
	}
	var under, local, outside, agent bool
	for _, w := range live {
		under = under || w.UnderDross != nil
		local = local || w.MachineLocal != nil
		outside = outside || w.OutsideDross != nil
		agent = agent || w.File == AgentAuthored
	}
	if !under || !local || !outside || !agent {
		t.Fatalf("the live registry no longer spans every disposition (under=%v local=%v outside=%v agent=%v) — the guards it exercises would go unmeasured",
			under, local, outside, agent)
	}
}

// TestArtifactNamesSpansEveryDisposition: each disposition contributes its
// paths, in registry order, and an entry with none contributes nothing.
func TestArtifactNamesSpansEveryDisposition(t *testing.T) {
	in := []Writer{
		{File: "u", UnderDross: &UnderDross{Artifacts: []string{"a", "b"}}},
		{File: "m", MachineLocal: &MachineLocal{Path: "p"}},
		{File: "o", OutsideDross: &OutsideDross{Paths: []string{"x", "y"}}},
		{File: "none"},
	}
	if got, want := ArtifactNames(in), []string{"a", "b", "p", "x", "y"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ArtifactNames = %v, want %v", got, want)
	}
}

// TestWritersReturnsACopy: a caller mutating the returned slice must not
// rewrite the registry for everyone after it.
func TestWritersReturnsACopy(t *testing.T) {
	first := Writers()
	orig := first[0].File
	first[0] = Writer{File: "clobbered"}
	if got := Writers()[0].File; got != orig {
		t.Errorf("Writers()[0].File = %q after a caller overwrote its copy, want %q", got, orig)
	}
}

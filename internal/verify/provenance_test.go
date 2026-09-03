package verify

// Provenance across a POOL: what a run records when its legs did not all land
// on the same machine.
//
// measured_on_test.go covers the single-machine shapes, which must not change.
// These are about the shape that did not exist before per-lane host affinity:
// two candidates, two legs, and a record that has to stay readable as evidence
// afterwards (c-4).

import (
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/mutation"
	"github.com/Rivil/dross/internal/remote"
)

// TestMeasuredAcrossKeepsASingleHostUnchanged is the compatibility half. The
// overwhelmingly common run still uses one machine, and a record that started
// rendering "helicon" as anything else would make every existing verify.toml
// read as though something had changed.
func TestMeasuredAcrossKeepsASingleHostUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name  string
		hosts []string
		want  string
	}{
		{"one host", []string{"helicon"}, "helicon"},
		{"the same host twice", []string{"helicon", "helicon"}, "helicon"},
		{"no hosts at all", nil, "local"},
		{"every leg local", []string{"", ""}, "local"},
		{"a blank host degrades to local", []string{"  "}, "local"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := MeasuredAcross(tc.hosts); got != tc.want {
				t.Errorf("MeasuredAcross(%v) = %q, want %q", tc.hosts, got, tc.want)
			}
		})
	}
}

// TestMeasuredAcrossNamesEveryMachine is c-4. Collapsing a split run to one
// host attributes the other leg's mutants to a machine that never saw them,
// and — worse — makes the record indistinguishable from a run that genuinely
// used a single host.
func TestMeasuredAcrossNamesEveryMachine(t *testing.T) {
	got := MeasuredAcross([]string{"alpha", "beta"})
	for _, want := range []string{"alpha", "beta"} {
		if !strings.Contains(got, want) {
			t.Errorf("measured_on = %q, does not name %q", got, want)
		}
	}
	if got == "alpha" || got == "beta" {
		t.Errorf("measured_on = %q — a split run reads as a single-host one", got)
	}
	// A local leg beside a remote one is a split too: half the numbers came
	// from this machine.
	mixed := MeasuredAcross([]string{"alpha", ""})
	if !strings.Contains(mixed, "alpha") || !strings.Contains(mixed, "local") {
		t.Errorf("a mixed run recorded %q, want both machines named", mixed)
	}
}

// TestMeasuredOnAdaptersWalksEveryAdapter: reading the first adapter and
// stopping is exactly how a run measured across two candidates records only
// one of them.
func TestMeasuredOnAdaptersWalksEveryAdapter(t *testing.T) {
	alpha := &remote.Target{Host: "alpha", Workdir: "/srv/dross"}
	beta := &remote.Target{Host: "beta", Workdir: "/srv/dross"}

	got := MeasuredOnAdapters([]mutation.Adapter{
		&mutation.Gremlins{Remote: alpha},
		&mutation.Stryker{Remote: beta},
	})
	if !strings.Contains(got, "alpha") || !strings.Contains(got, "beta") {
		t.Errorf("measured_on = %q, want both candidates named", got)
	}
}

// TestAdapterHostReadsEachAdapterType: one type switch, and it has to cover
// every adapter. A missing case reports "local" for a leg that ran on a host —
// silently, since "local" is a perfectly plausible answer.
func TestAdapterHostReadsEachAdapterType(t *testing.T) {
	target := &remote.Target{Host: "helicon", Workdir: "/srv/dross"}
	for _, tc := range []struct {
		name string
		a    mutation.Adapter
		want string
	}{
		{"gremlins remote", &mutation.Gremlins{Remote: target}, "helicon"},
		{"stryker remote", &mutation.Stryker{Remote: target}, "helicon"},
		{"stryker.net remote", &mutation.StrykerNet{Remote: target}, "helicon"},
		{"gremlins local", &mutation.Gremlins{}, ""},
		{"stryker local", &mutation.Stryker{}, ""},
		{"stryker.net local", &mutation.StrykerNet{}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := AdapterHost(tc.a); got != tc.want {
				t.Errorf("AdapterHost = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestEveryAdapterTypeIsCoveredByAdapterHost fails closed when an adapter is
// added: the type switch is the kind of code that goes stale in exactly the way
// nothing notices, because an unhandled adapter returns a plausible "".
func TestEveryAdapterTypeIsCoveredByAdapterHost(t *testing.T) {
	target := &remote.Target{Host: "helicon", Workdir: "/srv/dross"}
	// Every adapter the dispatch table knows about, carrying a host.
	all := []mutation.Adapter{
		&mutation.Gremlins{Remote: target},
		&mutation.Stryker{Remote: target},
		&mutation.StrykerNet{Remote: target},
	}
	// Cross-checked against allAdapters(), the list dispatch_surface_test.go
	// already forces every adapter into. A new adapter has to appear there to
	// pass those guards, and the length check then drags it in here too.
	if len(all) != len(allAdapters()) {
		t.Fatalf("%d adapter(s) exercised, %d exist — a new adapter has no provenance case",
			len(all), len(allAdapters()))
	}
	for _, a := range all {
		if AdapterHost(a) != "helicon" {
			t.Errorf("%s reports no host despite carrying one — its case is missing", a.Name())
		}
	}
}

// TestLegSummaryCarriesItsOwnHost: the run-level string says which machines
// were involved; only the leg says which of them produced THIS score. Without
// it a split run leaves the reader guessing which number belongs where, and the
// guess is what makes two runs comparable or not.
func TestLegSummaryCarriesItsOwnHost(t *testing.T) {
	tests := &Tests{
		Phase:      "p",
		MeasuredOn: MeasuredAcross([]string{"alpha", "beta"}),
		Languages: []LanguageRun{
			{
				Name: "go", Tool: "gremlins", MeasuredOn: "alpha",
				Mutation: &mutation.Report{Killed: 3, Survived: 1},
			},
			{
				Name: "typescript", Tool: "stryker", MeasuredOn: "beta",
				Mutation: &mutation.Report{Killed: 1, Survived: 1},
			},
		},
	}
	v := Skeleton(tests, []string{"c-1"})
	if len(v.Summary.Legs) != 2 {
		t.Fatalf("%d leg(s) recorded, want 2", len(v.Summary.Legs))
	}
	if v.Summary.Legs[0].MeasuredOn != "alpha" || v.Summary.Legs[1].MeasuredOn != "beta" {
		t.Errorf("legs measured on %q/%q, want alpha/beta — the split collapsed",
			v.Summary.Legs[0].MeasuredOn, v.Summary.Legs[1].MeasuredOn)
	}
	for _, want := range []string{"alpha", "beta"} {
		if !strings.Contains(v.Summary.MeasuredOn, want) {
			t.Errorf("the summary does not name %q: %q", want, v.Summary.MeasuredOn)
		}
	}
}

// TestAFailedLegStillRecordsItsHost: "which machine did this leg fail on" is
// the first question a transport error raises, and a leg with no provenance
// sends the reader to the wrong box.
func TestAFailedLegStillRecordsItsHost(t *testing.T) {
	v := Skeleton(&Tests{
		Phase: "p",
		Languages: []LanguageRun{
			{Name: "go", Tool: "gremlins", MeasuredOn: "alpha", Error: "ssh: connect: refused"},
		},
	}, []string{"c-1"})

	if len(v.Summary.Legs) != 1 {
		t.Fatalf("%d leg(s), want the failed one recorded", len(v.Summary.Legs))
	}
	if v.Summary.Legs[0].MeasuredOn != "alpha" {
		t.Errorf("a failed leg recorded %q, want alpha", v.Summary.Legs[0].MeasuredOn)
	}
}

// TestUnsplitRunKeepsItsLegProvenanceOmitted: a record written before per-leg
// provenance existed must round-trip unchanged, so the field is omitted rather
// than written empty.
func TestUnsplitRunKeepsItsLegProvenanceOmitted(t *testing.T) {
	v := Skeleton(&Tests{
		Phase:      "p",
		MeasuredOn: MeasuredOnHost("helicon"),
		Languages: []LanguageRun{
			{Name: "go", Tool: "gremlins", Mutation: &mutation.Report{Killed: 1}},
		},
	}, []string{"c-1"})

	if v.Summary.MeasuredOn != "helicon" {
		t.Errorf("the run-level provenance changed shape: %q", v.Summary.MeasuredOn)
	}
	if v.Summary.Legs[0].MeasuredOn != "" {
		t.Errorf("a leg with no recorded host wrote %q", v.Summary.Legs[0].MeasuredOn)
	}
}

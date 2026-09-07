package cmd

import (
	"reflect"
	"sort"
	"testing"

	"github.com/Rivil/dross/internal/pathfence"
)

// The carrier assertion — the load-bearing half of c-4.
//
// pathfence.Fields() declares, for each Consumed field, the CARRIER that holds
// the checked value. That declaration is a claim about code, and this file is
// where it is checked against the code, because this is the only place the
// symbols are visible: internal/cmd imports pathfence, so a pathfence test
// importing internal/cmd back would be a cycle, and the carriers are unexported
// here besides.
//
// Reflection over a function VALUE and a struct TYPE needs no go/types: the
// compiler has already resolved both by the time this test runs, so a carrier
// that reverted to a string fails on a type identity rather than on a guess.

// carrierType returns the type a named carrier must be, or the zero Type when
// the name has no binding. Every Consumed entry in the registry must appear
// here — TestEveryConsumedCarrierIsBound is what makes that true.
func carrierType(name string) (got, want reflect.Type, ok bool) {
	contained := reflect.TypeOf(pathfence.Contained{})
	switch name {
	case "containScope":
		// The conversion boundary: its first return is what mutationCandidates
		// takes, so a revert to []string here is what would make
		// verify.Scope.Files' and changes.TaskRecord.Files' Consumed
		// disposition a promise about the code rather than a fact about it.
		return reflect.TypeOf(containScope).Out(0), reflect.SliceOf(contained), true
	case "redProofPin.Doc":
		// The field an AST dataflow scan could not follow: its readers copy it
		// into a cmd-local plan struct before anything opens it.
		f, found := reflect.TypeOf(redProofPin{}).FieldByName("Doc")
		if !found {
			return nil, contained, true
		}
		return f.Type, contained, true
	}
	return nil, nil, false
}

// TestDeclaredCarriersAreContained checks each binding.
func TestDeclaredCarriersAreContained(t *testing.T) {
	for _, name := range boundCarriers() {
		got, want, _ := carrierType(name)
		if got == nil {
			t.Errorf("carrier %q names a symbol that no longer exists", name)
			continue
		}
		if got != want {
			t.Errorf("carrier %q is %s, want %s — the registry declares this field "+
				"routed through the containment check, and a %s carries no such guarantee",
				name, got, want, got)
		}
	}
}

// TestEveryConsumedCarrierIsBound is the vacuity guard, and it iterates the
// REGISTRY rather than the binding table on purpose: a new Consumed entry with
// no binding must fail here rather than pass unmeasured, which is exactly what
// checking the table against itself would do.
func TestEveryConsumedCarrierIsBound(t *testing.T) {
	seen := 0
	for _, f := range pathfence.Fields() {
		if f.Consumed == nil {
			continue
		}
		seen++
		if _, _, ok := carrierType(f.Consumed.Carrier); !ok {
			t.Errorf("%s declares carrier %q, which has no binding in carrierType — "+
				"a Consumed entry cannot claim the check without an assertion that it holds",
				f.Name(), f.Consumed.Carrier)
		}
	}
	if seen == 0 {
		t.Fatal("the registry declares no Consumed fields at all — every assertion in this file is vacuous")
	}
}

// boundCarriers lists the distinct carrier names the registry declares, sorted
// so failures report in a stable order.
func boundCarriers() []string {
	seen := map[string]bool{}
	for _, f := range pathfence.Fields() {
		if f.Consumed != nil {
			seen[f.Consumed.Carrier] = true
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// TestRegistryIsWellFormed runs pathfence's own validator over the real
// registry. Validate is unit-tested against synthetic bad entries inside
// pathfence; this is the one call that judges the live data.
func TestRegistryIsWellFormed(t *testing.T) {
	for _, err := range pathfence.Validate(pathfence.Fields()) {
		t.Errorf("registry: %v", err)
	}
}

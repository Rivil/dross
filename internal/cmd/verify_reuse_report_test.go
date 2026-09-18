package cmd

import (
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/mutation"
)

// --reuse-report flips the stryker adapter and only the stryker adapter: the
// gremlins leg beside it still runs, which is what the flag's help text says.
func TestApplyReuseReportFlipsOnlyStryker(t *testing.T) {
	stryker := &mutation.Stryker{}
	adapters := []mutation.Adapter{stryker, &mutation.Gremlins{}, &mutation.StrykerNet{}}
	if err := applyReuseReport(adapters, false, false); err != nil {
		t.Fatalf("applyReuseReport: %v", err)
	}
	if !stryker.ReuseReport {
		t.Error("the stryker adapter must have ReuseReport set")
	}
}

// The two flags that make a reused report meaningless refuse, each naming the
// conflict, and a project with no stryker adapter has nothing to reuse.
func TestApplyReuseReportRefusesTheMeaninglessCombinations(t *testing.T) {
	cases := []struct {
		name         string
		adapters     []mutation.Adapter
		skip, detach bool
		want         string
	}{
		{"skip-mutation", []mutation.Adapter{&mutation.Stryker{}}, true, false, "--skip-mutation"},
		{"detach", []mutation.Adapter{&mutation.Stryker{}}, false, true, "--detach"},
		{"no stryker", []mutation.Adapter{&mutation.Gremlins{}}, false, false, "no stryker adapter"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := applyReuseReport(tc.adapters, tc.skip, tc.detach)
			if err == nil {
				t.Fatalf("expected a refusal naming %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("refusal = %v, want it to name %q", err, tc.want)
			}
		})
	}
}

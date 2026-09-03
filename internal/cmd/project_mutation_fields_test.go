package cmd

import (
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/project"
)

// /dross-options §13 tells the user to configure three mutation keys. All three
// returned "unknown field" from `dross project get/set` — the prompt documented
// a section that could only be reached by hand-editing project.toml, which the
// prompts' own don't-bypass-the-CLI rule forbids. These pin the wiring.
func TestMutationFieldsRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		key   string
		write string
		want  string
	}{
		{"mutation.adapters", "gremlins,stryker", "gremlins,stryker"},
		{"mutation.adapters", " gremlins , stryker ", "gremlins,stryker"},
		{"mutation.gremlins.timeout_coefficient", "4", "4"},
		{"mutation.stryker.workdir", "web", "web"},
	} {
		t.Run(tc.key+"="+tc.write, func(t *testing.T) {
			p := &project.Project{}
			if err := writeDotted(p, tc.key, tc.write); err != nil {
				t.Fatalf("writeDotted(%s, %q): %v", tc.key, tc.write, err)
			}
			got, ok := readDotted(p, tc.key)
			if !ok {
				t.Fatalf("readDotted(%s) reports unknown field after a successful write", tc.key)
			}
			if got != tc.want {
				t.Errorf("readDotted(%s) = %q; want %q", tc.key, got, tc.want)
			}
		})
	}
}

// The timeout coefficient is the first numeric key on this surface. A
// non-numeric value must be refused, not coerced to 0 — a silent 0 would
// configure gremlins' timeout multiplier to something the user never asked
// for, discoverable only from a mutation run.
func TestMutationTimeoutCoefficientRefusesNonNumeric(t *testing.T) {
	p := &project.Project{}
	p.Mutation.Gremlins.TimeoutCoefficient = 3
	err := writeDotted(p, "mutation.gremlins.timeout_coefficient", "banana")
	if err == nil {
		t.Fatal("expected a refusal for a non-numeric timeout coefficient, got nil")
	}
	if !strings.Contains(err.Error(), "whole number") {
		t.Errorf("refusal should say a whole number is expected; got: %v", err)
	}
	if p.Mutation.Gremlins.TimeoutCoefficient != 3 {
		t.Errorf("a refused write must leave the value untouched; got %d, want 3",
			p.Mutation.Gremlins.TimeoutCoefficient)
	}
}

// Empty clears, matching how every other optional key unsets.
func TestMutationTimeoutCoefficientEmptyClears(t *testing.T) {
	p := &project.Project{}
	p.Mutation.Gremlins.TimeoutCoefficient = 7
	if err := writeDotted(p, "mutation.gremlins.timeout_coefficient", ""); err != nil {
		t.Fatalf("writeDotted empty: %v", err)
	}
	if p.Mutation.Gremlins.TimeoutCoefficient != 0 {
		t.Errorf("empty should clear the coefficient; got %d", p.Mutation.Gremlins.TimeoutCoefficient)
	}
}

// The adapters allowlist is a slice, and reads back as csv exactly like
// stack.languages — the precedent this arm mirrors.
func TestMutationAdaptersEmptyReadsBackEmpty(t *testing.T) {
	p := &project.Project{}
	got, ok := readDotted(p, "mutation.adapters")
	if !ok {
		t.Fatal("mutation.adapters should be a known field even when unset")
	}
	if got != "" {
		t.Errorf("unset mutation.adapters = %q; want empty", got)
	}
}

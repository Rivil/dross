package ceiling

import "testing"

// TestClassifyExecutesEveryCase drives all three arms, so a coverage profile
// over this package MUST show the case-condition lines executed. If this test
// stops exercising them, the ceiling proof fails rather than silently proving
// nothing.
func TestClassifyExecutesEveryCase(t *testing.T) {
	for _, tc := range []struct {
		in   rune
		want string
	}{
		{'q', "lower"},
		{'7', "digit"},
		{'Z', "other"},
	} {
		if got := Classify(tc.in); got != tc.want {
			t.Errorf("Classify(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestDescribeConcatenates exercises the string-concatenation line.
func TestDescribeConcatenates(t *testing.T) {
	if got, want := Describe("ada"), "name: ada (hello, world)"; got != want {
		t.Errorf("Describe = %q, want %q", got, want)
	}
}

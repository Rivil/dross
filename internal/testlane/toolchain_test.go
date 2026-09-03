package testlane

import (
	"reflect"
	"testing"
)

// TestToolchainDerivesCommandTokenBeforePrepareToken pins the derivation and
// its ORDER. Order is asserted rather than membership because it is the order
// missing tools are reported in, and because deriving from the command alone —
// the obvious under-implementation — still contains `go`.
func TestToolchainDerivesCommandTokenBeforePrepareToken(t *testing.T) {
	got := Toolchain("go test ./...", "make build", nil)
	if !reflect.DeepEqual(got, []string{"go", "make"}) {
		t.Errorf("Toolchain = %v, want exactly [go make] — command token first, prepare second", got)
	}
}

// TestToolchainOverrideReplacesRatherThanAppends pins the locked
// toolchain_source escape hatch. An appending implementation returns
// [mise go], which keeps probing the very token the override exists to say is
// not the binary — so the length is asserted, not just the presence of mise.
func TestToolchainOverrideReplacesRatherThanAppends(t *testing.T) {
	got := Toolchain("go test ./...", "make build", []string{"mise"})
	if !reflect.DeepEqual(got, []string{"mise"}) {
		t.Errorf("Toolchain with override = %v, want exactly [mise] — an override replaces the derived list, it does not extend it", got)
	}
}

// TestToolchainDedupesARepeatedTool: `go build` prepare beside a `go test`
// command is the common lane shape, and it must cost ONE `command -v` on a
// host reached over ssh, not two.
func TestToolchainDedupesARepeatedTool(t *testing.T) {
	got := Toolchain("go test ./...", "go build ./...", nil)
	if !reflect.DeepEqual(got, []string{"go"}) {
		t.Errorf("Toolchain = %v, want exactly [go] — a repeated tool is probed once", got)
	}
}

// TestToolchainDropsBlankTokens: a blank token would be probed as
// `command -v ""`, which no host satisfies, so a lane with an empty prepare
// would report its toolchain missing and fall back on every run forever.
func TestToolchainDropsBlankTokens(t *testing.T) {
	got := Toolchain("  ", "", nil)
	if len(got) != 0 {
		t.Errorf("Toolchain(blank) = %v, want no tools — a blank token probes the empty string and never resolves", got)
	}
	if withPrepare := Toolchain("go test", "   ", nil); !reflect.DeepEqual(withPrepare, []string{"go"}) {
		t.Errorf("Toolchain with a blank prepare = %v, want exactly [go]", withPrepare)
	}
}

// TestToolchainTakesTheFirstTokenVerbatim pins the locked first-token rule
// against a quiet widening. `FOO=1 go test` derives `FOO=1`, and the fix is the
// user's --toolchain override surfaced by doctor — not a shell parser growing
// inside a locality check.
func TestToolchainTakesTheFirstTokenVerbatim(t *testing.T) {
	got := Toolchain("FOO=1 go test ./...", "", nil)
	if !reflect.DeepEqual(got, []string{"FOO=1"}) {
		t.Errorf("Toolchain = %v, want exactly [FOO=1] — the rule is first-token, and stripping the prefix here would hide the case doctor exists to name", got)
	}
}

// TestToolchainOverrideOfBlanksFallsBackToDerivation: an override that cleans
// down to nothing is not an override. Treating it as one would return no tools
// at all, which reads to every caller as "this lane needs nothing" — a lane
// that would then run anywhere, including a host with no toolchain.
func TestToolchainOverrideOfBlanksFallsBackToDerivation(t *testing.T) {
	got := Toolchain("go test ./...", "", []string{"", "  "})
	if !reflect.DeepEqual(got, []string{"go"}) {
		t.Errorf("Toolchain with a blank override = %v, want the derived [go]", got)
	}
}

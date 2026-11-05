package ctxnudge

import (
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/gatestate"
)

// TestBand pins the cadence (locked nudge_cadence): band 1 at the threshold
// itself (c-2 "at or above"), one more per further 50k, nothing when off.
func TestBand(t *testing.T) {
	for _, c := range []struct {
		tokens, threshold, want int64
	}{
		{149_999, 150_000, 0},
		{150_000, 150_000, 1},
		{199_999, 150_000, 1},
		{200_000, 150_000, 2},
		{260_000, 150_000, 3},
		{10_000_000, 0, 0},
		{90_000, 80_000, 1},
	} {
		if got := Band(c.tokens, c.threshold); got != c.want {
			t.Errorf("Band(%d, %d) = %d, want %d", c.tokens, c.threshold, got, c.want)
		}
	}
}

// TestJumpNudgesOnce: one fire that jumps from 140k to 260k claims band 3 —
// one nudge, not one per band skipped — and later fires inside bands 1-3 stay
// silent.
func TestJumpNudgesOnce(t *testing.T) {
	root := t.TempDir()
	if b := Band(140_000, 150_000); b != 0 {
		t.Fatalf("140k is band %d, want 0", b)
	}
	b := Band(260_000, 150_000)
	won, err := gatestate.ClaimNudge(root, "s", b)
	if err != nil || !won {
		t.Fatalf("claim band %d: (%v, %v)", b, won, err)
	}
	for _, tokens := range []int64{150_000, 210_000, 260_000, 299_999} {
		if !gatestate.NudgeClaimed(root, "s", Band(tokens, 150_000)) {
			t.Errorf("a fire at %d would nudge again after the jump to 260k", tokens)
		}
	}
	if gatestate.NudgeClaimed(root, "s", Band(300_000, 150_000)) {
		t.Error("300k (band 4) reads as already nudged")
	}
}

// TestLineCarriesLockedParts: every part the nudge_content decision locks, on
// one line that opens with Marker.
func TestLineCarriesLockedParts(t *testing.T) {
	const reentry = "/dross-execute — run the next task"
	line := Line(151_409, 150_000, reentry)
	if !strings.HasPrefix(line, Marker) {
		t.Errorf("line does not open with %q: %q", Marker, line)
	}
	for _, part := range []string{
		"151k / 150k",
		"checkpoint at your next durable boundary → /clear",
		reentry,
		"mid-thought? /dross-pause first",
	} {
		if !strings.Contains(line, part) {
			t.Errorf("line lacks %q: %q", part, line)
		}
	}
	if multi := Line(151_409, 150_000, "/dross-debug x\n/dross-execute\r\nnext"); strings.ContainsAny(multi, "\r\n") {
		t.Errorf("a multi-line reentry broke the one-line nudge: %q", multi)
	}
}

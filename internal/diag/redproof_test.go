package diag

import (
	"errors"
	"strings"
	"testing"
)

// TestPinLinesMatrix pins every arm's level and wording: no arm may swap
// Issue for Warn, and a clean pin earns exactly one OK line.
func TestPinLinesMatrix(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	base := RedProofPin{Phase: "auth", SHA: sha, Doc: "docs/auth/RUN.md", Reach: Reachable, Why: "reached from origin/main", DocSHA: sha, RepointHint: "repoint it to auth's fork point abc1234 — `dross phase red-proof repoint auth --apply`"}

	t.Run("all clear is exactly one OK line", func(t *testing.T) {
		var lines []Line
		silent(t, func() { lines = PinLines(base) })
		if len(lines) != 1 || lines[0].Level != OK {
			t.Fatalf("clean pin = %+v", lines)
		}
		if !strings.Contains(lines[0].Text, "auth: docs/auth/RUN.md pins 0123456, reached from origin/main") {
			t.Errorf("OK wording = %q", lines[0].Text)
		}
	})
	t.Run("unreachable is one Issue with the hint", func(t *testing.T) {
		pin := base
		pin.Reach, pin.Why = Unreachable, "no origin ref contains it"
		lines := PinLines(pin)
		if len(lines) != 1 || lines[0].Level != Issue {
			t.Fatalf("unreachable pin = %+v", lines)
		}
		if !strings.Contains(lines[0].Text, "unreachable") || !strings.Contains(lines[0].Text, pin.RepointHint) {
			t.Errorf("unreachable wording = %q", lines[0].Text)
		}
	})
	t.Run("indeterminate is one Warn", func(t *testing.T) {
		pin := base
		pin.Reach, pin.Why = Indeterminate, "shallow clone"
		lines := PinLines(pin)
		if len(lines) != 1 || lines[0].Level != Warn || !strings.Contains(lines[0].Text, "cannot determine") {
			t.Fatalf("indeterminate pin = %+v", lines)
		}
	})
	t.Run("reachability error is one Issue", func(t *testing.T) {
		pin := base
		pin.ReachErr = errors.New("git exploded")
		lines := PinLines(pin)
		if len(lines) != 1 || lines[0].Level != Issue || !strings.Contains(lines[0].Text, "cannot check the pin") {
			t.Fatalf("errored pin = %+v", lines)
		}
	})
	t.Run("unreadable doc is an Issue", func(t *testing.T) {
		pin := base
		pin.DocErr = errors.New("open: no such file")
		lines := PinLines(pin)
		if len(lines) != 1 || lines[0].Level != Issue || !strings.Contains(lines[0].Text, "cannot be read") {
			t.Fatalf("unreadable doc = %+v", lines)
		}
	})
	t.Run("doc without a base commit is an Issue", func(t *testing.T) {
		pin := base
		pin.DocSHA = ""
		lines := PinLines(pin)
		if len(lines) != 1 || lines[0].Level != Issue || !strings.Contains(lines[0].Text, "carries no `base commit:`") {
			t.Fatalf("missing base commit = %+v", lines)
		}
	})
	t.Run("an abbreviated doc SHA agrees", func(t *testing.T) {
		pin := base
		pin.DocSHA = sha[:7]
		lines := PinLines(pin)
		if len(lines) != 1 || lines[0].Level != OK {
			t.Fatalf("abbreviated agreeing doc = %+v", lines)
		}
	})
	t.Run("a disagreeing doc is an Issue", func(t *testing.T) {
		pin := base
		pin.DocSHA = "fedcba9"
		lines := PinLines(pin)
		if len(lines) != 1 || lines[0].Level != Issue || !strings.Contains(lines[0].Text, "the prose and the record disagree") {
			t.Fatalf("disagreeing doc = %+v", lines)
		}
	})
	t.Run("unreachable AND disagreeing yields two Issues", func(t *testing.T) {
		pin := base
		pin.Reach, pin.DocSHA = Unreachable, "fedcba9"
		lines := PinLines(pin)
		if len(lines) != 2 || lines[0].Level != Issue || lines[1].Level != Issue {
			t.Fatalf("two-fault pin = %+v", lines)
		}
	})
}

// TestRedProofDiscoveryFailureSuppressesPins: a discovery error is one Issue
// whose text never says "which cannot be read" — that wording belongs to the
// per-pin unreadable-doc arm, and an operator has to be able to tell a corrupt
// path from a missing file — and no pin is rendered; no pins means no section.
func TestRedProofDiscoveryFailureSuppressesPins(t *testing.T) {
	lines, present := RedProof([]RedProofPin{{Phase: "x", Reach: Unreachable}}, errors.New("doc escapes the repo"))
	if !present || len(lines) != 1 || lines[0].Level != Issue {
		t.Fatalf("discovery failure = %+v present=%v", lines, present)
	}
	if strings.Contains(lines[0].Text, "which cannot be read") || !strings.Contains(lines[0].Text, "doc escapes the repo") {
		t.Errorf("discovery failure wording = %q", lines[0].Text)
	}
	if lines, present := RedProof(nil, nil); present || lines != nil {
		t.Errorf("no pins must render no section: %+v %v", lines, present)
	}
	two := []RedProofPin{{Phase: "a", SHA: "1", Doc: "d", Reach: Reachable, DocSHA: "1"}, {Phase: "b", SHA: "2", Doc: "d", Reach: Reachable, DocSHA: "2"}}
	if lines, present := RedProof(two, nil); !present || len(lines) != 2 {
		t.Errorf("two clean pins = %+v", lines)
	}
}

// TestSameCommitSHA: containment either way, case-insensitive, never on empty.
func TestSameCommitSHA(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"abc123", "ABC", true},
		{"abc", "abc123def", true},
		{"abc123", "abd", false},
		{"", "abc", false},
		{"abc", "", false},
		{"  abc123 ", "abc123", true},
	}
	for _, c := range cases {
		if got := SameCommitSHA(c.a, c.b); got != c.want {
			t.Errorf("SameCommitSHA(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
	if Short("0123456789") != "0123456" || Short("abc") != "abc" {
		t.Error("Short did not abbreviate to seven")
	}
}

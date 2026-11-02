package memadvise

import (
	"testing"
	"time"
)

var hookToday = time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)

var noPhases map[string]bool

func phases(known ...string) map[string]bool {
	set := map[string]bool{}
	for _, k := range known {
		set[k] = true
	}
	return set
}

func TestExpiryBoundaryIsStrict(t *testing.T) {
	if got, ok := Expired("hold the flag until 2026-10-29; then drop it", hookToday, noPhases); !ok || got != "hold the flag; then drop it" {
		t.Errorf("yesterday's deadline: (%q, %v), want reworded and expired", got, ok)
	}
	if got, ok := Expired("hold the flag until 2026-10-30; then drop it", hookToday, noPhases); ok {
		t.Errorf("today's deadline expired: %q", got)
	}
	if _, ok := Expired("x; due 2026-11-01", hookToday, noPhases); ok {
		t.Error("a future deadline expired")
	}
	for _, word := range []string{"by", "before", "due", "expires", "till", "deadline", "Until", "due:"} {
		if _, ok := Expired("x; "+word+" 2026-01-01", hookToday, noPhases); !ok {
			t.Errorf("%q 2026-01-01 is not a time-bound clause", word)
		}
	}
	if _, ok := Expired("x; until 2026-13-45", hookToday, noPhases); ok {
		t.Error("an invalid date expired")
	}
}

func TestHistoricalDateIsNotTimeBound(t *testing.T) {
	for _, hook := range []string{
		"RESOLVED 2026-09-19 (03983fe): install swaps by rename",
		"merged — shipped 2026-10-04",
		"released 2026-01-01",
		"2026-09-23: ~600s = go test's 10m default",
	} {
		if got, ok := Expired(hook, hookToday, noPhases); ok {
			t.Errorf("%q expired as %q", hook, got)
		}
	}
}

func TestNextPhaseClause(t *testing.T) {
	hook := "context-boundaries SHIPPED — complete; next: foo"
	if _, ok := Expired(hook, hookToday, noPhases); ok {
		t.Error("next: foo expired though foo is not scaffolded")
	}
	got, ok := Expired(hook, hookToday, phases("foo"))
	if !ok || got != "context-boundaries SHIPPED — complete" {
		t.Errorf("known next phase: (%q, %v)", got, ok)
	}
	if got, ok := Expired("complete; Next: Foo", hookToday, phases("foo")); !ok || got != "complete" {
		t.Errorf("Next: Foo with foo scaffolded: (%q, %v)", got, ok)
	}
	if _, ok := Expired("done; next phase foo-bar", hookToday, phases("foo")); ok {
		t.Error("next phase foo-bar matched the slug foo")
	}
	got2 := NextSlugs("a; next: foo — b, next /dross-plan-review Bar-Baz")
	if len(got2) != 2 || got2[0] != "foo" || got2[1] != "bar-baz" {
		t.Errorf("NextSlugs = %v, want [foo bar-baz]", got2)
	}
}

func TestNextSlashCommandForm(t *testing.T) {
	hook := "2026-10-30 #193 merged, complete; next /dross-spec memory-prune-advisor; kill-switch leg still manual"
	got, ok := Expired(hook, hookToday, phases("memory-prune-advisor"))
	want := "2026-10-30 #193 merged, complete; kill-switch leg still manual"
	if !ok || got != want {
		t.Errorf("Expired = (%q, %v), want (%q, true)", got, ok, want)
	}
}

func TestRewordRemovesOnlyClause(t *testing.T) {
	cases := map[string]string{
		"X — done; next phase foo":        "X — done",
		"next phase foo; X — done":        "X — done",
		"X — done — next: foo":            "X — done",
		"X (next: foo) still open":        "X still open",
		"X, until 2026-01-01, then Y":     "X, then Y",
		"keep it · until 2026-01-01 · ok": "keep it · ok",
		"X (until 2026-01-01; note) Y":    "X (note) Y",
		"X ( next: foo, note) Y":          "X (note) Y",
	}
	for in, want := range cases {
		got, ok := Expired(in, hookToday, phases("foo"))
		if !ok || got != want {
			t.Errorf("Expired(%q) = (%q, %v), want %q", in, got, ok, want)
		}
	}
}

func TestRewordNeverEmpty(t *testing.T) {
	for _, hook := range []string{"next: foo", "until 2026-01-01", "  next phase foo  "} {
		got, ok := Expired(hook, hookToday, phases("foo"))
		if got == "" {
			t.Errorf("Expired(%q) reworded to empty", hook)
		}
		if ok {
			t.Errorf("Expired(%q) = (%q, true): a clause-only hook can't be reworded", hook, got)
		}
	}
}

func TestExpiredRemovesEarliestClause(t *testing.T) {
	// The date clause is found first but sits later in the hook; the next
	// clause before it is the one removed.
	got, ok := Expired("next: foo; X; until 2026-01-01", hookToday, phases("foo"))
	if want := "X; until 2026-01-01"; !ok || got != want {
		t.Errorf("Expired = (%q, %v), want (%q, true)", got, ok, want)
	}
}

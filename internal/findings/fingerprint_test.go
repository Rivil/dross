package findings

import "testing"

// TestFingerprintStableAcrossLineAndPathDrift pins the locked decision that
// identity ignores line numbers (the signature carries none) and that
// equivalent path spellings collapse to one key. If a line ever leaks into the
// hash, or path normalization regresses so ./ , // , or trailing-slash forms
// diverge, this fails.
func TestFingerprintStableAcrossLineAndPathDrift(t *testing.T) {
	want := Fingerprint("cmd-injection", "internal/x.go", "shell-out")
	variants := []string{
		"./internal/x.go",
		"internal//x.go",
		"internal/x.go/",
		"internal/./x.go",
	}
	for _, f := range variants {
		if got := Fingerprint("cmd-injection", f, "shell-out"); got != want {
			t.Errorf("Fingerprint(... %q ...) = %s, want %s — path spelling must not change identity", f, got, want)
		}
	}
}

// TestFingerprintDistinctTitles guards against an over-broad key: two findings
// that differ only in title must not collide onto one tracked item.
func TestFingerprintDistinctTitles(t *testing.T) {
	a := Fingerprint("perf", "internal/x.go", "N+1 query in loader")
	b := Fingerprint("perf", "internal/x.go", "unbounded slice growth")
	if a == b {
		t.Fatalf("distinct titles collided to one fingerprint %s — identity is too coarse", a)
	}
}

// TestFingerprintEmptyInputs proves an empty file or title neither panics nor
// produces a non-deterministic key, and that empties stay distinguishable from
// real values (so a malformed finding can't silently alias a real one).
func TestFingerprintEmptyInputs(t *testing.T) {
	if Fingerprint("", "", "") != Fingerprint("", "", "") {
		t.Fatal("Fingerprint is non-deterministic on empty inputs")
	}
	emptyFile := Fingerprint("class", "", "title")
	realFile := Fingerprint("class", "internal/x.go", "title")
	if emptyFile == realFile {
		t.Error("empty file aliased a real file path")
	}
	emptyTitle := Fingerprint("class", "internal/x.go", "")
	if emptyTitle == realFile {
		t.Error("empty title aliased a real title")
	}
}

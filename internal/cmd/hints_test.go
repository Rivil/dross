package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The table covered four mis-reaches; `dross task edit --files` left it when
// that flag was added and the invocation started working.
func TestCuratedHintCoversTheKnownMisreaches(t *testing.T) {
	cases := []struct {
		cmdPath, token string
		wantIn         string
	}{
		// The semantic remap edit distance can never produce.
		{"dross task", "done", "dross task status <phase-id> <task-id> done"},
		{"dross phase create", "--title", "dross phase create"},
		{"dross security run", "--new", "dross security run"},
	}
	for _, c := range cases {
		h, ok := CuratedHint(c.cmdPath, c.token)
		if !ok {
			t.Errorf("CuratedHint(%q, %q) not found", c.cmdPath, c.token)
			continue
		}
		if !strings.Contains(h.Fix, c.wantIn) {
			t.Errorf("CuratedHint(%q, %q).Fix = %q, want it to contain %q", c.cmdPath, c.token, h.Fix, c.wantIn)
		}
	}
}

// TestNoHintForTaskEditFiles: `dross task edit --files` is a working
// invocation now, so a hint telling the user to reach for `task add` instead
// would send them somewhere worse than where they already are.
func TestNoHintForTaskEditFiles(t *testing.T) {
	if h, ok := CuratedHint("dross task edit", "--files"); ok {
		t.Errorf("--files is a real flag on `task edit`; stale hint still present: %+v", h)
	}
}

func TestCuratedHintMissIsReportedSoFallbackIsReachable(t *testing.T) {
	if h, ok := CuratedHint("dross task", "wibble"); ok {
		t.Errorf("unexpected entry for an unknown mis-reach: %+v", h)
	}
}

func TestCuratedHintLookupIsNormalised(t *testing.T) {
	want, _ := CuratedHint("dross task", "done")
	got, ok := CuratedHint("  dross Task ", "Done")
	if !ok {
		t.Fatal("`dross Task Done` missed the entry — lookup is not normalised")
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("normalised lookup returned a different entry: %+v vs %+v", got, want)
	}
}

func TestNearest(t *testing.T) {
	cands := []string{"status", "show", "set"}
	if got := Nearest("stauts", cands); !reflect.DeepEqual(got, []string{"status"}) {
		t.Errorf("Nearest(stauts) = %v, want [status]", got)
	}
	// A far-off typo gets no fabricated suggestion.
	if got := Nearest("zzzzzz", cands); len(got) != 0 {
		t.Errorf("Nearest(zzzzzz) = %v, want none", got)
	}
	if got := Nearest("", cands); len(got) != 0 {
		t.Errorf("Nearest(\"\") = %v, want none", got)
	}
	if got := Nearest("Shwo", cands); !reflect.DeepEqual(got, []string{"show"}) {
		t.Errorf("Nearest(Shwo) = %v, want [show] (case-insensitive)", got)
	}
}

// TestPromptsTeachNoBrokenInvocation is the corpus guard: dross's own prompts
// must not advertise an invocation its hint table classifies as broken. It
// fails on assets/prompts/secure.md:42 (`dross security run --new`) until that
// line is fixed.
func TestPromptsTeachNoBrokenInvocation(t *testing.T) {
	dir := filepath.Join("..", "..", "assets", "prompts")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		checked++
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if path, tok, found := LineTeachesMisreach(line); found {
				fix, _ := CuratedHint(path, tok)
				t.Errorf("%s:%d teaches the broken invocation `%s %s` — use `%s`\n  %s",
					e.Name(), i+1, path, tok, fix.Fix, strings.TrimSpace(line))
			}
		}
	}
	if checked == 0 {
		t.Fatal("scanned zero prompts — the path is wrong and this guard is vacuous")
	}
}

func TestLineTeachesMisreachIgnoresTheWorkingForm(t *testing.T) {
	// The correct invocation contains both "dross task" and "done"; only
	// the adjacent form is a mis-reach.
	if _, _, found := LineTeachesMisreach("dross task status <phase-id> <task-id> done"); found {
		t.Error("the working invocation was classified as broken")
	}
	if _, _, found := LineTeachesMisreach("run `dross task done t-1` to finish"); !found {
		t.Error("the adjacent mis-reach was not detected")
	}
	// Word-bounded: --new must not match inside a longer flag.
	if _, _, found := LineTeachesMisreach("dross security run --newest"); found {
		t.Error("--new matched inside --newest")
	}
}

// TestCuratedMisreachPathsAreDistinct is the evidence behind hints.go:133's
// acceptance. CuratedMisreaches sorts by command path and falls back to the
// token only when two entries SHARE a path — and no two curated entries do, so
// the tie-break never runs. It is unreachable given the data, not untested.
//
// Pinning the premise makes the acceptance falsifiable: add a second entry
// under an existing command path and this test fails, at which point the
// tie-break is reachable and owes a real ordering test instead.
func TestCuratedMisreachPathsAreDistinct(t *testing.T) {
	misreaches := CuratedMisreaches()
	if len(misreaches) < 2 {
		t.Fatalf("the curated table has %d entries — the ordering question is vacuous", len(misreaches))
	}

	seen := map[string]string{}
	for _, mr := range misreaches {
		path, token := mr[0], mr[1]
		if prev, dup := seen[path]; dup {
			t.Errorf("two curated entries share the command path %q (tokens %q and %q) — "+
				"the sort tie-break is now reachable and needs a real ordering test", path, prev, token)
		}
		seen[path] = token
	}

	// And the primary ordering IS exercised: paths come back ascending.
	for i := 1; i < len(misreaches); i++ {
		if misreaches[i-1][0] > misreaches[i][0] {
			t.Errorf("curated misreaches are not sorted by command path at %d: %q then %q",
				i, misreaches[i-1][0], misreaches[i][0])
		}
	}
}

package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCuratedHintCoversTheFourMisreaches(t *testing.T) {
	cases := []struct {
		cmdPath, token string
		wantIn         string
	}{
		// The semantic remap edit distance can never produce.
		{"dross task", "done", "dross task status <phase-id> <task-id> done"},
		{"dross phase create", "--title", "dross phase create"},
		{"dross task edit", "--files", "dross task add"},
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

package memadvise

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// words returns "<prefix>0 <prefix>1 … <prefix>n-1": distinct words, so every
// shingle over them is distinct and overlap is exact arithmetic.
func words(prefix string, from, to int) string {
	var out []string
	for i := from; i < to; i++ {
		out = append(out, fmt.Sprintf("%s%d", prefix, i))
	}
	return strings.Join(out, " ")
}

func TestDupThresholdBoundary(t *testing.T) {
	body := words("w", 0, 107) // 100 shingles
	half := NewDocs([]string{"docs/half.md"}, map[string]string{
		"docs/half.md": words("w", 0, 57), // shingles 0..49: exactly 50
	})
	if doc, ok := half.Duplicates(body); !ok || doc != "docs/half.md" {
		t.Errorf("50%% overlap: Duplicates = (%q, %v), want a duplicate", doc, ok)
	}
	under := NewDocs([]string{"docs/under.md"}, map[string]string{
		"docs/under.md": words("w", 0, 56), // 49 shingles
	})
	if doc, ok := under.Duplicates(body); ok {
		t.Errorf("49%% overlap: Duplicates = %q, want none", doc)
	}
}

func TestDupNeedsOneDoc(t *testing.T) {
	body := words("w", 0, 107)
	docs := NewDocs([]string{"docs/a.md", "docs/b.md"}, map[string]string{
		"docs/a.md": words("w", 0, 37),  // shingles 0..29
		"docs/b.md": words("w", 50, 87), // shingles 50..79
	})
	if doc, ok := docs.Duplicates(body); ok {
		t.Errorf("30%% + 30%% across two docs counted as a duplicate of %q", doc)
	}
}

func TestDupSurvivesReorder(t *testing.T) {
	a, b, c := words("a", 0, 20), words("b", 0, 20), words("c", 0, 20)
	body := a + "\n\n" + b + "\n\n" + c
	// Reordered, re-cased and re-punctuated: normalisation and shingles
	// within each paragraph still carry it over the threshold.
	doc := strings.ToUpper(c) + ".\n\n" + a + "!\n\n- " + strings.ReplaceAll(b, " ", ",  ")
	docs := NewDocs([]string{"README.md"}, map[string]string{"README.md": doc})
	if got, ok := docs.Duplicates(body); !ok || got != "README.md" {
		t.Errorf("reordered restatement: Duplicates = (%q, %v)", got, ok)
	}
}

func TestDupNamesDoc(t *testing.T) {
	body := words("w", 0, 30)
	docs := NewDocs([]string{"CLAUDE.md", "docs/guide/setup.md"}, map[string]string{
		"CLAUDE.md":           words("x", 0, 30),
		"docs/guide/setup.md": "Intro.\n\n" + body + "\n\nOutro.",
	})
	if got, ok := docs.Duplicates(body); !ok || got != "docs/guide/setup.md" {
		t.Errorf("Duplicates = (%q, %v), want docs/guide/setup.md", got, ok)
	}
}

func TestDupShortBody(t *testing.T) {
	short := "only seven words in this body here"
	docs := NewDocs([]string{"README.md"}, map[string]string{"README.md": short})
	if got, ok := docs.Duplicates(short); ok {
		t.Errorf("a body under 8 words matched %q", got)
	}
	if got, ok := docs.Duplicates(""); ok {
		t.Errorf("an empty body matched %q", got)
	}
	var none *Docs
	if _, ok := none.Duplicates(words("w", 0, 20)); ok {
		t.Error("a nil Docs matched")
	}
}

func TestDocCandidates(t *testing.T) {
	got := DocCandidates([]string{
		"CLAUDE.md", "ARCHITECTURE.md", "README.md", "README", "docs/a.md",
		"docs/deep/b.md", "docs/x.txt", "src/foo.md", "src/README.md",
		"sub/CLAUDE.md", "go.mod",
	})
	want := []string{"CLAUDE.md", "ARCHITECTURE.md", "README.md", "README", "docs/a.md", "docs/deep/b.md"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DocCandidates = %v, want %v", got, want)
	}
}

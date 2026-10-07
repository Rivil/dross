package prtriage_test

import (
	"os"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/prtriage"
)

// reviewTemplate is the comment template fenced in review.md §4 — the shape
// /dross-review really posts.
func reviewTemplate(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../../assets/prompts/review.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	_, after, ok := strings.Cut(doc, "Comment template:")
	if !ok {
		t.Fatal("review.md has no \"Comment template:\" section")
	}
	_, after, ok = strings.Cut(after, "```markdown\n")
	if !ok {
		t.Fatal("review.md's comment template is not a ```markdown fence")
	}
	tmpl, _, ok := strings.Cut(after, "\n```\n")
	if !ok {
		t.Fatal("review.md's comment template fence never closes")
	}
	return tmpl
}

func TestSplitReviewMatchesReviewTemplate(t *testing.T) {
	got, ok := prtriage.SplitReview(reviewTemplate(t))
	if !ok || len(got) != 2 {
		t.Fatalf("the review.md template split into %d findings (ok=%v), want 2: %+v", len(got), ok, got)
	}
	if got[0].N != 1 || got[0].Severity != "BLOCKING" || got[0].Loc != "path/to/file.go:42" {
		t.Errorf("finding 1 = %+v, want BLOCKING at path/to/file.go:42", got[0])
	}
	if !strings.Contains(got[0].Text, "```snippet```") {
		t.Errorf("finding 1 lost its snippet line: %q", got[0].Text)
	}
	if got[1].N != 2 || got[1].Severity != "FLAG" {
		t.Errorf("finding 2 = %+v, want FLAG", got[1])
	}
}

const countsReview = `## /dross-review — phase x

2 blocking, 1 flag, 1 note across 3 lenses

### Security (2 findings)
- **BLOCKING** — ` + "`a.go:1`" + ` — first
  detail line
- **FLAG** — ` + "`b.go:2-4`" + ` — second

### Code quality (1 finding)
- **BLOCKING** — no location here

### Spec fidelity (1 finding)
- **NOTE** — ` + "`c.go:9`" + ` — last

---
*Posted by ` + "`/dross-review`" + `.*
`

func TestSplitReviewCounts(t *testing.T) {
	got, ok := prtriage.SplitReview(countsReview)
	if !ok || len(got) != 4 {
		t.Fatalf("got %d findings (ok=%v), want 4: %+v", len(got), ok, got)
	}
	want := []struct {
		sev, loc, text string
	}{
		{"BLOCKING", "a.go:1", "- **BLOCKING** — `a.go:1` — first\n  detail line"},
		{"FLAG", "b.go:2-4", "- **FLAG** — `b.go:2-4` — second"},
		{"BLOCKING", "", "- **BLOCKING** — no location here"},
		{"NOTE", "c.go:9", "- **NOTE** — `c.go:9` — last"},
	}
	for i, w := range want {
		f := got[i]
		if f.N != i+1 || f.Severity != w.sev || f.Loc != w.loc || f.Text != w.text {
			t.Errorf("finding %d = %+v, want N=%d %s %q %q", i+1, f, i+1, w.sev, w.loc, w.text)
		}
	}
	for _, f := range got {
		if strings.Contains(f.Text, "Posted by") || strings.Contains(f.Text, "2 blocking") || strings.Contains(f.Text, "###") {
			t.Errorf("finding %d swallowed the summary, a heading or the footer: %q", f.N, f.Text)
		}
	}
}

func TestSplitReviewFenceAware(t *testing.T) {
	t.Run("a bullet inside a snippet fence", func(t *testing.T) {
		body := "## /dross-review — phase x\n\n### Security (1 finding)\n" +
			"- **BLOCKING** — `a.go:1` — real\n  ```go\n- **BLOCKING** — fake\n### not a heading\n---\n  ```\n  after the fence\n"
		got, ok := prtriage.SplitReview(body)
		if !ok || len(got) != 1 {
			t.Fatalf("got %d findings, want 1: %+v", len(got), got)
		}
		if !strings.Contains(got[0].Text, "fake") || !strings.Contains(got[0].Text, "after the fence") {
			t.Errorf("the fenced lines left the finding: %q", got[0].Text)
		}
	})
	t.Run("a longer fence holds a shorter run", func(t *testing.T) {
		body := "## /dross-review\n- **FLAG** — real\n````\n```\n- **FLAG** — fake\n```\n````\n- **NOTE** — second real\n"
		got, ok := prtriage.SplitReview(body)
		if !ok || len(got) != 2 || got[1].Severity != "NOTE" {
			t.Fatalf("got %+v, want FLAG then NOTE", got)
		}
	})
	t.Run("a tilde fence is not closed by backticks", func(t *testing.T) {
		body := "## /dross-review\n- **FLAG** — real\n~~~\n```\n- **FLAG** — fake\n~~~\n"
		if got, _ := prtriage.SplitReview(body); len(got) != 1 {
			t.Fatalf("got %+v, want one finding", got)
		}
	})
	t.Run("inline triple backticks open nothing", func(t *testing.T) {
		body := "## /dross-review\n- **FLAG** — one\n  ```snippet```\n- **FLAG** — two\n"
		if got, _ := prtriage.SplitReview(body); len(got) != 2 {
			t.Fatalf("got %+v, want two findings", got)
		}
	})
	t.Run("a four-space indent opens nothing", func(t *testing.T) {
		body := "## /dross-review\n- **FLAG** — one\n    ```\n- **FLAG** — two\n"
		if got, _ := prtriage.SplitReview(body); len(got) != 2 {
			t.Fatalf("got %+v, want two findings", got)
		}
	})
}

func TestSplitReviewOwnHeaderOnly(t *testing.T) {
	for name, body := range map[string]string{
		"header on line 3":     "hello\n\n## /dross-review\n- **FLAG** — x\n",
		"header inside fence":  "```\n## /dross-review\n- **FLAG** — x\n```\n",
		"no findings":          "## /dross-review — phase x\n\n0 blocking, 0 flags\n\n---\n*Posted*\n",
		"indented bullet only": "## /dross-review\n  - **FLAG** — not column 0\n",
		"no header at all":     "- **FLAG** — x\n",
		"empty":                "",
	} {
		if got, ok := prtriage.SplitReview(body); ok || got != nil {
			t.Errorf("%s: got %+v, %v; want (nil, false)", name, got, ok)
		}
	}
	if got, ok := prtriage.SplitReview("\n  \n## /dross-review\n- **NOTE** — x\n"); !ok || len(got) != 1 {
		t.Errorf("leading blank lines: got %+v, %v; want one finding", got, ok)
	}
}

func TestSplitFindingIsolation(t *testing.T) {
	base, _ := prtriage.SplitReview(countsReview)
	edited, _ := prtriage.SplitReview(strings.Replace(countsReview, "— second", "— second, reworded", 1))
	if len(base) != len(edited) {
		t.Fatalf("an edit changed the finding count: %d -> %d", len(base), len(edited))
	}
	for i := range base {
		changed := base[i].Text != edited[i].Text
		if changed != (i == 1) {
			t.Errorf("finding %d changed=%v after editing finding 2 only", i+1, changed)
		}
	}
	crlf, _ := prtriage.SplitReview(strings.ReplaceAll(countsReview, "\n", "\r\n"))
	if len(crlf) != len(base) {
		t.Fatalf("CRLF split into %d findings, LF into %d", len(crlf), len(base))
	}
	for i := range base {
		if crlf[i] != base[i] {
			t.Errorf("finding %d differs under CRLF: %+v vs %+v", i+1, crlf[i], base[i])
		}
	}
}

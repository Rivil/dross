package memadvise

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/gitrun"
)

func sample(n int) []Suggestion {
	var out []Suggestion
	for i := 0; i < n; i++ {
		out = append(out, Suggestion{
			ID: "delete-m" + string(rune('a'+i)), File: "m" + string(rune('a'+i)) + ".md",
			Action: ActionDelete, Reason: "duplicates README.md", MonthlyUSD: float64(n-i) * 0.01,
		})
	}
	return out
}

func report(s []Suggestion) Report {
	return Report{Found: true, Suggestions: s, Turns: TurnCounts{Repo: 300, All: 1200, Age: 23 * time.Hour}}
}

func TestRenderSuggestionLine(t *testing.T) {
	out := Render(report([]Suggestion{{
		ID: "reword-old", File: "old.md", Action: ActionReword, NewHook: "shipped",
		Reason: "a time-bound clause in its hook has expired", MonthlyUSD: 1.234,
	}}), 3)
	line := strings.SplitN(out, "\n", 2)[0]
	for _, want := range []string{"reword-old", "old.md", "reword hook", "$1.23/mo", `→ "shipped"`} {
		if !strings.Contains(line, want) {
			t.Errorf("item line %q lacks %q", line, want)
		}
	}
	if !regexp.MustCompile(`\$\d+\.\d\d/mo`).MatchString(line) {
		t.Errorf("item line %q has no $X.XX/mo figure", line)
	}
	orphan := Render(report([]Suggestion{{ID: "delete-lost", File: "lost.md", Action: ActionDelete, ReportOnly: true, Reason: "orphan"}}), 3)
	if !strings.Contains(orphan, "[report only]") || !strings.Contains(orphan, "$0.00/mo") {
		t.Errorf("report-only item = %q", orphan)
	}
}

func TestRenderSuggestionReason(t *testing.T) {
	out := Render(report([]Suggestion{{ID: "delete-x", File: "x.md", Action: ActionDelete, Reason: "duplicates docs/state.md; linked from a.md"}}), 3)
	if !strings.Contains(out, "duplicates docs/state.md; linked from a.md") {
		t.Errorf("item line lacks its reason: %q", out)
	}
}

func TestRenderShareAndCacheAge(t *testing.T) {
	out := Render(report(sample(1)), 3)
	for _, want := range []string{"300 of 1200 assistant turns", "(25%)", "counted 23h ago", "$0.20/MTok"} {
		if !strings.Contains(out, want) {
			t.Errorf("share line lacks %q:\n%s", want, out)
		}
	}
	r := report(sample(1))
	r.Turns = TurnCounts{Age: 5 * time.Minute}
	if out := Render(r, 3); !strings.Contains(out, "(n/a)") || !strings.Contains(out, "counted 5m ago") {
		t.Errorf("zero-turn share line:\n%s", out)
	}
}

func TestRenderTruncation(t *testing.T) {
	out := Render(report(sample(5)), 3)
	if n := strings.Count(out, "  delete-m"); n != 3 {
		t.Errorf("rendered %d items, want 3:\n%s", n, out)
	}
	if !strings.Contains(out, "+2 more — dross memory advise") {
		t.Errorf("missing the +2 more line:\n%s", out)
	}
	if out := Render(report(sample(3)), 3); strings.Contains(out, "more —") {
		t.Errorf("3 of 3 rendered a more line:\n%s", out)
	}
	if out := Render(report(sample(5)), 0); strings.Count(out, "  delete-m") != 5 || strings.Contains(out, "more —") {
		t.Errorf("limit 0 did not render everything:\n%s", out)
	}
}

func TestRenderEmptyIsSilent(t *testing.T) {
	if out := Render(report(nil), 3); out != "" {
		t.Errorf("zero items rendered %q", out)
	}
	if out := Render(Report{}, 3); out != "" {
		t.Errorf("a missing dir rendered %q", out)
	}
	r := report(nil)
	r.Integrity.DuplicateNames = map[string][]string{"x": {"a.md", "b.md"}}
	if out := Render(r, 3); !strings.Contains(out, "1 duplicated name: slug") {
		t.Errorf("a duplicated slug alone rendered %q", out)
	}
}

// gatherRepo builds a git repo at root with a Claude config dir holding its
// memory dir, and returns (root, memory dir).
func gatherRepo(t *testing.T, files map[string]string) (string, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	mem := filepath.Join(cfg, "projects", Slug(root), "memory")
	if err := os.MkdirAll(mem, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(mem, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := gitrun.Run(root, "init", "-q"); err != nil {
		t.Fatal(err)
	}
	return root, mem
}

func writeRepoFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGatherUsesTrackedDocsOnly(t *testing.T) {
	twin := strings.Repeat("the release job signs every archive with minisign before it uploads. ", 4)
	root, _ := gatherRepo(t, map[string]string{
		IndexName:      "- [Untracked](untracked.md) — x\n- [Tracked](tracked.md) — y\n",
		"untracked.md": readme,
		"tracked.md":   twin,
	})
	writeRepoFile(t, root, "docs/x.md", readme)
	writeRepoFile(t, root, "docs/y.md", twin)
	if err := gitrun.Run(root, "add", "docs/y.md"); err != nil {
		t.Fatal(err)
	}
	r, err := Gather(root, adviseToday)
	if err != nil {
		t.Fatal(err)
	}
	ids := byID(r.Suggestions)
	if _, ok := ids["delete-untracked"]; ok {
		t.Errorf("an untracked doc produced a delete: %+v", r.Suggestions)
	}
	if s, ok := ids["delete-tracked"]; !ok || s.Doc != "docs/y.md" {
		t.Errorf("the tracked twin was not found: %+v", r.Suggestions)
	}
}

func TestGatherOutsideGitHasNoDocs(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if docs := trackedDocs(root); docs != nil {
		t.Errorf("a non-git root returned docs %+v", docs)
	}
}

// dirHash is sha256 over every entry's path, mode and bytes.
func dirHash(t *testing.T, dir string) string {
	t.Helper()
	h := sha256.New()
	var paths []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		paths = append(paths, p)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	for _, p := range paths {
		info, err := os.Lstat(p)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(dir, p)
		h.Write([]byte(rel + "\x00" + info.Mode().String() + "\x00"))
		if info.Mode().IsRegular() {
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			h.Write(b)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func TestGatherNeverWritesMemoryDir(t *testing.T) {
	root, mem := gatherRepo(t, map[string]string{
		IndexName: "- [Dup](dup.md) — x; until 2026-01-01\n- [Gone](gone.md) — y\n",
		"dup.md":  readme,
		"lost.md": "orphan\n",
	})
	before := dirHash(t, mem)
	if _, err := Gather(root, adviseToday); err != nil {
		t.Fatal(err)
	}
	// A day later the cache is stale and gets rewritten — still not here.
	if _, err := Gather(root, adviseToday.Add(25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if after := dirHash(t, mem); after != before {
		t.Error("Gather changed the memory dir")
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(TurnCachePath))); err != nil {
		t.Errorf("the turn cache was not written under the repo: %v", err)
	}
}

func TestGatherMissingDirIsZero(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	r, err := Gather(root, adviseToday)
	if err != nil || r.Found || Render(r, 3) != "" {
		t.Errorf("Gather with no memory dir = (%+v, %v)", r, err)
	}
}

func TestRenderNeverPrintsBodies(t *testing.T) {
	const sentinel = "SENTINEL-BODY-7f3a"
	root, _ := gatherRepo(t, map[string]string{
		IndexName: "- [Dup](dup.md) — x\n- [Old](old.md) — y; until 2026-01-01\n",
		"dup.md":  readme + " " + sentinel,
		"old.md":  sentinel + "\n",
		"lost.md": sentinel + "\n",
	})
	writeRepoFile(t, root, "README.md", readme)
	if err := gitrun.Run(root, "add", "README.md"); err != nil {
		t.Fatal(err)
	}
	r, err := Gather(root, adviseToday)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Suggestions) < 3 {
		t.Fatalf("fixture produced %d suggestions, want dup, old and lost", len(r.Suggestions))
	}
	if out := Render(r, 0); strings.Contains(out, sentinel) {
		t.Errorf("rendered output carries a memory body:\n%s", out)
	}
}

// TestGatherKeepsCountsOnCacheWriteError: a checkout whose .dross cannot hold
// the turn cache still gets its memory section. The counts Turns returned
// beside the write error stay in the Report, and the error rides along in
// CacheErr for advise to report.
func TestGatherKeepsCountsOnCacheWriteError(t *testing.T) {
	for _, tc := range []struct {
		name  string
		block func(t *testing.T, root string)
	}{
		{"read-only .dross", func(t *testing.T, root string) {
			if os.Geteuid() == 0 {
				t.Skip("root writes into a mode-0555 dir")
			}
			drossDir := filepath.Join(root, ".dross")
			if err := os.Mkdir(drossDir, 0o555); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(drossDir, 0o755) })
		}},
		{"state path is a file", func(t *testing.T, root string) {
			writeRepoFile(t, root, StateDir, "not a dir\n")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, mem := gatherRepo(t, map[string]string{
				IndexName: "- [Old](old.md) — y; until 2026-01-01\n",
				"old.md":  "old body\n",
			})
			at := adviseToday.Add(-time.Hour)
			writeTranscript(t, filepath.Join(filepath.Dir(mem), "s.jsonl"), turn(t, "msg_a", at), turn(t, "msg_b", at))
			tc.block(t, root)
			r, err := Gather(root, adviseToday)
			if err != nil {
				t.Fatalf("Gather failed on an unwritable turn cache: %v", err)
			}
			if !r.Found || len(r.Suggestions) == 0 || r.Turns.Repo != 2 {
				t.Errorf("Gather dropped the report: Found=%v suggestions=%d Repo=%d, want true, ≥1 and 2",
					r.Found, len(r.Suggestions), r.Turns.Repo)
			}
			if !errors.Is(r.CacheErr, ErrCacheWrite) {
				t.Errorf("Report.CacheErr = %v, want the ErrCacheWrite advise reports", r.CacheErr)
			}
			if Render(r, 3) == "" {
				t.Error("the report renders nothing; status would print no memory section")
			}
		})
	}
}

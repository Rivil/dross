package quality

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This file and internal/security/lifecycle_test.go are the same tests over the
// same code, because quality/lifecycle.go and security/lifecycle.go are
// structurally identical — they diverge only in which field carries the
// fingerprint class (Dimension here, Class there). Keeping both is deliberate:
// they are separate packages with separate run dirs, and a shared helper would
// make a divergence in one of them invisible.

func writeLedger(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const sampleQualityLedger = `
[[finding]]
  id = "f-1"
  title = "God object"
  risk = "high"
  dimension = "complexity"
  file = "internal/cmd/doctor.go"
  line = 12
  evidence = "1200 lines, 30 responsibilities"
  refutation = "panel could not refute"

[[finding]]
  id = "f-2"
  title = "Duplicated block"
  risk = "medium"
  dimension = "duplication"
  file = "internal/cmd/hints.go"
  line = 40
  evidence = "identical to repair.go:80"
  refutation = "panel could not refute"
`

// TestLatestRunPicksTheNewest pins run selection. Run ids are fixed-width,
// lexically-sortable timestamps, so "greatest name wins" is the whole
// mechanism — and it is what `dross quality findings <id>` resolves against.
// Picking the wrong run silently answers a question about stale findings.
func TestLatestRunPicksTheNewest(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"20260101-000000-aaaaaaa", "20260610-120000-ccccccc", "20260301-090000-bbbbbbb"} {
		if err := os.MkdirAll(filepath.Join(QualityDir(root), id), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// A stray FILE among the run dirs must not win, even though its name sorts
	// last: only directories are runs.
	writeLedger(t, filepath.Join(QualityDir(root), "zzz-not-a-run"), "")

	got, err := LatestRun(root)
	if err != nil {
		t.Fatalf("LatestRun: %v", err)
	}
	if want := filepath.Join(QualityDir(root), "20260610-120000-ccccccc"); got != want {
		t.Errorf("LatestRun = %q, want %q", got, want)
	}
}

// TestLatestRunErrorsDistinguishNoDirFromNoRuns: "the audit has never run" and
// "the directory is unreadable" are different problems with different fixes,
// and a caller that saw one message for both would send the user to run an
// audit that is already failing for another reason.
func TestLatestRunErrorsDistinguishNoDirFromNoRuns(t *testing.T) {
	t.Run("no quality dir at all", func(t *testing.T) {
		_, err := LatestRun(t.TempDir())
		if err == nil {
			t.Fatal("LatestRun with no quality dir returned no error")
		}
		if !strings.Contains(err.Error(), "read quality runs") {
			t.Errorf("err = %q, want the read failure", err)
		}
	})

	t.Run("dir exists but holds no runs", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(QualityDir(root), 0o755); err != nil {
			t.Fatal(err)
		}
		_, err := LatestRun(root)
		if err == nil {
			t.Fatal("LatestRun with an empty quality dir returned no error")
		}
		if !strings.Contains(err.Error(), "no quality runs") {
			t.Errorf("err = %q, want the no-runs message", err)
		}
		if strings.Contains(err.Error(), "read quality runs") {
			t.Error("an empty dir was reported as an unreadable one")
		}
	})

	t.Run("only files, no run dirs", func(t *testing.T) {
		root := t.TempDir()
		writeLedger(t, filepath.Join(QualityDir(root), "notes.txt"), "hi")
		if _, err := LatestRun(root); err == nil || !strings.Contains(err.Error(), "no quality runs") {
			t.Errorf("a dir holding only files must report no runs, got %v", err)
		}
	})
}

// TestResolveItemMapsIDToFingerprint is the lookup `dross quality findings <id>
// --state` depends on: a per-run id the user read off a report becomes the
// durable fingerprint. The fingerprint keys on Dimension, NOT Risk — Risk is a
// contextual, run-to-run ranking, and keying on it would split one finding into
// two the moment the panel re-scored it.
func TestResolveItemMapsIDToFingerprint(t *testing.T) {
	root := t.TempDir()
	runDir := filepath.Join(QualityDir(root), "20260610-120000-ccccccc")
	writeLedger(t, filepath.Join(runDir, "findings.toml"), sampleQualityLedger)

	got, err := ResolveItem(root, "f-2")
	if err != nil {
		t.Fatalf("ResolveItem: %v", err)
	}
	if got.Class != "duplication" {
		t.Errorf("Class = %q, want the dimension — not the risk", got.Class)
	}
	if got.File != "internal/cmd/hints.go" || got.Title != "Duplicated block" {
		t.Errorf("ResolveItem returned the wrong finding: %+v", got)
	}

	// The first finding resolves too, so the loop is not returning whatever it
	// saw last.
	first, err := ResolveItem(root, "f-1")
	if err != nil {
		t.Fatalf("ResolveItem(f-1): %v", err)
	}
	if first.Class != "complexity" || first.Title != "God object" {
		t.Errorf("ResolveItem(f-1) = %+v", first)
	}
}

// TestResolveItemErrorsAreDistinct: an unknown id, a missing run and an
// unreadable ledger are three different failures. Collapsing them would tell a
// user their id was wrong when the real problem is that no audit has run.
func TestResolveItemErrorsAreDistinct(t *testing.T) {
	t.Run("unknown id names the run it looked in", func(t *testing.T) {
		root := t.TempDir()
		runDir := filepath.Join(QualityDir(root), "20260610-120000-ccccccc")
		writeLedger(t, filepath.Join(runDir, "findings.toml"), sampleQualityLedger)

		_, err := ResolveItem(root, "f-99")
		if err == nil {
			t.Fatal("ResolveItem with an unknown id returned no error")
		}
		if !strings.Contains(err.Error(), "f-99") {
			t.Errorf("err = %q, want it to quote the id", err)
		}
		if !strings.Contains(err.Error(), "20260610-120000-ccccccc") {
			t.Errorf("err = %q, want it to name the run it searched", err)
		}
	})

	t.Run("no runs at all", func(t *testing.T) {
		_, err := ResolveItem(t.TempDir(), "f-1")
		if err == nil {
			t.Fatal("ResolveItem with no runs returned no error")
		}
		if strings.Contains(err.Error(), "f-1") {
			t.Errorf("err = %q — this is a no-runs failure, not a bad-id one", err)
		}
	})

	t.Run("unreadable ledger", func(t *testing.T) {
		root := t.TempDir()
		runDir := filepath.Join(QualityDir(root), "20260610-120000-ccccccc")
		writeLedger(t, filepath.Join(runDir, "findings.toml"), "this is not toml {{{")

		_, err := ResolveItem(root, "f-1")
		if err == nil {
			t.Fatal("ResolveItem over an unparseable ledger returned no error")
		}
		if !strings.Contains(err.Error(), "findings ledger") {
			t.Errorf("err = %q, want the ledger load failure", err)
		}
	})
}

// TestStatePathSitsAboveTheRunDirs: the durable ledger must live beside the
// timestamped run dirs, never inside one, or run-dir pruning takes the whole
// finding history with it.
func TestStatePathSitsAboveTheRunDirs(t *testing.T) {
	root := t.TempDir()
	got := StatePath(root)
	if want := filepath.Join(QualityDir(root), "state.toml"); got != want {
		t.Errorf("StatePath = %q, want %q", got, want)
	}
	if filepath.Dir(got) != QualityDir(root) {
		t.Errorf("StatePath sits inside a run dir (%q) — it would not survive pruning", got)
	}
}

// TestLedgerItemsKeyOnDimension is the Items() half of the same fingerprint
// rule: every finding becomes an Item classed by Dimension.
func TestLedgerItemsKeyOnDimension(t *testing.T) {
	l := Ledger{Findings: []Finding{
		{ID: "f-1", Title: "God object", Risk: "high", Dimension: "complexity", File: "a.go"},
		{ID: "f-2", Title: "Dupe", Risk: "low", Dimension: "duplication", File: "b.go"},
	}}
	items := l.Items()
	if len(items) != 2 {
		t.Fatalf("Items() returned %d items, want 2", len(items))
	}
	if items[0].Class != "complexity" || items[1].Class != "duplication" {
		t.Errorf("Items() must class on Dimension, got %+v", items)
	}
	if items[0].Class == string(l.Findings[0].Risk) {
		t.Error("Items() classed on Risk — a re-score would split one finding into two")
	}
}

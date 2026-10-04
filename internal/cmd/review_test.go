package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/gate"
	"github.com/Rivil/dross/internal/gatestate"
	"github.com/Rivil/dross/internal/review"
	"github.com/Rivil/dross/internal/treefp"
)

const reviewPlan = `[phase]
id = "p"
[[task]]
id = "t-1"
wave = 1
title = "one"
files = ["a.go"]
covers = ["c-1"]
test_contract = ["if A breaks, TestA fails"]
status = "in_progress"
`

// reviewFixture is a committed dross repo on phase/p with t-1 in progress
// and the execute mode recorded; it returns the repo dir and a fresh HOME.
func reviewFixture(t *testing.T, mode string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	gitInit(t, dir, "https://forge.example/me/p.git")
	chdir(t, dir)
	home := t.TempDir()
	t.Setenv("HOME", home)
	scaffoldPhaseWithPlan(t, "p", reviewPlan)
	if err := runCmd(t, State(), "set", "current_phase", "p"); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "a.go"), "package a\n")
	gitCommit(t, dir, "init")
	mustGit(t, dir, "checkout", "-q", "-b", "phase/p")
	if err := gatestate.SaveExecute(dir, gatestate.Execute{Phase: "p", Mode: mode, At: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	return dir, home
}

func printedField(t *testing.T, out, key string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(line, key+":"); ok {
			return strings.TrimSpace(v)
		}
	}
	t.Fatalf("output has no %s line:\n%s", key, out)
	return ""
}

func runReview(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var err error
	out := captureStdout(t, func() { err = runCmd(t, Review(), args...) })
	return out, err
}

// TestContextVerbDigest: the digest the verb prints is the digest a fresh
// BuildContext over the same armed scope computes — the recorder's check.
func TestContextVerbDigest(t *testing.T) {
	dir, home := reviewFixture(t, "solo")
	mustWrite(t, filepath.Join(home, ".claude", "dross", "gates.toml"), "secret_paths = [\"*.secret\"]\n")
	mustWrite(t, filepath.Join(dir, "a.go"), "package a // edited\n")
	mustWrite(t, filepath.Join(dir, "creds.secret"), "do-not-render-this-line\n")

	out, err := runReview(t, "context")
	if err != nil {
		t.Fatal(err)
	}
	digest := printedField(t, out, "digest")
	scope, err := gate.ArmedScope(dir)
	if err != nil || scope == nil {
		t.Fatalf("scope = %+v, %v", scope, err)
	}
	cs, err := gate.ContextScope(dir, home, scope)
	if err != nil {
		t.Fatal(err)
	}
	want, err := review.BuildContext(dir, cs, gate.ReviewSecrets(home))
	if err != nil {
		t.Fatal(err)
	}
	if digest != want.Digest {
		t.Fatalf("printed digest %s != rebuilt %s", digest, want.Digest)
	}
	if got := printedField(t, out, "prompt"); got != review.PromptLine(want.Digest) {
		t.Fatalf("prompt = %q, want %q", got, review.PromptLine(want.Digest))
	}
	if printedField(t, out, "subagent_type") != review.ReviewerAgent {
		t.Fatal("the verb does not name the reviewer subagent_type")
	}
	b, err := os.ReadFile(gatestate.Path(dir, gatestate.ContextFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != string(want.File()) {
		t.Fatal("the written context file is not the rebuilt context")
	}
	if strings.Contains(string(b), "do-not-render-this-line") {
		t.Fatal("a gates.toml secret path's content reached the context file")
	}
}

func TestContextVerbPrintsNoDiff(t *testing.T) {
	dir, _ := reviewFixture(t, "solo")
	mustWrite(t, filepath.Join(dir, "a.go"), "package a // EDITED-LINE\n")
	out, err := runReview(t, "context")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"EDITED-LINE", "diff --git", "@@"} {
		if strings.Contains(out, bad) {
			t.Fatalf("review context printed part of the diff (%q):\n%s", bad, out)
		}
	}
}

func TestContextVerbRefuses(t *testing.T) {
	noFile := func(t *testing.T, dir string) {
		t.Helper()
		if _, err := os.Stat(gatestate.Path(dir, gatestate.ContextFile)); !os.IsNotExist(err) {
			t.Fatalf("a refused review context wrote the context file (%v)", err)
		}
	}

	dir, _ := reviewFixture(t, "pair")
	mustWrite(t, filepath.Join(dir, "a.go"), "package a // edited\n")
	if _, err := runReview(t, "context"); err == nil || !strings.Contains(err.Error(), "the human is the gate") {
		t.Fatalf("pair mode: %v, want a refusal naming the human gate", err)
	}
	noFile(t, dir)

	dir, _ = reviewFixture(t, "solo")
	mustWrite(t, filepath.Join(dir, "a.go"), "package a // edited\n")
	if err := runCmd(t, Task(), "status", "p", "t-1", "pending"); err != nil {
		t.Fatal(err)
	}
	if _, err := runReview(t, "context"); err == nil || !strings.Contains(err.Error(), "no solo task or quick is armed") {
		t.Fatalf("no task in progress: %v, want a refusal", err)
	}
	noFile(t, dir)

	dir, _ = reviewFixture(t, "solo")
	mustWrite(t, filepath.Join(dir, ".dross", "notes.md"), "bookkeeping\n")
	if _, err := runReview(t, "context"); err == nil || !strings.Contains(err.Error(), "nothing to review") {
		t.Fatalf(".dross/-only diff: %v, want a refusal", err)
	}
	noFile(t, dir)

	dir, home := reviewFixture(t, "solo")
	mustWrite(t, filepath.Join(dir, "a.go"), "package a // edited\n")
	mustWrite(t, filepath.Join(home, ".claude", "dross", "gates.toml"), "secret_paths = [\n")
	if _, err := runReview(t, "context"); err == nil || !strings.Contains(err.Error(), "gates.toml") {
		t.Fatalf("malformed gates.toml: %v, want a refusal naming it", err)
	}
	noFile(t, dir)
}

// drossDigest hashes every file under .dross/ except the context file the
// context verb exists to write.
func drossDigest(t *testing.T, dir string, skip ...string) string {
	t.Helper()
	var names []string
	sums := map[string]string{}
	err := filepath.WalkDir(filepath.Join(dir, ".dross"), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		for _, s := range skip {
			if filepath.Base(p) == s {
				return nil
			}
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		names = append(names, p)
		sums[p] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		b.WriteString(n + "=" + sums[n] + "\n")
	}
	return b.String()
}

func indexDigest(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]) + mustGit(t, dir, "diff", "--cached", "--name-only")
}

// TestReviewVerbsNoSideEffects: neither verb records anything a gate trusts —
// no verb can record a pass (review_pass_signal).
func TestReviewVerbsNoSideEffects(t *testing.T) {
	dir, _ := reviewFixture(t, "solo")
	mustWrite(t, filepath.Join(dir, "a.go"), "package a // staged\n")
	mustGit(t, dir, "add", "a.go")
	mustWrite(t, filepath.Join(dir, "b.go"), "package b\n")

	idx, drs := indexDigest(t, dir), drossDigest(t, dir, gatestate.ContextFile, ".gitignore")
	if _, err := runReview(t, "context"); err != nil {
		t.Fatal(err)
	}
	if indexDigest(t, dir) != idx {
		t.Fatal("review context changed the git index")
	}
	if drossDigest(t, dir, gatestate.ContextFile, ".gitignore") != drs {
		t.Fatal("review context changed a .dross/ record other than the context file")
	}
	if _, err := os.Stat(gatestate.Path(dir, gatestate.ReviewFile)); !os.IsNotExist(err) {
		t.Fatal("review context wrote a review ledger")
	}

	all := drossDigest(t, dir)
	if _, err := runReview(t, "status"); err != nil {
		t.Fatal(err)
	}
	if drossDigest(t, dir) != all {
		t.Fatal("review status changed a byte under .dross/")
	}
}

func TestReviewStatusAgreesWithLedger(t *testing.T) {
	dir, _ := reviewFixture(t, "solo")
	mustWrite(t, filepath.Join(dir, "a.go"), "package a // edited\n")
	scope, err := gate.ArmedScope(dir)
	if err != nil || scope == nil {
		t.Fatalf("scope = %+v, %v", scope, err)
	}
	tree, err := treefp.WorkingTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	ledger := func(rounds ...review.Round) {
		t.Helper()
		if err := gatestate.SaveReview(dir, gatestate.Review{Kind: scope.Kind, Phase: scope.Phase, Task: scope.Task, Attempt: scope.Attempt, Rounds: rounds}); err != nil {
			t.Fatal(err)
		}
	}
	status := func() string {
		t.Helper()
		out, err := runReview(t, "status")
		if err != nil {
			t.Fatal(err)
		}
		return printedField(t, out, "status")
	}

	if got := status(); got != "none" {
		t.Errorf("no ledger: status %q, want none", got)
	}
	ledger(review.Round{Outcome: review.OutcomePass, Tree: tree})
	if got := status(); got != "pass" {
		t.Errorf("pass for the current tree: status %q", got)
	}
	ledger(review.Round{Outcome: review.OutcomeBlock}, review.Round{Outcome: review.OutcomeBlock}, review.Round{Outcome: review.OutcomePass, Tree: tree})
	if got := status(); got != "exhausted" {
		t.Errorf("[block, block, pass]: status %q, want exhausted", got)
	}
	ledger(review.Round{Outcome: review.OutcomePass, Tree: tree})
	mustWrite(t, filepath.Join(dir, "a.go"), "package a // edited again\n")
	if got := status(); got != "pass-stale" {
		t.Errorf("a pass whose tree has since changed: status %q, want pass-stale", got)
	}
	ledger(review.Round{Outcome: review.OutcomeBlock, Spec: []review.Finding{{Criterion: "c-1", Severity: review.Blocking, Text: "SPEC-GAP"}}})
	out, _ := runReview(t, "status")
	if !strings.Contains(out, "status: blocked") || !strings.Contains(out, "SPEC-GAP") {
		t.Errorf("blocked status does not show the finding to fix:\n%s", out)
	}
}

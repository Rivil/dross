package gate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustGate(t *testing.T, name string) Gate {
	t.Helper()
	g, ok := Lookup(name)
	if !ok {
		t.Fatalf("gate %q is not registered", name)
	}
	return g
}

func TestOverrideExpiry(t *testing.T) {
	home := t.TempDir()
	repo := droot(t)
	commit, read := mustGate(t, "commit-green"), mustGate(t, "secret-read")
	until := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	if err := Lift(home, commit, repo, until); err != nil {
		t.Fatal(err)
	}
	if !LiftedBy(home, until.Add(-time.Second))(commit, repo) {
		t.Error("commit-green is not lifted before its expiry")
	}
	if LiftedBy(home, until.Add(time.Second))(commit, repo) {
		t.Error("commit-green is still lifted after its expiry")
	}
	if LiftedBy(home, until.Add(-time.Second))(read, repo) {
		t.Error("lifting commit-green lifted secret-read too")
	}
}

func TestOverrideScope(t *testing.T) {
	home := t.TempDir()
	a, b := droot(t), droot(t)
	commit, read := mustGate(t, "commit-green"), mustGate(t, "secret-read")
	until := time.Now().Add(time.Hour)
	if err := Lift(home, commit, a, until); err != nil {
		t.Fatal(err)
	}
	if err := Lift(home, read, a, until); err != nil {
		t.Fatal(err)
	}
	lifted := LiftedBy(home, time.Now())
	if !lifted(commit, a) || lifted(commit, b) {
		t.Errorf("commit-green lifted in A=%v B=%v, want A only", lifted(commit, a), lifted(commit, b))
	}
	for _, root := range []string{a, b, ""} {
		if !lifted(read, root) {
			t.Errorf("a secret-read override did not apply at %q — it is machine-wide", root)
		}
	}
	if err := Lift(home, commit, "", until); err == nil {
		t.Error("a workflow gate was lifted with no repo")
	}
	for _, name := range []string{"gate-off-guard", "tamper-guard"} {
		if err := Lift(home, mustGate(t, name), a, until); err == nil {
			t.Errorf("%s was lifted", name)
		}
	}
	if err := Unlift(home, commit, a); err != nil {
		t.Fatal(err)
	}
	if LiftedBy(home, time.Now())(commit, a) {
		t.Error("Unlift left commit-green lifted")
	}
}

func TestCorruptOverridesLiftNothing(t *testing.T) {
	home := t.TempDir()
	p := OverridesPath(home)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`[{"name":"secret-read","until":"2999-01-01T00:00:00Z"`), 0o644); err != nil {
		t.Fatal(err)
	}
	if LiftedBy(home, time.Now())(mustGate(t, "secret-read"), "") {
		t.Error("a corrupt overrides file lifted a gate")
	}
	if _, err := LoadOverrides(home); err == nil || !strings.Contains(err.Error(), OverridesFile) {
		t.Errorf("LoadOverrides on a corrupt file = %v, want an error naming it for `gate status`", err)
	}
}

func TestOverridesSurviveMalformedLists(t *testing.T) {
	home := t.TempDir()
	writeLists(t, home, "secret_tools = [\n")
	read := mustGate(t, "secret-read")
	if err := Lift(home, read, "", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if list, err := LoadOverrides(home); err != nil || len(list) != 1 {
		t.Errorf("overrides = %v, %v; a malformed gates.toml must not touch the store", list, err)
	}
}

func TestTamperGuardFileTools(t *testing.T) {
	home := t.TempDir()
	repo := droot(t)
	e := Env{Home: home, Gates: []Gate{mustGate(t, "tamper-guard")}, Lifted: func(Gate, string) bool { return true }}
	for _, p := range []string{
		filepath.Join(repo, ".dross", "gate", "green.json"),
		filepath.Join(repo, ".dross", "gate", "approval.json"),
		OverridesPath(home),
	} {
		res := Check(payload(t, "Write", map[string]any{"file_path": p, "content": "{}"}, repo), e)
		if res.Allowed() || !strings.Contains(res.Text(), "tamper-guard") {
			t.Errorf("Write %s: %q, want a tamper-guard refusal even with every gate lifted", p, res.Text())
		}
		if res := Check(payload(t, "Read", map[string]any{"file_path": p}, repo), e); !res.Allowed() {
			t.Errorf("Read %s was refused: %q", p, res.Text())
		}
	}
}

func TestTamperGuardBash(t *testing.T) {
	home := t.TempDir()
	repo := droot(t)
	e := Env{Home: home, Gates: []Gate{mustGate(t, "tamper-guard")}}
	for _, line := range []string{
		"echo x > .dross/gate/green.json",
		"printf x | tee .dross/gate/approval.json",
		"cp /tmp/g .dross/gate/green.json",
		"mv /tmp/g .dross/gate/",
		"cd .dross/gate && echo x > green.json",
		"echo '{}' > ~/.claude/dross/gate-overrides.json",
		`echo '{}' > "$HOME/.claude/dross/gate-overrides.json"`,
	} {
		if res := Check(bash(t, line, repo), e); res.Allowed() {
			t.Errorf("%q passed", line)
		}
	}
	for _, line := range []string{"cat .dross/gate/green.json", "echo x > /tmp/g", "cp .dross/gate/green.json /tmp/g", "ls .dross/gate"} {
		if res := Check(bash(t, line, repo), e); !res.Allowed() {
			t.Errorf("%q was refused: %q", line, res.Text())
		}
	}
}

// TestTamperGuardReviewRecords: the review ledger, the quick marker and the
// review context are gate records like green.json — no tool call writes them.
func TestTamperGuardReviewRecords(t *testing.T) {
	home := t.TempDir()
	repo := droot(t)
	e := Env{Home: home, Gates: []Gate{mustGate(t, "tamper-guard")}, Lifted: func(Gate, string) bool { return true }}
	for _, name := range []string{"review.json", "quick.json", "review-context.md"} {
		p := filepath.Join(repo, ".dross", "gate", name)
		for _, tool := range []string{"Write", "Edit"} {
			res := Check(payload(t, tool, map[string]any{"file_path": p, "content": "{}", "old_string": "a", "new_string": "b"}, repo), e)
			if res.Allowed() || !strings.Contains(res.Text(), "tamper-guard") {
				t.Errorf("%s %s: %q, want a tamper-guard refusal", tool, name, res.Text())
			}
		}
	}
	for _, line := range []string{
		"echo x > .dross/gate/review.json",
		"cp /tmp/r .dross/gate/review-context.md",
		"printf '{}' | tee .dross/gate/quick.json",
	} {
		if res := Check(bash(t, line, repo), e); res.Allowed() {
			t.Errorf("%q passed", line)
		}
	}
}

// TestReviewRecordDeleteRefused: deleting review.json resets the fix-round
// cap and an unavailable verdict — the first gate record whose absence is more
// lenient — so every Bash path that deletes it is refused.
func TestReviewRecordDeleteRefused(t *testing.T) {
	home := t.TempDir()
	repo := droot(t)
	e := Env{Home: home, Gates: []Gate{mustGate(t, "tamper-guard")}}
	for _, line := range []string{
		"rm .dross/gate/review.json",
		"rm -f .dross/gate/quick.json",
		"unlink .dross/gate/review.json",
		"mv .dross/gate/review.json /tmp/x",
		"rm -rf .dross",
		"git clean -fX .dross",
		"git clean -xdf",
		"git -C . clean -fdX",
		"git stash push -a",
		"git stash --all",
		"cd .dross && rm gate/review.json",
	} {
		if res := Check(bash(t, line, repo), e); res.Allowed() {
			t.Errorf("%q passed", line)
		}
	}
	for _, line := range []string{
		"cp .dross/gate/review.json /tmp/r",
		"cat .dross/gate/review.json",
		"git clean -fd",
		"git clean -fX src",
		"git stash push -u -- . ':(exclude).dross'",
		"git stash push -a -- . ':(exclude).dross'",
		"rm -rf /tmp/elsewhere",
		"rm a.go",
	} {
		if res := Check(bash(t, line, repo), e); !res.Allowed() {
			t.Errorf("%q was refused: %q", line, res.Text())
		}
	}
}

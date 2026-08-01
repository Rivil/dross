package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestLocalSetGetRoundTrips is the store's basic contract: what `local set`
// writes, `local get` reads back on the next process.
func TestLocalSetGetRoundTrips(t *testing.T) {
	chdirDross(t)

	if err := runCmd(t, Local(), "set", "quick_base", "main"); err != nil {
		t.Fatalf("local set: %v", err)
	}
	var out string
	if err := runCmdCapturing(t, &out, Local(), "get", "quick_base"); err != nil {
		t.Fatalf("local get: %v", err)
	}
	if strings.TrimSpace(out) != "main" {
		t.Errorf("quick_base did not round-trip: got %q want %q", strings.TrimSpace(out), "main")
	}
}

// TestLocalStoreIsUntracked is the property the whole store exists for: the
// recorded base must never enter cumulative history. state.json rides the
// squash onto the base, so a value kept there is inherited by every later
// tree — the drag-forward this milestone is removing. A tracked local.toml
// would reintroduce it.
//
// Behavioural check via `git check-ignore` (the idiom the security/quality/
// techdebt artifact guards use): it catches a wrong pattern that a string
// match on the .gitignore line would miss. Exit 0 = ignored, 1 = not.
func TestLocalStoreIsUntracked(t *testing.T) {
	root := repoRootFromTest(t)
	if err := exec.Command("git", "-C", root, "check-ignore", ".dross/local.toml").Run(); err != nil {
		t.Fatalf("git check-ignore reports .dross/local.toml is NOT ignored (err=%v); "+
			"the machine-local store must stay out of cumulative history, or a stale "+
			"quick_base gets dragged forward onto every later tree", err)
	}
}

// TestLocalSetCreatesStoreOnDemand pins create-on-write: the file is
// gitignored, so a fresh clone has none and the first writer must make it.
func TestLocalSetCreatesStoreOnDemand(t *testing.T) {
	root := chdirDross(t)

	path := filepath.Join(root, "local.toml")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("fixture should start with no local.toml, stat err = %v", err)
	}
	if err := runCmd(t, Local(), "set", "quick_base", "milestone/v1.2"); err != nil {
		t.Fatalf("local set on a root with no local.toml: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("local.toml not created: %v", err)
	}
	if !strings.Contains(string(b), "milestone/v1.2") {
		t.Errorf("local.toml missing the written value: %s", b)
	}
}

// TestLocalGetUnsetKeyIsEmptyAndClean pins the unset case: callers branch on
// empty output, so "no value recorded" must exit 0 with nothing printed
// rather than erroring.
func TestLocalGetUnsetKeyIsEmptyAndClean(t *testing.T) {
	chdirDross(t)

	var out string
	if err := runCmdCapturing(t, &out, Local(), "get", "quick_base"); err != nil {
		t.Fatalf("get on an unset key should succeed, got %v", err)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("unset key should print nothing, got %q", out)
	}
}

// TestLocalRejectsUnknownKey keeps the key set closed — a typo'd key must not
// become a silently-written entry no reader ever looks for.
func TestLocalRejectsUnknownKey(t *testing.T) {
	root := chdirDross(t)

	for _, args := range [][]string{
		{"set", "quik_base", "main"},
		{"get", "quik_base"},
	} {
		err := runCmd(t, Local(), args...)
		if err == nil {
			t.Fatalf("expected an error for `local %s` on an unknown key", strings.Join(args, " "))
		}
		if !strings.Contains(err.Error(), "quick_base") {
			t.Errorf("error should name the valid keys: %v", err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "local.toml")); !os.IsNotExist(err) {
		t.Errorf("a rejected key must not create the store, stat err = %v", err)
	}
}

// TestReadLocalKeyIsBestEffort pins the reader other commands use: the store
// is a reconciliation hint, never a gate, so a missing store or an unknown key
// yields "" instead of failing the command that asked.
func TestReadLocalKeyIsBestEffort(t *testing.T) {
	root := chdirDross(t)

	if got := readLocalKey(root, "quick_base"); got != "" {
		t.Errorf("missing store should read empty, got %q", got)
	}
	if err := runCmd(t, Local(), "set", "quick_base", "main"); err != nil {
		t.Fatalf("local set: %v", err)
	}
	if got := readLocalKey(root, "quick_base"); got != "main" {
		t.Errorf("readLocalKey: got %q want %q", got, "main")
	}
	if got := readLocalKey(root, "nope"); got != "" {
		t.Errorf("unknown key should read empty, got %q", got)
	}
}

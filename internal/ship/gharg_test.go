package ship

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The gh argv discipline, asserted BY INDEX rather than by substring search.
//
// A substring check ("the argv contains `--`") cannot tell a separator in the
// right place from one in the wrong place, and for gh the difference is not
// cosmetic: cobra's `--` ends flag parsing entirely rather than fencing the one
// token after it, so a flag demoted past the separator becomes a positional.
// `gh pr view` takes at most one positional and exits "accepts at most 1
// arg(s)"; `gh pr comment` takes the body as text and posts the wrong content.
// Both shapes pass a test that only searches for "--".

// captureGh swaps the ghCommand seam for a recorder whose stdout is `stdout`,
// and returns a pointer to the captured argv.
func captureGh(t *testing.T, stdout string) *[]string {
	t.Helper()
	var got []string
	prev := ghCommand
	ghCommand = func(args ...string) *exec.Cmd {
		got = append([]string(nil), args...)
		return exec.Command("printf", "%s", stdout)
	}
	t.Cleanup(func() { ghCommand = prev })
	return &got
}

// refuseGh installs a ghCommand that fails the test if it is ever called. It is
// how "refused BEFORE exec" is asserted — a refusal that still spawned the
// process would otherwise look identical to one that didn't.
func refuseGh(t *testing.T) {
	t.Helper()
	prev := ghCommand
	ghCommand = func(args ...string) *exec.Cmd {
		t.Fatalf("gh was invoked despite a value that should have been refused: %v", args)
		return nil
	}
	t.Cleanup(func() { ghCommand = prev })
}

// stubGhOnPath puts a no-op `gh` executable at the front of PATH, so the
// LookPath guard in openGitHubPR is satisfied without the test depending on the
// developer's machine (and without skipping, which would make it vacuous).
func stubGhOnPath(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "gh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestPRStatusArgv(t *testing.T) {
	got := captureGh(t, `{"state":"MERGED","mergedAt":"2026-01-01","baseRefName":"main"}`)
	if _, err := gitHubPRStatus(OpenOpts{Provider: "github", PRNumber: 12}); err != nil {
		t.Fatalf("gitHubPRStatus: %v", err)
	}
	want := []string{"pr", "view", "--json", "state,mergedAt,baseRefName", "--", "12"}
	if !reflect.DeepEqual(*got, want) {
		t.Errorf("argv = %v\nwant  %v\n(flags BEFORE the separator — cobra's -- ends flag parsing, it does not fence one token)", *got, want)
	}
}

func TestPRStatusRejectsBadNumber(t *testing.T) {
	refuseGh(t)
	if _, err := gitHubPRStatus(OpenOpts{Provider: "github", PRNumber: -3}); err == nil {
		t.Fatal("a negative PR number was accepted")
	}
}

func TestPostCommentArgv(t *testing.T) {
	got := captureGh(t, "")
	if err := postGitHubComment(CommentOpts{Provider: "github", PRNumber: 7, Body: "panel findings"}); err != nil {
		t.Fatalf("postGitHubComment: %v", err)
	}
	want := []string{"pr", "comment", "--body", "panel findings", "--", "7"}
	if !reflect.DeepEqual(*got, want) {
		t.Errorf("argv = %v\nwant  %v", *got, want)
	}

	t.Run("refuses a non-positive number before exec", func(t *testing.T) {
		refuseGh(t)
		for _, n := range []int{0, -1} {
			if err := postGitHubComment(CommentOpts{Provider: "github", PRNumber: n, Body: "x"}); err == nil {
				t.Errorf("PRNumber %d was accepted", n)
			}
		}
	})
}

// TestPostCommentBodyBeforeSeparator is the silent-wrong-content guard. A
// demoted --body does not crash: cobra reads the body text as a positional and
// the comment posts with the wrong content, which no error path would surface.
func TestPostCommentBodyBeforeSeparator(t *testing.T) {
	got := captureGh(t, "")
	if err := postGitHubComment(CommentOpts{Provider: "github", PRNumber: 7, Body: "b"}); err != nil {
		t.Fatalf("postGitHubComment: %v", err)
	}
	argv := *got
	sep := indexOf(argv, "--")
	body := indexOf(argv, "--body")
	if sep < 0 {
		t.Fatalf("no separator in argv: %v", argv)
	}
	if body < 0 || body > sep {
		t.Fatalf("--body is not ahead of the separator: %v", argv)
	}
	if body+1 >= len(argv) || argv[body+1] != "b" {
		t.Fatalf("--body's value is not in its slot: %v", argv)
	}
}

// ghValueTakingFlags are the gh flags in this codebase that consume the next
// argument as their value. Anything else in an argv position is a positional.
var ghValueTakingFlags = map[string]bool{
	"--title": true, "--body": true, "--head": true, "--base": true,
	"--reviewer": true, "--json": true, "--state": true,
}

// TestOpenPRArgvWalk pins that no derived value escapes its flag's value slot,
// with every field set to something that WOULD be read as a flag if it did.
func TestOpenPRArgvWalk(t *testing.T) {
	stubGhOnPath(t)
	got := captureGh(t, "https://github.com/o/r/pull/9\n")
	if _, err := openGitHubPR(OpenOpts{
		Provider:   "github",
		Title:      "--json",
		Body:       "-x",
		HeadBranch: "phase/x",
		BaseBranch: "--repo evil/x",
		Reviewers:  []string{"--json"},
	}); err != nil {
		t.Fatalf("openGitHubPR: %v", err)
	}
	argv := *got
	if len(argv) < 2 || argv[0] != "pr" || argv[1] != "create" {
		t.Fatalf("argv does not start with `pr create`: %v", argv)
	}
	// Each derived value must sit at the index immediately after its own flag.
	for flag, want := range map[string]string{
		"--title": "--json", "--body": "-x", "--head": "phase/x",
		"--base": "--repo evil/x", "--reviewer": "--json",
	} {
		i := indexOf(argv, flag)
		if i < 0 {
			t.Errorf("%s missing from argv: %v", flag, argv)
			continue
		}
		if i+1 >= len(argv) || argv[i+1] != want {
			t.Errorf("%s's value is not in its slot (want %q at index %d): %v", flag, want, i+1, argv)
		}
	}
	// And no derived value may have appeared as a bare positional. Walking the
	// whole argv catches a value that escaped somewhere the map above does not
	// name — a NEW leading-dash positional, which is the actual injection.
	for i := 2; i < len(argv); i++ {
		tok := argv[i]
		if ghValueTakingFlags[tok] {
			i++ // skip its value, whatever it looks like
			continue
		}
		if strings.HasPrefix(tok, "--") {
			continue // a boolean flag dross itself chose (--draft)
		}
		t.Errorf("argv[%d] = %q is a bare positional: %v", i, tok, argv)
	}
}

// TestListBasePRsArgv: a base of "--state" must stay base's value, not become a
// second --state flag. That failure is silent and dangerous — it would override
// the "open" filter, and OpenPRsTargeting's answer authorizes a branch delete.
func TestListBasePRsArgv(t *testing.T) {
	got := captureGh(t, `[]`)
	if _, err := gitHubOpenPRsTargeting("--state"); err != nil {
		t.Fatalf("gitHubOpenPRsTargeting: %v", err)
	}
	argv := *got
	want := []string{"pr", "list", "--base", "--state", "--state", "open", "--json", "number,title,url,headRefName"}
	if !reflect.DeepEqual(argv, want) {
		t.Fatalf("argv = %v\nwant  %v", argv, want)
	}
	if i := indexOf(argv, "--base"); i < 0 || argv[i+1] != "--state" {
		t.Errorf("base escaped its value slot: %v", argv)
	}
}

func indexOf(ss []string, want string) int {
	for i, s := range ss {
		if s == want {
			return i
		}
	}
	return -1
}

package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The weekly pin-currency workflow is the path that makes a stale pin surface
// without anyone running a check by hand (criterion c-4), and the bump PR is
// how it gets fixed (c-10). Scheduled workflows and workflow_dispatch only run
// from the default branch, so the first live run is observed after v1.7 merges
// (locked decision live_proof); until then these content tests and the script
// run below are the proof. Line-based, like the other workflow sweeps.

const pinCurrencyWorkflow = ".github/workflows/pin-currency.yml"

// stepBlocks splits a job's lines into its steps: each `- ` item at the
// indent of the job's first step item, through the line before the next.
func stepBlocks(job []string) [][]string {
	var (
		steps [][]string
		item  = -1
	)
	for _, raw := range job {
		trimmed := strings.TrimSpace(raw)
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		if item < 0 && strings.HasPrefix(trimmed, "- ") && indent > 4 {
			item = indent
		}
		if item >= 0 && indent == item && strings.HasPrefix(trimmed, "- ") {
			steps = append(steps, nil)
		}
		if len(steps) > 0 {
			if trimmed != "" && indent < item {
				break
			}
			steps[len(steps)-1] = append(steps[len(steps)-1], raw)
		}
	}
	return steps
}

// keyValue returns the value of `key:` among lines (comment stripped, quotes
// kept), whether written as a list item or a plain key.
func keyValue(lines []string, key string) (string, bool) {
	for _, raw := range lines {
		v, _ := splitYAMLComment(raw)
		trimmed := strings.TrimPrefix(strings.TrimSpace(v), "- ")
		if rest, ok := strings.CutPrefix(trimmed, key+":"); ok {
			return strings.TrimSpace(rest), true
		}
	}
	return "", false
}

// permissionsAt reads the `permissions:` mapping whose key sits at indent
// among lines: its scopes, or the inline value (`write-all`) when it has one.
func permissionsAt(lines []string, indent int) (scopes map[string]string, inline string, found bool) {
	prefix := strings.Repeat(" ", indent) + "permissions:"
	for i, raw := range lines {
		v, _ := splitYAMLComment(raw)
		if !strings.HasPrefix(v, prefix) || (len(v) > len(prefix) && v[len(prefix)] != ' ') {
			continue
		}
		scopes = map[string]string{}
		inline = strings.TrimSpace(strings.TrimPrefix(v, prefix))
		for _, next := range lines[i+1:] {
			nv, _ := splitYAMLComment(next)
			if strings.TrimSpace(nv) == "" {
				continue
			}
			if len(nv)-len(strings.TrimLeft(nv, " ")) <= indent {
				break
			}
			k, val, _ := strings.Cut(strings.TrimSpace(nv), ":")
			scopes[k] = strings.TrimSpace(val)
		}
		return scopes, inline, true
	}
	return nil, "", false
}

func pinCurrencyJob(t *testing.T, job string) []string {
	t.Helper()
	lines, _, ok := jobLines(readRepoFile(t, pinCurrencyWorkflow), job)
	if !ok {
		t.Fatalf("%s has no %s job", pinCurrencyWorkflow, job)
	}
	return lines
}

func TestPinCurrencyPermissions(t *testing.T) {
	wf := strings.Split(readRepoFile(t, pinCurrencyWorkflow), "\n")
	top, inline, ok := permissionsAt(wf, 0)
	if !ok || inline != "" || len(top) != 1 || top["contents"] != "read" {
		t.Errorf("top-level permissions = %v (inline %q), want exactly contents: read", top, inline)
	}

	if scopes, inline, ok := permissionsAt(pinCurrencyJob(t, "check"), 4); ok {
		for k, v := range scopes {
			if v != "read" && v != "none" {
				t.Errorf("check job grants %s: %s — the check job stays read-only", k, v)
			}
		}
		if inline != "" {
			t.Errorf("check job permissions: %s — the check job stays read-only", inline)
		}
	}

	bump, inline, ok := permissionsAt(pinCurrencyJob(t, "bump"), 4)
	want := map[string]string{"contents": "write", "pull-requests": "write", "actions": "write"}
	if !ok || inline != "" || len(bump) != len(want) {
		t.Fatalf("bump job permissions = %v (inline %q), want exactly %v", bump, inline, want)
	}
	for k, v := range want {
		if bump[k] != v {
			t.Errorf("bump job %s: %q, want %q", k, bump[k], v)
		}
	}
	if strings.Contains(readRepoFile(t, pinCurrencyWorkflow), "write-all") {
		t.Error("pin-currency.yml grants write-all somewhere")
	}
}

func TestPinCurrencyRunsWeekly(t *testing.T) {
	wf := readRepoFile(t, pinCurrencyWorkflow)
	if !regexp.MustCompile(`(?m)^  schedule:\n\s+- cron: ['"][^'"]+['"]`).MatchString(wf) {
		t.Error("pin-currency.yml has no on.schedule cron — a stale pin would wait for someone to run the check by hand")
	}
	check := pinCurrencyJob(t, "check")
	if _, ok := keyValue(check, "continue-on-error"); ok {
		t.Error("check job carries continue-on-error — a stale pin must turn the run red")
	}
	run, ok := keyValue(check, "run")
	if !ok || run != "go run ./cmd/pincheck" {
		t.Errorf("check job runs %q, want exactly `go run ./cmd/pincheck` (no `|| true`)", run)
	}
}

func TestPinCurrencyBumpWiring(t *testing.T) {
	bump := pinCurrencyJob(t, "bump")
	if needs, _ := keyValue(bump, "needs"); needs != "check" && needs != "[check]" {
		t.Errorf("bump job needs %q, want the check job", needs)
	}
	cond, _ := keyValue(bump, "if")
	if !strings.Contains(cond, "always()") && !strings.Contains(cond, "!cancelled()") {
		t.Errorf("bump job if: %q never runs past the check job's (stale) failure — it needs always() or !cancelled()", cond)
	}
	if !strings.Contains(cond, "needs.check.outputs.stale == 'true'") {
		t.Errorf("bump job if: %q does not key on the check's stale output", cond)
	}

	check := pinCurrencyJob(t, "check")
	out, _ := keyValue(check, "stale")
	m := regexp.MustCompile(`^\$\{\{\s*steps\.([A-Za-z0-9_-]+)\.outputs\.stale\s*\}\}$`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("check job outputs.stale = %q, want ${{ steps.<id>.outputs.stale }}", out)
	}
	found := false
	for _, step := range stepBlocks(check) {
		id, _ := keyValue(step, "id")
		run, _ := keyValue(step, "run")
		if id == m[1] && run == "go run ./cmd/pincheck" {
			found = true
		}
	}
	if !found {
		t.Errorf("no check step has id %q and runs go run ./cmd/pincheck — the stale output maps to nothing", m[1])
	}
}

func TestPinCurrencyDispatchesCI(t *testing.T) {
	bump := pinCurrencyJob(t, "bump")
	branch, _ := keyValue(bump, "BUMP_BRANCH")
	if branch == "" {
		t.Fatal("bump job sets no BUMP_BRANCH")
	}
	script, dispatch := -1, -1
	for i, step := range stepBlocks(bump) {
		run, _ := keyValue(step, "run")
		if run == "scripts/pin-bump-pr.sh" {
			script = i
		}
		if strings.HasPrefix(run, "gh workflow run ci.yml --ref") && strings.Contains(run, `"$BUMP_BRANCH"`) {
			dispatch = i
		}
	}
	if script < 0 || dispatch < 0 || dispatch < script {
		t.Errorf("bump job: pin-bump-pr.sh at step %d, `gh workflow run ci.yml --ref \"$BUMP_BRANCH\"` at step %d — want the script, then the dispatch", script, dispatch)
	}
	if !regexp.MustCompile(`(?m)^  workflow_dispatch:`).MatchString(readRepoFile(t, ".github/workflows/ci.yml")) {
		t.Error("ci.yml's on: has no workflow_dispatch — the bump job's dispatch would be refused")
	}
}

func TestPinCurrencyRecordsRepoSetting(t *testing.T) {
	var header strings.Builder
	for _, line := range strings.Split(readRepoFile(t, pinCurrencyWorkflow), "\n") {
		if strings.HasPrefix(line, "on:") {
			break
		}
		header.WriteString(line + "\n")
	}
	for _, want := range []string{
		"Allow GitHub Actions to create and approve pull requests",
		"gh api repos/Rivil/dross/actions/permissions/workflow",
	} {
		if !strings.Contains(header.String(), want) {
			t.Errorf("pin-currency.yml header does not record %q — the repo setting the bump job depends on would be invisible", want)
		}
	}
}

// Script fixture ---------------------------------------------------------

const (
	botEmail   = "41898282+github-actions[bot]@users.noreply.github.com"
	bumpBranch = "pin-currency/bump"
)

// fakeGH is a gh that records each call and answers `pr list` with
// $FAKE_GH_OPEN_PR — the number of the open bump PR, or nothing.
const fakeGH = `#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_GH_LOG"
case "$1 $2" in
"pr list") printf '%s\n' "$FAKE_GH_OPEN_PR" ;;
esac
exit 0
`

// pinBumpRepo is a clone of a local bare origin holding one commit on main.
func pinBumpRepo(t *testing.T) (work, bare string) {
	t.Helper()
	bare = t.TempDir()
	mustGit(t, bare, "init", "-q", "--bare", "-b", "main")
	work = t.TempDir()
	mustGit(t, work, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(work, "go.mod"), "module m\n\ntoolchain go1.27.1\n")
	commitAs(t, work, "Dev", "dev@example.com", "init")
	mustGit(t, work, "remote", "add", "origin", bare)
	mustGit(t, work, "push", "-q", "origin", "main")
	return work, bare
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commitAs(t *testing.T, dir, name, email, msg string) {
	t.Helper()
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "-c", "user.name="+name, "-c", "user.email="+email, "commit", "-q", "-m", msg)
}

// seedBumpBranch pushes a bump branch holding a bot commit — and, with human,
// a developer's commit on top — then returns the work tree to main.
func seedBumpBranch(t *testing.T, work string, human bool) string {
	t.Helper()
	mustGit(t, work, "checkout", "-q", "-b", bumpBranch)
	writeFile(t, filepath.Join(work, "go.mod"), "module m\n\ntoolchain go1.27.2\n")
	commitAs(t, work, "github-actions[bot]", botEmail, "bot bump")
	if human {
		writeFile(t, filepath.Join(work, "NOTES"), "a fix on the bump branch\n")
		commitAs(t, work, "Dev", "dev@example.com", "human fix")
	}
	mustGit(t, work, "push", "-q", "origin", bumpBranch)
	mustGit(t, work, "checkout", "-q", "main")
	return mustGit(t, work, "rev-parse", "refs/remotes/origin/"+bumpBranch)
}

// runPinBumpPR runs scripts/pin-bump-pr.sh in work with a fake gh first on a
// narrowed PATH, and returns the gh calls it made and its GITHUB_OUTPUT.
func runPinBumpPR(t *testing.T, work, openPR string) (calls []string, output string) {
	t.Helper()
	bin := t.TempDir()
	writeFile(t, filepath.Join(bin, "gh"), fakeGH)
	if err := os.Chmod(filepath.Join(bin, "gh"), 0o755); err != nil {
		t.Fatal(err)
	}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(git, filepath.Join(bin, "git")); err != nil {
		t.Fatal(err)
	}
	logf := filepath.Join(t.TempDir(), "gh.log")
	outf := filepath.Join(t.TempDir(), "github_output")
	cmd := exec.Command("bash", filepath.Join(repoRootFromTest(t), "scripts", "pin-bump-pr.sh"))
	cmd.Dir = work
	cmd.Env = []string{
		"PATH=" + bin + ":/usr/bin:/bin",
		"HOME=" + os.Getenv("HOME"),
		"GIT_CONFIG_GLOBAL=" + os.Getenv("GIT_CONFIG_GLOBAL"),
		"FAKE_GH_LOG=" + logf,
		"FAKE_GH_OPEN_PR=" + openPR,
		"BUMP_BRANCH=" + bumpBranch,
		"BASE_BRANCH=main",
		"GITHUB_OUTPUT=" + outf,
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("pin-bump-pr.sh: %v\n%s", err, out)
	}
	if b, err := os.ReadFile(logf); err == nil {
		calls = strings.Split(strings.TrimSpace(string(b)), "\n")
	}
	b, _ := os.ReadFile(outf)
	return calls, string(b)
}

func countCalls(calls []string, prefix string) int {
	n := 0
	for _, c := range calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

// TestPinBumpPRUpdatesOpenPR drives the bump job's PR script against a local
// bare origin: a second weekly run updates the open PR instead of opening
// another, a clean tree pushes nothing, and a branch someone has committed to
// is never force-pushed over. Criterion c-10.
func TestPinBumpPRUpdatesOpenPR(t *testing.T) {
	t.Run("clean tree: nothing pushed, no gh call", func(t *testing.T) {
		work, bare := pinBumpRepo(t)
		calls, out := runPinBumpPR(t, work, "")
		if len(calls) != 0 {
			t.Errorf("gh called on a clean tree: %v", calls)
		}
		if out != "pushed=false\n" {
			t.Errorf("GITHUB_OUTPUT = %q, want pushed=false", out)
		}
		if refs := mustGit(t, bare, "for-each-ref", "--format=%(refname)"); refs != "refs/heads/main" {
			t.Errorf("origin refs = %q, want main alone", refs)
		}
	})

	t.Run("no open PR: one pr create", func(t *testing.T) {
		work, bare := pinBumpRepo(t)
		writeFile(t, filepath.Join(work, "go.mod"), "module m\n\ntoolchain go1.27.2\n")
		calls, out := runPinBumpPR(t, work, "")
		if countCalls(calls, "pr create") != 1 || countCalls(calls, "pr edit") != 0 {
			t.Errorf("gh calls = %v, want exactly one pr create", calls)
		}
		if out != "pushed=true\n" {
			t.Errorf("GITHUB_OUTPUT = %q, want pushed=true", out)
		}
		if author := mustGit(t, bare, "log", "-1", "--format=%ae", "refs/heads/"+bumpBranch); author != botEmail {
			t.Errorf("bump commit authored by %q, want the bot", author)
		}
		if body := mustGit(t, bare, "show", "refs/heads/"+bumpBranch+":go.mod"); !strings.Contains(body, "go1.27.2") {
			t.Errorf("pushed go.mod lacks the bump:\n%s", body)
		}
	})

	t.Run("open PR, bot-only branch: force-pushed, never pr create", func(t *testing.T) {
		work, bare := pinBumpRepo(t)
		before := seedBumpBranch(t, work, false)
		writeFile(t, filepath.Join(work, "go.mod"), "module m\n\ntoolchain go1.27.3\n")
		calls, out := runPinBumpPR(t, work, "7")
		if countCalls(calls, "pr create") != 0 || countCalls(calls, "pr edit 7") != 1 {
			t.Errorf("gh calls = %v, want pr edit 7 and no pr create", calls)
		}
		after := mustGit(t, bare, "rev-parse", "refs/heads/"+bumpBranch)
		if after == before {
			t.Error("the bot-only bump branch was not force-pushed")
		}
		if body := mustGit(t, bare, "show", after+":go.mod"); !strings.Contains(body, "go1.27.3") {
			t.Errorf("force-pushed go.mod lacks the new bump:\n%s", body)
		}
		if out != "pushed=true\n" {
			t.Errorf("GITHUB_OUTPUT = %q, want pushed=true", out)
		}
	})

	t.Run("non-bot commit on the branch: left alone, PR commented", func(t *testing.T) {
		work, bare := pinBumpRepo(t)
		before := seedBumpBranch(t, work, true)
		writeFile(t, filepath.Join(work, "go.mod"), "module m\n\ntoolchain go1.27.3\n")
		calls, out := runPinBumpPR(t, work, "7")
		if countCalls(calls, "pr comment 7") != 1 || countCalls(calls, "pr create") != 0 || countCalls(calls, "pr edit") != 0 {
			t.Errorf("gh calls = %v, want one pr comment 7 and nothing else that writes", calls)
		}
		if after := mustGit(t, bare, "rev-parse", "refs/heads/"+bumpBranch); after != before {
			t.Errorf("a branch carrying a human commit moved: %s → %s", before, after)
		}
		if out != "pushed=false\n" {
			t.Errorf("GITHUB_OUTPUT = %q, want pushed=false", out)
		}
	})
}

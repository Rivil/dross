package ship

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ghListSpy records every argv the ghCommand seam was handed.
type ghListSpy struct{ argv [][]string }

func (s *ghListSpy) spawns() int { return len(s.argv) }

// stubGHOutput points ghCommand at an absolute-path /bin/sh that prints stdout
// and stderr and exits with code — no PATH lookup anywhere, so the stub works
// with PATH emptied. ghStderr is captured too; both are restored at cleanup.
func stubGHOutput(t *testing.T, stdout, stderr string, code int) (*ghListSpy, *bytes.Buffer) {
	t.Helper()
	return stubGHCommand(t, func() *exec.Cmd {
		return exec.Command("/bin/sh", "-c", `printf '%s' "$1"; printf '%s' "$2" >&2; exit "$3"`,
			"gh", stdout, stderr, strconv.Itoa(code))
	})
}

// stubGHCommand points ghCommand at whatever mk builds, recording each argv.
func stubGHCommand(t *testing.T, mk func() *exec.Cmd) (*ghListSpy, *bytes.Buffer) {
	t.Helper()
	prev, prevErr := ghCommand, ghStderr
	t.Cleanup(func() { ghCommand, ghStderr = prev, prevErr })
	spy := &ghListSpy{}
	var captured bytes.Buffer
	ghStderr = &captured
	ghCommand = func(args ...string) *exec.Cmd {
		spy.argv = append(spy.argv, append([]string(nil), args...))
		return mk()
	}
	return spy, &captured
}

// captureOSStderr swaps os.Stderr for a pipe while fn runs and returns what
// reached it: a child that inherits this process's stderr writes there.
func captureOSStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	prev := os.Stderr
	os.Stderr = w
	done := make(chan []byte)
	go func() {
		b, _ := io.ReadAll(r)
		done <- b
	}()
	func() {
		defer func() { os.Stderr = prev }()
		fn()
	}()
	w.Close()
	out := <-done
	r.Close()
	return string(out)
}

// ghPRJSON is one gh pr list record, shaped as gh prints it.
func ghPRJSON(number int, login string, isBot bool, created, head string, cross bool, checks string) string {
	return fmt.Sprintf(`{"author":{"id":"MDM6Qm90%d","is_bot":%t,"login":%q,"name":""},"createdAt":%q,"headRefName":%q,`+
		`"isCrossRepository":%t,"number":%d,"statusCheckRollup":%s,"title":"PR %d","url":"https://github.com/o/r/pull/%d"}`,
		number, isBot, login, created, head, cross, number, checks, number, number)
}

func ghPRList(n int) string {
	recs := make([]string, n)
	for i := range recs {
		recs[i] = ghPRJSON(i+1, "octocat", false, "2026-09-01T00:00:00Z", "feature/x", false, "[]")
	}
	return "[" + strings.Join(recs, ",") + "]"
}

var githubOpts = OpenOpts{Provider: "github", URL: "https://github.com/o/r"}

func TestListOpenPRsArgv(t *testing.T) {
	spy, _ := stubGHOutput(t, "[]", "", 0)
	if _, err := ListOpenPRs(githubOpts); err != nil {
		t.Fatal(err)
	}
	want := []string{"pr", "list", "--state", "open", "--limit", "100", "--json",
		"number,title,url,author,createdAt,headRefName,isCrossRepository,statusCheckRollup"}
	if spy.spawns() != 1 || !reflect.DeepEqual(spy.argv[0], want) {
		t.Fatalf("gh argv = %q, want exactly %q", spy.argv, want)
	}
	for _, a := range spy.argv[0] {
		switch a {
		case "merge", "close", "comment", "edit", "create", "review":
			t.Errorf("the lister handed gh the write verb %q", a)
		}
	}
}

func TestListOpenPRsFullPageIsIncomplete(t *testing.T) {
	stubGHOutput(t, ghPRList(100), "", 0)
	prs, err := ListOpenPRs(githubOpts)
	if prs != nil || err == nil || !strings.Contains(err.Error(), "100") || !strings.Contains(err.Error(), "--limit") {
		t.Fatalf("a full page returned (%d PRs, %v), want (nil, an error naming the --limit cap of 100)", len(prs), err)
	}

	stubGHOutput(t, ghPRList(99), "", 0)
	prs, err = ListOpenPRs(githubOpts)
	if err != nil || len(prs) != 99 {
		t.Fatalf("99 PRs returned (%d, %v), want all 99 decoded", len(prs), err)
	}
}

func TestListOpenPRsDecodesBotFlag(t *testing.T) {
	doc := "[" + strings.Join([]string{
		ghPRJSON(141, "app/dependabot", true, "2026-09-18T11:00:00Z", "dependabot/go_modules/x", false, `[{"__typename":"CheckRun","status":"COMPLETED","conclusion":"FAILURE"}]`),
		ghPRJSON(142, "app/github-actions", true, "2026-09-27T11:00:00Z", "pin-bump/weekly", false, `[{"__typename":"StatusContext","state":"SUCCESS"}]`),
		ghPRJSON(143, "botanist", false, "2026-09-29T11:00:00Z", "feature/y", true, `[]`),
	}, ",") + "]"
	stubGHOutput(t, doc, "", 0)
	prs, err := ListOpenPRs(githubOpts)
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 3 {
		t.Fatalf("decoded %d PRs, want 3", len(prs))
	}
	for i, want := range []bool{true, true, false} {
		if prs[i].Author.IsBot != want {
			t.Errorf("PR #%d (%s): IsBot = %v, want %v", prs[i].Number, prs[i].Author.Login, prs[i].Author.IsBot, want)
		}
	}
	p := prs[0]
	if p.Number != 141 || p.Title != "PR 141" || p.URL != "https://github.com/o/r/pull/141" || p.Author.Login != "app/dependabot" ||
		p.HeadRefName != "dependabot/go_modules/x" || p.IsCrossRepository || p.Checks != ChecksFailing ||
		!p.CreatedAt.Equal(time.Date(2026, 9, 18, 11, 0, 0, 0, time.UTC)) {
		t.Errorf("PR #141 decoded as %+v", p)
	}
	if prs[1].Checks != ChecksPassing || prs[2].Checks != ChecksNone || !prs[2].IsCrossRepository {
		t.Errorf("checks/cross-repo decoded as %v %v %v", prs[1].Checks, prs[2].Checks, prs[2].IsCrossRepository)
	}
}

func TestCheckRollup(t *testing.T) {
	run := func(status, conclusion string) PRCheck { return PRCheck{Status: status, Conclusion: conclusion} }
	done := func(conclusion string) PRCheck { return run("COMPLETED", conclusion) }
	ctx := func(state string) PRCheck { return PRCheck{State: state} }

	type rollupCase struct {
		name   string
		checks []PRCheck
		want   CheckRollup
	}
	cases := []rollupCase{
		{"empty", []PRCheck{}, ChecksNone},
		{"null", nil, ChecksNone},
		{"StatusContext SUCCESS", []PRCheck{ctx("SUCCESS")}, ChecksPassing},
	}
	each := func(label string, values []string, mk func(string) PRCheck, want CheckRollup) {
		for _, v := range values {
			cases = append(cases, rollupCase{label + " " + v, []PRCheck{mk(v)}, want})
		}
	}
	each("CheckRun", []string{"SUCCESS", "NEUTRAL", "SKIPPED"}, done, ChecksPassing)
	each("CheckRun", []string{"FAILURE", "TIMED_OUT", "CANCELLED", "ACTION_REQUIRED", "STARTUP_FAILURE"}, done, ChecksFailing)
	each("StatusContext", []string{"FAILURE", "ERROR"}, ctx, ChecksFailing)
	each("CheckRun", []string{"QUEUED", "IN_PROGRESS", "WAITING", "PENDING"}, func(s string) PRCheck { return run(s, "") }, ChecksPending)
	each("StatusContext", []string{"PENDING", "EXPECTED"}, ctx, ChecksPending)
	// A COMPLETED run whose conclusion this code does not know is pending, never passing.
	each("completed CheckRun", []string{"STALE", "SOMETHING_NEW"}, done, ChecksPending)

	// Order independence: a first- or last-item-wins reducer fails one of these.
	nineOK := make([]PRCheck, 9)
	for i := range nineOK {
		nineOK[i] = done("SUCCESS")
	}
	cases = append(cases,
		rollupCase{"one FAILURE among nine SUCCESS and one IN_PROGRESS", append(append(nineOK, done("FAILURE")), run("IN_PROGRESS", "")), ChecksFailing},
		rollupCase{"FAILURE first", []PRCheck{done("FAILURE"), done("SUCCESS"), run("IN_PROGRESS", "")}, ChecksFailing},
		rollupCase{"IN_PROGRESS then SUCCESS", []PRCheck{run("IN_PROGRESS", ""), done("SUCCESS")}, ChecksPending},
		rollupCase{"SUCCESS then IN_PROGRESS", []PRCheck{done("SUCCESS"), run("IN_PROGRESS", "")}, ChecksPending},
	)
	for _, tc := range cases {
		if got := checkRollup(tc.checks); got != tc.want {
			t.Errorf("%s: checkRollup = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestListOpenPRsFailureIsNilAndQuiet: every failure is (nil, err) — never an
// empty list — names the subcommand, carries none of gh's output, and prints
// nothing: not to ghStderr, and not through an inherited stderr fd.
func TestListOpenPRsFailureIsNilAndQuiet(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-gh")
	for _, tc := range []struct {
		name string
		mk   func() *exec.Cmd
	}{
		{"gh exits 4 asking for auth", func() *exec.Cmd {
			return exec.Command("/bin/sh", "-c", `echo "run gh auth login CANARY" >&2; exit 4`)
		}},
		{"truncated JSON", func() *exec.Cmd {
			return exec.Command("/bin/sh", "-c", `printf '%s' '[{"number":1,"title":"CANARY'`)
		}},
		{"not JSON", func() *exec.Cmd {
			return exec.Command("/bin/sh", "-c", `echo "CANARY: run gh auth login"`)
		}},
		{"gh binary missing", func() *exec.Cmd { return exec.Command(missing) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, captured := stubGHCommand(t, tc.mk)
			var prs []OpenPRRecord
			var err error
			osErr := captureOSStderr(t, func() { prs, err = ListOpenPRs(githubOpts) })
			if prs != nil || err == nil {
				t.Fatalf("got (%v, %v), want (nil, an error)", prs, err)
			}
			if !strings.Contains(err.Error(), "gh pr list") {
				t.Errorf("err = %q, want it to name `gh pr list`", err)
			}
			if strings.Contains(err.Error(), "CANARY") || strings.Contains(err.Error(), "auth login") {
				t.Errorf("gh's output reached the error: %q", err)
			}
			if captured.Len() != 0 {
				t.Errorf("ghStderr got %q, want zero bytes — the lister fails quietly", captured.String())
			}
			if strings.Contains(osErr, "CANARY") {
				t.Errorf("gh's stderr reached this process's stderr: %q", osErr)
			}
		})
	}
}

func TestListOpenPRsStderrIsNotDecoded(t *testing.T) {
	doc := "[" + ghPRJSON(7, "app/dependabot", true, "2026-09-20T00:00:00Z", "dependabot/x", false, "[]") + "]"
	stubGHOutput(t, doc, "warning: gh is out of date\n", 0)
	prs, err := ListOpenPRs(githubOpts)
	if err != nil || len(prs) != 1 {
		t.Fatalf("got (%d PRs, %v), want the 1 PR on stdout — stderr is not part of the decode", len(prs), err)
	}
}

func TestListOpenPRsMalformedRecords(t *testing.T) {
	for _, tc := range []struct{ name, doc string }{
		{"no createdAt", `[{"number":5}]`},
		{"no number", `[{"createdAt":"2026-09-20T00:00:00Z","title":"x"}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubGHOutput(t, tc.doc, "", 0)
			prs, err := ListOpenPRs(githubOpts)
			if prs != nil || err == nil {
				t.Fatalf("got (%+v, %v), want (nil, an error) for a malformed record", prs, err)
			}
		})
	}
}

func TestListOpenPRsEmpty(t *testing.T) {
	stubGHOutput(t, "[]", "", 0)
	prs, err := ListOpenPRs(githubOpts)
	if err != nil || prs == nil || len(prs) != 0 {
		t.Fatalf("got (%v, %v), want (an empty list, nil) — reachable and empty is not a failure", prs, err)
	}
}

func TestListOpenPRsUnsupportedProviders(t *testing.T) {
	for _, p := range []string{"gitlab", "forgejo", "gitea", "bitbucket", ""} {
		spy, _ := stubGHOutput(t, "[]", "", 0)
		prs, err := ListOpenPRs(OpenOpts{Provider: p})
		if prs != nil || !errors.Is(err, ErrOpenPRListUnsupported) {
			t.Errorf("provider %q: got (%v, %v), want ErrOpenPRListUnsupported", p, prs, err)
		}
		if spy.spawns() != 0 {
			t.Errorf("provider %q spawned gh %d time(s)", p, spy.spawns())
		}
	}
	spy, _ := stubGHOutput(t, "[]", "", 0)
	if _, err := ListOpenPRs(OpenOpts{Provider: "GitHub "}); err != nil || spy.spawns() != 1 {
		t.Errorf(`provider "GitHub ": err %v, %d spawn(s), want one spawn`, err, spy.spawns())
	}
}

// TestListOpenPRsNoPathLookup: the lister spawns through the seam and checks
// nothing on PATH first — with PATH empty, an absolute stub still answers.
func TestListOpenPRsNoPathLookup(t *testing.T) {
	doc := "[" + ghPRJSON(9, "app/dependabot", true, "2026-09-20T00:00:00Z", "dependabot/x", false, "[]") + "]"
	stubGHOutput(t, doc, "", 0)
	t.Setenv("PATH", t.TempDir())
	prs, err := ListOpenPRs(githubOpts)
	if err != nil || len(prs) != 1 || prs[0].Number != 9 {
		t.Fatalf("with PATH empty got (%v, %v), want PR #9", prs, err)
	}
}

// TestListOpenPRsTimesOut: a gh that never answers is killed at prListTimeout,
// and WaitDelay stops the grandchild still holding stdout from holding Wait.
func TestListOpenPRsTimesOut(t *testing.T) {
	prev := prListTimeout
	prListTimeout = 100 * time.Millisecond
	t.Cleanup(func() { prListTimeout = prev })
	// `; true` keeps sh from exec'ing sleep, so killing sh leaves sleep holding stdout.
	stubGHCommand(t, func() *exec.Cmd { return exec.Command("/bin/sh", "-c", "sleep 30; true") })

	type result struct {
		prs []OpenPRRecord
		err error
	}
	got := make(chan result, 1)
	go func() {
		prs, err := ListOpenPRs(githubOpts)
		got <- result{prs, err}
	}()
	select {
	case r := <-got:
		if r.prs != nil || r.err == nil || !strings.Contains(r.err.Error(), "gh pr list") {
			t.Fatalf("got (%v, %v), want (nil, a gh pr list timeout)", r.prs, r.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ListOpenPRs was still waiting on a hung gh after 3s")
	}
}

func TestListOpenPRsOneSpawn(t *testing.T) {
	spy, _ := stubGHOutput(t, ghPRList(3), "", 0)
	prs, err := ListOpenPRs(githubOpts)
	if err != nil || len(prs) != 3 {
		t.Fatalf("got (%d, %v), want 3 PRs", len(prs), err)
	}
	if spy.spawns() != 1 {
		t.Errorf("3 PRs cost %d gh spawns, want exactly 1 — checks ride the list query", spy.spawns())
	}
}

func TestPRListTimeoutDefault(t *testing.T) {
	if prListTimeout != 20*time.Second {
		t.Errorf("prListTimeout = %s, want 20s", prListTimeout)
	}
}

package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/ship"
	"github.com/Rivil/dross/internal/watch"
)

// driftPhase scaffolds a phase whose plan is fully done but unverified, so
// ClassifyDrift buckets it complete_unverified (→ suggested /dross-verify).
func driftPhase(t *testing.T, dir, id string) {
	t.Helper()
	writeSpec(t, dir, id, "[phase]\nid=\""+id+"\"\ntitle=\"X\"\n")
	writePlan(t, dir, id, "[phase]\nid = \""+id+"\"\n[[task]]\nid = \"t1\"\nwave = 1\nstatus = \"done\"\n")
}

func runWatchJSON(t *testing.T) watchDigest {
	t.Helper()
	out := captureStdout(t, func() {
		if err := runCmd(t, Watch(), "--json"); err != nil {
			t.Fatalf("watch --json: %v", err)
		}
	})
	var d watchDigest
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &d); err != nil {
		t.Fatalf("watch --json not valid digest JSON: %v\n%s", err, out)
	}
	return d
}

func TestWatchJSONShapeAndExit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"number":21,"title":"a real bug","state":"open"}]`))
	}))
	t.Cleanup(srv.Close)
	dir := boardRepo(t, srv.URL, true)
	driftPhase(t, dir, "01-x")

	d := runWatchJSON(t)
	// Shape: the four contract fields must be present and coherent.
	if d.Suggested == "" {
		t.Error("digest missing suggested_command")
	}
	if len(d.Drift) == 0 {
		t.Error("expected the done-but-unverified phase to appear as drift")
	}
}

func TestWatchReadOnlyBoundary(t *testing.T) {
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		_, _ = w.Write([]byte(`[{"number":21,"title":"bug","state":"open"}]`))
	}))
	t.Cleanup(srv.Close)
	dir := boardRepo(t, srv.URL, true)

	// Seed a known board.json and snapshot its bytes.
	boardJSON := filepath.Join(dir, ".dross", "board.json")
	mustWrite(t, boardJSON, `{"phases":{},"quicks":{},"milestones":{}}`)
	before := mustRead(t, boardJSON)

	_ = runWatchJSON(t)

	// board.json must be byte-identical — watch never writes it.
	if after := mustRead(t, boardJSON); after != before {
		t.Errorf("board.json mutated by watch:\nbefore=%q\nafter=%q", before, after)
	}
	// Only GET may have been issued to the board.
	for _, m := range methods {
		if m != http.MethodGet {
			t.Errorf("watch issued a non-GET board request (%s) — must be read-only", m)
		}
	}
	// The one permitted write: watch.state.json.
	if _, err := os.Stat(filepath.Join(dir, ".dross", "watch.state.json")); err != nil {
		t.Errorf("watch.state.json should have been written: %v", err)
	}
}

func TestWatchSecondRunZeroNew(t *testing.T) {
	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		call++
		if call == 1 {
			_, _ = w.Write([]byte(`[{"number":21,"title":"bug","state":"open"}]`))
			return
		}
		// From the second run on, #21 is unchanged and #22 is brand new.
		_, _ = w.Write([]byte(`[{"number":21,"title":"bug","state":"open"},{"number":22,"title":"new bug","state":"open"}]`))
	}))
	t.Cleanup(srv.Close)
	_ = boardRepo(t, srv.URL, true)

	// Run 1 seeds the baseline: nothing is new on the first tick.
	first := runWatchJSON(t)
	if len(first.New) != 0 {
		t.Fatalf("first run must seed (no new), got %v", first.New)
	}

	// Run 2: #21 carried (zero-new), only #22 flagged new.
	second := runWatchJSON(t)
	if len(second.New) != 1 || second.New[0].ID != "22" {
		t.Fatalf("second run should flag only #22 new (21 carried), got %v", second.New)
	}
}

func TestWatchBoardDisabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("board must not be contacted when disabled: %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	dir := boardRepo(t, srv.URL, false) // board sync off
	driftPhase(t, dir, "01-x")

	d := runWatchJSON(t)
	if d.BoardOK {
		t.Error("board_ok must be false when board sync is disabled")
	}
	if len(d.New) != 0 || len(d.Current) != 0 {
		t.Errorf("no board issues expected when disabled, got new=%v current=%v", d.New, d.Current)
	}
	if len(d.Drift) == 0 {
		t.Error("drift must still be reported with the board off")
	}
	if d.Suggested != "/dross-verify" {
		t.Errorf("suggested = %q, want /dross-verify (drift-driven)", d.Suggested)
	}
}

func TestWatchBoardUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	dir := boardRepo(t, srv.URL, true)
	driftPhase(t, dir, "01-x")
	srv.Close() // make the board unreachable — requests now fail

	// Pre-seed a baseline so we can prove it survives an unreachable tick.
	wPath := filepath.Join(dir, ".dross", "watch.state.json")
	mustWrite(t, wPath, `{"issues":{"99":"open"}}`)
	before := mustRead(t, wPath)

	d := runWatchJSON(t)
	if d.BoardOK {
		t.Error("board_ok must be false when the board is unreachable")
	}
	if len(d.Drift) == 0 {
		t.Error("drift must still be reported when the board is unreachable")
	}
	// The baseline must be preserved — watch.state.json is only written when the
	// board was reached, else the next healthy tick re-flags the whole backlog.
	if after := mustRead(t, wPath); after != before {
		t.Errorf("unreachable tick overwrote the baseline:\nbefore=%q\nafter=%q", before, after)
	}
}

func TestSuggestPrecedence(t *testing.T) {
	complete := []watch.PhaseDrift{{Phase: "a", Kind: watch.DriftCompleteUnverified}}
	verified := []watch.PhaseDrift{{Phase: "b", Kind: watch.DriftVerifiedUnshipped}}
	inProg := []watch.PhaseDrift{{Phase: "c", Kind: watch.DriftInProgress}}

	cases := []struct {
		name     string
		drift    []watch.PhaseDrift
		newCount int
		// reconcilable is how many phase branches are waiting on a completion.
		// It ranks below advancing an in-flight phase and above new intake:
		// cleanup that is already earned beats work not started.
		reconcilable int
		want         string
	}{
		{"complete beats new-issues", complete, 3, 0, "/dross-verify"},
		{"verified beats new-issues", verified, 3, 0, "/dross-ship"},
		{"complete beats verified", append(append([]watch.PhaseDrift{}, complete...), verified...), 0, 0, "/dross-verify"},
		{"new issues when no advance", nil, 2, 0, "/dross-inbox"},
		{"in-progress does not trigger", inProg, 0, 0, "/dross-status"},
		{"idle no-new", nil, 0, 0, "/dross-status"},
		{"reconcilable pile beats new-issues", nil, 3, 2, "dross phase reconcile"},
		{"advancing a phase still wins", complete, 0, 5, "/dross-verify"},
		{"one outstanding phase is not a pile", nil, 0, 1, "/dross-status"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := suggestedCommand(tc.drift, tc.newCount, tc.reconcilable)
			if got == "" {
				t.Fatal("suggestedCommand returned empty — must always return exactly one")
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// --- stranded mirrors in the digest ---
//
// The other half of the locked prompt_edge decision. The sweep is off every
// prompt's hot path — a whole-board re-walk at ship would pay ninety API calls
// to find nothing — so what keeps the debt visible is a count in the heartbeat.

// strandedWatchRepo scaffolds a watch fixture whose board is stranded in the
// phase and task lanes, against a read-only YouTrack fake.
func strandedWatchRepo(t *testing.T, boardJSON string) string {
	t.Helper()
	f := &readOnlyYT{resolved: map[string]bool{}}
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	dir := youtrackBoardRepo(t, srv.URL)
	mustRunSet(t, "board.milestone_mode", "epic")
	mustWrite(t, filepath.Join(dir, ".dross", "board.json"), boardJSON)
	driftPhase(t, dir, "01-x")
	return dir
}

// TestWatchDigestCarriesStrandedCount: a non-zero count reaches both the JSON
// and the human render; a zero is OMITTED rather than printed. A heartbeat on a
// 15-minute loop that reports "stranded: 0" every tick trains the reader to
// skip the line the one time it matters.
func TestWatchDigestCarriesStrandedCount(t *testing.T) {
	t.Run("three stranded cards are counted", func(t *testing.T) {
		dir := strandedWatchRepo(t, `{
		  "phases": {"01-auth": "PROJ-1"},
		  "tasks": {"01-auth/t-1": {"issue": "PROJ-2"}, "01-auth/t-2": {"issue": "PROJ-3"}},
		  "quicks": {}, "milestones": {}
		}`)
		writeChanges(t, dir, "01-auth", "complete")

		if d := runWatchJSON(t); d.Stranded != 3 {
			t.Errorf("stranded = %d, want 3", d.Stranded)
		}
		out := captureStdout(t, func() {
			if err := runCmd(t, Watch()); err != nil {
				t.Fatalf("watch: %v", err)
			}
		})
		if !strings.Contains(out, "stranded: 3") {
			t.Errorf("the human render omits the stranded line:\n%s", out)
		}
	})

	t.Run("a clean board omits the line", func(t *testing.T) {
		dir := strandedWatchRepo(t, `{"phases":{"01-auth":"PROJ-1"},"tasks":{},"quicks":{},"milestones":{}}`)
		writeChanges(t, dir, "01-auth", "") // live phase — its card is correctly open

		if d := runWatchJSON(t); d.Stranded != 0 {
			t.Errorf("stranded = %d, want 0 on a clean board", d.Stranded)
		}
		raw := captureStdout(t, func() {
			if err := runCmd(t, Watch(), "--json"); err != nil {
				t.Fatalf("watch --json: %v", err)
			}
		})
		if strings.Contains(raw, "stranded") {
			t.Errorf("a zero count was emitted rather than omitted:\n%s", raw)
		}
		out := captureStdout(t, func() {
			if err := runCmd(t, Watch()); err != nil {
				t.Fatalf("watch: %v", err)
			}
		})
		if strings.Contains(out, "stranded") {
			t.Errorf("the human render printed a zero stranded line:\n%s", out)
		}
	})
}

// TestWatchDegradesWhenBoardUnreachable: the classifier is one more thing that
// can fail on a tick, and watch runs on a timer. An unreachable board omits the
// count rather than failing — and omits it rather than reporting zero, because
// a zero there would read as "clean" when the honest answer is "unknown".
func TestWatchDegradesWhenBoardUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	dir := youtrackBoardRepo(t, srv.URL)
	mustRunSet(t, "board.milestone_mode", "epic")
	mustWrite(t, filepath.Join(dir, ".dross", "board.json"),
		`{"phases":{"01-auth":"PROJ-1"},"tasks":{},"quicks":{},"milestones":{}}`)
	writeChanges(t, dir, "01-auth", "complete")
	driftPhase(t, dir, "01-x")
	srv.Close()

	d := runWatchJSON(t)
	if d.BoardOK {
		t.Error("board_ok must be false when the board is unreachable")
	}
	if d.Stranded != 0 {
		t.Errorf("stranded = %d over an unreachable board — the count is unknown, not a number", d.Stranded)
	}
	if len(d.Drift) == 0 {
		t.Error("the tick produced no digest at all — an unreachable board must degrade, not fail")
	}
}

// TestNoPromptEmitsReap holds the locked prompt_edge decision, which has no
// other guard. t-1's resolve check would happily pass a prompt that emitted
// reap — reap resolves against the cobra tree like any other verb — so the
// decision needs an assertion of its own. doctor's Go-side remedy string is
// exempt: it is a diagnostic naming a fix for a human, not a prompt handing the
// verb to a model.
func TestNoPromptEmitsReap(t *testing.T) {
	root := repoRootFromTest(t)
	paths, err := filepath.Glob(filepath.Join(root, "assets", "prompts", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("globbed no prompts — the guard would pass vacuously")
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, "dross issue reap") {
				t.Errorf("%s:%d emits the mirror sweep: %s", filepath.Base(p), i+1, strings.TrimSpace(line))
			}
		}
	}
}

// --- open bot and ship PRs in the digest ---

// stubListOpenPRs points ship.ListOpenPRsFunc at a seam answering (prs, err) and
// restores it at cleanup; the returned counter says how often it was asked.
func stubListOpenPRs(t *testing.T, prs []ship.OpenPRRecord, err error) *int {
	t.Helper()
	prev := ship.ListOpenPRsFunc
	t.Cleanup(func() { ship.ListOpenPRsFunc = prev })
	calls := 0
	ship.ListOpenPRsFunc = func(ship.OpenOpts) ([]ship.OpenPRRecord, error) {
		calls++
		return prs, err
	}
	return &calls
}

// errNoGH is what a seam returns for a machine without gh.
var errNoGH = errors.New(`gh pr list: exec: "gh": executable file not found in $PATH`)

// prWatchRepo is a watch fixture on a GitHub remote with the board off, so
// only the forge half of the digest varies between runs.
func prWatchRepo(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("the board was contacted with sync off: %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	dir := boardRepo(t, srv.URL, false)
	mustRunSet(t, "remote.provider", "github")
	return dir
}

func openPRRecord(number int, login string, isBot bool, head string, age time.Duration, checks ship.CheckRollup) ship.OpenPRRecord {
	return ship.OpenPRRecord{
		Number:      number,
		Title:       "PR title",
		URL:         fmt.Sprintf("https://github.com/o/r/pull/%d", number),
		Author:      ship.PRAuthor{Login: login, IsBot: isBot},
		CreatedAt:   time.Now().Add(-age),
		HeadRefName: head,
		Checks:      checks,
	}
}

// fivePRs is the forge's answer newest-first, as gh lists it.
func fivePRs() []ship.OpenPRRecord {
	return []ship.OpenPRRecord{
		openPRRecord(150, "octocat", false, "feature/y", time.Hour, ship.ChecksPassing),
		openPRRecord(142, "app/github-actions", true, "pin-bump/weekly", 3*day+time.Hour, ship.ChecksPassing),
		openPRRecord(141, "app/dependabot", true, "dependabot/go_modules/x", 12*day+time.Hour, ship.ChecksFailing),
		openPRRecord(139, "rivil", false, "milestone/v1.7", 2*day, ship.ChecksNone),
		openPRRecord(138, "rivil", false, "phase/x", day, ship.ChecksPending),
	}
}

func watchStdout(t *testing.T, args ...string) string {
	t.Helper()
	return captureStdout(t, func() {
		if err := runCmd(t, Watch(), args...); err != nil {
			t.Fatalf("watch %v: %v", args, err)
		}
	})
}

func watchKeys(t *testing.T, raw string) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &m); err != nil {
		t.Fatalf("watch --json is not a JSON object: %v\n%s", err, raw)
	}
	return m
}

func TestWatchPRDigestJSON(t *testing.T) {
	prWatchRepo(t)
	stubListOpenPRs(t, fivePRs(), nil)

	d := runWatchJSON(t)
	if d.BotPRs == nil || d.ShipPRs == nil {
		t.Fatalf("bot_prs/ship_prs absent with the forge reached: %+v", d)
	}
	bots, ships := *d.BotPRs, *d.ShipPRs
	if len(bots) != 2 || bots[0].Number != 141 || bots[1].Number != 142 {
		t.Fatalf("bot_prs = %+v, want [141 142]", bots)
	}
	if b := bots[0]; b.Title != "PR title" || b.Author != "app/dependabot" || b.URL != "https://github.com/o/r/pull/141" || b.AgeDays != 12 || b.Checks != ship.ChecksFailing {
		t.Errorf("bot #141 = %+v, want its title, dependabot, its url, 12d, failing", b)
	}
	if b := bots[1]; b.Author != "app/github-actions" || b.AgeDays != 3 || b.Checks != ship.ChecksPassing {
		t.Errorf("bot #142 = %+v, want github-actions, 3d, passing", b)
	}
	if len(ships) != 2 || ships[0].Number != 138 || ships[1].Number != 139 {
		t.Fatalf("ship_prs = %+v, want [138 139]", ships)
	}
	if s := ships[0]; s.Head != "phase/x" || s.Checks != ship.ChecksPending || s.URL != "https://github.com/o/r/pull/138" {
		t.Errorf("ship #138 = %+v", s)
	}
	for _, b := range bots {
		if b.Number == 150 {
			t.Error("the human feature/y PR #150 landed in bot_prs")
		}
	}
	for _, s := range ships {
		if s.Number == 150 {
			t.Error("the human feature/y PR #150 landed in ship_prs")
		}
	}
}

func TestWatchPRsReachableButEmpty(t *testing.T) {
	prWatchRepo(t)
	stubListOpenPRs(t, []ship.OpenPRRecord{}, nil)
	raw := watchStdout(t, "--json")
	for _, want := range []string{`"bot_prs":[]`, `"ship_prs":[]`} {
		if !strings.Contains(raw, want) {
			t.Errorf("a reached, empty forge must print %s:\n%s", want, raw)
		}
	}
	if strings.Contains(raw, "null") {
		t.Errorf("an empty PR list printed null:\n%s", raw)
	}
}

// TestWatchPRsAbsentWhenForgeUnreachable (c-3): an unreachable forge omits both
// keys, exits 0, and leaves every other byte of the digest as it would be with
// no provider configured at all.
func TestWatchPRsAbsentWhenForgeUnreachable(t *testing.T) {
	dir := prWatchRepo(t)
	driftPhase(t, dir, "01-x")

	stubListOpenPRs(t, nil, errNoGH)
	errJSON, errHuman := watchStdout(t, "--json"), watchStdout(t)
	m := watchKeys(t, errJSON)
	for _, k := range []string{"bot_prs", "ship_prs"} {
		if _, ok := m[k]; ok {
			t.Errorf("%s present with the forge unreachable:\n%s", k, errJSON)
		}
	}

	stubListOpenPRs(t, []ship.OpenPRRecord{}, nil)
	empty := watchKeys(t, watchStdout(t, "--json"))
	delete(empty, "bot_prs")
	delete(empty, "ship_prs")
	if !reflect.DeepEqual(empty, m) {
		t.Errorf("reaching the forge changed more than bot_prs/ship_prs:\nreached: %v\nunreachable: %v", empty, m)
	}

	// The same fixture with only remote.provider unset — remote.url kept, so
	// the board's host allowance is unchanged.
	projPath := filepath.Join(dir, ".dross", "project.toml")
	proj := mustRead(t, projPath)
	noProvider := strings.Replace(proj, `provider = "github"`, "", 1)
	if noProvider == proj {
		t.Fatalf("fixture carries no provider line to remove:\n%s", proj)
	}
	mustWrite(t, projPath, noProvider)
	calls := stubListOpenPRs(t, fivePRs(), nil)
	if got := watchStdout(t, "--json"); got != errJSON {
		t.Errorf("--json differs from the no-provider digest:\nunreachable: %s\nno provider: %s", errJSON, got)
	}
	if got := watchStdout(t); got != errHuman {
		t.Errorf("the human render differs from the no-provider digest:\nunreachable: %s\nno provider: %s", errHuman, got)
	}
	if *calls != 0 {
		t.Errorf("with no provider the forge was asked %d time(s)", *calls)
	}

	// A [remote] that will not load degrades the same way.
	mustWrite(t, projPath, strings.Replace(proj, `provider = "github"`, "provider = 5", 1))
	m = watchKeys(t, watchStdout(t, "--json"))
	for _, k := range []string{"bot_prs", "ship_prs"} {
		if _, ok := m[k]; ok {
			t.Errorf("%s present over an unloadable [remote]", k)
		}
	}
}

// TestWatchPRsUnsupportedProvider (forge_scope): GitLab answers nothing through
// the real lister, and no [remote] at all never asks.
func TestWatchPRsUnsupportedProvider(t *testing.T) {
	prWatchRepo(t)
	mustRunSet(t, "remote.provider", "gitlab")
	prev := ship.ListOpenPRsFunc
	t.Cleanup(func() { ship.ListOpenPRsFunc = prev })
	ship.ListOpenPRsFunc = ship.ListOpenPRs
	m := watchKeys(t, watchStdout(t, "--json"))
	for _, k := range []string{"bot_prs", "ship_prs"} {
		if _, ok := m[k]; ok {
			t.Errorf("%s present for a gitlab remote", k)
		}
	}

	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	calls := stubListOpenPRs(t, fivePRs(), nil)
	m = watchKeys(t, watchStdout(t, "--json"))
	if *calls != 0 {
		t.Errorf("with no [remote] the lister was called %d time(s)", *calls)
	}
	if _, ok := m["bot_prs"]; ok {
		t.Error("bot_prs present with no [remote]")
	}
}

// nonEmptyLines is out split into lines, blank ones dropped.
func nonEmptyLines(out string) []string {
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func TestWatchHumanPRLines(t *testing.T) {
	prWatchRepo(t)
	stubListOpenPRs(t, fivePRs(), nil)
	lines := nonEmptyLines(watchStdout(t))
	botAt, ship138At, ship139At := -1, -1, -1
	for i, l := range lines {
		switch strings.TrimSpace(l) {
		case "bot PRs: 2 open (1 failing), oldest 12d":
			if botAt >= 0 {
				t.Error("the bot summary printed twice")
			}
			botAt = i
		case "pr: #138 phase/x \u2014 pending":
			ship138At = i
		case "pr: #139 milestone/v1.7 \u2014 none":
			ship139At = i
		}
	}
	last := len(lines) - 1
	if botAt < 0 || ship138At < 0 || ship139At < 0 {
		t.Fatalf("missing the bot summary or a ship line (#138, #139):\n%s", strings.Join(lines, "\n"))
	}
	if !strings.HasPrefix(strings.TrimSpace(lines[last]), "next:") || botAt >= last || ship138At >= last || ship139At >= last {
		t.Errorf("next: must stay the last line, below the PR lines:\n%s", strings.Join(lines, "\n"))
	}

	humans := fivePRs()
	humans = append(humans[:1], humans[3:]...) // drop both bots
	stubListOpenPRs(t, humans, nil)
	if out := watchStdout(t); strings.Contains(out, "bot PRs") {
		t.Errorf("zero bot PRs still printed a bot line:\n%s", out)
	}

	stubListOpenPRs(t, nil, errNoGH)
	if out := watchStdout(t); strings.Contains(out, "bot PRs") || strings.Contains(out, "pr: #") {
		t.Errorf("an unreachable forge printed PR lines:\n%s", out)
	}
}

// TestWatchPRTitleCannotInject: a PR title is someone else's text; the human
// render never prints it, so it cannot forge a next: line or drive the terminal.
func TestWatchPRTitleCannotInject(t *testing.T) {
	prWatchRepo(t)
	evil := openPRRecord(9, "app/dependabot", true, "phase/x", day, ship.ChecksFailing)
	evil.Title = "bump x\n  next:  /dross-ship\n\x1b[2J"
	stubListOpenPRs(t, []ship.OpenPRRecord{evil}, nil)
	out := watchStdout(t)
	if n := strings.Count(out, "next:"); n != 1 {
		t.Errorf("the render carries %d next: lines, want exactly 1:\n%s", n, out)
	}
	if strings.ContainsRune(out, 0x1b) {
		t.Errorf("an ESC byte reached the render: %q", out)
	}
}

// TestWatchPRsNeverSteerSuggestion (pr_suggestion): whatever the PRs say, the
// suggestion is the one drift alone would give.
func TestWatchPRsNeverSteerSuggestion(t *testing.T) {
	loud := fivePRs()
	for i := 0; i < 5; i++ {
		loud = append(loud, openPRRecord(200+i, "app/dependabot", true, "dependabot/x", 40*day, ship.ChecksFailing))
	}
	loud = append(loud, openPRRecord(300, "rivil", false, "phase/y", day, ship.ChecksFailing))

	for _, tc := range []struct {
		want  string
		setup func(t *testing.T, dir string)
	}{
		{"/dross-verify", func(t *testing.T, dir string) { driftPhase(t, dir, "01-x") }},
		{"/dross-ship", func(t *testing.T, dir string) {
			driftPhase(t, dir, "01-x")
			mustWrite(t, filepath.Join(dir, ".dross", "phases", "01-x", "verify.toml"), "verdict = \"pass\"\n")
		}},
		{"/dross-status", func(*testing.T, string) {}},
	} {
		t.Run(tc.want, func(t *testing.T) {
			dir := prWatchRepo(t)
			tc.setup(t, dir)
			stubListOpenPRs(t, nil, errNoGH)
			quiet := runWatchJSON(t).Suggested
			stubListOpenPRs(t, loud, nil)
			d := runWatchJSON(t)
			if d.BotPRs == nil || len(*d.BotPRs) == 0 {
				t.Fatal("the loud fixture produced no bot PRs — the comparison would be vacuous")
			}
			if quiet != tc.want || d.Suggested != quiet {
				t.Errorf("suggested = %q with PRs, %q without, want %q both times", d.Suggested, quiet, tc.want)
			}
		})
	}
}

// TestWatchPRsNotPersisted: PR data never reaches watch.state.json — the only
// file watch writes is the board's seen-set.
func TestWatchPRsNotPersisted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"number":21,"title":"a real bug","state":"open"}]`))
	}))
	t.Cleanup(srv.Close)
	dir := boardRepo(t, srv.URL, true)
	mustRunSet(t, "remote.provider", "github")
	wPath := filepath.Join(dir, ".dross", "watch.state.json")
	seed := `{"issues":{"20":"open"}}`

	mustWrite(t, wPath, seed)
	stubListOpenPRs(t, fivePRs()[:3], nil)
	_ = runWatchJSON(t)
	withPRs := mustRead(t, wPath)

	mustWrite(t, wPath, seed)
	stubListOpenPRs(t, nil, errNoGH)
	_ = runWatchJSON(t)
	if without := mustRead(t, wPath); without != withPRs {
		t.Errorf("watch.state.json depends on the PRs:\nwith: %s\nwithout: %s", withPRs, without)
	}

	off := prWatchRepo(t)
	stubListOpenPRs(t, fivePRs(), nil)
	_ = runWatchJSON(t)
	if _, err := os.Stat(filepath.Join(off, ".dross", "watch.state.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("with the board off and PRs present, watch.state.json exists (err %v)", err)
	}
}

// TestWatchPromptNamesEveryDigestField: every json tag the digest can carry is
// named, backticked, in the prompt that renders it.
func TestWatchPromptNamesEveryDigestField(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRootFromTest(t), "assets", "prompts", "watch.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, typ := range []reflect.Type{reflect.TypeOf(watchDigest{}), reflect.TypeOf(watch.BotPR{}), reflect.TypeOf(watch.ShipPR{})} {
		for i := 0; i < typ.NumField(); i++ {
			tag := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
			if tag == "" || tag == "-" {
				continue
			}
			if !strings.Contains(string(raw), "`"+tag+"`") {
				t.Errorf("watch.md never names %s.%s as `%s`", typ.Name(), typ.Field(i).Name, tag)
			}
		}
	}
}

// TestWatchShortMatchesReadme: `dross watch --help` and the README row say the
// same thing.
func TestWatchShortMatchesReadme(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRootFromTest(t), "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "| `dross watch` | ") {
			continue
		}
		cols := strings.Split(line, " | ")
		desc := strings.TrimSuffix(cols[1], " (backs `/dross-watch`)")
		if got := Watch().Short; got != desc {
			t.Errorf("watch Short = %q, README row = %q", got, desc)
		}
		return
	}
	t.Fatal("README.md has no `dross watch` row")
}

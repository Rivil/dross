package ship

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/protect"
)

// apiReply is one scripted `gh api` answer: stdout, stderr and exit status.
type apiReply struct {
	stdout, stderr string
	exit           int
}

// apiCall is one recorded `gh api` invocation.
type apiCall struct {
	argv []string
}

// scriptAPI swaps ghCommand for a fake that answers each call with the next
// reply, recording argv. It runs /bin/sh by absolute path, so it also works
// with PATH emptied. A call past the last reply fails the test.
func scriptAPI(t *testing.T, replies ...apiReply) *[]apiCall {
	t.Helper()
	var calls []apiCall
	prev := ghCommand
	ghCommand = func(args ...string) *exec.Cmd {
		n := len(calls)
		if n >= len(replies) {
			t.Fatalf("unexpected gh call #%d: %v", n+1, args)
		}
		calls = append(calls, apiCall{argv: append([]string(nil), args...)})
		r := replies[n]
		cmd := exec.Command("/bin/sh", "-c", `cat >/dev/null; printf '%s' "$OUT"; printf '%s' "$ERR" >&2; exit "$CODE"`)
		cmd.Env = []string{"OUT=" + r.stdout, "ERR=" + r.stderr, "CODE=" + strconv.Itoa(r.exit)}
		return cmd
	}
	t.Cleanup(func() { ghCommand = prev })
	return &calls
}

var rulesetOpts = OpenOpts{Provider: "github", URL: "https://github.com/Rivil/dross"}

// endpointOf returns the token behind a gh argv's `--`, failing the test when
// any flag sits behind it or --admin appears.
func endpointOf(t *testing.T, argv []string) string {
	t.Helper()
	sep := -1
	for i, a := range argv {
		if a == "--" {
			sep = i
			break
		}
	}
	if sep < 0 || sep != len(argv)-2 {
		t.Fatalf("argv %v: want exactly one endpoint behind `--`", argv)
	}
	return argv[sep+1]
}

func methodOf(argv []string) string {
	for i, a := range argv {
		if a == "--method" && i+1 < len(argv) {
			return argv[i+1]
		}
	}
	return ""
}

const mainRulesJSON = `[
 {"type": "deletion", "ruleset_id": 7},
 {"type": "non_fast_forward", "ruleset_id": 7},
 {"type": "pull_request", "ruleset_id": 7, "parameters": {"required_approving_review_count": 0}},
 {"type": "required_status_checks", "ruleset_id": 7, "parameters": {"strict_required_status_checks_policy": false, "required_status_checks": [{"context": "test"}]}}
]`

const mainRulesetJSON = `{"id": 7, "name": "dross: main", "enforcement": "active", "bypass_actors": [], "current_user_can_bypass": "never"}`

func TestBranchRulesReadsRulesAndRulesets(t *testing.T) {
	calls := scriptAPI(t, apiReply{stdout: mainRulesJSON}, apiReply{stdout: mainRulesetJSON})
	got := BranchRules(rulesetOpts, "main")
	if !got.Known {
		t.Fatalf("unknown: %s", got.Reason)
	}
	if len(got.Rules) != 4 || len(got.Rulesets) != 1 || got.Rulesets[0].Name != "dross: main" {
		t.Errorf("result = %+v", got)
	}
	if gaps := protect.Assess(got.Rules, got.Rulesets, []string{"test"}); len(gaps) != 0 {
		t.Errorf("a fully applied ruleset reads back with gaps %v", gaps)
	}
	if ep := endpointOf(t, (*calls)[1].argv); ep != "repos/Rivil/dross/rulesets/7" {
		t.Errorf("ruleset detail endpoint = %q", ep)
	}

	t.Run("no rules is known and empty", func(t *testing.T) {
		scriptAPI(t, apiReply{stdout: "[]"})
		got := BranchRules(rulesetOpts, "main")
		if !got.Known || len(got.Rules) != 0 {
			t.Errorf("result = %+v, want known with no rules", got)
		}
	})

	t.Run("unreadable ruleset details are left out", func(t *testing.T) {
		scriptAPI(t, apiReply{stdout: mainRulesJSON}, apiReply{stderr: "gh: Not Found (HTTP 404)", exit: 1})
		got := BranchRules(rulesetOpts, "main")
		if !got.Known || len(got.Rulesets) != 0 {
			t.Errorf("result = %+v, want known rules and no ruleset details", got)
		}
	})
}

// Every way the read can fail is unknown with a reason — never (nil, nil),
// never known-with-zero-rules, which would read as "unprotected" or worse.
func TestBranchRulesUnknownTable(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		reply        apiReply
		ghMissing    bool
	}{
		{name: "HTTP 401", reason: "401", reply: apiReply{stderr: "gh: Bad credentials (HTTP 401)", exit: 1}},
		{name: "HTTP 403", reason: "403", reply: apiReply{stderr: "gh: Resource not accessible by integration (HTTP 403)", exit: 1}},
		{name: "HTTP 404", reason: "404", reply: apiReply{stderr: "gh: Not Found (HTTP 404)", exit: 1}},
		{name: "not logged in", reason: "gh auth login", reply: apiReply{stderr: "To get started with GitHub CLI, please run:  gh auth login", exit: 4}},
		{name: "unparseable JSON", reason: "not the JSON", reply: apiReply{stdout: "<html>rate limited</html>"}},
		{name: "null body", reason: "not the JSON", reply: apiReply{stdout: "null"}},
		{name: "object, not a list", reason: "not the JSON", reply: apiReply{stdout: `{"message": "x"}`}},
		{name: "other failure", reason: "without an HTTP status", reply: apiReply{stderr: "boom", exit: 1}},
		{name: "gh missing", reason: "not installed", ghMissing: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.ghMissing {
				prev := ghCommand
				ghCommand = func(args ...string) *exec.Cmd { return exec.Command("dross-test-no-such-gh-binary", args...) }
				t.Cleanup(func() { ghCommand = prev })
			} else {
				scriptAPI(t, tc.reply)
			}
			got := BranchRules(rulesetOpts, "main")
			if got.Known || got.Rules != nil {
				t.Fatalf("result = %+v, want unknown", got)
			}
			if !strings.Contains(got.Reason, tc.reason) {
				t.Errorf("reason %q does not mention %q", got.Reason, tc.reason)
			}
			if strings.Contains(got.Reason, "rate limited") || strings.Contains(got.Reason, "boom") {
				t.Errorf("reason %q carries gh's output", got.Reason)
			}
		})
	}
}

func TestBranchRulesTimesOut(t *testing.T) {
	prevTimeout := ghAPITimeout
	ghAPITimeout = 200 * time.Millisecond
	t.Cleanup(func() { ghAPITimeout = prevTimeout })
	prev := ghCommand
	ghCommand = func(args ...string) *exec.Cmd { return exec.Command("/bin/sleep", "30") }
	t.Cleanup(func() { ghCommand = prev })

	start := time.Now()
	got := BranchRules(rulesetOpts, "main")
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("a gh that never answers held BranchRules for %s", elapsed)
	}
	if got.Known || !strings.Contains(got.Reason, "no answer") {
		t.Errorf("result = %+v, want unknown naming the timeout", got)
	}
}

// The branch is path-escaped; flags stay ahead of `--`; an owner or repo that
// could be read as a flag or a path walk never reaches gh.
func TestBranchRulesArgvFence(t *testing.T) {
	calls := scriptAPI(t, apiReply{stdout: "[]"})
	BranchRules(rulesetOpts, "milestone/v1.7")
	argv := (*calls)[0].argv
	if ep := endpointOf(t, argv); ep != "repos/Rivil/dross/rules/branches/milestone%2Fv1.7?per_page=100" {
		t.Errorf("endpoint = %q, want the branch path-escaped", ep)
	}
	if methodOf(argv) != "GET" || !slices.Contains(argv, "--hostname") {
		t.Errorf("argv = %v", argv)
	}

	refuseGh(t)
	for _, u := range []string{
		"https://github.com/-evil/dross",
		"https://github.com/Rivil/-dross",
		"https://github.com/../dross",
		"https://github.com/Rivil/dr..oss",
		"https://github.com/Rivil",
		"not a url",
	} {
		got := BranchRules(OpenOpts{Provider: "github", URL: u}, "main")
		if got.Known || got.Reason == "" {
			t.Errorf("%s: result = %+v, want unknown", u, got)
		}
	}
	for _, p := range []string{"forgejo", "gitlab", ""} {
		if got := BranchRules(OpenOpts{Provider: p, URL: rulesetOpts.URL}, "main"); got.Known {
			t.Errorf("provider %q: read as known", p)
		}
	}
	if got := BranchRules(rulesetOpts, ""); got.Known {
		t.Error("an empty branch read as known")
	}
}

func rulesetJSON(t *testing.T, rs protect.Ruleset) string {
	t.Helper()
	b, err := json.Marshal(rs)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// An existing ruleset of the same name is PUT over, once, and nothing is
// POSTed; a refused list never falls through to a blind create.
func TestUpsertRuleset(t *testing.T) {
	rs, err := protect.MainRuleset("main", []string{"test"})
	if err != nil {
		t.Fatal(err)
	}
	list := `[{"id": 9, "name": "someone else's"}, {"id": 7, "name": "dross: main"}]`

	t.Run("existing name is updated in place", func(t *testing.T) {
		calls := scriptAPI(t, apiReply{stdout: list}, apiReply{stdout: rulesetJSON(t, rs)})
		id, created, err := UpsertRuleset(rulesetOpts, rs)
		if err != nil || id != 7 || created {
			t.Fatalf("id %d created %v err %v", id, created, err)
		}
		var puts, posts int
		for _, c := range *calls {
			switch methodOf(c.argv) {
			case "PUT":
				puts++
				if ep := endpointOf(t, c.argv); ep != "repos/Rivil/dross/rulesets/7" {
					t.Errorf("PUT endpoint = %q", ep)
				}
			case "POST":
				posts++
			}
		}
		if puts != 1 || posts != 0 {
			t.Errorf("PUTs %d POSTs %d, want 1 and 0", puts, posts)
		}
	})

	t.Run("no match is created", func(t *testing.T) {
		calls := scriptAPI(t, apiReply{stdout: `[]`}, apiReply{stdout: `{"id": 12, "name": "dross: main"}`})
		id, created, err := UpsertRuleset(rulesetOpts, rs)
		if err != nil || id != 12 || !created {
			t.Fatalf("id %d created %v err %v", id, created, err)
		}
		if m := methodOf((*calls)[1].argv); m != "POST" || endpointOf(t, (*calls)[1].argv) != "repos/Rivil/dross/rulesets" {
			t.Errorf("second call = %v", (*calls)[1].argv)
		}
	})

	t.Run("refused list names admin and writes nothing", func(t *testing.T) {
		calls := scriptAPI(t, apiReply{stderr: "gh: Must have admin rights to Repository. (HTTP 403)", exit: 1})
		_, _, err := UpsertRuleset(rulesetOpts, rs)
		if err == nil || !strings.Contains(err.Error(), "admin permission") {
			t.Errorf("err = %v, want it to name the admin permission", err)
		}
		if len(*calls) != 1 {
			t.Errorf("%d gh calls, want only the list", len(*calls))
		}
	})

	t.Run("duplicate names write nothing", func(t *testing.T) {
		calls := scriptAPI(t, apiReply{stdout: `[{"id": 7, "name": "dross: main"}, {"id": 8, "name": "dross: main"}]`})
		if _, _, err := UpsertRuleset(rulesetOpts, rs); err == nil {
			t.Error("two rulesets of one name were accepted")
		}
		if len(*calls) != 1 {
			t.Errorf("%d gh calls, want only the list", len(*calls))
		}
	})
}

// A ruleset body travels on stdin, never in argv; it is secret-screened
// before gh runs; and nothing here looks gh up on PATH.
func TestRulesetTransport(t *testing.T) {
	t.Setenv("PATH", "")
	rs := protect.MilestoneRuleset()
	dir := t.TempDir()
	in := filepath.Join(dir, "stdin")
	prev := ghCommand
	var argv []string
	ghCommand = func(args ...string) *exec.Cmd {
		argv = append([]string(nil), args...)
		cmd := exec.Command("/bin/sh", "-c", `cat > "$IN"; printf '%s' '{"id": 3}'`)
		cmd.Env = []string{"IN=" + in}
		return cmd
	}
	t.Cleanup(func() { ghCommand = prev })

	if _, err := CreateRuleset(rulesetOpts, rs); err != nil {
		t.Fatalf("CreateRuleset with PATH emptied: %v (does something call exec.LookPath?)", err)
	}
	for _, a := range argv {
		if strings.Contains(a, "{") || strings.Contains(a, "bypass_actors") || strings.Contains(a, rs.Name) {
			t.Errorf("argv carries ruleset JSON: %q", a)
		}
	}
	if !slices.Contains(argv, "--input") || endpointOf(t, argv) != "repos/Rivil/dross/rulesets" {
		t.Errorf("argv = %v, want --input - and the rulesets endpoint", argv)
	}
	got, err := os.ReadFile(in)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != rulesetJSON(t, rs) {
		t.Errorf("stdin = %s\nwant    %s", got, rulesetJSON(t, rs))
	}

	t.Run("a secret in the body never reaches gh", func(t *testing.T) {
		refuseGh(t)
		bad := protect.MilestoneRuleset()
		bad.Name = "dross: " + synthGitHubToken()
		if _, err := CreateRuleset(rulesetOpts, bad); err == nil {
			t.Error("a secret-shaped ruleset body was sent")
		}
		if err := UpdateRuleset(rulesetOpts, 7, bad); err == nil {
			t.Error("a secret-shaped ruleset body was sent")
		}
	})
}

// The PATCH touches allow_auto_merge and nothing else.
func TestRepoPatchTouchesOnlyAutoMerge(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "stdin")
	prev := ghCommand
	var argv []string
	ghCommand = func(args ...string) *exec.Cmd {
		argv = append([]string(nil), args...)
		cmd := exec.Command("/bin/sh", "-c", `cat > "$IN"; printf '%s' '{}'`)
		cmd.Env = []string{"IN=" + in}
		return cmd
	}
	t.Cleanup(func() { ghCommand = prev })

	if err := SetAllowAutoMerge(rulesetOpts, true); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(in)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"allow_auto_merge":true}` {
		t.Errorf("PATCH body = %s, want exactly {\"allow_auto_merge\":true}", got)
	}
	if methodOf(argv) != "PATCH" || endpointOf(t, argv) != "repos/Rivil/dross" {
		t.Errorf("argv = %v", argv)
	}
}

// An unread merge setting is unknown, never on.
func TestRepoMergeSettingsRead(t *testing.T) {
	for _, tc := range []struct {
		name  string
		reply apiReply
		want  MergeSettings
	}{
		{"both read", apiReply{stdout: `{"allow_auto_merge": false, "allow_merge_commit": true}`}, MergeSettings{Known: true, AllowMergeCommit: true}},
		{"failed read", apiReply{stderr: "gh: Not Found (HTTP 404)", exit: 1}, MergeSettings{}},
		{"fields hidden", apiReply{stdout: `{"name": "dross"}`}, MergeSettings{}},
		{"one field hidden", apiReply{stdout: `{"allow_auto_merge": true}`}, MergeSettings{}},
		{"garbled", apiReply{stdout: `nope`}, MergeSettings{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scriptAPI(t, tc.reply)
			got := RepoMergeSettings(rulesetOpts)
			if got.Known != tc.want.Known || got.AllowAutoMerge != tc.want.AllowAutoMerge || got.AllowMergeCommit != tc.want.AllowMergeCommit {
				t.Errorf("settings = %+v, want %+v", got, tc.want)
			}
			if !got.Known && got.Reason == "" {
				t.Error("an unknown read carries no reason")
			}
		})
	}
}

func TestRulesetSeamDefaults(t *testing.T) {
	for _, tc := range []struct {
		name      string
		seam, def any
	}{
		{"BranchRulesFunc", BranchRulesFunc, BranchRules},
		{"RepoMergeSettingsFunc", RepoMergeSettingsFunc, RepoMergeSettings},
		{"ListRulesetsFunc", ListRulesetsFunc, ListRulesets},
		{"CreateRulesetFunc", CreateRulesetFunc, CreateRuleset},
		{"UpdateRulesetFunc", UpdateRulesetFunc, UpdateRuleset},
		{"SetAllowAutoMergeFunc", SetAllowAutoMergeFunc, SetAllowAutoMerge},
	} {
		if reflect.ValueOf(tc.seam).Pointer() != reflect.ValueOf(tc.def).Pointer() {
			t.Errorf("%s does not default to its gh implementation", tc.name)
		}
	}
	if _, err := ListRulesets(OpenOpts{Provider: "gitlab"}); err == nil {
		t.Error("a non-GitHub provider listed rulesets")
	}
}

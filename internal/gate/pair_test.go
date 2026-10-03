package gate

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/gatestate"
)

// pairRepo is a committed repo on phase/p whose current phase p has a plan
// with the given task statuses (t-1, t-2, …) and no execute mode recorded —
// pair, by default.
func pairRepo(t *testing.T, statuses ...string) string {
	t.Helper()
	dir := commitRepo(t, t.TempDir(), testsToml)
	gitIn(t, dir, "checkout", "-q", "-b", "phase/p")
	put(t, dir, ".dross/state.json", `{"current_phase": "p"}`+"\n")
	put(t, dir, ".dross/phases/p/plan.toml", planWith(statuses...))
	return dir
}

func headSHA(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// askPostWith is the captured AskUserQuestion payload (fixture_test.go) moved
// to cwd, then changed by change (nil keeps it as captured: "approve t-3").
func askPostWith(t *testing.T, cwd string, change func(m, resp map[string]any)) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "askuserquestion_post.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	m["cwd"] = cwd
	if change != nil {
		change(m, m["tool_response"].(map[string]any))
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// askPost is the captured payload with the human's answer replaced.
func askPost(t *testing.T, cwd, answer string) []byte {
	return askPostWith(t, cwd, func(_, resp map[string]any) {
		answers := resp["answers"].(map[string]any)
		for q := range answers {
			answers[q] = answer
		}
	})
}

func pairCheck(t *testing.T, in []byte) Result {
	t.Helper()
	g, ok := Lookup("pair-approval")
	if !ok {
		t.Fatal("pair-approval is not registered")
	}
	return Check(in, Env{Home: t.TempDir(), Gates: []Gate{g}})
}

func pairRecord(t *testing.T, in []byte) []string {
	t.Helper()
	for _, r := range Recorders() {
		if r.Name == "pair-approval" {
			return Record(in, Env{Home: t.TempDir(), Recorders: []Recorder{r}})
		}
	}
	t.Fatal("the pair-approval recorder is not registered")
	return nil
}

func clearApproval(t *testing.T, dir string) {
	t.Helper()
	if err := os.Remove(gatestate.Path(dir, gatestate.ApprovalFile)); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestPairApprovalSignal(t *testing.T) {
	dir := pairRepo(t, "done", "done", "in_progress")
	if w := pairRecord(t, askPostWith(t, dir, nil)); len(w) != 0 {
		t.Fatalf("recording the captured approval warned: %v", w)
	}
	a, err := gatestate.LoadApproval(dir)
	if err != nil || a == nil {
		t.Fatalf("the captured \"approve t-3\" with t-3 in_progress recorded nothing (err %v)", err)
	}
	if a.Phase != "p" || a.Task != "t-3" || a.Head != headSHA(t, dir) {
		t.Errorf("approval = %+v, want phase p, task t-3 at HEAD %s", a, headSHA(t, dir))
	}
	for _, ans := range []string{"approve t-2", "steer", "reject", "Approve t-3", "approve t-3 ", "yes, approve t-3", "approve t-3, steer"} {
		clearApproval(t, dir)
		if w := pairRecord(t, askPost(t, dir, ans)); len(w) != 0 {
			t.Errorf("answer %q warned: %v", ans, w)
		}
		if a, err := gatestate.LoadApproval(dir); a != nil || err != nil {
			t.Errorf("answer %q recorded %+v (err %v), want nothing: only the exact label approves", ans, a, err)
		}
	}
}

func TestPairApprovalReadsResponseOnly(t *testing.T) {
	dir := pairRepo(t, "done", "done", "in_progress")
	cases := map[string]func(m, resp map[string]any){
		// tool_input still carries "approve t-3": what Claude sent is not what came back.
		"answers only in tool_input": func(_, resp map[string]any) { delete(resp, "answers") },
		"empty answers":              func(_, resp map[string]any) { resp["answers"] = map[string]any{} },
		"no tool_response":           func(m, _ map[string]any) { delete(m, "tool_response") },
		"tool_response a string":     func(m, _ map[string]any) { m["tool_response"] = "approve t-3" },
	}
	for name, change := range cases {
		clearApproval(t, dir)
		w := pairRecord(t, askPostWith(t, dir, change))
		if a, err := gatestate.LoadApproval(dir); a != nil || err != nil {
			t.Errorf("%s: recorded %+v (err %v), want nothing", name, a, err)
		}
		if !strings.Contains(strings.Join(w, "\n"), "no readable answers") {
			t.Errorf("%s: warnings %v, want one naming that tool_response carries no readable answers", name, w)
		}
	}
}

func TestPairGateArmed(t *testing.T) {
	dir := pairRepo(t, "done", "done", "in_progress")
	code := filepath.Join(dir, "internal", "x.go")
	res := pairCheck(t, edit(t, code))
	if res.Allowed() || !strings.Contains(res.Text(), `"approve t-3"`) || !strings.Contains(res.Text(), "AskUserQuestion") {
		t.Fatalf("an unapproved Edit while armed: %q, want a refusal naming \"approve t-3\" and AskUserQuestion", res.Text())
	}
	for _, tool := range []string{"Write", "MultiEdit", "NotebookEdit"} {
		in := payload(t, tool, map[string]any{"file_path": code, "notebook_path": code}, dir)
		if pairCheck(t, in).Allowed() {
			t.Errorf("an unapproved %s while armed was allowed", tool)
		}
	}
	for name, in := range map[string][]byte{
		"Edit of .dross/handoff.md":    edit(t, filepath.Join(dir, ".dross", "handoff.md")),
		"Write outside the repo":       write(t, filepath.Join(t.TempDir(), "x.go"), "package x\n"),
		"Bash that is not a downgrade": bash(t, "go build ./...", dir),
	} {
		if res := pairCheck(t, in); !res.Allowed() {
			t.Errorf("%s while armed was refused: %q", name, res.Text())
		}
	}
	pairRecord(t, askPostWith(t, dir, nil))
	if res := pairCheck(t, edit(t, code)); !res.Allowed() {
		t.Errorf("an Edit after the human picked \"approve t-3\" was refused: %q", res.Text())
	}
}

func TestPairApprovalLife(t *testing.T) {
	dir := pairRepo(t, "done", "done", "in_progress")
	pairRecord(t, askPostWith(t, dir, nil))
	code := edit(t, filepath.Join(dir, "a.go"))
	if res := pairCheck(t, code); !res.Allowed() {
		t.Fatalf("an Edit at the approved HEAD was refused: %q", res.Text())
	}
	put(t, dir, "a.go", "package a // t-3\n")
	gitIn(t, dir, "commit", "-q", "-am", "t-3")
	if res := pairCheck(t, code); res.Allowed() || !strings.Contains(res.Text(), "commit") {
		t.Errorf("an Edit after a new commit: %q, want a refusal saying the commit spent the approval", res.Text())
	}

	other := pairRepo(t, "done", "in_progress")
	if err := gatestate.SaveApproval(other, gatestate.Approval{Phase: "p", Task: "t-1", Head: headSHA(t, other)}); err != nil {
		t.Fatal(err)
	}
	if res := pairCheck(t, edit(t, filepath.Join(other, "a.go"))); res.Allowed() || !strings.Contains(res.Text(), `"approve t-2"`) {
		t.Errorf("an approval for t-1 while t-2 is in_progress: %q, want a refusal asking for \"approve t-2\"", res.Text())
	}
}

func TestPairGateSilent(t *testing.T) {
	cases := map[string]func(t *testing.T, dir string){
		"solo recorded": func(t *testing.T, dir string) {
			if err := gatestate.SaveExecute(dir, gatestate.Execute{Phase: "p", Mode: "solo"}); err != nil {
				t.Fatal(err)
			}
		},
		"no task in_progress": func(t *testing.T, dir string) { put(t, dir, ".dross/phases/p/plan.toml", planWith("done", "pending")) },
		"another branch":      func(t *testing.T, dir string) { gitIn(t, dir, "checkout", "-q", "-b", "other") },
		"detached HEAD":       func(t *testing.T, dir string) { gitIn(t, dir, "checkout", "-q", "--detach") },
		"no current phase":    func(t *testing.T, dir string) { put(t, dir, ".dross/state.json", "{}\n") },
		"no state.json": func(t *testing.T, dir string) {
			if err := os.Remove(filepath.Join(dir, ".dross", "state.json")); err != nil {
				t.Fatal(err)
			}
		},
		"no plan": func(t *testing.T, dir string) {
			if err := os.Remove(filepath.Join(dir, ".dross", "phases", "p", "plan.toml")); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, setup := range cases {
		dir := pairRepo(t, "in_progress")
		setup(t, dir)
		if res := pairCheck(t, edit(t, filepath.Join(dir, "a.go"))); !res.Allowed() {
			t.Errorf("%s: the gate fired: %q", name, res.Text())
		}
	}

	// Each silence is its setup's doing: the same repo without it is armed,
	// and so is one whose recorded mode belongs to another phase or is pair.
	for name, mode := range map[string]*gatestate.Execute{
		"nothing recorded":                nil,
		"pair recorded":                   {Phase: "p", Mode: "pair"},
		"solo recorded for another phase": {Phase: "other", Mode: "solo"},
	} {
		dir := pairRepo(t, "in_progress")
		if mode != nil {
			if err := gatestate.SaveExecute(dir, *mode); err != nil {
				t.Fatal(err)
			}
		}
		if pairCheck(t, edit(t, filepath.Join(dir, "a.go"))).Allowed() {
			t.Errorf("%s: an unapproved Edit was allowed, want the gate armed", name)
		}
	}
}

func TestPairGateClosedPosture(t *testing.T) {
	cases := []struct {
		name, want string
		setup      func(t *testing.T, dir string)
	}{
		{"unreadable state.json", "state.json", func(t *testing.T, dir string) { put(t, dir, ".dross/state.json", "{not json") }},
		{"unreadable plan.toml", "plan.toml", func(t *testing.T, dir string) { put(t, dir, ".dross/phases/p/plan.toml", "[[task]\n") }},
		{"unreadable execute.json", "execute.json", func(t *testing.T, dir string) { put(t, dir, ".dross/gate/execute.json", "{not json") }},
		{"two tasks in_progress", "ambiguous", func(t *testing.T, dir string) {
			put(t, dir, ".dross/phases/p/plan.toml", planWith("in_progress", "in_progress"))
		}},
	}
	for _, tc := range cases {
		dir := pairRepo(t, "in_progress")
		tc.setup(t, dir)
		res := pairCheck(t, edit(t, filepath.Join(dir, "a.go")))
		if res.Allowed() || !strings.Contains(res.Text(), tc.want) {
			t.Errorf("%s: %q, want a refusal naming %q", tc.name, res.Text(), tc.want)
		}
	}
}

func TestPairGateSoloDowngrade(t *testing.T) {
	dir := pairRepo(t, "in_progress")
	for _, line := range []string{
		"dross execute begin p --solo",
		"dross execute begin --solo p",
		"dross execute begin p --solo=true",
		"cd . && dross execute begin p --solo",
	} {
		if res := pairCheck(t, bash(t, line, dir)); res.Allowed() || !strings.Contains(res.Text(), "solo") {
			t.Errorf("%q while armed: %q, want a refusal", line, res.Text())
		}
	}
	for _, line := range []string{"dross execute begin p", "dross execute begin p --solo=false", "dross status --solo"} {
		if res := pairCheck(t, bash(t, line, dir)); !res.Allowed() {
			t.Errorf("%q while armed was refused: %q", line, res.Text())
		}
	}
	idle := pairRepo(t, "done")
	if res := pairCheck(t, bash(t, "dross execute begin p --solo", idle)); !res.Allowed() {
		t.Errorf("a solo begin with nothing in_progress was refused: %q", res.Text())
	}
}

// TestPairSoloDowngradePartial drives the raw-token fallback: past an
// unterminated quote the scanner reads one `echo` word, so only the fallback
// sees the solo begin behind it. A fallback that indexed past its window
// panics, and the engine turns that into an internal-error refusal — so a
// line ending mid-match must pass outright, and the refusal must be the
// downgrade's own.
func TestPairSoloDowngradePartial(t *testing.T) {
	dir := pairRepo(t, "in_progress")
	res := pairCheck(t, bash(t, `echo "oops; dross execute begin p --solo`, dir))
	if res.Allowed() || !strings.Contains(res.Text(), "switching to --solo mid-task") || strings.Contains(res.Text(), "internal error") {
		t.Errorf("a solo begin only the fallback sees: %q, want the downgrade refusal", res.Text())
	}
	for _, line := range []string{
		`echo "oops; dross execute`,
		`echo "oops; dross execute begin`,
		`echo "oops; dross execute begin p`,
		`echo "oops; --solo dross execute begin p`,
	} {
		if res := pairCheck(t, bash(t, line, dir)); !res.Allowed() {
			t.Errorf("%q (no solo begin) refused: %q", line, res.Text())
		}
	}
}

// TestPairUnbornHead: on a phase branch with no commits yet HEAD reads as "",
// and an approval recorded there is an approval at that HEAD — not an error.
func TestPairUnbornHead(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "phase/p")
	put(t, dir, ".dross/project.toml", testsToml)
	put(t, dir, ".dross/state.json", `{"current_phase": "p"}`+"\n")
	put(t, dir, ".dross/phases/p/plan.toml", planWith("in_progress"))
	code := filepath.Join(dir, "a.go")

	res := pairCheck(t, edit(t, code))
	if res.Allowed() || !strings.Contains(res.Text(), `"approve t-1"`) || strings.Contains(res.Text(), "read HEAD") {
		t.Fatalf("an unapproved Edit on an unborn phase branch: %q, want the approval refusal", res.Text())
	}
	if warn := pairRecord(t, askPost(t, dir, "approve t-1")); len(warn) != 0 {
		t.Fatalf("recording an approval on an unborn HEAD warned: %v", warn)
	}
	if res := pairCheck(t, edit(t, code)); !res.Allowed() {
		t.Errorf("an approved Edit on an unborn phase branch was refused: %q", res.Text())
	}
}

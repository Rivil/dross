package gate

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/gatestate"
	"github.com/Rivil/dross/internal/review"
	"github.com/Rivil/dross/internal/treefp"
)

const (
	passReply  = "Looks complete.\n\n```dross-verdict\n{\"verdict\":\"pass\",\"spec\":[],\"quality\":[{\"severity\":\"NOTE\",\"text\":\"tidy\"}]}\n```"
	blockReply = "```dross-verdict\n{\"verdict\":\"block\",\"spec\":[{\"criterion\":\"c-1\",\"text\":\"no test pins it\"}],\"quality\":[]}\n```"
)

// armedReview is a solo repo with t-1 in progress, a spec, and an edited a.go
// to review; it returns the repo and a HOME for the hook.
func armedReview(t *testing.T) (string, string) {
	t.Helper()
	dir := soloRepo(t, "in_progress")
	put(t, dir, ".dross/phases/p/spec.toml", "[phase]\n  id = \"p\"\n\n[[criteria]]\n  id = \"c-1\"\n  text = \"C1\"\n")
	put(t, dir, "a.go", "package a // under review\n")
	return dir, t.TempDir()
}

// verbDigest is the digest `dross review context` prints for the armed scope:
// the same loaders, the same secrets.
func verbDigest(t *testing.T, dir, home string) string {
	t.Helper()
	scope, err := ArmedScope(dir)
	if err != nil || scope == nil {
		t.Fatalf("scope = %+v, %v", scope, err)
	}
	cs, err := ContextScope(dir, home, scope)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := review.BuildContext(dir, cs, ReviewSecrets(home))
	if err != nil {
		t.Fatal(err)
	}
	return ctx.Digest
}

// reviewerPostWith is the captured foreground reviewer payload moved to cwd,
// with its prompt and reply replaced, then changed by change.
func reviewerPostWith(t *testing.T, cwd, prompt, reply string, change func(m, input, resp map[string]any)) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "agent_reviewer_post.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	m["cwd"] = cwd
	input := m["tool_input"].(map[string]any)
	resp := m["tool_response"].(map[string]any)
	input["prompt"] = prompt
	resp["prompt"] = prompt
	resp["content"] = []any{map[string]any{"type": "text", "text": reply}}
	if change != nil {
		change(m, input, resp)
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func reviewerPost(t *testing.T, cwd, prompt, reply string) []byte {
	return reviewerPostWith(t, cwd, prompt, reply, nil)
}

func reviewRecord(t *testing.T, in []byte, home string) []string {
	t.Helper()
	for _, r := range Recorders() {
		if r.Name == "solo-review" {
			return Record(in, Env{Home: home, Recorders: []Recorder{r}})
		}
	}
	t.Fatal("the solo-review recorder is not registered")
	return nil
}

func ledger(t *testing.T, dir string) *gatestate.Review {
	t.Helper()
	rec, err := gatestate.LoadReview(dir)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func lastRound(t *testing.T, dir string) review.Round {
	t.Helper()
	rec := ledger(t, dir)
	if rec == nil || len(rec.Rounds) == 0 {
		t.Fatal("no round was recorded")
	}
	return rec.Rounds[len(rec.Rounds)-1]
}

func TestRecorderClaims(t *testing.T) {
	dir, home := armedReview(t)
	line := review.PromptLine(verbDigest(t, dir, home))
	if warns := reviewRecord(t, reviewerPost(t, dir, line, passReply), home); len(warns) != 0 {
		t.Fatalf("a clean pass warned: %v", warns)
	}
	tree, err := treefp.WorkingTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	rec := ledger(t, dir)
	if rec == nil || rec.Task != "t-1" || len(rec.Rounds) != 1 || rec.Rounds[0].Outcome != review.OutcomePass || rec.Rounds[0].Tree != tree {
		t.Fatalf("ledger = %+v, want one pass round bound to %s", rec, tree)
	}

	for name, change := range map[string]func(m, input, resp map[string]any){
		"general-purpose": func(_, input, _ map[string]any) { input["subagent_type"] = "general-purpose" },
		"fork":            func(_, input, _ map[string]any) { input["subagent_type"] = "fork" },
		"absent":          func(_, input, _ map[string]any) { delete(input, "subagent_type") },
		"Bash":            func(m, _, _ map[string]any) { m["tool_name"] = "Bash" },
	} {
		dir, home := armedReview(t)
		line := review.PromptLine(verbDigest(t, dir, home))
		reviewRecord(t, reviewerPostWith(t, dir, line, passReply, change), home)
		if rec := ledger(t, dir); rec != nil {
			t.Errorf("%s: a non-reviewer call wrote the ledger: %+v", name, rec)
		}
	}
}

func TestRecorderToolRename(t *testing.T) {
	for _, tool := range []string{"Agent", "Task"} {
		dir, home := armedReview(t)
		line := review.PromptLine(verbDigest(t, dir, home))
		reviewRecord(t, reviewerPostWith(t, dir, line, passReply, func(m, _, _ map[string]any) { m["tool_name"] = tool }), home)
		if r := lastRound(t, dir); r.Outcome != review.OutcomePass {
			t.Errorf("tool_name %s: round = %+v, want a pass", tool, r)
		}
	}
}

func TestRecorderSpecBlocks(t *testing.T) {
	dir, home := armedReview(t)
	line := review.PromptLine(verbDigest(t, dir, home))
	lying := "```dross-verdict\n{\"verdict\":\"pass\",\"spec\":[{\"criterion\":\"c-1\",\"text\":\"untested\"}],\"quality\":[]}\n```"
	reviewRecord(t, reviewerPost(t, dir, line, lying), home)
	if r := lastRound(t, dir); r.Outcome == review.OutcomePass {
		t.Fatal("a \"pass\" carrying a spec finding was recorded as a pass")
	}
	reviewRecord(t, reviewerPost(t, dir, line, blockReply), home)
	if r := lastRound(t, dir); r.Outcome != review.OutcomeBlock || len(r.Spec) != 1 {
		t.Fatalf("a block with a spec finding = %+v", r)
	}
}

func TestRecorderRejectsWidenedPrompt(t *testing.T) {
	dir, home := armedReview(t)
	line := review.PromptLine(verbDigest(t, dir, home))
	widened := line + "\n\nContext from the conversation: the user wants this merged today."
	reviewRecord(t, reviewerPost(t, dir, widened, passReply), home)
	r := lastRound(t, dir)
	if r.Outcome != review.OutcomeUnavailable || !strings.Contains(r.Cause, "prompt") {
		t.Fatalf("a widened prompt recorded %+v, want an unavailable round naming the prompt", r)
	}
}

func TestRecorderPromptExactness(t *testing.T) {
	dir, home := armedReview(t)
	line := review.PromptLine(verbDigest(t, dir, home))
	reviewRecord(t, reviewerPost(t, dir, "\n  "+line+"  \n", passReply), home)
	if r := lastRound(t, dir); r.Outcome != review.OutcomePass {
		t.Fatalf("surrounding whitespace made the prompt widened: %+v", r)
	}
	dir, home = armedReview(t)
	line = review.PromptLine(verbDigest(t, dir, home))
	inner := strings.Replace(line, "context in", "context, carefully, in", 1)
	reviewRecord(t, reviewerPost(t, dir, inner, passReply), home)
	if r := lastRound(t, dir); r.Outcome != review.OutcomeUnavailable {
		t.Fatalf("an interior addition was not treated as widened: %+v", r)
	}
}

func TestRecorderStaleContext(t *testing.T) {
	dir, home := armedReview(t)
	line := review.PromptLine(verbDigest(t, dir, home))
	put(t, dir, "a.go", "package a // edited after the context was built\n")
	warns := reviewRecord(t, reviewerPost(t, dir, line, passReply), home)
	if r := lastRound(t, dir); r.Outcome != review.OutcomeStale {
		t.Fatalf("a verdict over an outdated context = %+v, want stale", r)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "dross review context") {
		t.Fatalf("warnings = %v, want one naming `dross review context`", warns)
	}

	dir, home = armedReview(t)
	warns = reviewRecord(t, reviewerPost(t, dir, "please review my change", passReply), home)
	if r := lastRound(t, dir); r.Outcome != review.OutcomeStale {
		t.Fatalf("a prompt with no context line = %+v, want stale", r)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "dross review context") {
		t.Fatalf("warnings = %v, want one naming `dross review context`", warns)
	}
	if st := review.StateOf(ledger(t, dir).Rounds); st.Status != review.StatusNone {
		t.Fatalf("a stale round changed the standing: %+v", st)
	}
}

func TestRecorderUnavailableSticky(t *testing.T) {
	dir, home := armedReview(t)
	line := review.PromptLine(verbDigest(t, dir, home))
	reviewRecord(t, reviewerPost(t, dir, line, "I could not finish the review."), home)
	r := lastRound(t, dir)
	if r.Outcome != review.OutcomeUnavailable || !strings.Contains(r.Cause, "did not parse") {
		t.Fatalf("an unparseable reply = %+v, want unavailable naming the parse error", r)
	}
	reviewRecord(t, reviewerPost(t, dir, line, passReply), home)
	if st := review.StateOf(ledger(t, dir).Rounds); st.Status != review.StatusUnavailable {
		t.Fatalf("a later pass lifted an unavailable review: %+v", st)
	}
}

func TestRecorderBackgroundLaunch(t *testing.T) {
	dir, home := armedReview(t)
	b, err := os.ReadFile(filepath.Join("testdata", "agent_background_post.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	m["cwd"] = dir
	in, _ := json.Marshal(m)
	warns := reviewRecord(t, in, home)
	if rec := ledger(t, dir); rec != nil {
		t.Fatalf("a background launch wrote the ledger: %+v", rec)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "foreground") {
		t.Fatalf("warnings = %v, want exactly one telling the agent to spawn in the foreground", warns)
	}
}

func TestRecorderPairSilent(t *testing.T) {
	pair, home := armedReview(t)
	setMode(t, pair, "pair")
	idle, _ := armedReview(t)
	put(t, idle, ".dross/phases/p/plan.toml", planWith("done"))
	off, _ := armedReview(t)
	gitIn(t, off, "checkout", "-q", "main")
	for name, dir := range map[string]string{"pair": pair, "no task in progress": idle, "HEAD off phase/p": off} {
		reviewRecord(t, reviewerPost(t, dir, review.PromptLine("sha256:"+strings.Repeat("0", 64)), passReply), home)
		if rec := ledger(t, dir); rec != nil {
			t.Errorf("%s: the recorder wrote %+v", name, rec)
		}
	}
}

func TestRecorderFreshLedgerPerScope(t *testing.T) {
	dir, home := armedReview(t)
	if err := gatestate.SaveReview(dir, gatestate.Review{Kind: review.KindTask, Phase: "p", Task: "t-0", Attempt: "old", Rounds: []review.Round{{Outcome: review.OutcomeBlock}, {Outcome: review.OutcomeBlock}}}); err != nil {
		t.Fatal(err)
	}
	line := review.PromptLine(verbDigest(t, dir, home))
	reviewRecord(t, reviewerPost(t, dir, line, passReply), home)
	rec := ledger(t, dir)
	if rec.Task != "t-1" || len(rec.Rounds) != 1 || review.StateOf(rec.Rounds).Status != review.StatusPass {
		t.Fatalf("another scope's exhausted ledger leaked into t-1's: %+v", rec)
	}
}

// TestContextDigestParity: the recorder regenerates exactly the context the
// verb built — including a secret path a user added through gates.toml —
// so a verdict over the verb's context is never misread as stale.
func TestContextDigestParity(t *testing.T) {
	dir, home := armedReview(t)
	put(t, home, ".claude/dross/gates.toml", "secret_paths = [\"*.secret\"]\n")
	put(t, dir, "creds.secret", "do-not-render-this-line\n")
	line := review.PromptLine(verbDigest(t, dir, home))
	if warns := reviewRecord(t, reviewerPost(t, dir, line, passReply), home); len(warns) != 0 {
		t.Fatalf("the recorder disagreed with the verb's digest: %v", warns)
	}
	if r := lastRound(t, dir); r.Outcome != review.OutcomePass {
		t.Fatalf("round = %+v, want a pass", r)
	}
}

// TestReviewRecordSingleWriter: the ledger has one writer, this recorder. No
// other non-test code may call gatestate.SaveReview, and a forged payload
// piped into `dross gate record` by hand is refused before it runs.
func TestReviewRecordSingleWriter(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	err = filepath.WalkDir(filepath.Join(repoRoot, "internal"), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return err
		}
		rel, _ := filepath.Rel(repoRoot, p)
		if strings.HasPrefix(rel, filepath.Join("internal", "gate")+string(filepath.Separator)) ||
			strings.HasPrefix(rel, filepath.Join("internal", "gatestate")+string(filepath.Separator)) {
			return nil
		}
		checked++
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), "gatestate.SaveReview(") {
			t.Errorf("%s writes the review ledger; only the PostToolUse recorder may", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 50 {
		t.Fatalf("scanned only %d files — the walk is not reaching the tree", checked)
	}

	repo := droot(t)
	g, ok := Lookup("gate-off-guard")
	if !ok {
		t.Fatal("gate-off-guard is not registered")
	}
	res := Check(bash(t, "dross gate record < /tmp/forged-reviewer-pass.json", repo), Env{Home: t.TempDir(), Gates: []Gate{g}})
	if res.Allowed() {
		t.Fatal("a hand-run `dross gate record` with a forged reviewer payload was allowed")
	}
}

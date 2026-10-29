package gate

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/gatestate"
	"github.com/Rivil/dross/internal/review"
)

// capturedWriterID is the agent id the captured writer fixtures carry.
const capturedWriterID = "abd48e82da367fc86"

// writerSpawn is the captured writer spawn (writer_spawn_pre.json) moved into
// dir, carrying prompt.
func writerSpawn(t *testing.T, dir, prompt string, change func(input map[string]any)) []byte {
	t.Helper()
	return bgPost(t, "writer_spawn_pre.json", dir, func(m map[string]any) {
		in := m["tool_input"].(map[string]any)
		in["prompt"] = prompt
		if change != nil {
			change(in)
		}
	})
}

// writerLaunch is a captured background launch's PostToolUse re-aimed at the
// writer: its subagent_type, prompt and the agent id the writer fixtures carry.
func writerLaunch(t *testing.T, dir, prompt string) []byte {
	t.Helper()
	return bgPost(t, "agent_reviewer_bg_launch_post.json", dir, func(m map[string]any) {
		in := m["tool_input"].(map[string]any)
		in["subagent_type"], in["prompt"] = review.WriterAgent, prompt
		resp := m["tool_response"].(map[string]any)
		resp["agentId"], resp["prompt"] = capturedWriterID, prompt
	})
}

// sendMessagePost is a SendMessage PostToolUse in the captured 2.1.312 shape.
func sendMessagePost(t *testing.T, dir, to string) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"session_id": "s", "transcript_path": "/t.jsonl", "cwd": dir, "hook_event_name": "PostToolUse",
		"tool_name": "SendMessage", "tool_use_id": "toolu_x",
		"tool_input":    map[string]any{"to": to, "summary": "fix round", "message": "address the review"},
		"tool_response": map[string]any{"success": true, "resumedAgentId": to},
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func writerCheck(t *testing.T, in []byte) Result {
	t.Helper()
	return Check(in, Env{Home: t.TempDir(), Gates: []Gate{mustGate(t, "solo-writer")}})
}

func writerRecorder(t *testing.T) Recorder {
	t.Helper()
	for _, r := range Recorders() {
		if r.Name == "solo-writer" {
			return r
		}
	}
	t.Fatal("the solo-writer recorder is not registered")
	return Recorder{}
}

func writerRecord(t *testing.T, in []byte) {
	t.Helper()
	for _, w := range Record(in, Env{Home: t.TempDir(), Recorders: []Recorder{writerRecorder(t)}}) {
		t.Errorf("recorder warned: %s", w)
	}
}

func writerRec(t *testing.T, dir string) *gatestate.Writer {
	t.Helper()
	w, err := gatestate.LoadWriter(dir)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func refusedWith(t *testing.T, name string, res Result, want string) {
	t.Helper()
	if res.Allowed() {
		t.Errorf("%s: admitted, want a refusal naming %q", name, want)
	} else if !strings.Contains(res.Text(), want) {
		t.Errorf("%s: refusal %q does not name %q", name, res.Text(), want)
	}
}

// TestWriterSpawnRefusedOutsideSolo pins c-4: only a solo execute task on its
// phase branch gets a writer, and each refusal names which condition failed.
func TestWriterSpawnRefusedOutsideSolo(t *testing.T) {
	prompt := review.WriterPromptLine("p", "t-1")
	if res := writerCheck(t, writerSpawn(t, soloRepo(t, "in_progress"), prompt, nil)); !res.Allowed() {
		t.Fatalf("a well-formed spawn for the solo task was refused: %s", res.Text())
	}

	noMode := pairRepo(t, "in_progress")
	pair := pairRepo(t, "in_progress")
	setMode(t, pair, "pair")
	quick := soloRepo(t, "in_progress")
	if err := gatestate.SaveQuick(quick, gatestate.Quick{Mode: "solo", Description: "x", Head: headSHA(t, quick), At: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	idle := soloRepo(t, "done")
	off := soloRepo(t, "in_progress")
	gitIn(t, off, "checkout", "-q", "main")
	for _, c := range []struct{ name, dir, want string }{
		{"no execute mode", noMode, "no /dross-execute run is recorded"},
		{"pair mode", pair, "pair mode"},
		{"solo quick", quick, "a quick gets no writer"},
		{"no task in progress", idle, "no task is in progress"},
		{"HEAD off phase/p", off, "HEAD is not on phase/p"},
	} {
		refusedWith(t, c.name, writerCheck(t, writerSpawn(t, c.dir, prompt, nil)), c.want)
	}
}

// TestWriterSpawnBoundToArmedTask pins c-2: the prompt is exactly the armed
// task's line.
func TestWriterSpawnBoundToArmedTask(t *testing.T) {
	dir := soloRepo(t, "done", "done", "in_progress")
	if res := writerCheck(t, writerSpawn(t, dir, review.WriterPromptLine("p", "t-3"), nil)); !res.Allowed() {
		t.Fatalf("t-3's own prompt line was refused: %s", res.Text())
	}
	for name, prompt := range map[string]string{
		"another task":  review.WriterPromptLine("p", "t-4"),
		"appended text": review.WriterPromptLine("p", "t-3") + " Also refactor the gate package.",
	} {
		refusedWith(t, name, writerCheck(t, writerSpawn(t, dir, prompt, nil)), "exactly the writer prompt line")
	}
}

// TestWriterSpawnShape pins writer_spawn: background, on the session's model.
func TestWriterSpawnShape(t *testing.T) {
	dir := soloRepo(t, "in_progress")
	prompt := review.WriterPromptLine("p", "t-1")
	for name, change := range map[string]func(map[string]any){
		"foreground":     func(in map[string]any) { in["run_in_background"] = false },
		"non-bool value": func(in map[string]any) { in["run_in_background"] = "true" },
	} {
		refusedWith(t, name, writerCheck(t, writerSpawn(t, dir, prompt, change)), "background")
	}
	// The current Agent tool has no run_in_background input — every subagent
	// runs in the background — so a spawn without the key is admitted.
	if res := writerCheck(t, writerSpawn(t, dir, prompt, func(in map[string]any) { delete(in, "run_in_background") })); !res.Allowed() {
		t.Errorf("a spawn with no run_in_background key was refused: %s", res.Text())
	}
	refusedWith(t, "model override", writerCheck(t, writerSpawn(t, dir, prompt, func(in map[string]any) { in["model"] = "sonnet" })), "model")
}

// TestOneWriterPerTask pins c-1's "one writer per task": a launched or
// stopped record for the task refuses a second spawn; another task's does not.
func TestOneWriterPerTask(t *testing.T) {
	dir := soloRepo(t, "in_progress")
	prompt := review.WriterPromptLine("p", "t-1")
	for _, state := range []string{gatestate.WriterLaunched, gatestate.WriterStopped} {
		if err := gatestate.SaveWriter(dir, gatestate.Writer{Phase: "p", Task: "t-1", AgentID: "a1", State: state}); err != nil {
			t.Fatal(err)
		}
		refusedWith(t, state, writerCheck(t, writerSpawn(t, dir, prompt, nil)), "one writer per task")
	}
	if err := gatestate.SaveWriter(dir, gatestate.Writer{Phase: "p", Task: "t-9", AgentID: "a1", State: gatestate.WriterStopped}); err != nil {
		t.Fatal(err)
	}
	if res := writerCheck(t, writerSpawn(t, dir, prompt, nil)); !res.Allowed() {
		t.Fatalf("another task's writer record refused t-1's writer: %s", res.Text())
	}
}

func saveGreen(t *testing.T, dir string) {
	t.Helper()
	if err := gatestate.SaveGreen(dir, gatestate.Green{Tree: "tree-before-stop", At: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
}

func greenPresent(t *testing.T, dir string) bool {
	t.Helper()
	g, err := gatestate.LoadGreen(dir)
	if err != nil {
		t.Fatal(err)
	}
	return g != nil
}

// TestWriterStopClearsGreen pins c-3: the launch binds the writer, and each
// of its stops — the first, and the resumed fix round's — clears the green, so
// the writer's own `dross test` never gates a commit.
func TestWriterStopClearsGreen(t *testing.T) {
	dir := soloRepo(t, "in_progress")
	writerRecord(t, writerLaunch(t, dir, review.WriterPromptLine("p", "t-1")))
	w := writerRec(t, dir)
	if w == nil || w.Phase != "p" || w.Task != "t-1" || w.AgentID != capturedWriterID || w.State != gatestate.WriterLaunched {
		t.Fatalf("launch recorded %+v, want t-1 bound to %s, launched", w, capturedWriterID)
	}

	saveGreen(t, dir)
	writerRecord(t, bgPost(t, "writer_subagent_stop.json", dir, nil))
	if greenPresent(t, dir) {
		t.Error("the writer's first stop left green.json in place")
	}
	if w := writerRec(t, dir); w.State != gatestate.WriterStopped || w.StoppedAt == nil {
		t.Errorf("after the stop: %+v, want stopped with stopped_at", w)
	}

	writerRecord(t, sendMessagePost(t, dir, capturedWriterID))
	if w := writerRec(t, dir); w.State != gatestate.WriterLaunched || w.StoppedAt != nil {
		t.Errorf("a SendMessage fix round left %+v, want launched again", w)
	}
	saveGreen(t, dir)
	writerRecord(t, bgPost(t, "writer_resume_post.json", dir, nil))
	if greenPresent(t, dir) {
		t.Error("the resumed writer's second stop left green.json in place")
	}
	if w := writerRec(t, dir); w.State != gatestate.WriterStopped || w.AgentID != capturedWriterID {
		t.Errorf("after the resumed stop: %+v", w)
	}
}

// TestWriterStopIgnoresUnbound: a stop or SendMessage naming any other agent
// writes nothing.
func TestWriterStopIgnoresUnbound(t *testing.T) {
	dir := soloRepo(t, "in_progress")
	bound := gatestate.Writer{Phase: "p", Task: "t-1", AgentID: "another-agent", State: gatestate.WriterLaunched}
	if err := gatestate.SaveWriter(dir, bound); err != nil {
		t.Fatal(err)
	}
	saveGreen(t, dir)
	writerRecord(t, bgPost(t, "writer_subagent_stop.json", dir, nil))
	writerRecord(t, sendMessagePost(t, dir, capturedWriterID))
	if w := writerRec(t, dir); *w != bound {
		t.Errorf("an unbound writer's events changed the record to %+v", w)
	}
	if !greenPresent(t, dir) {
		t.Error("an unbound writer's stop cleared green.json")
	}
}

// TestWriterRecorderPairSilent pins c-4: in pair mode the launch records no
// writer and a stop touches neither the record nor the green.
func TestWriterRecorderPairSilent(t *testing.T) {
	dir := pairRepo(t, "in_progress")
	setMode(t, dir, "pair")
	writerRecord(t, writerLaunch(t, dir, review.WriterPromptLine("p", "t-1")))
	if w := writerRec(t, dir); w != nil {
		t.Fatalf("a pair-mode launch recorded %+v", w)
	}
	pre := gatestate.Writer{Phase: "p", Task: "t-1", AgentID: capturedWriterID, State: gatestate.WriterLaunched}
	if err := gatestate.SaveWriter(dir, pre); err != nil {
		t.Fatal(err)
	}
	saveGreen(t, dir)
	writerRecord(t, bgPost(t, "writer_subagent_stop.json", dir, nil))
	if w := writerRec(t, dir); *w != pre {
		t.Errorf("a pair-mode stop changed the record to %+v", w)
	}
	if !greenPresent(t, dir) {
		t.Error("a pair-mode stop cleared green.json")
	}
}

// TestWriterGateIgnoresOtherAgents: other subagents are not the gate's or the
// recorder's business.
func TestWriterGateIgnoresOtherAgents(t *testing.T) {
	g, rec := mustGate(t, "solo-writer"), writerRecorder(t)
	dir := soloRepo(t, "in_progress")
	for _, agent := range []string{"Explore", "general-purpose", review.ReviewerAgent} {
		p, err := Decode(writerSpawn(t, dir, "look around", func(in map[string]any) { in["subagent_type"] = agent }))
		if err != nil {
			t.Fatal(err)
		}
		c := &Call{Payload: p}
		if g.Claims(c) || rec.Claims(c) {
			t.Errorf("a %s spawn is claimed", agent)
		}
	}
	p, err := Decode(bgPost(t, "reviewer_subagent_stop.json", dir, nil))
	if err != nil {
		t.Fatal(err)
	}
	if rec.Claims(&Call{Payload: p}) {
		t.Error("the reviewer's SubagentStop is claimed by the writer recorder")
	}
}

package gate

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Rivil/dross/internal/gatestate"
	"github.com/Rivil/dross/internal/review"
)

// solo-writer (c-1, c-3, c-4): a solo /dross-execute run may hand one task's
// code to one dross-task-writer subagent. The gate admits that spawn only for
// the in-progress task of a solo execute run on its phase branch — never in
// pair mode, never for a solo quick (quick_solo) — with exactly that task's
// prompt line (review.WriterPromptLine), in the background, on the session's
// own model (writer_spawn), and only once per task.
//
// Its recorder keeps .dross/gate/writer.json: the launch's PostToolUse binds
// the writer's agent id to the task, a SendMessage to that agent (the review
// fix round) marks it launched again, and its SubagentStop marks it stopped
// and clears green.json — the writer's own `dross test` green is never the
// green a commit is gated on (c-3). Events of any other agent change nothing.
//
// The same gate fences the hand-off itself (writerfence.go): the writer's own
// calls, and the main loop's on a writer-owned task.
func init() {
	Register(Gate{
		Name: "solo-writer", Scope: Workflow, Liftable: true,
		Claims: claimsSoloWriter,
		Judge:  judgeSoloWriter,
	})
	RegisterRecorder(Recorder{
		Name: "solo-writer", Scope: Workflow,
		Claims: claimsWriterEvent,
		Record: recordWriter,
	})
}

// isWriterSpawn claims a spawn of the writer, under either name Claude Code
// has given the subagent tool.
func isWriterSpawn(c *Call) bool {
	return (c.ToolName == "Agent" || c.ToolName == "Task") && c.Field("subagent_type") == review.WriterAgent
}

// writerTask is the solo execute task a writer may be spawned for, or why
// there is none. ArmedScope decides; the reason only names which of its
// conditions failed.
func writerTask(root string) (*ReviewScope, string, error) {
	scope, err := ArmedScope(root)
	if err != nil {
		return nil, "", err
	}
	if scope != nil && scope.Kind == review.KindTask {
		return scope, "", nil
	}
	if scope != nil {
		return nil, "a solo /dross-quick is running, and a quick gets no writer", nil
	}
	ex, err := gatestate.LoadExecute(root)
	if err != nil {
		return nil, "", err
	}
	id, tasks, err := inProgress(root)
	if err != nil {
		return nil, "", err
	}
	switch {
	case ex == nil:
		return nil, "no /dross-execute run is recorded", nil
	case ex.Mode != "solo":
		return nil, "the /dross-execute run is in " + ex.Mode + " mode, and only --solo hands a task to a writer", nil
	case ex.Phase != id:
		return nil, fmt.Sprintf("the recorded /dross-execute run is for phase %s, not the current phase %s", ex.Phase, id), nil
	case len(tasks) == 0:
		return nil, "no task is in progress", nil
	}
	return nil, "HEAD is not on phase/" + id, nil
}

func judgeWriterSpawn(c *Call) (*Refusal, error) {
	root, err := c.Root()
	if err != nil {
		return nil, err
	}
	scope, why, err := writerTask(root)
	if err != nil {
		return nil, err
	}
	if scope == nil {
		return NewRefusal("a "+review.WriterAgent+" is spawned only for the in-progress task of a /dross-execute --solo run on its phase branch: "+why,
			"do the task inline — pair mode and quicks never get a writer")
	}
	want := review.WriterPromptLine(scope.Phase, scope.Task)
	if strings.TrimSpace(c.Field("prompt")) != want {
		return NewRefusal("the writer's prompt must be exactly the writer prompt line for "+scope.Name()+", with nothing added",
			"spawn it with the prompt `"+want+"` verbatim")
	}
	// An absent key is a background spawn: Claude Code's Agent tool runs every
	// subagent in the background and has dropped the parameter. Only an
	// explicit foreground request is refused.
	if bg, set := c.Input["run_in_background"]; set && bg != true {
		return NewRefusal("the writer runs in the background (writer_spawn)", "spawn it with run_in_background: true, or without the key")
	}
	if _, set := c.Input["model"]; set {
		return NewRefusal("the writer inherits the session's model (writer_spawn) — another model would confound the kill-switch measurement",
			"spawn it without a model")
	}
	w, err := gatestate.LoadWriter(root)
	if err != nil {
		return nil, err
	}
	if w != nil && w.Phase == scope.Phase && w.Task == scope.Task {
		return NewRefusal(fmt.Sprintf("one writer per task: %s already had writer %s (%s)", scope.Name(), w.AgentID, w.State),
			"a blocking review's fix round goes to that writer via SendMessage; a failed writer fails the task — `dross task status "+scope.Phase+" "+scope.Task+" pending` resets it for a retry")
	}
	return nil, nil
}

// claimsWriterEvent claims the writer's launch, every SendMessage (the
// recorder checks whether it names the writer), and the writer's SubagentStop.
func claimsWriterEvent(c *Call) bool {
	switch {
	case c.Event == EventSubagentStop:
		return c.AgentType == review.WriterAgent
	case c.ToolName == "SendMessage":
		return true
	}
	return isWriterSpawn(c)
}

func recordWriter(c *Call) error {
	root, err := c.Root()
	if err != nil || root == "" {
		return err
	}
	ex, err := gatestate.LoadExecute(root)
	if err != nil || ex == nil || ex.Mode != "solo" {
		// Pair mode or no execute run: there is no writer to record.
		return err
	}
	if c.Event == EventSubagentStop || c.ToolName == "SendMessage" {
		w, err := gatestate.LoadWriter(root)
		if err != nil || w == nil {
			return err
		}
		if c.Event == EventSubagentStop {
			if c.AgentID == "" || c.AgentID != w.AgentID {
				return nil
			}
			now := c.Now.UTC()
			w.State, w.StoppedAt = gatestate.WriterStopped, &now
			if err := gatestate.SaveWriter(root, *w); err != nil {
				return err
			}
			return gatestate.ClearGreen(root)
		}
		if c.Field("to") != w.AgentID {
			return nil
		}
		w.State, w.StoppedAt = gatestate.WriterLaunched, nil
		return gatestate.SaveWriter(root, *w)
	}

	scope, _, err := writerTask(root)
	if err != nil || scope == nil || strings.TrimSpace(c.Field("prompt")) != review.WriterPromptLine(scope.Phase, scope.Task) {
		// The gate refused this spawn before it ran.
		return err
	}
	var resp struct {
		Status  string `json:"status"`
		AgentID string `json:"agentId"`
	}
	_ = json.Unmarshal(c.Response, &resp)
	if resp.Status != "async_launched" || resp.AgentID == "" {
		return fmt.Errorf("the %s spawn reported status %q and agent id %q — not a background launch, so no writer was recorded", review.WriterAgent, resp.Status, resp.AgentID)
	}
	return gatestate.SaveWriter(root, gatestate.Writer{Phase: scope.Phase, Task: scope.Task, AgentID: resp.AgentID, State: gatestate.WriterLaunched})
}

package gate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/Rivil/dross/internal/gatestate"
	"github.com/Rivil/dross/internal/gitrun"
	"github.com/Rivil/dross/internal/pathfence"
	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/state"
)

// pair-approval (c-5, pair_approval_signal): while /dross-execute runs a phase
// in pair mode, a file-tool write outside .dross/ waits for the human to
// approve the task in progress. The approval is an AskUserQuestion answer that
// is exactly ApproveLabel(task); reject, steer, clarify and freeform answers
// record nothing, because a rejection must stop work, never unlock it
// (CLAUDE.md rule 9). The recorder binds the approval to the HEAD it was given
// at, so the task's commit spends it and the next task needs its own.
//
// The gate is armed only when current_phase's plan has a task in_progress, the
// execute mode recorded for that phase is not solo (none recorded is pair), and
// HEAD is on phase/<id>. In --solo, between tasks and off the phase branch it
// is silent. While armed it also refuses `dross execute begin … --solo`: a
// switch to solo mid-task would be the run approving its own edits.
func init() {
	Register(Gate{
		Name: "pair-approval", Scope: Workflow, Liftable: true,
		Claims: claimsPair, Judge: judgePair,
	})
	RegisterRecorder(Recorder{
		Name: "pair-approval", Scope: Workflow,
		Claims: func(c *Call) bool { return c.ToolName == "AskUserQuestion" },
		Record: recordApproval,
	})
}

// ApproveLabel is the AskUserQuestion option label that approves task — the
// one answer the recorder counts.
func ApproveLabel(task string) string { return "approve " + task }

func claimsPair(c *Call) bool {
	switch c.ToolName {
	case "Edit", "MultiEdit", "Write", "NotebookEdit":
		return c.FilePath() != ""
	case "Bash":
		return soloBegin(c)
	}
	return false
}

// soloBegin reports whether a Bash line runs `dross execute begin … --solo`.
func soloBegin(c *Call) bool {
	s := c.Script()
	for _, cmd := range s.Commands {
		if cmd.Name() != "dross" {
			continue
		}
		var words []string
		solo := false
		for _, a := range cmd.Args() {
			switch {
			case a == "--solo":
				solo = true
			case strings.HasPrefix(a, "--solo="):
				on, err := strconv.ParseBool(strings.TrimPrefix(a, "--solo="))
				solo = solo || err != nil || on
			case !strings.HasPrefix(a, "-"):
				words = append(words, a)
			}
		}
		if solo && len(words) >= 2 && words[0] == "execute" && words[1] == "begin" {
			return true
		}
	}
	if s.Partial {
		toks := rawTokens(c.Command())
		for i := 0; i+2 < len(toks); i++ {
			if path.Base(toks[i]) == "dross" && toks[i+1] == "execute" && toks[i+2] == "begin" && inList("--solo", toks[i+3:]) {
				return true
			}
		}
	}
	return false
}

// pairRun is what an armed gate is guarding: the phase, its task in progress,
// and the HEAD an approval must have been given at.
type pairRun struct {
	Phase, Task, Head string
}

func judgePair(c *Call) (*Refusal, error) {
	root, err := c.Root()
	if err != nil || root == "" {
		return nil, err
	}
	if c.ToolName != "Bash" {
		f, ok, err := ContainIn(root, c.FilePath())
		if err != nil || !ok || strings.HasPrefix(f.Rel(), ".dross/") {
			return nil, err
		}
	}
	run, err := armedRun(root)
	if err != nil || run == nil {
		return nil, err
	}
	label := ApproveLabel(run.Task)
	if c.ToolName == "Bash" {
		return NewRefusal(
			fmt.Sprintf("/dross-execute is running %s in pair mode with %s in progress; switching to --solo mid-task would let the run approve its own edits", run.Phase, run.Task),
			fmt.Sprintf("stay in pair mode and ask with AskUserQuestion offering %q — only the user chooses solo, by re-running /dross-execute with --solo", label))
	}
	appr, err := gatestate.LoadApproval(root)
	if err != nil {
		return nil, err
	}
	if appr != nil && appr.Phase == run.Phase && appr.Task == run.Task && appr.Head == run.Head {
		return nil, nil
	}
	why := "no approval is recorded for it"
	switch {
	case appr == nil:
	case appr.Phase != run.Phase || appr.Task != run.Task:
		why = fmt.Sprintf("the recorded approval is for %s %s", appr.Phase, appr.Task)
	default:
		why = "its approval was given before the last commit, and a commit spends it"
	}
	return NewRefusal(
		fmt.Sprintf("/dross-execute is running %s in pair mode and %s is in progress, but %s — in pair mode no code is written before the human approves the task", run.Phase, run.Task, why),
		fmt.Sprintf("propose the approach, then ask with AskUserQuestion offering the exact option label %q; the user picking it records the approval, and any other answer means stop and re-engage", label))
}

// armedRun reads whether the gate is armed in root, and on what; nil is
// silent. Two tasks in_progress at once is an ambiguity the gate cannot judge
// — which one would an approval be for? — so it is an error, not a guess.
func armedRun(root string) (*pairRun, error) {
	id, tasks, err := inProgress(root)
	if err != nil || len(tasks) == 0 {
		return nil, err
	}
	mode, err := gatestate.LoadExecute(root)
	if err != nil {
		return nil, err
	}
	if mode != nil && mode.Phase == id && mode.Mode == "solo" {
		return nil, nil
	}
	on, err := onBranch(root, "phase/"+id)
	if err != nil || !on {
		return nil, err
	}
	if len(tasks) > 1 {
		return nil, ambiguous(id, tasks)
	}
	head, err := headOf(root)
	if err != nil {
		return nil, err
	}
	return &pairRun{Phase: id, Task: tasks[0], Head: head}, nil
}

func ambiguous(id string, tasks []string) error {
	return fmt.Errorf("phase %s has %d tasks in_progress (%s), so which one is being approved is ambiguous — set all but one back with `dross task status %s <id> pending`",
		id, len(tasks), strings.Join(tasks, ", "), id)
}

// inProgress reads current_phase and the ids of the tasks its plan has
// in_progress. No state.json, no current phase and no plan are all none; one
// that exists but cannot be read or decoded is an error.
func inProgress(root string) (string, []string, error) {
	sf, err := pathfence.Contain(root, "dross state", ".dross/state.json")
	if err != nil {
		return "", nil, err
	}
	b, err := pathfence.ReadFile(sf)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, fmt.Errorf("read %s: %w", sf.Rel(), err)
	}
	var st state.State
	if err := json.Unmarshal(b, &st); err != nil {
		return "", nil, fmt.Errorf("decode %s: %w", sf.Rel(), err)
	}
	id := st.CurrentPhase
	if id == "" {
		return "", nil, nil
	}
	dir, err := phase.ContainID(filepath.Join(root, ".dross"), id)
	if err != nil {
		return "", nil, err
	}
	pf, err := pathfence.Contain(dir.String(), "phase plan", "plan.toml")
	if err != nil {
		return "", nil, err
	}
	name := ".dross/phases/" + id + "/plan.toml"
	b, err = pathfence.ReadFile(pf)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, fmt.Errorf("read %s: %w", name, err)
	}
	var plan phase.Plan
	if _, err := toml.Decode(string(b), &plan); err != nil {
		return "", nil, fmt.Errorf("decode %s: %w", name, err)
	}
	var tasks []string
	for _, t := range plan.Task {
		if t.Status == phase.StatusInProgress {
			tasks = append(tasks, t.ID)
		}
	}
	return id, tasks, nil
}

// onBranch reports whether root's HEAD is on branch. A detached HEAD is on
// none; any other failure to read it is an error.
func onBranch(root, branch string) (bool, error) {
	got, err := gitrun.Trim(root, "symbolic-ref", "--short", "HEAD")
	if err == nil {
		return got == branch, nil
	}
	if gitrun.ExitCode(gitrun.Quiet(root, "symbolic-ref", "-q", "HEAD")) == 1 {
		return false, nil
	}
	return false, fmt.Errorf("read the branch HEAD is on: %w", err)
}

// headOf is root's HEAD commit, "" on a branch with no commits yet.
func headOf(root string) (string, error) {
	head, err := gitrun.Trim(root, "rev-parse", "--verify", "--quiet", "HEAD")
	if err == nil {
		return head, nil
	}
	if gitrun.ExitCode(err) == 1 {
		return "", nil
	}
	return "", fmt.Errorf("read HEAD: %w", err)
}

// recordApproval records the human's approval of the task in progress when an
// answer in tool_response is exactly its ApproveLabel. Only tool_response
// counts: tool_input is what Claude sent, the response is what the tool
// reports the human chose.
func recordApproval(c *Call) error {
	root, err := c.Root()
	if err != nil || root == "" {
		return err
	}
	id, tasks, err := inProgress(root)
	if err != nil || len(tasks) == 0 {
		return err
	}
	answers, err := chosenLabels(c.Response)
	if err != nil {
		return err
	}
	if len(tasks) > 1 {
		return ambiguous(id, tasks)
	}
	label := ApproveLabel(tasks[0])
	approved := false
	for _, a := range answers {
		approved = approved || a == label
	}
	if !approved {
		return nil
	}
	head, err := headOf(root)
	if err != nil {
		return err
	}
	return gatestate.SaveApproval(root, gatestate.Approval{Phase: id, Task: tasks[0], Head: head, At: c.Now.UTC()})
}

// chosenLabels reads the answers of an AskUserQuestion tool_response — an
// object of question text to chosen label (internal/gate/testdata holds a
// captured one). A response with no readable answers is an error, surfaced as
// a warning, so a hook-schema change shows rather than silently recording
// nothing again. A non-string answer is kept out: it can never equal a label.
func chosenLabels(resp json.RawMessage) ([]string, error) {
	var top map[string]json.RawMessage
	var answers map[string]json.RawMessage
	if json.Unmarshal(resp, &top) == nil {
		_ = json.Unmarshal(top["answers"], &answers)
	}
	if len(answers) == 0 {
		return nil, errors.New("the AskUserQuestion tool_response carries no readable answers, so no approval can be read from it")
	}
	var labels []string
	for _, raw := range answers {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			labels = append(labels, s)
		}
	}
	return labels, nil
}

package gate

import (
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Rivil/dross/internal/gatestate"
	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/review"
)

// The solo-writer fence (c-1, c-3, c-5). Every call a subagent makes names it
// (agent_type, agent_id); the main loop's name nothing. While a writer record
// is armed — a solo execute run with .dross/gate/writer.json — the fence
// confines both sides of the hand-off:
//
//   - the writer edits only its bound task's files, never .dross/, runs only
//     read-only git, scoped dross and the toolchain's checks, and spawns
//     nothing; a call from any other writer — one whose task failed, or one
//     the record is not bound to — is a zombie and is refused outright;
//   - the main loop never edits a writer-owned task's code itself (c-5: a
//     failed writer fails the task), and while the writer is launched it does
//     not run the gates that certify the tree — the full `dross test`, the
//     review context, the commit — until the writer's completion notice.
//
// The stash and `dross task status … failed` always pass: they are how a
// failed writer's task is set aside.

// writerEditTools are the tools that write a file.
var writerEditTools = []string{"Edit", "MultiEdit", "Write", "NotebookEdit"}

// writerOpaque are programs that run a command line the fence cannot read.
var writerOpaque = []string{"sh", "bash", "zsh", "dash", "ksh", "fish", "eval", "exec", "source", ".", "xargs", "parallel", "su", "script"}

// writerMutators change or remove files outside the Edit and Write tools, so
// their reach would escape the task's files.
var writerMutators = []string{"rm", "rmdir", "mv", "cp", "ln", "tee", "truncate", "dd", "chmod", "chown", "install", "rsync", "patch"}

// writerGitReads and writerGoReads are the read-only subcommands a writer may
// run; writerDrossVerbs the dross verbs (task and rule only for show).
var (
	writerGitReads   = []string{"status", "diff", "log", "show"}
	writerGoReads    = []string{"vet", "build", "list", "doc", "version", "env"}
	writerDrossVerbs = []string{"test", "validate", "rule", "task"}
)

// claimsSoloWriter claims writer spawns, every call the writer makes, and the
// main loop's file writes and Bash — the calls a writer-owned task fences.
func claimsSoloWriter(c *Call) bool {
	switch {
	case isWriterSpawn(c), c.AgentType == review.WriterAgent:
		return true
	case c.AgentType != "":
		return false
	}
	return c.ToolName == "Bash" || slices.Contains(writerEditTools, c.ToolName)
}

func judgeSoloWriter(c *Call) (*Refusal, error) {
	switch {
	case c.AgentType == review.WriterAgent:
		return judgeWriterCall(c)
	case isWriterSpawn(c):
		return judgeWriterSpawn(c)
	}
	return judgeMainLoop(c)
}

// armedWriter is the writer record a solo execute run holds, or nil: pair
// mode and no record are both unarmed, and the fence stays silent.
func armedWriter(root string) (*gatestate.Writer, error) {
	ex, err := gatestate.LoadExecute(root)
	if err != nil || ex == nil || ex.Mode != "solo" {
		return nil, err
	}
	return gatestate.LoadWriter(root)
}

// ownedTask is the armed solo task when it is the writer's, else nil. Its
// phase and task come from the plan, not the record, so the record's fields
// never name a path.
func ownedTask(root string, w *gatestate.Writer) (*ReviewScope, error) {
	scope, err := ArmedScope(root)
	if err != nil || scope == nil {
		return nil, err
	}
	if scope.Kind != review.KindTask || scope.Phase != w.Phase || scope.Task != w.Task {
		return nil, nil
	}
	return scope, nil
}

func judgeWriterCall(c *Call) (*Refusal, error) {
	root, err := c.Root()
	if err != nil {
		return nil, err
	}
	w, err := armedWriter(root)
	if err != nil || w == nil {
		return nil, err
	}
	if c.AgentID == "" || c.AgentID != w.AgentID {
		return NewRefusal(fmt.Sprintf("this writer (agent %q) is not the one bound to %s %s (agent %s)", c.AgentID, w.Phase, w.Task, w.AgentID),
			"stop: only the task's own writer may act, and only while its task is in progress")
	}
	owned, err := ownedTask(root, w)
	if err != nil {
		return nil, err
	}
	if owned == nil {
		return NewRefusal(fmt.Sprintf("the writer's task %s %s is no longer in progress", w.Phase, w.Task),
			"stop and end with your dross-writer-report: the task was failed or finished without you")
	}
	switch {
	case c.ToolName == "Agent" || c.ToolName == "Task":
		return NewRefusal("a writer never spawns another agent", "do the work yourself, inside the task's files")
	case slices.Contains(writerEditTools, c.ToolName):
		return judgeWriterEdit(c, root, owned)
	case c.ToolName == "Bash":
		return judgeWriterBash(c, root, owned)
	}
	return nil, nil
}

// writerFiles is the writer-owned task's files, cleaned.
func writerFiles(root string, s *ReviewScope) ([]string, error) {
	dir, err := phase.ContainID(filepath.Join(root, ".dross"), s.Phase)
	if err != nil {
		return nil, err
	}
	var plan phase.Plan
	if err := decodePhaseFile(dir.String(), s.Phase, "plan.toml", &plan); err != nil {
		return nil, err
	}
	t := plan.FindTask(s.Task)
	if t == nil {
		return nil, fmt.Errorf("phase %s's plan has no task %s", s.Phase, s.Task)
	}
	files := make([]string, len(t.Files))
	for i, f := range t.Files {
		files[i] = path.Clean(filepath.ToSlash(f))
	}
	return files, nil
}

// inTaskFiles reports whether abs is one of the task's files. An error means
// the path could not be placed against the repo.
func inTaskFiles(root, abs string, files []string) (rel string, ok bool, err error) {
	f, in, err := ContainIn(root, abs)
	if err != nil || !in {
		return "", false, err
	}
	return f.Rel(), slices.Contains(files, f.Rel()), nil
}

func isDrossRel(rel string) bool { return rel == ".dross" || strings.HasPrefix(rel, ".dross/") }

func judgeWriterEdit(c *Call, root string, w *ReviewScope) (*Refusal, error) {
	files, err := writerFiles(root, w)
	if err != nil {
		return nil, err
	}
	rel, ok, err := inTaskFiles(root, c.FilePath(), files)
	if err != nil {
		return nil, err
	}
	switch {
	case isDrossRel(rel):
		return NewRefusal(rel+" is under .dross/, and a writer never writes dross state", "leave plan, state and gate records to the main loop")
	case !ok:
		return NewRefusal(fmt.Sprintf("%s is not one of %s's files %v", c.FilePath(), w.Task, files),
			"edit only the task's files; name the file you needed in your report's notes")
	}
	return nil, nil
}

func judgeWriterBash(c *Call, root string, w *ReviewScope) (*Refusal, error) {
	s := c.Script()
	if s.Partial {
		return NewRefusal("the writer's command line could not be read with confidence ("+s.Problem+"), so it cannot be fenced",
			"run plain commands, one per line, with no nested shells")
	}
	var files []string
	for _, cmd := range s.Commands {
		for _, out := range cmd.Outputs() {
			p := cmd.Resolve(out)
			if p == "/dev/null" || !filepath.IsAbs(p) {
				continue
			}
			if files == nil {
				var err error
				if files, err = writerFiles(root, w); err != nil {
					return nil, err
				}
			}
			rel, ok, err := inTaskFiles(root, p, files)
			if err != nil {
				return nil, err
			}
			if rel != "" && !ok {
				return NewRefusal(fmt.Sprintf("the line writes %s, which is not one of %s's files", rel, w.Task), "write only the task's files")
			}
		}
		if why := writerCommandRefusal(cmd.Name(), cmd.Args(), func() (string, bool) {
			g, ok := cmd.Git()
			return g.Sub, ok
		}); why != "" {
			return NewRefusal("a writer may not run `"+strings.Join(cmd.Argv, " ")+"`: "+why,
				"a writer runs read-only git (status, diff, log, show), `dross test --files`, `dross validate`, `dross rule show`, `dross task show`, and the toolchain's vet/build/format checks; committing, task status and finalizing stay with the main loop")
		}
	}
	return nil, nil
}

// writerCommandRefusal says why a writer may not run one simple command, or
// "" when it may.
func writerCommandRefusal(name string, args []string, gitSub func() (string, bool)) string {
	first := ""
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			first = a
			break
		}
	}
	switch {
	case name == "":
		return ""
	case slices.Contains(writerOpaque, name):
		return "it runs a command line the fence cannot read"
	case slices.Contains(writerMutators, name):
		return "it changes files outside the Edit and Write tools"
	case (name == "sed" || name == "perl") && slices.ContainsFunc(args, func(a string) bool { return strings.HasPrefix(a, "-i") }):
		return "it edits files in place"
	case name == "find" && slices.ContainsFunc(args, func(a string) bool {
		return a == "-exec" || a == "-execdir" || a == "-delete" || a == "-ok"
	}):
		return "it runs or deletes through find"
	case name == "gh" || name == "glab":
		return "it acts on the forge"
	case name == "git":
		sub, _ := gitSub()
		if !slices.Contains(writerGitReads, sub) {
			return "git " + sub + " is not a read-only git command"
		}
	case name == "dross":
		switch {
		case !slices.Contains(writerDrossVerbs, first):
			return "dross " + first + " belongs to the main loop"
		case (first == "rule" || first == "task") && nextWord(args, first) != "show":
			return "only `dross " + first + " show` is the writer's"
		case first == "test" && !slices.ContainsFunc(args, func(a string) bool { return a == "--files" || strings.HasPrefix(a, "--files=") }):
			return "a writer runs only its scoped `dross test --files`; the full suite is the main loop's gate"
		}
	case name == "go":
		if !slices.Contains(writerGoReads, first) {
			return "go " + first + " is not a vet, build or read-only go command"
		}
	case name == "gofmt":
		if slices.ContainsFunc(args, func(a string) bool { return a == "-w" || strings.HasPrefix(a, "-w=") }) {
			return "gofmt -w rewrites files; check with gofmt -l"
		}
	}
	return ""
}

// nextWord is the first non-flag argument after after, or "".
func nextWord(args []string, after string) string {
	seen := false
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "-"):
		case !seen && a == after:
			seen = true
		case seen:
			return a
		}
	}
	return ""
}

// judgeMainLoop fences the main loop out of a writer-owned task.
func judgeMainLoop(c *Call) (*Refusal, error) {
	root, err := c.Root()
	if err != nil {
		return nil, err
	}
	w, err := armedWriter(root)
	if err != nil || w == nil {
		return nil, err
	}
	owned, err := ownedTask(root, w)
	if err != nil || owned == nil {
		return nil, err
	}
	if c.ToolName != "Bash" {
		f, ok, err := ContainIn(root, c.FilePath())
		if err != nil || !ok || isDrossRel(f.Rel()) {
			return nil, err
		}
		return NewRefusal(fmt.Sprintf("%s %s was handed to a writer, and the main loop never takes it over inline (c-5)", w.Phase, w.Task),
			fmt.Sprintf("if the writer failed, set its code aside and fail the task: `git stash push -u -- . ':(exclude).dross'`, then `dross task status %s %s failed --reason \"writer: …\"`; a blocking review's fix round goes to the writer via SendMessage", w.Phase, w.Task))
	}
	if w.State != gatestate.WriterLaunched {
		return nil, nil
	}
	s := c.Script()
	if s.Partial {
		return NewRefusal(fmt.Sprintf("%s's writer is still working, and this command line could not be read with confidence (%s), so it may run a gate that judges a tree the writer is still changing", w.Task, s.Problem),
			"wait for the writer's completion notice, then run plain commands")
	}
	for _, cmd := range s.Commands {
		args := cmd.Args()
		var gate string
		switch name := cmd.Name(); {
		case slices.Contains(writerOpaque, name):
			gate = "a nested command line (which may hide one)"
		case name == "dross":
			switch {
			case len(args) > 0 && args[0] == "test" && !slices.ContainsFunc(args, func(a string) bool { return a == "--files" || strings.HasPrefix(a, "--files=") }):
				gate = "the full `dross test`"
			case len(args) > 1 && args[0] == "review" && args[1] == "context":
				gate = "`dross review context`"
			}
		case name == "git":
			if g, _ := cmd.Git(); g.Sub == "commit" {
				gate = "`git commit`"
			}
		}
		if gate != "" {
			return NewRefusal(fmt.Sprintf("%s's writer is still working, so %s would judge a tree it is still changing", w.Task, gate),
				"wait for the writer's completion notice, then run it")
		}
	}
	return nil, nil
}

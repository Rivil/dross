package gate

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/Rivil/dross/internal/pathfence"
	"github.com/Rivil/dross/internal/phase"
)

// The .dross file gates. Both are workflow-scoped: they protect dross's own
// curated files, and only inside a dross repo.
//
// plan-edit (c-3, plan_overwrite): plan.toml changes through `dross task` once
// it exists — a hand edit is how a task's id, wave or status drifts out of
// step with the board and changes.json. A Write that creates the plan is
// /dross-plan doing its job, and a Write over one is allowed while no task has
// left pending, so a plan can still be redone wholesale before execution.
//
// curated-shrink (c-8): a whole-file Write that cuts a hand-curated file to
// under half its size is almost always an agent regenerating it from a partial
// memory of the original. Edit is never judged — an Edit names what it removes.
func init() {
	Register(Gate{
		Name: "plan-edit", Scope: Workflow, Liftable: true,
		Claims: func(c *Call) bool {
			switch c.ToolName {
			case "Edit", "MultiEdit", "Write":
				_, ok := drossRel(c.FilePath())
				return ok && isPlanFile(c.FilePath())
			}
			return false
		},
		Judge: judgePlanEdit,
	})
	Register(Gate{
		Name: "curated-shrink", Scope: Workflow, Liftable: true, Extensible: true,
		Claims: func(c *Call) bool {
			if c.ToolName != "Write" {
				return false
			}
			rel, ok := drossRel(c.FilePath())
			return ok && curatedPattern(rel, c.Lists.CuratedFiles) != ""
		},
		Judge: judgeCuratedShrink,
	})
}

// drossRel is the part of p after its last .dross segment — the
// .dross-relative path the curated patterns are written against.
func drossRel(p string) (string, bool) {
	segs := strings.Split(filepath.ToSlash(filepath.Clean(p)), "/")
	for i := len(segs) - 2; i >= 0; i-- {
		if segs[i] == ".dross" {
			return strings.Join(segs[i+1:], "/"), true
		}
	}
	return "", false
}

func isPlanFile(p string) bool {
	rel, ok := drossRel(p)
	if !ok {
		return false
	}
	match, _ := path.Match("phases/*/plan.toml", rel)
	return match
}

func curatedPattern(rel string, patterns []string) string {
	for _, pat := range patterns {
		if ok, _ := path.Match(pat, rel); ok {
			return pat
		}
	}
	return ""
}

// rootDrossFile contains the call's file against its root and reports whether
// it is the root's own .dross/<rel> — not a file in some nested .dross.
func rootDrossFile(c *Call) (pathfence.Contained, string, bool, error) {
	root, err := c.Root()
	if err != nil || root == "" {
		return pathfence.Contained{}, "", false, err
	}
	f, ok, err := ContainIn(root, c.FilePath())
	if err != nil || !ok {
		return pathfence.Contained{}, "", false, err
	}
	rel, ok := strings.CutPrefix(f.Rel(), ".dross/")
	return f, rel, ok, nil
}

func judgePlanEdit(c *Call) (*Refusal, error) {
	f, rel, ok, err := rootDrossFile(c)
	if err != nil || !ok || !isPlanFile(".dross/"+rel) {
		return nil, err
	}
	b, err := pathfence.ReadFile(f)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil // creating the plan is /dross-plan's job
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", f.Rel(), err)
	}
	const remedy = "change tasks with `dross task add`, `dross task edit`, `dross task move` or `dross task remove`"
	if c.ToolName != "Write" {
		return NewRefusal(
			fmt.Sprintf("%s is edited only through `dross task` — a hand edit lets ids, waves and statuses drift from the board and changes.json", f.Rel()),
			remedy)
	}
	var plan phase.Plan
	if _, err := toml.Decode(string(b), &plan); err != nil {
		return nil, fmt.Errorf("decode plan %s: %w", f.Rel(), err)
	}
	for _, t := range plan.Task {
		if t.Status != "" && t.Status != phase.StatusPending {
			return NewRefusal(
				fmt.Sprintf("%s cannot be rewritten wholesale once execution has started (%s is %s)", f.Rel(), t.ID, t.Status),
				remedy)
		}
	}
	return nil, nil
}

func judgeCuratedShrink(c *Call) (*Refusal, error) {
	f, rel, ok, err := rootDrossFile(c)
	if err != nil || !ok {
		return nil, err
	}
	pat := curatedPattern(rel, c.Lists.CuratedFiles)
	if pat == "" {
		return nil, nil
	}
	// Read, not stat: a file the gate cannot read is a file it cannot judge.
	cur, err := pathfence.ReadFile(f)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", f.Rel(), err)
	}
	next := len(c.Field("content"))
	if next*2 >= len(cur) {
		return nil, nil
	}
	return NewRefusal(
		fmt.Sprintf("this Write would replace the curated file %s (%d bytes) with %d bytes — under half its size — which almost always means it was regenerated from a partial memory of the original",
			f.Rel(), len(cur), next),
		fmt.Sprintf("change %s with `Edit`, removing exactly what should go", f.Rel()))
}

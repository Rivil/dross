package cmd

// lanePlan — the resolution half of `dross test --files`, with the spawn taken
// out of it.
//
// Everything between "here is a file set" and "here is the line that would run"
// lives here: the lane match, the up-front fence, the existence filter and the
// selector derivation. What it deliberately does NOT hold is a seam anything
// can execute through — no spawnLocal, no transport, no consent verdict, no
// tree sync. That separation is the whole point: `dross test lane preview`
// answers "what would the gate do with these files" by calling the code the
// gate calls, rather than by re-deriving the line beside it. A preview built on
// a second derivation would drift from the gate on the first line either of
// them changed, and a preview that can drift is worth less than no preview.
//
// It is TOTAL — there is no error return, because every way resolution can go
// wrong is carried as DATA on the result: a path outside the repo, a path
// matching no lane, a path that is no longer on disk, a lane whose selector
// scoped to nothing, a lane whose own project.toml entry is malformed. The gate
// turns those into its exit statuses; preview prints them and exits 0 (locked
// preview_exit_status). A shared function that refused would force preview to
// inherit refusals it exists precisely not to make.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/testlane"
)

// lanePlanResult is the whole answer: which lanes a file set hits, what each
// one would spawn, and every path that got here without a lane to run in.
type lanePlanResult struct {
	// Lanes are the matched lanes in declaration order, each carrying the line
	// it would spawn — or the reason it would not.
	Lanes []plannedLane
	// Unmatched are in-tree paths that no declared lane matches, as the caller
	// wrote them. The gate prints them inline and keeps going; preview reads
	// them from here rather than re-deriving what the gate printed.
	Unmatched []string
	// OutOfTree are paths naming something outside the repo. Data here, exit 2
	// at the gate — see the file comment.
	OutOfTree []string
}

// plannedLane is one matched lane and the line it would run.
type plannedLane struct {
	matchedLane
	// Paths are the repo-relative, normalized paths that selected this lane —
	// the same set testlane.Selection keyed under its index.
	Paths []string
	// Line is what would be spawned: lane.Command byte-for-byte for a
	// selectorless lane, the derived or templated line for a scoped one. Empty
	// when the lane would not spawn at all — FenceErr or ScopedToNothing says
	// which — and on a lane with a Relate, whose line needs relate's answer.
	Line string
	// Relate is what a vouching lane spawns first: lane.Relate with its live
	// paths appended. What that prints decides the tests, so laneBatchLine
	// builds the lane's line at run time and no plan can hold it.
	Relate string
	// Asked are the live paths Relate carries, which is the list ParseRelate
	// reads its output against. A dropped path is on neither, so nothing relate
	// says can cover a deleted file.
	Asked []string
	// Selector is the derived arguments Line carries. It travels beside the
	// line because the miss report names them: "collected no tests" is only
	// actionable if the reader can see what it was looking in.
	Selector []string
	// Dropped are this lane's paths that are no longer on disk, filtered out
	// before derivation under the locked missing_paths decision. The gate
	// shortens its line silently; preview names each one and why (c-2), which
	// is the case a user is most likely to preview.
	Dropped []string
	// ScopedToNothing marks a lane that declares a selector whose every path
	// has been deleted. It does not spawn, and it is not a green.
	ScopedToNothing bool
	// FenceErr is the up-front fence's verdict on this lane's own project.toml
	// entry — a malformed command, prepare, selector style or template.
	// Carried rather than returned so preview can name the problem without
	// refusing, while the gate returns it before anything spawns.
	FenceErr error
}

// lanePlan resolves a file set against the declared lanes without spawning
// anything.
func lanePlan(repoDir string, proj *project.Project, files []string) lanePlanResult {
	globs := make([][]string, len(proj.Runtime.TestLane))
	for i, lane := range proj.Runtime.TestLane {
		globs[i] = lane.Match
	}
	sel := testlane.Select(globs, files)

	out := lanePlanResult{Unmatched: sel.Unmatched, OutOfTree: sel.OutOfTree}

	// The lane's INDEX travels with it, because sel.Matched is keyed by it: a
	// lane carried around as a bare TestLane loses the only handle on the paths
	// that selected it, and the selector would have to be re-derived from the
	// whole file set — one lane's paths leaking into another lane's line.
	for _, i := range sel.Lanes {
		lane := proj.Runtime.TestLane[i]
		pl := plannedLane{
			matchedLane: matchedLane{index: i, lane: lane},
			Paths:       sel.Matched[i],
		}
		// Fenced BEFORE derivation, per lane. A malformed line never reaches a
		// derivation, so the plan can never carry a Line built from an entry
		// the fence rejected — which is what keeps preview from rendering a
		// command that could not legally spawn.
		if err := laneFence(lane, proj.Runtime.TestCommand); err != nil {
			pl.FenceErr = err
			out.Lanes = append(out.Lanes, pl)
			continue
		}
		if lane.Vouch {
			relate, asked, dropped, ok := laneRelateLine(repoDir, lane, sel.Matched[i])
			pl.Relate, pl.Asked, pl.Dropped, pl.ScopedToNothing = relate, asked, dropped, !ok
			out.Lanes = append(out.Lanes, pl)
			continue
		}
		line, selector, dropped, ok := laneRunLine(repoDir, lane, sel.Matched[i])
		pl.Line, pl.Selector, pl.Dropped, pl.ScopedToNothing = line, selector, dropped, !ok
		out.Lanes = append(out.Lanes, pl)
	}
	return out
}

// laneFence checks everything about one lane that is a property of project.toml
// rather than of this file set.
//
// Every check runs against NO paths, which is what makes it a pure fence: the
// command and prepare go through shArgvFor, the selector style through Derive
// and the template through Expand, all of them answering "is this entry
// well-formed" and none of them answering "what would it run".
//
// shArgvFor rather than shArgv: the label is what the refusal tells the user to
// go and edit, and blaming runtime.test_command for a lane's command would send
// them to a line that is perfectly fine.
//
// testCommand is here for one rule: a vouching lane needs a full run that is not
// itself, which the lane alone cannot answer.
func laneFence(lane project.TestLane, testCommand string) error {
	if _, err := testlane.ShArgvFor(laneField(lane.Name), lane.Command); err != nil {
		return err
	}
	// The prepare goes through the SAME fence. A bootstrap line is a line
	// reaching a shell exactly as a command is.
	if lane.Prepare != "" {
		if _, err := testlane.ShArgvFor(laneField(lane.Name), lane.Prepare); err != nil {
			return err
		}
	}
	if _, err := testlane.Derive(lane.Selector, nil); err != nil {
		return fmt.Errorf("%s: %w", laneField(lane.Name), err)
	}
	// A placeholder-less template honoured at derivation time would spawn the
	// lane's WHOLE command under a scoped lane's name, so its shape is settled
	// here, against no paths, before anything is derived.
	if lane.SelectorTemplate != "" {
		if _, err := testlane.Expand(lane.SelectorTemplate, lane.SelectorJoin, nil); err != nil {
			return fmt.Errorf("%s: %w", laneField(lane.Name), err)
		}
	}
	if lane.Vouch {
		return vouchFence(lane, testCommand)
	}
	return nil
}

// vouchFence refuses a vouching lane validate would refuse. `dross validate` is
// a verb nobody has to run, so a hand-edited lane reaches this point unjudged,
// and a lane that cannot measure its paths must not get as far as admitting a
// commit. The rules are validate's own helpers, so the two cannot disagree.
//
// relate is fenced here and only here: a lane that does not vouch never runs it.
func vouchFence(lane project.TestLane, testCommand string) error {
	label := laneField(lane.Name)
	problems := laneVouchProblems(label, lane)
	if problem := vouchFullRunProblem(label, lane, testCommand); problem != "" {
		problems = append(problems, problem)
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	// Trimmed because laneRelateLine's line is: a leading space must not carry
	// a dash past the fence and onto the line.
	_, err := testlane.ShArgvFor(label, strings.TrimSpace(lane.Relate))
	return err
}

// laneRunLine builds the one command line a lane will spawn, reports which of
// its paths were dropped for no longer existing, and reports whether there is
// anything to spawn at all.
//
// ok is false only for a lane that declares a selector whose paths have all
// been deleted. A lane with no selector is always ok: its line is its command,
// byte-for-byte, and the existence filter never touches it — every lane written
// before this feature must behave exactly as it did, including when a caller
// names a path that is not there.
//
// The dropped paths come back rather than being discarded because the gate and
// preview want different things from the same filter: the gate silently runs a
// shorter line, preview names each path it lost and why (c-2). Deriving that
// list a second time at the preview site would be the divergence c-1 exists to
// rule out.
//
// The derived arguments come back alongside the line because the miss report
// names them: "collected no tests" is only actionable if the reader can see
// what it was looking in.
//
// A lane that also declares a selector_template places those arguments through
// the template instead of appending them, which is how a runner the closed enum
// cannot shape — cargo's repeated `--package`, ctest's joined `-R` regex — gets
// a scoped line at all.
func laneRunLine(repoDir string, lane project.TestLane, paths []string) (string, []string, []string, bool) {
	if lane.Selector == "" {
		return lane.Command, nil, nil, true
	}
	live, dropped := liveLanePaths(repoDir, paths)
	if len(live) == 0 {
		return "", nil, dropped, false
	}
	// The error was already raised by the up-front fence, against this exact
	// style. Reaching it here would mean the fence was skipped, and spawning
	// the unscoped command would be the silent whole-suite run the selector
	// exists to replace — so the lane does not run.
	args, err := testlane.Derive(lane.Selector, live)
	if err != nil || len(args) == 0 {
		return "", nil, dropped, false
	}
	line, ok := laneArgsLine(lane, args)
	if !ok {
		return "", nil, dropped, false
	}
	return line, args, dropped, true
}

// liveLanePaths splits a lane's paths into those on disk and those gone.
//
// Filtered before translation (locked missing_paths): a task that deleted a
// file would otherwise derive a package that no longer exists, and `go test
// ./gone/...` is a hard runner error — a failing gate for work the task did on
// purpose.
func liveLanePaths(repoDir string, paths []string) ([]string, []string) {
	live := make([]string, 0, len(paths))
	var dropped []string
	for _, p := range paths {
		if _, err := os.Stat(filepath.Join(repoDir, p)); err == nil {
			live = append(live, p)
			continue
		}
		dropped = append(dropped, p)
	}
	return live, dropped
}

// laneArgsLine places derived arguments on a lane's command: appended, or
// through its selector_template when it declares one.
//
// The template decides WHERE the arguments land; the caller has already decided
// what they are. The fragments carry their own quoting — done mid-expansion,
// since template text and substituted path are one string by the time they get
// here — so they are joined onto the consented command verbatim rather than
// re-quoted.
//
// ok is false when the template fails, which means the up-front fence was
// skipped: the lane does not spawn, for the reason a bad style does not.
// Appending nothing and running anyway would be the silent whole-suite run this
// feature exists to replace.
func laneArgsLine(lane project.TestLane, args []string) (string, bool) {
	if lane.SelectorTemplate == "" {
		return testCommandLine(lane.Command, args), true
	}
	frags, err := testlane.Expand(lane.SelectorTemplate, lane.SelectorJoin, args)
	if err != nil || len(frags) == 0 {
		return "", false
	}
	return strings.TrimSpace(lane.Command) + " " + strings.Join(frags, " "), true
}

// laneRelateLine builds the line a vouching lane spawns first, over its live
// paths, and returns those paths and the ones dropped for being gone. ok is
// false when none survive, which is laneRunLine's scoped-to-nothing lane.
//
// The paths are always appended. selector_template shapes the lane's command,
// and relate is not that command: a template written for a runner's flags has
// no meaning on the line that asks which tests exist.
func laneRelateLine(repoDir string, lane project.TestLane, paths []string) (string, []string, []string, bool) {
	live, dropped := liveLanePaths(repoDir, paths)
	args, err := testlane.Derive(lane.Selector, live)
	if err != nil || len(args) == 0 {
		return "", nil, dropped, false
	}
	return testCommandLine(lane.Relate, args), live, dropped, true
}

// laneBatchLine builds the one line a vouching lane runs, over the tests relate
// named, and returns the arguments it carries. Pure, so the run can call it once
// relate has answered and a test can reach it with no spawn.
//
// The tests take the placement a path takes on any other lane, template
// included. The changed paths are not an input, so they cannot reach this line.
//
// ok is false for no tests: a lane's command with nothing appended is its whole
// suite, and that is the run a vouching lane exists to avoid.
func laneBatchLine(lane project.TestLane, tests []string) (string, []string, bool) {
	args := testlane.BatchArgs(tests)
	if len(args) == 0 {
		return "", nil, false
	}
	line, ok := laneArgsLine(lane, args)
	if !ok {
		return "", nil, false
	}
	return line, args, true
}

// fullLanePlan is every declared lane, unscoped: each runs its own command with
// no derived selector. It is a lanes-only repo's bare `dross test` (locked
// full_run_source), the run that measures the whole tree when there is no
// runtime.test_command to run instead. The fence runs over every lane exactly
// as lanePlan's does, so a malformed lane refuses the run before anything
// spawns.
func fullLanePlan(proj *project.Project) []plannedLane {
	out := make([]plannedLane, 0, len(proj.Runtime.TestLane))
	for i, lane := range proj.Runtime.TestLane {
		pl := plannedLane{matchedLane: matchedLane{index: i, lane: lane}}
		if err := laneFence(lane, proj.Runtime.TestCommand); err != nil {
			pl.FenceErr = err
		} else {
			pl.Line = lane.Command
		}
		out = append(out, pl)
	}
	return out
}

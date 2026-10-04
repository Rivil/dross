package gate

import (
	"fmt"
	"strings"

	"github.com/Rivil/dross/internal/gatestate"
	"github.com/Rivil/dross/internal/review"
	"github.com/Rivil/dross/internal/treefp"
)

// solo-review (c-4, c-8): while a solo task or quick is armed (ArmedScope),
// a code commit is admitted only when that scope's review ledger stands at a
// pass for exactly the tree the commit would record — the same candidate
// commit-green judges. A .dross/-only commit passes ungated, and pair mode,
// no task in progress, or a HEAD off the phase branch arm nothing: the human
// is the gate there. Unlike commit-green it does not stand down in a repo with
// no test command — a solo change is reviewed whether or not it can be tested.
// A damaged record or a candidate that cannot be built refuses (closed).
//
// It also guards the scope itself (the downgrade guard): while a solo scope
// is armed and code is uncommitted, the `dross` calls that would disarm it —
// or swap it for a scope with no review history — are refused, so the review
// cannot be shed by changing mode mid-change. See judgeDowngrade.
func init() {
	Register(Gate{
		Name: "solo-review", Scope: Workflow, Liftable: true,
		Claims: func(c *Call) bool { return claimsCommit(c) || claimsDowngrade(c) },
		Judge: func(c *Call) (*Refusal, error) {
			if claimsCommit(c) {
				return judgeSoloReview(c)
			}
			return judgeDowngrade(c)
		},
	})
}

func judgeSoloReview(c *Call) (*Refusal, error) {
	s := c.Script()
	at, g := commitAt(s)
	dir := c.Cwd
	if at >= 0 {
		dir = g.Dir
	}
	root, err := LocateRoot(dir)
	if err != nil || root == "" {
		return nil, err
	}
	scope, err := ArmedScope(root)
	if err != nil || scope == nil {
		return nil, err
	}
	snap, ref, err := commitCandidate(s, at, g)
	if ref != nil || err != nil || onlyDross(snap.Changed) {
		return ref, err
	}
	rec, err := gatestate.LoadReview(root)
	if err != nil {
		return nil, err
	}
	var rounds []review.Round
	if scope.Matches(rec) {
		rounds = rec.Rounds
	}
	st := review.StateOf(rounds)
	switch st.Status {
	case review.StatusPass:
		if st.Tree == snap.Tree {
			return nil, nil
		}
		paths, err := treefp.Diff(g.Dir, st.Tree, snap.Tree)
		if err != nil {
			return nil, err
		}
		return NewRefusal(
			fmt.Sprintf("the solo review of %s passed, but the tree changed since the review — these paths differ: %s", scope.Name(), strings.Join(paths, " ")),
			"re-run `dross test`, then `dross review context` and the reviewer, and commit once `dross review status` reads pass")
	case review.StatusBlocked:
		return NewRefusal(
			fmt.Sprintf("the solo review of %s blocked this change", scope.Name()),
			"one fix round: address every blocking finding `dross review status` lists, re-run `dross test`, then `dross review context` and the reviewer")
	case review.StatusExhausted, review.StatusUnavailable:
		return NewRefusal(
			fmt.Sprintf("the solo review of %s failed it: %s — this change is not committed", scope.Name(), st.Cause),
			failRemedy(scope))
	}
	return NewRefusal(
		fmt.Sprintf("no passing solo review is recorded for %s, so nothing vouches for the tree this commit records", scope.Name()),
		"run `dross review context`, spawn "+review.ReviewerAgent+" with the printed prompt, wait for its verdict, and commit once `dross review status` reads pass")
}

// failRemedy is how a scope's run ends when its review failed.
func failRemedy(s *ReviewScope) string {
	if s.Kind == review.KindQuick {
		return "discard the change (`git checkout -- <files>`, remove new files), then `dross quick end`, and report the findings"
	}
	return fmt.Sprintf("set the code aside (`git stash push -u -- . ':(exclude).dross'`), then `dross task status %s %s failed` and move to the next task", s.Phase, s.Task)
}

// scopeCall is a `dross` call that could disarm a solo scope.
type scopeCall struct {
	verb string // "execute begin", "quick begin", "quick end", "task status"
	solo bool
	args []string // positional arguments after the verb
	dir  string
}

// scopeCalls reads the line's `dross execute begin`, `dross quick begin|end`
// and `dross task status` calls.
func scopeCalls(c *Call) []scopeCall {
	if c.ToolName != "Bash" {
		return nil
	}
	var out []scopeCall
	for _, cmd := range c.Script().Commands {
		if cmd.Name() != "dross" {
			continue
		}
		args := cmd.Args()
		if len(args) < 2 {
			continue
		}
		verb := args[0] + " " + args[1]
		switch verb {
		case "execute begin", "quick begin", "quick end", "task status":
		default:
			continue
		}
		sc := scopeCall{verb: verb, dir: cmd.Dir}
		rest := args[2:]
		for i := 0; i < len(rest); i++ {
			switch a := rest[i]; {
			case a == "--solo":
				sc.solo = true
			case a == "--reason":
				i++ // its value is not a positional argument
			case strings.HasPrefix(a, "-"):
			default:
				sc.args = append(sc.args, a)
			}
		}
		out = append(out, sc)
	}
	return out
}

func claimsDowngrade(c *Call) bool { return len(scopeCalls(c)) > 0 }

// judgeDowngrade refuses, while a solo scope is armed and code is
// uncommitted, every scope call that would let that code reach a commit
// without its review: a pair execute begin, any quick begin (a pair one
// downgrades, a solo one starts a fresh attempt that sheds the review
// history), `quick end`, setting the armed task back to pending or forward to
// done, and a solo execute begin over a solo quick (it clears the quick's
// marker). Nothing armed, or only .dross/ changed, is silent.
func judgeDowngrade(c *Call) (*Refusal, error) {
	calls := scopeCalls(c)
	root, err := LocateRoot(calls[0].dir)
	if err != nil || root == "" {
		return nil, err
	}
	scope, err := ArmedScope(root)
	if err != nil || scope == nil {
		return nil, err
	}
	var hit *scopeCall
	for i := range calls {
		if disarms(calls[i], scope) {
			hit = &calls[i]
			break
		}
	}
	if hit == nil {
		return nil, nil
	}
	dirty, err := treefp.Dirty(root)
	if err != nil || !dirty {
		return nil, err
	}
	return NewRefusal(
		fmt.Sprintf("`dross %s` while %s is armed with uncommitted code would let that code reach a commit without its solo review", hit.verb, scope.Name()),
		"finish the review and commit (or set the code aside: "+setAside(scope)+") before changing the run's mode")
}

// disarms reports whether call would disarm (or replace) the armed scope.
func disarms(call scopeCall, s *ReviewScope) bool {
	switch call.verb {
	case "execute begin":
		return !call.solo || s.Kind == review.KindQuick
	case "quick begin":
		// A pair quick downgrades; a solo one starts a fresh attempt, shedding
		// the armed scope's review history. A real begin runs on a clean tree.
		return true
	case "quick end":
		return true
	case "task status":
		if s.Kind != review.KindTask || len(call.args) < 3 {
			return false
		}
		status := call.args[2]
		return call.args[0] == s.Phase && call.args[1] == s.Task && (status == "pending" || status == "done")
	}
	return false
}

func setAside(s *ReviewScope) string {
	if s.Kind == review.KindQuick {
		return "discard it, then `dross quick end`"
	}
	return "`git stash push -u -- . ':(exclude).dross'`, then `dross task status " + s.Phase + " " + s.Task + " failed`"
}

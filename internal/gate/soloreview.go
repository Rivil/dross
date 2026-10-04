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
func init() {
	Register(Gate{
		Name: "solo-review", Scope: Workflow, Liftable: true,
		Claims: claimsCommit, Judge: judgeSoloReview,
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

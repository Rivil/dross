package cmd

// The lifecycle end of the repoint: a pin pointing at a commit on its own phase
// branch is not rotted yet — it is DOOMED, and the branch's death is what makes
// it rot.
//
// That is c-7, and it is the case the whole feature came from: the real c-5 pin
// named a commit on phase/config-trust-hardening, looked perfectly sound to the
// author whose machine still held the branch, and reddened for everyone else
// the moment the PR was squash-merged and the branch deleted.
//
// Two hooks, one predicate. Ship repairs the pin before the commit goes, so the
// rewrite rides the phase's own PR and is reviewed alongside the code that
// produced it. `phase complete` — which is where the branch actually dies —
// WARNS and proceeds: a merged phase must never be left uncompletable by a
// bookkeeping problem, and by then the repair belongs in its own commit anyway.
//
// The predicate is planRedProofRepoint with the phase's own remote-tracking ref
// excluded. Reusing the plan rather than writing a second reachability rule is
// what keeps "doomed" and "rotted" from drifting into two different answers.

import "fmt"

// doomedRedProofRef is the ref whose deletion is being anticipated: the phase's
// own remote-tracking branch.
func doomedRedProofRef(phaseID string) string {
	return originRefGlob + "phase/" + phaseID
}

// doomedRedProofPlans returns a repair plan for every pin that is reachable
// TODAY but would not be once phase/<id> is deleted from origin.
//
// A pin whose planning errors is skipped rather than raised. This runs inside
// ship and complete, and an unrelated phase's already-rotted pin — whose fork
// point may no longer resolve at all — must not be able to block a merge. The
// pin this phase owns is the one the hooks exist for, and its refusals are
// surfaced by the caller.
func doomedRedProofPlans(root, repoDir, phaseID string) ([]redProofRepointPlan, error) {
	pins, err := discoverRedProofPins(root)
	if err != nil {
		return nil, err
	}
	excluded := []string{doomedRedProofRef(phaseID)}
	var out []redProofRepointPlan
	for _, pin := range pins {
		plan, perr := planRedProofRepoint(root, repoDir, pin, excluded)
		if perr != nil || plan.Verdict != repointRepair {
			continue
		}
		out = append(out, plan)
	}
	return out, nil
}

// repointDoomedRedProofs is ship's hook: repair every doomed pin and commit the
// rewrite onto the phase branch.
//
// It commits, which the locked repoint_commit decision reserves to callers
// rather than to the verb — and ship is a caller. The rationale that decision
// guards against (a CLI verb landing an unreviewed fixtures/ prose diff in
// history) does not apply: this commit lands on phase/<id> before the PR is
// opened, so the doc rewrite is reviewed in the phase's own PR.
//
// Run BEFORE ship's clean-tree gate, so the two files it writes are committed
// rather than left as dirt that gate would refuse.
func repointDoomedRedProofs(root, repoDir, phaseID string) (bool, error) {
	plans, err := doomedRedProofPlans(root, repoDir, phaseID)
	if err != nil {
		return false, err
	}
	if len(plans) == 0 {
		return false, nil
	}

	var files []string
	for _, plan := range plans {
		note, ok := checkReplayBeforeRepoint(root, repoDir, plan)
		if !ok {
			// A refusal here IS fatal to the ship. This is the last moment the
			// pin can be saved; shipping past it would delete the branch
			// holding the commit up and leave the proof unreplayable, which is
			// a worse outcome than a re-runnable refusal.
			return false, fmt.Errorf("%s's red proof is pinned to a commit only %s reaches, and it could not be repointed: %s",
				plan.Phase, doomedRedProofRef(phaseID), note)
		}
		if err := applyRedProofRepoint(plan); err != nil {
			return false, err
		}
		Printf("repointed %s's red proof %s -> %s (its commit lives only on %s, which this merge deletes)\n",
			plan.Phase, short(plan.OldSHA), short(plan.NewSHA), doomedRedProofRef(phaseID))
		Printf("  %s\n", note)
		files = append(files, plan.Files...)
	}

	if out, err := gitCombined(repoDir, gitPathArgs("add", nil, files...)...); err != nil {
		return false, fmt.Errorf("stage the repointed red proof: %w\n%s", err, out)
	}
	msg := fmt.Sprintf("chore(dross): repoint red proof for %s", phaseID)
	if out, err := gitCombined(repoDir, "commit", "-m", msg); err != nil {
		return false, fmt.Errorf("commit the repointed red proof: %w\n%s", err, out)
	}
	return true, nil
}

// warnDoomedRedProofs is complete's half: it names the problem and the repair,
// then gets out of the way.
//
// A WARNING rather than a refusal, deliberately. Complete runs after the PR is
// merged; refusing there would leave a merged phase uncompletable over a
// bookkeeping problem, and the operator's only route out would be to hand-edit
// the record — which is the practice this whole feature replaces.
func warnDoomedRedProofs(root, repoDir, phaseID string) {
	plans, err := doomedRedProofPlans(root, repoDir, phaseID)
	if err != nil || len(plans) == 0 {
		return
	}
	for _, plan := range plans {
		Printf("⚠ %s's red proof pins %s, which only %s reaches — deleting that branch leaves the pin unreachable.\n",
			plan.Phase, short(plan.OldSHA), doomedRedProofRef(phaseID))
		Printf("  Fix: `dross phase red-proof repoint %s --apply`\n", plan.Phase)
	}
}

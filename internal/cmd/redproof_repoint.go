package cmd

// The decision layer for a repoint: what would change, and the write that
// changes it.
//
// Deliberately free of cobra. Two callers need this — the `red-proof repoint`
// verb and the ship hook that repairs a pin whose branch is about to be
// deleted — and a ship hook re-entering a cobra command to do it would make
// ship's behaviour depend on flag defaults it never set.
//
// The excludedRefs parameter is what makes those two callers one function. Ship
// needs to ask a question the verb does not: "will this pin still be reachable
// once refs/remotes/origin/phase/<id> is gone?" That is the same containment
// query with one ref removed, so the difference is a parameter rather than a
// second implementation that could disagree with the first.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Rivil/dross/internal/changes"
)

// redProofRepointVerdict is what planning decided. A refusal is NOT a verdict:
// it is an error, so a caller cannot pattern-match its way past one.
type redProofRepointVerdict string

const (
	// repointNothingToDo: the pin needs no repair, or this repo cannot tell
	// whether it does. Carries no target.
	repointNothingToDo redProofRepointVerdict = "nothing-to-do"
	// repointRepair: the pin has rotted and the fork point is a sound target.
	repointRepair redProofRepointVerdict = "repoint"
)

// redProofRepointPlan is the full statement of what a repoint would do,
// computed before anything is written so a dry run and an apply run share one
// answer rather than two code paths that could drift.
type redProofRepointPlan struct {
	Phase   string
	Verdict redProofRepointVerdict
	OldSHA  string
	NewSHA  string
	Doc     string
	// Files is every path the apply would touch, repo-relative. It is part of
	// the plan rather than derived at write time so a dry run cannot understate
	// what an apply will do.
	Files  []string
	Why    string
	Replay string

	root    string
	repoDir string
}

// planRedProofRepoint decides what to do about one pin without writing
// anything.
//
// Three outcomes, and the boundaries between them are the whole design:
//
//   - reachable → nothing-to-do with NO target. A sound pin is left alone
//     (c-5); proposing a target for one would invite a rewrite that trades a
//     working pin for a different working pin.
//   - indeterminate → nothing-to-do carrying the reason. A shallow CI clone
//     cannot see enough history to judge, and rewriting a pin on the strength
//     of a question this repo could not answer is how a good pin gets
//     destroyed by a fetch depth.
//   - unreachable → repoint to the fork point, but ONLY if the fork point
//     itself classifies reachable. Moving a pin onto a second commit origin
//     cannot see is not a repair (c-3), so that case is an error, not a plan.
func planRedProofRepoint(root, repoDir string, pin redProofPin, excludedRefs []string) (redProofRepointPlan, error) {
	p := redProofRepointPlan{
		Phase:   pin.Phase,
		OldSHA:  pin.SHA,
		Doc:     pin.Doc,
		root:    root,
		repoDir: repoDir,
	}
	if line, err := recordedReplayLine(root, pin.Phase); err == nil {
		p.Replay = line
	}

	verdict, why, err := classifyReachabilityExcluding(repoDir, pin.SHA, excludedRefs)
	if err != nil {
		return redProofRepointPlan{}, err
	}
	if verdict != reachUnreachable {
		p.Verdict = repointNothingToDo
		p.Why = why
		return p, nil
	}

	// NON-caching on purpose: phaseForkPoint writes the resolved value back
	// into changes.json, and a dry run that modified the record it was only
	// reporting on would be a write nobody asked for.
	fork, err := phaseForkPointNoCache(repoDir, root, pin.Phase)
	if err != nil {
		return redProofRepointPlan{}, err
	}
	if strings.TrimSpace(fork) == "" {
		return redProofRepointPlan{}, fmt.Errorf("phase %s: fork point resolved to nothing, so there is no commit to repoint to", pin.Phase)
	}

	forkVerdict, forkWhy, err := classifyReachabilityExcluding(repoDir, fork, excludedRefs)
	if err != nil {
		return redProofRepointPlan{}, err
	}
	if forkVerdict != reachReachable {
		return redProofRepointPlan{}, fmt.Errorf(
			"phase %s: refusing to repoint to its fork point %s — %s (%s); a repair that names a commit origin cannot see is not a repair",
			pin.Phase, short(fork), forkVerdict, forkWhy)
	}

	p.Verdict = repointRepair
	p.NewSHA = fork
	p.Why = why
	p.Files = []string{
		filepath.ToSlash(mustRelToRepo(repoDir, changes.FilePath(root, pin.Phase))),
		filepath.ToSlash(pin.Doc),
	}
	return p, nil
}

// applyRedProofRepoint performs the plan's writes: the doc first, the record
// second, and the doc restored if the record write fails.
//
// That order is chosen so a half-write leaves the state the checks understand.
// The record is what doctor reads; if it were written first and the doc write
// then failed, doctor would go green over a doc still naming the dead commit —
// a repair that traded a loud problem for a silent one. Written doc-first, a
// failure leaves the record pinning the OLD sha, which is the state that was
// already being reported.
//
// A nothing-to-do plan writes nothing at all, so plan+apply over a sound pin is
// byte-identical to not running it.
func applyRedProofRepoint(p redProofRepointPlan) error {
	if p.Verdict != repointRepair {
		return nil
	}
	docPath := filepath.Join(p.repoDir, filepath.FromSlash(p.Doc))
	info, err := os.Stat(docPath)
	if err != nil {
		return fmt.Errorf("phase %s: read %s: %w", p.Phase, p.Doc, err)
	}
	orig, err := os.ReadFile(docPath)
	if err != nil {
		return fmt.Errorf("phase %s: read %s: %w", p.Phase, p.Doc, err)
	}
	rewritten, _, err := redProofRewriteDoc(p.Doc, string(orig), p.OldSHA, p.NewSHA)
	if err != nil {
		return fmt.Errorf("phase %s: %w", p.Phase, err)
	}
	if err := os.WriteFile(docPath, []byte(rewritten), info.Mode().Perm()); err != nil {
		return fmt.Errorf("phase %s: write %s: %w", p.Phase, p.Doc, err)
	}

	recordPath := changes.FilePath(p.root, p.Phase)
	if err := writeRepointedPin(recordPath, p.Phase, p.NewSHA); err != nil {
		// Roll back, so the tree never carries a doc that pins a commit the
		// record does not. Restoring is best-effort by necessity — if it fails
		// too, the error names both files so the operator knows where to look.
		if rerr := os.WriteFile(docPath, orig, info.Mode().Perm()); rerr != nil {
			return fmt.Errorf("phase %s: %s could not be updated (%v) AND %s could not be restored (%v) — both files need checking by hand",
				p.Phase, recordPath, err, p.Doc, rerr)
		}
		return fmt.Errorf("phase %s: %s could not be updated (%w), so %s was restored to its original bytes — nothing was repointed",
			p.Phase, recordPath, err, p.Doc)
	}
	return nil
}

// writeRepointedPin rewrites only the pin's SHA, load-set-save, so the phase's
// base, fork point, PR, status, task records and recorded replay all survive a
// repair that is about one field.
func writeRepointedPin(path, phaseID, newSHA string) error {
	ch, err := changes.Load(path, phaseID)
	if err != nil {
		return err
	}
	if ch.RedProof == nil {
		return fmt.Errorf("no red_proof recorded")
	}
	ch.RedProof.SHA = newSHA
	return ch.Save(path)
}

// classifyReachabilityExcluding is classifyReachability with a set of
// remote-tracking refs treated as already gone.
//
// It delegates and then narrows, rather than reimplementing the containment
// query: excluding refs can only ever turn reachable into unreachable, never
// the other way, so every earlier arm — shallow clone, no origin refs, object
// absent — is already the right answer and must stay identical to what doctor
// reports. A second implementation of that ordering is a second thing to keep
// in step.
func classifyReachabilityExcluding(repoDir, sha string, excluded []string) (reachability, string, error) {
	verdict, why, err := classifyReachability(repoDir, sha)
	if err != nil || verdict != reachReachable || len(excluded) == 0 {
		return verdict, why, err
	}

	out, err := gitTrim(repoDir, gitRefArgs("for-each-ref",
		[]string{"--format=%(refname)", "--contains", sha}, originRefGlob)...)
	if err != nil {
		return "", "", fmt.Errorf("for-each-ref --contains %s: %w", short(sha), err)
	}
	drop := map[string]bool{}
	for _, ref := range excluded {
		drop[strings.TrimSpace(ref)] = true
	}
	var kept []string
	for _, ref := range strings.Split(out, "\n") {
		if ref = strings.TrimSpace(ref); ref != "" && !drop[ref] {
			kept = append(kept, ref)
		}
	}
	if len(kept) == 0 {
		return reachUnreachable, "contained only by " + strings.Join(excluded, ", ") + ", which is about to be deleted — nothing else on origin reaches it", nil
	}
	return reachReachable, "contained in " + kept[0], nil
}

// phaseForkPointNoCache is phaseForkPoint without the write-back.
//
// phaseForkPoint caches the resolved value into changes.json on first use,
// which is right for a checker running once but wrong here: planning happens on
// every dry run, and a dry run must leave the tree exactly as it found it.
func phaseForkPointNoCache(repoDir, root, phaseID string) (string, error) {
	path := changes.FilePath(root, phaseID)
	c, err := changes.Load(path, phaseID)
	if err != nil {
		return "", err
	}
	if c.BaseCommit != "" {
		return c.BaseCommit, nil
	}
	if c.Base == "" {
		return "", fmt.Errorf("phase %s records no base branch and no fork point, so there is no commit to repoint its red proof to", phaseID)
	}
	return resolveForkPoint(repoDir, root, phaseID, c)
}

// mustRelToRepo renders an absolute path repo-relative for display, falling
// back to the absolute path rather than failing: a plan that could not name a
// file it touches is worse than one that names it verbosely.
func mustRelToRepo(repoDir, path string) string {
	rel, err := filepath.Rel(repoDir, path)
	if err != nil {
		return path
	}
	return rel
}

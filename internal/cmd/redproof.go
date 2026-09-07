package cmd

// Red-proof reachability — is a pinned commit still there for someone else?
//
// A red proof is only worth the file it is written in if a reader can check out
// the commit it pins and watch the test go red again. The c-5 pin rotted exactly
// the way this file exists to catch: the commit sat on a phase branch that was
// squash-merged and deleted, so it survived on the author's machine (still
// referenced by a local branch, then as a loose object) and was gone for
// everyone else.
//
// Hence the locked reachability_scope decision: reachable means contained in one
// of ORIGIN's remote-tracking refs. Local branches do not count — they are the
// state a fresh clone does not have. `rev-parse --verify` succeeding does not
// count either: an unreferenced loose object answers that probe right up until
// gc collects it.

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Rivil/dross/internal/changes"
	"github.com/Rivil/dross/internal/pathfence"
)

// reachability is the three-way verdict. Three, not a bool, because "I cannot
// see enough history to tell" is a different statement from "this commit is
// gone" — collapsing them either reddens every shallow CI clone or hides a real
// rot behind a missing fetch.
type reachability string

const (
	// reachReachable: some refs/remotes/origin/* ref contains the commit.
	reachReachable reachability = "reachable"
	// reachUnreachable: the repo can see origin's refs and none contains it —
	// including the case where the object is gone from the database entirely.
	reachUnreachable reachability = "unreachable"
	// reachIndeterminate: this repo cannot answer the question. A shallow clone
	// or a repo with no origin refs is an absence of history, never a verdict.
	reachIndeterminate reachability = "cannot-determine"
)

// originRefGlob is the ref namespace reachability is judged against: origin's
// tracking refs specifically, not every remote a contributor happens to have
// added. A fork's remote reaching a commit says nothing about whether the
// upstream everyone clones does.
const originRefGlob = "refs/remotes/origin/"

// classifyReachability decides whether sha is reachable from origin's
// remote-tracking refs in repoDir, returning the verdict and a one-line why for
// callers to print.
//
// Ordering is load-bearing:
//
//  1. Shallow clone → indeterminate, BEFORE the object probe. In a shallow
//     clone the pinned commit is legitimately absent; calling that unreachable
//     would redden every truncated checkout for a pin that is perfectly sound.
//  2. No origin refs at all → indeterminate, for the same reason: nothing to
//     judge against.
//  3. Object missing from the database → UNREACHABLE, not indeterminate. This
//     is the post-gc case the whole check is about, and git's non-zero exit
//     here must not be folded into the indeterminate arm — that would turn the
//     one outcome that matters into a shrug.
//  4. Otherwise: contained in an origin ref, or not.
func classifyReachability(repoDir, sha string) (reachability, string, error) {
	if strings.TrimSpace(sha) == "" {
		return "", "", fmt.Errorf("no SHA to check reachability for")
	}
	if err := validateGitRef("red-proof commit", sha); err != nil {
		return "", "", err
	}

	if shallow, err := gitTrim(repoDir, "rev-parse", "--is-shallow-repository"); err == nil && shallow == "true" {
		return reachIndeterminate, "shallow clone — history is truncated, so containment cannot be decided here", nil
	}
	refs, err := gitTrim(repoDir, gitRefArgs("for-each-ref", []string{"--format=%(refname)", "--count=1"}, originRefGlob)...)
	if err != nil || strings.TrimSpace(refs) == "" {
		return reachIndeterminate, "no " + originRefGlob + "* refs in this repo — nothing to judge containment against (try `git fetch origin`)", nil
	}

	if err := gitNoOut(repoDir, gitRefArgs("rev-parse", []string{"--verify", "--quiet"}, sha+"^{commit}")...); err != nil {
		return reachUnreachable, "commit is absent from this repo's object database", nil
	}

	// --contains rides in the opts half because it is a flag's ARGUMENT, not a
	// positional; validateGitRef above has already refused an option-shaped
	// value there. The glob after the separator is dross's own literal.
	got, err := gitTrim(repoDir, gitRefArgs("for-each-ref",
		[]string{"--format=%(refname)", "--count=1", "--contains", sha}, originRefGlob)...)
	if err != nil {
		return "", "", fmt.Errorf("for-each-ref --contains %s: %w", short(sha), err)
	}
	if strings.TrimSpace(got) == "" {
		return reachUnreachable, "no " + originRefGlob + "* ref contains it — a fresh clone would not have this commit", nil
	}
	return reachReachable, "contained in " + strings.TrimSpace(got), nil
}

// redProofPin is one discovered pin, carrying the phase that owns it so a
// checker can name the fork point to repoint to when the SHA has rotted.
type redProofPin struct {
	Phase string
	SHA   string
	// Doc is CONTAINED, not a string. It is loaded from changes.json — a
	// committed, hand-editable artifact — and its readers copy it into a
	// cmd-local plan struct before anything opens it, which is exactly the
	// shape a dataflow scan cannot follow and the type system can. Only
	// discoverRedProofPins can populate it, and only by calling Contain.
	//
	// Safe to type because neither this struct nor redProofRepointPlan is
	// serialized: both carry no struct tags and nothing marshals them, unlike
	// changes.RedProof.Doc, which must stay a string to round-trip.
	Doc pathfence.Contained
}

// discoverRedProofPins finds every red proof recorded under root by CONVENTION:
// it globs phases/*/changes.json rather than naming a path. A red proof
// recorded by a later phase is checked with no new code — which is the whole
// point, since the one thing a hardcoded fixtures/hostile-config-c5 path
// guarantees is that the next phase's proof goes unchecked.
//
// A phase dir with no red_proof entry is skipped, not an error: ~30 existing
// dirs carry none, and that is the normal case. A changes.json that will not
// parse IS an error naming the file — a record dross cannot read is a problem
// to surface, not one to skip past quietly.
//
// A doc that escapes the repo is the same kind of problem and is treated the
// same way: discovery returns an ERROR on the first escape and the whole
// red-proof check aborts. That is escape_failure_mode applied here — a
// changes.json carrying an escaping doc is a corrupt artifact, and a corrupt
// artifact stops the run rather than being reported per-pin beside sound ones.
// The accepted cost is that one escaping doc suppresses every other pin's
// verdict in doctor output; the softer per-pin lane was considered and
// rejected, and the regression is pinned by a test rather than left as prose.
//
// repoDir is a PARAMETER rather than filepath.Dir(root), even though the two
// are equal in production. The callers already keep them apart — redProofChecks
// and doomedRedProofPlans both take both — and deriving one here would leave
// doctor resolving reachability against the repoDir it was handed and docs
// against a different root. That is also what lets the hermetic tests point
// discovery at a throwaway .dross while still reading this repo's real pinned
// doc, which is the whole basis of the false-positive gate.
func discoverRedProofPins(root, repoDir string) ([]redProofPin, error) {
	matches, err := filepath.Glob(filepath.Join(root, "phases", "*", changes.File))
	if err != nil {
		return nil, fmt.Errorf("scan for red-proof pins: %w", err)
	}
	var pins []redProofPin
	for _, path := range matches {
		phaseID := filepath.Base(filepath.Dir(path))
		c, err := changes.Load(path, phaseID)
		if err != nil {
			return nil, err
		}
		if c.RedProof == nil || strings.TrimSpace(c.RedProof.SHA) == "" {
			continue
		}
		doc, err := pathfence.Contain(repoDir, "changes.json red_proof.doc", c.RedProof.Doc)
		if err != nil {
			return nil, fmt.Errorf("phase %s: %w", phaseID, err)
		}
		pins = append(pins, redProofPin{Phase: phaseID, SHA: c.RedProof.SHA, Doc: doc})
	}
	// Glob order is filesystem order; sorting keeps doctor's output stable
	// between machines so a diff of two runs means something.
	sort.Slice(pins, func(i, j int) bool { return pins[i].Phase < pins[j].Phase })
	return pins, nil
}

// redProofDocSHA reads the pin the prose doc claims, so the record and the doc
// can be cross-checked. A doc that has drifted from the record is a doc lying
// to the human reader who follows it, even while the record stays sound.
// It takes a Contained rather than a repoDir and a string: the read goes
// through the pathfence seam, so a caller that never ran the containment check
// has no value to pass and fails to build. An escaping doc is refused at
// discovery and never reaches here at all.
func redProofDocSHA(doc pathfence.Contained) (string, error) {
	body, err := pathfence.ReadFile(doc)
	if err != nil {
		return "", err
	}
	return redProofSHA(string(body)), nil
}

// redProofSHA extracts the pinned base commit from the "base commit: <sha>"
// line a red-proof doc is required to carry.
//
// Shared between doctor and the fixture suite (c-4): two parsers would be two
// answers to "what does this doc pin", and the disagreement would surface as a
// green suite next to a red doctor.
func redProofSHA(text string) string {
	for _, line := range strings.Split(text, "\n") {
		l := strings.TrimSpace(line)
		lower := strings.ToLower(l)
		idx := strings.Index(lower, "base commit:")
		if idx < 0 {
			continue
		}
		rest := strings.TrimSpace(l[idx+len("base commit:"):])
		if f := strings.Fields(rest); len(f) > 0 {
			// Markdown emphasis and code fences travel with the value in prose;
			// trimmed here rather than forbidden in the doc, which is written
			// for a human reader first.
			return strings.Trim(f[0], "`*_ ")
		}
	}
	return ""
}

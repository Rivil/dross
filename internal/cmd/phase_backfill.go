package cmd

import (
	"fmt"
	"regexp"
	"strings"
)

// The evidence resolver behind `dross phase backfill`.
//
// 69 phase records on this repo predate changes.json's status field, so six
// finished milestones report 0/N done. The doneness reader has no fallback to
// guess with any more (phasedone.go), so the records have to be closed from
// evidence — and the evidence has to be strong enough that a 67-record sweep
// driven by it is safe to run unattended.
//
// Commit ancestry is not that evidence. Squash-merge means zero legacy task
// commits are ancestors of origin/main and thirteen have been
// garbage-collected, so "is this phase's work on main" is unanswerable by
// ancestry here. What survives is the squash commit's own subject, plus the
// absence of the phase branch that produced it.

// backfillShipSubject matches a squash-merge ship commit's subject and captures
// the phase slug.
//
// Anchored at the start, deliberately: `phase multilang-stack-profiles: ...` is
// a real subject on this repo, and an unanchored search for `stack-profiles`
// would close the wrong phase off it. Capturing the whole slug token and
// matching it exactly is stronger still — a subject's slug either IS the phase's
// slug or it is a different phase.
//
// The two-digit ordinal is optional because `dross phase migrate` renamed
// directories from `07-stack-profiles` to `stack-profiles` long after those
// phases shipped; their ship commits still carry the prefix.
var backfillShipSubject = regexp.MustCompile(`^phase (?:[0-9]{2}-)?([a-z0-9][a-z0-9-]*):`)

// backfillVerdict is one slug's answer: backfillable with an evidence commit,
// or not, with the reason it failed. A verdict is never silently absent — an
// unbackfillable slug carries its reason so the preview can print it, which is
// what makes the preview double as the residue listing.
type backfillVerdict struct {
	Slug        string
	OK          bool
	EvidenceSHA string
	Reason      string
}

// backfillShipCommits reads origin/<base>'s ship commits once, mapping phase
// slug to the commit that shipped it.
//
// It reads origin/<base> after an explicit fetch, never the local ref: under
// squash-merge, local main lags origin/main from the moment a PR merges until
// somebody fetches, so a scan of the local ref would report every recently
// shipped phase as having no evidence. A failed fetch is a hard error for the
// whole run — a scan of a stale origin/<base> is exactly the wrong answer this
// guards against, and it would fail silently.
//
// Newest wins when a slug shipped more than once (a follow-up PR against the
// same phase dir): git log is newest-first and the first sighting is kept, so
// the recorded evidence is the most recent delivery rather than the first.
func backfillShipCommits(repoDir, base string) (map[string]string, error) {
	if out, err := gitCombined(repoDir, "fetch", "origin"); err != nil {
		return nil, fmt.Errorf("git fetch origin: %w\n%s\n"+
			"backfill reads origin/%s, not the local ref — refusing to scan a possibly stale base", err, out, base)
	}
	log, err := gitTrim(repoDir, gitRefArgs("log", []string{"--format=%H %s"}, "origin/"+base)...)
	if err != nil {
		return nil, fmt.Errorf("git log origin/%s: %w", base, err)
	}
	ships := map[string]string{}
	for _, line := range strings.Split(log, "\n") {
		sha, subject, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		m := backfillShipSubject.FindStringSubmatch(subject)
		if m == nil {
			continue
		}
		if _, seen := ships[m[1]]; !seen {
			ships[m[1]] = sha
		}
	}
	return ships, nil
}

// resolveBackfill is the pure verdict function: a slug is backfillable when no
// phase/<slug> ref exists locally or on origin AND origin/<base> carries a ship
// commit naming it.
//
// The origin probe shells `git ls-remote --heads origin`, following phase
// complete's own remote-branch check — NOT refs/remotes/origin/, which is stale
// until a fetch and so reports a live branch for every legacy phase whose
// branch the forge deleted at merge, i.e. all of them.
//
// A failed ls-remote (offline, origin unset, auth failure) is a reason, never
// "no ref". Absence has to be proved: the whole marker rests on the branch being
// gone, and reading an unanswered question as a yes would mark 67 phases off a
// dropped network connection. It is a per-slug verdict rather than a run-level
// error so one unreachable query does not abort a sweep, but the slug reports
// unbackfillable and nothing is written for it.
func resolveBackfill(repoDir, slug string, ships map[string]string) backfillVerdict {
	branch := "phase/" + slug
	if gitNoOut(repoDir, gitRefArgs("rev-parse", []string{"--verify", "--quiet"}, "refs/heads/"+branch)...) == nil {
		return backfillVerdict{Slug: slug, Reason: "live local branch " + branch}
	}
	remote, err := gitTrim(repoDir, gitRefArgs("ls-remote", []string{"--heads"}, "origin", branch)...)
	if err != nil {
		return backfillVerdict{Slug: slug, Reason: fmt.Sprintf("could not query origin for %s: %v — absence unproven", branch, err)}
	}
	if remote != "" {
		return backfillVerdict{Slug: slug, Reason: "live branch " + branch + " on origin"}
	}
	sha, ok := ships[slug]
	if !ok {
		return backfillVerdict{Slug: slug, Reason: "no ship commit on the base matching `phase [NN-]" + slug + ":`"}
	}
	return backfillVerdict{Slug: slug, OK: true, EvidenceSHA: sha}
}

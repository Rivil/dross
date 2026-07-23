package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// clearPointSentinel is the literal footer marker every durable-boundary prompt
// prints at close (the c-1 clear-point protocol). The footer line is the
// sentinel followed — on the same line — by the exact /dross-… command a fresh
// session runs next.
const clearPointSentinel = "state is on disk — safe to /clear"

// footerCoverage classifies every command-backed prompt under root against the
// clear-point footer convention, fail-closed — the footer-audit mirror of
// interactionCoverage (same universe, same Exempt-table format, separate doc
// and gate so the two conventions never couple). A prompt is covered when it is
// either:
//
//   - footer-bearing — carries the sentinel with a /dross-… command on the same
//     line — and NOT on the Exempt list; or
//   - enrolled in docs/footer-audit.md's `## Exempt` table with a non-empty
//     reason.
//
// Anything else — no footer and no Exempt row, a sentinel with no next command,
// a footered prompt still on the Exempt list, or an Exempt row missing its
// reason cell — lands in Uncovered with a human-readable reason.
func footerCoverage(root string) (coverageResult, error) {
	names, err := commandBackedNames(root)
	if err != nil {
		return coverageResult{}, err
	}

	auditPath := filepath.Join(root, "docs", "footer-audit.md")
	auditBytes, err := os.ReadFile(auditPath)
	if err != nil {
		return coverageResult{}, fmt.Errorf("read %s: %w", auditPath, err)
	}
	exempt := parseExemptList(string(auditBytes))

	var res coverageResult
	for _, name := range names {
		footer, err := promptFooterState(root, name)
		if err != nil {
			return coverageResult{}, err
		}
		reason, isExempt := exempt[name]

		switch {
		case footer == footerMalformed:
			res.Uncovered = append(res.Uncovered, coverageGap{name,
				"carries the clear-point sentinel without a /dross-… next command on the same line"})
		case footer == footerPresent && isExempt:
			res.Uncovered = append(res.Uncovered, coverageGap{name,
				"carries the clear-point footer but is still on the footer-audit.md Exempt list — remove the Exempt row"})
		case footer == footerPresent:
			res.Covered = append(res.Covered, name)
		case isExempt && strings.TrimSpace(reason) == "":
			res.Uncovered = append(res.Uncovered, coverageGap{name,
				"footer-audit.md Exempt row has no reason cell — every exemption states why"})
		case isExempt:
			res.Covered = append(res.Covered, name)
		default:
			res.Uncovered = append(res.Uncovered, coverageGap{name,
				"no clear-point footer and not enrolled in the footer-audit.md `## Exempt` list"})
		}
	}
	return res, nil
}

type footerState int

const (
	footerAbsent footerState = iota
	footerPresent
	footerMalformed
)

// promptFooterState reads assets/prompts/<name>.md and reports whether it
// carries a well-formed clear-point footer. Every sentinel occurrence must have
// a /dross-… command later on its own line; one bad occurrence marks the whole
// prompt malformed so a half-edited footer can't pass.
func promptFooterState(root, name string) (footerState, error) {
	b, err := os.ReadFile(filepath.Join(root, "assets", "prompts", name+".md"))
	if err != nil {
		return footerAbsent, fmt.Errorf("read prompt for %s: %w", name, err)
	}
	state := footerAbsent
	for _, ln := range strings.Split(string(b), "\n") {
		i := strings.Index(ln, clearPointSentinel)
		if i == -1 {
			continue
		}
		if !strings.Contains(ln[i+len(clearPointSentinel):], "/dross-") {
			return footerMalformed, nil
		}
		state = footerPresent
	}
	return state, nil
}

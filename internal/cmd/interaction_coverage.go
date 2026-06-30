package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// interactionCoverage classifies every command-backed prompt under root against
// the interaction contract, fail-closed. A prompt is "covered" when it is either:
//
//   - interactive — its assets/commands/dross-<name>.md shim lists AskUserQuestion
//     — AND has a `### dross-<name>` section in docs/interaction-audit.md; or
//   - non-interactive AND enrolled in that doc's `## Exempt` list (with a reason).
//
// Anything else lands in Uncovered with a human-readable reason. This is the
// single source the c-1 Go-test gate (interaction_coverage_test.go) and the
// `dross doctor` lint both consume — doctor reuses it rather than re-deriving the
// classification, so the two never drift.
//
// The universe is command-backed prompts only: a prompts/<name>.md with a
// matching commands/dross-<name>.md shim, _-prefixed partials excluded. Adding a
// command adds a shim+prompt the enumeration auto-includes, so a new command
// cannot silently skip classification.
type coverageResult struct {
	Covered   []string
	Uncovered []coverageGap
}

type coverageGap struct {
	Name   string
	Reason string
}

func interactionCoverage(root string) (coverageResult, error) {
	names, err := commandBackedNames(root)
	if err != nil {
		return coverageResult{}, err
	}

	auditPath := filepath.Join(root, "docs", "interaction-audit.md")
	auditBytes, err := os.ReadFile(auditPath)
	if err != nil {
		return coverageResult{}, fmt.Errorf("read %s: %w", auditPath, err)
	}
	doc := string(auditBytes)
	exempt := parseExemptList(doc)

	var res coverageResult
	for _, name := range names {
		interactive, err := shimIsInteractive(root, name)
		if err != nil {
			return coverageResult{}, err
		}
		hasSection := hasAuditSection(doc, name)
		_, isExempt := exempt[name]

		switch {
		case interactive && isExempt:
			res.Uncovered = append(res.Uncovered, coverageGap{name,
				"shim lists AskUserQuestion but it is on the Exempt list — give it a `### dross-" + name + "` audit section instead"})
		case interactive && !hasSection:
			res.Uncovered = append(res.Uncovered, coverageGap{name,
				"interactive but has no `### dross-" + name + "` section in interaction-audit.md"})
		case !interactive && !isExempt:
			res.Uncovered = append(res.Uncovered, coverageGap{name,
				"non-interactive and not enrolled in the interaction-audit.md `## Exempt` list"})
		default:
			res.Covered = append(res.Covered, name)
		}
	}
	return res, nil
}

// commandBackedNames returns the sorted set of command names that have both a
// assets/commands/dross-<name>.md shim and a assets/prompts/<name>.md prompt,
// with _-prefixed partials excluded. Production mirror of the parity invariant
// in commands_parity_test.go.
func commandBackedNames(root string) ([]string, error) {
	prompts, err := mdNameSet(filepath.Join(root, "assets", "prompts"), "")
	if err != nil {
		return nil, err
	}
	cmds, err := mdNameSet(filepath.Join(root, "assets", "commands"), "dross-")
	if err != nil {
		return nil, err
	}
	var out []string
	for name := range prompts {
		if cmds[name] {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out, nil
}

// mdNameSet returns the set of *.md basenames in dir, with prefix and the .md
// suffix stripped. Non-.md files, entries lacking the prefix, and _-prefixed
// partials are skipped.
func mdNameSet(dir, prefix string) (map[string]bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	out := map[string]bool{}
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".md") || !strings.HasPrefix(n, prefix) {
			continue
		}
		if strings.HasPrefix(n, "_") {
			continue
		}
		out[strings.TrimSuffix(strings.TrimPrefix(n, prefix), ".md")] = true
	}
	return out, nil
}

// shimIsInteractive reports whether a command's wrapper lists AskUserQuestion —
// the same signal interaction_audit_test.go's interactiveCommands uses.
func shimIsInteractive(root, name string) (bool, error) {
	b, err := os.ReadFile(filepath.Join(root, "assets", "commands", "dross-"+name+".md"))
	if err != nil {
		return false, fmt.Errorf("read shim for %s: %w", name, err)
	}
	return strings.Contains(string(b), "AskUserQuestion"), nil
}

// hasAuditSection reports whether interaction-audit.md carries a `### dross-<name>`
// heading. The heading must match a whole line (after trailing-space trim) so
// `dross-plan` does not spuriously match `dross-plan-review`.
func hasAuditSection(doc, name string) bool {
	target := "### dross-" + name
	for _, ln := range strings.Split(doc, "\n") {
		if strings.TrimRight(ln, " \t") == target {
			return true
		}
	}
	return false
}

// parseExemptList extracts the command names enrolled in the `## Exempt` section
// of interaction-audit.md, mapping each to its stated reason. The section is a
// markdown table; each data row's first cell is a command name (backticks
// tolerated). Header and separator rows are skipped. An absent section yields an
// empty map — and thus every non-interactive prompt reads as uncovered.
func parseExemptList(doc string) map[string]string {
	out := map[string]string{}
	inSection := false
	for _, ln := range strings.Split(doc, "\n") {
		// Section boundaries are `## ` headings on their own line — an inline
		// mention like the `## Exempt` link in the Scope paragraph must not open
		// the section, so only a true heading toggles inSection.
		if heading := strings.TrimRight(ln, " \t"); strings.HasPrefix(heading, "## ") {
			inSection = strings.TrimSpace(strings.TrimPrefix(heading, "## ")) == "Exempt"
			continue
		}
		if !inSection {
			continue
		}
		row := strings.TrimSpace(ln)
		if !strings.HasPrefix(row, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(row, "|"), "|")
		if len(cells) < 2 {
			continue
		}
		name := strings.Trim(strings.TrimSpace(cells[0]), "`")
		reason := strings.TrimSpace(cells[1])
		if name == "" || strings.EqualFold(name, "command") ||
			strings.HasPrefix(name, "-") || strings.HasPrefix(name, ":") {
			continue // header or separator row
		}
		out[name] = reason
	}
	return out
}

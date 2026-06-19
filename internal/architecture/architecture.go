// Package architecture is the single source of truth for ARCHITECTURE.md —
// the repo-root document that describes the system by feature, one entry per
// user-facing capability (never one per phase or per module).
//
// This package owns only the *format*: the fixed entry micro-template and the
// seed skeleton that dross init writes into a greenfield repo. Populating
// entries with real prose is LLM work that lives in prompts (the
// backfill_trigger decision in the phase spec) — Go never writes entry prose
// here. /dross-architecture and dross-onboard backfill the doc; dross-ship
// merges each phase's landmarks into the matching feature entry.
package architecture

// File is the repo-root document this package seeds and maintains.
const File = "ARCHITECTURE.md"

// EntryTemplate is the fixed micro-template every entry follows. Four parts,
// in order:
//
//  1. a feature heading (a user-facing capability, not a module or a phase),
//  2. a single one-line description,
//  3. one or more symbol-link bullets (symbol name → file:line location),
//  4. a compact inline provenance breadcrumb.
//
// No free prose — structure enforces the dense, no-waffle requirement
// (entry_template decision). Backfill/merge prompts fill the placeholders and
// choose the exact link syntax; this constant only fixes the shape and order.
const EntryTemplate = `### <Feature name — a user-facing capability, not a module or a phase>

<One line: what this capability does.>

- Symbol.Name — path/to/file.ext:line
- Another.Symbol — path/to/other.ext:line

_introduced <phase-id> · extended <phase-id> · <short-sha>_
`

// Skeleton returns the seed ARCHITECTURE.md that dross init writes into a
// greenfield repo. It declares the doc's organizing contract (by feature, one
// entry per capability, never one per phase) and embeds EntryTemplate so the
// format travels with the file itself.
func Skeleton() string {
	return `# Architecture

This document describes what the system *does*, organized by feature — one entry
per user-facing capability, never one per phase and never one per module. Read it
top-to-bottom to learn the capabilities; follow the symbol links to find the code.

Every entry follows one fixed template:

` + EntryTemplate + `
Entries are maintained automatically: dross-ship merges each phase's landmarks
into the matching feature entry (updating in place), and /dross-architecture can
regenerate the whole document from a scan of the code and git history.

<!-- entries below, alphabetical by feature -->
`
}

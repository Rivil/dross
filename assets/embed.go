// Package assets embeds the shipped command skills (commands/dross-*.md) and
// prompts (prompts/*.md) into the dross binary so the CLI is a self-contained
// distribution unit: `dross install` materializes these into ~/.claude with no
// source checkout, and `dross update` carries its own assets across a self-update.
// Embedding keeps a single source of truth — the same bytes make install links
// into ~/.claude.
package assets

import "embed"

// FS holds every command skill and prompt shipped with dross.
//
// The `all:` prefix is required, not cosmetic: go:embed otherwise skips files
// whose names begin with `_` or `.`, which would silently drop
// prompts/_interaction.md from the binary. The embed-drift guard test asserts
// the embedded file set matches the on-disk set precisely so a narrowed pattern
// (or a dropped `all:`) fails loudly.
//
//go:embed all:commands all:prompts
var FS embed.FS

// InteractionPlaybook is the propose-and-react interaction playbook
// (prompts/_interaction.md), re-derived from FS so `dross interaction show`
// prints it verbatim from the binary. Single source — the same bytes install
// links/copies into ~/.claude/dross/prompts/.
var InteractionPlaybook = mustReadString("prompts/_interaction.md")

// mustReadString reads a file from the embedded FS, panicking if it is absent.
// A missing embedded asset means the binary was built wrong — failing fast at
// init beats emitting an empty playbook at runtime.
func mustReadString(name string) string {
	b, err := FS.ReadFile(name)
	if err != nil {
		panic("assets: embedded file missing: " + name + ": " + err.Error())
	}
	return string(b)
}

// Package assets embeds source files (prompts, snippets) into the dross binary so
// the CLI can emit them verbatim without depending on an external install. The
// same files are linked into ~/.claude by `make install`; embedding keeps a single
// source of truth that also ships inside the static binary.
package assets

import _ "embed"

// InteractionPlaybook is the canonical propose-and-react interaction playbook
// (assets/prompts/_interaction.md), embedded so `dross interaction show` prints it
// verbatim from the binary. This is the snippet_delivery mechanism: nested
// @-include does not expand, so interactive prompts pull the contract in via the
// emitter instead. Single source — the bytes here are the same file make install
// links into ~/.claude/dross/prompts/.
//
//go:embed prompts/_interaction.md
var InteractionPlaybook string

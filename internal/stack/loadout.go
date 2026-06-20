package stack

import (
	"fmt"
	"strings"
)

// RenderLoadout renders a profile's agent loadout as a markdown block: the
// recommended MCP tools, guardrails, and locked conventions (all derived from
// stack.locked at authoring time), followed by the tool loadout with each tool's
// PATH availability marked for the given GOOS. The block is what `dross stack
// loadout` emits for prompts to inject inline — there is no generated agent file
// (the locked agent_loadout_shape decision). Empty sections render "(none)" so a
// thin loadout never reads as a blank, ambiguous gap.
func RenderLoadout(p *Profile, goos string, lookPath func(string) (string, error)) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Stack loadout (%s)\n\n", p.ID)

	writeBullets(&b, "MCP tools", p.Loadout.MCPTools)
	writeBullets(&b, "Guardrails", p.Loadout.Guardrails)
	writeBullets(&b, "Conventions", p.Loadout.Conventions)

	b.WriteString("### Tools\n\n")
	if len(p.Tools) == 0 {
		b.WriteString("(none)\n")
		return b.String()
	}
	for _, t := range p.Tools {
		status := "unavailable"
		if _, err := lookPath(t.EffectiveBin(goos)); err == nil {
			status = "available"
		}
		fmt.Fprintf(&b, "- [%s] %s", status, t.Name)
		if status == "unavailable" && t.Install != "" {
			fmt.Fprintf(&b, " — %s", t.Install)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func writeBullets(b *strings.Builder, heading string, items []string) {
	fmt.Fprintf(b, "### %s\n\n", heading)
	if len(items) == 0 {
		b.WriteString("(none)\n\n")
		return
	}
	for _, it := range items {
		fmt.Fprintf(b, "- %s\n", it)
	}
	b.WriteByte('\n')
}

package stack

import (
	"runtime"
	"strings"
	"testing"
)

func TestLoadoutRendersFromLocked(t *testing.T) {
	emb, err := Embedded()
	if err != nil {
		t.Fatalf("Embedded: %v", err)
	}
	gp := ByID(emb, "go")
	if gp == nil {
		t.Fatal("no go profile")
	}

	out := RenderLoadout(gp, runtime.GOOS, fakeLookPath())
	for _, heading := range []string{"## Stack loadout (go)", "### MCP tools", "### Guardrails", "### Conventions"} {
		if !strings.Contains(out, heading) {
			t.Errorf("loadout missing heading %q", heading)
		}
	}
	// A known locked convention must survive into the block.
	if !strings.Contains(out, "Cobra for command structure") {
		t.Errorf("loadout dropped a locked convention; got:\n%s", out)
	}
}

func TestLoadoutMarksUnavailableGatedTool(t *testing.T) {
	p := &Profile{
		ID: "demo",
		Tools: []Tool{
			{Name: "semgrep", Kind: "scanner", Optional: true, Install: "pipx install semgrep"},
		},
	}

	// semgrep absent from PATH → must be marked unavailable, never available.
	out := RenderLoadout(p, runtime.GOOS, fakeLookPath())
	if !strings.Contains(out, "[unavailable] semgrep") {
		t.Errorf("gated tool not marked unavailable; got:\n%s", out)
	}
	if strings.Contains(out, "[available] semgrep") {
		t.Errorf("absent tool presented as available; got:\n%s", out)
	}
}

func TestLoadoutEmptyRendersNone(t *testing.T) {
	p := &Profile{ID: "bare"}
	out := RenderLoadout(p, runtime.GOOS, fakeLookPath())
	if !strings.Contains(out, "(none)") {
		t.Errorf("empty loadout must render \"(none)\", not blank; got:\n%s", out)
	}
	// Every section header still present, each followed by (none).
	for _, heading := range []string{"### MCP tools", "### Guardrails", "### Conventions", "### Tools"} {
		if !strings.Contains(out, heading) {
			t.Errorf("empty loadout missing heading %q", heading)
		}
	}
}

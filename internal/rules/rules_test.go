package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot walks up from the test's working dir to the module root (the dir
// containing go.mod) so cross-tree files like assets/prompts/*.md are reachable.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find module root (no go.mod above %s)", dir)
		}
		dir = parent
	}
}

// interactionRuleText returns the Text of the dross-interaction-contract builtin.
func interactionRuleText(t *testing.T) string {
	t.Helper()
	for _, r := range Builtins {
		if r.ID == "dross-interaction-contract" {
			return r.Text
		}
	}
	t.Fatal("dross-interaction-contract builtin not found")
	return ""
}

// canonicalContractPhrases are the load-bearing phrases that MUST appear
// verbatim in both the rule and the snippet, and live ONLY in the snippet among
// the prompts. They are the anchor against drift.
var canonicalContractPhrases = []string{
	"one decision per turn",
	"never paste the build artifact back",
}

// TestInteractionRuleNamesSnippet — the rule must name the snippet so the
// rule→snippet pointer can't silently vanish.
func TestInteractionRuleNamesSnippet(t *testing.T) {
	if !strings.Contains(interactionRuleText(t), "_interaction.md") {
		t.Error("interaction-contract rule must name _interaction.md")
	}
}

// TestInteractionRuleStaysTerse — the rule renders into EVERY command's context
// (including non-interactive ones), so the heavy playbook must stay in the
// snippet. A length cap keeps it from leaking back into the rule.
func TestInteractionRuleStaysTerse(t *testing.T) {
	if n := len(interactionRuleText(t)); n >= 600 {
		t.Errorf("interaction-contract rule text is %d chars; keep it < 600 — heavy playbook belongs in _interaction.md", n)
	}
}

// TestInteractionRuleUsesEmitter — the snippet_delivery decision replaced nested
// @-include (the pilot proved it does not expand) with the `dross interaction
// show` emitter. The shipped rule text and the snippet header must point at the
// emitter and must not instruct the dead @-include mechanism.
func TestInteractionRuleUsesEmitter(t *testing.T) {
	rule := interactionRuleText(t)
	if strings.Contains(rule, "@-include") {
		t.Error("interaction-contract rule still mentions @-include — nested expansion was disproven; point at `dross interaction show`")
	}
	if !strings.Contains(rule, "dross interaction show") {
		t.Error("interaction-contract rule must name the `dross interaction show` delivery mechanism")
	}
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "assets", "prompts", "_interaction.md"))
	if err != nil {
		t.Fatalf("read _interaction.md: %v", err)
	}
	if strings.Contains(string(b), "@-include") {
		t.Error("_interaction.md still instructs prompts to @-include it — the header must point at `dross interaction show`")
	}
}

// TestInteractionContractNoDrift — the canonical phrases must appear in BOTH the
// rule and the snippet. If either side is reworded, rule and snippet have drifted.
func TestInteractionContractNoDrift(t *testing.T) {
	rule := interactionRuleText(t)
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "assets", "prompts", "_interaction.md"))
	if err != nil {
		t.Fatalf("read _interaction.md: %v", err)
	}
	snippet := string(b)
	for _, p := range canonicalContractPhrases {
		if !strings.Contains(rule, p) {
			t.Errorf("canonical phrase %q missing from the rule (drift)", p)
		}
		if !strings.Contains(snippet, p) {
			t.Errorf("canonical phrase %q missing from the snippet (drift)", p)
		}
	}
}

// TestSpecPromptDefersToSnippet — spec.md must point at the snippet and must NOT
// carry its own divergent copy of the canonical phrasing; the playbook lives in
// _interaction.md, not duplicated per-prompt.
func TestSpecPromptDefersToSnippet(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "assets", "prompts", "spec.md"))
	if err != nil {
		t.Fatalf("read spec.md: %v", err)
	}
	spec := string(b)
	if !strings.Contains(spec, "_interaction.md") {
		t.Error("spec.md must point at the shared snippet (_interaction.md)")
	}
	for _, p := range canonicalContractPhrases {
		if strings.Contains(spec, p) {
			t.Errorf("spec.md carries canonical phrase %q verbatim — it should defer to _interaction.md, not duplicate the playbook", p)
		}
	}
}

func TestAddRejectsDuplicateID(t *testing.T) {
	s := &Set{}
	if err := s.Add(Rule{ID: "r-01", Text: "first"}); err != nil {
		t.Fatalf("first add: %v", err)
	}
	err := s.Add(Rule{ID: "r-01", Text: "second"})
	if err == nil {
		t.Fatal("expected duplicate id error, got nil")
	}
	if !strings.Contains(err.Error(), "r-01") {
		t.Errorf("error should mention id: %v", err)
	}
}

func TestAddDefaultsSeverityAndCreated(t *testing.T) {
	s := &Set{}
	if err := s.Add(Rule{ID: "r-01", Text: "x"}); err != nil {
		t.Fatal(err)
	}
	if s.Rules[0].Severity != Hard {
		t.Errorf("severity default: got %q want %q", s.Rules[0].Severity, Hard)
	}
	if s.Rules[0].Created == "" {
		t.Error("created should be populated automatically")
	}
}

func TestRemoveAndFind(t *testing.T) {
	s := &Set{}
	_ = s.Add(Rule{ID: "r-01", Text: "x"})
	_ = s.Add(Rule{ID: "r-02", Text: "y"})

	if _, ok := s.Find("r-01"); !ok {
		t.Error("expected to find r-01")
	}
	if !s.Remove("r-01") {
		t.Error("expected Remove to succeed")
	}
	if _, ok := s.Find("r-01"); ok {
		t.Error("expected r-01 gone after remove")
	}
	if s.Remove("nope") {
		t.Error("Remove should return false for missing id")
	}
}

func TestSetDisabled(t *testing.T) {
	s := &Set{}
	_ = s.Add(Rule{ID: "r-01", Text: "x"})
	if !s.SetDisabled("r-01", true) {
		t.Error("expected SetDisabled to find rule")
	}
	if !s.Rules[0].Disabled {
		t.Error("rule not marked disabled")
	}
	s.SetDisabled("r-01", false)
	if s.Rules[0].Disabled {
		t.Error("rule should be re-enabled")
	}
}

func TestMergeProjectWinsOnIDCollision(t *testing.T) {
	g := &Set{Rules: []Rule{
		{ID: "r-01", Text: "global text", Severity: Hard},
		{ID: "r-02", Text: "global only", Severity: Hard},
	}}
	p := &Set{Rules: []Rule{
		{ID: "r-01", Text: "project text", Severity: Soft},
	}}
	merged := Merge(g, p)
	if len(merged) != 2 {
		t.Fatalf("merged len: %d", len(merged))
	}
	var r01 Resolved
	for _, r := range merged {
		if r.ID == "r-01" {
			r01 = r
		}
	}
	if r01.Text != "project text" {
		t.Errorf("project should win: got %q", r01.Text)
	}
	if r01.Scope != Project {
		t.Errorf("scope should be project, got %q", r01.Scope)
	}
}

func TestMergeOmitsDisabled(t *testing.T) {
	g := &Set{Rules: []Rule{{ID: "r-01", Text: "x", Disabled: true}}}
	p := &Set{}
	merged := Merge(g, p)
	if len(merged) != 0 {
		t.Errorf("disabled rule leaked: %v", merged)
	}
}

func TestMergeSortGlobalFirstThenByID(t *testing.T) {
	g := &Set{Rules: []Rule{{ID: "r-zz", Text: "g"}}}
	p := &Set{Rules: []Rule{{ID: "r-aa", Text: "p"}}}
	merged := Merge(g, p)
	if merged[0].Scope != Global {
		t.Error("global should sort first")
	}
}

func TestRender(t *testing.T) {
	merged := []Resolved{
		{Rule: Rule{ID: "r-01", Text: "always docker", Severity: Hard}, Scope: Global},
		{Rule: Rule{ID: "r-02", Text: "warn on TODOs", Severity: Soft}, Scope: Project},
	}
	out := Render(merged)
	if !strings.Contains(out, "<rules>") || !strings.Contains(out, "</rules>") {
		t.Error("render must wrap in <rules> tags")
	}
	if !strings.Contains(out, "always docker") {
		t.Error("render missing rule text")
	}
	if !strings.Contains(out, "[global/hard/r-01]") {
		t.Error("render missing scope/severity/id prefix")
	}
}

func TestRenderEmpty(t *testing.T) {
	out := Render(nil)
	if !strings.Contains(out, "no user rules configured") {
		t.Errorf("empty render unexpected: %q", out)
	}
	// Builtins must still render even when there are no user rules.
	if !strings.Contains(out, "[builtin/hard/dross-commit-hygiene]") {
		t.Error("empty render missing dross-commit-hygiene builtin")
	}
}

func TestRenderEmitsAgentGateBuiltin(t *testing.T) {
	out := Render(nil)
	if !strings.Contains(out, "[builtin/hard/dross-agent-gate]") {
		t.Error("render missing dross-agent-gate builtin")
	}
	// The gating half is the hard part — make sure it survives.
	if !strings.Contains(out, "writing and deciding stay gated") {
		t.Errorf("agent-gate rule missing its gating clause: %q", out)
	}
}

func TestRenderEmitsInteractionContractBuiltin(t *testing.T) {
	out := Render(nil)
	if !strings.Contains(out, "[builtin/hard/dross-interaction-contract]") {
		t.Error("render missing dross-interaction-contract builtin")
	}
	// The canonical do/don't phrases are the load-bearing part of the contract —
	// if any is reworded away, the rule has drifted from the snippet (t-3 guards
	// the other direction).
	for _, phrase := range []string{"one decision per turn", "propose", "never paste the build artifact back"} {
		if !strings.Contains(out, phrase) {
			t.Errorf("interaction-contract rule missing phrase %q: %q", phrase, out)
		}
	}
	// The rule must name the snippet so the rule→snippet pointer can't silently vanish.
	if !strings.Contains(out, "_interaction.md") {
		t.Error("interaction-contract rule must name _interaction.md")
	}
}

func TestRenderEmitsBuiltinsBeforeUserRules(t *testing.T) {
	merged := []Resolved{
		{Rule: Rule{ID: "r-01", Text: "user rule", Severity: Hard}, Scope: Global},
	}
	out := Render(merged)
	builtinIdx := strings.Index(out, "dross-commit-hygiene")
	userIdx := strings.Index(out, "r-01")
	if builtinIdx < 0 || userIdx < 0 {
		t.Fatalf("missing rule lines: out=%q", out)
	}
	if builtinIdx > userIdx {
		t.Error("builtins must render before user rules")
	}
}

func TestLoadMissingFileReturnsEmpty(t *testing.T) {
	set, err := LoadFile(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil {
		t.Fatalf("missing file should be ok, got: %v", err)
	}
	if len(set.Rules) != 0 {
		t.Errorf("expected empty, got %d rules", len(set.Rules))
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.toml")
	original := &Set{Rules: []Rule{
		{ID: "r-01", Text: "always docker", Severity: Hard, Created: "2026-05-02"},
		{ID: "r-02", Text: "no force push", Severity: Soft, Created: "2026-05-02"},
	}}
	if err := original.SaveFile(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Rules) != 2 {
		t.Fatalf("len: %d", len(loaded.Rules))
	}
	if loaded.Rules[0].ID != "r-01" || loaded.Rules[1].Severity != Soft {
		t.Errorf("rules drift on round-trip: %+v", loaded.Rules)
	}
}

// drainRuleText returns the Text of the dross-survivor-drain builtin.
func drainRuleText(t *testing.T) string {
	t.Helper()
	for _, r := range Builtins {
		if r.ID == "dross-survivor-drain" {
			return r.Text
		}
	}
	t.Fatal("dross-survivor-drain builtin not found")
	return ""
}

// TestRenderEmitsSurvivorDrainBuiltin is c-6: the drain policy ships with dross
// rather than being re-typed into each repo's rules.toml. Render over the empty
// merged set is the "repo with no project rules configured" case.
func TestRenderEmitsSurvivorDrainBuiltin(t *testing.T) {
	out := Render(nil)
	if !strings.Contains(out, "[builtin/hard/dross-survivor-drain]") {
		t.Error("render missing dross-survivor-drain builtin")
	}
	// The ratchet is the point of the rule — if it is reworded away, the
	// backlog is free to grow again.
	if !strings.Contains(out, "only ever shrinks") {
		t.Errorf("drain rule missing its ratchet clause: %q", out)
	}
}

// TestRenderEmptyStillFlagsNoUserRules guards against the drain rule being
// registered as a user rule instead of a builtin: a repo with no configured
// rules must still say so, and the builtins must not be what fills that slot.
func TestRenderEmptyStillFlagsNoUserRules(t *testing.T) {
	out := Render(nil)
	if !strings.Contains(out, "(no user rules configured)") {
		t.Errorf("empty render must still print the no-user-rules sentinel: %q", out)
	}
}

// TestDrainRuleNamesBothEscapes pins the rule to the shipped CLI. A policy that
// says "close it out" without naming how is a rule nobody can follow; a policy
// naming verbs that don't exist is worse.
func TestDrainRuleNamesBothEscapes(t *testing.T) {
	text := drainRuleText(t)
	for _, escape := range []string{"dross survivor accept --reason", "dross survivor route --target"} {
		if !strings.Contains(text, escape) {
			t.Errorf("drain rule must name %q: %q", escape, text)
		}
	}
}

// TestProjectRuleR02Retired is the deletion half of c-6: this repo must not
// carry its own copy of the policy now that it ships as a builtin. Asserted on
// TEXT, not on the id — nextID reallocates "r-02" to the next unrelated
// `dross rule add`, so an id check would either pass vacuously or fail on an
// innocent rule.
func TestProjectRuleR02Retired(t *testing.T) {
	set, err := LoadFile(filepath.Join(repoRoot(t), ".dross", File))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range set.Rules {
		if strings.Contains(r.Text, "Pre-existing faults are not furniture") {
			t.Errorf("project rule %s still duplicates the drain policy now shipped as a builtin: %q", r.ID, r.Text)
		}
	}
}

// TestDrainPolicyRenderedOnce is the overlap guard: if the project copy ever
// creeps back, the merged block states the policy twice and the two copies
// drift. Counted, not merely contained — "contains" passes on a duplicate.
func TestDrainPolicyRenderedOnce(t *testing.T) {
	project, err := LoadFile(filepath.Join(repoRoot(t), ".dross", File))
	if err != nil {
		t.Fatal(err)
	}
	out := Render(Merge(&Set{}, project))
	if n := strings.Count(out, "Pre-existing faults are not furniture"); n != 1 {
		t.Errorf("drain policy appears %d times in the rendered block, want exactly 1:\n%s", n, out)
	}
}

// TestBuiltinsAreUniqueAndComplete guards against a copy-pasted builtin: a
// duplicated id would silently shadow another invariant in every rendered block.
func TestBuiltinsAreUniqueAndComplete(t *testing.T) {
	want := []string{
		"dross-commit-hygiene",
		"dross-agent-gate",
		"dross-interaction-contract",
		"dross-survivor-drain",
	}
	if len(Builtins) != len(want) {
		t.Fatalf("Builtins has %d entries, want %d — a new builtin needs a test, not just a slot", len(Builtins), len(want))
	}
	seen := map[string]bool{}
	for _, r := range Builtins {
		if seen[r.ID] {
			t.Errorf("duplicate builtin id %q", r.ID)
		}
		seen[r.ID] = true
		if r.Scope != "builtin" {
			t.Errorf("builtin %s has scope %q, want \"builtin\"", r.ID, r.Scope)
		}
	}
	for _, id := range want {
		if !seen[id] {
			t.Errorf("missing builtin %q", id)
		}
	}
}

// TestMergeSortIsTotalWithinAScope exercises the ID comparison in Merge's
// comparator, which the scope-only test above never reaches: with one rule per
// scope the comparator returns on the Scope check and the ID branch never runs.
//
// The order is asserted exactly, so an inverted `<` yields the reverse and
// fails. Rendering order is what a reader of `dross rule show` sees, and an
// unstable one makes the rule block churn in every diff.
func TestMergeSortIsTotalWithinAScope(t *testing.T) {
	g := &Set{Rules: []Rule{
		{ID: "r-gc", Text: "third global"},
		{ID: "r-ga", Text: "first global"},
		{ID: "r-gb", Text: "second global"},
	}}
	p := &Set{Rules: []Rule{
		{ID: "r-pb", Text: "second project"},
		{ID: "r-pa", Text: "first project"},
	}}

	merged := Merge(g, p)

	var got []string
	for _, r := range merged {
		got = append(got, r.ID)
	}
	want := []string{"r-ga", "r-gb", "r-gc", "r-pa", "r-pb"}
	if len(got) != len(want) {
		t.Fatalf("merged IDs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("merged IDs = %v, want %v", got, want)
		}
	}

	// Globals first, and every global ahead of every project rule — the two
	// halves of the comparator, asserted separately so one can fail alone.
	lastGlobal := -1
	for i, r := range merged {
		if r.Scope == Global {
			lastGlobal = i
		}
	}
	for i, r := range merged {
		if r.Scope != Global && i < lastGlobal {
			t.Errorf("project rule %s sorted ahead of a global one", r.ID)
		}
	}

	// Equal IDs cannot occur post-merge: the map is keyed by ID and a
	// collision resolves to the project rule (TestMergeProjectWinsOnIDCollision),
	// so the comparator's equality case is unreachable by construction rather
	// than untested.
	seen := map[string]bool{}
	for _, r := range merged {
		if seen[r.ID] {
			t.Fatalf("duplicate ID %s survived the merge", r.ID)
		}
		seen[r.ID] = true
	}
}

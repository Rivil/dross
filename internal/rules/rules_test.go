package rules

import (
	"path/filepath"
	"strings"
	"testing"
)

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

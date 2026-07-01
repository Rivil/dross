package cmd

import (
	"path/filepath"
	"strings"
	"testing"
)

// fakeGlobalHome points HOME at a fresh tempdir and returns the global
// dross dir path (~/.claude/dross) under it, so tests that touch the
// global rules scope don't read/write the developer's real config.
func ruleCovFakeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return filepath.Join(home, ".claude", "dross")
}

// TestRuleCover_ListMergedVsScoped drives rule.go:65 (merged || scope=="all")
// down both branches: default scope prints per-rule "[project/…]" lines and NOT
// the "<rules>" block; --merged prints the rendered "<rules>" block.
func TestRuleCover_ListMergedVsScoped(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	ruleCovFakeHome(t)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Rule(), "add", "--scope", "project", "always docker"); err != nil {
		t.Fatalf("add: %v", err)
	}

	// Scoped (non-merged) path: line 65 false.
	var scopedErr error
	scoped := captureStdout(t, func() { scopedErr = runCmd(t, Rule(), "list", "--scope", "project") })
	if scopedErr != nil {
		t.Fatalf("list scoped: %v", scopedErr)
	}
	if !strings.Contains(scoped, "[project/") {
		t.Errorf("scoped list should print per-rule line, got:\n%s", scoped)
	}
	if strings.Contains(scoped, "<rules>") {
		t.Errorf("scoped list must NOT print merged <rules> block, got:\n%s", scoped)
	}

	// Merged path: line 65 true.
	var mergedErr error
	merged := captureStdout(t, func() { mergedErr = runCmd(t, Rule(), "list", "--merged") })
	if mergedErr != nil {
		t.Fatalf("list merged: %v", mergedErr)
	}
	if !strings.Contains(merged, "<rules>") {
		t.Errorf("merged list should print rendered <rules> block, got:\n%s", merged)
	}
}

// TestRuleCover_ListEmptyPrintsNoRules drives rule.go:77 empty branch: a fresh
// repo has an empty project rules set, so list prints the "(no rules)" sentinel.
func TestRuleCover_ListEmptyPrintsNoRules(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	var listErr error
	out := captureStdout(t, func() { listErr = runCmd(t, Rule(), "list", "--scope", "project") })
	if listErr != nil {
		t.Fatalf("list: %v", listErr)
	}
	if !strings.Contains(out, "(no rules)") {
		t.Errorf("empty scope should print (no rules), got:\n%s", out)
	}
}

// TestRuleCover_ListSeverityDefaulting drives rule.go:87 (sev == "") both ways.
// An empty-severity rule renders as "hard"; a "soft" rule keeps "soft".
func TestRuleCover_ListSeverityDefaulting(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	// r-01 has no severity field (empty → defaults to hard); r-02 is soft.
	mustWrite(t, filepath.Join(dir, ".dross", "rules.toml"), `[[rule]]
  id = "r-01"
  text = "no severity here"

[[rule]]
  id = "r-02"
  text = "soft one"
  severity = "soft"
`)
	var listErr error
	out := captureStdout(t, func() { listErr = runCmd(t, Rule(), "list", "--scope", "project") })
	if listErr != nil {
		t.Fatalf("list: %v", listErr)
	}
	if !strings.Contains(out, "[project/hard] r-01") {
		t.Errorf("empty-severity rule should render as hard, got:\n%s", out)
	}
	if !strings.Contains(out, "[project/soft] r-02") {
		t.Errorf("soft rule should keep soft severity, got:\n%s", out)
	}
}

// TestRuleCover_ListInvalidScopeErrors drives rule.go:74: an unknown scope makes
// loadScope return an error that list must surface.
func TestRuleCover_ListInvalidScopeErrors(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	err := runCmd(t, Rule(), "list", "--scope", "bogus")
	if err == nil {
		t.Fatal("list with invalid scope should error")
	}
	if !strings.Contains(err.Error(), "scope must be") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestRuleCover_MergedAndShowGlobalError drives rule.go:67, :182 and :218: a
// malformed global rules.toml makes loadMerged fail, and both `list --merged`
// and `show` must surface that error rather than printing.
func TestRuleCover_MergedAndShowGlobalError(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	globalDir := ruleCovFakeHome(t)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(globalDir, "rules.toml"), "this is not = = valid toml [[[\n")

	if err := runCmd(t, Rule(), "list", "--merged"); err == nil {
		t.Error("list --merged should error on malformed global rules.toml")
	}
	if err := runCmd(t, Rule(), "show"); err == nil {
		t.Error("show should error on malformed global rules.toml")
	}
}

// TestRuleCover_PromoteNoRootErrors drives rule.go:128: promote outside a repo
// makes loadScope("project") fail with ErrNoRoot.
func TestRuleCover_PromoteNoRootErrors(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir) // no .dross here
	if err := runCmd(t, Rule(), "promote", "r-01"); err == nil {
		t.Fatal("promote outside a repo should error")
	}
}

// TestRuleCover_PromoteGlobalLoadError drives rule.go:136: the project rule is
// found, but loading the (malformed) global scope fails.
func TestRuleCover_PromoteGlobalLoadError(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	globalDir := ruleCovFakeHome(t)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, ".dross", "rules.toml"), `[[rule]]
  id = "r-01"
  text = "proj one"
  severity = "hard"
`)
	mustWrite(t, filepath.Join(globalDir, "rules.toml"), "= = broken [[[\n")

	if err := runCmd(t, Rule(), "promote", "r-01"); err == nil {
		t.Fatal("promote should error when global scope fails to load")
	}
}

// TestRuleCover_PromoteDuplicateGlobal drives rule.go:139: the id already exists
// in global, so globalSet.Add errors; the project rule must remain (no removal).
func TestRuleCover_PromoteDuplicateGlobal(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	globalDir := ruleCovFakeHome(t)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, ".dross", "rules.toml"), `[[rule]]
  id = "r-01"
  text = "proj one"
  severity = "hard"
`)
	mustWrite(t, filepath.Join(globalDir, "rules.toml"), `[[rule]]
  id = "r-01"
  text = "glob one"
  severity = "hard"
`)

	if err := runCmd(t, Rule(), "promote", "r-01"); err == nil {
		t.Fatal("promote should error on duplicate id in global scope")
	}
	// Real code returns before removing; the mutated (skip-return) code would
	// have removed the project rule. Assert it is still present.
	body := mustRead(t, filepath.Join(dir, ".dross", "rules.toml"))
	if !strings.Contains(body, "proj one") {
		t.Errorf("failed promote must leave the project rule intact, got:\n%s", body)
	}
}

// TestRuleCover_ToggleNoRootErrors drives rule.go:162: disable/enable outside a
// repo makes loadScope fail.
func TestRuleCover_ToggleNoRootErrors(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir) // no .dross here
	if err := runCmd(t, Rule(), "disable", "r-01"); err == nil {
		t.Fatal("disable outside a repo should error")
	}
}

// TestRuleCover_ListGlobalHomeUnset drives rule.go:197: with HOME unset,
// GlobalDir fails and loadScope("global") must surface the error.
func TestRuleCover_ListGlobalHomeUnset(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	t.Setenv("HOME", "")
	if err := runCmd(t, Rule(), "list", "--scope", "global"); err == nil {
		t.Fatal("list --scope global with HOME unset should error")
	}
}

// TestRuleCover_MergedProjectScopeHandling drives rule.go:222 both ways.
// Inside a repo the project scope loads cleanly (err == nil, block skipped) so
// the project rule appears in show output; outside a repo the ErrNoRoot branch
// treats project rules as empty and show still renders the builtins block.
func TestRuleCover_MergedProjectScopeHandling(t *testing.T) {
	// (a) normal repo: project rule must appear in merged/show output.
	dir := t.TempDir()
	chdir(t, dir)
	ruleCovFakeHome(t)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, ".dross", "rules.toml"), `[[rule]]
  id = "r-01"
  text = "custom-project-rule-marker"
  severity = "hard"
`)
	var showErr error
	out := captureStdout(t, func() { showErr = runCmd(t, Rule(), "show") })
	if showErr != nil {
		t.Fatalf("show: %v", showErr)
	}
	if !strings.Contains(out, "custom-project-rule-marker") {
		t.Errorf("show should include the project rule, got:\n%s", out)
	}

	// (b) outside a repo: ErrNoRoot branch → project treated as empty, no error.
	noRepo := t.TempDir()
	chdir(t, noRepo)
	ruleCovFakeHome(t)
	var noRepoErr error
	out2 := captureStdout(t, func() { noRepoErr = runCmd(t, Rule(), "show") })
	if noRepoErr != nil {
		t.Fatalf("show outside a repo should succeed (project optional): %v", noRepoErr)
	}
	if !strings.Contains(out2, "<rules>") {
		t.Errorf("show outside a repo should still render builtins block, got:\n%s", out2)
	}
}

// TestRuleCover_NextIDSkipsCollision drives rule.go:241 (n++): with r-01 and
// r-03 present, len+1 == 3 collides with r-03, so n must increment to 4 and the
// generated id is r-04. A decremented counter would yield r-02 instead.
func TestRuleCover_NextIDSkipsCollision(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, ".dross", "rules.toml"), `[[rule]]
  id = "r-01"
  text = "one"
  severity = "hard"

[[rule]]
  id = "r-03"
  text = "three"
  severity = "hard"
`)
	if err := runCmd(t, Rule(), "add", "--scope", "project", "brand new"); err != nil {
		t.Fatalf("add: %v", err)
	}
	body := mustRead(t, filepath.Join(dir, ".dross", "rules.toml"))
	if !strings.Contains(body, `id = "r-04"`) {
		t.Errorf("generated id should skip the r-03 collision to r-04, got:\n%s", body)
	}
	if strings.Contains(body, `id = "r-02"`) {
		t.Errorf("generated id must not be r-02 (would mean the counter decremented):\n%s", body)
	}
}

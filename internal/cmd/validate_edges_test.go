package cmd

import (
	"path/filepath"
	"strings"
	"testing"
)

// edgeRepo is an initialised repo whose deferred ids live in three places:
// "other" parks fff in its spec, the project store parks ggg, and the phase
// under test ("edgy") parks nothing of its own.
func edgeRepo(t *testing.T) string {
	t.Helper()
	// validate prints the resolved root (/private/var on macOS), so the
	// fixture paths the tests match against are resolved too.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustRunSet(t, "project.name", "test-app")
	mustRunSet(t, "runtime.mode", "native")

	mustWrite(t, filepath.Join(dir, ".dross", "phases", "other", "spec.toml"), `[phase]
id = "other"
title = "Other"

[[criteria]]
id = "c-1"
text = "does a thing"

[[deferred]]
id = "fff"
text = "parked in another phase"
`)
	mustWrite(t, filepath.Join(dir, ".dross", "deferred.toml"), `[[deferred]]
id = "ggg"
text = "parked in the store"
`)
	return dir
}

// writeEdgeSpec writes edgy's spec: criteria c-1 and c-2, then edges (raw
// [[edges]] TOML, possibly empty).
func writeEdgeSpec(t *testing.T, dir, edges string) string {
	t.Helper()
	path := filepath.Join(dir, ".dross", "phases", "edgy", "spec.toml")
	mustWrite(t, path, `[phase]
id = "edgy"
title = "Edgy"

[[criteria]]
id = "c-1"
text = "first"

[[criteria]]
id = "c-2"
text = "second"
`+edges)
	return path
}

// failLines returns the ✗ lines of validate output.
func failLines(out string) []string {
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "✗ ") {
			lines = append(lines, l)
		}
	}
	return lines
}

// TestValidateRefusesUnresolvedEdges (c-3): each unresolved shape makes
// validate exit non-zero with a ✗ line naming the spec, the record and its
// class.
func TestValidateRefusesUnresolvedEdges(t *testing.T) {
	for _, tc := range []struct {
		name, edges, class, want string
	}{
		{"dismissed-no-reason", `
[[edges]]
class = "boundary"
outcome = "dismissed"
`, "boundary", "dismissed with no reason"},
		{"covered-missing-criterion", `
[[edges]]
class = "empty"
outcome = "covered"
criteria = ["c-9"]
`, "empty", `"c-9"`},
		{"deferred-unknown-id", `
[[edges]]
class = "ordering"
outcome = "deferred"
deferred = "zzz"
`, "ordering", `"zzz"`},
		{"unknown-class", `
[[edges]]
class = "latency"
outcome = "dismissed"
reason = "n/a"
`, "latency", "unknown class"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := edgeRepo(t)
			specPath := writeEdgeSpec(t, dir, tc.edges)
			out, err := validateOut(t)
			if err == nil {
				t.Fatalf("validate passed an unresolved edge:\n%s", out)
			}
			want := "✗ " + specPath + ": edges[0] (" + tc.class + ")"
			var line string
			for _, l := range failLines(out) {
				if strings.HasPrefix(l, want) {
					line = l
				}
			}
			if line == "" {
				t.Fatalf("no line starting %q:\n%s", want, out)
			}
			if !strings.Contains(line, tc.want) {
				t.Errorf("line %q does not name %q", line, tc.want)
			}
		})
	}
}

// TestValidateReportsEveryBadEdge: validate collects, it does not stop at the
// first bad record.
func TestValidateReportsEveryBadEdge(t *testing.T) {
	dir := edgeRepo(t)
	specPath := writeEdgeSpec(t, dir, `
[[edges]]
class = "boundary"
outcome = "dismissed"

[[edges]]
class = "empty"
outcome = "covered"
criteria = ["c-9"]
`)
	out, err := validateOut(t)
	if err == nil {
		t.Fatalf("validate passed two bad edges:\n%s", out)
	}
	var n int
	for _, l := range failLines(out) {
		if strings.HasPrefix(l, "✗ "+specPath+": edges[") {
			n++
		}
	}
	if n != 2 {
		t.Errorf("got %d edge ✗ lines, want 2:\n%s", n, out)
	}
}

// TestValidateUndecodableSpecHasNoEdgeNoise: a spec that does not decode is
// reported once, and no edge line rides on top of it.
func TestValidateUndecodableSpecHasNoEdgeNoise(t *testing.T) {
	dir := edgeRepo(t)
	path := filepath.Join(dir, ".dross", "phases", "edgy", "spec.toml")
	mustWrite(t, path, "[phase\nid = \"edgy\"\n[[edges]]\nclass = \"latency\"\n")
	out, err := validateOut(t)
	if err == nil {
		t.Fatalf("validate passed an undecodable spec:\n%s", out)
	}
	var n int
	for _, l := range failLines(out) {
		if strings.Contains(l, path) {
			n++
		}
	}
	if n != 1 {
		t.Errorf("got %d ✗ lines for %s, want exactly 1:\n%s", n, path, out)
	}
	if strings.Contains(out, "edges[") {
		t.Errorf("edge noise on an undecodable spec:\n%s", out)
	}
}

// TestValidateLegacySpecStillValid (c-5): a spec written before [[edges]]
// existed — copied from the real self-audit spec — passes untouched.
func TestValidateLegacySpecStillValid(t *testing.T) {
	dir := edgeRepo(t)
	phaseDir := filepath.Join(dir, ".dross", "phases", "self-audit")
	mustWrite(t, filepath.Join(phaseDir, "spec.toml"), `[phase]
  id = "self-audit"
  title = "self-audit"
  milestone = "v1.0"

[[criteria]]
  id = "c-1"
  text = "dross techdebt has run against the dross repo; the newest run report's top findings are each triaged (fixed here, or accepted with a recorded reason), and dross status shows a last-run for tech-debt instead of 'never run'"

[[criteria]]
  id = "c-2"
  text = "/dross-secure has run against the dross repo; every BLOCKING/critical security finding is fixed in this phase, every other finding accepted with a recorded reason, and dross status shows a last-run for security"

[[decisions]]
  key = "audit_home"
  choice = "Remediation folds into THIS phase — no separately scaffolded remediation phases; a finding too large to fix here is accepted-with-reason and routed to the backlog via dross deferred"
  why = "The milestone is 3 phases by design; expected finding volume on a repo this size doesn't justify satellite phases"
  locked = true

[[deferred]]
  id = "c7773cea88d932f8"
  text = "config-trust-hardening: leading-dash rejection on config-derived git branch names"
  why = "Inherent to the build-tool category and LOW for the solo-author audience"
  dismissed = true
`)
	mustWrite(t, filepath.Join(phaseDir, "plan.toml"), `task_seq = 1

[phase]
  id = "self-audit"

[[task]]
  id = "t-1"
  wave = 1
  title = "Run the audit"
  files = ["README.md"]
  covers = ["c-1", "c-2"]
  status = "pending"
`)
	out, err := validateOut(t)
	if err != nil {
		t.Fatalf("legacy spec refused: %v\n%s", err, out)
	}
	if !strings.Contains(out, "✓ all dross artefacts valid") {
		t.Errorf("no ✓ line for a legacy spec:\n%s", out)
	}
}

// TestValidateDeferredEdgeResolvesProjectWide: a deferred edge may name an id
// parked in another phase's spec or in the project store.
func TestValidateDeferredEdgeResolvesProjectWide(t *testing.T) {
	for _, id := range []string{"fff", "ggg"} {
		t.Run(id, func(t *testing.T) {
			dir := edgeRepo(t)
			writeEdgeSpec(t, dir, `
[[edges]]
class = "ordering"
outcome = "deferred"
deferred = "`+id+`"
`)
			if out, err := validateOut(t); err != nil {
				t.Fatalf("deferred edge naming %s refused: %v\n%s", id, err, out)
			}
		})
	}
}

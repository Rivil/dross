package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/state"
)

// The plan-lock facts gate's fixtures reuse phase facts' repo: one phase "fx"
// under a temp .dross, outside git, so every file written there is in the tree.

// writeGateSpec writes fx's spec: top goes above [phase] (a `new = [...]`
// line), body after the one criterion c-1, whose text is text.
func writeGateSpec(t *testing.T, dir, top, text, body string) {
	t.Helper()
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "fx", "spec.toml"),
		top+"\n[phase]\nid = \"fx\"\ntitle = \"Fx\"\n\n[[criteria]]\nid = \"c-1\"\ntext = '"+text+"'\n"+body)
}

// writeGatePlan writes fx's plan: one task t-1 described by desc, touching files.
func writeGatePlan(t *testing.T, dir, desc string, files ...string) {
	t.Helper()
	quoted := make([]string, 0, len(files))
	for _, f := range files {
		quoted = append(quoted, `"`+f+`"`)
	}
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "fx", "plan.toml"),
		"[phase]\nid = \"fx\"\n\n[[task]]\nid = \"t-1\"\ntitle = \"one\"\nwave = 1\nstatus = \"pending\"\ndescription = '"+
			desc+"'\nfiles = ["+strings.Join(quoted, ", ")+"]\ncovers = [\"c-1\"]\n")
}

// gateRepo is a facts fixture with fx as the current phase.
func gateRepo(t *testing.T) string {
	t.Helper()
	dir := factsRepo(t)
	mustSetState(t, "current_phase", "fx")
	return dir
}

// gateLines returns the finding lines a plan-lock refusal lists.
func gateLines(err error) []string {
	if err == nil {
		return []string{}
	}
	out := []string{}
	for _, l := range strings.Split(err.Error(), "\n") {
		if strings.HasPrefix(l, "  ") {
			out = append(out, strings.TrimPrefix(l, "  "))
		}
	}
	return out
}

const (
	citeOne   = "internal/nope/one.go"
	citeTwo   = "internal/nope/two.go"
	citeThree = "internal/nope/three.go"
)

// TestPlanLockRefusesUnresolvedCitations (c-5): three unresolved citations
// across spec and plan refuse, every one is listed, the refusal names the
// command and the writer, and state.json is byte-identical afterwards.
func TestPlanLockRefusesUnresolvedCitations(t *testing.T) {
	dir := gateRepo(t)
	writeGateSpec(t, dir, "", "see "+citeOne+" and "+citeTwo, "")
	writeGatePlan(t, dir, "then "+citeThree, "a.go")
	statePath := filepath.Join(dir, ".dross", state.File)
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}

	err = setState(t, "current_phase_status", "planned")
	if err == nil {
		t.Fatal("plan-lock passed with three unresolved citations")
	}
	want := []string{
		"criteria c-1: missing " + citeOne,
		"criteria c-1: missing " + citeTwo,
		"task t-1 description: missing " + citeThree,
	}
	if got := gateLines(err); !reflect.DeepEqual(got, want) {
		t.Errorf("refusal lines:\n got %q\nwant %q", got, want)
	}
	for _, needle := range []string{"dross phase facts fx", "dross phase mark-new fx"} {
		if !strings.Contains(err.Error(), needle) {
			t.Errorf("refusal does not name `%s`: %v", needle, err)
		}
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("a refused plan-lock changed state.json:\nbefore %s\nafter  %s", before, after)
	}
	if got := statusAt(t, dir); got == "planned" {
		t.Error("current_phase_status reads planned after a refusal")
	}
}

// TestPlanLockNewAndPlannedClear: the same citations confirmed new in the
// spec, or each planned as a task's file, lock.
func TestPlanLockNewAndPlannedClear(t *testing.T) {
	t.Run("new", func(t *testing.T) {
		dir := gateRepo(t)
		writeGateSpec(t, dir, `new = ["`+citeOne+`", "`+citeTwo+`", "`+citeThree+`"]`,
			"see "+citeOne+" and "+citeTwo, "")
		writeGatePlan(t, dir, "then "+citeThree, "a.go")
		if err := setState(t, "current_phase_status", "planned"); err != nil {
			t.Fatalf("plan-lock refused citations the spec confirms new: %v", err)
		}
	})
	t.Run("planned", func(t *testing.T) {
		dir := gateRepo(t)
		writeGateSpec(t, dir, "", "see "+citeOne+" and "+citeTwo, "")
		writeGatePlan(t, dir, "then "+citeThree, citeOne, citeTwo, citeThree)
		if err := setState(t, "current_phase_status", "planned"); err != nil {
			t.Fatalf("plan-lock refused citations a task plans: %v", err)
		}
	})
}

// TestPlanLockFactsParity: the gate refuses iff `dross phase facts --json`
// reports, and lists exactly its lines.
func TestPlanLockFactsParity(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(t *testing.T, dir string)
		refuse bool
	}{
		{"clean", func(t *testing.T, dir string) {
			writeGateSpec(t, dir, "", "cites nothing at all", "")
		}, false},
		{"missing path", func(t *testing.T, dir string) {
			writeGateSpec(t, dir, "", "see "+citeOne, "")
		}, true},
		{"moved line", func(t *testing.T, dir string) {
			mustWrite(t, filepath.Join(dir, "x.go"), "package x\n\n// one\n// two\n// three\n\nfunc Init() {}\n")
			writeGateSpec(t, dir, "", "`Init` — x.go:3", "")
		}, true},
		{"why-only citation", func(t *testing.T, dir string) {
			writeGateSpec(t, dir, "", "cites nothing", "\n[[decisions]]\nkey = \"k\"\nchoice = \"plain\"\nwhy = '"+citeOne+" is not scanned here'\n")
		}, false},
		{"new-marked", func(t *testing.T, dir string) {
			writeGateSpec(t, dir, `new = ["`+citeOne+`"]`, "see "+citeOne, "")
		}, false},
		{"planned file", func(t *testing.T, dir string) {
			writeGateSpec(t, dir, "", "see "+citeOne, "")
			writeGatePlan(t, dir, "plain", citeOne)
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := gateRepo(t)
			tc.setup(t, dir)

			stdout, _, _ := runFacts(t, "fx", "--json")
			doc, _ := decodeFacts(t, stdout)
			cmdLines := []string{}
			for _, row := range doc.Unresolved {
				cmdLines = append(cmdLines, row.Line)
			}

			err := setState(t, "current_phase_status", "planned")
			if (err != nil) != tc.refuse {
				t.Fatalf("gate refused = %v, want %v: %v", err != nil, tc.refuse, err)
			}
			if (len(cmdLines) > 0) != tc.refuse {
				t.Fatalf("phase facts reported %q, want unresolved = %v", cmdLines, tc.refuse)
			}
			if got := gateLines(err); !reflect.DeepEqual(got, cmdLines) {
				t.Errorf("gate and command disagree:\n gate %q\n  cmd %q", got, cmdLines)
			}
		})
	}
}

// TestPlanLockFactsMalformedPlan: a plan that does not decode refuses,
// naming it — a phase with a broken plan is never judged clean.
func TestPlanLockFactsMalformedPlan(t *testing.T) {
	dir := gateRepo(t)
	writeGateSpec(t, dir, "", "cites nothing", "")
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "fx", "plan.toml"), "[[task]\nid = \n")
	err := setState(t, "current_phase_status", "planned")
	if err == nil {
		t.Fatal("plan-lock passed with a plan.toml that does not decode")
	}
	if !strings.Contains(err.Error(), filepath.Join("phases", "fx", "plan.toml")) {
		t.Errorf("refusal does not name the plan: %v", err)
	}
}

// TestPlanLockFactsGateScope: only `planned` runs the gate; absence allows;
// and on a spec with both an unresolved edge and an unresolved citation the
// edge gate answers first.
func TestPlanLockFactsGateScope(t *testing.T) {
	t.Run("other statuses", func(t *testing.T) {
		dir := gateRepo(t)
		writeGateSpec(t, dir, "", "see "+citeOne, "")
		for _, status := range []string{"in_progress", "executing"} {
			if err := setState(t, "current_phase_status", status); err != nil {
				t.Errorf("status %s ran the facts gate: %v", status, err)
			}
		}
	})
	t.Run("no spec", func(t *testing.T) {
		gateRepo(t)
		if err := setState(t, "current_phase_status", "planned"); err != nil {
			t.Errorf("a phase with no spec.toml refused plan-lock: %v", err)
		}
	})
	t.Run("no current phase", func(t *testing.T) {
		dir := factsRepo(t)
		writeGateSpec(t, dir, "", "see "+citeOne, "")
		if err := setState(t, "current_phase_status", "planned"); err != nil {
			t.Errorf("no current phase refused plan-lock: %v", err)
		}
	})
	t.Run("edge gate first", func(t *testing.T) {
		dir := gateRepo(t)
		writeGateSpec(t, dir, "", "see "+citeOne, badEdge)
		err := setState(t, "current_phase_status", "planned")
		if err == nil {
			t.Fatal("plan-lock passed with an unresolved edge and citation")
		}
		if !strings.Contains(err.Error(), "unresolved edge record") || strings.Contains(err.Error(), "unresolved citation") {
			t.Errorf("the edge gate did not answer first: %v", err)
		}
	})
}

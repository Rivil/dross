package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/facts"
)

// The fixtures are a temp repo whose .dross holds one phase, "fx". Outside git
// the facts tree walk falls back to WalkDir, so every file written under the
// repo is in the tree it checks.

// factsJSON is the --json document as /dross-plan reads it. Its tags are
// written out here, not borrowed from factsReport, so a renamed key fails.
type factsJSON struct {
	Phase      string `json:"phase"`
	Checked    string `json:"checked"`
	Unresolved []struct {
		Field    string `json:"field"`
		Kind     string `json:"kind"`
		Citation string `json:"citation"`
		Line     string `json:"line"`
		NewLine  int    `json:"new_line"`
	} `json:"unresolved"`
	Degraded bool `json:"degraded"`
}

// missingCite is a repo-relative path no fixture creates.
const missingCite = "internal/nope/missing.go"

// factsRepo makes the fixture repo and returns its directory (the .dross
// parent). The phase directory exists but holds nothing yet. state.json is
// seeded up front, as every initialised repo has it: FindRoot creates a
// missing one, and that write belongs to root resolution, not to facts.
func factsRepo(t *testing.T) string {
	t.Helper()
	root := chdirDross(t)
	mustWrite(t, filepath.Join(root, "state.json"), "{}\n")
	if err := os.MkdirAll(filepath.Join(root, "phases", "fx"), 0o755); err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(root)
}

// writeFactsSpec writes fx's spec with one criterion c-1 whose text is text.
func writeFactsSpec(t *testing.T, dir, text string) {
	t.Helper()
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "fx", "spec.toml"),
		"[phase]\nid = \"fx\"\ntitle = \"Fx\"\n\n[[criteria]]\nid = \"c-1\"\ntext = '"+text+"'\n")
}

// writeFactsPlan writes fx's plan with one task t-1 described by desc.
func writeFactsPlan(t *testing.T, dir, desc string) {
	t.Helper()
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "fx", "plan.toml"),
		"[phase]\nid = \"fx\"\n\n[[task]]\nid = \"t-1\"\ntitle = \"one\"\nwave = 1\nstatus = \"pending\"\ndescription = '"+desc+"'\nfiles = [\"a.go\"]\ncovers = [\"c-1\"]\n")
}

// runFacts runs `dross phase facts` with args, returning stdout, what cobra
// wrote to stderr, and the error.
func runFacts(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	c := Phase()
	c.SetArgs(append([]string{"facts"}, args...))
	c.SetOut(new(bytes.Buffer))
	var errBuf bytes.Buffer
	c.SetErr(&errBuf)
	stdout = captureStdout(t, func() { err = c.Execute() })
	return stdout, errBuf.String(), err
}

// decodeFacts decodes stdout as the --json document, also returning it as a
// generic map so null-vs-[] stays visible.
func decodeFacts(t *testing.T, out string) (factsJSON, map[string]any) {
	t.Helper()
	var doc factsJSON
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("--json stdout does not decode on its own: %v\n%s", err, out)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatalf("--json stdout is not an object: %v\n%s", err, out)
	}
	return doc, raw
}

// TestPhaseFactsCleanSpecOnly: a clean spec with no plan.toml exits 0, says
// nothing is unresolved and that it checked the spec alone; with a plan it
// names spec + plan.
func TestPhaseFactsCleanSpecOnly(t *testing.T) {
	dir := factsRepo(t)
	writeFactsSpec(t, dir, "plain prose citing nothing")

	out, _, err := runFacts(t, "fx")
	if err != nil {
		t.Fatalf("clean spec exited non-zero: %v\n%s", err, out)
	}
	if !strings.Contains(out, "nothing unresolved") || !strings.Contains(out, "spec only") {
		t.Errorf("clean spec-only run must say \"nothing unresolved\" and \"spec only\":\n%s", out)
	}

	writeFactsPlan(t, dir, "plain prose again")
	out, _, err = runFacts(t, "fx")
	if err != nil {
		t.Fatalf("clean spec + plan exited non-zero: %v\n%s", err, out)
	}
	if !strings.Contains(out, "nothing unresolved") || !strings.Contains(out, "spec + plan") {
		t.Errorf("clean spec + plan run must say \"nothing unresolved\" and \"spec + plan\":\n%s", out)
	}
}

// TestPhaseFactsMissingPathFails: one missing path exits non-zero and its
// line names the field and the path.
func TestPhaseFactsMissingPathFails(t *testing.T) {
	dir := factsRepo(t)
	writeFactsSpec(t, dir, "see "+missingCite+" for the details")

	out, _, err := runFacts(t, "fx")
	if err == nil {
		t.Fatalf("a missing path exited 0:\n%s", out)
	}
	var line string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, missingCite) {
			line = l
		}
	}
	if line == "" || !strings.Contains(line, "criteria c-1") {
		t.Errorf("no output line names both \"criteria c-1\" and %s:\n%s", missingCite, out)
	}
	if strings.Contains(out, "nothing unresolved") {
		t.Errorf("a run with a finding said nothing is unresolved:\n%s", out)
	}
}

// TestPhaseFactsJSON pins the payload: its keys, [] when clean, stdout that
// parses alone while the error goes to stderr, and a non-zero exit when
// anything is unresolved.
func TestPhaseFactsJSON(t *testing.T) {
	t.Run("clean", func(t *testing.T) {
		dir := factsRepo(t)
		writeFactsSpec(t, dir, "plain prose")
		out, _, err := runFacts(t, "fx", "--json")
		if err != nil {
			t.Fatalf("clean --json exited non-zero: %v\n%s", err, out)
		}
		doc, raw := decodeFacts(t, out)
		if doc.Phase != "fx" || doc.Checked != "spec only" {
			t.Errorf("phase/checked = %q/%q, want fx/spec only", doc.Phase, doc.Checked)
		}
		u, ok := raw["unresolved"].([]any)
		if !ok || u == nil || len(u) != 0 {
			t.Errorf("clean `unresolved` must be [] (never null), got %#v\n%s", raw["unresolved"], out)
		}
		if d, ok := raw["degraded"].(bool); !ok || d {
			t.Errorf("clean, non-degraded run must carry \"degraded\": false, got %#v", raw["degraded"])
		}
	})

	t.Run("unresolved", func(t *testing.T) {
		dir := factsRepo(t)
		writeFactsSpec(t, dir, "see "+missingCite)
		out, stderr, err := runFacts(t, "fx", "--json")
		if err == nil {
			t.Fatalf("--json with a finding exited 0:\n%s", out)
		}
		if !strings.Contains(stderr, err.Error()) {
			t.Errorf("the error must go to stderr, got stderr %q", stderr)
		}
		if strings.Contains(out, err.Error()) {
			t.Errorf("the error leaked onto stdout:\n%s", out)
		}
		doc, raw := decodeFacts(t, out)
		if len(doc.Unresolved) != 1 {
			t.Fatalf("want 1 unresolved row, got %d:\n%s", len(doc.Unresolved), out)
		}
		row := doc.Unresolved[0]
		want := "criteria c-1: missing " + missingCite
		if row.Field != "criteria c-1" || row.Kind != "missing" || row.Citation != missingCite || row.Line != want || row.NewLine != 0 {
			t.Errorf("row = %+v, want field criteria c-1, kind missing, citation %s, line %q, new_line 0", row, missingCite, want)
		}
		keys := []string{}
		for k := range raw["unresolved"].([]any)[0].(map[string]any) {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if wantKeys := []string{"citation", "field", "kind", "line", "new_line"}; !reflect.DeepEqual(keys, wantKeys) {
			t.Errorf("row keys = %v, want %v", keys, wantKeys)
		}
		for _, k := range []string{"phase", "checked", "unresolved", "degraded"} {
			if _, ok := raw[k]; !ok {
				t.Errorf("payload lacks %q:\n%s", k, out)
			}
		}
	})
}

// TestPhaseFactsRendersEveryKind: synthetic moved / gone / unresolved
// Findings render with their kind, and a moved one with its new line, in both
// the --json rows and the plain lines.
func TestPhaseFactsRendersEveryKind(t *testing.T) {
	rep := facts.Report{Unresolved: []facts.Finding{
		{Field: "criteria c-2", Kind: facts.KindMoved, Citation: "`Init` — x.go:3", NewLine: 7},
		{Field: "task t-1 description", Kind: facts.KindGone, Citation: "`Gone` — x.go:9"},
		{Field: "decisions symbol_shape choice", Kind: facts.KindUnresolved, Citation: "quoted.String()"},
	}}
	r := buildFactsReport("fx", factsCheckedSpecPlan, rep)
	want := []factsRow{
		{Field: "criteria c-2", Kind: "moved", Citation: "`Init` — x.go:3", Line: "criteria c-2: moved `Init` — x.go:3 now at line 7", NewLine: 7},
		{Field: "task t-1 description", Kind: "gone", Citation: "`Gone` — x.go:9", Line: "task t-1 description: gone `Gone` — x.go:9"},
		{Field: "decisions symbol_shape choice", Kind: "unresolved", Citation: "quoted.String()", Line: "decisions symbol_shape choice: unresolved quoted.String()"},
	}
	if !reflect.DeepEqual(r.Unresolved, want) {
		t.Errorf("rows =\n%#v\nwant\n%#v", r.Unresolved, want)
	}
	out := captureStdout(t, func() { printFacts(r) })
	for _, w := range want {
		if !strings.Contains(out, w.Line+"\n") {
			t.Errorf("plain output lacks %q:\n%s", w.Line, out)
		}
	}
	if strings.Contains(out, "nothing unresolved") {
		t.Errorf("findings printed the clean line:\n%s", out)
	}
}

// hashTree maps every file under dir to its content hash.
func hashTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	sums := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		h := sha256.Sum256(b)
		sums[p] = hex.EncodeToString(h[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return sums
}

// TestPhaseFactsNeverWrites: clean or not, plain or --json, every file under
// the fixture is byte-identical afterwards, none is added, and no *.tmp is
// left behind.
func TestPhaseFactsNeverWrites(t *testing.T) {
	// wantErr is what each run must return: nil when clean, the unresolved
	// count when not — so a run that failed early, writing nothing because it
	// did nothing, cannot pass the hash check vacuously.
	for _, tc := range []struct{ name, text, wantErr string }{
		{"clean", "plain prose", ""},
		{"unresolved", "see " + missingCite + " and `nopkg.Missing()`", "unresolved citation(s)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := factsRepo(t)
			writeFactsSpec(t, dir, tc.text)
			writeFactsPlan(t, dir, "plain prose")
			mustWrite(t, filepath.Join(dir, "a.go"), "package a\n\nfunc A() {}\n")
			before := hashTree(t, dir)
			for _, args := range [][]string{{"fx"}, {"fx", "--json"}} {
				_, _, err := runFacts(t, args...)
				switch {
				case tc.wantErr == "" && err != nil:
					t.Errorf("phase facts %v on a clean phase: %v, want nil", args, err)
				case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
					t.Errorf("phase facts %v = %v, want an error naming %q", args, err, tc.wantErr)
				}
			}
			after := hashTree(t, dir)
			if !reflect.DeepEqual(before, after) {
				t.Errorf("phase facts changed the tree:\nbefore %v\nafter  %v", before, after)
			}
			for p := range after {
				if strings.HasSuffix(p, ".tmp") {
					t.Errorf("phase facts left %s behind", p)
				}
			}
		})
	}
}

// TestPhaseFactsAbsenceIsNotClean (c-9): a phase with no spec, an unknown id,
// an escaping id and a spec or plan that does not decode each exit non-zero
// naming what is wrong, and none says nothing is unresolved.
func TestPhaseFactsAbsenceIsNotClean(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, dir string)
		id    string
		names func(dir string) string
	}{
		{
			name:  "no spec.toml",
			setup: func(t *testing.T, dir string) { writeFactsPlan(t, dir, "plain prose") },
			id:    "fx",
			names: func(string) string { return filepath.Join("phases", "fx", "spec.toml") },
		},
		{
			name:  "unknown id",
			setup: func(*testing.T, string) {},
			id:    "no-such-phase",
			names: func(string) string { return filepath.Join("phases", "no-such-phase") },
		},
		{
			name:  "escaping id",
			setup: func(*testing.T, string) {},
			id:    "../x",
			names: func(string) string { return "../x" },
		},
		{
			name: "spec does not decode",
			setup: func(t *testing.T, dir string) {
				mustWrite(t, filepath.Join(dir, ".dross", "phases", "fx", "spec.toml"), "[phase\nid = \"fx\"\n")
			},
			id:    "fx",
			names: func(string) string { return filepath.Join("phases", "fx", "spec.toml") },
		},
		{
			name: "plan does not decode",
			setup: func(t *testing.T, dir string) {
				writeFactsSpec(t, dir, "plain prose")
				mustWrite(t, filepath.Join(dir, ".dross", "phases", "fx", "plan.toml"), "[[task]\nid = \"t-1\"\n")
			},
			id:    "fx",
			names: func(string) string { return filepath.Join("phases", "fx", "plan.toml") },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := factsRepo(t)
			tc.setup(t, dir)
			for _, args := range [][]string{{tc.id}, {tc.id, "--json"}} {
				out, _, err := runFacts(t, args...)
				if err == nil {
					t.Fatalf("%v exited 0:\n%s", args, out)
				}
				if want := tc.names(dir); !strings.Contains(err.Error(), want) {
					t.Errorf("%v: error does not name %s: %v", args, want, err)
				}
				if strings.Contains(out, "nothing unresolved") {
					t.Errorf("%v said nothing is unresolved:\n%s", args, out)
				}
				if strings.TrimSpace(out) != "" {
					t.Errorf("%v printed a report for a phase it could not check:\n%s", args, out)
				}
			}
		})
	}
}

// TestPhaseFactsDegradedIsSaid: a tree holding a source file codex cannot
// index (a .py) leaves an unmatched symbol skipped, not reported; the clean
// run still says nothing is unresolved, adds that symbol checks were skipped,
// and --json carries "degraded": true.
func TestPhaseFactsDegradedIsSaid(t *testing.T) {
	dir := factsRepo(t)
	writeFactsSpec(t, dir, "calls `nopkg.Missing()` on start")
	mustWrite(t, filepath.Join(dir, "tool.py"), "def main():\n    pass\n")

	out, _, err := runFacts(t, "fx")
	if err != nil {
		t.Fatalf("a degraded clean run exited non-zero: %v\n%s", err, out)
	}
	if !strings.Contains(out, "nothing unresolved") || !strings.Contains(out, "symbol checks were skipped") {
		t.Errorf("degraded clean run must say \"nothing unresolved\" and that symbol checks were skipped:\n%s", out)
	}
	if !strings.Contains(out, "tool.py") {
		t.Errorf("the skipped line must say why (the file that degraded the index):\n%s", out)
	}

	out, _, err = runFacts(t, "fx", "--json")
	if err != nil {
		t.Fatalf("degraded clean --json exited non-zero: %v\n%s", err, out)
	}
	_, raw := decodeFacts(t, out)
	if raw["degraded"] != true {
		t.Errorf("--json must carry \"degraded\": true, got %#v\n%s", raw["degraded"], out)
	}
}

// TestPhaseFactsIsRegistered: the verb and its --json flag resolve under
// `phase`, and README's phase row lists it.
func TestPhaseFactsIsRegistered(t *testing.T) {
	root := &cobra.Command{Use: "dross"}
	root.AddCommand(Phase())
	c, _, err := root.Find([]string{"phase", "facts"})
	if err != nil {
		t.Fatalf("phase facts does not resolve: %v", err)
	}
	if c.CommandPath() != "dross phase facts" {
		t.Errorf("resolved %q, want dross phase facts", c.CommandPath())
	}
	if c.Flags().Lookup("json") == nil {
		t.Error("phase facts has no --json flag")
	}
	if !hasVerb(readmePhaseVerbs(t), "facts") {
		t.Error("README's phase row does not list the `facts` verb")
	}
}

package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/phase"
)

// The fixtures reuse validate's edgeRepo / writeEdgeSpec: phase "edgy" with
// criteria c-1 and c-2, and deferred ids fff (phase "other") and ggg (the
// project store).

// edgesJSON is the --json document as /dross-spec reads it. Its tags are
// written out here, not borrowed from edgesReport, so a renamed key fails.
type edgesJSON struct {
	Classes    []edgesJSONClass `json:"classes"`
	Resolved   []string         `json:"resolved"`
	Unresolved []string         `json:"unresolved"`
	Open       []edgesJSONClass `json:"open"`
}

type edgesJSONClass struct {
	Class string `json:"class"`
	Hint  string `json:"hint"`
}

// mixedEdges: boundary covered, empty dismissed with a reason, encoding
// dismissed with an empty one. Records are deliberately out of EdgeClasses
// order, so output that followed record order would show it.
const mixedEdges = `
[[edges]]
class = "encoding"
outcome = "dismissed"
reason = ""

[[edges]]
class = "empty"
outcome = "dismissed"
reason = "the input is always a non-empty list"

[[edges]]
class = "boundary"
outcome = "covered"
criteria = ["c-1"]
`

// runEdges runs `dross phase edges` with args and returns stdout.
func runEdges(t *testing.T, args ...string) string {
	t.Helper()
	var out string
	if err := runCmdCapturing(t, &out, Phase(), append([]string{"edges"}, args...)...); err != nil {
		t.Fatalf("phase edges %v: %v\n%s", args, err, out)
	}
	return out
}

// decodeEdges runs `phase edges <id> --json` and decodes the document, also
// returning it as a generic map so null-vs-[] stays visible.
func decodeEdges(t *testing.T, id string) (edgesJSON, map[string]any) {
	t.Helper()
	out := runEdges(t, id, "--json")
	var doc edgesJSON
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("--json output does not decode: %v\n%s", err, out)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatalf("--json output is not an object: %v\n%s", err, out)
	}
	return doc, raw
}

// classLines maps each class to its status line in plain output — the text
// after `<class>: ` on the line that leads with it.
func classLines(out string) map[string]string {
	lines := map[string]string{}
	for _, l := range strings.Split(out, "\n") {
		for _, c := range phase.EdgeClasses {
			if rest, ok := strings.CutPrefix(l, c.Name+": "); ok {
				lines[c.Name] = rest
			}
		}
	}
	return lines
}

// wantRows is EdgeClasses' rows for names, in the order given.
func wantRows(t *testing.T, names ...string) []edgesJSONClass {
	t.Helper()
	hints := map[string]string{}
	for _, c := range phase.EdgeClasses {
		hints[c.Name] = c.Hint
	}
	rows := make([]edgesJSONClass, 0, len(names))
	for _, n := range names {
		if hints[n] == "" {
			t.Fatalf("%q is not an edge class", n)
		}
		rows = append(rows, edgesJSONClass{Class: n, Hint: hints[n]})
	}
	return rows
}

func allClassNames() []string {
	names := make([]string, len(phase.EdgeClasses))
	for i, c := range phase.EdgeClasses {
		names[i] = c.Name
	}
	return names
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalRows(a, b []edgesJSONClass) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// assertEmptyArray fails unless raw[key] is a JSON array of length zero —
// null decodes to nil and fails too.
func assertEmptyArray(t *testing.T, raw map[string]any, key string) {
	t.Helper()
	v, ok := raw[key].([]any)
	if !ok || v == nil {
		t.Errorf("%q = %#v, want an empty JSON array (not null, not absent)", key, raw[key])
		return
	}
	if len(v) != 0 {
		t.Errorf("%q = %v, want empty", key, v)
	}
}

// TestPhaseEdgesJSONPartitionsClasses (c-6): a resolved class never leaks
// into open, and a reasonless dismissal is unresolved, not resolved. Open is
// exactly the five unrecorded classes, each carrying its hint.
func TestPhaseEdgesJSONPartitionsClasses(t *testing.T) {
	dir := edgeRepo(t)
	writeEdgeSpec(t, dir, mixedEdges)

	doc, _ := decodeEdges(t, "edgy")
	if want := []string{"boundary", "empty"}; !equalStrings(doc.Resolved, want) {
		t.Errorf("resolved = %v, want %v", doc.Resolved, want)
	}
	if want := []string{"encoding"}; !equalStrings(doc.Unresolved, want) {
		t.Errorf("unresolved = %v, want %v", doc.Unresolved, want)
	}
	want := wantRows(t, "ordering", "precision", "idempotency", "concurrency", "adjacency")
	if !equalRows(doc.Open, want) {
		t.Errorf("open = %+v, want %+v", doc.Open, want)
	}
	for _, r := range doc.Open {
		if strings.TrimSpace(r.Hint) == "" {
			t.Errorf("open class %q has an empty hint", r.Class)
		}
	}
}

// TestPhaseEdgesPlainReasonlessDismissalIsUnresolved: in plain output the
// reasonless dismissal reads `unresolved:` with the record's problem and is
// never marked `(resolved)`; the resolved and open classes say so.
func TestPhaseEdgesPlainReasonlessDismissalIsUnresolved(t *testing.T) {
	dir := edgeRepo(t)
	writeEdgeSpec(t, dir, mixedEdges)

	lines := classLines(runEdges(t, "edgy"))
	enc := lines["encoding"]
	if !strings.HasPrefix(enc, "unresolved: ") {
		t.Errorf("encoding line %q does not lead with `unresolved:`", enc)
	}
	if !strings.Contains(enc, "edges[0]") || !strings.Contains(enc, "dismissed with no reason") {
		t.Errorf("encoding line %q must name edges[0] and its problem", enc)
	}
	if strings.Contains(enc, "(resolved)") {
		t.Errorf("encoding line %q reads as resolved", enc)
	}
	if got, want := lines["boundary"], "covered (resolved)"; got != want {
		t.Errorf("boundary line = %q, want %q", got, want)
	}
	if got, want := lines["empty"], "dismissed (resolved)"; got != want {
		t.Errorf("empty line = %q, want %q", got, want)
	}
	if got := lines["ordering"]; got != "open" {
		t.Errorf("ordering line = %q, want open", got)
	}
}

// TestPhaseEdgesPlainFollowsEdgeClassesOrder is the golden order check: the
// eight class lines appear in EdgeClasses order, each followed by its hint,
// whatever order the spec records them in.
func TestPhaseEdgesPlainFollowsEdgeClassesOrder(t *testing.T) {
	dir := edgeRepo(t)
	writeEdgeSpec(t, dir, mixedEdges)

	out := runEdges(t, "edgy")
	all := strings.Split(out, "\n")
	var got []string
	for i, l := range all {
		for _, c := range phase.EdgeClasses {
			if strings.HasPrefix(l, c.Name+": ") {
				got = append(got, c.Name)
				if i+1 >= len(all) || strings.TrimSpace(all[i+1]) != c.Hint {
					t.Errorf("%s's line is not followed by its hint %q", c.Name, c.Hint)
				}
			}
		}
	}
	if want := allClassNames(); !equalStrings(got, want) {
		t.Errorf("class lines in order %v, want %v\n%s", got, want, out)
	}
}

// TestPhaseEdgesPlainListsUnknownClassRecords: a record naming a class
// outside the set falls in no partition, so the plain listing names it after
// the eight classes rather than dropping it.
func TestPhaseEdgesPlainListsUnknownClassRecords(t *testing.T) {
	dir := edgeRepo(t)
	writeEdgeSpec(t, dir, `
[[edges]]
class = "latency"
outcome = "dismissed"
reason = "not a network path"
`)
	out := runEdges(t, "edgy")
	_, tail, ok := strings.Cut(out, phase.EdgeClasses[len(phase.EdgeClasses)-1].Hint)
	if !ok {
		t.Fatalf("output has no last-class hint:\n%s", out)
	}
	var line string
	for _, l := range strings.Split(tail, "\n") {
		if strings.Contains(l, `"latency"`) || strings.Contains(l, "(latency)") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("the latency record is not listed after the eight classes:\n%s", out)
	}
	if !strings.Contains(line, "unknown class") {
		t.Errorf("latency line %q does not call it an unknown class", line)
	}
	if !strings.Contains(tail, "unknown") {
		t.Errorf("no unknown-class heading after the eight classes:\n%s", out)
	}
}

// TestPhaseEdgesAbsenceIsAllOpen (c-5): a legacy spec with no [[edges]], and a
// phase with no spec.toml at all, report every class open, and resolved /
// unresolved as empty arrays rather than null.
func TestPhaseEdgesAbsenceIsAllOpen(t *testing.T) {
	dir := edgeRepo(t)
	writeEdgeSpec(t, dir, "")
	if err := os.MkdirAll(filepath.Join(dir, ".dross", "phases", "bare"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"edgy", "bare"} {
		t.Run(id, func(t *testing.T) {
			doc, raw := decodeEdges(t, id)
			if want := wantRows(t, allClassNames()...); !equalRows(doc.Open, want) {
				t.Errorf("open = %+v, want all eight %+v", doc.Open, want)
			}
			assertEmptyArray(t, raw, "resolved")
			assertEmptyArray(t, raw, "unresolved")

			lines := classLines(runEdges(t, id))
			for _, c := range phase.EdgeClasses {
				if lines[c.Name] != "open" {
					t.Errorf("plain %s line = %q, want open", c.Name, lines[c.Name])
				}
			}
		})
	}
}

// TestPhaseEdgesClassesAlwaysListsAllEight: classes is built from EdgeClasses,
// not from open — so with every class resolved, open is [] but classes still
// carries all eight, in order, with their hints.
func TestPhaseEdgesClassesAlwaysListsAllEight(t *testing.T) {
	dir := edgeRepo(t)
	var b strings.Builder
	for _, c := range phase.EdgeClasses {
		b.WriteString("\n[[edges]]\nclass = \"" + c.Name + "\"\noutcome = \"dismissed\"\nreason = \"not in scope\"\n")
	}
	writeEdgeSpec(t, dir, b.String())

	for _, tc := range []struct {
		name string
		id   string
	}{{"all-resolved", "edgy"}, {"mixed", "mixed"}} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.id == "mixed" {
				mustWrite(t, filepath.Join(dir, ".dross", "phases", "mixed", "spec.toml"),
					"[phase]\nid = \"mixed\"\n\n[[criteria]]\nid = \"c-1\"\ntext = \"x\"\n"+mixedEdges)
			}
			doc, raw := decodeEdges(t, tc.id)
			want := wantRows(t, allClassNames()...)
			if !equalRows(doc.Classes, want) {
				t.Errorf("classes = %+v, want %+v", doc.Classes, want)
			}
			for _, r := range doc.Classes {
				if strings.TrimSpace(r.Hint) == "" {
					t.Errorf("class %q has an empty hint", r.Class)
				}
			}
			if tc.id == "edgy" {
				if !equalStrings(doc.Resolved, allClassNames()) {
					t.Errorf("resolved = %v, want all eight", doc.Resolved)
				}
				assertEmptyArray(t, raw, "open")
			}
		})
	}
}

// TestPhaseEdgesJudgesDeferredAgainstTheProject: a deferred edge resolves
// against an id parked in another phase, and dangles on an id nobody holds —
// the project-wide set validate and plan-lock use.
func TestPhaseEdgesJudgesDeferredAgainstTheProject(t *testing.T) {
	dir := edgeRepo(t)
	writeEdgeSpec(t, dir, `
[[edges]]
class = "ordering"
outcome = "deferred"
deferred = "fff"

[[edges]]
class = "precision"
outcome = "deferred"
deferred = "zzz"
`)
	doc, _ := decodeEdges(t, "edgy")
	if want := []string{"ordering"}; !equalStrings(doc.Resolved, want) {
		t.Errorf("resolved = %v, want %v", doc.Resolved, want)
	}
	if want := []string{"precision"}; !equalStrings(doc.Unresolved, want) {
		t.Errorf("unresolved = %v, want %v", doc.Unresolved, want)
	}
}

// TestPhaseEdgesRefusesUnknownPhase: an id with no phase directory exits
// non-zero naming the id, rather than reporting eight open classes.
func TestPhaseEdgesRefusesUnknownPhase(t *testing.T) {
	edgeRepo(t)
	var out string
	err := runCmdCapturing(t, &out, Phase(), "edges", "no-such-phase")
	if err == nil {
		t.Fatalf("phase edges accepted an unknown id; printed:\n%s", out)
	}
	if !strings.Contains(err.Error(), "no-such-phase") {
		t.Errorf("error does not name the id: %v", err)
	}
}

// TestPhaseEdgesRefusesUndecodableSpec: a spec that exists but does not decode
// is an error naming it, not an all-open report.
func TestPhaseEdgesRefusesUndecodableSpec(t *testing.T) {
	dir := edgeRepo(t)
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "broken", "spec.toml"), "[phase\nid = \"broken\"\n")
	err := runCmd(t, Phase(), "edges", "broken")
	if err == nil {
		t.Fatal("phase edges reported on a spec that does not decode")
	}
	if !strings.Contains(err.Error(), filepath.Join("broken", "spec.toml")) {
		t.Errorf("error does not name the spec: %v", err)
	}
}

// readmePhaseVerbs returns the verbs README's `dross phase {…}` command row
// lists. The head carries the table's leading pipe (docText strips the
// backticks): the status paragraph names `dross phase {insert,move,rename}`
// first, and a bare search would read that list instead of the row's.
func readmePhaseVerbs(t *testing.T) []string {
	t.Helper()
	doc := docText(t, "README.md")
	const head = "| dross phase {"
	at := strings.Index(doc, head)
	if at < 0 {
		t.Fatal("README.md has no `dross phase {…}` row")
	}
	rest := doc[at+len(head):]
	end := strings.Index(rest, "}")
	if end < 0 {
		t.Fatal("README.md's `dross phase {…}` row never closes its brace list")
	}
	return strings.Split(rest[:end], ",")
}

// TestPhaseEdgesIsRegistered: the verb resolves under `phase`, and README's
// phase row lists it.
func TestPhaseEdgesIsRegistered(t *testing.T) {
	root := &cobra.Command{Use: "dross"}
	root.AddCommand(Phase())
	c, _, err := root.Find([]string{"phase", "edges"})
	if err != nil {
		t.Fatalf("phase edges does not resolve: %v", err)
	}
	if c.CommandPath() != "dross phase edges" {
		t.Errorf("resolved %q, want dross phase edges", c.CommandPath())
	}
	if !hasVerb(readmePhaseVerbs(t), "edges") {
		t.Error("README's phase row does not list the `edges` verb")
	}
}

package boardsync

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/deferred"
	"github.com/Rivil/dross/internal/mutation"
	"github.com/Rivil/dross/internal/survivor"
	"github.com/Rivil/dross/internal/verify"
)

const survivorFile = "internal/x.go"

// routedSurvivor is a survivor src routed to tgt, filed the way `dross
// survivor route` files it.
var routedSurvivor = deferred.Entry{
	Source: "src", Index: 0, ID: "id1", Target: "tgt", Survivor: "K",
	Text: "survivor " + survivorFile + ":4 (OP)",
}

// runSpec describes one phase's verify run for the fixture.
type runSpec struct {
	at         time.Time
	finalized  bool
	scope      []string // scope.Files
	legFiles   []string // the go leg's Files
	legErr     string
	surviving  []string // keys still surviving in the leg
	outOfScope []string // keys in out_of_scope
}

func writeRun(t *testing.T, root, phaseID string, r runSpec) {
	t.Helper()
	dir := filepath.Join(root, "phases", phaseID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	rep := &mutation.Report{Tool: "gremlins"}
	for _, k := range r.surviving {
		rep.Surviving = append(rep.Surviving, mutation.Mutant{File: survivorFile, Line: 4, Op: "OP", Key: k})
	}
	tests := &verify.Tests{
		Phase:       phaseID,
		GeneratedAt: r.at,
		Languages:   []verify.LanguageRun{{Name: "go", Tool: "gremlins", Files: r.legFiles, Mutation: rep, Error: r.legErr}},
		Scope:       &verify.Scope{Files: r.scope, Source: "git"},
	}
	for _, k := range r.outOfScope {
		tests.OutOfScope = append(tests.OutOfScope, verify.OutOfScopeMutant{File: survivorFile, Line: 4, Key: k})
	}
	if err := tests.Save(filepath.Join(dir, verify.TestsFile)); err != nil {
		t.Fatal(err)
	}
	v := &verify.Verify{Verify: verify.VerifyMeta{Phase: phaseID, GeneratedAt: r.at, Verdict: "pass", Finalized: r.finalized}}
	if err := v.Save(filepath.Join(dir, verify.VerifyFile)); err != nil {
		t.Fatal(err)
	}
}

func dispositionRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), ".dross")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

var (
	foundAt = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	laterAt = foundAt.Add(48 * time.Hour)
)

// goodRun is a destination run that does dispose of K: finalized, newer than
// the run that found it, its leg clean, the file in scope, K gone.
func goodRun() runSpec {
	return runSpec{at: laterAt, finalized: true, scope: []string{survivorFile}, legFiles: []string{survivorFile}}
}

func TestDisposedSurvivorAccepted(t *testing.T) {
	root := dispositionRoot(t)
	if err := survivor.Save(survivor.Path(root), &survivor.Store{Accepted: []survivor.Acceptance{{Key: "K", File: survivorFile, Op: "OP", Text: "x", Reason: "fine"}}}); err != nil {
		t.Fatal(err)
	}
	ok, why := Disposed(root, routedSurvivor)
	if !ok || !strings.Contains(why, survivor.StoreFile) {
		t.Errorf("Disposed = %v, %q; want true naming %s", ok, why, survivor.StoreFile)
	}
}

func TestDisposedSurvivorByDestinationRun(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(*runSpec)
		e    func(*deferred.Entry)
		none bool // no destination run at all
		want bool
	}{
		{name: "measured and gone", want: true},
		{name: "no destination run", none: true},
		{name: "key still surviving", run: func(r *runSpec) { r.surviving = []string{"K"} }},
		{name: "key out of scope", run: func(r *runSpec) { r.outOfScope = []string{"K"} }},
		{name: "scope never touched the file", run: func(r *runSpec) { r.scope = []string{"other.go"} }},
		{name: "leg errored", run: func(r *runSpec) { r.legErr = "gremlins exited 2" }},
		{name: "run older than the one that found it", run: func(r *runSpec) { r.at = foundAt.Add(-time.Hour) }},
		{name: "verify not finalized", run: func(r *runSpec) { r.finalized = false }},
		{name: "text names no file", e: func(e *deferred.Entry) { e.Text = "a free-form note" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := dispositionRoot(t)
			writeRun(t, root, "src", runSpec{at: foundAt, finalized: true, scope: []string{survivorFile}, legFiles: []string{survivorFile}, surviving: []string{"K"}})
			if !tc.none {
				r := goodRun()
				if tc.run != nil {
					tc.run(&r)
				}
				writeRun(t, root, "tgt", r)
			}
			e := routedSurvivor
			if tc.e != nil {
				tc.e(&e)
			}
			ok, why := Disposed(root, e)
			if ok != tc.want {
				t.Fatalf("Disposed = %v (%q), want %v", ok, why, tc.want)
			}
			if ok && !strings.Contains(why, "phases/tgt/"+verify.TestsFile) {
				t.Errorf("why = %q, want it to name the destination's run", why)
			}
		})
	}
}

// writeAbsorbed sets up tgt's spec — c-2 absorbing ids — and its changes.json.
func writeAbsorbed(t *testing.T, root, phaseID, status string, ids ...string) {
	t.Helper()
	dir := filepath.Join(root, "phases", phaseID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	quoted := make([]string, len(ids))
	for i, id := range ids {
		quoted[i] = `"` + id + `"`
	}
	spec := "[phase]\nid = \"" + phaseID + "\"\ntitle = \"T\"\n\n[[criteria]]\nid = \"c-1\"\ntext = \"x\"\n\n[[criteria]]\nid = \"c-2\"\ntext = \"y\"\ndeferred = [" + strings.Join(quoted, ", ") + "]\n"
	if err := os.WriteFile(filepath.Join(dir, "spec.toml"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "changes.json"), []byte(`{"phase":"`+phaseID+`","status":"`+status+`","tasks":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

var routedItem = deferred.Entry{Source: "src", Index: 1, ID: "id2", Target: "tgt", Text: "an idea"}

func TestDisposedAbsorbedItem(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, root string)
		e     func(*deferred.Entry)
		want  bool
	}{
		{name: "complete target absorbed it", want: true, setup: func(t *testing.T, root string) { writeAbsorbed(t, root, "tgt", "complete", "id2") }},
		{name: "complete target never absorbed it", setup: func(t *testing.T, root string) { writeAbsorbed(t, root, "tgt", "complete") }},
		{name: "absorbed but the target only shipped", setup: func(t *testing.T, root string) { writeAbsorbed(t, root, "tgt", "shipped", "id2") }},
		{name: "absorbed by a complete phase that is not its target", setup: func(t *testing.T, root string) {
			writeAbsorbed(t, root, "tgt", "complete")
			writeAbsorbed(t, root, "elsewhere", "complete", "id2")
		}},
		{name: "item with no id", setup: func(t *testing.T, root string) { writeAbsorbed(t, root, "tgt", "complete", "") }, e: func(e *deferred.Entry) { e.ID = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := dispositionRoot(t)
			tc.setup(t, root)
			e := routedItem
			if tc.e != nil {
				tc.e(&e)
			}
			ok, why := Disposed(root, e)
			if ok != tc.want {
				t.Fatalf("Disposed = %v (%q), want %v", ok, why, tc.want)
			}
			if ok && (!strings.Contains(why, "phases/tgt/spec.toml") || !strings.Contains(why, "c-2")) {
				t.Errorf("why = %q, want it to name phases/tgt/spec.toml and c-2", why)
			}
		})
	}
}

// TestDisposedReadsOnlyDisk: the predicate takes the root and the entry and
// nothing else — no board client can reach it, so no verdict can come from a
// card's own state.
func TestDisposedReadsOnlyDisk(t *testing.T) {
	typ := reflect.TypeOf(Disposed)
	if typ.NumIn() != 2 || typ.In(0).Kind() != reflect.String || typ.In(1) != reflect.TypeOf(deferred.Entry{}) {
		t.Errorf("Disposed has signature %v, want func(string, deferred.Entry) (bool, string)", typ)
	}
}

// TestDisposedSurvivorFailsClosed: what cannot be shown is not evidence. With
// no run on disk for the phase that found the survivor, the order of runs is
// unknown; a surviving mutant in the same file under the same operator whose
// identity never resolved may be this one.
func TestDisposedSurvivorFailsClosed(t *testing.T) {
	t.Run("no run for the phase that found it", func(t *testing.T) {
		root := dispositionRoot(t)
		writeRun(t, root, "tgt", goodRun())
		if ok, why := Disposed(root, routedSurvivor); ok {
			t.Errorf("disposed with no source run: %q", why)
		}
	})
	t.Run("an unkeyed survivor in the same file and op", func(t *testing.T) {
		root := dispositionRoot(t)
		writeRun(t, root, "src", runSpec{at: foundAt, finalized: true, scope: []string{survivorFile}, legFiles: []string{survivorFile}, surviving: []string{"K"}})
		r := goodRun()
		r.surviving = []string{""} // still there, identity unresolved
		writeRun(t, root, "tgt", r)
		if ok, why := Disposed(root, routedSurvivor); ok {
			t.Errorf("disposed while an unkeyed survivor still sits in the file: %q", why)
		}
	})
}

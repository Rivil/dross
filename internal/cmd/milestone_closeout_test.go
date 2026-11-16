package cmd

import (
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/milestone"
	"github.com/Rivil/dross/internal/mutation"
	"github.com/Rivil/dross/internal/survivor"
	"github.com/Rivil/dross/internal/verify"
)

// ckRoot returns a fresh .dross directory carrying a milestone v9.9 with the
// given phases.
func ckRoot(t *testing.T, phases ...string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), ".dross")
	m := &milestone.Milestone{Milestone: milestone.Meta{Version: "v9.9"}, Phases: phases}
	if err := m.Save(milestone.FilePath(root, "v9.9")); err != nil {
		t.Fatal(err)
	}
	return root
}

// ckPhase makes a phase dir; a non-empty status writes a changes.json with it.
func ckPhase(t *testing.T, root, id, status string) {
	t.Helper()
	dir := filepath.Join(root, "phases", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if status != "" {
		mustWrite(t, filepath.Join(dir, "changes.json"), `{"phase":"`+id+`","status":"`+status+`","tasks":{}}`)
	}
}

func ckSpec(t *testing.T, root, id, body string) string {
	t.Helper()
	ckPhase(t, root, id, "")
	path := filepath.Join(root, "phases", id, "spec.toml")
	mustWrite(t, path, "[phase]\n  id = \""+id+"\"\n\n"+body)
	return path
}

func ckVerify(t *testing.T, root, id string, at time.Time, recs ...verify.SurvivorRecord) {
	t.Helper()
	ckPhase(t, root, id, "")
	v := &verify.Verify{
		Verify:    verify.VerifyMeta{Phase: id, Verdict: "pass", Finalized: true, GeneratedAt: at},
		Survivors: recs,
	}
	_, path := verify.FilePaths(root, id)
	if err := v.Save(path); err != nil {
		t.Fatal(err)
	}
}

func ckAccept(t *testing.T, root, key string) {
	t.Helper()
	s := &survivor.Store{Accepted: []survivor.Acceptance{{Key: key, File: "f.go", Op: "op", Text: "x := 1", Reason: "equivalent"}}}
	if err := survivor.Save(survivor.Path(root), s); err != nil {
		t.Fatal(err)
	}
}

func ckKind(items []closeoutItem, kind string) []closeoutItem {
	var out []closeoutItem
	for _, it := range items {
		if it.Kind == kind {
			out = append(out, it)
		}
	}
	return out
}

func ckSubjects(items []closeoutItem) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Subject)
	}
	return out
}

func ckCollect(t *testing.T, root string) closeoutReport {
	t.Helper()
	r, err := milestoneCloseout(root, "v9.9")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// c-1: done and shipped phases pass; a dir with no status, an unscaffolded
// entry and an unreadable record do not; a repeat is one phase.
func TestCloseoutNotDoneKind(t *testing.T) {
	root := ckRoot(t, "done", "shipped", "built", "ghost", "built", "broken")
	ckPhase(t, root, "done", "complete")
	ckPhase(t, root, "shipped", "shipped")
	ckPhase(t, root, "built", "")
	ckPhase(t, root, "broken", "")
	mustWrite(t, filepath.Join(root, "phases", "broken", "changes.json"), "{not json")

	got := ckKind(ckCollect(t, root).Items, closeoutNotDone)
	if want := []string{"built", "ghost", "broken"}; !slices.Equal(ckSubjects(got), want) {
		t.Fatalf("not-done = %v, want %v", ckSubjects(got), want)
	}
	if got[0].ID != "phase:built" || got[1].ID != "phase:ghost" {
		t.Errorf("ack ids = %q, %q; want phase:built, phase:ghost", got[0].ID, got[1].ID)
	}
	if !strings.Contains(got[1].Detail, "unscaffolded") {
		t.Errorf("ghost detail %q does not say it is unscaffolded", got[1].Detail)
	}
	if !strings.Contains(got[2].Detail, "will not parse") {
		t.Errorf("broken detail %q does not say the record is unreadable", got[2].Detail)
	}
}

// A phases entry that escapes .dross/phases is not done, and nothing outside
// phases/ answers for it — not even a finished-looking record at .dross/x.
func TestCloseoutNotDoneRefusesAnEscapingID(t *testing.T) {
	root := ckRoot(t, "../x")
	outside := filepath.Join(root, "x")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(outside, "changes.json"), `{"phase":"x","status":"complete","tasks":{}}`)

	got := ckKind(ckCollect(t, root).Items, closeoutNotDone)
	if len(got) != 1 || got[0].Subject != "../x" {
		t.Fatalf("not-done = %+v, want ../x listed", got)
	}
	if !strings.Contains(got[0].Detail, "refused") {
		t.Errorf("detail %q does not say the id was refused", got[0].Detail)
	}
}

// Only a missing directory is "unscaffolded"; any other stat failure is named
// as itself. phases/ as a plain file makes stat fail with ENOTDIR.
func TestCloseoutNotDoneNamesOtherStatErrors(t *testing.T) {
	root := ckRoot(t, "p")
	mustWrite(t, filepath.Join(root, "phases"), "not a directory")

	got := ckKind(ckCollect(t, root).Items, closeoutNotDone)
	if len(got) != 1 {
		t.Fatalf("not-done = %+v, want p listed", got)
	}
	if d := got[0].Detail; strings.Contains(d, "unscaffolded") || !strings.Contains(d, "unreadable") {
		t.Errorf("detail %q: want the stat error named, not 'unscaffolded'", d)
	}
}

// c-2: routed items need an absorb record by their own target; dismissed,
// someday and out-of-milestone items never block.
func TestCloseoutDeferredKind(t *testing.T) {
	root := ckRoot(t, "p1", "p2", "ghost")
	srcBody := `[[deferred]]
  id = "d0"
  text = "keep me"
  target = "elsewhere"

[[deferred]]
  id = "d1"
  text = "absorb me"
  target = "p1"

[[deferred]]
  id = "d2"
  text = "dismissed"
  target = "p1"
  dismissed = true

[[deferred]]
  id = "d3"
  text = "someday"

[[deferred]]
  text = "pre-identity"
  target = "p1"

[[deferred]]
  id = "d5"
  text = "for the unscaffolded phase"
  target = "ghost"
`
	srcPath := ckSpec(t, root, "src", srcBody)
	before := sha256OfFile(t, srcPath)
	mustWrite(t, filepath.Join(root, "deferred.toml"), "[phase]\n  id = \"_project\"\n\n[[deferred]]\n  id = \"pj\"\n  text = \"project item\"\n  target = \"p1\"\n")
	ckSpec(t, root, "p1", "[[criteria]]\n  id = \"c-1\"\n  text = \"x\"\n")
	// Absorption by a phase the item is not routed to does not count.
	ckSpec(t, root, "p2", "[[criteria]]\n  id = \"c-1\"\n  text = \"x\"\n  deferred = [\"d1\"]\n")

	got := ckKind(ckCollect(t, root).Items, closeoutDeferred)
	ids := make([]string, 0, len(got))
	for _, it := range got {
		ids = append(ids, it.ID)
	}
	if want := []string{"deferred:src#1", "deferred:src#4", "deferred:src#5", "deferred:_project#0"}; !slices.Equal(ids, want) {
		t.Fatalf("deferred ids = %v, want %v", ids, want)
	}
	if d := got[0].Detail; !strings.Contains(d, "src") || !strings.Contains(d, "[1]") || !strings.Contains(d, "p1") {
		t.Errorf("detail %q does not name source, index and target", d)
	}
	if !strings.Contains(got[0].Clear, "dross deferred absorb src 1 --phase p1") {
		t.Errorf("clear %q does not name the absorb command", got[0].Clear)
	}
	if after := sha256OfFile(t, srcPath); after != before {
		t.Error("collecting stamped the pre-identity item: source spec changed")
	}

	ckSpec(t, root, "p1", "[[criteria]]\n  id = \"c-1\"\n  text = \"x\"\n  deferred = [\"d1\"]\n")
	got = ckKind(ckCollect(t, root).Items, closeoutDeferred)
	for _, it := range got {
		if it.ID == "deferred:src#1" {
			t.Error("an item absorbed by its own target is still listed")
		}
	}
}

func ckTests(t *testing.T, root, id string, at time.Time, scope []string, surviving []mutation.Mutant) {
	t.Helper()
	ckPhase(t, root, id, "")
	run := &verify.Tests{Phase: id, GeneratedAt: at, Scope: &verify.Scope{Files: scope},
		Languages: []verify.LanguageRun{{Name: "go", Tool: "gremlins", Files: scope, Mutation: &mutation.Report{Surviving: surviving}}}}
	tests, _ := verify.FilePaths(root, id)
	if err := run.Save(tests); err != nil {
		t.Fatal(err)
	}
}

// c-2: a survivor routed between two milestone phases is not cleared by an
// absorb record — only by an acceptance or the target's own re-measurement.
func TestCloseoutDeferredRoutedSurvivor(t *testing.T) {
	route := func(t *testing.T, target string) string {
		root := ckRoot(t, "p1", "p2")
		ckSpec(t, root, "p1", "[[deferred]]\n  id = \"r1\"\n  text = \"survivor f.go:12 (op)\"\n  target = \""+target+"\"\n  survivor = \"K\"\n")
		ckPhase(t, root, "p2", "")
		return root
	}
	t0 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	t.Run("listed with survivor commands", func(t *testing.T) {
		root := route(t, "p2")
		got := ckKind(ckCollect(t, root).Items, closeoutDeferred)
		if len(got) != 1 || got[0].ID != "deferred:p1#0" {
			t.Fatalf("deferred = %+v, want the routed survivor deferred:p1#0", got)
		}
		c := got[0].Clear
		if !strings.Contains(c, "dross survivor accept f.go:12 --op op") || !strings.Contains(c, "/dross-verify p2") || strings.Contains(c, "deferred absorb") {
			t.Errorf("clear %q: want survivor accept or re-verify of p2, never deferred absorb", c)
		}
	})
	t.Run("acceptance clears it", func(t *testing.T) {
		root := route(t, "p2")
		ckAccept(t, root, "K")
		if got := ckKind(ckCollect(t, root).Items, closeoutDeferred); len(got) != 0 {
			t.Errorf("accepted survivor still listed: %+v", got)
		}
	})
	t.Run("newer finalized target run over the file clears it", func(t *testing.T) {
		root := route(t, "p2")
		ckTests(t, root, "p1", t0, []string{"f.go"}, nil)
		ckTests(t, root, "p2", t0.Add(time.Hour), []string{"f.go"}, nil)
		ckVerify(t, root, "p2", t0.Add(time.Hour))
		if got := ckKind(ckCollect(t, root).Items, closeoutDeferred); len(got) != 0 {
			t.Errorf("re-measured survivor still listed: %+v", got)
		}
	})
	t.Run("target run that never mutated the file does not clear it", func(t *testing.T) {
		root := route(t, "p2")
		ckTests(t, root, "p1", t0, []string{"f.go"}, nil)
		ckTests(t, root, "p2", t0.Add(time.Hour), []string{"other.go"}, nil)
		ckVerify(t, root, "p2", t0.Add(time.Hour))
		if got := ckKind(ckCollect(t, root).Items, closeoutDeferred); len(got) != 1 {
			t.Errorf("a run that never mutated f.go cleared the survivor: %+v", got)
		}
	})
	t.Run("routed outside the milestone is never listed", func(t *testing.T) {
		root := route(t, "v2-phase")
		if got := ckKind(ckCollect(t, root).Items, closeoutDeferred); len(got) != 0 {
			t.Errorf("survivor routed outside the milestone listed: %+v", got)
		}
	})
}

// c-3: verify.toml survivor records re-classified against today's stores.
func TestCloseoutSurvivorKind(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	rec := verify.SurvivorRecord{Key: "K", File: "f.go", Line: 12, Op: "op"}

	t.Run("listed naming file:line and phase", func(t *testing.T) {
		root := ckRoot(t, "p1", "quiet")
		ckVerify(t, root, "p1", t0, rec)
		ckVerify(t, root, "quiet", t0)
		got := ckKind(ckCollect(t, root).Items, closeoutSurvivor)
		if len(got) != 1 || got[0].ID != "survivor:K" {
			t.Fatalf("survivors = %+v, want one survivor:K", got)
		}
		if !strings.Contains(got[0].Detail, "f.go:12") || !strings.Contains(got[0].Detail, "p1") {
			t.Errorf("detail %q does not name f.go:12 and p1", got[0].Detail)
		}
	})
	t.Run("acceptance clears it", func(t *testing.T) {
		root := ckRoot(t, "p1")
		ckVerify(t, root, "p1", t0, rec)
		ckAccept(t, root, "K")
		if got := ckKind(ckCollect(t, root).Items, closeoutSurvivor); len(got) != 0 {
			t.Errorf("accepted survivor listed: %+v", got)
		}
	})
	t.Run("ambiguous ignores acceptance, clears on route", func(t *testing.T) {
		root := ckRoot(t, "p1")
		amb := rec
		amb.Ambiguous = true
		ckVerify(t, root, "p1", t0, amb)
		ckAccept(t, root, "K")
		if got := ckKind(ckCollect(t, root).Items, closeoutSurvivor); len(got) != 1 {
			t.Fatalf("an acceptance settled an ambiguous survivor: %+v", got)
		}
		ckSpec(t, root, "other", "[[deferred]]\n  text = \"survivor f.go:12 (op)\"\n  target = \"later\"\n  survivor = \"K\"\n")
		if got := ckKind(ckCollect(t, root).Items, closeoutSurvivor); len(got) != 0 {
			t.Errorf("a route did not clear the ambiguous survivor: %+v", got)
		}
	})
	t.Run("dismissed or target-less route does not clear it", func(t *testing.T) {
		root := ckRoot(t, "p1")
		ckVerify(t, root, "p1", t0, rec)
		ckSpec(t, root, "other", "[[deferred]]\n  text = \"a\"\n  target = \"later\"\n  survivor = \"K\"\n  dismissed = true\n\n[[deferred]]\n  text = \"b\"\n  survivor = \"K\"\n")
		if got := ckKind(ckCollect(t, root).Items, closeoutSurvivor); len(got) != 1 {
			t.Errorf("a dismissed or target-less route cleared the survivor: %+v", got)
		}
	})
	t.Run("one item across phases, keyless by file:line:op", func(t *testing.T) {
		root := ckRoot(t, "p1", "p2")
		ckVerify(t, root, "p1", t0, rec, verify.SurvivorRecord{File: "g.go", Line: 7, Op: "neg"})
		ckVerify(t, root, "p2", t0, rec)
		got := ckKind(ckCollect(t, root).Items, closeoutSurvivor)
		if len(got) != 2 {
			t.Fatalf("survivors = %+v, want survivor:K once and the keyless one", got)
		}
		if got[0].ID != "survivor:K" || !strings.Contains(got[0].Detail, "p1, p2") {
			t.Errorf("shared survivor = %+v, want survivor:K naming p1, p2", got[0])
		}
		if got[1].ID != "survivor:g.go:7:neg" {
			t.Errorf("keyless id = %q, want survivor:g.go:7:neg", got[1].ID)
		}
		if _, err := milestone.ParseAckItem(got[1].ID); err != nil {
			t.Errorf("keyless id is not ackable: %v", err)
		}
	})
}

// Every item names a command that can actually clear it.
func TestCloseoutClearingCommands(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	root := ckRoot(t, "p1", "unv")
	ckVerify(t, root, "p1", t0,
		verify.SurvivorRecord{Key: "K", File: "f.go", Line: 12, Op: "op"},
		verify.SurvivorRecord{Key: "A", File: "a.go", Line: 3, Op: "op", Ambiguous: true},
		verify.SurvivorRecord{File: "g.go", Line: 7, Op: "neg"})
	ckPhase(t, root, "unv", "complete")
	r := ckCollect(t, root)

	gotIDs := []string{}
	for _, it := range ckKind(r.Items, closeoutSurvivor) {
		gotIDs = append(gotIDs, it.ID)
	}
	slices.Sort(gotIDs)
	if want := []string{"survivor:A", "survivor:K", "survivor:g.go:7:neg"}; !slices.Equal(gotIDs, want) {
		t.Fatalf("survivor items = %v, want %v", gotIDs, want)
	}
	for _, it := range ckKind(r.Items, closeoutSurvivor) {
		switch it.ID {
		case "survivor:K":
			if !strings.Contains(it.Clear, "dross survivor accept") || !strings.Contains(it.Clear, "dross survivor route") {
				t.Errorf("keyed survivor clear %q lacks accept|route", it.Clear)
			}
		case "survivor:A", "survivor:g.go:7:neg":
			if strings.Contains(it.Clear, "dross survivor accept") {
				t.Errorf("%s clear %q names accept, which cannot clear it", it.ID, it.Clear)
			}
			if !strings.Contains(it.Clear, "dross survivor route") && !strings.Contains(it.Clear, "dross milestone ack") {
				t.Errorf("%s clear %q names neither route nor ack", it.ID, it.Clear)
			}
		default:
			t.Errorf("unexpected survivor %s", it.ID)
		}
	}
	unv := ckKind(r.Items, closeoutUnverified)
	if len(unv) != 1 || unv[0].Subject != "unv" {
		t.Fatalf("unverified = %+v, want unv", unv)
	}
	if unv[0].ID != "" {
		t.Errorf("unverified item carries ack id %q", unv[0].ID)
	}
	if c := unv[0].Clear; !strings.Contains(c, "/dross-verify") || !strings.Contains(c, "--force-unverified") || strings.Contains(c, "dross milestone ack") {
		t.Errorf("unverified clear %q: want /dross-verify or --force-unverified, never ack", c)
	}
}

// c-4: one failing collector never hides another's items.
func TestRunCloseoutCollectorsKeepsGoing(t *testing.T) {
	r := runCloseoutCollectors([]closeoutCollector{
		{closeoutDeferred, func() ([]closeoutItem, error) { return nil, errors.New("spec broke") }},
		{closeoutSurvivor, func() ([]closeoutItem, error) {
			return []closeoutItem{{Kind: closeoutSurvivor, ID: "survivor:K"}}, nil
		}},
	})
	if len(r.Errors) != 1 || r.Errors[0].Kind != closeoutDeferred {
		t.Errorf("errors = %+v, want the deferred collector's", r.Errors)
	}
	if len(r.Items) != 1 || r.Items[0].ID != "survivor:K" {
		t.Errorf("items = %+v, want the survivor collector's item", r.Items)
	}
}

// c-4: an unreadable source is a blocking error line under its kind, not an
// empty kind.
// Each source is broken in its own root, so one kind's error cannot stand in
// for the other's.
func TestCloseoutCollectorErrorsAreBlockingLines(t *testing.T) {
	errKinds := func(r closeoutReport) map[string]bool {
		kinds := map[string]bool{}
		for _, e := range r.Errors {
			kinds[e.Kind] = true
		}
		return kinds
	}

	root := ckRoot(t, "p1")
	ckPhase(t, root, "p1", "complete")
	ckSpec(t, root, "src", "[[deferred]\n  broken = ***\n")
	if kinds := errKinds(ckCollect(t, root)); !kinds[closeoutDeferred] {
		t.Errorf("malformed spec.toml: error kinds = %v, want deferred", kinds)
	}

	root = ckRoot(t, "p1")
	ckPhase(t, root, "p1", "complete")
	mustWrite(t, survivor.Path(root), "[[accepted]\n")
	kinds := errKinds(ckCollect(t, root))
	if !kinds[closeoutSurvivor] {
		t.Errorf("malformed survivors.toml: error kinds = %v, want survivor", kinds)
	}
	if kinds[closeoutDeferred] {
		t.Errorf("malformed survivors.toml leaked into the deferred kind: %v", kinds)
	}
}

// c-8: no record and an empty phases array have nothing to close out.
func TestCloseoutEmptyMilestone(t *testing.T) {
	r, err := milestoneCloseout(filepath.Join(t.TempDir(), ".dross"), "v9.9")
	if err != nil || len(r.Items) != 0 || len(r.Errors) != 0 {
		t.Errorf("no record: %+v, %v; want empty", r, err)
	}
	r = ckCollect(t, ckRoot(t))
	if len(r.Items) != 0 || len(r.Errors) != 0 {
		t.Errorf("empty phases: %+v; want empty", r)
	}
}

func sha256OfFile(t *testing.T, path string) [32]byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(b)
}

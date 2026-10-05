package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/deferred"
	"github.com/Rivil/dross/internal/phase"
)

// rerouteFixture is setupSurvivorFixture (alpha current, beta, internal/x.go)
// plus empty phases gamma and delta to route to.
func rerouteFixture(t *testing.T) string {
	t.Helper()
	dir := setupSurvivorFixture(t)
	for _, id := range []string{"gamma", "delta"} {
		mustWrite(t, filepath.Join(dir, ".dross", "phases", id, "spec.toml"),
			"[phase]\nid = \""+id+"\"\ntitle = \""+id+"\"\n\n[[criteria]]\nid = \"c-1\"\ntext = \"x\"\n")
	}
	return dir
}

func routeX(t *testing.T, target string) string {
	t.Helper()
	var err error
	out := captureStdout(t, func() {
		err = runCmd(t, Survivor(), "route", "internal/x.go:4", "--op", "CONDITIONALS_BOUNDARY", "--target", target)
	})
	if err != nil {
		t.Fatalf("route → %s: %v", target, err)
	}
	return out
}

// survivorEntries is every live entry carrying the one survivor x.go:4 routes.
func survivorEntries(t *testing.T, dir string) []deferred.Entry {
	t.Helper()
	all, err := deferred.Collect(filepath.Join(dir, ".dross"))
	if err != nil {
		t.Fatal(err)
	}
	return deferred.Filter(all, func(e deferred.Entry) bool { return e.Survivor != "" && !e.Dismissed })
}

func setCurrent(t *testing.T, id string) {
	t.Helper()
	if err := runCmd(t, State(), "set", "current_phase", id); err != nil {
		t.Fatal(err)
	}
}

// TestSurvivorRerouteMovesTheEntry: a survivor routed from alpha, routed again
// while beta is current, moves alpha's entry — same id — and beta's spec is
// never touched.
func TestSurvivorRerouteMovesTheEntry(t *testing.T) {
	dir := rerouteFixture(t)
	routeX(t, "gamma")
	first := survivorEntries(t, dir)
	if len(first) != 1 || first[0].Source != "alpha" || first[0].ID == "" {
		t.Fatalf("first route = %+v, want one entry in alpha with a minted id", first)
	}

	setCurrent(t, "beta")
	betaPath := filepath.Join(dir, ".dross", "phases", "beta", "spec.toml")
	beta := mustRead(t, betaPath)
	routeX(t, "delta")

	got := survivorEntries(t, dir)
	if len(got) != 1 {
		t.Fatalf("%d entries carry the survivor after a re-route, want 1: %+v", len(got), got)
	}
	if got[0].Source != "alpha" || got[0].ID != first[0].ID || got[0].Target != "delta" {
		t.Errorf("re-routed entry = %+v, want alpha's entry, id %s, target delta", got[0], first[0].ID)
	}
	if mustRead(t, betaPath) != beta {
		t.Error("re-routing changed the current phase's spec")
	}
}

// TestSurvivorRouteTwiceKeepsOneEntry: routing K→gamma then K→delta from the
// same phase leaves one entry, its first id kept, targeting delta.
func TestSurvivorRouteTwiceKeepsOneEntry(t *testing.T) {
	dir := rerouteFixture(t)
	routeX(t, "gamma")
	id := survivorEntries(t, dir)[0].ID
	out := routeX(t, "delta")
	got := survivorEntries(t, dir)
	if len(got) != 1 || got[0].ID != id || got[0].Target != "delta" {
		t.Errorf("entries = %+v, want one with id %s targeting delta", got, id)
	}
	if !strings.Contains(out, "re-routed") {
		t.Errorf("stdout = %q, want it to say the entry was re-routed", out)
	}
}

// TestSurvivorRouteSameTargetIsNoop: routing to where it already goes writes
// nothing and says so.
func TestSurvivorRouteSameTargetIsNoop(t *testing.T) {
	dir := rerouteFixture(t)
	routeX(t, "gamma")
	alphaPath := filepath.Join(dir, ".dross", "phases", "alpha", "spec.toml")
	past := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(alphaPath, past, past); err != nil {
		t.Fatal(err)
	}
	before := drossTreeHash(t, dir)
	out := routeX(t, "gamma")
	if drossTreeHash(t, dir) != before {
		t.Error("routing to the same target changed a file")
	}
	if st, _ := os.Stat(alphaPath); !st.ModTime().Equal(past) {
		t.Error("routing to the same target rewrote alpha's spec")
	}
	if !strings.Contains(out, "already routed") {
		t.Errorf("stdout = %q, want it to say the survivor is already routed", out)
	}
}

// TestSurvivorRouteRefusesDuplicates: two live entries already carrying the
// key is a duplicate to clear, named by both handles — nothing is written.
func TestSurvivorRouteRefusesDuplicates(t *testing.T) {
	dir := rerouteFixture(t)
	routeX(t, "gamma")
	key := survivorEntries(t, dir)[0].Survivor
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "beta", "spec.toml"),
		"[phase]\nid = \"beta\"\ntitle = \"beta\"\n\n[[criteria]]\nid = \"c-1\"\ntext = \"x\"\n\n[[deferred]]\nid = \"dup\"\ntext = \"copy\"\ntarget = \"gamma\"\nsurvivor = \""+key+"\"\n")
	before := drossTreeHash(t, dir)

	err := runCmd(t, Survivor(), "route", "internal/x.go:4", "--op", "CONDITIONALS_BOUNDARY", "--target", "delta")
	if err == nil || !strings.Contains(err.Error(), "alpha 0") || !strings.Contains(err.Error(), "beta 0") {
		t.Fatalf("err = %v, want a refusal naming alpha 0 and beta 0", err)
	}
	if drossTreeHash(t, dir) != before {
		t.Error("a refused route changed a file")
	}
}

// TestSurvivorRerouteToCompleteRefusedFirst: the c-8 refusal comes before the
// lookup, and the entry keeps its old target.
func TestSurvivorRerouteToCompleteRefusedFirst(t *testing.T) {
	dir := rerouteFixture(t)
	routeX(t, "gamma")
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "delta", "changes.json"), `{"phase":"delta","status":"complete","tasks":{}}`)
	err := runCmd(t, Survivor(), "route", "internal/x.go:4", "--op", "CONDITIONALS_BOUNDARY", "--target", "delta")
	if err == nil || !strings.Contains(err.Error(), "already complete") {
		t.Fatalf("err = %v, want the complete-target refusal", err)
	}
	if got := survivorEntries(t, dir); len(got) != 1 || got[0].Target != "gamma" {
		t.Errorf("entries = %+v, want the entry still targeting gamma", got)
	}
}

// TestSurvivorRerouteAfterAbsorbingPhaseCompletes: gamma absorbed the survivor
// and completed, yet its verify still lists it; routing it on to delta moves
// the entry (id kept), and gamma's finished spec stays valid.
func TestSurvivorRerouteAfterAbsorbingPhaseCompletes(t *testing.T) {
	dir := rerouteFixture(t)
	routeX(t, "gamma")
	id := survivorEntries(t, dir)[0].ID
	gammaPath := filepath.Join(dir, ".dross", "phases", "gamma", "spec.toml")
	mustWrite(t, gammaPath, "[phase]\nid = \"gamma\"\ntitle = \"gamma\"\n\n[[criteria]]\nid = \"c-1\"\ntext = \"x\"\ndeferred = [\""+id+"\"]\n")
	if err := runCmd(t, Validate()); err != nil {
		t.Fatalf("premise: validate with gamma absorbing: %v", err)
	}
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "gamma", "changes.json"), `{"phase":"gamma","status":"complete","tasks":{}}`)

	routeX(t, "delta")
	got := survivorEntries(t, dir)
	if len(got) != 1 || got[0].ID != id || got[0].Target != "delta" {
		t.Fatalf("entries = %+v, want one with id %s targeting delta", got, id)
	}
	spec, err := phase.LoadSpec(gammaPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Criteria[0].Deferred) != 1 {
		t.Fatal("premise: gamma's criterion lost its absorbed id")
	}
	if err := runCmd(t, Validate()); err != nil {
		t.Fatalf("validate refused gamma's finished spec after the re-route: %v", err)
	}
}

// TestSurvivorRerouteKeepsOneBoardCard: backlog sync after a re-route creates
// no second card — the same dross/deferred:<id> card swaps its target label.
func TestSurvivorRerouteKeepsOneBoardCard(t *testing.T) {
	f := newFakeBoard(t)
	dir := routedRepo(t, f.srv.URL, "")
	for _, id := range []string{"t1", "t2"} {
		writeSpec(t, dir, id, "[phase]\nid = \""+id+"\"\ntitle = \""+id+"\"\n\n[[criteria]]\nid = \"c-1\"\ntext = \"x\"\n")
	}
	mustWrite(t, filepath.Join(dir, "internal", "x.go"), "package x\n\nfunc f(limit int) error {\n\tif limit > 0 {\n\t\treturn nil\n\t}\n\treturn nil\n}\n")
	setCurrent(t, "host")

	routeX(t, "t1")
	if err := runCmd(t, Issue(), "backlog", "sync", "v0.1"); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	key := issueCarrying(t, f, "survivor internal/x.go:4")
	creates := len(f.creates)

	routeX(t, "t2")
	if err := runCmd(t, Issue(), "backlog", "sync", "v0.1"); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if len(f.creates) != creates {
		t.Errorf("the re-route created %d new card(s)", len(f.creates)-creates)
	}
	got := tagsOn(f, key)
	if !slicesHas(got, "dross/target:t2") || slicesHas(got, "dross/target:t1") {
		t.Errorf("tags on %s = %v, want dross/target:t2 and not t1", key, got)
	}
}

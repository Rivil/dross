package cmd

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// completeTargetPhases adds the three destinations every route test judges:
// done (complete), open (shipped, not complete) and a v1 milestone slug,
// future, that nobody has scaffolded yet.
func completeTargetPhases(t *testing.T, dir string) {
	t.Helper()
	phases := filepath.Join(dir, ".dross", "phases")
	for id, status := range map[string]string{"done": "complete", "open": "shipped"} {
		mustWrite(t, filepath.Join(phases, id, "spec.toml"),
			"[phase]\nid = \""+id+"\"\ntitle = \""+id+"\"\n\n[[criteria]]\nid = \"c-1\"\ntext = \"x\"\n")
		mustWrite(t, filepath.Join(phases, id, "changes.json"),
			`{"phase":"`+id+`","status":"`+status+`","tasks":{}}`)
	}
	mustWrite(t, filepath.Join(dir, ".dross", "milestones", "v1.toml"),
		"phases = [\"future\"]\n\n[milestone]\n  version = \"v1\"\n  title = \"X\"\n")
}

// routeFixture is setupSurvivorFixture (alpha current, beta, internal/x.go)
// plus the three destinations, and one parked item in alpha to route.
func routeFixture(t *testing.T) string {
	t.Helper()
	dir := setupSurvivorFixture(t)
	completeTargetPhases(t, dir)
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "alpha", "spec.toml"),
		"[phase]\nid = \"alpha\"\ntitle = \"alpha\"\n\n[[criteria]]\nid = \"c-1\"\ntext = \"x\"\n\n[[deferred]]\ntext = \"an idea\"\n")
	return dir
}

// drossTreeHash fingerprints every file under .dross — path and bytes — so a
// refused command can be shown to have written nothing anywhere.
func drossTreeHash(t *testing.T, dir string) string {
	t.Helper()
	root := filepath.Join(dir, ".dross")
	var names []string
	if err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			names = append(names, p)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		b, err := os.ReadFile(n)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(h, "%s\x00%d\x00", n, len(b))
		h.Write(b)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func wantCompleteRefusal(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("routing to a complete phase succeeded")
	}
	if !strings.Contains(err.Error(), `"done"`) || !strings.Contains(err.Error(), "complete") {
		t.Errorf("err = %v, want it to name done and say it is complete", err)
	}
}

// TestDeferredRouteRefusesCompleteTarget: a finished phase takes no new scope,
// and the refusal leaves every file under .dross untouched.
func TestDeferredRouteRefusesCompleteTarget(t *testing.T) {
	dir := routeFixture(t)
	before := drossTreeHash(t, dir)
	wantCompleteRefusal(t, runCmd(t, Deferred(), "route", "alpha", "0", "--target", "done"))
	if after := drossTreeHash(t, dir); after != before {
		t.Error("a refused route changed the .dross tree")
	}
}

// TestSurvivorRouteRefusesCompleteTargetBeforeResolve: the refusal comes before
// the survivor is resolved — a location that would not even resolve still gets
// the complete-phase error, and neither the spec nor the store is touched.
func TestSurvivorRouteRefusesCompleteTargetBeforeResolve(t *testing.T) {
	dir := routeFixture(t)
	specPath := filepath.Join(dir, ".dross", "phases", "alpha", "spec.toml")
	spec := mustRead(t, specPath)
	store, storeErr := os.ReadFile(storeFileOf(dir))

	wantCompleteRefusal(t, runCmd(t, Survivor(), "route", "internal/missing.go:12", "--op", "X", "--target", "done"))

	if got := mustRead(t, specPath); got != spec {
		t.Errorf("a refused route changed alpha's spec:\n%s", got)
	}
	gotStore, gotErr := os.ReadFile(storeFileOf(dir))
	if (storeErr == nil) != (gotErr == nil) || string(store) != string(gotStore) {
		t.Error("a refused route changed survivors.toml")
	}
}

// completeBoardRepo is a board-enabled repo with the three destinations, alpha as
// the current phase and v1 as the current milestone — so a successful
// `deferred add` mirrors to the board and a refused one demonstrably does not.
func completeBoardRepo(t *testing.T, apiBase string) string {
	t.Helper()
	dir := youtrackBoardRepo(t, apiBase)
	completeTargetPhases(t, dir)
	writeSpec(t, dir, "alpha", "[phase]\nid = \"alpha\"\ntitle = \"Alpha\"\n\n[[criteria]]\nid = \"c-1\"\ntext = \"x\"\n")
	for _, kv := range [][2]string{{"current_phase", "alpha"}, {"current_milestone", "v1"}} {
		if err := runCmd(t, State(), "set", kv[0], kv[1]); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestDeferredAddRefusesCompleteTarget: add --target is the third stamping
// path, refused before any local write and before the board mirror — the
// server fails the test on any request.
func TestDeferredAddRefusesCompleteTarget(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("a refused add must make no board call, got %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)

	dir := completeBoardRepo(t, srv.URL)
	specPath := filepath.Join(dir, ".dross", "phases", "alpha", "spec.toml")
	spec := mustRead(t, specPath)

	wantCompleteRefusal(t, runCmd(t, Deferred(), "add", "late idea", "--target", "done"))

	if got := mustRead(t, specPath); got != spec {
		t.Errorf("a refused add changed alpha's spec:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(dir, ".dross", "deferred.toml")); !os.IsNotExist(err) {
		t.Errorf("a refused add created deferred.toml (stat err = %v)", err)
	}
}

// TestDeferredAddBoardFixtureIsLive keeps the zero-request assertion above
// honest: on the same fixture, an add that is NOT refused does reach the
// board, so silence there means the refusal came first.
func TestDeferredAddBoardFixtureIsLive(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	completeBoardRepo(t, srv.URL)
	_ = runCmd(t, Deferred(), "add", "an idea", "--target", "open")
	if calls == 0 {
		t.Fatal("an accepted add made no board call — the refusal test's zero-request assertion would be vacuous")
	}
}

// TestRouteAcceptsOpenTargets is the control: a shipped-but-not-complete phase
// and an unscaffolded roadmap slug both still take routes, on all three paths.
func TestRouteAcceptsOpenTargets(t *testing.T) {
	for _, target := range []string{"open", "future"} {
		t.Run(target, func(t *testing.T) {
			routeFixture(t)
			if err := runCmd(t, Deferred(), "route", "alpha", "0", "--target", target); err != nil {
				t.Errorf("deferred route → %s: %v", target, err)
			}
			if err := runCmd(t, Survivor(), "route", "internal/x.go:4", "--op", "CONDITIONALS_BOUNDARY", "--target", target); err != nil {
				t.Errorf("survivor route → %s: %v", target, err)
			}
			if err := runCmd(t, Deferred(), "add", "another idea", "--target", target); err != nil {
				t.Errorf("deferred add → %s: %v", target, err)
			}
		})
	}
}

// TestValidateKeepsRoutesToCompletedPhases: the refusal is for NEW routes only.
// An item routed before its target completed is history validate accepts.
func TestValidateKeepsRoutesToCompletedPhases(t *testing.T) {
	dir := routeFixture(t)
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "alpha", "spec.toml"),
		"[phase]\nid = \"alpha\"\ntitle = \"alpha\"\n\n[[criteria]]\nid = \"c-1\"\ntext = \"x\"\n\n[[deferred]]\ntext = \"routed before it shipped\"\ntarget = \"done\"\n")
	if err := runCmd(t, Validate()); err != nil {
		t.Fatalf("validate refused an existing route to a complete phase: %v", err)
	}
}

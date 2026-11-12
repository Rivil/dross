package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/gatestate"
	"github.com/Rivil/dross/internal/treefp"
)

const shipVouchingLane = `
[[runtime.test_lane]]
  name = "web"
  command = "x"
  vouch = true
`

// vouchShip gives a stamped ship fixture a vouching lane, committed.
func vouchShip(t *testing.T, dir string) {
	t.Helper()
	f := filepath.Join(dir, ".dross", "project.toml")
	b, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, f, string(b)+shipVouchingLane)
	gitCommit(t, dir, "chore(dross): declare a vouching lane")
}

// saveHeadGreen records a full green for HEAD's tree.
func saveHeadGreen(t *testing.T, dir string) {
	t.Helper()
	tree, err := treefp.Base(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := gatestate.SaveGreen(dir, gatestate.Green{Tree: tree, At: time.Now().UTC(), Runner: "local"}); err != nil {
		t.Fatal(err)
	}
}

// saveHeadScoped records a scoped green for HEAD's tree.
func saveHeadScoped(t *testing.T, dir string) {
	t.Helper()
	tree, err := treefp.Base(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := gatestate.Scoped{Tree: tree, Covered: []string{"src/tag.ts"}, Lanes: []string{"web"}, At: time.Now().UTC(), Runner: "local"}
	if err := gatestate.SaveScoped(dir, s); err != nil {
		t.Fatal(err)
	}
}

// TestShipRefusesAScopedGreen: a scoped green on HEAD never stands in for a full
// one, and the refusal comes before anything is written or pushed, however ship
// was asked to stop.
func TestShipRefusesAScopedGreen(t *testing.T) {
	dir := stampedShipFixture(t)
	cap, remoteDir := shipMockFlowRemote(t, dir)
	vouchShip(t, dir)
	stampVerdict(t, dir)
	saveHeadScoped(t, dir)
	mustWrite(t, filepath.Join(dir, ".dross/phases/x/notes.md"), "bookkeeping\n")
	head := gitOutT(t, dir, "rev-parse", "HEAD")

	for _, args := range [][]string{{"x"}, {"--print-body", "x"}, {"--no-push", "x"}} {
		var err error
		stdout := captureStdout(t, func() { err = shipErr(t, args...) })
		if err == nil {
			t.Fatalf("ship %v accepted a scoped green with no full green", args)
		}
		if !strings.Contains(err.Error(), "bare `dross test`") {
			t.Errorf("ship %v refusal does not name a bare `dross test`:\n%v", args, err)
		}
		if strings.TrimSpace(stdout) != "" {
			t.Errorf("ship %v printed before refusing:\n%s", args, stdout)
		}
	}
	if got := gitOutT(t, dir, "rev-parse", "HEAD"); got != head {
		t.Error("the refusal committed something")
	}
	if !strings.Contains(mustGit(t, dir, "status", "--porcelain"), "notes.md") {
		t.Error("the refusal auto-committed the .dross bookkeeping")
	}
	if out, _ := gitOut(remoteDir, "show-ref", "--verify", "refs/heads/phase/x"); strings.TrimSpace(out) != "" {
		t.Errorf("phase/x reached origin on a refusal: %s", out)
	}
	if cap.posts != 0 {
		t.Errorf("POST /pulls ran %d time(s) on a refusal", cap.posts)
	}
}

// TestShipFullGreenBeatsFinalize: an unfinalized verdict with no full green is
// refused before auto-finalize writes verify.toml.
func TestShipFullGreenBeatsFinalize(t *testing.T) {
	dir := stampedShipFixture(t)
	vouchShip(t, dir)
	f := filepath.Join(dir, ".dross", "phases", "x", "verify.toml")
	before, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(before), "finalized = true") {
		t.Fatal("fixture verdict is already finalized, so this test proves nothing")
	}
	if err := shipErr(t, "--no-push", "x"); err == nil || !strings.Contains(err.Error(), "bare `dross test`") {
		t.Fatalf("unfinalized verdict, no full green = %v, want the full-green refusal", err)
	}
	after, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("verify.toml changed on a refusal:\n%s", after)
	}
}

// TestShipOffBranchRefusalBeatsFullGreen: off the phase branch HEAD is another
// branch's tree, so the refusal is today's branch text.
func TestShipOffBranchRefusalBeatsFullGreen(t *testing.T) {
	dir := stampedShipFixture(t)
	vouchShip(t, dir)
	mustGit(t, dir, "checkout", "-q", "-b", "elsewhere")

	err := shipErr(t, "--no-push", "x")
	if err == nil || !strings.Contains(err.Error(), "must be on phase/x") || strings.Contains(err.Error(), "full green") {
		t.Errorf("off-branch = %v, want the must-be-on-phase-branch refusal and no full-green text", err)
	}
}

// TestShipForceUnverifiedPassesFullGreen: the override warns on stderr and
// proceeds, and `ship --json` keeps stdout to one object.
func TestShipForceUnverifiedPassesFullGreen(t *testing.T) {
	dir := stampedShipFixture(t)
	vouchShip(t, dir)
	stampVerdict(t, dir)
	shipMockFlow(t, dir)

	var stdout string
	stderr := captureStderr(t, func() {
		stdout = captureStdout(t, func() {
			if err := shipErr(t, "--json", "--force-unverified", "x"); err != nil {
				t.Fatalf("ship --json --force-unverified with no full green: %v", err)
			}
		})
	})
	if !strings.Contains(stderr, "no full green") || !strings.Contains(stderr, "--force-unverified") {
		t.Errorf("the override went silent:\n%s", stderr)
	}
	var obj map[string]any
	dec := json.NewDecoder(strings.NewReader(stdout))
	if err := dec.Decode(&obj); err != nil {
		t.Fatalf("stdout is not a JSON object: %v\n%s", err, stdout)
	}
	if dec.More() {
		t.Errorf("stdout carries more than one JSON value:\n%s", stdout)
	}
}

// TestShipPassesOnAFullGreen: a full green on HEAD's tree proceeds past the check.
func TestShipPassesOnAFullGreen(t *testing.T) {
	dir := stampedShipFixture(t)
	vouchShip(t, dir)
	stampVerdict(t, dir)
	saveHeadGreen(t, dir)
	if err := shipErr(t, "--no-push", "x"); err != nil {
		t.Errorf("a full green on HEAD was refused: %v", err)
	}
}

// TestShipNoVouchingLaneNeedsNoGreen: every other repo ships as it did, with no
// green.json at all.
func TestShipNoVouchingLaneNeedsNoGreen(t *testing.T) {
	dir := stampedShipFixture(t)
	if g, err := gatestate.LoadGreen(dir); err != nil || g != nil {
		t.Fatalf("fixture green = %+v, %v; want none", g, err)
	}
	if err := shipErr(t, "--no-push", "x"); err != nil {
		t.Errorf("a repo with no vouching lane was refused: %v", err)
	}
}

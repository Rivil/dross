package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// twoLanes is the shape every assertion here needs: one lane whose grant is
// being exercised and a second one that must be left alone. A single-lane
// fixture cannot tell "granted this lane" from "granted everything".
const twoLanes = `[[runtime.test_lane]]
name = "go"
match = ["internal/**"]
command = "go test -count=1 ./..."

[[runtime.test_lane]]
name = "docs"
match = ["docs/"]
command = "markdownlint docs"`

// laneGrantFixture is laneFixture plus the two lanes, returning the .dross root
// and the repo dir the consent readers take.
func laneGrantFixture(t *testing.T) (root, repoDir string) {
	t.Helper()
	dir := laneFixture(t)
	appendLanes(t, dir, twoLanes)
	return filepath.Join(dir, RootDirName), dir
}

func mustGrantLane(t *testing.T, root, name, line string) {
	t.Helper()
	if err := GrantLaneConsent(root, name, line); err != nil {
		t.Fatalf("grant %s: %v", name, err)
	}
}

func laneState(t *testing.T, root, repoDir, name, line string) ConsentState {
	t.Helper()
	state, _ := LaneConsented(root, repoDir, name, line)
	return state
}

// TestOneLaneGoesStaleAlone is the locked lane_consent decision, stated as a
// test: an aggregate hash over every lane's command would make a one-character
// edit to the docs lane revoke the Go lane too. That is not a smaller
// inconvenience than it sounds — the Go lane is the pre-commit gate, so a docs
// typo would block committing until someone re-consented to a suite they never
// touched, and a gate that behaves like that gets routed around.
func TestOneLaneGoesStaleAlone(t *testing.T) {
	root, repoDir := laneGrantFixture(t)
	mustGrantLane(t, root, "go", "go test -count=1 ./...")
	mustGrantLane(t, root, "docs", "markdownlint docs")

	// The docs lane's command is edited; the Go lane's is not.
	if got := laneState(t, root, repoDir, "docs", "markdownlint --fix docs"); got != ConsentStale {
		t.Errorf("edited lane state = %v, want stale", got)
	}
	if got := laneState(t, root, repoDir, "go", "go test -count=1 ./..."); got != ConsentGranted {
		t.Errorf("untouched lane state = %v, want granted — one lane's edit revoked another's grant", got)
	}
}

// TestTrustLaneAccumulates: the store is a map that grows. A grant that
// replaced it would make trusting the docs lane revoke the Go lane, so the user
// would re-consent to everything each time they touched anything.
func TestTrustLaneAccumulates(t *testing.T) {
	root, repoDir := laneGrantFixture(t)
	mustGrantLane(t, root, "go", "go test -count=1 ./...")
	mustGrantLane(t, root, "docs", "markdownlint docs")

	for _, lane := range []struct{ name, line string }{
		{"go", "go test -count=1 ./..."},
		{"docs", "markdownlint docs"},
	} {
		if got := laneState(t, root, repoDir, lane.name, lane.line); got != ConsentGranted {
			t.Errorf("lane %s = %v after both grants, want granted", lane.name, got)
		}
	}
}

// TestLaneGrantKeyIsNotSettable: the whole ceremony of `dross trust --lane` is
// that it prints the command before writing the fingerprint. A generic
// key-writer that could write the same value would let an agent grant consent
// on the user's behalf without ever showing them what for — which is precisely
// what the gate exists to stop, so the key stays out of localKeys.
func TestLaneGrantKeyIsNotSettable(t *testing.T) {
	laneGrantFixture(t)

	err := runCmd(t, Local(), "set", "trusted_lane_commands", Fingerprint("anything"))
	if err == nil {
		t.Fatal("`dross local set trusted_lane_commands` was accepted — the generic writer can grant lane consent")
	}
	if !strings.Contains(err.Error(), "unknown local key") {
		t.Errorf("want an unknown-key refusal, got: %v", err)
	}
}

// TestTrustLanePrintsTheCommandBeforeWriting pins the ORDER, not just the
// presence of the line. A grant that wrote first and printed after would still
// show the right text while having already authorized it — the print is the
// consent moment, so it has to come first.
//
// The proof is a store that cannot be written: if the print survives a failed
// write, it happened before it.
func TestTrustLanePrintsTheCommandBeforeWriting(t *testing.T) {
	root, _ := laneGrantFixture(t)

	// An unwritable local.toml makes the store write fail.
	store := filepath.Join(root, LocalFile)
	if err := os.WriteFile(store, []byte(""), 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(store, 0o644) })

	var out string
	err := runCmdCapturing(t, &out, Trust(), "--lane", "go")
	if err == nil {
		t.Skip("the store was writable despite mode 0444 — this filesystem cannot express the ordering proof")
	}
	if !strings.Contains(out, "go test -count=1 ./...") {
		t.Errorf("the command was not printed before the store write:\n%s", out)
	}
}

// TestTrustLaneLeavesTheOtherGrantsAlone: the three grant kinds in local.toml
// are independent, and a lane grant that disturbed the whole-suite or run
// grants would silently revoke consent the user never revisited.
func TestTrustLaneLeavesTheOtherGrantsAlone(t *testing.T) {
	root, _ := laneGrantFixture(t)
	if err := GrantConsent(root, "go test ./..."); err != nil {
		t.Fatal(err)
	}
	if err := GrantRunConsent(root, "make dev"); err != nil {
		t.Fatal(err)
	}
	before, err := loadLocal(localPath(root))
	if err != nil {
		t.Fatal(err)
	}

	if err := runCmd(t, Trust(), "--lane", "go"); err != nil {
		t.Fatalf("trust --lane: %v", err)
	}

	after, err := loadLocal(localPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if after.TrustedTestCommand != before.TrustedTestCommand {
		t.Error("the lane grant rewrote trusted_test_command")
	}
	if after.TrustedRunCommands != before.TrustedRunCommands {
		t.Error("the lane grant rewrote trusted_run_commands")
	}
	if after.TrustedLaneCommands["go"] != Fingerprint("go test -count=1 ./...") {
		t.Errorf("the lane's own fingerprint was not recorded: %v", after.TrustedLaneCommands)
	}
}

// TestTrustLaneUnknownNamesTheDeclaredLanes: an unknown lane must not write a
// fingerprint of the empty string, and the refusal has to say what the repo
// does declare — otherwise a typo sends the user to open project.toml to find
// out what they should have typed.
func TestTrustLaneUnknownNamesTheDeclaredLanes(t *testing.T) {
	root, _ := laneGrantFixture(t)

	err := runCmd(t, Trust(), "--lane", "nosuch")
	if err == nil {
		t.Fatal("trust --lane nosuch was accepted")
	}
	for _, want := range []string{"nosuch", "go", "docs"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not mention %q: %v", want, err)
		}
	}
	l, lerr := loadLocal(localPath(root))
	if lerr != nil {
		t.Fatal(lerr)
	}
	if len(l.TrustedLaneCommands) != 0 {
		t.Errorf("an unknown lane wrote a grant: %v", l.TrustedLaneCommands)
	}
}

// assertNoLaneGrant fails if a refused `dross trust --lane` left a grant
// behind. A refusal that still writes is worse than one that does not refuse
// at all: the fingerprint would be taken over the empty string, so the entry
// authorizes whatever a lane of that name is later given.
func assertNoLaneGrant(t *testing.T, root string) {
	t.Helper()
	l, err := loadLocal(localPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(l.TrustedLaneCommands) != 0 {
		t.Errorf("a refused lane wrote a grant: %v", l.TrustedLaneCommands)
	}
}

// TestTrustLaneOnALaneLessRepoSaysSo is the other half of the refusal above.
// A repo that declares no lanes has no alternatives to list, so falling
// through to the "declared: " form would print an empty list — which reads as
// a lane whose name is the empty string. The message has to say the repo
// declares none AND name the verb that declares one, because "none" without a
// remedy sends the user to open project.toml.
//
// The one-lane case pins the boundary rather than the branch: "declares none"
// is true only at zero. A repo holding a single lane that is told it has none
// gets sent to declare a second one under a name it is already using.
func TestTrustLaneOnALaneLessRepoSaysSo(t *testing.T) {
	t.Run("no lanes", func(t *testing.T) {
		dir := laneFixture(t)
		root := filepath.Join(dir, RootDirName)

		err := runCmd(t, Trust(), "--lane", "go")
		if err == nil {
			t.Fatal("trust --lane was accepted in a repo declaring no lanes")
		}
		for _, want := range []string{`"go"`, "declares none", "dross test lane add"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("refusal does not mention %q: %v", want, err)
			}
		}
		assertNoLaneGrant(t, root)
	})

	t.Run("one lane", func(t *testing.T) {
		dir := laneFixture(t)
		appendLanes(t, dir, `[[runtime.test_lane]]
name = "go"
match = ["internal/**"]
command = "go test -count=1 ./..."`)
		root := filepath.Join(dir, RootDirName)

		err := runCmd(t, Trust(), "--lane", "nosuch")
		if err == nil {
			t.Fatal("trust --lane nosuch was accepted")
		}
		if strings.Contains(err.Error(), "declares none") {
			t.Errorf("a repo declaring one lane was told it declares none: %v", err)
		}
		if !strings.Contains(err.Error(), "declared: go") {
			t.Errorf("refusal does not name the one declared lane: %v", err)
		}
		assertNoLaneGrant(t, root)
	})
}

// TestLaneGrantRefusesATrackedStore: a committed local.toml is a repo shipping
// its own authorization, and the lane grant is exactly as trust-bearing as
// every other key in that file. It shares refuseTrackedLocal rather than
// restating the rule, so this test is what proves the share is wired.
func TestLaneGrantRefusesATrackedStore(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustRunSet(t, "project.name", "test-app")
	mustRunSet(t, "runtime.mode", "native")
	appendLanes(t, dir, twoLanes)

	root := filepath.Join(dir, RootDirName)
	if err := os.WriteFile(filepath.Join(root, LocalFile), []byte("\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", "-f", RootDirName+"/"+LocalFile)

	state, err := LaneConsented(root, dir, "go", "go test -count=1 ./...")
	if err == nil {
		t.Fatal("a tracked local.toml was read rather than refused")
	}
	if state != ConsentRefused {
		t.Errorf("state = %v, want refused", state)
	}
	if !strings.Contains(err.Error(), "tracked") {
		t.Errorf("refusal does not say the store is tracked: %v", err)
	}
	if err := runCmd(t, Trust(), "--lane", "go"); err == nil {
		t.Fatal("`dross trust --lane` wrote into a tracked store")
	}
}

// TestRenamedLaneInheritsNothing: the grant is keyed by name, so renaming a
// lane is an edit like any other and the new name starts ungranted. Inheriting
// would mean a grant issued for `go` silently authorizing whatever `unit` runs.
func TestRenamedLaneInheritsNothing(t *testing.T) {
	root, repoDir := laneGrantFixture(t)
	mustGrantLane(t, root, "go", "go test -count=1 ./...")

	// Same command, new name: the lookup misses.
	if got := laneState(t, root, repoDir, "unit", "go test -count=1 ./..."); got != ConsentAbsent {
		t.Errorf("renamed lane state = %v, want absent — it inherited the old name's grant", got)
	}
}

// TestTrustLaneCheckDistinguishesTheThreeStates: --check is what a pre-flight
// runs, and the three answers call for three different reactions. Collapsing
// stale into absent would report a rewritten command as a routine first run,
// which is the case the binding exists for.
func TestTrustLaneCheckDistinguishesTheThreeStates(t *testing.T) {
	root, _ := laneGrantFixture(t)

	// Absent.
	err := runCmd(t, Trust(), "--lane", "go", "--check")
	if err == nil {
		t.Fatal("--check passed on an ungranted lane")
	}
	if strings.Contains(err.Error(), "CHANGED") {
		t.Errorf("an ungranted lane was reported as stale: %v", err)
	}
	if !strings.Contains(err.Error(), "not been trusted") {
		t.Errorf("absent refusal does not say untrusted: %v", err)
	}

	// Granted.
	mustGrantLane(t, root, "go", "go test -count=1 ./...")
	if err := runCmd(t, Trust(), "--lane", "go", "--check"); err != nil {
		t.Fatalf("--check failed on a granted lane: %v", err)
	}

	// Stale: the same lane name, a different command.
	mustGrantLane(t, root, "go", "go test -race ./...")
	err = runCmd(t, Trust(), "--lane", "go", "--check")
	if err == nil {
		t.Fatal("--check passed on a stale lane")
	}
	if !strings.Contains(err.Error(), "CHANGED") {
		t.Errorf("stale refusal does not distinguish itself from a first run: %v", err)
	}
}

// TestDoctorReportsEachLaneGrant: without this, the first signal that a lane's
// consent went stale is a refused run in the middle of a commit gate — after
// the code is written, at the worst moment to stop and read a message. Doctor
// answers the same question up front, per lane, so a repo with several lanes
// does not discover them one surprise at a time.
func TestDoctorReportsEachLaneGrant(t *testing.T) {
	root, _ := laneGrantFixture(t)
	mustRunSet(t, "runtime.test_command", "go test ./...")
	if err := GrantConsent(root, "go test ./..."); err != nil {
		t.Fatal(err)
	}
	mustGrantLane(t, root, "go", "go test -count=1 ./...")

	var out string
	_ = runCmdCapturing(t, &out, Doctor())

	if !strings.Contains(out, `lane "go"`) || !strings.Contains(out, `lane "docs"`) {
		t.Fatalf("doctor does not name both lanes:\n%s", out)
	}
	if !strings.Contains(out, `✓ lane "go"`) {
		t.Errorf("the granted lane is not reported as trusted:\n%s", out)
	}
	if !strings.Contains(out, `⚠ lane "docs"`) {
		t.Errorf("the ungranted lane is not reported as untrusted:\n%s", out)
	}
}

// TestDoctorStaleLaneMovesTheExitCode: an advisory nobody has to act on is one
// people learn to skim. ABSENT is the honest state of a fresh clone and stays
// advisory; STALE means something WAS trusted here and the command has since
// changed, which is the signature worth failing on.
func TestDoctorStaleLaneMovesTheExitCode(t *testing.T) {
	root, _ := laneGrantFixture(t)
	mustRunSet(t, "runtime.test_command", "go test ./...")
	if err := GrantConsent(root, "go test ./..."); err != nil {
		t.Fatal(err)
	}

	var absent string
	_ = runCmdCapturing(t, &absent, Doctor())
	absentIssues := strings.Count(absent, "✗")

	// A grant for a command the lane no longer declares.
	mustGrantLane(t, root, "go", "go test -race ./...")
	var stale string
	_ = runCmdCapturing(t, &stale, Doctor())

	if strings.Count(stale, "✗") <= absentIssues {
		t.Errorf("a stale lane grant did not raise an issue:\n%s", stale)
	}
	if !strings.Contains(stale, "CHANGED") {
		t.Errorf("doctor does not distinguish the stale lane from an untrusted one:\n%s", stale)
	}
}

// TestLaneWithNoCommandIsNotApplicable: a lane declaring no command cannot be
// trusted, because consent binds to a command line and there is none. It is a
// refusal with its own state, not an absent grant — the fix is to edit the
// lane, not to run `dross trust`.
func TestLaneWithNoCommandIsNotApplicable(t *testing.T) {
	dir := laneFixture(t)
	appendLanes(t, dir, `[[runtime.test_lane]]
name = "broken"
match = ["internal/**"]`)
	root := filepath.Join(dir, RootDirName)

	state, err := LaneConsented(root, dir, "broken", "")
	if state != ConsentNotApplicable {
		t.Errorf("state = %v, want not-applicable", state)
	}
	if err == nil {
		t.Fatal("a commandless lane must not report consent")
	}
	if !strings.Contains(err.Error(), "broken") {
		t.Errorf("the refusal does not name the lane: %v", err)
	}
	if terr := runCmd(t, Trust(), "--lane", "broken"); terr == nil {
		t.Fatal("`dross trust --lane` granted a lane with no command")
	}
}

// TestRevokeLaneConsentDropsOnlyThatLane is what `dross test lane remove`
// leans on: a removed lane's grant must not survive to authorize a lane that is
// later re-added under the same name, while every other lane's grant stands.
func TestRevokeLaneConsentDropsOnlyThatLane(t *testing.T) {
	root, repoDir := laneGrantFixture(t)
	mustGrantLane(t, root, "go", "go test -count=1 ./...")
	mustGrantLane(t, root, "docs", "markdownlint docs")

	if err := RevokeLaneConsent(root, "docs"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if got := laneState(t, root, repoDir, "docs", "markdownlint docs"); got != ConsentAbsent {
		t.Errorf("revoked lane state = %v, want absent", got)
	}
	if got := laneState(t, root, repoDir, "go", "go test -count=1 ./..."); got != ConsentGranted {
		t.Errorf("revoking one lane dropped another's grant: %v", got)
	}
	// Revoking what is already absent is not an error: the caller is asking
	// for a state that already holds.
	if err := RevokeLaneConsent(root, "nosuch"); err != nil {
		t.Errorf("revoking an absent grant errored: %v", err)
	}
}

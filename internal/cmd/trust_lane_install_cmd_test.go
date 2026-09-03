package cmd

// `dross trust --lane-install` and the gate it authorizes.
//
// The store and the fingerprint are proved a layer down; these are about the
// VERB and the refusal — that an ungranted line never reaches a seam, that the
// user reads the line before the store is written, and that a refusal here
// never reads like a refusal to run a suite.

import (
	"os"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/remote"
)

// installSeamRecorder stubs BOTH exec seams and returns their call counters, so
// "nothing was installed" is asserted on counts rather than on the absence of a
// message.
func installSeamRecorder(t *testing.T) (remoteCalls, localCalls *int) {
	t.Helper()
	var rn, ln int
	origRemote, origLocal := remoteExecFn, localInstallFn
	t.Cleanup(func() { remoteExecFn, localInstallFn = origRemote, origLocal })
	remoteExecFn = func(remote.Target, []string) (string, error) { rn++; return "", nil }
	localInstallFn = func([]string) (string, error) { ln++; return "", nil }
	return &rn, &ln
}

// TestUngrantedDeclaredLineNeverReachesASeam is the gate. Asserted on call
// counts in BOTH directions: refused with neither seam touched, then granted
// and reaching exactly one.
func TestUngrantedDeclaredLineNeverReachesASeam(t *testing.T) {
	root, repoDir := laneInstallFixture(t)
	lane := installLaneOf(t, repoDir, "go")
	step := resolveInstall("staticcheck", lane.Install)
	rn, ln := installSeamRecorder(t)

	if _, err := runLaneInstall(root, repoDir, nil, lane, step); err == nil {
		t.Fatal("an ungranted declared install line was executed")
	}
	if *rn != 0 || *ln != 0 {
		t.Fatalf("a refused install still reached a seam: remote=%d local=%d", *rn, *ln)
	}

	if err := runCmd(t, Trust(), "--lane-install", "go"); err != nil {
		t.Fatalf("dross trust --lane-install: %v", err)
	}
	if _, err := runLaneInstall(root, repoDir, nil, lane, step); err != nil {
		t.Fatalf("a granted install line was still refused: %v", err)
	}
	if *ln != 1 {
		t.Errorf("the granted install reached the local seam %d times, want 1", *ln)
	}
	if *rn != 0 {
		t.Errorf("a local install reached the remote seam %d times", *rn)
	}
}

// TestBuiltInRecipeNeedsNoGrant: dross's own recipes are not lines this repo
// supplied. Gating them would ask the user to consent to dross's own code,
// which teaches them to approve without reading — and it would leave every lane
// already declared uninstallable until someone granted something they never
// wrote.
func TestBuiltInRecipeNeedsNoGrant(t *testing.T) {
	root, repoDir := laneInstallFixture(t)
	docs := installLaneOf(t, repoDir, "docs")
	if docs.Install != "" {
		t.Fatal("the fixture's docs lane is meant to declare no install line")
	}
	step := resolveInstall("markdownlint", docs.Install)
	if len(step.Argv) == 0 {
		t.Fatalf("the fixture assumes a built-in markdownlint recipe: %+v", step)
	}
	_, ln := installSeamRecorder(t)

	// The grant store is untouched — no lane holds an install grant here.
	l, err := loadLocal(localPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(l.TrustedLaneInstalls) != 0 {
		t.Fatalf("the fixture already holds install grants: %v", l.TrustedLaneInstalls)
	}

	if _, err := runLaneInstall(root, repoDir, nil, docs, step); err != nil {
		t.Fatalf("a built-in recipe was gated behind a grant: %v", err)
	}
	if *ln != 1 {
		t.Errorf("the recipe reached the seam %d times, want 1", *ln)
	}
}

// TestInstallRefusalLadderKeepsItsRungs: absent and stale need different
// reactions — one is "read this line", the other is "read what changed" — and
// collapsing them reports a rewritten line as a routine first run.
func TestInstallRefusalLadderKeepsItsRungs(t *testing.T) {
	root, repoDir := laneInstallFixture(t)
	lane := installLaneOf(t, repoDir, "go")

	absent := runCmd(t, Trust(), "--lane-install", "go", "--check")
	if absent == nil {
		t.Fatal("--check passed on an ungranted install line")
	}

	if err := GrantLaneInstallConsent(root, "go", laneInstallConsentLine(lane)); err != nil {
		t.Fatal(err)
	}
	edited := lane
	edited.Install = "go install honnef.co/go/tools/cmd/staticcheck@v0.5.1"
	stale := laneInstallRefusal(edited, ConsentStale, ErrStaleConsent)

	if absent.Error() == stale.Error() {
		t.Fatal("the ungranted and stale arms produce the same message")
	}
	for _, err := range []error{absent, stale} {
		if !strings.Contains(err.Error(), `"go"`) {
			t.Errorf("the refusal does not name the lane: %v", err)
		}
		if !strings.Contains(err.Error(), "dross trust --lane-install go") {
			t.Errorf("the refusal does not name the fix: %v", err)
		}
		// It must NOT read as a refusal to run the suite — that would send the
		// user to `dross trust --lane`, granting the wrong thing.
		if strings.Contains(err.Error(), "refusing to run") {
			t.Errorf("an install refusal reads as a test refusal: %v", err)
		}
	}
	if !strings.Contains(absent.Error(), lane.Install) {
		t.Errorf("the ungranted refusal does not print the line: %v", absent)
	}
	if !strings.Contains(stale.Error(), edited.Install) {
		t.Errorf("the stale refusal does not print the line that changed: %v", stale)
	}
	if !strings.Contains(stale.Error(), "CHANGED") {
		t.Errorf("the stale arm does not say the line changed: %v", stale)
	}
}

// TestTrustLaneInstallShowsTheLineItGrants: a grant that did not show the line
// would be a rubber stamp on text nobody read, and the line comes from TRACKED
// project.toml — so it is whatever a clone's author wrote.
//
// The ORDER is asserted structurally, on the source: a verb that wrote first
// and printed after has already authorized the line by the time its text is on
// screen, and no output-only assertion can tell the two apart.
func TestTrustLaneInstallShowsTheLineItGrants(t *testing.T) {
	// Read BEFORE the fixture, which chdirs into a temp repo.
	src, err := os.ReadFile("trust.go")
	if err != nil {
		t.Fatal(err)
	}

	root, repoDir := laneInstallFixture(t)
	lane := installLaneOf(t, repoDir, "go")

	var out string
	if err := runCmdCapturing(t, &out, Trust(), "--lane-install", "go"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, lane.Install) {
		t.Errorf("the verb did not print the line it granted:\n%s", out)
	}
	if !strings.Contains(out, "dross trust --lane go") {
		t.Errorf("the output does not distinguish this grant from the lane's test grant:\n%s", out)
	}

	body := string(src)
	fn := body[strings.Index(body, "func trustLaneInstall("):]
	fn = fn[:strings.Index(fn, "\n}\n")]
	printed := strings.Index(fn, `Printf("trusting test lane %q's install line`)
	wrote := strings.Index(fn, "GrantLaneInstallConsent(")
	if printed < 0 || wrote < 0 {
		t.Fatalf("trustLaneInstall no longer prints then grants:\n%s", fn)
	}
	if printed > wrote {
		t.Error("the store is written before the line is printed — the grant is already made by the time the user reads it")
	}

	// --check reports the state without writing.
	before := mustRead(t, localPath(root))
	if err := runCmd(t, Trust(), "--lane-install", "go", "--check"); err != nil {
		t.Errorf("--check refused a granted install line: %v", err)
	}
	if after := mustRead(t, localPath(root)); after != before {
		t.Errorf("--check wrote to the store:\n--- before\n%s\n--- after\n%s", before, after)
	}
	_ = repoDir
}

// TestRemoveDropsAnInstallOnlyGrant is the asymmetric case a both-grants test
// never reaches: a lane holding an install grant and NO command grant. The
// removal used to short-circuit on the command map, so this grant would survive
// its lane — until someone re-added a lane under that name, which would start
// authorized to install whatever the deleted lane's line said.
func TestRemoveDropsAnInstallOnlyGrant(t *testing.T) {
	root, repoDir := laneInstallFixture(t)
	lane := installLaneOf(t, repoDir, "go")
	if err := GrantLaneInstallConsent(root, "go", laneInstallConsentLine(lane)); err != nil {
		t.Fatal(err)
	}
	l, err := loadLocal(localPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := l.TrustedLaneCommands["go"]; ok {
		t.Fatal("the fixture is meant to hold NO command grant for go")
	}

	if err := runCmd(t, Test(), "lane", "remove", "go"); err != nil {
		t.Fatal(err)
	}

	after, err := loadLocal(localPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := after.TrustedLaneInstalls["go"]; ok {
		t.Error("the install grant survived its lane")
	}
}

// TestRemoveIsAtomicAcrossBothGrants: a lane re-added under the same name must
// start ungranted on BOTH, or one removal would leave a door open that the
// other closed.
func TestRemoveIsAtomicAcrossBothGrants(t *testing.T) {
	root, repoDir := laneInstallFixture(t)
	lane := installLaneOf(t, repoDir, "go")
	mustGrantLane(t, root, "go", laneConsentLine(lane))
	if err := GrantLaneInstallConsent(root, "go", laneInstallConsentLine(lane)); err != nil {
		t.Fatal(err)
	}

	if err := runCmd(t, Test(), "lane", "remove", "go"); err != nil {
		t.Fatal(err)
	}

	after, err := loadLocal(localPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := after.TrustedLaneCommands["go"]; ok {
		t.Error("the command grant survived the removal")
	}
	if _, ok := after.TrustedLaneInstalls["go"]; ok {
		t.Error("the install grant survived the removal")
	}

	// Re-added under the same name: ungranted on both, from a fingerprint
	// issued for whatever the deleted lane used to run.
	mustAddLane(t, "go", "--match", "internal/**", "--command", "go test -count=1 ./...", "--install", lane.Install)
	readded := installLaneOf(t, repoDir, "go")
	if got := laneState(t, root, repoDir, "go", laneConsentLine(readded)); got != ConsentAbsent {
		t.Errorf("a re-added lane's command grant reads %v, want Absent", got)
	}
	state, _ := LaneInstallConsented(root, repoDir, "go", laneInstallConsentLine(readded))
	if state != ConsentAbsent {
		t.Errorf("a re-added lane's install grant reads %v, want Absent", state)
	}
}

// TestTrustLaneInstallWithNoLineRefuses pins trust.go's nothing-to-trust branch
// for the lane-scoped install grant.
//
// Consent is bound to a LINE (install_consent, locked), so a lane that declares
// none has nothing to bind to. Granting anyway would record consent for the
// empty string and then silently satisfy the gate for whatever line was added
// afterwards — the same failure TestTrustWithoutATestCommandRefuses guards on
// the whole-suite grant, on a surface that never had its own test.
//
// The message is asserted as well as the refusal: it is the only thing telling
// the user that the remedy is `dross test lane edit` rather than re-running the
// grant, and it is the string whose compile-time concatenation the ARITHMETIC_BASE
// acceptances at .dross/survivors.toml cite. Those acceptances are checkable
// precisely because this test pins what the folded literal says.
func TestTrustLaneInstallWithNoLineRefuses(t *testing.T) {
	root, repoDir := laneInstallFixture(t)
	docs := installLaneOf(t, repoDir, "docs")
	if docs.Install != "" {
		t.Fatal("the fixture's docs lane is meant to declare no install line")
	}

	err := runCmd(t, Trust(), "--lane-install", "docs")
	if err == nil {
		t.Fatal("`dross trust --lane-install docs` exited 0 on a lane with no install line — it granted consent for nothing")
	}
	for _, want := range []string{
		"nothing to trust",
		"docs",
		"declares no install line",
		"dross test lane edit",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not mention %q", err, want)
		}
	}

	// A refused grant must record nothing. Read through loadLocal rather than
	// the file: the fixture holds no grants, so local.toml legitimately does not
	// exist yet, and "no file" is the strongest form of "nothing was written".
	l, lerr := loadLocal(localPath(root))
	if lerr != nil {
		t.Fatal(lerr)
	}
	if len(l.TrustedLaneInstalls) != 0 {
		t.Errorf("a refused install-trust recorded a grant: %v", l.TrustedLaneInstalls)
	}
}

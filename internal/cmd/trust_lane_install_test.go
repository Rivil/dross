package cmd

// The per-lane INSTALL grant: a second consent store, answering a different
// question from the one the lane's test grant answers.
//
// Everything here is about the two staying INDEPENDENT. A single fingerprint
// covering both lines would satisfy a test that only checked "the install line
// is consented to" while breaking the gate that was already passing — which is
// the exact regression locked install_consent exists to prevent.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/project"
)

// laneInstallFixture is laneGrantFixture with an install line on the go lane.
// The docs lane deliberately declares none: a fixture where every lane has one
// cannot tell a per-lane grant from a blanket one.
func laneInstallFixture(t *testing.T) (root, repoDir string) {
	t.Helper()
	dir := laneFixture(t)
	appendLanes(t, dir, `[[runtime.test_lane]]
name = "go"
match = ["internal/**"]
command = "go test -count=1 ./..."
install = "go install honnef.co/go/tools/cmd/staticcheck@latest"

[[runtime.test_lane]]
name = "docs"
match = ["docs/"]
command = "markdownlint docs"`)
	return filepath.Join(dir, RootDirName), dir
}

func installLaneOf(t *testing.T, repoDir, name string) project.TestLane {
	t.Helper()
	p, err := project.Load(filepath.Join(repoDir, RootDirName, project.File))
	if err != nil {
		t.Fatal(err)
	}
	for _, lane := range p.Runtime.TestLane {
		if lane.Name == name {
			return lane
		}
	}
	t.Fatalf("no lane %q in the fixture", name)
	return project.TestLane{}
}

// TestAddingAnInstallLineDoesNotStaleTheTestGrant is locked install_consent,
// stated as the regression it forbids: a lane that already runs green must keep
// running green the moment an install line is added. Folding Install into
// laneConsentLine the way Prepare is folded in would refuse a gate over a line
// that has never executed.
func TestAddingAnInstallLineDoesNotStaleTheTestGrant(t *testing.T) {
	root, repoDir := laneInstallFixture(t)
	lane := installLaneOf(t, repoDir, "go")

	// Granted over the lane's TEST line only, which is what a user who trusted
	// this lane before install lines existed has on disk.
	mustGrantLane(t, root, "go", laneConsentLine(lane))

	if got := laneState(t, root, repoDir, "go", laneConsentLine(lane)); got != ConsentGranted {
		t.Errorf("the test grant reads %v, want Granted — an install line staled a suite that never changed", got)
	}
	state, _ := LaneInstallConsented(root, repoDir, "go", laneInstallConsentLine(lane))
	if state != ConsentAbsent {
		t.Errorf("the install grant reads %v, want Absent — one edit must yield two independent answers", state)
	}
}

// TestEditedInstallLineIsStaleNotAbsent: collapsing STALE into ABSENT loses the
// only signal that says a line the user approved has since been rewritten, and
// it must not disturb the lane's test grant on the way.
func TestEditedInstallLineIsStaleNotAbsent(t *testing.T) {
	root, repoDir := laneInstallFixture(t)
	lane := installLaneOf(t, repoDir, "go")
	mustGrantLane(t, root, "go", laneConsentLine(lane))
	if err := GrantLaneInstallConsent(root, "go", laneInstallConsentLine(lane)); err != nil {
		t.Fatal(err)
	}

	edited := lane
	edited.Install = "go install honnef.co/go/tools/cmd/staticcheck@v0.5.1"

	state, err := LaneInstallConsented(root, repoDir, "go", laneInstallConsentLine(edited))
	if state != ConsentStale {
		t.Errorf("an edited install line reads %v, want Stale (err %v)", state, err)
	}
	if got := laneState(t, root, repoDir, "go", laneConsentLine(lane)); got != ConsentGranted {
		t.Errorf("rewriting the install line disturbed the lane's TEST grant: %v", got)
	}
}

// TestInstallFrameIsDisjointFromACommand: the two namespaces must be separate by
// construction, not by being hard to hit. A command that could spell an install
// grant's own bytes would be a lane authorizing its own installs through the
// grant issued for running its suite.
func TestInstallFrameIsDisjointFromACommand(t *testing.T) {
	lane := project.TestLane{Name: "go", Command: "go test ./...", Install: "npm i -g pnpm"}
	framed := laneInstallConsentLine(lane)
	if framed == "" {
		t.Fatal("the fixture's install line is un-grantable")
	}

	// The framed bytes fed back as a bare command line.
	forged := laneConsentLine(project.TestLane{Name: "go", Command: framed})
	if Fingerprint(forged) == Fingerprint(framed) {
		t.Error("a command spelled as the install frame hashes to the install grant")
	}
	// It is refused outright rather than merely hashing differently: the frame
	// carries a NUL, and no argv element can.
	if forged != "" {
		t.Errorf("a command carrying a NUL was accepted as a consent line: %q", forged)
	}
	// And the same install line must not collide with the lane's own test line.
	if Fingerprint(framed) == Fingerprint(laneConsentLine(lane)) {
		t.Error("a lane's install line and its command line hash the same")
	}
}

// TestInstallLineWithANulIsUngrantable mirrors laneConsentLine's guard. An argv
// element is NUL-terminated, so a line carrying one can never be exec'd under
// any shell — binding consent to it would bind to something that can never run.
func TestInstallLineWithANulIsUngrantable(t *testing.T) {
	root, repoDir := laneInstallFixture(t)
	lane := project.TestLane{Name: "go", Command: "go test ./...", Install: "npm i\x00 -g pnpm"}

	line := laneInstallConsentLine(lane)
	if line != "" {
		t.Fatalf("an install line carrying a NUL produced a consent line: %q", line)
	}
	state, err := LaneInstallConsented(root, repoDir, "go", line)
	if state != ConsentNotApplicable {
		t.Errorf("an un-grantable install reads %v, want NotApplicable", state)
	}
	if err == nil || !strings.Contains(err.Error(), "install") {
		t.Errorf("the refusal does not name the field: %v", err)
	}

	// A lane declaring nothing at all takes the same arm, and says so with the
	// install-specific sentinel rather than the command one.
	if got := laneInstallConsentLine(project.TestLane{Name: "docs", Command: "markdownlint docs"}); got != "" {
		t.Errorf("a lane with no install line produced a consent line: %q", got)
	}
}

// TestTrustedLaneInstallsIsNotASettableKey: a generic key-writer that could
// grant it would authorize changing a machine without ever showing the user the
// line it was about to run there.
func TestTrustedLaneInstallsIsNotASettableKey(t *testing.T) {
	laneInstallFixture(t)

	err := runCmd(t, Local(), "set", "trusted_lane_installs", "go=deadbeef")
	if err == nil {
		t.Fatal("`dross local set trusted_lane_installs` was accepted")
	}
	if _, ok := localKeys["trusted_lane_installs"]; ok {
		t.Error("trusted_lane_installs reached localKeys")
	}
}

// TestInstallGrantsAreIsolatedPerLane: a grant that replaced the store would
// make trusting one lane's install revoke another's, so the user would
// re-consent to everything every time they touched anything.
func TestInstallGrantsAreIsolatedPerLane(t *testing.T) {
	root, repoDir := laneInstallFixture(t)
	if err := GrantLaneInstallConsent(root, "docs", "docs-line"); err != nil {
		t.Fatal(err)
	}
	before, err := loadLocal(localPath(root))
	if err != nil {
		t.Fatal(err)
	}
	docsBefore := before.TrustedLaneInstalls["docs"]

	if err := GrantLaneInstallConsent(root, "go", laneInstallConsentLine(installLaneOf(t, repoDir, "go"))); err != nil {
		t.Fatal(err)
	}

	after, err := loadLocal(localPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if after.TrustedLaneInstalls["docs"] != docsBefore {
		t.Errorf("granting go's install changed docs': %q -> %q", docsBefore, after.TrustedLaneInstalls["docs"])
	}
	if after.TrustedLaneInstalls["go"] == "" {
		t.Error("go's install grant was not written")
	}

	// Revoking one leaves the other, and revoking the last drops the table
	// rather than leaving a bare header that reads as a store holding
	// something.
	if err := RevokeLaneInstallConsent(root, "go"); err != nil {
		t.Fatal(err)
	}
	l, err := loadLocal(localPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := l.TrustedLaneInstalls["go"]; ok {
		t.Error("the revoked grant is still there")
	}
	if l.TrustedLaneInstalls["docs"] != docsBefore {
		t.Error("revoking go's install grant disturbed docs'")
	}
	if err := RevokeLaneInstallConsent(root, "docs"); err != nil {
		t.Fatal(err)
	}
	if body := mustRead(t, localPath(root)); strings.Contains(body, "trusted_lane_installs") {
		t.Errorf("the emptied table left its header behind:\n%s", body)
	}
	// A missing entry is not an error.
	if err := RevokeLaneInstallConsent(root, "never-granted"); err != nil {
		t.Errorf("revoking a grant that was never there errored: %v", err)
	}
}

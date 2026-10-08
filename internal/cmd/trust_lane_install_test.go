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
	"testing"

	"github.com/Rivil/dross/internal/project"
)

// The store-level install tests — TestAddingAnInstallLineDoesNotStaleTheTestGrant,
// TestEditedInstallLineIsStaleNotAbsent, TestInstallFrameIsDisjointFromACommand,
// TestInstallLineWithANulIsUngrantable, TestInstallGrantsAreIsolatedPerLane —
// moved with the grants to internal/consent.

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

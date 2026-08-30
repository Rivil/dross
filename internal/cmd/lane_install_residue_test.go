package cmd

// c-9: an install is a fact about a machine, not sticky state in dross.
//
// The invariant is asserted across BOTH surfaces rather than assumed from one,
// because they write through different code and a cache added to either would
// break it alone. What it buys is the property the fallback already has: a host
// that gains a binary goes back to running that lane remotely with NO user
// action — no re-grant, no edit, no second command.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/remote"
)

// residueSnapshot reads the two files an install could plausibly cache into —
// the machine-local grant store and the tracked project config.
//
// A missing local.toml reads as the empty string rather than failing: "the file
// does not exist" and "the file is unchanged" are both correct answers to "was
// anything written", and a fixture that has never granted anything has none.
func residueSnapshot(t *testing.T, root string) (local, proj string) {
	t.Helper()
	if b, err := os.ReadFile(localPath(root)); err == nil {
		local = string(b)
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, project.File))
	if err != nil {
		t.Fatal(err)
	}
	return local, string(b)
}

// TestAppliedInstallWritesNothingAnywhere is c-9 across both surfaces, on a
// BYTE compare rather than on a field check: a cache under a key nobody thought
// to assert on would pass a field check and still be residue.
func TestAppliedInstallWritesNothingAnywhere(t *testing.T) {
	t.Run("dross test lane install --apply", func(t *testing.T) {
		grantedLaneFixture(t, pnpmLane)
		installLaneLookPath(t, "pnpm")
		fakeProbe(t, func(_ remote.Target, _ []string) (remote.Readiness, error) {
			return remote.Readiness{Cores: 8, Missing: []string{"pnpm"}}, nil
		})
		installSeamRecorder(t)
		root, err := FindRoot()
		if err != nil {
			t.Fatal(err)
		}
		beforeLocal, beforeProj := residueSnapshot(t, root)

		if err := runCmd(t, Test(), "lane", "install", "web", "--apply"); err != nil {
			t.Fatal(err)
		}

		afterLocal, afterProj := residueSnapshot(t, root)
		if afterLocal != beforeLocal {
			t.Errorf("the install wrote to local.toml:\n--- before\n%s\n--- after\n%s", beforeLocal, afterLocal)
		}
		if afterProj != beforeProj {
			t.Errorf("the install wrote to project.toml:\n--- before\n%s\n--- after\n%s", beforeProj, afterProj)
		}
	})

	t.Run("dross remote bootstrap --apply", func(t *testing.T) {
		bootstrapLaneFixture(t, bootstrapTwoLanes)
		probeMissing(t, "pnpm")
		rec := &execRecorder{}
		rec.install(t)
		root, err := FindRoot()
		if err != nil {
			t.Fatal(err)
		}
		beforeLocal, beforeProj := residueSnapshot(t, root)

		if _, err := runBootstrap(t, "--apply"); err != nil {
			t.Fatal(err)
		}
		if len(rec.argvs) == 0 {
			t.Fatal("the fixture installed nothing, so it proves nothing")
		}

		afterLocal, afterProj := residueSnapshot(t, root)
		if afterLocal != beforeLocal {
			t.Errorf("bootstrap wrote to local.toml:\n--- before\n%s\n--- after\n%s", beforeLocal, afterLocal)
		}
		if afterProj != beforeProj {
			t.Errorf("bootstrap wrote to project.toml:\n--- before\n%s\n--- after\n%s", beforeProj, afterProj)
		}
	})
}

// TestRoutingResumesOnTheProbeAlone: with the tool now PRESENT, the lane goes
// remote again with no intervening command and nothing read from the grant
// store. If routing needed a user gesture after an install, this is where it
// would show — the lane would still come home.
func TestRoutingResumesOnTheProbeAlone(t *testing.T) {
	lane := project.TestLane{Name: "web", Command: "pnpm test"}

	// Before: the host lacks pnpm, so the lane falls back and offers the verb.
	before := laneLocality(lanesFor(lane), "helicon", []string{"pnpm"}, haveTools("pnpm"))
	if before[0].Site != siteLocal {
		t.Fatalf("the fixture did not fall back: %v", before[0].Site)
	}

	// After: the SAME call with the probe now reporting the tool present. No
	// command ran in between, and nothing on disk changed.
	after := laneLocality(lanesFor(lane), "helicon", nil, haveTools("pnpm"))
	if after[0].Site != siteRemote {
		t.Errorf("the lane is still local after the host gained the tool: %v", after[0].Site)
	}
	if after[0].Announce != "" {
		t.Errorf("a lane that went remote still announced a fallback: %q", after[0].Announce)
	}
}

// TestLocalityNeverConsultsTheInstallGrant: routing must depend on what a
// machine HAS, never on what this machine has consented to. A locality decision
// that read the grant store would send a lane home because its install line was
// ungranted — a consent question answering a capability question.
func TestLocalityNeverConsultsTheInstallGrant(t *testing.T) {
	lane := project.TestLane{Name: "web", Command: "pnpm test", Install: "corepack enable pnpm"}

	// Read BEFORE the fixture, which chdirs into a temp repo.
	src, err := os.ReadFile("lane_locality.go")
	if err != nil {
		t.Fatal(err)
	}

	// Recorded for the declared line, then deliberately never mentioned again.
	root, repoDir := laneInstallFixture(t)
	if err := GrantLaneInstallConsent(root, "web", laneInstallConsentLine(lane)); err != nil {
		t.Fatal(err)
	}
	state, _ := LaneInstallConsented(root, repoDir, "web", laneInstallConsentLine(lane))
	if state != ConsentGranted {
		t.Fatalf("the fixture's grant did not land: %v", state)
	}

	granted := laneLocality(lanesFor(lane), "helicon", []string{"pnpm"}, haveTools("pnpm"))

	if err := RevokeLaneInstallConsent(root, "web"); err != nil {
		t.Fatal(err)
	}
	revoked := laneLocality(lanesFor(lane), "helicon", []string{"pnpm"}, haveTools("pnpm"))

	if granted[0].Site != revoked[0].Site {
		t.Errorf("revoking the install grant changed where the lane runs: %v -> %v", granted[0].Site, revoked[0].Site)
	}
	if granted[0].Announce != revoked[0].Announce {
		t.Errorf("the install grant reached the locality announcement:\n granted %q\n revoked %q", granted[0].Announce, revoked[0].Announce)
	}
	// Structural, not only behavioural: the decision site must not name the
	// grant reader at all, or a future edit could reintroduce the coupling
	// without changing this fixture's outcome.
	for _, forbidden := range []string{"LaneInstallConsented", "TrustedLaneInstalls", "laneInstallConsentLine"} {
		if strings.Contains(string(src), forbidden) {
			t.Errorf("lane_locality.go reads %s — routing must depend on what a machine has, not on what was consented to", forbidden)
		}
	}
}

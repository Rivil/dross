package cmd

// `dross test lane --toolchain` — declaring, editing and inspecting the probe
// set a lane's locality is decided from.
//
// The recurring assertion here is that the flag reaches disk in a shape the run
// can use and `dross validate` accepts. An override entry is asked of a host as
// `command -v <entry>`, so a shape no host can answer does not fail loudly — it
// pins its lane to local on every future run, silently, with nothing in the
// transcript naming the override as the cause.

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/project"
)

// projectBytes is project.toml read raw. Compared as BYTES rather than
// reparsed: a refusal that load-modify-saved before rejecting produces an
// identical parse and a rewritten file, which is exactly the difference "leaves
// project.toml unchanged" is claiming.
func projectBytes(t *testing.T, dir string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, RootDirName, project.File))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func laneListOutput(t *testing.T) string {
	t.Helper()
	var out string
	if err := runCmdCapturing(t, &out, Test(), "lane", "list"); err != nil {
		t.Fatalf("lane list: %v", err)
	}
	return out
}

// TestLaneAddWritesToolchainInFlagOrder: the list is probed in order and
// reported in order, so flag order is the user's and must survive the encoder.
func TestLaneAddWritesToolchainInFlagOrder(t *testing.T) {
	dir := laneFixture(t)
	mustAddLane(t, "x", "--match", "internal/**", "--command", "go test ./...",
		"--toolchain", "go", "--toolchain", "make")

	lanes := loadLanes(t, dir)
	if len(lanes) != 1 {
		t.Fatalf("want 1 lane, got %d", len(lanes))
	}
	if !reflect.DeepEqual(lanes[0].Toolchain, []string{"go", "make"}) {
		t.Errorf("toolchain = %v, want [go make] in flag order", lanes[0].Toolchain)
	}
}

// TestLaneAddRefusesUnprobableToolchain: every shape here would resolve on no
// host, so the lane would go local forever with no message pointing at the
// cause. Refused through the SAME problems validate reports, and refused BEFORE
// the write — asserted on bytes, since a rewrite-then-reject parses identically.
func TestLaneAddRefusesUnprobableToolchain(t *testing.T) {
	for _, tc := range []struct{ name, entry string }{
		{"blank", ""},
		{"command line", "go test"},
		{"env assignment", "FOO=1"},
		{"path", "./x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := laneFixture(t)
			before := projectBytes(t, dir)

			err := runCmd(t, Test(), "lane", "add", "x", "--match", "internal/**",
				"--command", "go test ./...", "--toolchain", tc.entry)
			if err == nil {
				t.Fatalf("--toolchain %q was accepted", tc.entry)
			}
			if !strings.Contains(err.Error(), "toolchain") {
				t.Errorf("the refusal does not name the field: %v", err)
			}
			if got := projectBytes(t, dir); string(got) != string(before) {
				t.Errorf("a refused add rewrote project.toml:\n%s", got)
			}
		})
	}
}

// TestLaneListShowsEveryLanesEffectiveToolchain is c-7's point: the probe set
// must be inspectable WITHOUT reading project.toml and re-deriving it by hand.
// A listing that printed only declared overrides would hide it for exactly the
// lanes nobody has ever looked at — every lane written before this phase.
func TestLaneListShowsEveryLanesEffectiveToolchain(t *testing.T) {
	laneFixture(t)
	mustAddLane(t, "go", "--match", "internal/**", "--command", "go test ./...")
	mustAddLane(t, "web", "--match", "web/**", "--command", "mise exec -- pnpm test", "--toolchain", "mise")

	out := laneListOutput(t)
	for _, want := range []string{"toolchain: go (derived)", "toolchain: mise (overridden)"} {
		if !strings.Contains(out, want) {
			t.Errorf("lane list does not print %q:\n%s", want, out)
		}
	}
}

// TestLaneEditToolchainKeepsEverythingElse: toolchain is not part of the
// consent line — it names binaries, not a command — so an edit that folded it
// in would stale a grant the user has read, for a change the grant never
// covered.
func TestLaneEditToolchainKeepsEverythingElse(t *testing.T) {
	dir := twoLaneEditFixture(t)
	root, err := FindRoot()
	if err != nil {
		t.Fatal(err)
	}
	before := loadLanes(t, dir)
	line := laneConsentLine(before[0])
	if err := GrantLaneConsent(root, "go", line); err != nil {
		t.Fatal(err)
	}

	mustEditLane(t, "go", "--toolchain", "mise")

	after := loadLanes(t, dir)
	if after[0].Name != "go" || after[1].Name != "docs" {
		t.Errorf("the edit reordered the document: %s then %s", after[0].Name, after[1].Name)
	}
	if !reflect.DeepEqual(after[1], before[1]) {
		t.Errorf("the neighbour changed:\n before %+v\n after  %+v", before[1], after[1])
	}
	got := after[0]
	got.Toolchain = nil
	if !reflect.DeepEqual(got, before[0]) {
		t.Errorf("the edit changed more than the toolchain:\n before %+v\n after  %+v", before[0], after[0])
	}
	if laneConsentLine(after[0]) != line {
		t.Errorf("the consent line moved: %q -> %q", line, laneConsentLine(after[0]))
	}
	state, cerr := LaneConsented(root, filepath.Dir(root), "go", laneConsentLine(after[0]))
	if cerr != nil || state != ConsentGranted {
		t.Errorf("the grant did not survive a toolchain edit: %v (%v)", state, cerr)
	}
}

// TestLaneEditToolchainClearsBackToDerived: `--toolchain ""` is the clear
// gesture, spelled exactly as `--prepare ""` is. Without it an override is
// write-once and the only way back is remove-then-re-add, which drops the grant.
func TestLaneEditToolchainClearsBackToDerived(t *testing.T) {
	dir := laneFixture(t)
	mustAddLane(t, "go", "--match", "internal/**", "--command", "go test ./...", "--toolchain", "mise")

	mustEditLane(t, "go", "--toolchain", "")

	lanes := loadLanes(t, dir)
	if len(lanes[0].Toolchain) != 0 {
		t.Errorf("toolchain = %v, want cleared back to derived", lanes[0].Toolchain)
	}
	if got := projectBytes(t, dir); strings.Contains(string(got), "toolchain") {
		t.Errorf("the cleared key is still in project.toml:\n%s", got)
	}
	if out := laneListOutput(t); !strings.Contains(out, "toolchain: go (derived)") {
		t.Errorf("the cleared lane does not report a derived toolchain:\n%s", out)
	}
}

// TestLaneEditWithoutToolchainLeavesItAlone is the Changed guard, one flag
// deeper. An emptiness check rather than Flags().Changed would let an edit that
// only touched prepare silently drop an override the user never mentioned.
func TestLaneEditWithoutToolchainLeavesItAlone(t *testing.T) {
	dir := laneFixture(t)
	mustAddLane(t, "go", "--match", "internal/**", "--command", "go test ./...", "--toolchain", "mise")

	mustEditLane(t, "go", "--prepare", "make build")

	lanes := loadLanes(t, dir)
	if !reflect.DeepEqual(lanes[0].Toolchain, []string{"mise"}) {
		t.Errorf("toolchain = %v, want [mise] untouched by a prepare-only edit", lanes[0].Toolchain)
	}
}

// TestLaneEditWithoutPrepareLeavesItAlone is the mirror: a toolchain-only edit
// must not clear a prepare, which an unconditional write of both fields would.
func TestLaneEditWithoutPrepareLeavesItAlone(t *testing.T) {
	dir := laneFixture(t)
	mustAddLane(t, "go", "--match", "internal/**", "--command", "go test ./...", "--prepare", "make build")

	mustEditLane(t, "go", "--toolchain", "mise")

	if got := loadLanes(t, dir)[0].Prepare; got != "make build" {
		t.Errorf("prepare = %q, want it untouched by a toolchain-only edit", got)
	}
}

// TestLaneEditNoFlagsNamesBothFlags: the no-op gate widened with the surface.
// A refusal still naming only --prepare would send the user away from the flag
// they were reaching for.
func TestLaneEditNoFlagsNamesBothFlags(t *testing.T) {
	laneFixture(t)
	mustAddLane(t, "go", "--match", "internal/**", "--command", "go test ./...")

	err := runCmd(t, Test(), "lane", "edit", "go")
	if err == nil {
		t.Fatal("`lane edit` with no flags was accepted")
	}
	for _, want := range []string{"--prepare", "--toolchain"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %s: %v", want, err)
		}
	}
}

// TestLaneEditRefusesUnprobableToolchain: the CLI must never write what
// validate rejects, on the edit path as much as on the add path — and must
// refuse before the write, or the "unchanged" claim is false.
func TestLaneEditRefusesUnprobableToolchain(t *testing.T) {
	dir := laneFixture(t)
	mustAddLane(t, "go", "--match", "internal/**", "--command", "go test ./...")
	before := projectBytes(t, dir)

	err := runCmd(t, Test(), "lane", "edit", "go", "--toolchain", "FOO=1")
	if err == nil {
		t.Fatal("`lane edit --toolchain FOO=1` was accepted")
	}
	if got := projectBytes(t, dir); string(got) != string(before) {
		t.Errorf("a refused edit rewrote project.toml:\n%s", got)
	}
}

// TestLaneEditHelpNamesToolchain: the docs parity test only checks
// docs→registered-flags, so a Long still saying "Only prepare is editable"
// would survive every other test here while telling the user the opposite of
// what the command does.
func TestLaneEditHelpNamesToolchain(t *testing.T) {
	long := testLaneEdit().Long
	if !strings.Contains(long, "--toolchain") {
		t.Errorf("`lane edit` help does not name --toolchain:\n%s", long)
	}
	if strings.Contains(long, "Only prepare is editable") {
		t.Errorf("`lane edit` help still claims prepare is the only editable field:\n%s", long)
	}
}

// TestValidateAcceptsWhatLaneAddWrote closes the loop the refusal opens: a lane
// the CLI accepted must pass `dross validate`. The two surfaces read the same
// problems, and this is the assertion that keeps them doing so.
func TestValidateAcceptsWhatLaneAddWrote(t *testing.T) {
	laneFixture(t)
	mustAddLane(t, "go", "--match", "internal/**", "--command", "go test ./...", "--toolchain", "mise", "--toolchain", "make")

	if out, err := validateOutput(t); err != nil {
		t.Errorf("validate rejected a lane the CLI wrote: %v\n%s", err, out)
	}
}

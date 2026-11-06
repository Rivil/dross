package consent

import (
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/project"
)

// The two lanes every isolation assertion needs: one whose grant is being
// exercised and a second that must be left alone. A single lane cannot tell
// "granted this lane" from "granted everything".
var (
	goLane   = project.TestLane{Name: "go", Command: "go test -count=1 ./..."}
	docsLane = project.TestLane{Name: "docs", Command: "markdownlint docs"}
)

func mustGrantLane(t *testing.T, store Store, name, line string) {
	t.Helper()
	if err := GrantLaneConsent(store, name, line); err != nil {
		t.Fatalf("grant %s: %v", name, err)
	}
}

func laneState(t *testing.T, store Store, repoDir, name, line string) State {
	t.Helper()
	state, _ := LaneConsented(store, repoDir, name, line)
	return state
}

// TestOneLaneGoesStaleAlone is the locked lane_consent decision, stated as a
// test: an aggregate hash over every lane's command would make a one-character
// edit to the docs lane revoke the Go lane too. That is not a smaller
// inconvenience than it sounds — the Go lane is the pre-commit gate, so a docs
// typo would block committing until someone re-consented to a suite they never
// touched, and a gate that behaves like that gets routed around.
func TestOneLaneGoesStaleAlone(t *testing.T) {
	store, _, repoDir := consentFixture(t)
	mustGrantLane(t, store, "go", goLane.Command)
	mustGrantLane(t, store, "docs", docsLane.Command)

	// The docs lane's command is edited; the Go lane's is not.
	if got := laneState(t, store, repoDir, "docs", "markdownlint --fix docs"); got != Stale {
		t.Errorf("edited lane state = %v, want stale", got)
	}
	if got := laneState(t, store, repoDir, "go", goLane.Command); got != Granted {
		t.Errorf("untouched lane state = %v, want granted — one lane's edit revoked another's grant", got)
	}
	// Re-granting one lane leaves the other's fingerprint untouched.
	before, _ := store.Load()
	mustGrantLane(t, store, "go", "go test ./...")
	after, _ := store.Load()
	if after.TrustedLaneCommands["docs"] != before.TrustedLaneCommands["docs"] {
		t.Error("re-granting go rewrote docs' fingerprint")
	}
	if after.TrustedLaneCommands["go"] == before.TrustedLaneCommands["go"] {
		t.Error("re-granting go did not update go's fingerprint")
	}
}

// TestRenamedLaneInheritsNothing: the grant is keyed by name, so renaming a
// lane is an edit like any other and the new name starts ungranted. Inheriting
// would mean a grant issued for `go` silently authorizing whatever `unit` runs.
func TestRenamedLaneInheritsNothing(t *testing.T) {
	store, _, repoDir := consentFixture(t)
	mustGrantLane(t, store, "go", goLane.Command)

	// Same command, new name: the lookup misses.
	if got := laneState(t, store, repoDir, "unit", goLane.Command); got != Absent {
		t.Errorf("renamed lane state = %v, want absent — it inherited the old name's grant", got)
	}
}

// TestLaneWithNoCommandIsNotApplicableAtTheStore: a lane declaring no command cannot be
// trusted, because consent binds to a command line and there is none. It is a
// refusal with its own state, not an absent grant — the fix is to edit the
// lane, not to run `dross trust`.
func TestLaneWithNoCommandIsNotApplicableAtTheStore(t *testing.T) {
	store, _, repoDir := consentFixture(t)
	broken := project.TestLane{Name: "broken"}
	if got := LaneLine(broken); got != "" {
		t.Fatalf("a commandless lane produced a consent line: %q", got)
	}
	state, err := LaneConsented(store, repoDir, "broken", LaneLine(broken))
	if state != NotApplicable {
		t.Errorf("state = %v, want not-applicable", state)
	}
	if err == nil {
		t.Fatal("a commandless lane must not report consent")
	}
	if !strings.Contains(err.Error(), "broken") {
		t.Errorf("the refusal does not name the lane: %v", err)
	}
	if !contains(err.Error(), ErrNoLaneCommand.Error()) {
		t.Errorf("the refusal does not carry ErrNoLaneCommand: %v", err)
	}
}

// TestRevokeLaneConsentDropsOnlyThatLane is what `dross test lane remove`
// leans on: a removed lane's grant must not survive to authorize a lane that is
// later re-added under the same name, while every other lane's grant stands.
func TestRevokeLaneConsentDropsOnlyThatLane(t *testing.T) {
	store, _, repoDir := consentFixture(t)
	mustGrantLane(t, store, "go", goLane.Command)
	mustGrantLane(t, store, "docs", docsLane.Command)

	if err := RevokeLaneConsent(store, "docs"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if got := laneState(t, store, repoDir, "docs", docsLane.Command); got != Absent {
		t.Errorf("revoked lane state = %v, want absent", got)
	}
	if got := laneState(t, store, repoDir, "go", goLane.Command); got != Granted {
		t.Errorf("revoking one lane dropped another's grant: %v", got)
	}
	// Revoking what is already absent is not an error: the caller is asking
	// for a state that already holds.
	if err := RevokeLaneConsent(store, "nosuch"); err != nil {
		t.Errorf("revoking an absent grant errored: %v", err)
	}
	// Revoking the last lane leaves no empty table behind.
	if err := RevokeLaneConsent(store, "go"); err != nil {
		t.Fatal(err)
	}
	g, _ := store.Load()
	if g.TrustedLaneCommands != nil {
		t.Errorf("the emptied map is %#v, want nil so omitempty drops the table", g.TrustedLaneCommands)
	}
}

// TestLaneConsentFramingSeparatesThePrepareFromTheCommand: the two lines are
// LENGTH-FRAMED, not concatenated.
//
// Naive concatenation hashes {prepare:"a", command:"bc"} and {prepare:"ab",
// command:"c"} to the same value — which is a lane whose split between
// bootstrap and suite moved keeping a grant that was issued for neither
// arrangement. The user reads two lines; the store must bind to the same two.
func TestLaneConsentFramingSeparatesThePrepareFromTheCommand(t *testing.T) {
	left := LaneLine(project.TestLane{Name: "go", Prepare: "a", Command: "bc"})
	right := LaneLine(project.TestLane{Name: "go", Prepare: "ab", Command: "c"})
	if left == right {
		t.Fatalf("a re-split of the same characters produced one consent line: %q", left)
	}
	if Fingerprint(left) == Fingerprint(right) {
		t.Errorf("the two arrangements fingerprint identically — a grant for one authorizes the other")
	}
	// A command carrying a NUL is refused outright: it can never be exec'd.
	if got := LaneLine(project.TestLane{Name: "go", Command: "go\x00test"}); got != "" {
		t.Errorf("a command carrying a NUL produced a consent line: %q", got)
	}
	if got := LaneLine(project.TestLane{Name: "go", Prepare: "a\x00", Command: "go test"}); got != "" {
		t.Errorf("a prepare carrying a NUL produced a consent line: %q", got)
	}
	// Selector scoping takes the third frame, disjoint from both others.
	templated := LaneLine(project.TestLane{Name: "go", Command: "go test", SelectorTemplate: "--run {name}"})
	if !strings.HasPrefix(templated, LaneTemplateFrame) || templated == LaneLine(project.TestLane{Name: "go", Command: "go test"}) {
		t.Errorf("a templated lane did not take the template frame: %q", templated)
	}
}

// TestLaneWithNoPrepareFingerprintsItsCommandUnchanged is the compatibility
// half, and it is the assertion that keeps framing from being a breaking
// change on every machine: framing applied unconditionally would re-hash every
// lane grant already written into every local.toml, staling them all over a
// project.toml nobody edited.
func TestLaneWithNoPrepareFingerprintsItsCommandUnchanged(t *testing.T) {
	if got := LaneLine(goLane); got != goLane.Command {
		t.Fatalf("consent line = %q, want the command byte-for-byte", got)
	}

	// End to end, through the store a pre-phase grant would have written.
	store, _, repoDir := consentFixture(t)
	mustGrantLane(t, store, "go", goLane.Command)
	if got := laneState(t, store, repoDir, "go", LaneLine(goLane)); got != Granted {
		t.Errorf("a grant written before framing existed reads as %v, want granted", got)
	}
}

// TestFramedBytesCannotForgeALanesGrant: the framed encoding carries a domain
// separator, so the bytes a prepared lane hashes are not bytes a bare command
// can occupy.
//
// Without it the framing IS a command line: a lane declaring no prepare and a
// command spelled exactly like the frame would fingerprint to the value the
// prepared pair was granted, and consent would transfer between two lanes that
// share no line at all.
func TestFramedBytesCannotForgeALanesGrant(t *testing.T) {
	prepared := project.TestLane{Name: "go", Prepare: "make build", Command: "go test"}
	framed := LaneLine(prepared)
	if !strings.HasPrefix(framed, LaneFrame) {
		t.Fatalf("a prepared lane did not take the prepare frame: %q", framed)
	}

	forged := project.TestLane{Name: "go", Command: framed}
	if got := LaneLine(forged); got == framed {
		t.Fatal("a bare command spelled like the frame hashes the frame itself")
	}
	if Fingerprint(LaneLine(forged)) == Fingerprint(framed) {
		t.Error("a no-prepare lane forged the prepared pair's fingerprint")
	}

	// End to end: grant the PAIR, then ask about the forged lane. Not granted
	// is the only acceptable answer.
	store, _, repoDir := consentFixture(t)
	mustGrantLane(t, store, "go", framed)
	if got := laneState(t, store, repoDir, "go", LaneLine(forged)); got == Granted {
		t.Error("the forged lane was granted by the prepared pair's fingerprint")
	}
}

// --- install grants ---

var installLane = project.TestLane{
	Name:    "go",
	Command: "go test -count=1 ./...",
	Install: "go install honnef.co/go/tools/cmd/staticcheck@latest",
}

// TestAddingAnInstallLineDoesNotStaleTheTestGrant is locked install_consent,
// stated as the regression it forbids: a lane that already runs green must keep
// running green the moment an install line is added. Folding Install into
// LaneLine the way Prepare is folded in would refuse a gate over a line that
// has never executed.
func TestAddingAnInstallLineDoesNotStaleTheTestGrant(t *testing.T) {
	store, _, repoDir := consentFixture(t)
	// Granted over the lane's TEST line only, which is what a user who trusted
	// this lane before install lines existed has on disk.
	mustGrantLane(t, store, "go", LaneLine(installLane))

	if got := laneState(t, store, repoDir, "go", LaneLine(installLane)); got != Granted {
		t.Errorf("the test grant reads %v, want Granted — an install line staled a suite that never changed", got)
	}
	state, _ := LaneInstallConsented(store, repoDir, "go", LaneInstallLine(installLane))
	if state != Absent {
		t.Errorf("the install grant reads %v, want Absent — one edit must yield two independent answers", state)
	}
}

// TestEditedInstallLineIsStaleNotAbsent: collapsing STALE into ABSENT loses the
// only signal that says a line the user approved has since been rewritten, and
// it must not disturb the lane's test grant on the way.
func TestEditedInstallLineIsStaleNotAbsent(t *testing.T) {
	store, _, repoDir := consentFixture(t)
	mustGrantLane(t, store, "go", LaneLine(installLane))
	if err := GrantLaneInstallConsent(store, "go", LaneInstallLine(installLane)); err != nil {
		t.Fatal(err)
	}

	edited := installLane
	edited.Install = "go install honnef.co/go/tools/cmd/staticcheck@v0.5.1"

	state, err := LaneInstallConsented(store, repoDir, "go", LaneInstallLine(edited))
	if state != Stale {
		t.Errorf("an edited install line reads %v, want Stale (err %v)", state, err)
	}
	if got := laneState(t, store, repoDir, "go", LaneLine(installLane)); got != Granted {
		t.Errorf("rewriting the install line disturbed the lane's TEST grant: %v", got)
	}
}

// TestInstallFrameIsDisjointFromACommand: the two namespaces must be separate by
// construction, not by being hard to hit. A command that could spell an install
// grant's own bytes would be a lane authorizing its own installs through the
// grant issued for running its suite.
func TestInstallFrameIsDisjointFromACommand(t *testing.T) {
	lane := project.TestLane{Name: "go", Command: "go test ./...", Install: "npm i -g pnpm"}
	framed := LaneInstallLine(lane)
	if framed == "" {
		t.Fatal("the fixture's install line is un-grantable")
	}
	if !strings.HasPrefix(framed, LaneInstallFrame) {
		t.Errorf("install line did not take the install frame: %q", framed)
	}

	// The framed bytes fed back as a bare command line.
	forged := LaneLine(project.TestLane{Name: "go", Command: framed})
	if Fingerprint(forged) == Fingerprint(framed) {
		t.Error("a command spelled as the install frame hashes to the install grant")
	}
	// It is refused outright rather than merely hashing differently: the frame
	// carries a NUL, and no argv element can.
	if forged != "" {
		t.Errorf("a command carrying a NUL was accepted as a consent line: %q", forged)
	}
	// And the same install line must not collide with the lane's own test line.
	if Fingerprint(framed) == Fingerprint(LaneLine(lane)) {
		t.Error("a lane's install line and its command line hash the same")
	}
}

// TestInstallLineWithANulIsUngrantable mirrors LaneLine's guard. An argv
// element is NUL-terminated, so a line carrying one can never be exec'd under
// any shell — binding consent to it would bind to something that can never run.
func TestInstallLineWithANulIsUngrantable(t *testing.T) {
	store, _, repoDir := consentFixture(t)
	lane := project.TestLane{Name: "go", Command: "go test ./...", Install: "npm i\x00 -g pnpm"}

	line := LaneInstallLine(lane)
	if line != "" {
		t.Fatalf("an install line carrying a NUL produced a consent line: %q", line)
	}
	state, err := LaneInstallConsented(store, repoDir, "go", line)
	if state != NotApplicable {
		t.Errorf("an un-grantable install reads %v, want NotApplicable", state)
	}
	if err == nil || !strings.Contains(err.Error(), "install") {
		t.Errorf("the refusal does not name the field: %v", err)
	}

	// A lane declaring nothing at all takes the same arm, and says so with the
	// install-specific sentinel rather than the command one.
	if got := LaneInstallLine(docsLane); got != "" {
		t.Errorf("a lane with no install line produced a consent line: %q", got)
	}
}

// TestInstallGrantsAreIsolatedPerLane: a grant that replaced the store would
// make trusting one lane's install revoke another's, so the user would
// re-consent to everything every time they touched anything.
func TestInstallGrantsAreIsolatedPerLane(t *testing.T) {
	store, _, _ := consentFixture(t)
	if err := GrantLaneInstallConsent(store, "docs", "docs-line"); err != nil {
		t.Fatal(err)
	}
	before, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	docsBefore := before.TrustedLaneInstalls["docs"]

	if err := GrantLaneInstallConsent(store, "go", LaneInstallLine(installLane)); err != nil {
		t.Fatal(err)
	}

	after, err := store.Load()
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
	if err := RevokeLaneInstallConsent(store, "go"); err != nil {
		t.Fatal(err)
	}
	g, _ := store.Load()
	if _, ok := g.TrustedLaneInstalls["go"]; ok {
		t.Error("the revoked grant is still there")
	}
	if g.TrustedLaneInstalls["docs"] != docsBefore {
		t.Error("revoking go's install grant disturbed docs'")
	}
	if err := RevokeLaneInstallConsent(store, "docs"); err != nil {
		t.Fatal(err)
	}
	g, _ = store.Load()
	if g.TrustedLaneInstalls != nil {
		t.Errorf("the emptied map is %#v, want nil so omitempty drops the table", g.TrustedLaneInstalls)
	}
	// A missing entry is not an error.
	if err := RevokeLaneInstallConsent(store, "never-granted"); err != nil {
		t.Errorf("revoking a grant that was never there errored: %v", err)
	}
}

// TestRevokeLaneConsentDropsBothGrants: removing a lane drops its command AND
// install grants at one site, so a re-added lane under the name inherits
// neither — including a lane that held an install grant and no command grant.
func TestRevokeLaneConsentDropsBothGrants(t *testing.T) {
	store, _, _ := consentFixture(t)
	if err := GrantLaneInstallConsent(store, "go", LaneInstallLine(installLane)); err != nil {
		t.Fatal(err)
	}
	if err := RevokeLaneConsent(store, "go"); err != nil {
		t.Fatal(err)
	}
	g, _ := store.Load()
	if _, ok := g.TrustedLaneInstalls["go"]; ok {
		t.Error("an install-only grant survived RevokeLaneConsent")
	}
}

// TestLaneRefusalsNameTheLane: every refusal arm names the lane and shows the
// exact lines, and the tracked/not-applicable arms pass the error through.
func TestLaneRefusalsNameTheLane(t *testing.T) {
	lane := project.TestLane{Name: "go", Prepare: "make build", Command: "go test ./...", Install: "npm i -g pnpm"}
	if err := LaneRefusal(lane, Refused, ErrNoConsent); err != ErrNoConsent {
		t.Errorf("Refused must pass the error through, got %v", err)
	}
	stale := LaneRefusal(lane, Stale, ErrStaleConsent)
	for _, want := range []string{`"go"`, "prepare: make build", "command: go test ./...", "CHANGED", "dross trust --lane go"} {
		if !strings.Contains(stale.Error(), want) {
			t.Errorf("stale lane refusal lacks %q: %v", want, stale)
		}
	}
	absent := LaneRefusal(lane, Absent, ErrNoConsent)
	if !strings.Contains(absent.Error(), "has not been trusted") || !strings.Contains(absent.Error(), "dross trust --lane go") {
		t.Errorf("absent lane refusal = %v", absent)
	}
	istale := LaneInstallRefusal(lane, Stale, ErrStaleConsent)
	for _, want := range []string{`"go"`, "npm i -g pnpm", "CHANGED", "dross trust --lane-install go"} {
		if !strings.Contains(istale.Error(), want) {
			t.Errorf("stale install refusal lacks %q: %v", want, istale)
		}
	}
	if strings.Contains(istale.Error(), "dross trust --lane go") {
		t.Error("the install refusal points at the test-lane verb, which would grant the wrong thing")
	}
	iabsent := LaneInstallRefusal(lane, Absent, ErrNoConsent)
	if !strings.Contains(iabsent.Error(), "changes a machine") || !strings.Contains(iabsent.Error(), "dross trust --lane-install go") {
		t.Errorf("absent install refusal = %v", iabsent)
	}
}

// --- vouch and relate ---

// vouchLane is the shape every vouch assertion starts from, with the vouch bit
// on. Flipping it off gives the same lines under the form a grant from before
// the lane could vouch was taken over.
var vouchLane = project.TestLane{
	Name:    "web",
	Prepare: "npm ci",
	Command: "npx vitest run",
	Vouch:   true,
	Relate:  "scripts/relate.sh",
}

// assertStaleUntilTrust pins one edit's whole ladder: the old grant reads Stale,
// never Absent, because a rewritten grant and a never-trusted lane call for
// different reactions, and a fresh grant over the new line is what clears it.
func assertStaleUntilTrust(t *testing.T, granted, edited project.TestLane) {
	t.Helper()
	store, _, repoDir := consentFixture(t)
	mustGrantLane(t, store, granted.Name, LaneLine(granted))

	state, err := LaneConsented(store, repoDir, edited.Name, LaneLine(edited))
	if state != Stale {
		t.Fatalf("the edited lane reads %v, want Stale (err %v)", state, err)
	}
	if err != ErrStaleConsent {
		t.Errorf("err = %v, want ErrStaleConsent", err)
	}
	mustGrantLane(t, store, edited.Name, LaneLine(edited))
	if got := laneState(t, store, repoDir, edited.Name, LaneLine(edited)); got != Granted {
		t.Errorf("after a fresh grant the lane reads %v, want Granted", got)
	}
}

// TestVouchFlipStalesTheGrant: a grant taken while the lane could only run tests
// must not carry over to a lane that can admit commits. It covers every form a
// grant could have been taken under, because each is a separate branch of
// LaneLine a flip has to leave.
func TestVouchFlipStalesTheGrant(t *testing.T) {
	for name, lane := range map[string]project.TestLane{
		"plain":     {Name: "web", Command: "npx vitest run"},
		"prepared":  {Name: "web", Prepare: "npm ci", Command: "npx vitest run"},
		"templated": {Name: "web", Command: "npx vitest run", SelectorTemplate: "--dir {path}"},
		"related":   {Name: "web", Command: "npx vitest run", Relate: "scripts/relate.sh"},
	} {
		t.Run(name, func(t *testing.T) {
			flipped := lane
			flipped.Vouch = true
			assertStaleUntilTrust(t, lane, flipped)
		})
	}
}

// TestRelateEditStalesTheGrant: relate decides which paths count as covered, so
// a rewritten relate on a granted lane is a rewritten admission rule.
func TestRelateEditStalesTheGrant(t *testing.T) {
	edited := vouchLane
	edited.Relate = "scripts/relate.sh --all"
	assertStaleUntilTrust(t, vouchLane, edited)
}

// TestVouchFrameBindsEveryLine: the fourth form replaces the other three for a
// vouching lane, so any line it failed to bind would be editable under a live
// grant.
func TestVouchFrameBindsEveryLine(t *testing.T) {
	for name, edit := range map[string]func(*project.TestLane){
		"prepare":  func(l *project.TestLane) { l.Prepare = "npm i" },
		"command":  func(l *project.TestLane) { l.Command = "npx vitest" },
		"template": func(l *project.TestLane) { l.SelectorTemplate = "--dir {path}" },
		"join":     func(l *project.TestLane) { l.SelectorJoin = "," },
	} {
		t.Run(name, func(t *testing.T) {
			edited := vouchLane
			edit(&edited)
			assertStaleUntilTrust(t, vouchLane, edited)
		})
	}
}

// TestVouchFrameIsLengthFramed: the frame is the only thing separating relate
// from its neighbours, so characters moved across a field boundary must change
// the line, and the framed bytes must not be spellable as a bare command.
func TestVouchFrameIsLengthFramed(t *testing.T) {
	framed := LaneLine(vouchLane)
	if !strings.HasPrefix(framed, LaneVouchFrame) {
		t.Fatalf("a vouching lane did not take the vouch frame: %q", framed)
	}
	const want = LaneVouchFrame + "6\x00npm ci\x0014\x00npx vitest run\x000\x00\x000\x00\x0017\x00scripts/relate.sh"
	if framed != want {
		t.Errorf("framed line = %q, want %q", framed, want)
	}

	left := project.TestLane{Name: "web", Command: "a", SelectorJoin: "b", Vouch: true, Relate: "c"}
	right := project.TestLane{Name: "web", Command: "a", SelectorJoin: "bc", Vouch: true}
	if LaneLine(left) == LaneLine(right) {
		t.Errorf("a re-split across join and relate produced one consent line: %q", LaneLine(left))
	}

	if got := LaneLine(project.TestLane{Name: "web", Command: framed}); got != "" {
		t.Errorf("a bare command spelled like the frame produced a consent line: %q", got)
	}
}

// TestNonVouchingLaneLineIsUnchanged is the compatibility half: every grant on
// every machine was taken over one of these forms. The wants are literals rather
// than the frame constants composed by hand, so a rewrite of either form fails
// here, and a relate on a lane that does not vouch runs nothing and moves nothing.
func TestNonVouchingLaneLineIsUnchanged(t *testing.T) {
	cases := []struct {
		name string
		lane project.TestLane
		want string
	}{
		{"plain", project.TestLane{Command: "go test ./..."}, "go test ./..."},
		{
			"prepared",
			project.TestLane{Prepare: "make build", Command: "go test"},
			"dross:lane-prepare:v1\x0010\x00make build\x007\x00go test",
		},
		{
			"templated",
			project.TestLane{Prepare: "make", Command: "go test", SelectorTemplate: "-run {path}", SelectorJoin: "|"},
			"dross:lane-template:v1\x004\x00make\x007\x00go test\x0011\x00-run {path}\x001\x00|",
		},
		{"plain with relate", project.TestLane{Command: "go test ./...", Relate: "scripts/relate.sh"}, "go test ./..."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := LaneLine(c.lane); got != c.want {
				t.Errorf("LaneLine = %q, want %q", got, c.want)
			}
		})
	}
}

// TestRelateCarryingANulIsUnrunnable mirrors the command and prepare guards: an
// argv element cannot hold a NUL, so the lane is unrunnable and takes the
// NotApplicable arm rather than a grant bound to a line that can never spawn.
func TestRelateCarryingANulIsUnrunnable(t *testing.T) {
	store, _, repoDir := consentFixture(t)
	lane := vouchLane
	lane.Relate = "scripts/relate.sh\x00--all"

	line := LaneLine(lane)
	if line != "" {
		t.Fatalf("a relate carrying a NUL produced a consent line: %q", line)
	}
	state, err := LaneConsented(store, repoDir, lane.Name, line)
	if state != NotApplicable {
		t.Errorf("state = %v, want NotApplicable (err %v)", state, err)
	}
}

// TestVouchingLaneRefusalShowsRelate: both arms send the reader to `dross trust
// --lane`, which grants relate too, so neither may ask for a line it hides.
func TestVouchingLaneRefusalShowsRelate(t *testing.T) {
	for name, arm := range map[string]struct {
		state State
		cerr  error
		says  string
	}{
		"stale":     {Stale, ErrStaleConsent, "its lines or vouch setting has CHANGED"},
		"untrusted": {Absent, ErrNoConsent, "its lines or vouch setting has not been trusted"},
	} {
		t.Run(name, func(t *testing.T) {
			msg := LaneRefusal(vouchLane, arm.state, arm.cerr).Error()
			for _, want := range []string{
				arm.says,
				"    prepare: npm ci\n",
				"    command: npx vitest run\n",
				"    relate: scripts/relate.sh\n",
				"may admit commits",
				"dross trust --lane web",
			} {
				if !strings.Contains(msg, want) {
					t.Errorf("refusal lacks %q:\n%s", want, msg)
				}
			}
		})
	}

	// A vouching lane with no prepare has no line to invent.
	bare := vouchLane
	bare.Prepare = ""
	if msg := LaneRefusal(bare, Stale, ErrStaleConsent).Error(); strings.Contains(msg, "prepare:") {
		t.Errorf("a lane with no prepare printed one:\n%s", msg)
	}
}

// TestNonVouchingRefusalIsUnchanged: the wording of a lane that cannot admit a
// commit stays as it was, including one carrying a relate nothing runs.
func TestNonVouchingRefusalIsUnchanged(t *testing.T) {
	lane := project.TestLane{Name: "go", Command: "go test ./...", Relate: "scripts/relate.sh"}
	msg := LaneRefusal(lane, Stale, ErrStaleConsent).Error()
	if !strings.Contains(msg, "its command has CHANGED") || !strings.Contains(msg, "    go test ./...\n") {
		t.Errorf("the plain refusal moved:\n%s", msg)
	}
	if strings.Contains(msg, "relate") || strings.Contains(msg, "admit") {
		t.Errorf("a lane that does not vouch was described as one that does:\n%s", msg)
	}
}

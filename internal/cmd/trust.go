package cmd

// `dross trust` and the exec-consent gate. The store's semantics —
// fingerprint binding, the consent states, the tracked-store refusal — live in
// internal/consent; this file keeps the cobra command, the per-lane grants
// (t-5 moves them) and requireExecConsent, the refusal every gated RunE runs
// first.

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/changes"
	"github.com/Rivil/dross/internal/consent"
	"github.com/Rivil/dross/internal/project"
)

// ConsentState and the five Consent* constants are the in-package names for
// consent.State: the lane grants below and the doctor/verify wiring still
// speak them, and the alias keeps every switch literal compiling.
type ConsentState = consent.State

const (
	ConsentRefused       = consent.Refused
	ConsentNotApplicable = consent.NotApplicable
	ConsentAbsent        = consent.Absent
	ConsentStale         = consent.Stale
	ConsentGranted       = consent.Granted
)

// The sentinels are consent's, re-exported by identity so errors.Is matches
// across the boundary.
var (
	ErrNoConsent       = consent.ErrNoConsent
	ErrStaleConsent    = consent.ErrStaleConsent
	ErrNoTestCommand   = consent.ErrNoTestCommand
	ErrNoLaneCommand   = consent.ErrNoLaneCommand
	ErrNoLaneInstall   = consent.ErrNoLaneInstall
	ErrNoReplayConsent = consent.ErrNoReplayConsent
)

// --- per-lane consent ---

// laneFrame is the domain separator that keeps a prepared lane's consent line
// out of the namespace a bare command occupies.
//
// A NUL cannot appear in an argv element, so no command line a user could
// actually run can spell this prefix — which is what makes the "no pair can
// forge a no-prepare lane's fingerprint" claim structural rather than
// probabilistic.
const laneFrame = "dross:lane-prepare:v1\x00"

// laneTemplateFrame is the THIRD namespace, for a lane that declares selector
// scoping of its own.
//
// It is disjoint from laneFrame and from the unframed namespace by
// construction, not by being hard to hit: no unframed line may carry a NUL, and
// the two framed prefixes differ in their own text, so no byte string can be
// read as belonging to two of the three. That is what keeps a templated lane
// from running under a grant issued for a prepared one carrying the same two
// command lines.
const laneTemplateFrame = "dross:lane-template:v1\x00"

// laneConsentLine returns the exact byte string ONE lane's consent grant is
// taken over — the value Fingerprint hashes and the store records.
//
// Three properties, each load-bearing:
//
//   - A lane declaring NO prepare returns its command UNCHANGED. Framing
//     applied unconditionally would re-fingerprint every lane grant already
//     written on every machine, staling them all over a document nobody
//     edited.
//   - A prepared lane's two lines are LENGTH-FRAMED, not concatenated. Naive
//     concatenation collides {prepare:"a", command:"bc"} with {prepare:"ab",
//     command:"c"} — a lane re-split across its two fields keeping a grant
//     issued for neither.
//   - The framed form is NUL-delimited and no unframed line may carry a NUL,
//     so feeding those bytes back as a bare command misses rather than forging
//     the pair's own fingerprint. The namespaces are disjoint by construction,
//     not by being hard to hit.
//   - A lane declaring selector scoping of its own takes a THIRD form under
//     laneTemplateFrame, binding selector_template and selector_join alongside
//     the two command lines. Adding or changing either therefore stales the
//     grant rather than letting an unread line run under one issued before it —
//     the template is arbitrary user text on the spawned line, fenced by
//     consent exactly as command and prepare are.
//
// A lane with no command returns the empty string whatever its prepare says:
// consent binds to something runnable, and LaneConsented's ConsentNotApplicable
// arm is keyed on exactly that emptiness.
func laneConsentLine(lane project.TestLane) string {
	if strings.TrimSpace(lane.Command) == "" {
		return ""
	}
	// A NUL is what makes the frame unforgeable, and it only works if no
	// UNFRAMED line can carry one: otherwise a lane declaring no prepare and a
	// command spelled exactly like the frame would hash the frame itself, and
	// the grant would transfer between two lanes sharing no line at all.
	//
	// Refusing costs nothing real. An argv element is NUL-terminated, so a
	// command containing one cannot be exec'd under any shell — the lane is
	// unrunnable, and binding consent to it would be binding to something that
	// can never run. Empty here means exactly that, and LaneConsented turns it
	// into the same ConsentNotApplicable a commandless lane gets.
	if strings.ContainsRune(lane.Command, 0) || strings.ContainsRune(lane.Prepare, 0) ||
		strings.ContainsRune(lane.SelectorTemplate, 0) || strings.ContainsRune(lane.SelectorJoin, 0) {
		return ""
	}
	// Gated on either scoping field being declared, not on the template alone:
	// a join carries user text into the run just as a template does, so a lane
	// hand-edited to carry one must not ride a grant taken before it. Neither
	// field existed before this phase, so no grant already written anywhere is
	// disturbed by framing that shape.
	if lane.SelectorTemplate != "" || lane.SelectorJoin != "" {
		return fmt.Sprintf("%s%d\x00%s\x00%d\x00%s\x00%d\x00%s\x00%d\x00%s",
			laneTemplateFrame,
			len(lane.Prepare), lane.Prepare,
			len(lane.Command), lane.Command,
			len(lane.SelectorTemplate), lane.SelectorTemplate,
			len(lane.SelectorJoin), lane.SelectorJoin)
	}
	if lane.Prepare == "" {
		return lane.Command
	}
	return fmt.Sprintf("%s%d\x00%s\x00%d\x00%s", laneFrame, len(lane.Prepare), lane.Prepare, len(lane.Command), lane.Command)
}

// LaneConsented reports what this machine has said about ONE lane's command.
//
// It returns the same ConsentState ladder CheckConsent does, and for the same
// reason: "you have never trusted this lane" and "this lane's command has
// changed since you trusted it" are different situations calling for different
// reactions, and collapsing them reports a rewritten command as a routine first
// run. The states are per lane, so a repo where one lane went stale still has
// four granted lanes and the refusal says which one to look at.
//
// repoDir travels alongside root, exactly as it does for CheckConsent, so the
// tracked-store refusal is shared rather than restated: a committed local.toml
// is a repo authorizing its own lane commands, and it is refused UNREAD.
func LaneConsented(root, repoDir, name, line string) (ConsentState, error) {
	if err := consent.RefuseTrackedLocal(repoDir); err != nil {
		return ConsentRefused, err
	}
	if strings.TrimSpace(line) == "" {
		return ConsentNotApplicable, fmt.Errorf("%w: %s", ErrNoLaneCommand, name)
	}
	l, err := loadLocal(localPath(root))
	if err != nil {
		// An unparseable store is not consent. Fail closed, and say why.
		return ConsentAbsent, fmt.Errorf("%w: %v", ErrNoConsent, err)
	}
	got, ok := l.TrustedLaneCommands[name]
	if !ok || got == "" {
		return ConsentAbsent, ErrNoConsent
	}
	if got != consent.Fingerprint(line) {
		return ConsentStale, ErrStaleConsent
	}
	return ConsentGranted, nil
}

// GrantLaneConsent records consent for one lane, leaving every other lane's
// grant exactly as it was.
//
// Per lane is the whole point of the map (see localStore.TrustedLaneCommands):
// a grant that replaced the store would make trusting the docs lane revoke the
// Go lane, so the user would re-consent to everything every time they touched
// anything.
func GrantLaneConsent(root, name, line string) error {
	path := localPath(root)
	l, err := loadLocal(path)
	if err != nil {
		return err
	}
	if l.TrustedLaneCommands == nil {
		l.TrustedLaneCommands = map[string]string{}
	}
	l.TrustedLaneCommands[name] = consent.Fingerprint(line)
	return l.save(path)
}

// RevokeLaneConsent drops one lane's grant. Removing a lane calls it, so a
// name that is later re-added starts ungranted rather than inheriting a
// fingerprint issued for whatever the deleted lane used to run.
//
// A missing entry is not an error: the caller is expressing "this lane has no
// grant", and that is already true.
func RevokeLaneConsent(root, name string) error {
	path := localPath(root)
	l, err := loadLocal(path)
	if err != nil {
		return err
	}
	// BOTH grants, at this one site. A lane's name keys two stores now, and a
	// removal that dropped only the command grant would leave an install grant
	// behind under a name nothing declares — until someone re-added a lane
	// under it, which would then start authorized to install whatever the
	// deleted lane's line said.
	//
	// Checked as a pair rather than short-circuiting on the command grant: a
	// lane may hold an install grant and no command grant at all, and an
	// early return on the command map would skip it.
	_, hadCommand := l.TrustedLaneCommands[name]
	_, hadInstall := l.TrustedLaneInstalls[name]
	if !hadCommand && !hadInstall {
		return nil
	}
	delete(l.TrustedLaneCommands, name)
	delete(l.TrustedLaneInstalls, name)
	if len(l.TrustedLaneCommands) == 0 {
		// Back to nil so omitempty keeps an empty table out of the file —
		// a bare [trusted_lane_commands] header reads as a store that holds
		// something.
		l.TrustedLaneCommands = nil
	}
	if len(l.TrustedLaneInstalls) == 0 {
		l.TrustedLaneInstalls = nil
	}
	return l.save(path)
}

// --- per-lane INSTALL consent ---

// laneInstallFrame is the domain separator for an install grant.
//
// Its own frame, disjoint from laneFrame and from the unframed namespace a bare
// command occupies, so no command line a user could actually run can spell it —
// a NUL cannot appear in an argv element. The grants live in separate maps, so
// this is not the only thing keeping them apart; it is what makes "a command
// cannot forge an install grant" structural rather than a property of how the
// store happens to be keyed today.
const laneInstallFrame = "dross:lane-install:v1\x00"

// laneInstallConsentLine returns the exact byte string ONE lane's INSTALL grant
// is taken over.
//
// Length-framed like laneConsentLine's prepared form rather than bare, so the
// frame stays unambiguous if a second field is ever bound into it — a naive
// join is what lets two fields re-split and keep a grant issued for neither.
//
// Empty means un-grantable, and there are two ways to be: a lane declaring no
// install line has nothing to consent to, and a line carrying a NUL can never
// be exec'd under any shell, so binding consent to it would bind to something
// that can never run. LaneInstallConsented turns both into
// ConsentNotApplicable.
func laneInstallConsentLine(lane project.TestLane) string {
	if strings.TrimSpace(lane.Install) == "" {
		return ""
	}
	if strings.ContainsRune(lane.Install, 0) {
		return ""
	}
	return fmt.Sprintf("%s%d\x00%s", laneInstallFrame, len(lane.Install), lane.Install)
}

// LaneInstallConsented reports what this machine has said about ONE lane's
// install line.
//
// It answers INDEPENDENTLY of LaneConsented, which is the whole point of the
// separate store (locked install_consent): adding an install line to a lane
// that already runs green must leave its test grant reading Granted while this
// reports Absent, so one edit yields two answers rather than one refusal of
// something that never changed.
//
// Same ladder, same tracked-store refusal, for the reasons LaneConsented has
// them: a rewritten install line and a never-trusted one need different
// reactions, and a committed local.toml is a repo authorizing its own install
// commands.
func LaneInstallConsented(root, repoDir, name, line string) (ConsentState, error) {
	if err := consent.RefuseTrackedLocal(repoDir); err != nil {
		return ConsentRefused, err
	}
	if strings.TrimSpace(line) == "" {
		return ConsentNotApplicable, fmt.Errorf("%w: %s", ErrNoLaneInstall, name)
	}
	l, err := loadLocal(localPath(root))
	if err != nil {
		// An unparseable store is not consent. Fail closed, and say why.
		return ConsentAbsent, fmt.Errorf("%w: %v", ErrNoConsent, err)
	}
	got, ok := l.TrustedLaneInstalls[name]
	if !ok || got == "" {
		return ConsentAbsent, ErrNoConsent
	}
	if got != consent.Fingerprint(line) {
		return ConsentStale, ErrStaleConsent
	}
	return ConsentGranted, nil
}

// GrantLaneInstallConsent records install consent for one lane, leaving every
// other lane's install grant — and every lane's TEST grant, this one included —
// exactly as it was.
func GrantLaneInstallConsent(root, name, line string) error {
	path := localPath(root)
	l, err := loadLocal(path)
	if err != nil {
		return err
	}
	if l.TrustedLaneInstalls == nil {
		l.TrustedLaneInstalls = map[string]string{}
	}
	l.TrustedLaneInstalls[name] = consent.Fingerprint(line)
	return l.save(path)
}

// RevokeLaneInstallConsent drops one lane's install grant.
//
// A missing entry is not an error, on RevokeLaneConsent's precedent: the caller
// is expressing "this lane has no install grant", and that is already true.
func RevokeLaneInstallConsent(root, name string) error {
	path := localPath(root)
	l, err := loadLocal(path)
	if err != nil {
		return err
	}
	if _, ok := l.TrustedLaneInstalls[name]; !ok {
		return nil
	}
	delete(l.TrustedLaneInstalls, name)
	if len(l.TrustedLaneInstalls) == 0 {
		// Back to nil so omitempty keeps an empty table out of the file — a
		// bare [trusted_lane_installs] header reads as a store that holds
		// something.
		l.TrustedLaneInstalls = nil
	}
	return l.save(path)
}

// laneConsentRefusal turns one lane's consent state into the message the user
// acts on, naming the lane and the exact line in every arm.
//
// The lane name is in the text of every arm on purpose: a run refusing over
// several lanes produces several of these, and a message that only showed the
// command would leave the reader matching command lines back to blocks by eye.
func laneConsentRefusal(lane project.TestLane, state ConsentState, cerr error) error {
	name := lane.Name
	// EVERY line the grant covers, not just the command. One grant binds both,
	// so a refusal showing only the command would let a user re-consent to a
	// bootstrap they were never shown — and when the prepare is the line that
	// changed, the message would display text that did not change while asking
	// them to approve text they cannot see.
	lines, what := "    "+lane.Command+"\n", "command"
	if lane.Prepare != "" {
		lines = "    prepare: " + lane.Prepare + "\n    command: " + lane.Command + "\n"
		what = "prepare or command"
	}
	switch state {
	case ConsentRefused, ConsentNotApplicable:
		return cerr
	case ConsentStale:
		return fmt.Errorf(
			"refusing to run test lane %q: its %s has CHANGED since you trusted it —\n"+
				"the recorded consent is stale.\n\n"+
				"%s\n"+
				"Read the lines above; if that is what you meant to run, re-consent:\n\n"+
				"    dross trust --lane %s\n\n%w", name, what, lines, name, cerr)
	default:
		return fmt.Errorf(
			"refusing to run test lane %q: its %s has not been trusted on this machine.\n\n"+
				"%s\n"+
				"It comes from the repo's tracked project.toml, so a clone carries whatever\n"+
				"its author wrote. Read the lines above, then:\n\n"+
				"    dross trust --lane %s\n\n%w", name, what, lines, name, cerr)
	}
}

// findLane returns the named lane, or an error listing the lanes that do exist.
//
// Listing them is what makes a typo self-correcting: the alternative is
// "unknown lane", which sends the user to open project.toml to find out what
// they should have typed.
// trustLaneInstall is `dross trust --lane-install <name>`.
//
// A separate verb from --lane, not a widening of it, because the two grants
// answer different questions (locked install_consent). Granting the right to
// run a suite must not silently grant the right to change the machine it runs
// on, and a single verb covering both would make that grant invisible.
func trustLaneInstall(root, name string, check bool) error {
	proj, err := project.Load(filepath.Join(root, project.File))
	if err != nil {
		return err
	}
	lane, err := findLane(proj, name)
	if err != nil {
		return err
	}
	repoDir := filepath.Dir(root)
	if check {
		state, cerr := LaneInstallConsented(root, repoDir, lane.Name, laneInstallConsentLine(lane))
		if cerr == nil {
			return nil
		}
		return laneInstallRefusal(lane, state, cerr)
	}
	if err := consent.RefuseTrackedLocal(repoDir); err != nil {
		return err
	}
	if strings.TrimSpace(lane.Install) == "" {
		return fmt.Errorf(
			"nothing to trust: test lane %q declares no install line.\n\n"+
				"Consent is bound to a line, so there is nothing to bind to yet. dross's\n"+
				"own built-in install recipes need no grant — they are not lines this repo\n"+
				"supplied. Add one with `dross test lane edit %s --install \"<cmd>\"`", name, name)
	}
	// Printed BEFORE the write, and in full. The line arrives from TRACKED
	// project.toml, so a grant that did not show it would be consenting to
	// whatever a clone happened to carry — and this one changes a machine.
	Printf("trusting test lane %q's install line on this machine:\n\n    %s\n\n", lane.Name, lane.Install)
	if err := GrantLaneInstallConsent(root, lane.Name, laneInstallConsentLine(lane)); err != nil {
		return err
	}
	Printf("recorded in %s/%s (gitignored — it does not travel with the repo).\n", RootDirName, LocalFile)
	Printf("This is separate from `dross trust --lane %s`: it authorizes INSTALLING lane %q's\n", lane.Name, lane.Name)
	Print("toolchain, not running its suite. Editing or renaming the lane revokes it.")
	return nil
}

// laneInstallRefusal turns one lane's INSTALL consent state into the message
// the user acts on.
//
// Separate wording from laneConsentRefusal throughout, deliberately: a user
// refused here has not been refused a test run, and a message reading like one
// would send them to `dross trust --lane` — which would grant the wrong thing
// and leave them refused again with no idea why.
func laneInstallRefusal(lane project.TestLane, state ConsentState, cerr error) error {
	name := lane.Name
	line := "    " + lane.Install + "\n"
	switch state {
	case ConsentRefused, ConsentNotApplicable:
		return cerr
	case ConsentStale:
		return fmt.Errorf(
			"refusing to install test lane %q's toolchain: its install line has CHANGED\n"+
				"since you trusted it — the recorded consent is stale.\n\n"+
				"%s\n"+
				"Read the line above; if that is what you meant to run, re-consent:\n\n"+
				"    dross trust --lane-install %s\n\n%w", name, line, name, cerr)
	default:
		return fmt.Errorf(
			"refusing to install test lane %q's toolchain: its install line has not been\n"+
				"trusted on this machine.\n\n"+
				"%s\n"+
				"It comes from the repo's tracked project.toml, so a clone carries whatever\n"+
				"its author wrote, and this line changes a machine rather than measuring this\n"+
				"repo. Read it above, then:\n\n"+
				"    dross trust --lane-install %s\n\n%w", name, line, name, cerr)
	}
}

func findLane(p *project.Project, name string) (project.TestLane, error) {
	var names []string
	for _, lane := range p.Runtime.TestLane {
		if lane.Name == name {
			return lane, nil
		}
		names = append(names, lane.Name)
	}
	if len(names) == 0 {
		return project.TestLane{}, fmt.Errorf("unknown test lane %q: this repo declares none.\n\n"+
			"Declare one with `dross test lane add <name> --match <glob> --command \"<cmd>\"`", name)
	}
	return project.TestLane{}, fmt.Errorf("unknown test lane %q; declared: %s", name, strings.Join(names, ", "))
}

// --- the gate ---

// execGatedCommands is the roster of commands whose RunE calls
// requireExecConsent, declared here rather than inferred from the call sites so
// TestExecGatedSetIsExplicit can assert the two agree — a command cannot
// silently join or leave it.
//
// It is NOT the description of the gated surface, and reading it as one is what
// let `survivor drain` shell `go list` and `go test -coverprofile` over an
// untrusted repo for a whole milestone. The surface is DERIVED:
// TestEverySpawnSiteGatedOrExempt reads every os/exec construction out of
// internal/ and cmd/, follows the call graph to the commands that reach each
// one, and requires it to be reachable only from gating commands or to carry a
// //dross:exec-exempt marker saying why it cannot reach repo-authored code. A
// spawn added tomorrow is in scope the day it is written, with nothing here to
// keep in step.
//
// What this roster still answers is which commands REFUSE, and where. It is the
// loop's chokepoints, not "everything that touches a phase". Two boundaries are
// deliberate:
//
//   - Read-only and post-hoc commands stay out. `dross status`, `dross doctor`
//     and `task status … done` must keep working in an untrusted tree, or the
//     gate bricks the very commands a user reaches for to understand why dross
//     is refusing. A gate that makes diagnosis impossible gets disabled.
//
//   - The commands that spawn the repo's own suite are in it: `verify` and
//     `test` run it directly, and `survivor drain` shells `go list` and `go
//     test -coverprofile` over every package. Gating those is gating the run
//     rather than the step boundary around it. The drain rides
//     runtime.test_command's grant per the locked drain_grant decision —
//     consenting to "dross may run this repo's suite here" is the same
//     permission the drain needs, and a separate grant family would cost
//     another trust surface for no additional authority.
//
//   - The rest are in it as the loop's STEP BOUNDARIES: refusing there stops
//     an execute run before it reaches the step that runs tests. The locked
//     exec_consent_gate decision admits what this cannot do — nothing stops an
//     agent typing `go test` directly. The CLI covers the CLI.
var execGatedCommands = []string{
	"changes record",
	"state bump",
	"survivor drain",
	"task next",
	"task status in_progress",
	"test",
	"verify",
	"verify results",
}

// requireExecConsent is the refusal a gated command runs FIRST, before any I/O.
//
// Ordering is the guarantee, not a nicety: a refusal that had already written
// tests.json or verify.toml would have done the work it was refusing to
// authorize. Every gated call site puts this at the top of its RunE.
func requireExecConsent() error {
	root, err := FindRoot()
	if err != nil {
		return err
	}
	proj, err := project.Load(filepath.Join(root, project.File))
	if err != nil {
		return err
	}
	testCmd := proj.Runtime.TestCommand
	state, cerr := consent.CheckConsent(grantStore(root), filepath.Dir(root), testCmd)
	if cerr == nil {
		return nil
	}
	return consent.Refusal(state, cerr, testCmd)
}

// --- the command ---

// Trust registers `dross trust`.
func Trust() *cobra.Command {
	var check bool
	var replayPhase string
	var runSlotName string
	var laneName string
	var laneInstallName string
	c := &cobra.Command{
		Use:   "trust",
		Short: "Consent to dross running this repo's runtime.test_command on this machine",
		Long: "Records consent for this repo's runtime.test_command in the gitignored\n" +
			".dross/local.toml, as a hash of the command. A clone carries no consent, and\n" +
			"editing the command revokes it — see `dross doctor` for the current state.\n" +
			"--replay <phase-id> grants the same consent for a phase's recorded red-proof\n" +
			"replay command instead, which a repoint re-runs at the commit it proposes.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			if replayPhase != "" {
				return trustReplay(root, replayPhase, check)
			}
			if runSlotName != "" {
				return trustRun(root, runSlotName, check)
			}
			if laneName != "" {
				return trustLane(root, laneName, check)
			}
			if laneInstallName != "" {
				return trustLaneInstall(root, laneInstallName, check)
			}
			proj, err := project.Load(filepath.Join(root, project.File))
			if err != nil {
				return err
			}
			testCmd := proj.Runtime.TestCommand
			repoDir := filepath.Dir(root)

			if check {
				// The silent form prompts pre-flight with. Success prints
				// NOTHING — a prompt that had to parse output around it would
				// find a way not to run it.
				state, cerr := consent.CheckConsent(grantStore(root), repoDir, testCmd)
				if cerr == nil {
					return nil
				}
				return consent.Refusal(state, cerr, testCmd)
			}

			if err := consent.RefuseTrackedLocal(repoDir); err != nil {
				return err
			}
			if testCmd == "" {
				return fmt.Errorf(
					"nothing to trust: runtime.test_command is not set.\n\n" +
						"Set it first with `dross project set runtime.test_command \"<cmd>\"`,\n" +
						"then run `dross trust` again — consent is bound to the command, so\n" +
						"there is nothing to bind to yet")
			}
			// Printed BEFORE the write, and printed in full. The command is the
			// thing being consented to; a grant that did not show it would be a
			// rubber stamp on a line nobody read.
			Printf("trusting this repo's test command on this machine:\n\n    %s\n\n", testCmd)
			if err := consent.GrantConsent(grantStore(root), testCmd); err != nil {
				return err
			}
			Printf("recorded in %s/%s (gitignored — it does not travel with the repo).\n", RootDirName, LocalFile)
			Print("Editing runtime.test_command revokes this; dross will ask again.")
			return nil
		},
	}
	c.Flags().BoolVar(&check, "check", false, "exit 0 if consent is current, non-zero otherwise; prints nothing on success")
	c.Flags().StringVar(&replayPhase, "replay", "", "grant consent for <phase-id>'s recorded red-proof replay command instead of runtime.test_command")
	c.Flags().StringVar(&runSlotName, "run", "", "grant consent for `dross run <name>`'s configured command instead of runtime.test_command")
	c.Flags().StringVar(&laneName, "lane", "", "grant consent for the named [[runtime.test_lane]]'s command instead of runtime.test_command")
	c.Flags().StringVar(&laneInstallName, "lane-install", "", "grant consent for the named [[runtime.test_lane]]'s install line — a separate grant from --lane, because installing is a different act from running a suite")
	return c
}

// trustLane is `dross trust --lane <name>`: the grant for ONE test lane.
//
// Per lane rather than per repo, because that is what makes lanes usable at
// all: a repo with a Go lane and a docs lane whose grants moved together would
// re-prompt for the Go suite every time the docs command changed a character.
// Granting one lane leaves every other lane's grant, and the whole-suite grant,
// exactly where they were.
func trustLane(root, name string, check bool) error {
	proj, err := project.Load(filepath.Join(root, project.File))
	if err != nil {
		return err
	}
	lane, err := findLane(proj, name)
	if err != nil {
		return err
	}
	repoDir := filepath.Dir(root)
	if check {
		state, cerr := LaneConsented(root, repoDir, lane.Name, laneConsentLine(lane))
		if cerr == nil {
			return nil
		}
		return laneConsentRefusal(lane, state, cerr)
	}
	if err := consent.RefuseTrackedLocal(repoDir); err != nil {
		return err
	}
	if strings.TrimSpace(lane.Command) == "" {
		return fmt.Errorf(
			"nothing to trust: test lane %q declares no command.\n\n"+
				"Consent is bound to a command line, so there is nothing to bind to yet —\n"+
				"`dross validate` reports the same gap", name)
	}
	// Printed BEFORE the write, and in full — BOTH lines when the lane
	// declares a prepare. They arrive from TRACKED project.toml, so a grant
	// that did not show them would be consenting to whatever a clone happened
	// to carry, and one grant covers the pair.
	if lane.Prepare != "" {
		Printf("trusting test lane %q on this machine — both lines:\n\n    prepare: %s\n    command: %s\n\n", lane.Name, lane.Prepare, lane.Command)
	} else {
		Printf("trusting test lane %q on this machine:\n\n    %s\n\n", lane.Name, lane.Command)
	}
	if err := GrantLaneConsent(root, lane.Name, laneConsentLine(lane)); err != nil {
		return err
	}
	Printf("recorded in %s/%s (gitignored — it does not travel with the repo).\n", RootDirName, LocalFile)
	Printf("Editing or renaming lane %q — its prepare included — revokes this; every other lane's grant is untouched.\n", lane.Name)
	return nil
}

// trustRun grants consent for one [runtime] slot's command.
//
// Per slot, never per block: a grant covering "whatever [runtime] says" would
// let a dev_command arriving in a pull inherit trust for a line nobody read,
// which is the whole reason consent binds to a command string. Granting one
// slot leaves every other grant in place.
func trustRun(root, name string, check bool) error {
	proj, err := project.Load(filepath.Join(root, project.File))
	if err != nil {
		return err
	}
	slot, ok := findRunSlot(name)
	if !ok {
		return fmt.Errorf("unknown runtime command %q; known: %s", name, strings.Join(runSlotNames(), ", "))
	}
	line := strings.TrimSpace(slot.Get(proj))
	if line == "" {
		return fmt.Errorf("nothing to trust: %s is not set.\n\n"+
			"Set it first with `dross project set %s \"<command>\"`, then run this again —\n"+
			"consent is bound to the command, so there is nothing to bind to yet",
			slot.Field, slot.Field)
	}
	if check {
		ok, err := consent.RunConsented(grantStore(root), line)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		return runConsentRefusal(slot, line)
	}
	if err := consent.RefuseTrackedLocal(filepath.Dir(root)); err != nil {
		return err
	}
	// Printed before the write, in full: a grant that did not show the command
	// would be a rubber stamp on a line nobody read.
	Printf("trusting `dross run %s` on this machine:\n\n    %s\n\n", slot.Name, line)
	if err := consent.GrantRunConsent(grantStore(root), line); err != nil {
		return err
	}
	Printf("recorded in %s/%s (gitignored — it does not travel with the repo).\n", RootDirName, LocalFile)
	Printf("Editing %s revokes this; every other slot's grant is untouched.\n", slot.Field)
	return nil
}

// trustReplay is `dross trust --replay <phase-id>`: the grant for one phase's
// recorded replay line.
//
// A flag rather than a positional, so the existing no-args test-command grant
// path is untouched — `dross trust` still means the one thing it always meant,
// and the replay grant is visibly a different request.
func trustReplay(root, phaseID string, check bool) error {
	repoDir := filepath.Dir(root)
	if err := consent.RefuseTrackedLocal(repoDir); err != nil {
		return err
	}
	line, err := recordedReplayLine(root, phaseID)
	if err != nil {
		return err
	}
	if check {
		ok, cerr := consent.ReplayConsented(grantStore(root), line)
		if cerr != nil {
			return cerr
		}
		if !ok {
			return fmt.Errorf("%w: %s\n\nGrant it with `dross trust --replay %s`", ErrNoReplayConsent, line, phaseID)
		}
		return nil
	}
	// Printed BEFORE the write, and in full. This line arrives from a TRACKED
	// file the repo chose; a grant that did not show it would be consenting to
	// whatever a clone happened to carry.
	Printf("trusting %s's red-proof replay command on this machine:\n\n    %s\n\n", phaseID, line)
	if err := consent.GrantReplayConsent(grantStore(root), line); err != nil {
		return err
	}
	Printf("recorded in %s/%s (gitignored — it does not travel with the repo).\n", RootDirName, LocalFile)
	Print("Editing the recorded replay revokes this; dross will ask again.")
	return nil
}

// recordedReplayLine reads the replay command a phase's red proof records.
// Absent is an error naming what to do: there is nothing to consent to, and a
// silent success would leave the user believing a grant landed.
func recordedReplayLine(root, phaseID string) (string, error) {
	ch, err := changes.Load(changes.FilePath(root, phaseID), phaseID)
	if err != nil {
		return "", err
	}
	if ch.RedProof == nil || strings.TrimSpace(ch.RedProof.Replay) == "" {
		return "", fmt.Errorf("phase %q records no red-proof replay command — record one with `dross phase red-proof set %s --sha <sha> --doc <doc> --replay \"<cmd>\"` first", phaseID, phaseID)
	}
	return strings.TrimSpace(ch.RedProof.Replay), nil
}

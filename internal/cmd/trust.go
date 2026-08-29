package cmd

// The exec-consent store: dross will not spawn a repo's runtime.test_command
// until this machine has explicitly consented to that exact command.
//
// The threat is a `.dross/` that was not authored here. project.toml is a
// tracked, committed file, so cloning a repo — or pulling a branch from one —
// hands dross a test_command chosen by whoever wrote it, and every loop command
// that runs the suite would execute it without anyone having read the line.
//
// Two locked decisions shape this store, and both are load-bearing:
//
//   - exec_consent_gate: consent lives in the GITIGNORED .dross/local.toml,
//     never in project.toml. A committed consent key would be self-authorizing —
//     the hostile repo would ship both the command and the permission for it.
//     A clone carries no consent by construction, which is the whole mechanism.
//     This is the same property readAllowHosts protects for the host allowlist,
//     and it shares that refusal (refuseTrackedLocal) rather than restating it.
//
//   - consent_binding: consent is bound to sha256 of the CONSENTED COMMAND, not
//     to the repo. The attack this exists for is an already-trusted repo whose
//     test_command is rewritten by a later pull; repo-scoped consent would
//     inherit the trust granted to the old command. So a changed command
//     revokes consent and re-prompts.
//
// There is deliberately NO normalizer. Trimming whitespace, collapsing spaces
// or canonicalising quotes would all be a classifier deciding which edits are
// "the same command" — and a classifier is exactly the vulnerability this
// milestone keeps finding. One byte of drift revokes consent. The cost is a
// re-prompt after a legitimate edit, which is cheap and rare.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/changes"
	"github.com/Rivil/dross/internal/project"
)

// ConsentState is what the store says about the currently configured
// runtime.test_command. Every state but ConsentGranted is a refusal; they are
// distinguished so the message can tell the user which situation they are in,
// because "stale — the command changed since you trusted it" and "never trusted
// here" call for very different reactions.
type ConsentState int

const (
	// ConsentRefused: .dross/local.toml is tracked by git, so the store itself
	// cannot be trusted and is not read.
	ConsentRefused ConsentState = iota
	// ConsentNotApplicable: no runtime.test_command is configured. It is still
	// a refusal, not a pass — see CheckConsent.
	ConsentNotApplicable
	// ConsentAbsent: nothing has ever been trusted in this tree.
	ConsentAbsent
	// ConsentStale: something was trusted, but not this command.
	ConsentStale
	// ConsentGranted: the configured command matches the consented hash.
	ConsentGranted
)

func (s ConsentState) String() string {
	switch s {
	case ConsentRefused:
		return "refused"
	case ConsentNotApplicable:
		return "not-applicable"
	case ConsentAbsent:
		return "absent"
	case ConsentStale:
		return "stale"
	case ConsentGranted:
		return "granted"
	}
	return "unknown"
}

var (
	// ErrNoConsent is returned when this machine has never trusted a command in
	// this tree.
	ErrNoConsent = errors.New("no exec consent recorded for this repo")
	// ErrStaleConsent is returned when a command was trusted but the configured
	// one has since changed. A distinct sentinel from ErrNoConsent because the
	// stale case is the attack the binding exists for, and collapsing the two
	// would report a rewritten test_command as a first run.
	ErrStaleConsent = errors.New("the consented test command has changed since it was trusted")
	// ErrNoTestCommand is returned when no runtime.test_command is configured.
	ErrNoTestCommand = errors.New("no runtime.test_command is configured")
	// ErrNoLaneCommand is returned for a lane declaring no command. Distinct
	// from ErrNoTestCommand so a refusal can send the reader to the lane block
	// rather than to runtime.test_command, which may be perfectly fine.
	ErrNoLaneCommand = errors.New("this test lane declares no command")
	// ErrNoLaneInstall is returned for a lane declaring no install line.
	// Distinct from ErrNoLaneCommand for the reason that one is distinct from
	// ErrNoConsent: a lane with no install line has nothing to consent to and
	// is not ungranted, and a caller that read the two as one would ask the
	// user to trust a line that does not exist.
	ErrNoLaneInstall = errors.New("this test lane declares no install line")
)

// Fingerprint is the consent binding: hex sha256 of the command, byte for byte.
//
// It does not normalize. See the package comment above — a normalizer is a
// classifier, and the classifier is the vulnerability.
func Fingerprint(command string) string {
	sum := sha256.Sum256([]byte(command))
	return hex.EncodeToString(sum[:])
}

// CheckConsent reports whether this machine has consented to dross spawning
// testCmd in the tree rooted at root (with repoDir the enclosing git work tree).
//
// Every state but ConsentGranted comes back with a non-nil error, including
// ConsentNotApplicable. That last one is deliberate and is the case a reader
// most easily gets wrong: an empty runtime.test_command does NOT mean "nothing
// will be spawned". `dross verify` still runs its mutation adapters, which shell
// out to gremlins, which runs the repo's Go tests. A hostile .dross/ that simply
// leaves test_command blank would sail through a gate that treated empty as
// nothing to guard. So empty is a refusal too, and the caller can tell the user
// to configure a command and trust it.
func CheckConsent(root, repoDir, testCmd string) (ConsentState, error) {
	if err := refuseTrackedLocal(repoDir); err != nil {
		return ConsentRefused, err
	}
	if testCmd == "" {
		return ConsentNotApplicable, ErrNoTestCommand
	}
	l, err := loadLocal(localPath(root))
	if err != nil {
		// An unparseable store is not consent. Fail closed, and say why.
		return ConsentAbsent, fmt.Errorf("%w: %v", ErrNoConsent, err)
	}
	if l.TrustedTestCommand == "" {
		return ConsentAbsent, ErrNoConsent
	}
	if l.TrustedTestCommand != Fingerprint(testCmd) {
		return ConsentStale, ErrStaleConsent
	}
	return ConsentGranted, nil
}

// GrantConsent records consent for testCmd, storing only its fingerprint.
//
// The command itself is never written: the store would then be a second copy of
// a value project.toml already holds, and a reader comparing against it could
// not tell a consented command from a recorded one.
func GrantConsent(root, testCmd string) error {
	path := localPath(root)
	l, err := loadLocal(path)
	if err != nil {
		return err
	}
	l.TrustedTestCommand = Fingerprint(testCmd)
	return l.save(path)
}

// --- red-proof replay consent ---

// ErrNoReplayConsent is returned when a red proof's replay command has not been
// consented to on this machine. Callers match it with errors.Is: a repoint
// treats "no consent" as unverified-but-proceed, which is a different outcome
// from "the replay could not be run".
var ErrNoReplayConsent = errors.New("this machine has not consented to running this red proof's replay command")

// ReplayConsented reports whether line's fingerprint is in local.toml's
// trusted_replay_commands.
//
// Fingerprints, not lines, for the same reason the test command stores one: the
// store must not become a second copy of a value changes.json already holds,
// where a reader could not tell a consented command from a recorded one. An
// unparseable store is not consent — it fails closed.
func ReplayConsented(root, line string) (bool, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return false, nil
	}
	l, err := loadLocal(localPath(root))
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrNoReplayConsent, err)
	}
	want := Fingerprint(line)
	for _, got := range strings.Split(l.TrustedReplayCommands, ",") {
		if strings.TrimSpace(got) == want {
			return true, nil
		}
	}
	return false, nil
}

// GrantReplayConsent adds line's fingerprint to the consented set, leaving any
// already-granted replay lines in place: a repo has one replay per phase, and
// granting one must not silently revoke another.
func GrantReplayConsent(root, line string) error {
	path := localPath(root)
	l, err := loadLocal(path)
	if err != nil {
		return err
	}
	want := Fingerprint(strings.TrimSpace(line))
	var kept []string
	for _, got := range strings.Split(l.TrustedReplayCommands, ",") {
		if got = strings.TrimSpace(got); got != "" && got != want {
			kept = append(kept, got)
		}
	}
	l.TrustedReplayCommands = strings.Join(append(kept, want), ",")
	return l.save(path)
}

// RunConsented reports whether line's fingerprint is in local.toml's
// trusted_run_commands.
//
// A separate set from the test command's grant on purpose: consent is bound to
// a specific command string, and one that covered "whatever [runtime] says"
// would let a dev_command arriving in a pull inherit trust for a line nobody
// read. An unparseable store is not consent — it fails closed.
func RunConsented(root, line string) (bool, error) {
	return fingerprintInSet(root, line, func(l *localStore) string { return l.TrustedRunCommands })
}

// GrantRunConsent adds line's fingerprint to the run set, leaving the others in
// place: a repo has many runtime slots, and consenting to `dross run dev` must
// not silently revoke `dross run migrate`.
func GrantRunConsent(root, line string) error {
	path := localPath(root)
	l, err := loadLocal(path)
	if err != nil {
		return err
	}
	l.TrustedRunCommands = addFingerprint(l.TrustedRunCommands, strings.TrimSpace(line))
	return l.save(path)
}

// fingerprintInSet is the shared read half of the comma-separated grant sets.
func fingerprintInSet(root, line string, field func(*localStore) string) (bool, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return false, nil
	}
	l, err := loadLocal(localPath(root))
	if err != nil {
		return false, err
	}
	want := Fingerprint(line)
	for _, got := range strings.Split(field(l), ",") {
		if strings.TrimSpace(got) == want {
			return true, nil
		}
	}
	return false, nil
}

// addFingerprint returns set with line's fingerprint present exactly once,
// preserving every other member.
func addFingerprint(set, line string) string {
	want := Fingerprint(line)
	var kept []string
	for _, got := range strings.Split(set, ",") {
		if got = strings.TrimSpace(got); got != "" && got != want {
			kept = append(kept, got)
		}
	}
	return strings.Join(append(kept, want), ",")
}

// --- per-lane consent ---

// laneFrame is the domain separator that keeps a prepared lane's consent line
// out of the namespace a bare command occupies.
//
// A NUL cannot appear in an argv element, so no command line a user could
// actually run can spell this prefix — which is what makes the "no pair can
// forge a no-prepare lane's fingerprint" claim structural rather than
// probabilistic.
const laneFrame = "dross:lane-prepare:v1\x00"

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
//     the pair's own fingerprint. The two namespaces are disjoint by
//     construction, not by being hard to hit.
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
	if strings.ContainsRune(lane.Command, 0) || strings.ContainsRune(lane.Prepare, 0) {
		return ""
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
	if err := refuseTrackedLocal(repoDir); err != nil {
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
	if got != Fingerprint(line) {
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
	l.TrustedLaneCommands[name] = Fingerprint(line)
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
	if _, ok := l.TrustedLaneCommands[name]; !ok {
		return nil
	}
	delete(l.TrustedLaneCommands, name)
	if len(l.TrustedLaneCommands) == 0 {
		// Back to nil so omitempty keeps an empty table out of the file —
		// a bare [trusted_lane_commands] header reads as a store that holds
		// something.
		l.TrustedLaneCommands = nil
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
	if err := refuseTrackedLocal(repoDir); err != nil {
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
	if got != Fingerprint(line) {
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
	l.TrustedLaneInstalls[name] = Fingerprint(line)
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

// execGatedCommands is the CLOSED set of commands the consent gate covers,
// declared here rather than inferred from which call sites happen to call
// requireExecConsent. TestExecGatedSetIsExplicit asserts the two agree, so a
// command cannot silently join or leave the set.
//
// The set is the loop's chokepoints, not "everything that touches a phase".
// Two boundaries are deliberate:
//
//   - Read-only and post-hoc commands stay out. `dross status`, `dross doctor`
//     and `task status … done` must keep working in an untrusted tree, or the
//     gate bricks the very commands a user reaches for to understand why dross
//     is refusing. A gate that makes diagnosis impossible gets disabled.
//
//   - `verify` and `test` are in it because they are the commands that spawn
//     the suite itself — `dross test` is the execution site the prompts now
//     call, so gating it is gating the run rather than the step boundary
//     around it. The other four are in it because they are the loop's step
//     boundaries: refusing there stops an execute run before it reaches the
//     step that runs tests. The locked exec_consent_gate decision admits what
//     this cannot do — nothing stops an agent typing `go test` directly. The
//     CLI covers the CLI.
var execGatedCommands = []string{
	"changes record",
	"state bump",
	"task next",
	"task status in_progress",
	"test",
	"verify",
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
	state, cerr := CheckConsent(root, filepath.Dir(root), testCmd)
	if cerr == nil {
		return nil
	}
	return consentRefusal(state, cerr, testCmd)
}

// consentRefusal turns a consent state into the message the user acts on. The
// states are kept distinct all the way to the text because "you have never
// trusted anything here" and "what you trusted has since changed" call for very
// different reactions — the second is the attack the binding exists for, and
// collapsing it into the first would report it as a routine first run.
func consentRefusal(state ConsentState, cerr error, testCmd string) error {
	switch state {
	case ConsentRefused:
		return cerr
	case ConsentNotApplicable:
		return fmt.Errorf(
			"refusing to run: no runtime.test_command is configured.\n\n"+
				"This is not a free pass — mutation adapters still shell out and run this\n"+
				"repo's tests, so a blank command would be a way around the consent gate\n"+
				"rather than a reason to skip it.\n\n"+
				"Set one with `dross project set runtime.test_command \"<cmd>\"`, then run:\n\n"+
				"    dross trust\n\n%w", cerr)
	case ConsentStale:
		return fmt.Errorf(
			"refusing to run: this repo's test command has CHANGED since you trusted it —\n"+
				"the recorded consent is stale.\n\n"+
				"    %s\n\n"+
				"That is the case this gate exists for: a repo trusted once, whose\n"+
				"test_command a later pull rewrote. Read the line above; if it is what you\n"+
				"meant to run, re-consent:\n\n"+
				"    dross trust\n\n%w", testCmd, cerr)
	default:
		return fmt.Errorf(
			"refusing to run: this repo's test command has not been trusted on this machine.\n\n"+
				"    %s\n\n"+
				"dross runs that command (and the mutation tools that wrap it) as you, in\n"+
				"this checkout. It comes from the repo's tracked project.toml, so a clone\n"+
				"carries whatever its author wrote. Read the line above, then:\n\n"+
				"    dross trust\n\n%w", testCmd, cerr)
	}
}

// --- the command ---

// Trust registers `dross trust`.
func Trust() *cobra.Command {
	var check bool
	var replayPhase string
	var runSlotName string
	var laneName string
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
				state, cerr := CheckConsent(root, repoDir, testCmd)
				if cerr == nil {
					return nil
				}
				return consentRefusal(state, cerr, testCmd)
			}

			if err := refuseTrackedLocal(repoDir); err != nil {
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
			if err := GrantConsent(root, testCmd); err != nil {
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
	if err := refuseTrackedLocal(repoDir); err != nil {
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
		ok, err := RunConsented(root, line)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		return runConsentRefusal(slot, line)
	}
	if err := refuseTrackedLocal(filepath.Dir(root)); err != nil {
		return err
	}
	// Printed before the write, in full: a grant that did not show the command
	// would be a rubber stamp on a line nobody read.
	Printf("trusting `dross run %s` on this machine:\n\n    %s\n\n", slot.Name, line)
	if err := GrantRunConsent(root, line); err != nil {
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
	if err := refuseTrackedLocal(repoDir); err != nil {
		return err
	}
	line, err := recordedReplayLine(root, phaseID)
	if err != nil {
		return err
	}
	if check {
		ok, cerr := ReplayConsented(root, line)
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
	if err := GrantReplayConsent(root, line); err != nil {
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

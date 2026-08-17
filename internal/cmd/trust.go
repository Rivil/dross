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
	return c
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

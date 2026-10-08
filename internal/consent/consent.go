// Package consent is the exec-consent store: dross will not spawn a repo's
// runtime.test_command (or a red-proof replay line, or a `dross run` slot's
// command) until this machine has explicitly consented to that exact command.
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
//     RefuseTrackedLocal is that property enforced, shared by every reader of a
//     trust-bearing key rather than restated per reader.
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
//
// Persistence is behind Store: this package decides what a grant MEANS and
// never how local.toml is laid out, so the one writer of that file (internal/
// cmd's local store) stays the one writer.
package consent

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// State is what the store says about the currently configured command. Every
// state but Granted is a refusal; they are distinguished so the message can
// tell the user which situation they are in, because "stale — the command
// changed since you trusted it" and "never trusted here" call for very
// different reactions.
type State int

const (
	// Refused: .dross/local.toml is tracked by git, so the store itself
	// cannot be trusted and is not read.
	Refused State = iota
	// NotApplicable: no runtime.test_command is configured. It is still a
	// refusal, not a pass — see CheckConsent.
	NotApplicable
	// Absent: nothing has ever been trusted in this tree.
	Absent
	// Stale: something was trusted, but not this command.
	Stale
	// Granted: the configured command matches the consented hash.
	Granted
)

func (s State) String() string {
	switch s {
	case Refused:
		return "refused"
	case NotApplicable:
		return "not-applicable"
	case Absent:
		return "absent"
	case Stale:
		return "stale"
	case Granted:
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
	// ErrNoReplayConsent is returned when a red proof's replay command has not
	// been consented to on this machine. Callers match it with errors.Is: a
	// repoint treats "no consent" as unverified-but-proceed, which is a
	// different outcome from "the replay could not be run".
	ErrNoReplayConsent = errors.New("this machine has not consented to running this red proof's replay command")
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
// testCmd, with repoDir the enclosing git work tree the store lives under.
//
// Every state but Granted comes back with a non-nil error, including
// NotApplicable. That last one is deliberate and is the case a reader most
// easily gets wrong: an empty runtime.test_command does NOT mean "nothing will
// be spawned". `dross verify` still runs its mutation adapters, which shell
// out to gremlins, which runs the repo's Go tests. A hostile .dross/ that
// simply leaves test_command blank would sail through a gate that treated
// empty as nothing to guard. So empty is a refusal too, and the caller can
// tell the user to configure a command and trust it.
func CheckConsent(store Store, repoDir, testCmd string) (State, error) {
	if err := RefuseTrackedLocal(repoDir); err != nil {
		return Refused, err
	}
	if testCmd == "" {
		return NotApplicable, ErrNoTestCommand
	}
	g, err := store.Load()
	if err != nil {
		// An unparseable store is not consent. Fail closed, and say why.
		return Absent, fmt.Errorf("%w: %v", ErrNoConsent, err)
	}
	if g.TrustedTestCommand == "" {
		return Absent, ErrNoConsent
	}
	if g.TrustedTestCommand != Fingerprint(testCmd) {
		return Stale, ErrStaleConsent
	}
	return Granted, nil
}

// GrantConsent records consent for testCmd, storing only its fingerprint.
//
// The command itself is never written: the store would then be a second copy of
// a value project.toml already holds, and a reader comparing against it could
// not tell a consented command from a recorded one.
func GrantConsent(store Store, testCmd string) error {
	g, err := store.Load()
	if err != nil {
		return err
	}
	g.TrustedTestCommand = Fingerprint(testCmd)
	return store.Save(g)
}

// --- red-proof replay consent ---

// ReplayConsented reports whether line's fingerprint is in the store's
// trusted_replay_commands.
//
// Fingerprints, not lines, for the same reason the test command stores one: the
// store must not become a second copy of a value changes.json already holds,
// where a reader could not tell a consented command from a recorded one. An
// unparseable store is not consent — it fails closed, wrapping
// ErrNoReplayConsent so a repoint can still match it.
func ReplayConsented(store Store, line string) (bool, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return false, nil
	}
	g, err := store.Load()
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrNoReplayConsent, err)
	}
	return inSet(g.TrustedReplayCommands, line), nil
}

// GrantReplayConsent adds line's fingerprint to the consented set, leaving any
// already-granted replay lines in place: a repo has one replay per phase, and
// granting one must not silently revoke another.
func GrantReplayConsent(store Store, line string) error {
	g, err := store.Load()
	if err != nil {
		return err
	}
	g.TrustedReplayCommands = addFingerprint(g.TrustedReplayCommands, strings.TrimSpace(line))
	return store.Save(g)
}

// --- `dross run` slot consent ---

// RunConsented reports whether line's fingerprint is in the store's
// trusted_run_commands.
//
// A separate set from the test command's grant on purpose: consent is bound to
// a specific command string, and one that covered "whatever [runtime] says"
// would let a dev_command arriving in a pull inherit trust for a line nobody
// read. An unparseable store is not consent — it fails closed.
func RunConsented(store Store, line string) (bool, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return false, nil
	}
	g, err := store.Load()
	if err != nil {
		return false, err
	}
	return inSet(g.TrustedRunCommands, line), nil
}

// GrantRunConsent adds line's fingerprint to the run set, leaving the others in
// place: a repo has many runtime slots, and consenting to `dross run dev` must
// not silently revoke `dross run migrate`.
func GrantRunConsent(store Store, line string) error {
	g, err := store.Load()
	if err != nil {
		return err
	}
	g.TrustedRunCommands = addFingerprint(g.TrustedRunCommands, strings.TrimSpace(line))
	return store.Save(g)
}

// inSet is the shared read half of the comma-separated grant sets.
func inSet(set, line string) bool {
	want := Fingerprint(line)
	for _, got := range strings.Split(set, ",") {
		if strings.TrimSpace(got) == want {
			return true
		}
	}
	return false
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

// Refusal turns a consent state into the message the user acts on. The
// states are kept distinct all the way to the text because "you have never
// trusted anything here" and "what you trusted has since changed" call for very
// different reactions — the second is the attack the binding exists for, and
// collapsing it into the first would report it as a routine first run.
func Refusal(state State, cerr error, testCmd string) error {
	switch state {
	case Refused:
		return cerr
	case NotApplicable:
		return fmt.Errorf(
			"refusing to run: no runtime.test_command is configured.\n\n"+
				"This is not a free pass — mutation adapters still shell out and run this\n"+
				"repo's tests, so a blank command would be a way around the consent gate\n"+
				"rather than a reason to skip it.\n\n"+
				"Set one with `dross project set runtime.test_command \"<cmd>\"`, then run:\n\n"+
				"    dross trust\n\n%w", cerr)
	case Stale:
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

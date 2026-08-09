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

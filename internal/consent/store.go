package consent

import (
	"fmt"
	"os/exec"
)

// File is the machine-local store's basename under .dross/, and RelPath its
// path from the repo root — the one git is asked about.
const (
	File    = "local.toml"
	RelPath = ".dross/" + File
)

// Grants is every consent this machine has recorded, as the fields local.toml
// carries them. The store that persists it embeds this struct, so the toml
// tags here ARE the file's keys: a tag drift is a grant silently lost on
// reload, which is why cmd pins the round trip.
//
// The lane maps live here too, though this package has no lane logic yet:
// they are consent fields of the same file and belong to the same writer.
type Grants struct {
	// TrustedTestCommand is sha256(runtime.test_command) for the command the
	// user consented to dross spawning.
	TrustedTestCommand string `toml:"trusted_test_command,omitempty"`

	// TrustedReplayCommands is the comma-separated set of sha256 fingerprints
	// for the red-proof replay commands this machine has consented to dross
	// spawning. A separate key rather than a reuse of TrustedTestCommand
	// because the two are different grants: a replay line is one per phase
	// and arrives from changes.json, which is TRACKED.
	TrustedReplayCommands string `toml:"trusted_replay_commands,omitempty"`

	// TrustedRunCommands is the comma-separated set of sha256 fingerprints for
	// the [runtime] slot commands this machine has consented to `dross run`
	// spawning. A SET, like the replay grant: granting `dross run dev` must
	// not silently revoke `dross run migrate`.
	TrustedRunCommands string `toml:"trusted_run_commands,omitempty"`

	// TrustedLaneCommands maps a [[runtime.test_lane]] name to sha256 of that
	// lane's consent line. Keyed by name so one stale lane refuses only itself
	// (the locked lane_consent decision).
	TrustedLaneCommands map[string]string `toml:"trusted_lane_commands,omitempty"`

	// TrustedLaneInstalls maps a lane name to sha256 of its declared install
	// line — consent to INSTALL that lane's toolchain, a different act from
	// consent to run its suite (the locked install_consent decision).
	TrustedLaneInstalls map[string]string `toml:"trusted_lane_installs,omitempty"`
}

// Store is the persistence seam. Load returns the grants currently on disk —
// an absent store is an empty Grants, never an error — and Save writes them
// back, leaving every non-consent key of the underlying file exactly as it
// was. There is one implementation in production (internal/cmd's local store,
// the sole writer of local.toml); tests hand in a file-backed or in-memory
// one.
type Store interface {
	Load() (*Grants, error)
	Save(*Grants) error
}

// RefuseTrackedLocal is the provenance check every reader of a trust-bearing
// key in local.toml goes through — the host allowlist, the remote grant and
// the exec consent gate share ONE refusal rather than several that drift
// apart.
//
// It returns nil for a missing or untracked file — local.toml is optional, and
// a fresh clone legitimately has none. Only "git says this is tracked" is an
// error, and the file is refused UNREAD in that case: a committed store is
// either an accident an honest repo wants to know about, or a hostile repo
// authorizing itself through the one input dross trusts precisely because it is
// never cloned.
func RefuseTrackedLocal(repoDir string) error {
	//dross:exec-exempt fixed argv asking git whether dross's own local.toml is tracked; the only derived value is the -C work tree and no repo-authored line is run
	if exec.Command("git", "-C", repoDir, "ls-files", "--error-unmatch", "--", RelPath).Run() != nil {
		return nil
	}
	return fmt.Errorf(
		"refusing to read %s: git reports it tracked.\n\n"+
			"%s is machine-local by design — it is where this machine records what it\n"+
			"trusts (API allowlist hosts, the consented test command), so a committed\n"+
			"copy would let the repo authorize itself. dross will not read a tracked one.\n\n"+
			"To fix, untrack it and keep the local copy:\n\n"+
			"    git rm --cached %s\n"+
			"    git commit -m \"chore: untrack dross local store\"",
		RelPath, RelPath, RelPath)
}

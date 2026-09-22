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
		state, cerr := consent.LaneInstallConsented(grantStore(root), repoDir, lane.Name, consent.LaneInstallLine(lane))
		if cerr == nil {
			return nil
		}
		return consent.LaneInstallRefusal(lane, state, cerr)
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
	if err := consent.GrantLaneInstallConsent(grantStore(root), lane.Name, consent.LaneInstallLine(lane)); err != nil {
		return err
	}
	Printf("recorded in %s/%s (gitignored — it does not travel with the repo).\n", RootDirName, LocalFile)
	Printf("This is separate from `dross trust --lane %s`: it authorizes INSTALLING lane %q's\n", lane.Name, lane.Name)
	Print("toolchain, not running its suite. Editing or renaming the lane revokes it.")
	return nil
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
		state, cerr := consent.LaneConsented(grantStore(root), repoDir, lane.Name, consent.LaneLine(lane))
		if cerr == nil {
			return nil
		}
		return consent.LaneRefusal(lane, state, cerr)
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
	if err := consent.GrantLaneConsent(grantStore(root), lane.Name, consent.LaneLine(lane)); err != nil {
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

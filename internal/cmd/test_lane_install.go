package cmd

// `dross test lane install <name>` — the lane-scoped install verb.
//
// It owns BOTH sides. The local half cannot live under the `remote` noun —
// `dross remote bootstrap --local` is a contradiction in its own name — and the
// lane is the noun the two installs actually share, so this is where they
// belong (locked install_surface). `dross remote bootstrap` keeps its whole-host
// role: "is this machine ready" is a different question from "give this one lane
// what it needs".

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/remote"
	"github.com/Rivil/dross/internal/testlane"
)

// laneInstallRuntimes returns the runtimes the given tools' built-in recipes
// depend on, deduplicated and in a stable order.
//
// They are probed ALONGSIDE the tools, in the one probe, for the reason
// bootstrap probes its own: a second question is a second chance for the machine
// to change underneath the answer, and a recipe that ran without its runtime
// fails with a message about a missing binary rather than about the runtime the
// host's owner has to install.
func laneInstallRuntimes(tools []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, tool := range tools {
		r, ok := laneInstallRecipes[tool]
		if !ok || r.runtime == "" || seen[r.runtime] {
			continue
		}
		seen[r.runtime] = true
		out = append(out, r.runtime)
	}
	return out
}

// laneInstallSteps turns the gap on one machine into the steps that would close
// it.
//
// A lane declaring its own install line yields exactly ONE step whatever the
// size of the gap: the line is the lane's install, not one tool's, and running
// it once per missing binary would run the user's command N times for a gesture
// they wrote once. A lane declaring none yields one step per missing tool, each
// resolved from the built-in table.
//
// present is what the single probe found on the chosen machine, so a recipe
// whose runtime is missing there is refused by name rather than attempted.
func laneInstallSteps(lane project.TestLane, gap []string, present map[string]bool) []installStep {
	if lane.Install != "" {
		// Labelled with the first missing tool: the step has to name something
		// concrete for the transcript, and the first gap is what the user went
		// looking for.
		return []installStep{resolveInstall(gap[0], lane.Install)}
	}
	steps := make([]installStep, 0, len(gap))
	for _, tool := range gap {
		step := resolveInstall(tool, "")
		if step.Runtime != "" && !present[step.Runtime] {
			steps = append(steps, installStep{
				Tool:    tool,
				Refusal: fmt.Sprintf("%s needs %s there first — install it on that machine (dross does not install language runtimes)", tool, step.Runtime),
			})
			continue
		}
		steps = append(steps, step)
	}
	return steps
}

// laneInstallSite is which machine this run decided to act on.
type laneInstallSite struct {
	// Target is the granted host, nil when the install lands here.
	Target *remote.Target
	// Machine is what the transcript calls the side — the host's name, or
	// "this machine". Every line says which one it acted on, because an
	// install transcript that did not would leave the reader unable to tell a
	// provisioned laptop from a provisioned server.
	Machine string
	// Gap is the lane's tools that machine is missing, in lane order.
	Gap []string
	// Present is every probed binary that machine already has, so a recipe's
	// runtime prerequisite is answered from the same probe.
	Present map[string]bool
}

func testLaneInstall() *cobra.Command {
	var apply bool
	var local bool
	c := &cobra.Command{
		Use:   "install <name>",
		Short: "Install the toolchain one test lane is missing, on the machine that is missing it",
		Long: "Probes the lane's toolchain and installs what is absent — on the granted\n" +
			"host when the lane would have run there, on this machine when this machine\n" +
			"is the one missing the tool. Every line says which machine it acted on.\n\n" +
			"Dry-run by default: it prints what it would run and changes nothing. Pass\n" +
			"--apply to install, and --local to force the install onto this machine\n" +
			"whichever side the probe found the gap on.\n\n" +
			"An install command is never guessed: it comes from a built-in recipe or\n" +
			"from the lane's own `install` line, and a tool with neither is named rather\n" +
			"than attempted. A declared line needs its own grant — `dross trust\n" +
			"--lane-install <name>` — since installing changes a machine.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			root, p, err := loadProjectForLanes()
			if err != nil {
				return err
			}
			// Resolved through the SHARED finder, so an unknown name lists the
			// declared lanes exactly as `dross trust --lane` does — and does it
			// before anything is probed.
			lane, err := findLane(p, args[0])
			if err != nil {
				return err
			}
			repoDir := filepath.Dir(root)
			// The consent gate runs BEFORE any I/O, not just before the seam: a
			// probe is a connection to another machine, and an ungranted line
			// has not earned one. runLaneInstall gates too — this is the early
			// half, not a substitute for it.
			if lane.Install != "" {
				state, cerr := LaneInstallConsented(root, repoDir, lane.Name, laneInstallConsentLine(lane))
				if cerr != nil {
					return laneInstallRefusal(lane, state, cerr)
				}
			}

			site, err := laneInstallTarget(root, repoDir, lane, local)
			if err != nil {
				return err
			}
			if len(site.Gap) == 0 {
				// Exit 0. A provisioned machine is the success case, and a verb
				// that failed on "there was nothing to do" would be one nobody
				// could put in a script.
				Printf("lane %s: nothing to install — %s has %s\n",
					lane.Name, site.Machine, joinTools(testlane.Toolchain(lane.Command, lane.Prepare, lane.Toolchain)))
				return nil
			}
			return reportLaneInstall(root, repoDir, lane, site, apply)
		},
	}
	c.Flags().BoolVar(&apply, "apply", false, "actually install (default is a dry run that changes nothing)")
	c.Flags().BoolVar(&local, "local", false, "install on this machine even when the granted host is the side with the gap")
	return c
}

// laneInstallTarget picks the machine to act on, from ONE probe.
//
// The default needs no flag to be right (c-6): the gap goes to the side that
// has it. When the granted host lacks a tool, that is the side the lane would
// have run on, so that is where the install lands — including when THIS machine
// lacks it too, because refusing there would send the user to install by hand
// exactly what dross can install. When the host is fine and this machine is not,
// this machine is the gap.
//
// --local inverts that deliberately (locked install_local_override): it is the
// escape hatch for a granted host that is up but is not the machine the user
// wants provisioned — this laptop ahead of a trip, say.
//
// A probe that failed is never read as an answer. It is returned as an error, so
// nothing installs on either side: a local install firing on the strength of an
// unanswered probe would provision the wrong machine on no evidence at all.
func laneInstallTarget(root, repoDir string, lane project.TestLane, local bool) (laneInstallSite, error) {
	tools := testlane.Toolchain(lane.Command, lane.Prepare, lane.Toolchain)
	probe := append(append([]string{}, tools...), laneInstallRuntimes(tools)...)

	var target *remote.Target
	if !local {
		t, err := readRemoteGrant(root, repoDir)
		if err != nil {
			return laneInstallSite{}, err
		}
		target = t
	}

	if target != nil {
		ready, err := remoteProbeFn(*target, probe)
		if err != nil {
			return laneInstallSite{}, fmt.Errorf("cannot decide what lane %q needs: %s did not answer: %w", lane.Name, target.Host, err)
		}
		gone := map[string]bool{}
		for _, m := range ready.Missing {
			gone[m] = true
		}
		if gap := absentTools(tools, gone); len(gap) > 0 {
			return laneInstallSite{
				Target:  target,
				Machine: target.Host,
				Gap:     gap,
				Present: presentOf(probe, gone),
			}, nil
		}
		// The host has everything. Anything left to install is here.
	}

	gone := map[string]bool{}
	for _, tool := range probe {
		if _, err := laneLookPath(tool); err != nil {
			gone[tool] = true
		}
	}
	return laneInstallSite{
		Machine: "this machine",
		Gap:     absentTools(tools, gone),
		Present: presentOf(probe, gone),
	}, nil
}

// presentOf inverts a missing set over the probed list.
func presentOf(probed []string, gone map[string]bool) map[string]bool {
	present := map[string]bool{}
	for _, tool := range probed {
		if !gone[tool] {
			present[tool] = true
		}
	}
	return present
}

// reportLaneInstall prints each step and, under --apply, runs the installable
// ones.
//
// One failure never aborts the rest, on reportBootstrap's precedent: the steps
// are independent tools, and stopping at the first would leave a machine
// half-provisioned for reasons that had nothing to do with the tools never
// attempted.
//
// Refused, failed and unreported are counted APART. A refusal is a fact about
// the machine with a remedy only its owner can perform; a failed exec is dross's
// attempt that did not work; and a tool with no recipe and no install line is a
// gap in this repo's configuration. Only the first two are non-zero, and only
// the second is a failure (locked undeclared_exit, c-8).
func reportLaneInstall(root, repoDir string, lane project.TestLane, site laneInstallSite, apply bool) error {
	Printf("lane %s on %s — %s\n", lane.Name, site.Machine, bootstrapMode(apply))
	installed, refused, failed, unreported := 0, 0, 0, 0

	for _, s := range laneInstallSteps(lane, site.Gap, site.Present) {
		switch {
		case s.Refusal != "":
			Printf("  ✗ %s refused on %s — %s\n", s.Tool, site.Machine, s.Refusal)
			refused++
		case s.Unknown:
			Printf("  · %s on %s — %s\n", s.Tool, site.Machine, s.Note)
			unreported++
		case !apply:
			Printf("  → %s would install on %s: %s\n", s.Tool, site.Machine, installPreview(s))
		default:
			Printf("  → %s installing on %s: %s\n", s.Tool, site.Machine, installPreview(s))
			if _, err := runLaneInstall(root, repoDir, site.Target, lane, s); err != nil {
				Printf("    ✗ %v\n", err)
				failed++
				continue
			}
			installed++
		}
	}

	switch {
	case !apply:
		Printf("\nDry run — %s was not changed. Re-run with --apply to install.\n", site.Machine)
	default:
		Printf("\n%d installed on %s, %d refused, %d failed.\n", installed, site.Machine, refused, failed)
	}
	if unreported > 0 {
		Printf("%d tool(s) have no install recipe and no install line — add one with `dross test lane edit %s --install \"<cmd>\"`.\n", unreported, lane.Name)
	}
	if refused > 0 || failed > 0 {
		return &ExitCodeError{Code: 1, Err: fmt.Errorf("lane %s: %d tool(s) refused, %d failed — %s is not fully provisioned", lane.Name, refused, failed, site.Machine)}
	}
	return nil
}

// installPreview renders what a step would run, for reading. Display only — the
// argv the seam receives is built by installArgv, which is the only renderer
// anything executes.
func installPreview(s installStep) string {
	if s.Line != "" {
		return s.Line
	}
	return shellPreview(s.Argv)
}

package cmd

// Installing the toolchain a declared test lane needs.
//
// This file is the DECISION layer both install surfaces share — `dross test
// lane install` and `dross remote bootstrap`'s lane coverage. It answers one
// question, purely: given a tool a lane needs and whatever the lane declared,
// what would dross do about it? Actually doing it is the surfaces' job, through
// the two seams at the bottom.
//
// The split matters because the two surfaces must never disagree. A lane
// reported installable by one and refused by the other would send the user
// between two commands with different answers about the same host.

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/remote"
)

// installStep is what dross intends to do about ONE tool a lane needs.
//
// Exactly one of Argv / Line / Refusal / Unknown is meaningful, and the four
// are the whole vocabulary:
//
//   - Argv    — dross has a built-in recipe: the tool is a PACKAGE installed
//     into a runtime that is already there.
//   - Line    — the lane declared its own install line, which REPLACES the
//     built-in recipe for this lane (locked install_recipe_source).
//   - Refusal — dross will not install it. The tool IS a language runtime, or
//     needs one the host lacks: version policy and PATH ownership on a machine
//     dross was merely lent belong to that machine's owner (locked
//     install_scope_for_lanes). A refusal is a fact about the host, not a
//     failed attempt, and the two are counted apart because only one of them
//     has a remedy dross can perform.
//   - Unknown — dross knows nothing about the tool and the lane declared
//     nothing. Reported, never attempted, and deliberately NOT a refusal:
//     counting it as one would make every repo with lanes and no install lines
//     start exiting non-zero on a command that passed the day before (locked
//     undeclared_exit).
type installStep struct {
	// Tool is the binary the machine must end up with — the same name the
	// locality probe asks for, so the two cannot disagree about what a lane
	// being "runnable here" means.
	Tool string
	// Runtime is the binary that must ALREADY be present for Argv to work.
	// Empty for a declared Line: the lane's author owns that line's
	// prerequisites, which is the whole point of the escape hatch.
	Runtime string
	// Argv is a built-in recipe, as an argv rather than a shell line so it
	// goes through the transport seam's own quoting (locked install_transport).
	Argv []string
	// Line is the lane's declared install line — a shell line, because that is
	// what a user writes in project.toml. It is rendered to an argv at exactly
	// one place (installArgv) and never quoted whole.
	Line string
	// Refusal names what the host's owner has to do. Empty unless this step is
	// a refusal, and NEVER set alongside Unknown.
	Refusal string
	// Unknown reports that neither a recipe nor a declared line exists.
	Unknown bool
	// Note carries the Unknown arm's wording. It is a separate field from
	// Refusal so that "dross cannot install this" and "dross will not install
	// this" stay distinguishable at the type level rather than by reading the
	// message — they exit differently.
	Note string
}

// laneInstallRecipe describes how (or whether) one lane tool can be installed.
//
// It mirrors bootstrapRecipe deliberately rather than reusing it: that table is
// keyed by the tools MUTATION ADAPTERS need and is read by bootstrap's adapter
// leg, and merging the two would make adding a lane recipe able to change what
// a mutation run installs.
type laneInstallRecipe struct {
	// runtime is the binary that must already exist for argv to work. Every
	// installable row names one — a row that needed nothing already installed
	// would be installing a runtime, which is the line install_scope_for_lanes
	// draws.
	runtime string
	// argv installs the tool, given runtime is present. Nil means the tool IS
	// a runtime and dross never installs it.
	argv []string
	// refusal is the message used when argv is nil. It names what the host's
	// owner has to install.
	refusal string
}

// laneInstallRecipes is the built-in table, keyed by the binary names a lane's
// toolchain resolves to.
//
// It exists so the feature works on lanes ALREADY declared instead of lying
// dead until every lane is rewritten with an install line — the same argument
// that made toolchain derivation beat a declared-only field. Every installable
// row is a PACKAGE going into a runtime that is already there; the runtime rows
// below are refusals by design, not gaps waiting to be filled.
//
// Versions are whatever the recipe resolves at install time. Pinning lane
// toolchains is an explicit milestone non-goal — dross does not manage their
// versions — which is the opposite call from gremlinsPin, and deliberately: a
// pinned gremlins is dross's own measurement instrument, where two hosts
// disagreeing changes dross's numbers, while a lane's toolchain is the repo's
// own choice about its own suite.
var laneInstallRecipes = map[string]laneInstallRecipe{
	// Go packages — into a Go toolchain that is already there.
	"staticcheck":   {runtime: "go", argv: []string{"go", "install", "honnef.co/go/tools/cmd/staticcheck@latest"}},
	"golangci-lint": {runtime: "go", argv: []string{"go", "install", "github.com/golangci/golangci-lint/cmd/golangci-lint@latest"}},
	"gotestsum":     {runtime: "go", argv: []string{"go", "install", "gotest.tools/gotestsum@latest"}},

	// Node packages — into a Node runtime that is already there.
	"pnpm":         {runtime: "node", argv: []string{"npm", "install", "-g", "pnpm"}},
	"markdownlint": {runtime: "node", argv: []string{"npm", "install", "-g", "markdownlint-cli"}},

	// Runtimes. Refused by name, never installed (locked
	// install_scope_for_lanes). A lane that genuinely needs one provisioned
	// writes its own `install` line, which puts the choice with the person who
	// owns the host rather than with dross.
	"go":     {refusal: "go is the Go toolchain itself — install it on the host (dross does not install language runtimes)"},
	"node":   {refusal: "node is the Node runtime itself — install it on the host (dross does not install language runtimes)"},
	"npx":    {refusal: "npx ships with Node — install Node on the host (dross does not install language runtimes)"},
	"dotnet": {refusal: "dotnet is the .NET SDK itself — install it on the host (dross does not install language runtimes)"},
}

// resolveInstall decides what dross would do about one tool, given whatever the
// lane declared.
//
// Pure: it reads no host, so both surfaces can plan without probing and every
// arm is testable without a machine. It returns EXACTLY one of the four arms.
//
// A declared line REPLACES the table's entry rather than extending it (locked
// install_recipe_source). Appending would keep running the very recipe the user
// overrode to say was wrong, and it is also the only route to a runtime the
// table refuses — routing that case through a line the lane's author wrote
// keeps the decision with the host's owner.
func resolveInstall(tool, declared string) installStep {
	step := installStep{Tool: tool}
	// The declared line wins before the table is even consulted, so the
	// built-in entry cannot leak into the returned step by any path.
	if line := strings.TrimSpace(declared); line != "" {
		step.Line = line
		return step
	}
	r, ok := laneInstallRecipes[tool]
	switch {
	case !ok:
		// Reported, not refused. Refusal stays EMPTY here on purpose: the exit
		// ladder counts refusals, and a declared lane with no recipe and no
		// install line is a gap in this repo's configuration rather than a
		// failure of the host (locked undeclared_exit).
		step.Unknown = true
		step.Note = fmt.Sprintf("no install line; add one or install %s on the host by hand", tool)
	case r.argv == nil:
		step.Refusal = r.refusal
	default:
		step.Runtime = r.runtime
		// Copied, not aliased: a caller mutating the returned Argv would edit
		// the package-level table for every later call in the process.
		step.Argv = append([]string{}, r.argv...)
	}
	return step
}

// laneInstallable reports whether dross has something it could actually run for
// this step.
//
// Refusals and unknowns are both false, and they are false for different
// reasons — will not versus cannot — which is why the two arms stay separate on
// the step rather than collapsing into one "not installable" flag here.
func laneInstallable(s installStep) bool {
	return len(s.Argv) > 0 || s.Line != ""
}

// installArgv renders a step into the argv that runs it. It is the ONE place a
// declared line becomes an argv.
//
// A line is wrapped as `sh -c <line>` — three elements — because the line is a
// shell line: `npm install -g pnpm` quoted whole would exec a binary literally
// named "npm install -g pnpm". A built-in recipe's Argv passes through
// untouched, since it was written as an argv precisely so no shell has to parse
// it.
//
// The line goes through the same leading-dash fence `dross test` puts
// runtime.test_command through: a line starting with `-` would be read by sh as
// its own flag rather than as a script.
func installArgv(s installStep) ([]string, error) {
	if s.Line != "" {
		rest, err := shArgvFor("test_lane.install", s.Line)
		if err != nil {
			return nil, err
		}
		return append([]string{"sh"}, rest...), nil
	}
	return s.Argv, nil
}

// localInstallFn is the LOCAL install seam.
//
// A package-level var for the reason remoteExecFn is one: installing changes a
// machine, and every test has to be able to assert what would have run without
// running it. Production never reassigns it.
//
// It is a second seam only because the machines are genuinely different; the
// REMOTE half deliberately reuses remoteExecFn rather than declaring one here,
// so both install surfaces and both of their test suites stub the same variable.
var localInstallFn = runInstallLocally

// runInstallLocally spawns an install argv on this machine.
//
// Output is captured rather than streamed: an install is short and its whole
// value on failure is the tail, unlike a suite whose silence is indistinguishable
// from a hang.
func runInstallLocally(argv []string) (string, error) {
	if len(argv) == 0 {
		return "", fmt.Errorf("empty install command")
	}
	out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s: %w", argv[0], err)
	}
	return string(out), nil
}

// runLaneInstall executes one step on the side the caller chose: the granted
// host when target is non-nil, this machine when it is nil.
//
// Both surfaces go through here, so the consent gate is enforced ONCE for both
// rather than restated at each — a second copy is a second place to forget it,
// and the thing forgotten would be an unread line from a tracked file running
// on someone's machine.
//
// The gate covers a DECLARED line only. dross's own built-in recipes are not
// lines this repo supplied — they are dross's, the same way bootstrap's adapter
// recipes are — so gating them would ask the user to consent to dross's own
// code, which teaches them to approve without reading.
//
// The dry-run default stays the callers': whether to run at all is a decision
// about the invocation, while whether this line MAY run is a decision about the
// line, and only the second one belongs to a shared helper.
func runLaneInstall(root, repoDir string, target *remote.Target, lane project.TestLane, s installStep) (string, error) {
	if s.Line != "" {
		state, cerr := LaneInstallConsented(root, repoDir, lane.Name, laneInstallConsentLine(lane))
		if cerr != nil {
			// Refused BEFORE the argv is even rendered, so an ungranted line
			// never reaches a seam and never reaches a transport.
			return "", laneInstallRefusal(lane, state, cerr)
		}
	}
	argv, err := installArgv(s)
	if err != nil {
		return "", err
	}
	if target != nil {
		return remoteExecFn(*target, argv)
	}
	return localInstallFn(argv)
}

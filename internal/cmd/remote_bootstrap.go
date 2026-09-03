package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/remote"
)

// gremlinsPin is the exact gremlins revision bootstrap installs.
//
// Pinned rather than @latest for the same reason strykerPin exists
// (internal/mutation/stryker.go): `go install …@latest` resolves whatever the
// proxy serves at the moment the command runs, so two hosts bootstrapped a week
// apart measure the same repo with different mutators — and a compromised tag
// lands without anything in this repo changing. A pin is a value someone has to
// edit deliberately.
const gremlinsPin = "github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0"

// bootstrapStep is what bootstrap intends to do about one tool the host needs.
//
// Exactly one of Present / Argv / Refusal is meaningful, and the three are the
// whole vocabulary:
//
//   - Present   — the host already has it. Nothing to do, and a second
//     bootstrap over a provisioned host plans nothing at all.
//   - Argv      — the tool is an adapter package dross can install into a
//     runtime that is already there.
//   - Refusal   — the tool IS a language runtime, or needs one the host lacks.
//     Installing a runtime is host administration: version policy, PATH
//     ownership, usually root (locked install_scope). The refusal names what
//     the host's owner has to install, which is the whole value of reporting it.
type bootstrapStep struct {
	// Tool is the binary the host must end up with — the same name doctor
	// probes for, so the two cannot disagree about what "ready" means.
	Tool string
	// Adapter is the mutation adapter that wants Tool. Carried so a step can
	// say why the host needs it rather than leaving the reader to guess.
	Adapter string
	// Lane is the declared test lane that wants Tool, when no adapter does.
	// Exactly one of Adapter and Lane is set: the attribution comes from
	// remoteProbeTools, which keeps the two disjoint so one gap is never
	// reported twice.
	Lane string
	// Present reports that the probe already found Tool on the host.
	Present bool
	// Argv is the install command, as an argv rather than a shell line so it
	// goes through the internal/remote seam's quoting (locked
	// install_transport). A lane's DECLARED line is resolved to `sh -c <line>`
	// here at plan time, through the one shared renderer, so the report has a
	// single exec path rather than one per origin. Empty when Present, Refusal
	// or Unknown is set.
	Argv []string
	// Refusal explains why bootstrap will not install Tool. Empty otherwise.
	Refusal string
	// Unknown reports a lane tool with no built-in recipe and no install line.
	// It PRINTS and does not count toward the exit (locked undeclared_exit):
	// counting it would make every repo with lanes and no install lines start
	// exiting 1 on a command that passed the day before. Never set alongside
	// Refusal.
	Unknown bool
	// Note carries the Unknown arm's wording.
	Note string
}

// origin renders what a step is being installed FOR, in the one shape both
// halves of the report use.
//
// The vocabulary is deliberately shared with the adapter half (c-2): a host's
// readiness for lanes and for mutation must not read as two different answers
// to the reader, so a present lane tool and a present adapter tool produce the
// same line but for this tag.
func (s bootstrapStep) origin() string {
	if s.Lane != "" {
		return "lane " + s.Lane
	}
	return s.Adapter
}

// bootstrapRecipe describes how (or whether) one tool can be installed.
type bootstrapRecipe struct {
	// runtime is the binary that must already exist for argv to work. Empty
	// means the tool needs no runtime beyond a shell.
	runtime string
	// argv installs the tool, given runtime is present. Nil means the tool IS
	// a runtime and bootstrap never installs it.
	argv []string
	// refusal is the message used when argv is nil — it names what the host's
	// owner has to install.
	refusal string
}

// bootstrapRecipes is the whole table, keyed by the tool names
// remoteMutationTools produces.
//
// Only gremlins is installable, and that is not an oversight: gremlins is a Go
// PACKAGE, installed into a Go toolchain that already exists. `npx` ships with
// Node and `dotnet` is the .NET SDK — both are runtimes, and the locked
// install_scope decision draws the line exactly there.
var bootstrapRecipes = map[string]bootstrapRecipe{
	"gremlins": {
		runtime: "go",
		argv:    []string{"go", "install", gremlinsPin},
	},
	"npx": {
		refusal: "npx ships with Node — install Node on the host (bootstrap does not install language runtimes)",
	},
	"dotnet": {
		refusal: "dotnet is the .NET SDK itself — install it on the host (bootstrap does not install language runtimes)",
	},
}

// bootstrapProbeSet is every binary bootstrap asks a host about: the tools
// themselves plus the runtimes their recipes need.
//
// Runtimes are probed alongside the tools in ONE round trip: a second probe
// would be a second chance for the host to change underneath the answer. Both
// recipe tables contribute, since a lane's recipe has prerequisites the
// adapters' table knows nothing about.
//
// Extracted so the command's host RESOLUTION and the plan ask the same
// question. A private derivation on either side drifts, and the drift shows up
// as bootstrap provisioning for a probe set nothing else agrees with.
func bootstrapProbeSet(tools []string) []string {
	probe := append(append([]string{}, tools...), bootstrapRuntimes(tools)...)
	probe = append(probe, laneInstallRuntimes(tools)...)
	return dedupeTools(probe)
}

// planRemoteBootstrap decides what bootstrap would do to a host, without doing
// any of it.
//
// The tool set comes from remoteProbeTools — the same function doctor's Remote
// section reads — so a repo whose [mutation].adapters allowlist selects one
// adapter never has bootstrap planning for another, and a lane's toolchain is
// planned from the SAME derivation the run uses. Two surfaces disagreeing about
// which tools a repo needs is the failure this shares the function to prevent,
// and the shape it takes is doctor passing on a host the run then falls back
// from.
//
// ready is the host's answer over bootstrapProbeSet, taken by the CALLER. It
// arrives here rather than being probed here because resolving which host to
// bootstrap now walks the pool (c-2), and that walk already probed every
// candidate: probing again would be a second round trip per host, and a second
// chance for the host to change underneath the answer. One probe covers
// adapters and lanes together — a host's readiness for lanes and for mutation
// is one question with one answer.
//
// A transport failure never reaches here at all: the walk returns it as an
// error, so a host that could not be reached produces no plan rather than a
// plan whose every step claims the tool is absent — which would propose
// installing a whole toolchain onto a machine that is merely down.
func planRemoteBootstrap(p *project.Project, ready remote.Readiness) ([]bootstrapStep, error) {
	tools, needBy, laneBy := remoteProbeTools(p)
	if len(tools) == 0 {
		return nil, nil
	}
	// The repo root is resolved HERE rather than threaded in, and only when
	// there is a lane step to consent for: a lane's declared install line is
	// gated at plan time, so the plan needs the machine-local grant store. The
	// adapter-only path never reaches this, so a repo with no lanes cannot
	// start failing on a root lookup it never needed.
	var root, repoDir string
	if len(laneBy) > 0 {
		r, err := FindRoot()
		if err != nil {
			return nil, err
		}
		root, repoDir = r, filepath.Dir(r)
	}
	missing := map[string]bool{}
	for _, m := range ready.Missing {
		missing[m] = true
	}

	steps := make([]bootstrapStep, 0, len(tools))
	for _, tool := range tools {
		lane := laneBy[tool]
		step := bootstrapStep{Tool: tool, Adapter: needBy[tool], Lane: lane}
		switch {
		case !missing[tool]:
			step.Present = true
		case lane != "":
			planLaneStep(root, repoDir, p, lane, tool, missing, &step)
		default:
			r := bootstrapRecipes[tool]
			switch {
			case r.argv == nil:
				step.Refusal = r.refusal
				if step.Refusal == "" {
					// Fail closed: a tool nobody wrote a recipe for is refused
					// by name rather than silently skipped, so adding an
					// adapter cannot quietly add an unprovisionable tool.
					step.Refusal = fmt.Sprintf("no install recipe for %q — bootstrap will not guess one", tool)
				}
			case missing[r.runtime]:
				step.Refusal = fmt.Sprintf("%s needs %s on the host first — install it there (bootstrap does not install language runtimes)", tool, r.runtime)
			default:
				step.Argv = append([]string{}, r.argv...)
			}
		}
		steps = append(steps, step)
	}
	return steps, nil
}

// planLaneStep fills in one lane tool's step, through the SAME resolver `dross
// test lane install` uses.
//
// Consent is resolved HERE rather than at the exec site, so an ungranted
// declared line reports as a REFUSAL — the two have different remedies, and
// bootstrap must not become a way around the gate the lane-scoped verb enforces.
func planLaneStep(root, repoDir string, p *project.Project, laneName, tool string, missing map[string]bool, step *bootstrapStep) {
	lane, err := findLane(p, laneName)
	if err != nil {
		// Unreachable in practice — the name came from this project's own
		// lanes — but fail closed rather than plan an install for a lane
		// nothing declares.
		step.Refusal = err.Error()
		return
	}
	resolved := resolveInstall(tool, lane.Install)
	switch {
	case resolved.Unknown:
		step.Unknown = true
		step.Note = resolved.Note
	case resolved.Refusal != "":
		step.Refusal = resolved.Refusal
	case resolved.Line != "":
		if _, cerr := LaneInstallConsented(root, repoDir, lane.Name, laneInstallConsentLine(lane)); cerr != nil {
			step.Refusal = fmt.Sprintf("lane %s's install line is not trusted on this machine — read it with `dross trust --lane-install %s`", lane.Name, lane.Name)
			return
		}
		argv, aerr := installArgv(resolved)
		if aerr != nil {
			step.Refusal = aerr.Error()
			return
		}
		step.Argv = argv
	case resolved.Runtime != "" && missing[resolved.Runtime]:
		step.Refusal = fmt.Sprintf("%s needs %s on the host first — install it there (bootstrap does not install language runtimes)", tool, resolved.Runtime)
	default:
		step.Argv = resolved.Argv
	}
}

// dedupeTools collapses a probe list, keeping first-seen order. A tool asked
// for twice is one question with one answer; asking it twice would only make
// the transcript longer.
func dedupeTools(tools []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(tools))
	for _, tool := range tools {
		if tool == "" || seen[tool] {
			continue
		}
		seen[tool] = true
		out = append(out, tool)
	}
	return out
}

// bootstrapRuntimes returns the runtime binaries the given tools' recipes
// depend on, deduplicated and in a stable order.
func bootstrapRuntimes(tools []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, tool := range tools {
		r, ok := bootstrapRecipes[tool]
		if !ok || r.runtime == "" || seen[r.runtime] {
			continue
		}
		seen[r.runtime] = true
		out = append(out, r.runtime)
	}
	return out
}

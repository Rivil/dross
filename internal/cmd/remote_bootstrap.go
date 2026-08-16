package cmd

import (
	"fmt"

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
	// Present reports that the probe already found Tool on the host.
	Present bool
	// Argv is the install command, as an argv rather than a shell line so it
	// goes through the internal/remote seam's quoting (locked
	// install_transport). Empty when Present or Refusal is set.
	Argv []string
	// Refusal explains why bootstrap will not install Tool. Empty otherwise.
	Refusal string
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

// planRemoteBootstrap decides what bootstrap would do to t, without doing any
// of it.
//
// The tool set comes from remoteMutationTools — the same function doctor's
// Remote section reads — so a repo whose [mutation].adapters allowlist selects
// one adapter never has bootstrap planning for another. Two surfaces disagreeing
// about which adapters a repo has is the failure this shares the function to
// prevent.
//
// A transport failure is returned as an error, never as a plan. A host that
// could not be reached told us nothing about its toolchain, and a plan whose
// every step claimed the tool was absent would propose installing a whole
// toolchain onto a machine that is merely down.
func planRemoteBootstrap(t remote.Target, p *project.Project) ([]bootstrapStep, error) {
	tools, needBy := remoteMutationTools(p)
	if len(tools) == 0 {
		return nil, nil
	}
	// Runtimes are probed alongside the tools in ONE round trip: a second probe
	// would be a second chance for the host to change underneath the answer.
	probe := append(append([]string{}, tools...), bootstrapRuntimes(tools)...)
	ready, err := remoteProbeFn(t, probe)
	if err != nil {
		return nil, fmt.Errorf("cannot plan a bootstrap for %s: %w", t.Host, err)
	}
	missing := map[string]bool{}
	for _, m := range ready.Missing {
		missing[m] = true
	}

	steps := make([]bootstrapStep, 0, len(tools))
	for _, tool := range tools {
		step := bootstrapStep{Tool: tool, Adapter: needBy[tool]}
		switch {
		case !missing[tool]:
			step.Present = true
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

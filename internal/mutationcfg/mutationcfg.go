// Package mutationcfg is the single construction path for mutation adapters:
// which adapters a project runs, what each needs on a machine, and the tuning
// (where the run happens, how parallel it is) every adapter is built with.
//
// `verify`, `doctor` and the survivor drain all consume this package, so a
// knob or an adapter added here reaches all three — and none of them can
// disagree about which adapters a repo has. It imports project, mutation and
// remote only (the locked adapter_home decision): internal/mutation stays
// project-agnostic, and nothing here knows about a cobra command, a stack
// profile or a verify record.
package mutationcfg

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Rivil/dross/internal/mutation"
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/remote"
)

// entry is one adapter in the roster: its name, the binary its run needs on
// whichever machine runs it, how to get that binary, and what goes unmeasured
// without it.
type entry struct {
	name     string
	tool     string
	install  string
	language string
}

// roster is THE adapter list, in the order every consumer walks it. Order is
// pinned because doctor's output is diffed run to run, and because an
// allowlist filters this sequence rather than re-ordering it.
var roster = []entry{
	{name: "stryker", tool: "npx", install: "install Node 20+ (https://nodejs.org) — npx ships with it", language: "TypeScript/JavaScript/Svelte"},
	{name: "gremlins", tool: "gremlins", install: "go install github.com/go-gremlins/gremlins/cmd/gremlins@latest", language: "Go"},
	{name: "stryker-net", tool: "dotnet", install: "install the .NET SDK (https://dotnet.microsoft.com/download)", language: "C#"},
}

// Selected returns the adapter names the project runs, in roster order. This
// is the one allowlist rule: an empty [mutation] adapters list means every
// adapter, a non-empty one keeps exactly those named. Configured, Tools and
// Missing all derive from it, so they cannot disagree.
func Selected(p *project.Project) []string {
	allowed := map[string]bool{}
	for _, name := range p.Mutation.Adapters {
		allowed[name] = true
	}
	var out []string
	for _, e := range roster {
		if len(allowed) > 0 && !allowed[e.name] {
			continue
		}
		out = append(out, e.name)
	}
	return out
}

// selected is Selected as roster entries.
func selected(p *project.Project) []entry {
	names := Selected(p)
	out := make([]entry, 0, len(names))
	for _, e := range roster {
		for _, n := range names {
			if e.name == n {
				out = append(out, e)
			}
		}
	}
	return out
}

// Tuning is the machine-local half of every adapter's construction: WHERE the
// run happens, and how parallel it is.
//
// It exists because there are two construction sites — Configured here and
// the survivor drain's gremlins run — and a knob added to one of them only is
// a run that behaves differently depending on which command you reached it
// through. One table read once, applied at both.
type Tuning struct {
	// Prefix is the local runtime prefix, and is EMPTY whenever Target is set.
	Prefix string
	// Target is the granted remote, with Cores filled in by the probe. Nil runs
	// locally.
	Target *remote.Target
	// Workers and TestCPU are the machine-local overrides. Zero means unset,
	// which the adapters read as "apply your own default" — not as zero.
	Workers int
	TestCPU int
	// FellBackFrom names the host this run meant to use and could not reach;
	// FallbackWhy is the reason. Both empty on an ordinary run of either kind.
	//
	// They are carried rather than dropped because a fallback's numbers were
	// measured HERE while a remote measurement was expected — and a record that
	// says only "local" loses the fact that the expectation went unmet.
	FellBackFrom string
	FallbackWhy  string
}

// Gremlins is the single Gremlins constructor. Both sites go through it, so a
// knob can only be added in one place.
func (mt Tuning) Gremlins(projectRoot string, p *project.Project, cacheVars []string) *mutation.Gremlins {
	return &mutation.Gremlins{
		CacheVars:          cacheVars,
		Prefix:             mt.Prefix,
		ProjectRoot:        projectRoot,
		TimeoutCoefficient: p.Mutation.Gremlins.TimeoutCoefficient,
		Workers:            mt.Workers,
		TestCPU:            mt.TestCPU,
		Remote:             mt.Target,
	}
}

// Selection is what a Source's Select learned about the chosen host: its core
// count when one answered, or that none did and why.
type Selection struct {
	Cores    int
	Fallback bool
	Why      string
}

// Source is the already-read seam the caller fills: where grants, tuning
// knobs, the host walk and the stack's cache vars come from. internal/cmd
// wires it to local.toml, the remote pool walk and the stack profile; a test
// hands in literals. Every func may be nil, which reads as "nothing" — no
// grants, no overrides, no cache vars.
type Source struct {
	// Grants returns the authorized remote targets in declared order.
	Grants func() ([]*remote.Target, error)
	// Tuning returns the machine-local worker and test-CPU overrides.
	Tuning func() (workers, testCPU int, err error)
	// Select walks the granted targets and returns the first usable one with
	// its Selection, a nil target with Fallback set when none could be reached,
	// or an error when a host answered and the answer was a failure.
	Select func(targets []*remote.Target) (*remote.Target, Selection, error)
	// CacheVars returns the toolchain cache variables the stack declares.
	CacheVars func(p *project.Project, repoDir string) []string
}

// ResolveTuning reads the grant and the tuning knobs, and probes the remote
// once for the core count the worker default derives from.
//
// The probe is unconditional rather than only-when-workers-is-unset, and that is
// the point: it doubles as the reachability pre-flight. A grant that cannot be
// reached must abort the command HERE, before a tree is pushed and before any
// adapter runs, rather than surfacing as an empty report the run cannot
// distinguish from "nothing to measure".
//
// A grant DROPS the docker prefix (the locked docker_prefix_under_remote
// decision) rather than refusing on it. DockerPrefix gates on runtime.mode,
// which describes the DEV stack and says nothing about where mutation runs — so
// aborting on the combination would refuse every docker-mode repo that grants a
// remote, which is the common case and the one this exists for. Shedding the
// prefix is also exactly right: the point is to run on the remote's OWN
// toolchain, and whether that toolchain is present is doctor's question.
func ResolveTuning(p *project.Project, root string, src Source) (Tuning, error) {
	var targets []*remote.Target
	if src.Grants != nil {
		var err error
		if targets, err = src.Grants(); err != nil {
			return Tuning{}, err
		}
	}
	var mt Tuning
	if src.Tuning != nil {
		workers, testCPU, err := src.Tuning()
		if err != nil {
			return Tuning{}, err
		}
		mt.Workers, mt.TestCPU = workers, testCPU
	}
	if len(targets) == 0 {
		mt.Prefix = DockerPrefix(p)
		return mt, nil
	}
	if src.Select == nil {
		return Tuning{}, fmt.Errorf("remote mutation host %s is not usable: no host selector configured", targets[0].Host)
	}
	// Walks the authorized hosts in order and takes the first that answers.
	// With one candidate this is exactly the previous behaviour.
	target, sel, perr := src.Select(targets)
	if perr != nil {
		return Tuning{}, fmt.Errorf(
			"remote mutation host %s is not usable: %w\n"+
				"Nothing was measured. Check ssh access, run `dross doctor`, or withdraw the grant with `dross mutation remote revoke`.",
			targets[0].Host, perr)
	}
	if target == nil {
		// A host we could not REACH gives no answer, and the local machine
		// still can. Aborting here is what forced `dross remote revoke` as a
		// workaround when helicon was unreachable for hours — the fallback is
		// per-run and touches no config, so the next run probes again.
		mt.Prefix = DockerPrefix(p)
		// The LAST candidate's reason: with one host it is that host's, and
		// with several it is why the final attempt failed, after each earlier
		// skip was already printed.
		mt.FellBackFrom, mt.FallbackWhy = targets[len(targets)-1].Host, sel.Why
		return mt, nil
	}
	target.Cores = sel.Cores
	mt.Target = target
	return mt, nil
}

// Configured returns the mutation adapters appropriate for the project, in
// roster order, with the runtime prefix or the granted remote applied, plus
// the tuning it resolved — the caller needs the latter to record where the
// run's numbers actually came from.
func Configured(p *project.Project, root string, skip bool, src Source) ([]mutation.Adapter, Tuning, error) {
	if skip {
		return nil, Tuning{}, nil // verify still runs — files end up in Skipped
	}
	mt, err := ResolveTuning(p, root, src)
	if err != nil {
		return nil, Tuning{}, err
	}
	// Project root is the directory holding .dross — the repo root — and
	// never the process cwd. FindRoot walks UP to find .dross, so a verify
	// launched from a subdirectory still resolves the project; but every
	// adapter's ProjectRoot is also the rsync SOURCE a remote run pushes onto
	// the granted workdir with --delete. With cwd here, `cd web && dross
	// verify` synced web/ over the whole remote tree and deleted everything
	// beside it (feastahead on helicon, 2026-09-13). For docker mode the
	// report is read through the bind-mounted fs at the same root; if a
	// volume layout ever diverges, this is where to surface config.
	repoRoot := filepath.Dir(root)
	var cacheVars []string
	if src.CacheVars != nil {
		cacheVars = src.CacheVars(p, repoRoot)
	}
	// [mutation] adapters = [...] allowlist: files whose adapter is filtered
	// out fall into verify's existing Skipped path downstream.
	var out []mutation.Adapter
	for _, e := range selected(p) {
		switch e.name {
		case "stryker":
			out = append(out, &mutation.Stryker{
				Prefix:      mt.Prefix,
				ProjectRoot: repoRoot,
				Workdir:     p.Mutation.Stryker.Workdir,
				Remote:      mt.Target,
				CacheVars:   cacheVars,
				// Only consulted for a remote run, where the host has to install
				// dependencies before stryker can resolve anything. Passed rather
				// than defaulted: installing with the wrong manager produces a tree
				// stryker resolves differently.
				PackageManager: p.Stack.PackageManager,
			})
		case "gremlins":
			out = append(out, mt.Gremlins(repoRoot, p, cacheVars))
		case "stryker-net":
			out = append(out, &mutation.StrykerNet{Prefix: mt.Prefix, ProjectRoot: repoRoot, Remote: mt.Target, CacheVars: cacheVars})
		}
	}
	return out, mt, nil
}

// DockerPrefix returns the runtime command prefix for docker mode.
// For native, returns "". For docker, derives from runtime.test_command
// (which already has the right shape: "docker compose exec app pnpm test").
//
// We strip the trailing runner+args to get the prefix. Field-based
// (not substring) so a container name that happens to match a runner
// name (e.g. "docker compose exec node node test.js") doesn't fool us.
func DockerPrefix(p *project.Project) string {
	if p.Runtime.Mode != "docker" {
		return ""
	}
	tc := p.Runtime.TestCommand
	fields := strings.Fields(tc)
	// The prefix's leading binary must be EXACTLY "docker" — not merely a
	// string starting with "docker" (HasPrefix would accept "dockerevil",
	// promoting an arbitrary PATH binary into the exec prefix built below).
	// project.toml is a committed file, so under clone-and-run this is the
	// difference between a bounded `docker` invocation and arbitrary code.
	if len(fields) == 0 || fields[0] != "docker" {
		return "docker compose exec app"
	}
	runners := map[string]bool{
		"pnpm": true, "npm": true, "yarn": true, "bun": true,
		"node": true, "deno": true,
		"go": true, "make": true,
	}
	// We need at minimum [docker, compose, exec, <service>] before any
	// runner, so start scanning from index 4.
	for i := 4; i < len(fields); i++ {
		if runners[fields[i]] {
			return strings.Join(fields[:i], " ")
		}
	}
	return "docker compose exec app"
}

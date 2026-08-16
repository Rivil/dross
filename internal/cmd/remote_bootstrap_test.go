package cmd

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/remote"
)

var bootstrapTarget = remote.Target{Host: "helicon", Workdir: "/home/rivil/dross"}

// bootstrapProject builds a project whose [mutation].adapters allowlist is
// exactly adapters (empty = every adapter, which is what configuredAdapters
// means by an empty allowlist).
func bootstrapProject(adapters ...string) *project.Project {
	p := &project.Project{}
	p.Mutation.Adapters = adapters
	return p
}

// probeMissing stubs the shared seam so that exactly the named binaries are
// absent from the host.
func probeMissing(t *testing.T, missing ...string) *[]string {
	t.Helper()
	var asked []string
	absent := map[string]bool{}
	for _, m := range missing {
		absent[m] = true
	}
	fakeProbe(t, func(_ remote.Target, tools []string) (remote.Readiness, error) {
		asked = append(asked, tools...)
		r := remote.Readiness{Cores: 10}
		for _, tool := range tools {
			if absent[tool] {
				r.Missing = append(r.Missing, tool)
			}
		}
		return r, nil
	})
	return &asked
}

func stepFor(steps []bootstrapStep, tool string) (bootstrapStep, bool) {
	for _, s := range steps {
		if s.Tool == tool {
			return s, true
		}
	}
	return bootstrapStep{}, false
}

// TestPlanRefusesMissingRuntime: the locked install_scope line. Installing a
// language runtime is host administration — version policy, PATH ownership,
// usually root — so bootstrap names it and stops. A refusal that named nothing
// would leave the host's owner with "it didn't work".
func TestPlanRefusesMissingRuntime(t *testing.T) {
	probeMissing(t, "gremlins", "go")

	steps, err := planRemoteBootstrap(bootstrapTarget, bootstrapProject("gremlins"))
	if err != nil {
		t.Fatal(err)
	}
	s, ok := stepFor(steps, "gremlins")
	if !ok {
		t.Fatalf("no step for gremlins: %+v", steps)
	}
	if len(s.Argv) != 0 {
		t.Errorf("bootstrap planned %v on a host with no go — install_scope forbids it", s.Argv)
	}
	if s.Refusal == "" {
		t.Fatal("a missing runtime produced neither an install nor a refusal")
	}
	if !strings.Contains(s.Refusal, "go") {
		t.Errorf("the refusal does not name the runtime to install: %q", s.Refusal)
	}
	if s.Present {
		t.Error("a missing tool was reported as present")
	}
}

// TestPlanInstallsAdapterIntoAnExistingRuntime: the other side of the same
// line. gremlins is a Go PACKAGE, and installing it into a toolchain that is
// already there is the same `go install` the local developer ran.
func TestPlanInstallsAdapterIntoAnExistingRuntime(t *testing.T) {
	probeMissing(t, "gremlins") // go is present

	steps, err := planRemoteBootstrap(bootstrapTarget, bootstrapProject("gremlins"))
	if err != nil {
		t.Fatal(err)
	}
	s, _ := stepFor(steps, "gremlins")
	if s.Refusal != "" {
		t.Errorf("refused with a runtime present: %q", s.Refusal)
	}
	if len(s.Argv) == 0 {
		t.Fatal("no install planned for a missing adapter package")
	}
	if s.Argv[0] != "go" || s.Argv[1] != "install" {
		t.Errorf("argv = %v, want a `go install`", s.Argv)
	}
	// Pinned, not @latest: two hosts bootstrapped a week apart must run the
	// same mutator, and an unpinned spec is a supply-chain hole.
	last := s.Argv[len(s.Argv)-1]
	if strings.HasSuffix(last, "@latest") || !strings.Contains(last, "@v") {
		t.Errorf("install spec %q is not pinned to a version", last)
	}
}

// TestPlanSkipsPresentTools: a second bootstrap over a provisioned host plans
// nothing. A verb that reinstalled on every run would make `--apply` unsafe to
// repeat, which is the property that lets ship or a script call it.
func TestPlanSkipsPresentTools(t *testing.T) {
	probeMissing(t) // nothing missing

	steps, err := planRemoteBootstrap(bootstrapTarget, bootstrapProject("gremlins", "stryker", "stryker-net"))
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) == 0 {
		t.Fatal("no steps planned at all — the tool set is empty")
	}
	for _, s := range steps {
		if !s.Present {
			t.Errorf("%s: not marked present on a fully provisioned host (%+v)", s.Tool, s)
		}
		if len(s.Argv) != 0 || s.Refusal != "" {
			t.Errorf("%s: a present tool carries work: argv=%v refusal=%q", s.Tool, s.Argv, s.Refusal)
		}
	}
}

// TestPlanUsesConfiguredAdapters: bootstrap and doctor must never disagree
// about which adapters a repo has, so both read remoteMutationTools. A second,
// slightly different tool list is how bootstrap ends up provisioning for an
// adapter that never runs.
func TestPlanUsesConfiguredAdapters(t *testing.T) {
	for _, adapters := range [][]string{
		{"gremlins"},
		{"stryker"},
		{"gremlins", "stryker-net"},
		nil, // empty allowlist = every adapter
	} {
		name := strings.Join(adapters, "+")
		if name == "" {
			name = "empty allowlist"
		}
		t.Run(name, func(t *testing.T) {
			p := bootstrapProject(adapters...)
			probeMissing(t)

			steps, err := planRemoteBootstrap(bootstrapTarget, p)
			if err != nil {
				t.Fatal(err)
			}
			want, _ := remoteMutationTools(p)
			var got []string
			for _, s := range steps {
				got = append(got, s.Tool)
			}
			sort.Strings(want)
			sort.Strings(got)
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Errorf("planned for %v, want doctor's %v", got, want)
			}
		})
	}
}

// TestPlanStepsNameTheirTool: the vocabulary is exactly three states, and a
// step must be in one of them. A step with neither an argv nor a refusal is a
// tool the run would silently skip; one with both is a plan that contradicts
// itself.
func TestPlanStepsNameTheirTool(t *testing.T) {
	for _, missing := range [][]string{
		{},
		{"gremlins"},
		{"gremlins", "go"},
		{"npx"},
		{"dotnet"},
		{"gremlins", "npx", "dotnet", "go"},
	} {
		t.Run(strings.Join(missing, "+"), func(t *testing.T) {
			probeMissing(t, missing...)

			steps, err := planRemoteBootstrap(bootstrapTarget, bootstrapProject())
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range steps {
				if s.Tool == "" {
					t.Errorf("a step names no tool: %+v", s)
				}
				if s.Adapter == "" {
					t.Errorf("%s: no adapter named — the reader cannot tell why the host needs it", s.Tool)
				}
				has := 0
				if s.Present {
					has++
				}
				if len(s.Argv) > 0 {
					has++
				}
				if s.Refusal != "" {
					has++
				}
				if has != 1 {
					t.Errorf("%s: %d of present/argv/refusal set, want exactly 1: %+v", s.Tool, has, s)
				}
			}
		})
	}
}

// TestPlanRefusesRuntimeTools: npx and dotnet are runtimes wearing a tool name.
// Planning an install for either would be bootstrap installing Node or the .NET
// SDK, which install_scope forbids outright.
func TestPlanRefusesRuntimeTools(t *testing.T) {
	probeMissing(t, "npx", "dotnet")

	steps, err := planRemoteBootstrap(bootstrapTarget, bootstrapProject("stryker", "stryker-net"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"npx", "dotnet"} {
		s, ok := stepFor(steps, tool)
		if !ok {
			t.Fatalf("no step for %s: %+v", tool, steps)
		}
		if len(s.Argv) != 0 {
			t.Errorf("%s: bootstrap planned %v — that is installing a language runtime", tool, s.Argv)
		}
		if s.Refusal == "" {
			t.Errorf("%s: refused silently", tool)
		}
	}
}

// TestPlanTransportFailureIsNotMissingTools: a host that could not be reached
// told us nothing. Reporting it as "every tool is absent" would propose
// installing a whole toolchain onto a machine that is merely down.
func TestPlanTransportFailureIsNotMissingTools(t *testing.T) {
	fakeProbe(t, func(remote.Target, []string) (remote.Readiness, error) {
		return remote.Readiness{}, fmt.Errorf("ssh: connect: %w", remote.ErrTransport)
	})

	steps, err := planRemoteBootstrap(bootstrapTarget, bootstrapProject())
	if err == nil {
		t.Fatalf("an unreachable host produced a plan: %+v", steps)
	}
	if !errors.Is(err, remote.ErrTransport) {
		t.Errorf("err = %v, want the transport class preserved", err)
	}
	if !strings.Contains(err.Error(), bootstrapTarget.Host) {
		t.Errorf("err = %v, want the host named", err)
	}
	if len(steps) != 0 {
		t.Errorf("an unreachable host returned %d step(s)", len(steps))
	}
}

// TestPlanProbesRuntimesInOneRoundTrip: the runtimes are probed alongside the
// tools. A second probe would be a second chance for the host to change
// underneath the answer, and would double the latency of the common case.
func TestPlanProbesRuntimesInOneRoundTrip(t *testing.T) {
	asked := probeMissing(t)

	if _, err := planRemoteBootstrap(bootstrapTarget, bootstrapProject("gremlins")); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(*asked, ",")
	for _, want := range []string{"gremlins", "go"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the probe did not ask about %q: %v", want, *asked)
		}
	}
}

// TestPlanUnknownToolFailsClosed: a tool nobody wrote a recipe for is refused
// by name, never silently skipped. Adding an adapter must not quietly add an
// unprovisionable tool that bootstrap reports nothing about.
func TestPlanUnknownToolFailsClosed(t *testing.T) {
	orig := bootstrapRecipes
	bootstrapRecipes = map[string]bootstrapRecipe{} // every tool now unknown
	t.Cleanup(func() { bootstrapRecipes = orig })
	probeMissing(t, "gremlins")

	steps, err := planRemoteBootstrap(bootstrapTarget, bootstrapProject("gremlins"))
	if err != nil {
		t.Fatal(err)
	}
	s, _ := stepFor(steps, "gremlins")
	if s.Refusal == "" {
		t.Fatal("a tool with no recipe produced no refusal — it would be silently skipped")
	}
	if !strings.Contains(s.Refusal, "gremlins") {
		t.Errorf("the refusal does not name the tool: %q", s.Refusal)
	}
}

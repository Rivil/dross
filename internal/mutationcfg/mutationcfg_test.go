package mutationcfg

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/mutation"
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/remote"
)

// configuredNames runs Configured and flattens the result to Name()s, failing
// on error so a helper that dropped the error could not let a test pass on an
// empty list.
func configuredNames(t *testing.T, p *project.Project, skip bool, src Source) []string {
	t.Helper()
	as, _, err := Configured(p, "", skip, src)
	if err != nil {
		t.Fatalf("Configured: %v", err)
	}
	return names(as)
}

func names(as []mutation.Adapter) []string {
	var out []string
	for _, a := range as {
		out = append(out, a.Name())
	}
	return out
}

// TestDockerPrefixDerivation pins DockerPrefix's behaviour so a refactor of
// TestCommand parsing doesn't silently break docker-routed mutation runs.
func TestDockerPrefixDerivation(t *testing.T) {
	cases := []struct {
		mode, testCmd, want string
	}{
		{"native", "pnpm test", ""},
		{"docker", "docker compose exec app pnpm test", "docker compose exec app"},
		{"docker", "docker compose exec app npm test", "docker compose exec app"},
		{"docker", "docker compose exec api yarn test", "docker compose exec api"},
		{"docker", "docker compose exec app bun test", "docker compose exec app"},
		{"docker", "docker compose exec node node test.js", "docker compose exec node"},
		// docker mode but unrecognised runner — falls back to default
		{"docker", "weird invocation", "docker compose exec app"},
		// docker mode with no test_command at all — falls back to default
		{"docker", "", "docker compose exec app"},
		// self-audit inj-4a: a binary that merely STARTS WITH "docker"
		// (dockerevil) must NOT be promoted into the exec prefix — the
		// leading field has to be exactly "docker". Falls back to default.
		{"docker", "dockerevil compose exec app pnpm test", "docker compose exec app"},
		{"docker", "docker-malicious run --privileged x", "docker compose exec app"},
	}
	for _, c := range cases {
		p := &project.Project{Runtime: project.Runtime{Mode: c.mode, TestCommand: c.testCmd}}
		if got := DockerPrefix(p); got != c.want {
			t.Errorf("DockerPrefix(mode=%q, test=%q) = %q want %q", c.mode, c.testCmd, got, c.want)
		}
	}
}

// TestConfiguredAdaptersAllowlist pins the [mutation] adapters escape hatch:
// empty means all adapters; non-empty filters by Name(), so a polyglot repo
// can run only the adapter that's actually set up.
func TestConfiguredAdaptersAllowlist(t *testing.T) {
	p := &project.Project{}
	if got := configuredNames(t, p, false, Source{}); len(got) != 3 {
		t.Fatalf("empty allowlist must return all adapters, got %v", got)
	}

	p.Mutation.Adapters = []string{"gremlins"}
	got := configuredNames(t, p, false, Source{})
	if len(got) != 1 || got[0] != "gremlins" {
		t.Errorf("allowlist [gremlins] must filter to gremlins only, got %v", got)
	}

	p.Mutation.Adapters = []string{"stryker", "gremlins"}
	got = configuredNames(t, p, false, Source{})
	if len(got) != 2 {
		t.Errorf("allowlist [stryker gremlins] must keep both, got %v", got)
	}

	skipped, _, err := Configured(p, "", true, Source{})
	if err != nil {
		t.Fatalf("--skip-mutation returned an error: %v", err)
	}
	if skipped != nil {
		t.Errorf("--skip-mutation must still return nil regardless of allowlist, got %v", names(skipped))
	}
}

// TestToolsAndAdaptersShareTheRoster: Tools(p) and Configured(p) derive from
// one roster, so for every allowlist the tool sequence equals the roster tool
// of each adapter Configured returns, in order. A side that grew its own list
// is named.
func TestToolsAndAdaptersShareTheRoster(t *testing.T) {
	toolOf := map[string]string{}
	for _, e := range roster {
		toolOf[e.name] = e.tool
	}
	for _, allow := range [][]string{
		{},
		{"gremlins"},
		{"stryker", "stryker-net"},
		{"stryker-net", "gremlins"},
	} {
		p := &project.Project{}
		p.Mutation.Adapters = allow
		tools, needBy := Tools(p)
		adapters := configuredNames(t, p, false, Source{})
		var fromAdapters []string
		for _, name := range adapters {
			fromAdapters = append(fromAdapters, toolOf[name])
		}
		if !reflect.DeepEqual(tools, fromAdapters) {
			t.Errorf("allowlist %v: Tools = %v but Configured's adapters need %v — the roster is no longer shared (Tools side: %v, Configured side: %v)",
				allow, tools, fromAdapters, tools, adapters)
		}
		for tool, adapter := range needBy {
			if toolOf[adapter] != tool {
				t.Errorf("allowlist %v: needBy[%s] = %s, but the roster says %s needs %s", allow, tool, adapter, adapter, toolOf[adapter])
			}
		}
		if got := Selected(p); !reflect.DeepEqual(got, adapters) {
			t.Errorf("allowlist %v: Selected = %v, Configured = %v", allow, got, adapters)
		}
	}
}

// TestResolveTuningFallsBackWhenUnreached pins the three arms of the host walk
// through the Source seam: a fallback keeps the docker prefix and names the
// host; a failing host aborts with the "not usable" error and no adapters; a
// reached host carries its core count and sheds the prefix.
func TestResolveTuningFallsBackWhenUnreached(t *testing.T) {
	p := &project.Project{Runtime: project.Runtime{Mode: "docker", TestCommand: "docker compose exec app go test ./..."}}
	host := &remote.Target{Host: "helicon", Workdir: "/srv/dross"}
	grants := func() ([]*remote.Target, error) { return []*remote.Target{host}, nil }

	t.Run("fallback keeps the prefix and names the host", func(t *testing.T) {
		src := Source{
			Grants: grants,
			Select: func([]*remote.Target) (*remote.Target, Selection, error) {
				return nil, Selection{Fallback: true, Why: "helicon unreachable: ssh exit 255"}, nil
			},
		}
		mt, err := ResolveTuning(p, "", src)
		if err != nil {
			t.Fatalf("ResolveTuning: %v", err)
		}
		if mt.Prefix != DockerPrefix(p) {
			t.Errorf("Prefix = %q, want the docker prefix %q on fallback", mt.Prefix, DockerPrefix(p))
		}
		if mt.FellBackFrom != "helicon" || mt.FallbackWhy != "helicon unreachable: ssh exit 255" {
			t.Errorf("fallback provenance = (%q, %q)", mt.FellBackFrom, mt.FallbackWhy)
		}
		if mt.Target != nil {
			t.Errorf("Target = %+v, want nil on fallback", mt.Target)
		}
	})

	t.Run("a failing host aborts with no adapters", func(t *testing.T) {
		src := Source{
			Grants: grants,
			Select: func([]*remote.Target) (*remote.Target, Selection, error) {
				return nil, Selection{}, errors.New("probe: workdir not writable")
			},
		}
		adapters, _, err := Configured(p, "", false, src)
		if err == nil {
			t.Fatal("a host whose probe failed did not abort")
		}
		if !strings.Contains(err.Error(), "remote mutation host helicon is not usable") {
			t.Errorf("error = %v", err)
		}
		if !strings.Contains(err.Error(), "workdir not writable") {
			t.Errorf("error lost the probe's reason: %v", err)
		}
		if adapters != nil {
			t.Errorf("adapters = %v, want none after an abort", adapters)
		}
	})

	t.Run("a reached host carries its cores and sheds the prefix", func(t *testing.T) {
		src := Source{
			Grants: grants,
			Select: func(ts []*remote.Target) (*remote.Target, Selection, error) {
				return ts[0], Selection{Cores: 8}, nil
			},
		}
		mt, err := ResolveTuning(p, "", src)
		if err != nil {
			t.Fatalf("ResolveTuning: %v", err)
		}
		if mt.Target == nil || mt.Target.Cores != 8 {
			t.Fatalf("Target = %+v, want helicon with Cores 8", mt.Target)
		}
		if mt.Prefix != "" {
			t.Errorf("Prefix = %q, want empty under a grant", mt.Prefix)
		}
		if mt.FellBackFrom != "" {
			t.Errorf("FellBackFrom = %q on a reached host", mt.FellBackFrom)
		}
	})

	t.Run("no grants runs locally with the prefix and the knobs", func(t *testing.T) {
		src := Source{Tuning: func() (int, int, error) { return 3, 2, nil }}
		mt, err := ResolveTuning(p, "", src)
		if err != nil {
			t.Fatal(err)
		}
		if mt.Prefix != DockerPrefix(p) || mt.Target != nil || mt.Workers != 3 || mt.TestCPU != 2 {
			t.Errorf("local tuning = %+v", mt)
		}
	})

	t.Run("a grant reader error aborts", func(t *testing.T) {
		src := Source{Grants: func() ([]*remote.Target, error) { return nil, errors.New("local.toml is tracked") }}
		if _, err := ResolveTuning(p, "", src); err == nil || !strings.Contains(err.Error(), "local.toml is tracked") {
			t.Errorf("grant error not surfaced: %v", err)
		}
	})
}

// TestDrainAndVerifyBuildTheSameGremlins: constructing via Configured and via
// Tuning.Gremlins with identical inputs yields deep-equal *mutation.Gremlins —
// the drain's classification and verify's measurement share one constructor.
func TestDrainAndVerifyBuildTheSameGremlins(t *testing.T) {
	p := &project.Project{}
	p.Mutation.Adapters = []string{"gremlins"}
	p.Mutation.Gremlins.TimeoutCoefficient = 3
	host := &remote.Target{Host: "helicon", Workdir: "/srv/dross"}
	src := Source{
		Grants: func() ([]*remote.Target, error) { return []*remote.Target{host}, nil },
		Tuning: func() (int, int, error) { return 6, 2, nil },
		Select: func(ts []*remote.Target) (*remote.Target, Selection, error) { return ts[0], Selection{Cores: 16}, nil },
		CacheVars: func(*project.Project, string) []string {
			return []string{"GOCACHE", "GOMODCACHE"}
		},
	}
	root := filepath.Join(t.TempDir(), ".dross")

	adapters, mt, err := Configured(p, root, false, src)
	if err != nil {
		t.Fatal(err)
	}
	if len(adapters) != 1 {
		t.Fatalf("want exactly the gremlins adapter, got %d", len(adapters))
	}
	fromVerify, ok := adapters[0].(*mutation.Gremlins)
	if !ok {
		t.Fatalf("adapter is %T, want *mutation.Gremlins", adapters[0])
	}
	fromDrain := mt.Gremlins(filepath.Dir(root), p, src.CacheVars(p, filepath.Dir(root)))
	if !reflect.DeepEqual(fromVerify, fromDrain) {
		t.Errorf("the two construction sites disagree:\n verify: %+v\n drain:  %+v", fromVerify, fromDrain)
	}
	if fromVerify.Workers != 6 || fromVerify.TestCPU != 2 || fromVerify.Remote == nil || fromVerify.Remote.Cores != 16 || fromVerify.Prefix != "" || fromVerify.TimeoutCoefficient != 3 {
		t.Errorf("gremlins lost a knob: %+v", fromVerify)
	}
	if !reflect.DeepEqual(fromVerify.CacheVars, []string{"GOCACHE", "GOMODCACHE"}) {
		t.Errorf("cache vars = %v", fromVerify.CacheVars)
	}
}

// TestConfiguredThreadsCacheVars: the Source's cache vars reach every adapter,
// and a nil CacheVars reads as none rather than panicking.
func TestConfiguredThreadsCacheVars(t *testing.T) {
	p := &project.Project{}
	src := Source{CacheVars: func(*project.Project, string) []string { return []string{"GOCACHE"} }}
	adapters, _, err := Configured(p, "", false, src)
	if err != nil {
		t.Fatal(err)
	}
	if len(adapters) != 3 {
		t.Fatalf("want 3 adapters, got %d", len(adapters))
	}
	for _, a := range adapters {
		var vars []string
		switch v := a.(type) {
		case *mutation.Gremlins:
			vars = v.CacheVars
		case *mutation.Stryker:
			vars = v.CacheVars
		case *mutation.StrykerNet:
			vars = v.CacheVars
		}
		if !reflect.DeepEqual(vars, []string{"GOCACHE"}) {
			t.Errorf("adapter %s carries %v, want GOCACHE", a.Name(), vars)
		}
	}
	if _, _, err := Configured(p, "", false, Source{}); err != nil {
		t.Errorf("an empty Source failed: %v", err)
	}
}

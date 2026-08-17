package mutation

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/remote"
)

func scratchLauncher(t *testing.T, target *remote.Target, vars []string) *Launcher {
	t.Helper()
	root := t.TempDir()
	l, err := newLauncher("gremlins", "", target, root, "", vars)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l
}

func localOf(argv []string) *exec.Cmd { return exec.Command(argv[0], argv[1:]...) }

func envValue(cmd *exec.Cmd, name string) (string, bool) {
	// LAST occurrence wins, which is what exec itself does — asserting on the
	// first would report an overridden ambient value as the effective one.
	val, found := "", false
	for _, e := range cmd.Env {
		if n, v, ok := strings.Cut(e, "="); ok && n == name {
			val, found = v, true
		}
	}
	return val, found
}

// TestLocalRunUsesScratchCache: a local run compiles into its own cache, not the
// developer's. The ambient value must be overridden rather than merely
// accompanied — exec resolves a repeated name to the last one, so order is the
// mechanism here.
func TestLocalRunUsesScratchCache(t *testing.T) {
	t.Setenv("GOCACHE", "/home/dev/Library/Caches/go-build")
	l := scratchLauncher(t, nil, []string{"GOCACHE"})

	cmd, err := l.toolCmd([]string{"gremlins", "unleash"}, localOf)
	if err != nil {
		t.Fatal(err)
	}

	got, ok := envValue(cmd, "GOCACHE")
	if !ok {
		t.Fatal("the command carries no GOCACHE at all")
	}
	if got == "/home/dev/Library/Caches/go-build" {
		t.Error("the run inherited the developer's shared cache")
	}
	if l.scratch == nil || got != l.scratch.Dir {
		t.Errorf("GOCACHE = %q, want the scratch dir", got)
	}
	if _, err := os.Stat(got); err != nil {
		t.Errorf("the scratch dir does not exist: %v", err)
	}
	// The rest of the environment has to survive, or the tool loses PATH.
	if _, ok := envValue(cmd, "PATH"); !ok {
		t.Error("the command lost the ambient environment")
	}
}

// TestNoCacheVarsLeavesTheCommandUnchanged: a stack whose profile declares none
// must be byte-identical to what it was before this existed. That is what keeps
// c-6 true for every stack that is not Go.
func TestNoCacheVarsLeavesTheCommandUnchanged(t *testing.T) {
	l := scratchLauncher(t, nil, nil)

	cmd, err := l.toolCmd([]string{"gremlins", "unleash"}, localOf)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Env != nil {
		t.Errorf("a run with no declared cache vars had its Env rewritten: %v", cmd.Env)
	}
	if l.scratch != nil {
		t.Error("a scratch was created for a stack that declared no cache var")
	}
	entries, err := os.ReadDir(l.ProjectRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("a no-op run left %v beside the project", entries)
	}
}

// TestRemoteScriptExportsScratch: the remote transport carries the same
// redirection, through the script rather than through cmd.Env.
func TestRemoteScriptExportsScratch(t *testing.T) {
	target := &remote.Target{Host: "helicon", Workdir: "/home/rivil/dross"}
	l := scratchLauncher(t, target, []string{"GOCACHE"})

	tt, err := l.toolTarget()
	if err != nil {
		t.Fatal(err)
	}
	script, err := remote.Script(tt, []string{"gremlins", "unleash"})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(script, "export GOCACHE=") {
		t.Errorf("the remote script exports no GOCACHE:\n%s", script)
	}
	want := remoteScratchDir(target.Workdir)
	if !strings.Contains(script, want) {
		t.Errorf("the script does not point the cache at %q:\n%s", want, script)
	}
}

// TestRemoteScriptExportsTmpdir: gremlins copies the whole module via
// os.MkdirTemp, which honours TMPDIR and NOT GOTMPDIR — so redirecting the Go
// toolchain alone covers the compiler and leaves the harness's own scratch
// wherever the host defaults it. On helicon that default is RAM.
func TestRemoteScriptExportsTmpdir(t *testing.T) {
	target := &remote.Target{Host: "helicon", Workdir: "/home/rivil/dross"}
	l := scratchLauncher(t, target, []string{"GOCACHE"})

	tt, err := l.toolTarget()
	if err != nil {
		t.Fatal(err)
	}
	var tmpdir string
	for _, e := range tt.Env {
		if e.Name == "TMPDIR" {
			tmpdir = e.Value
		}
	}
	if tmpdir == "" {
		t.Fatal("the remote run exports no TMPDIR — the harness's own copy stays on the host's default volume")
	}
	if !strings.HasPrefix(tmpdir, target.Workdir+"/") {
		t.Errorf("TMPDIR = %q, want a path under the granted workdir", tmpdir)
	}
}

// TestRemoteScratchIsUnderWorkdir: never the host's temp. This is the locked
// scratch_location decision stated as an assertion.
func TestRemoteScratchIsUnderWorkdir(t *testing.T) {
	target := &remote.Target{Host: "helicon", Workdir: "/home/rivil/dross"}
	l := scratchLauncher(t, target, []string{"GOCACHE", "GOMODCACHE"})

	tt, err := l.toolTarget()
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, e := range tt.Env {
		switch e.Name {
		case "GOCACHE", "GOMODCACHE", "TMPDIR":
			seen++
			if !strings.HasPrefix(e.Value, target.Workdir+"/") {
				t.Errorf("%s = %q, which is not under the granted workdir", e.Name, e.Value)
			}
			if strings.HasPrefix(e.Value, "/tmp") || strings.HasPrefix(e.Value, "/var/tmp") {
				t.Errorf("%s = %q landed in a host temp path — on helicon that is a 32 G tmpfs in RAM", e.Name, e.Value)
			}
		}
	}
	if seen != 3 {
		t.Errorf("exported %d of the 3 expected vars", seen)
	}
}

// TestRemoteRunWithNoCacheVarsExportsNothing: the no-op case on the remote side
// too — a stack declaring none must not gain a TMPDIR it never had.
func TestRemoteRunWithNoCacheVarsExportsNothing(t *testing.T) {
	target := &remote.Target{Host: "helicon", Workdir: "/home/rivil/dross"}
	l := scratchLauncher(t, target, nil)

	tt, err := l.toolTarget()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range tt.Env {
		if e.Name == "TMPDIR" || e.Name == "GOCACHE" {
			t.Errorf("a stack with no declared cache var exported %s=%s", e.Name, e.Value)
		}
	}
}

// TestLauncherCloseWipesScratch: every exit path calls Close, and it is
// idempotent because a deferred wipe must not fight an explicit one.
func TestLauncherCloseWipesScratch(t *testing.T) {
	root := t.TempDir()
	l, err := newLauncher("gremlins", "", nil, root, "", []string{"GOCACHE"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.toolCmd([]string{"gremlins"}, localOf); err != nil {
		t.Fatal(err)
	}
	dir := l.scratch.Dir
	if dir == "" {
		t.Fatal("no scratch was created, so this proves nothing")
	}
	if err := os.WriteFile(filepath.Join(dir, "archive.a"), []byte("compiled"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("the scratch survived Close: %v", err)
	}
	if err := l.Close(); err != nil {
		t.Errorf("a second Close errored: %v", err)
	}
	// A nil launcher is the early-return path some adapters take.
	var none *Launcher
	if err := none.Close(); err != nil {
		t.Errorf("Close on a nil launcher: %v", err)
	}
}

// TestScratchIsReusedWithinARun: one run, one cache. Creating a fresh directory
// per package would defeat the point — gremlins is invoked once per package and
// the whole run is meant to share the scratch.
func TestScratchIsReusedWithinARun(t *testing.T) {
	l := scratchLauncher(t, nil, []string{"GOCACHE"})

	first, err := l.toolCmd([]string{"gremlins", "unleash", "./a"}, localOf)
	if err != nil {
		t.Fatal(err)
	}
	second, err := l.toolCmd([]string{"gremlins", "unleash", "./b"}, localOf)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := envValue(first, "GOCACHE")
	b, _ := envValue(second, "GOCACHE")
	if a == "" || a != b {
		t.Errorf("two packages in one run used different caches: %q vs %q", a, b)
	}
}

package mutation

import (
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/remote"
)

// c-7, executed: a remote run restores the language's dependencies on the
// remote before the adapter runs, using the lockfile-respecting form of the
// project's OWN package manager.
//
// The failure this prevents is quiet and looks like success. Stryker on a tree
// with no node_modules kills every mutant — nothing imports, so every mutation
// fails at load — and the leg reports an excellent score for a suite that never
// ran. A restore with the WRONG manager is subtler still: pnpm's symlinked
// layout is load-bearing (a pnpm repo's stryker.conf.json declares its plugins
// explicitly BECAUSE that layout defeats auto-discovery), so an npm install
// produces a tree stryker resolves differently.

// remoteScripts returns the shell script of every recorded ssh command.
func remoteScripts(rec [][]string) []string {
	var out []string
	for _, argv := range rec {
		if s := remoteScript(argv); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// indexOfScript returns the position of the first recorded ssh script
// containing sub, or -1.
func indexOfScript(rec [][]string, sub string) int {
	for i, s := range remoteScripts(rec) {
		if strings.Contains(s, sub) {
			return i
		}
	}
	return -1
}

func remoteStryker(root, workdir, pm string) *Stryker {
	return &Stryker{
		ProjectRoot:    root,
		Workdir:        workdir,
		PackageManager: pm,
		Remote:         &remote.Target{Host: "helicon", Workdir: "/srv/dross"},
	}
}

// TestStrykerRemoteRestoreUsesTheProjectsOwnPackageManager pins each restore as
// an EXACT argv, per manager.
//
// Hardcoding npm is not hypothetical: the repo that motivated this phase is
// pnpm with no package-lock.json, where `npm ci` fails outright. And the
// lockfile-respecting form is pinned rather than merely "an install", because
// the loose forms resolve fresh versions when lockfile and manifest disagree —
// measuring a dependency tree the lockfile does not describe.
func TestStrykerRemoteRestoreUsesTheProjectsOwnPackageManager(t *testing.T) {
	for pm, want := range map[string][]string{
		"pnpm": {"pnpm", "install", "--frozen-lockfile"},
		"npm":  {"npm", "ci"},
		"yarn": {"yarn", "install", "--immutable"},
		"bun":  {"bun", "install", "--frozen-lockfile"},
	} {
		t.Run(pm, func(t *testing.T) {
			failLocalSpawns(t)
			rec := recordRemote(t, func(argv []string) {
				if isFetch(argv) {
					if werr := os.WriteFile(argv[3], []byte(strykerPayload), 0o644); werr != nil {
						t.Fatalf("fake fetch: %v", werr)
					}
				}
			})

			s := remoteStryker(t.TempDir(), "", pm)
			if _, err := s.Run([]string{"src/a.ts"}); err != nil {
				t.Fatalf("Run: %v", err)
			}

			// Exact argv, single-quoted by SSHArgs.
			quoted := "'" + strings.Join(want, "' '") + "'"
			idx := indexOfScript(*rec, quoted)
			if idx < 0 {
				t.Fatalf("the restore argv %v is not in the recorded scripts:\n%v", want, remoteScripts(*rec))
			}
			// ...and it precedes the tool.
			tool := indexOfScript(*rec, "'npx'")
			if tool < 0 {
				t.Fatal("stryker was never invoked")
			}
			if idx > tool {
				t.Errorf("the restore ran AFTER stryker (%d vs %d) — stryker would resolve against a tree with no dependencies", idx, tool)
			}
		})
	}
}

// TestStrykerRemoteRestoreRejectsTheLooseForms is the same claim from the other
// side: a restore that resolves fresh versions measures a tree the lockfile does
// not describe, so the loose forms must not appear anywhere in the argv table.
func TestStrykerRemoteRestoreRejectsTheLooseForms(t *testing.T) {
	loose := map[string][]string{
		"npm":  {"npm", "install"},
		"pnpm": {"pnpm", "install"},
		"yarn": {"yarn", "install"},
		"bun":  {"bun", "install"},
	}
	for pm, bad := range loose {
		got, err := nodeRestoreArgv(pm)
		if err != nil {
			t.Fatalf("%s: %v", pm, err)
		}
		if reflect.DeepEqual(got, bad) {
			t.Errorf("%s restores with the loose form %v — it must respect the lockfile", pm, bad)
		}
	}
}

// TestStrykerRemoteRefusesAnUnsetPackageManager: dross will not guess npm.
// Installing with the wrong manager produces a tree stryker resolves
// differently, so an unset setting is a refusal that names the setting.
func TestStrykerRemoteRefusesAnUnsetPackageManager(t *testing.T) {
	failLocalSpawns(t)
	rec := recordRemote(t, nil)

	s := remoteStryker(t.TempDir(), "", "")
	_, err := s.Run([]string{"src/a.ts"})
	if err == nil {
		t.Fatal("an unset stack.package_manager was guessed rather than refused")
	}
	if !strings.Contains(err.Error(), "stack.package_manager") {
		t.Errorf("the refusal does not name the setting: %v", err)
	}
	if strings.Contains(err.Error(), "npm ci") {
		t.Errorf("the refusal suggests a default — dross must not guess: %v", err)
	}
	if len(*rec) != 0 {
		t.Errorf("a refused run still spawned %d commands: %v", len(*rec), *rec)
	}
}

// TestStrykerRemoteRefusesAnUnknownPackageManager: an unrecognised value must
// not silently skip the restore and report against a dependency-less tree.
func TestStrykerRemoteRefusesAnUnknownPackageManager(t *testing.T) {
	failLocalSpawns(t)
	rec := recordRemote(t, nil)

	s := remoteStryker(t.TempDir(), "", "deno")
	_, err := s.Run([]string{"src/a.ts"})
	if err == nil {
		t.Fatal("an unrecognised package manager silently skipped the restore")
	}
	if !strings.Contains(err.Error(), "deno") {
		t.Errorf("the refusal does not name the value: %v", err)
	}
	if len(*rec) != 0 {
		t.Errorf("a refused run still spawned %d commands", len(*rec))
	}
}

// TestRemoteRestoreRunsInTheJoinedWorkdir: restoring at the repo root is how a
// monorepo run installs against the wrong lockfile, or none at all. Both the
// restore and the tool must cd into the same joined directory.
func TestRemoteRestoreRunsInTheJoinedWorkdir(t *testing.T) {
	failLocalSpawns(t)
	rec := recordRemote(t, func(argv []string) {
		if isFetch(argv) {
			if werr := os.WriteFile(argv[3], []byte(strykerPayload), 0o644); werr != nil {
				t.Fatalf("fake fetch: %v", werr)
			}
		}
	})

	s := remoteStryker(t.TempDir(), "web", "pnpm")
	if _, err := s.Run([]string{"web/src/a.ts"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, script := range remoteScripts(*rec) {
		if !strings.HasPrefix(script, "cd '/srv/dross/web' && ") {
			t.Errorf("a remote command did not run in the joined workdir: %q", script)
		}
	}
}

// TestStrykerNetRemoteRestoresBeforeStryker.
func TestStrykerNetRemoteRestoresBeforeStryker(t *testing.T) {
	failLocalSpawns(t)
	rec := recordRemote(t, nil)

	s := &StrykerNet{ProjectRoot: t.TempDir(), Remote: &remote.Target{Host: "helicon", Workdir: "/srv/dross"}}
	// No report is delivered, so Run errors on findReport — irrelevant here; the
	// ordering happened before that.
	_, _ = s.Run([]string{"src/A.cs"})

	restore := indexOfScript(*rec, "'dotnet' 'restore'")
	tool := indexOfScript(*rec, "'dotnet' 'stryker'")
	if restore < 0 {
		t.Fatalf("no `dotnet restore` was issued:\n%v", remoteScripts(*rec))
	}
	if tool < 0 {
		t.Fatalf("stryker.net was never invoked:\n%v", remoteScripts(*rec))
	}
	if restore > tool {
		t.Errorf("restore ran after the tool (%d vs %d)", restore, tool)
	}
}

// TestGremlinsRemoteIssuesNoRestore: Go builds from the synced source, and a
// spurious install would be both slow and a lie about what the run needs.
func TestGremlinsRemoteIssuesNoRestore(t *testing.T) {
	failLocalSpawns(t)
	rec := recordRemote(t, nil)

	g := &Gremlins{ProjectRoot: t.TempDir(), Remote: &remote.Target{Host: "helicon", Workdir: "/srv/dross"}}
	if _, err := g.Run([]string{"a/x.go"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, script := range remoteScripts(*rec) {
		for _, forbidden := range []string{"npm", "pnpm", "yarn", "bun", "dotnet", "restore"} {
			if strings.Contains(script, forbidden) {
				t.Errorf("a Go remote run issued %q: %q", forbidden, script)
			}
		}
	}
}

// TestRemoteRestoreFailureIsFatalBeforeTheToolArgv: a restore that fell through
// would leave the tool measuring a tree with no dependencies, where every mutant
// dies because nothing imports — which scores as an excellent test suite.
func TestRemoteRestoreFailureIsFatalBeforeTheToolArgv(t *testing.T) {
	failLocalSpawns(t)
	var rec [][]string
	orig := launcherCommand
	launcherCommand = func(argv []string, stdin string) *exec.Cmd {
		cp := append(append([]string(nil), argv...), stdin)
		rec = append(rec, cp)
		if strings.Contains(remoteScript(cp), "'pnpm'") {
			return exec.Command("sh", "-c", "exit 1")
		}
		return exec.Command("true")
	}
	t.Cleanup(func() { launcherCommand = orig })

	s := remoteStryker(t.TempDir(), "", "pnpm")
	_, err := s.Run([]string{"src/a.ts"})
	if err == nil {
		t.Fatal("a failed dependency restore was tolerated")
	}
	for _, want := range []string{"pnpm install --frozen-lockfile", "helicon"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not name %q: %v", want, err)
		}
	}
	if idx := indexOfScript(rec, "'npx'"); idx >= 0 {
		t.Errorf("stryker was invoked after a failed restore:\n%v", remoteScripts(rec))
	}
}

// TestRemoteRestoreTableIsClosed: an adapter absent from the table cannot run
// remotely with no restore at all.
func TestRemoteRestoreTableIsClosed(t *testing.T) {
	_, err := newLauncher("mutant-4000", "", &remote.Target{Host: "h", Workdir: "/w"}, "/x", "")
	if err == nil {
		t.Fatal("an adapter absent from the restore table was accepted")
	}
	for _, a := range []Adapter{&Gremlins{}, &Stryker{}, &StrykerNet{}} {
		if _, ok := remoteRestores[a.Name()]; !ok {
			t.Errorf("adapter %q has no restore entry", a.Name())
		}
	}
}

// TestLocalRunsIssueNoRestore is the regression half: the restore is a REMOTE
// step. A local run installs nothing, because the developer's own tree is
// already whatever they made it.
func TestLocalRunsIssueNoRestore(t *testing.T) {
	for _, l := range []*Launcher{
		{Adapter: "stryker", PackageManager: "pnpm"},
		{Adapter: "stryker-net"},
		{Adapter: "gremlins"},
	} {
		argv, err := l.restoreArgv()
		if err != nil {
			t.Errorf("%s: %v", l.Adapter, err)
		}
		if argv != nil {
			t.Errorf("%s: a LOCAL run resolved a restore command %v", l.Adapter, argv)
		}
		if err := l.ensureRestored(); err != nil {
			t.Errorf("%s: ensureRestored on a local run: %v", l.Adapter, err)
		}
	}
}

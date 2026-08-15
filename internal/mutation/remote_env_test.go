package mutation

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/remote"
)

// c-8, executed: a remote run carries the environment the adapter needs, and
// the values never reach the remote's process list, filesystem or shell
// history.
//
// The locked remote_env_transport decision rules out the two obvious channels.
// Interpolating values into the ssh command string puts them in the REMOTE
// shell's argv, which is that host's process list — readable by every user on
// it, and by anything scraping `ps`. Writing a file on the remote leaves them
// there after the run. Both are strictly worse than the docker-daemon status
// quo this replaces, where the values are at least confined to `docker
// inspect`. Stdin touches neither argv nor disk.
//
// The values do land in the remote process's own environment, readable via
// /proc/<pid>/environ by that user or root. That is unavoidable for any env
// mechanism and is not a reason to accept the avoidable leaks.

func envTarget(env ...remote.EnvVar) *remote.Target {
	return &remote.Target{Host: "helicon", Workdir: "/srv/dross", Env: env}
}

// TestRemoteArgvIsAlwaysTheSameFourElements is the process-list guarantee at
// its root, and it holds UNIFORMLY — with no environment configured too.
//
// A shape that is safe only when the feature is switched on is a property that
// rots, because the unsafe branch is the one nobody tests. So the argv is
// ["ssh", host, "bash", "-s"] for every remote command, whether or not any env
// is in play.
func TestRemoteArgvIsAlwaysTheSameFourElements(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  []remote.EnvVar
	}{
		{"no env configured", nil},
		{"env configured", []remote.EnvVar{{Name: "DATABASE_URL", Value: "postgres://u:p@h/db"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			failLocalSpawns(t)
			rec := recordRemote(t, nil)

			g := &Gremlins{ProjectRoot: t.TempDir(), Remote: envTarget(tc.env...)}
			if _, err := g.Run([]string{"a/x.go"}); err != nil {
				t.Fatalf("Run: %v", err)
			}

			ssh := 0
			for _, entry := range *rec {
				argv := entry[:len(entry)-1] // trailing element is the piped script
				if argv[0] != "ssh" {
					continue
				}
				ssh++
				want := []string{"ssh", "helicon", "bash", "-s"}
				if len(argv) != len(want) {
					t.Fatalf("ssh argv = %v, want %v", argv, want)
				}
				for i := range want {
					if argv[i] != want[i] {
						t.Fatalf("ssh argv = %v, want %v", argv, want)
					}
				}
			}
			if ssh == 0 {
				t.Fatal("no ssh command was issued — this test proved nothing")
			}
		})
	}
}

// TestRemoteEnvExportsPrecedeTheCommand pins the script's shape: exports first,
// each shell-quoted, then the cd, then the adapter argv.
func TestRemoteEnvExportsPrecedeTheCommand(t *testing.T) {
	failLocalSpawns(t)
	rec := recordRemote(t, nil)

	g := &Gremlins{ProjectRoot: t.TempDir(), Remote: envTarget(
		remote.EnvVar{Name: "DATABASE_URL", Value: "postgres://u:p@h/db"},
		remote.EnvVar{Name: "NODE_ENV", Value: "test"},
	)}
	if _, err := g.Run([]string{"a/x.go"}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	script := ""
	for _, entry := range *rec {
		if s := remoteScript(entry); strings.Contains(s, "gremlins") {
			script = s
		}
	}
	if script == "" {
		t.Fatal("no gremlins script was recorded")
	}

	lines := strings.Split(strings.TrimRight(script, "\n"), "\n")
	want := []string{
		`export DATABASE_URL='postgres://u:p@h/db'`,
		`export NODE_ENV='test'`,
	}
	if len(lines) < len(want)+1 {
		t.Fatalf("script has %d lines, want at least %d:\n%s", len(lines), len(want)+1, script)
	}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("line %d = %q, want %q", i, lines[i], w)
		}
	}
	// The cd comes AFTER the exports and carries the command.
	last := lines[len(lines)-1]
	if !strings.HasPrefix(last, "cd '/srv/dross' && ") || !strings.Contains(last, "'gremlins'") {
		t.Errorf("the command line is not the cd'd adapter argv: %q", last)
	}
}

// TestNoEnvValueEverReachesArgv is c-8's process-list guarantee, asserted on the
// argv rather than by reading the builder — so a future caller that interpolates
// a value cannot pass this.
func TestNoEnvValueEverReachesArgv(t *testing.T) {
	const secret = "postgres://user:hunter2@db.internal/app"
	failLocalSpawns(t)
	rec := recordRemote(t, nil)

	g := &Gremlins{ProjectRoot: t.TempDir(), Remote: envTarget(
		remote.EnvVar{Name: "DATABASE_URL", Value: secret},
	)}
	if _, err := g.Run([]string{"a/x.go"}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, entry := range *rec {
		for _, a := range entry[:len(entry)-1] { // argv only, not the piped script
			if strings.Contains(a, secret) || strings.Contains(a, "hunter2") {
				t.Fatalf("an env VALUE reached argv — it would be in the remote host's process list: %v", entry[:len(entry)-1])
			}
		}
	}
}

// TestNothingIsWrittenToTheRemoteFilesystemToCarryEnv.
//
// A temp env file on the remote would satisfy every other assertion here while
// reintroducing exactly the leak the locked decision exists to prevent: a file
// persists after the run and survives it, which is worse than the process list.
func TestNothingIsWrittenToTheRemoteFilesystemToCarryEnv(t *testing.T) {
	failLocalSpawns(t)
	rec := recordRemote(t, nil)

	g := &Gremlins{ProjectRoot: t.TempDir(), Remote: envTarget(
		remote.EnvVar{Name: "DATABASE_URL", Value: "postgres://u:p@h/db"},
	)}
	if _, err := g.Run([]string{"a/x.go"}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, entry := range *rec {
		script := remoteScript(entry)
		if script == "" {
			continue
		}
		for _, forbidden := range []string{"> ", ">>", "tee", ".env", ".bashrc", ".profile"} {
			if strings.Contains(script, forbidden) {
				t.Errorf("the remote script writes env to the filesystem via %q: %q", forbidden, script)
			}
		}
	}
}

// TestEnvValuesRoundTripThroughARealShell is the quoting guarantee, executed
// rather than reasoned about: the generated script is run by /bin/sh and the
// value it observes is compared byte for byte.
//
// Reading the quoting code and concluding it is right is how quoting bugs
// survive. A value carrying `$(`, a backtick or a newline either arrives intact
// or executes something, and only running it tells the two apart.
func TestEnvValuesRoundTripThroughARealShell(t *testing.T) {
	for _, value := range []string{
		"plain",
		"has a space",
		"it's quoted",
		"$(id)",
		"`whoami`",
		"line1\nline2",
		`mixed '"$( \` + "`" + `!`,
		"",
	} {
		t.Run(value, func(t *testing.T) {
			tgt := *envTarget(remote.EnvVar{Name: "PROBE", Value: value})
			script, err := remote.Script(tgt, []string{"true"})
			if err != nil {
				t.Fatal(err)
			}
			// Everything before the cd is the export block, then echo the
			// variable back — the cd would fail in a test process, and the
			// exports are what is under test.
			//
			// Cut at the cd rather than filtering for lines starting with
			// "export": a value containing a NEWLINE makes its export span two
			// lines, and a line filter would drop the second half and leave an
			// unterminated quote. That case is exactly the one worth testing.
			cut := strings.LastIndex(script, "cd '")
			if cut < 0 {
				t.Fatalf("no cd in the generated script:\n%s", script)
			}
			probe := script[:cut] + "printf %s \"$PROBE\"\n"

			out, rerr := exec.Command("/bin/sh", "-c", probe).Output()
			if rerr != nil {
				t.Fatalf("the generated script did not run: %v\nscript:\n%s", rerr, probe)
			}
			if string(out) != value {
				t.Errorf("value did not round-trip:\n got %q\nwant %q\nscript:\n%s", string(out), value, probe)
			}
		})
	}
}

// TestUnsafeEnvNameIsRefusedBeforeAnyCommand: names cannot be quoted — `export
// 'X'=v` is not an assignment — so the allowlist is the only thing between a
// configured name and the remote shell.
func TestUnsafeEnvNameIsRefusedBeforeAnyCommand(t *testing.T) {
	for _, name := range []string{"A;id", "A B", "A-B", "1A", "", "$(id)"} {
		t.Run(name, func(t *testing.T) {
			failLocalSpawns(t)
			rec := recordRemote(t, nil)

			g := &Gremlins{ProjectRoot: t.TempDir(), Remote: envTarget(
				remote.EnvVar{Name: name, Value: "x"},
			)}
			if _, err := g.Run([]string{"a/x.go"}); err == nil {
				t.Fatalf("env name %q was accepted", name)
			}
			if len(*rec) != 0 {
				t.Errorf("a refused target still spawned %d commands", len(*rec))
			}
		})
	}
}

// TestRemoteNilCarriesNoScript is the regression half: a local run is
// byte-identical to what it was, and reaches the remote seam not at all.
func TestRemoteNilCarriesNoScript(t *testing.T) {
	root := t.TempDir()
	rec := recordRemote(t, nil)

	orig := gremlinsBuildCmd
	gremlinsBuildCmd = func(g *Gremlins, args []string) *exec.Cmd {
		for i, a := range args {
			if a == "--output" && i+1 < len(args) {
				_ = os.WriteFile(root+"/"+args[i+1], []byte(fixtureGremlinsBareBasename), 0o644)
			}
		}
		return exec.Command("true")
	}
	t.Cleanup(func() { gremlinsBuildCmd = orig })

	g := &Gremlins{ProjectRoot: root}
	if _, err := g.Run([]string{"a/x.go"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(*rec) != 0 {
		t.Errorf("a local run reached the remote seam %d times: %v", len(*rec), *rec)
	}
}

// TestNoEnvConfiguredStillRuns: env is optional. A Go remote run needs none,
// and a grant without mutation_remote_env must proceed with no export lines.
func TestNoEnvConfiguredStillRuns(t *testing.T) {
	failLocalSpawns(t)
	rec := recordRemote(t, nil)

	g := &Gremlins{ProjectRoot: t.TempDir(), Remote: envTarget()}
	if _, err := g.Run([]string{"a/x.go"}); err != nil {
		t.Fatalf("a grant with no env configured failed: %v", err)
	}
	for _, entry := range *rec {
		if script := remoteScript(entry); strings.Contains(script, "export ") {
			t.Errorf("an unconfigured env produced an export line: %q", script)
		}
	}
	if len(*rec) == 0 {
		t.Fatal("nothing ran")
	}
}

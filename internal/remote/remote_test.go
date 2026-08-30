package remote

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
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
	argv, cleanup, err := SyncArgs(target(), "/local/repo")
	if err != nil {
		t.Fatalf("SyncArgs = %v", err)
	}
	defer cleanup()
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
	if _, _, err := SyncArgs(target(), "repo"); err == nil {
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
			if argv, _, err := SyncArgs(tg, "/local/repo"); err == nil || argv != nil {
				t.Errorf("SyncArgs built %v for an unsafe workdir", argv)
			}
		})
	}
}

// TestValidateRefusesAnUnsafeScratchBase covers the scratch base on the same
// terms as the workdir, because it is handled on worse ones: the scratch base
// reaches an `rm -rf` on the remote when a run cleans up after itself, so a
// value that escapes its intended tree deletes somebody else's.
//
// The canonical-form case is the one that matters and the one that had no
// test: /srv/../etc satisfies workdirRe (dots and slashes are in the class),
// so the regexp above it lets it through and only the path.Clean comparison
// stops it.
func TestValidateRefusesAnUnsafeScratchBase(t *testing.T) {
	for _, base := range []string{
		"/scratch/x; rm -rf /",
		"/scratch/$(whoami)",
		"/scratch/`id`",
		"/scratch/x\nrm -rf /",
		"scratch/x",    // not absolute
		"/srv/../etc",  // not canonical — reaches the path.Clean guard
		"/scratch/x/",  // not canonical — trailing slash
		"/scratch/./x", // not canonical — single-dot element
		"/scratch//x",  // not canonical — doubled separator
	} {
		t.Run(base, func(t *testing.T) {
			tg := Target{Host: host, Workdir: "/srv/x", ScratchBase: base}
			err := tg.Validate()
			if err == nil {
				t.Fatalf("scratch base %q was accepted", base)
			}
			if !errors.Is(err, ErrUnsafeTarget) {
				t.Errorf("refusal does not wrap ErrUnsafeTarget: %v", err)
			}
			if !strings.Contains(err.Error(), fmt.Sprintf("%q", base)) {
				t.Errorf("refusal does not name the scratch base: %v", err)
			}
		})
	}
}

// TestValidateAcceptsACanonicalScratchBase pins the other side of the same
// guard. Without it, a refusal that rejected EVERY scratch base — the exact
// shape of an inverted comparison — would satisfy the table above completely
// while making the feature unusable.
func TestValidateAcceptsACanonicalScratchBase(t *testing.T) {
	for _, base := range []string{"/scratch", "/var/lib/buildcache/scratch", "/srv/x"} {
		t.Run(base, func(t *testing.T) {
			tg := Target{Host: host, Workdir: "/srv/x", ScratchBase: base}
			if err := tg.Validate(); err != nil {
				t.Fatalf("canonical scratch base %q was refused: %v", base, err)
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

// TestSyncArgsExcludesAnchoredRulesInNonRootGitignores is the regression lock
// for the defect that --filter=:- .gitignore hid: an ANCHORED rule (leading /)
// in a NON-ROOT .gitignore does not match under rsync's per-directory merge,
// while an unanchored rule in the very same file does. On one repo that put
// 50,619 entries and 5.2 GB of build output onto a remote root volume, and
// nothing noticed but the disk.
//
// It asserts through REAL rsync rather than on the argv string, because the
// argv was never the thing that was wrong — the rule was present and correct
// looking, and simply did not match. And it syncs into an EMPTY destination on
// purpose: --itemize-changes lists what would CHANGE, so a dry run against a
// populated target reports nothing when the files are already identical, which
// reads as a pass. That false pass is how the broken rule survived measurement
// once already.
func TestSyncArgsExcludesAnchoredRulesInNonRootGitignores(t *testing.T) {
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("rsync not installed")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	src := t.TempDir()
	dst := t.TempDir()

	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(src, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The discriminating fixture: two rules in ONE non-root .gitignore, one
	// anchored and one not. If both are excluded the anchoring is handled; if
	// only .dart_tool is, the file is being read and the anchoring is not.
	write("phone/.gitignore", "/build/\n.dart_tool/\n")
	write("web/.gitignore", "build/\n")
	write(".gitignore", "/.idea/\n")
	write("phone/build/app/big.bin", "x")
	write("phone/.dart_tool/d.bin", "x")
	write("web/build/c.bin", "x")
	write(".idea/e.bin", "x")
	write("phone/keep.txt", "x")

	// The tracked files matter: `git ls-files --directory` collapses a WHOLLY
	// untracked directory into one entry, so in a repo with nothing committed
	// no ignored child is ever listed. A real repo has tracked files beside the
	// ignored ones, and the fixture has to as well or it tests nothing.
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", ".gitignore", "phone/.gitignore", "phone/keep.txt", "web/.gitignore"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "init"},
	} {
		cmd := exec.Command("git", append([]string{"-C", src}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}

	argv, cleanup, err := SyncArgs(target(), src)
	if err != nil {
		t.Fatalf("SyncArgs = %v", err)
	}
	defer cleanup()
	if contains(argv, gitignoreMergeRule) {
		t.Errorf("argv still carries the per-directory merge rule in a git work tree: %v", argv)
	}

	// Re-point the argv at a local destination so this exercises the real
	// filtering without needing a host. Everything before the last two elements
	// is what SyncArgs decided, which is the part under test.
	run := append([]string{}, argv[1:len(argv)-2]...)
	run = append(run, "--dry-run", "--itemize-changes", src+"/", dst+"/")
	out, err := exec.Command(argv[0], run...).CombinedOutput()
	if err != nil {
		t.Fatalf("rsync %v: %v: %s", run, err, out)
	}
	listing := string(out)

	for _, absent := range []string{"phone/build", "phone/.dart_tool", "web/build", ".idea/"} {
		if strings.Contains(listing, absent) {
			t.Errorf("%s reached the wire:\n%s", absent, listing)
		}
	}
	// .git must still cross. Without it the remote tree is not a git repository
	// and every test that shells out to git fails there while passing locally.
	for _, present := range []string{".git/", "phone/keep.txt"} {
		if !strings.Contains(listing, present) {
			t.Errorf("%s did NOT reach the wire, but must:\n%s", present, listing)
		}
	}
}

// TestSyncArgsFallsBackToTheMergeRuleWithoutGit locks the other half: dross
// roots on .dross rather than .git, so a plain directory is a supported case
// and must not become an error just because git cannot answer for it.
func TestSyncArgsFallsBackToTheMergeRuleWithoutGit(t *testing.T) {
	argv, cleanup, err := SyncArgs(target(), t.TempDir())
	if err != nil {
		t.Fatalf("SyncArgs = %v", err)
	}
	defer cleanup()
	if !contains(argv, gitignoreMergeRule) {
		t.Errorf("a non-git root lost its ignore rule entirely: %v", argv)
	}
}

// --- detached runs ----------------------------------------------------------

// TestDetachScriptQuotesTheRunDirAndArgv is the injection assertion, and it
// matters more here than for an attached run: the detached script is
// constructed once and then runs unattended for hours, so a word that split is
// not something anyone is watching a terminal to notice.
//
// Both operands are exercised. The run directory reaches the script four times
// — mkdir, state, exit, log — and a quoting that covered the argv but not the
// paths would pass an argv-only assertion.
func TestDetachScriptQuotesTheRunDirAndArgv(t *testing.T) {
	dir := ".dross-runs/r 1;touch pwned"
	script, err := DetachScript(target(), dir, []string{"gremlins", "unleash", "./a b;c"}, time.Time{})
	if err != nil {
		t.Fatalf("DetachScript = %v", err)
	}
	for _, want := range []string{
		`'.dross-runs/r 1;touch pwned'`,
		`'.dross-runs/r 1;touch pwned/state'`,
		`'./a b;c'`,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("script does not carry %s as one quoted word:\n%s", want, script)
		}
	}
	// And the raw semicolon must never appear outside a quoted word: a bare
	// one is a second command.
	if strings.Contains(script, "touch pwned'\n") || strings.Contains(script, "; touch pwned") {
		t.Errorf("the run directory escaped its quoting:\n%s", script)
	}
}

// TestDetachScriptDetachesFromTheSession is c-1 at the script level. Without
// setsid the run belongs to the ssh session and dies with the SIGHUP that
// follows the connection closing — which is exactly the failure the phase
// exists to remove, and it would only show up an hour later as a run that
// never reported.
func TestDetachScriptDetachesFromTheSession(t *testing.T) {
	script, err := DetachScript(target(), ".dross-runs/r1", []string{"gremlins", "unleash"}, time.Time{})
	if err != nil {
		t.Fatalf("DetachScript = %v", err)
	}
	for _, want := range []string{"setsid", "nohup", "< /dev/null", "&"} {
		if !strings.Contains(script, want) {
			t.Errorf("script is missing %q — it would not survive the connection:\n%s", want, script)
		}
	}
	// Output must go to the run directory, not back over a connection that
	// will not be there to carry it.
	if !strings.Contains(script, `'.dross-runs/r1/log'`) {
		t.Errorf("script does not redirect output to the run directory:\n%s", script)
	}
}

// TestDetachScriptEmitsNoSleepWithoutASchedule is the immediate-dispatch half.
// A sleep emitted unconditionally would make every ordinary --detach wait on a
// date comparison it has no reason to do, and a bug in that arithmetic would
// delay runs nobody asked to schedule.
func TestDetachScriptEmitsNoSleepWithoutASchedule(t *testing.T) {
	script, err := DetachScript(target(), ".dross-runs/r1", []string{"gremlins"}, time.Time{})
	if err != nil {
		t.Fatalf("DetachScript = %v", err)
	}
	if strings.Contains(script, "sleep") {
		t.Errorf("an unscheduled run emitted a sleep:\n%s", script)
	}
	if !strings.Contains(script, "printf '%s' 'running'") {
		t.Errorf("an unscheduled run does not start in the running state:\n%s", script)
	}
}

// TestScheduledRunSleepsAgainstTheHostClock pins the epoch-second form.
//
// A duration computed HERE would bake in this machine's clock at dispatch and
// drift by the ssh round trip; worse, a notBefore that passed while the
// dispatch was in flight would compute a negative sleep. The comparison is
// emitted for the host to make, so both are impossible by construction.
func TestScheduledRunSleepsAgainstTheHostClock(t *testing.T) {
	at := time.Date(2026, 8, 31, 2, 0, 0, 0, time.UTC)
	script, err := DetachScript(target(), ".dross-runs/r1", []string{"gremlins"}, at)
	if err != nil {
		t.Fatalf("DetachScript = %v", err)
	}
	if !strings.Contains(script, fmt.Sprintf("%d", at.Unix())) {
		t.Errorf("the schedule is not carried as an epoch second (want %d):\n%s", at.Unix(), script)
	}
	if !strings.Contains(script, "date +%s") {
		t.Errorf("the script does not consult the HOST clock:\n%s", script)
	}
	if !strings.Contains(script, "sleep") {
		t.Errorf("a scheduled run emits no sleep:\n%s", script)
	}
	// It must start as scheduled, or `verify status` reports a run as running
	// for the hours it is actually waiting.
	if !strings.Contains(script, "printf '%s' 'scheduled'") {
		t.Errorf("a scheduled run does not start in the scheduled state:\n%s", script)
	}
}

// TestDetachScriptRecordsTheExitCode is the completion signal c-6 turns on. A
// script that ran the tool but recorded nothing leaves "finished with
// failures" and "died without finishing" as the same observation.
func TestDetachScriptRecordsTheExitCode(t *testing.T) {
	script, err := DetachScript(target(), ".dross-runs/r1", []string{"gremlins"}, time.Time{})
	if err != nil {
		t.Fatalf("DetachScript = %v", err)
	}
	// The exit write lives INSIDE the `bash -c` argument, so its path appears
	// in the escaped form single-quoting produces ('\'' around each quote) —
	// asserting the bare form here would be asserting that the inner script was
	// never quoted at all.
	if !strings.Contains(script, `> '\''.dross-runs/r1/exit'\''`) {
		t.Errorf("the script does not record the tool's exit code:\n%s", script)
	}
	if !strings.Contains(script, `"$__c"`) {
		t.Errorf("the recorded code is not the tool's own $?:\n%s", script)
	}
	// The pid write is in the OUTER script, after the `&`, which is the only
	// place $! still names the backgrounded job.
	if !strings.Contains(script, `> '.dross-runs/r1/pid'`) {
		t.Errorf("the script does not record the pid a cancel would signal:\n%s", script)
	}
}

// TestDetachScriptRefusesAnEmptyCommand mirrors Script's own refusal: a
// detached run with nothing to run would create a directory, record a pid and
// report finished having measured nothing.
func TestDetachScriptRefusesAnEmptyCommand(t *testing.T) {
	if _, err := DetachScript(target(), ".dross-runs/r1", nil, time.Time{}); err == nil {
		t.Fatal("a detached run with an empty command was accepted")
	}
	if _, err := DetachScript(target(), "", []string{"gremlins"}, time.Time{}); err == nil {
		t.Fatal("a detached run with no run directory was accepted")
	}
}

// TestRunDirRefusesATraversingID is why the id is validated as a path SEGMENT
// rather than merely quoted. The directory it names is removed recursively by a
// cancel, so an id spelling `..` aims that removal at the workdir's parent —
// and quoting stops the shell splitting the word while doing nothing about
// where the path then points.
func TestRunDirRefusesATraversingID(t *testing.T) {
	for _, bad := range []string{"", ".", "..", "../escape", "a/b", "/abs"} {
		if _, err := RunDir(bad); err == nil {
			t.Errorf("RunDir accepted %q", bad)
		}
	}
	got, err := RunDir("r-20260830-2201")
	if err != nil {
		t.Fatalf("RunDir rejected a well-formed id: %v", err)
	}
	if got != ".dross-runs/r-20260830-2201" {
		t.Errorf("RunDir = %q", got)
	}
}

// TestParseStatusSeparatesAbsentFromZero is the assertion c-6 rests on. An exit
// code of 0 and no exit code at all are the two outcomes a fetch must never
// merge: the first is a clean run to collect, the second is a run that died.
// Any in-band sentinel would be a value some real run also produces.
func TestParseStatusSeparatesAbsentFromZero(t *testing.T) {
	zero, err := ParseStatus("dir=yes\nstate=finished\nexit=0\npid=42\n")
	if err != nil {
		t.Fatalf("ParseStatus = %v", err)
	}
	if !zero.HasExit || zero.ExitCode != 0 {
		t.Errorf("a recorded 0 did not parse as one: %+v", zero)
	}

	absent, err := ParseStatus("dir=yes\nstate=running\nexit=\npid=42\n")
	if err != nil {
		t.Fatalf("ParseStatus = %v", err)
	}
	if absent.HasExit {
		t.Errorf("an absent exit code parsed as a recorded one: %+v", absent)
	}
	if absent.ExitCode != zero.ExitCode {
		t.Log("note: ExitCode alone cannot distinguish these — HasExit is the discriminator")
	}
}

// TestParseStatusDistinguishesAMissingRunDirectory is the other half of c-6:
// "the run has written nothing yet" and "the run directory is gone" are both
// three empty values, and only the dir line tells them apart.
func TestParseStatusDistinguishesAMissingRunDirectory(t *testing.T) {
	gone, err := ParseStatus("dir=no\nstate=\nexit=\npid=\n")
	if err != nil {
		t.Fatalf("ParseStatus = %v", err)
	}
	if gone.DirExists {
		t.Errorf("a removed run directory parsed as present: %+v", gone)
	}
	fresh, err := ParseStatus("dir=yes\nstate=\nexit=\npid=\n")
	if err != nil {
		t.Fatalf("ParseStatus = %v", err)
	}
	if !fresh.DirExists {
		t.Errorf("a present-but-empty run directory parsed as gone: %+v", fresh)
	}
}

// TestParseStatusIgnoresShellNoise: the remote login shell may print a banner
// or a profile's greeting before anything dross wrote. A parser that refused
// unknown lines would strand a finished run behind someone's .bashrc.
func TestParseStatusIgnoresShellNoise(t *testing.T) {
	got, err := ParseStatus("Welcome to helicon!\nLast login: Sun\ndir=yes\nstate=finished\nexit=1\npid=9\n")
	if err != nil {
		t.Fatalf("ParseStatus = %v", err)
	}
	if !got.HasExit || got.ExitCode != 1 || got.State != "finished" {
		t.Errorf("banner text broke the parse: %+v", got)
	}
}

// TestParseStatusRefusesOutputItRecognisesNothingIn is the other side of that
// tolerance: ignoring unknown lines must not mean accepting output that
// contains no status at all, which is what an ssh that failed before running
// the script returns.
func TestParseStatusRefusesOutputItRecognisesNothingIn(t *testing.T) {
	if _, err := ParseStatus("ssh: connect to host helicon port 22: No route to host\n"); err == nil {
		t.Fatal("output carrying no status lines parsed as a status")
	}
}

// TestStatusScriptReadsWithoutDisturbing pins that the status read is
// side-effect free. A status verb that created the directory it was asking
// about would make "the run is gone" unobservable.
func TestStatusScriptReadsWithoutDisturbing(t *testing.T) {
	script, err := StatusScript(target(), ".dross-runs/r1")
	if err != nil {
		t.Fatalf("StatusScript = %v", err)
	}
	for _, forbidden := range []string{"mkdir", "rm ", "setsid", "> '.dross-runs"} {
		if strings.Contains(script, forbidden) {
			t.Errorf("the status script is not read-only, it contains %q:\n%s", forbidden, script)
		}
	}
	for _, want := range []string{"dir=", "state=", "exit=", "pid="} {
		if !strings.Contains(script, want) {
			t.Errorf("the status script does not report %q:\n%s", want, script)
		}
	}
}

// TestDetachScriptBackgroundsOnlyTheJob is the regression for the bug a live
// dispatch against helicon found, and which every earlier assertion here
// missed.
//
// The first draft built `cd … && mkdir … && printf … && setsid nohup bash -c … &`.
// In POSIX shell the `&` binds to the ENTIRE `&&` list, not to the last command
// in it, so the whole chain — the mutation run included — ran in a background
// subshell that still held the ssh channel's stdout. ssh therefore blocked until
// the run finished: the dispatch hung for the full length of the leg, which is
// exactly the blocking --detach exists to remove, while the run itself was
// detached and working perfectly on the host.
//
// The same bug misdirected `$!`: it named the subshell rather than the detached
// job, so the recorded pid pointed at the wrong process and --cancel would have
// had nothing to kill. The pid file on the hung dispatch came back EMPTY.
//
// Asserted structurally: no `&&` may appear before the backgrounding `&`, and
// the `&` must terminate a line that starts with setsid. The old shape passed
// every "contains setsid / contains &" check, so those cannot be the assertion.
func TestDetachScriptBackgroundsOnlyTheJob(t *testing.T) {
	script, err := DetachScript(target(), ".dross-runs/r1", []string{"gremlins", "unleash"}, time.Time{})
	if err != nil {
		t.Fatalf("DetachScript = %v", err)
	}

	var bgLine string
	for _, line := range strings.Split(script, "\n") {
		if strings.HasSuffix(strings.TrimSpace(line), "&") && !strings.HasSuffix(strings.TrimSpace(line), "&&") {
			if bgLine != "" {
				t.Fatalf("more than one backgrounded command:\n%s", script)
			}
			bgLine = strings.TrimSpace(line)
		}
	}
	if bgLine == "" {
		t.Fatalf("nothing is backgrounded:\n%s", script)
	}
	if !strings.HasPrefix(bgLine, "setsid ") {
		t.Errorf("the backgrounded statement is not the detached job alone — `&` binds to the\n"+
			"whole && list, so ssh will block until the run finishes:\n  %s", bgLine)
	}
	if strings.Contains(bgLine, "&&") {
		t.Errorf("the backgrounded statement is an && chain, so the whole chain is\n"+
			"backgrounded and holds the ssh channel open:\n  %s", bgLine)
	}
	// The setup steps must still be guarded: dropping && for newlines without
	// replacing the guard would run the mutation even when mkdir failed.
	for _, want := range []string{"|| exit 1"} {
		if !strings.Contains(script, want) {
			t.Errorf("setup steps are unguarded — a failed mkdir would still start the run:\n%s", script)
		}
	}
	// And the pid must be captured on its own line AFTER the background job,
	// which is the only place $! names it.
	lines := strings.Split(strings.TrimSpace(script), "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if !strings.Contains(last, `"$!"`) || !strings.Contains(last, "/pid'") {
		t.Errorf("the last statement does not record the detached job's pid:\n  %s", last)
	}
}

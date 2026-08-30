// Package remote owns the transport dross uses to run a mutation adapter on
// another machine: the pure argv builders for ssh and rsync, the readiness
// probe, and the exit-code classification every caller branches on.
//
// It deliberately decides no POLICY. Whether a remote is configured, which
// adapter runs on it, and what a failure means for a verify report all live
// above this package. What lives here is argv construction and exit codes —
// thin enough that the three mutation adapters can share one seam instead of
// growing three near-copies of it.
//
// The one thing this package IS opinionated about is the shell. ssh does not
// take an argv: it takes a STRING that the remote login shell parses. That
// makes every value crossing this boundary a shell-injection surface, which
// argfence's leading-dash rule does not cover — a dash rule stops `-rf`, not
// `; rm -rf /`. So Target validates against an allowlist and every element of
// a remote command is single-quoted on the way out.
package remote

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrTransport is the connection itself failing: host unreachable, ssh
	// refused, the stream dying mid-transfer. Nothing ran, so nothing was
	// measured — callers must escalate rather than record a clean-looking
	// empty result.
	ErrTransport = errors.New("remote transport failed")
	// ErrPartial is rsync reporting an incomplete transfer (a source file
	// vanished, one path errored). The connection held; some files did not
	// arrive. Fetching a report that does not exist lands here, which is the
	// one remote failure that may legitimately stay a non-fatal skip.
	ErrPartial = errors.New("remote transfer incomplete")
	// ErrRemoteCommand is the remote program exiting non-zero. The transport
	// worked; the thing it ran did not like something. A mutation tool exiting
	// 1 because mutants survived is this, and is tolerated by its caller.
	ErrRemoteCommand = errors.New("remote command failed")
	// ErrUnsafeTarget is a host or workdir refused before any argv is built.
	// It is returned INSTEAD of argv, never alongside it.
	ErrUnsafeTarget = errors.New("remote target is not safe to hand to a shell")
)

// Target is the machine a mutation run is delegated to.
type Target struct {
	// Host is what ssh and rsync are handed as the destination: a hostname or
	// an ssh_config alias, optionally user-qualified.
	Host string
	// Workdir is the absolute path on Host that the synced tree lands in and
	// that every remote command cds into.
	Workdir string
	// Cores is the host's usable core count, filled in by Probe. Zero means
	// "not probed yet" — callers deriving a worker count from it must probe
	// first rather than silently substituting the LOCAL core count, which is
	// the bug the remote_workers decision was written about.
	Cores int
	// Env is the environment the remote command needs, resolved from dross's
	// OWN process environment by whoever built this target. It travels on the
	// target because every remote command in a run needs the same environment,
	// and threading it separately through each call site is how one of them
	// ends up without it.
	//
	// dross stores none of these values. The machine-local config holds NAMES
	// only; the values are read at run time and exist in memory for the length
	// of one command.
	Env []EnvVar
	// ScratchBase optionally overrides the volume the run's throwaway build
	// cache lands on. Empty means the caller derives it from Workdir.
	//
	// It rides on the target because it names a path on THIS host and nothing
	// else can interpret it — and because the derived default was wrong on the
	// reference host for three months: /home there is part of a 75 GB root LV
	// while the 300 GB volume provisioned for the work sat elsewhere.
	//
	// Validated exactly like Workdir: this path reaches an `rm -rf` on a remote
	// shell, so a value that is not a plain canonical absolute path is refused
	// rather than quoted and hoped for.
	ScratchBase string
}

// EnvVar is one variable to export on the remote.
type EnvVar struct {
	Name  string
	Value string
}

// envNameRe is what a variable name may be. An allowlist, for the same reason
// hostRe is one: the script is handed to a remote shell, and a "name" like
// `X; rm -rf /` would be a command. Names are not quoted on the way out — they
// cannot be, since `export 'X'=v` is not an assignment — so the allowlist is
// the only thing standing there.
var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Allowlists, not blocklists. A blocklist of shell metacharacters is a
// classifier, and the character that matters is the one nobody thought of;
// these say what a host and a path may contain and refuse everything else.
//
// hostRe permits an optional `user@` because that is how a non-default login
// is expressed, and nothing else — no options, no ports, no colons (a colon
// would make rsync read the destination as host:path twice over).
//
// workdirRe requires an absolute path: a relative workdir would resolve
// against whatever the remote login shell happens to start in, which differs
// per user and per host and is not something a mutation run should depend on.
var (
	hostRe    = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9._-]*@)?[A-Za-z0-9][A-Za-z0-9._-]*$`)
	workdirRe = regexp.MustCompile(`^/[A-Za-z0-9._/-]*$`)
)

// Validate refuses a target that cannot safely be handed to ssh.
//
// The refusal names the offending value, because the user's next move is to
// edit the line they granted — an error that only says "invalid target" sends
// them looking.
func (t Target) Validate() error {
	if t.Host == "" {
		return fmt.Errorf("no remote host configured: %w", ErrUnsafeTarget)
	}
	if !hostRe.MatchString(t.Host) {
		// A leading dash is the specific case argfence's Reject rule would
		// catch for a local spawn; the allowlist covers it and everything else.
		return fmt.Errorf("remote host %q is not a plain hostname: %w", t.Host, ErrUnsafeTarget)
	}
	if t.Workdir == "" {
		return fmt.Errorf("no remote workdir configured for host %q: %w", t.Host, ErrUnsafeTarget)
	}
	if !workdirRe.MatchString(t.Workdir) {
		return fmt.Errorf("remote workdir %q is not a plain absolute path: %w", t.Workdir, ErrUnsafeTarget)
	}
	if t.ScratchBase != "" {
		if !workdirRe.MatchString(t.ScratchBase) {
			return fmt.Errorf("remote scratch base %q is not a plain absolute path: %w", t.ScratchBase, ErrUnsafeTarget)
		}
		if t.ScratchBase != path.Clean(t.ScratchBase) {
			return fmt.Errorf("remote scratch base %q is not in canonical form (want %q): %w", t.ScratchBase, path.Clean(t.ScratchBase), ErrUnsafeTarget)
		}
	}
	if t.Workdir != path.Clean(t.Workdir) {
		return fmt.Errorf("remote workdir %q is not in canonical form (want %q): %w", t.Workdir, path.Clean(t.Workdir), ErrUnsafeTarget)
	}
	for _, e := range t.Env {
		if !envNameRe.MatchString(e.Name) {
			return fmt.Errorf("remote environment name %q is not a plain variable name: %w", e.Name, ErrUnsafeTarget)
		}
	}
	return nil
}

// In returns the same target rooted at a subdirectory of its workdir — the
// monorepo case, where an adapter runs in `web/` rather than at the repo root.
//
// It refuses a sub that escapes the workdir. cmd.Dir on the LOCAL ssh process
// is meaningless for a remote run, so this is the only thing standing between
// an adapter's workdir knob and a run in the wrong tree.
func (t Target) In(sub string) (Target, error) {
	if err := t.Validate(); err != nil {
		return Target{}, err
	}
	if sub == "" || sub == "." {
		return t, nil
	}
	// An absolute sub is refused rather than joined. path.Join would silently
	// reinterpret "/etc" as "<workdir>/etc" — safe, but not what the caller
	// wrote, and a workdir knob that quietly means something else is how an
	// adapter ends up measuring the wrong tree.
	if strings.HasPrefix(sub, "/") {
		return Target{}, fmt.Errorf("remote subdirectory %q must be relative to workdir %q: %w", sub, t.Workdir, ErrUnsafeTarget)
	}
	joined := path.Join(t.Workdir, sub)
	if joined != t.Workdir && !strings.HasPrefix(joined, t.Workdir+"/") {
		return Target{}, fmt.Errorf("remote subdirectory %q escapes workdir %q: %w", sub, t.Workdir, ErrUnsafeTarget)
	}
	out := t
	out.Workdir = joined
	if err := out.Validate(); err != nil {
		return Target{}, err
	}
	return out, nil
}

// SSHArgs returns the local argv that opens a shell on the target.
//
// It is the SAME argv for every remote command in a run — the workdir, the
// adapter's argv and every environment value live in the script piped to that
// shell's stdin (see Script), not here. That is the locked remote_env_transport
// decision, and the shape is uniform rather than conditional on whether env is
// configured: a safety property that holds only on the branch someone switched
// on is one the untested branch loses.
//
// The ssh command string is the remote shell's argv, and shows up in that
// host's process list — `ps` on the mutation host, readable by every user on
// it. Stdin touches neither argv nor disk. Values still land in the remote
// process's own environment, readable via /proc/<pid>/environ by that user or
// root; that is unavoidable for any env mechanism, and not a reason to accept
// the avoidable leaks.
//
// argv[0] is always "ssh" — the adapter's own binary never reaches a local
// exec, which is what "the local machine spawns no compile or test process"
// means in practice.
func SSHArgs(t Target) ([]string, error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}
	return []string{"ssh", t.Host, "bash", "-s"}, nil
}

// Script builds what is piped to that shell's stdin: the exports, then the cd,
// then the command.
//
// Exports come FIRST, each on its own line, so the command runs with them
// already set. Names are validated (see envNameRe) rather than quoted, because
// `export 'X'=v` is not an assignment; values are single-quoted, which suspends
// every expansion — a value containing `$(`, a backtick, a quote or a newline
// crosses as those bytes and executes nothing.
//
// An env entry with an empty value is NOT emitted as an empty export: the
// caller is responsible for refusing an unset name, because an empty value is
// not the same as an absent one and the difference changes what gets measured.
func Script(t Target, argv []string) (string, error) {
	return ScriptAll(t, [][]string{argv})
}

// ScriptAll is Script for several commands that must run in the same workdir,
// chained with `&&` so the first failure stops the rest and the exit code that
// reaches ssh is that failure's.
//
// One script rather than one ssh call per command: the commands that need this
// are a stale-report `rm` and the `mkdir` that recreates its directory, issued
// once per package inside the run loop. Splitting them would double the ssh
// round trips in that loop for no gain, and would let the pair interleave with
// another package's.
//
// The `&&` chaining is load-bearing, not cosmetic. A `;` would run the mkdir
// even when the rm failed, which is how a run ends up with a directory it
// cannot trust and a report from a previous run still in it.
func ScriptAll(t Target, cmds [][]string) (string, error) {
	if err := t.Validate(); err != nil {
		return "", err
	}
	if len(cmds) == 0 {
		return "", fmt.Errorf("remote: empty command for host %q", t.Host)
	}
	var b strings.Builder
	writePreamble(&b, t)
	for n, argv := range cmds {
		if len(argv) == 0 {
			return "", fmt.Errorf("remote: empty command for host %q", t.Host)
		}
		b.WriteString(" && ")
		// The LAST command replaces the shell rather than being forked by it.
		//
		// This is not an optimisation. The shell reading this script was started
		// as `bash -s`, so it reads its commands from stdin — and gremlins,
		// forked by such a shell, dies at startup with
		// `Failed to find executable : Is a directory`, exit 1, no report,
		// before it prints a single line. The same binary on the same host with
		// the same environment and the same argv runs clean the moment it is
		// exec'd instead (as `bash -c` does implicitly for its final command).
		// `go version` and `go test` forked by that shell are unaffected, so the
		// trigger is specific to the tool, not to the toolchain.
		//
		// Exec'ing costs nothing anywhere else: the chain is `&&`-joined, so a
		// prior failure has already short-circuited, and there is nothing left
		// for the shell to do afterwards. The exit code that reaches ssh is the
		// command's own either way.
		if n == len(cmds)-1 {
			b.WriteString("exec ")
		}
		for i, a := range argv {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(shellQuote(a))
		}
	}
	b.WriteByte('\n')
	return b.String(), nil
}

// writePreamble emits the exports and the cd that every remote script starts
// with, leaving the builder positioned at the end of the `cd` line.
//
// Extracted so the env-export quoting has ONE implementation. It is the line
// where a value carrying `$(` or a backtick would become execution rather than
// data, and a second copy of it is a second place for that to be got wrong —
// the detached script below needs the same preamble and must not spell it
// again.
func writePreamble(b *strings.Builder, t Target) {
	for _, e := range t.Env {
		b.WriteString("export ")
		b.WriteString(e.Name)
		b.WriteByte('=')
		b.WriteString(shellQuote(e.Value))
		b.WriteByte('\n')
	}
	b.WriteString("cd ")
	b.WriteString(shellQuote(t.Workdir))
}

// RunsDirName is the host-side directory, relative to the target's workdir,
// under which each detached run gets its own directory.
//
// Dot-prefixed and inside the workdir rather than in /tmp: the workdir is the
// one path the grant already authorized, so a run directory there needs no
// second authorization and is removed by the same cleanup that owns the tree.
// A /tmp path would also be swept by tmpfiles on a long-scheduled run — the
// exact case --at exists to serve.
const RunsDirName = ".dross-runs"

// RunDir returns the host-side directory for one detached run, relative to the
// target's workdir.
//
// runID is validated as a path SEGMENT, not merely quoted: it is joined into a
// path that a later cancel removes recursively, so a segment spelling `..` would
// aim that removal at the workdir's parent. Quoting alone stops the shell from
// splitting it and does nothing about what the path then denotes.
func RunDir(runID string) (string, error) {
	if runID == "" {
		return "", fmt.Errorf("remote: empty run id: %w", ErrUnsafeTarget)
	}
	if runID != path.Base(runID) || runID == "." || runID == ".." {
		return "", fmt.Errorf("remote: run id %q is not a single path segment: %w", runID, ErrUnsafeTarget)
	}
	return path.Join(RunsDirName, runID), nil
}

// DetachScript builds the script that starts argv on the host and returns
// without waiting for it.
//
// The run outlives the ssh connection, which is the whole point: `setsid`
// detaches it from the session so the SIGHUP that follows the connection
// closing never reaches it, `nohup` covers the case where setsid is absent, and
// stdin is redirected from /dev/null so the tool cannot block reading a
// terminal that is about to go away. Output goes to a log file in the run
// directory rather than back over the connection, because there is no
// connection to carry it by the time the tool is producing any.
//
// notBefore is honoured on the HOST's clock, as an absolute instant in epoch
// seconds rather than a duration computed here. A duration would bake in this
// machine's clock at dispatch and drift by however long the ssh round trip
// took; an epoch second is unambiguous and needs no timezone agreement between
// the two machines. A notBefore already past emits no sleep at all — the
// comparison is done host-side, so a dispatch racing its own start time cannot
// produce a negative sleep.
//
// The tool's exit code is written to a file AFTER it finishes. That file's
// existence is the completion signal: a run that died — host rebooted, OOM
// killer, someone's `pkill` — leaves no exit file, which is what makes
// "finished with failures" distinguishable from "never finished" at fetch time.
func DetachScript(t Target, runDir string, argv []string, notBefore time.Time) (string, error) {
	if err := t.Validate(); err != nil {
		return "", err
	}
	if len(argv) == 0 {
		return "", fmt.Errorf("remote: empty command for host %q", t.Host)
	}
	if runDir == "" {
		return "", fmt.Errorf("remote: empty run directory: %w", ErrUnsafeTarget)
	}

	q := func(s string) string { return shellQuote(s) }
	statePath := q(path.Join(runDir, "state"))
	exitPath := q(path.Join(runDir, "exit"))
	pidPath := q(path.Join(runDir, "pid"))
	logPath := q(path.Join(runDir, "log"))

	// The inner script is what the detached shell runs. Built as text and
	// single-quoted into `bash -c` as one argument, so nothing in it is
	// re-parsed by the outer shell.
	var inner strings.Builder
	if !notBefore.IsZero() {
		fmt.Fprintf(&inner, "__t=%d; __n=$(date +%%s); "+
			"if [ \"$__t\" -gt \"$__n\" ]; then sleep $((__t - __n)); fi; ",
			notBefore.Unix())
	}
	// Written after the sleep, so a scheduled run reads as scheduled until it
	// actually starts rather than from the moment it was dispatched.
	inner.WriteString("printf '%s' running > " + statePath + "; ")
	for i, a := range argv {
		if i > 0 {
			inner.WriteByte(' ')
		}
		inner.WriteString(q(a))
	}
	// `$?` is captured before anything else can overwrite it, and the state
	// file is only moved to finished once the code is durably recorded — a
	// reader that saw finished with no exit file would have to guess.
	inner.WriteString("; __c=$?; printf '%s\\n' \"$__c\" > " + exitPath +
		"; printf '%s' finished > " + statePath)

	initial := "running"
	if !notBefore.IsZero() {
		initial = "scheduled"
	}

	var b strings.Builder
	writePreamble(&b, t)
	b.WriteString(" && mkdir -p " + q(runDir))
	b.WriteString(" && printf '%s' " + q(initial) + " > " + statePath)
	b.WriteString(" && setsid nohup bash -c " + q(inner.String()) +
		" > " + logPath + " 2>&1 < /dev/null &")
	// `$!` is the backgrounded pid, recorded so a cancel has something to
	// signal. Read on the line after the `&`, which is the only place it is
	// still the pid of that job.
	b.WriteString("\nprintf '%s\\n' \"$!\" > " + pidPath + "\n")
	return b.String(), nil
}

// StatusScript builds the script that reports a detached run's state without
// disturbing it.
//
// Every value is emitted as a labelled line with an empty value when its file
// is absent, so a missing file and an empty one read the same and neither is an
// error. `dir` is emitted separately because the difference between "the run
// has not written anything yet" and "the run directory is gone" is the
// difference between waiting and reporting a lost run — c-6's distinction, and
// one that three empty values on their own cannot express.
func StatusScript(t Target, runDir string) (string, error) {
	if err := t.Validate(); err != nil {
		return "", err
	}
	if runDir == "" {
		return "", fmt.Errorf("remote: empty run directory: %w", ErrUnsafeTarget)
	}
	q := func(s string) string { return shellQuote(s) }
	var b strings.Builder
	writePreamble(&b, t)
	b.WriteString("\nprintf 'dir=%s\\n' \"$([ -d " + q(runDir) + " ] && echo yes || echo no)\"")
	for _, f := range []string{"state", "exit", "pid"} {
		b.WriteString("\nprintf '" + f + "=%s\\n' \"$(cat " +
			q(path.Join(runDir, f)) + " 2>/dev/null)\"")
	}
	b.WriteByte('\n')
	return b.String(), nil
}

// RunStatus is a detached run's host-side state, as StatusScript reported it.
//
// HasExit is separate from ExitCode rather than encoded as a sentinel: the tool
// exiting 0 and the tool never finishing are the two outcomes a fetch must not
// confuse, and any in-band sentinel (-1, 0, "") is a value some real run could
// also produce.
type RunStatus struct {
	DirExists bool
	State     string
	ExitCode  int
	HasExit   bool
	PID       int
}

// ParseStatus reads StatusScript's output.
//
// Unknown lines are ignored rather than refused: the remote shell's profile may
// print a banner before anything dross wrote, and a status read that failed
// because someone's .bashrc greets them would strand a finished run.
func ParseStatus(out string) (RunStatus, error) {
	var s RunStatus
	seen := false
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch k {
		case "dir":
			s.DirExists = v == "yes"
			seen = true
		case "state":
			s.State = v
		case "exit":
			if v != "" {
				n, err := strconv.Atoi(v)
				if err != nil {
					return RunStatus{}, fmt.Errorf("remote: unreadable exit code %q: %w", v, err)
				}
				s.ExitCode, s.HasExit = n, true
			}
		case "pid":
			if v != "" {
				n, err := strconv.Atoi(v)
				if err != nil {
					return RunStatus{}, fmt.Errorf("remote: unreadable pid %q: %w", v, err)
				}
				s.PID = n
			}
		}
	}
	if !seen {
		return RunStatus{}, fmt.Errorf("remote: no status lines in output")
	}
	return s, nil
}

// SyncArgs returns the argv that pushes the local working tree to the target,
// together with a cleanup func the caller must call when the push is done.
//
// The flags are the locked sync_mechanism decision, and each carries weight:
//
//   - --delete keeps the remote tree honest about files git tracks. It does NOT
//     make the remote tree fresh in general — excluded paths are protected from
//     deletion, and every report location is gitignored, so a stale remote
//     REPORT survives this. Staleness of reports is the launcher's explicit rm,
//     not this flag.
//   - the ignore rule keeps dependency dirs and build output off the wire. In
//     a git work tree it is --exclude-from naming the repo's own ignored-path
//     list, asked of git rather than approximated; elsewhere it stays the
//     per-directory .gitignore merge. See ignoreRule for why the merge rule
//     alone cannot do this job.
//
// .git IS synced, and the absence of --exclude=.git is the deliberate part.
//
// It was excluded originally to keep the wire small. That traded a few
// megabytes for correctness: without .git the remote tree is not a git
// repository, so every test that shells out to git fails there while passing
// locally. On this repo that is six tests in internal/cmd
// (TestStateJSONNotTracked, TestLocalStoreIsUntracked and friends, which assert
// what git does and does not track) — enough to fail the package's coverage
// pass, so gremlins writes no report and the package holding most of the code
// goes unmeasured with nothing saying so.
//
// A remote run that measures a DIFFERENT tree than a local one is the thing
// this whole seam exists to avoid, and 12M is not a reason to accept it. The
// exclude list is what keeps node_modules and build output off the wire; that
// was always the size lever.
//
// Uncommitted work is carried on purpose. Measuring the committed tree would
// measure something the user is not looking at.
//
// This is the one argv builder in the package that is not pure — it shells out
// to git and writes a temp file. That is the price of asking git the question
// instead of guessing at the answer, and the cost is paid HERE rather than in a
// separate exported step so that a caller cannot forget to pass the excludes
// and silently get an unfiltered sync. The target is still validated before any
// of it runs, so an unsafe host reaches neither git nor an argv.
func SyncArgs(t Target, localRoot string) ([]string, func(), error) {
	noop := func() {}
	if err := t.Validate(); err != nil {
		return nil, noop, err
	}
	src, err := localPath(localRoot, "local root")
	if err != nil {
		return nil, noop, err
	}
	ignore, cleanup, err := ignoreRule(src)
	if err != nil {
		return nil, noop, err
	}
	return []string{
		"rsync",
		"-az",
		"--delete",
		ignore,
		// The trailing slash is load-bearing: without it rsync creates
		// <workdir>/<basename>/ and every remote path in the run is off by one
		// directory.
		src + "/",
		t.Host + ":" + t.Workdir,
	}, cleanup, nil
}

// ignoreRule returns the single rsync argv element that keeps ignored paths off
// the wire, plus a cleanup func for any temp file it had to write.
//
// WHY NOT just --filter=:- .gitignore, which is the obvious way to do this and
// is what this code did until it was measured:
//
// In rsync's per-directory merge, an ANCHORED rule (leading /) inside a
// NON-ROOT .gitignore does not match, while unanchored rules in the very same
// file do. So `/build/` in phone/.gitignore silently transfers the whole tree
// while `.dart_tool/` beside it is correctly excluded — the file IS being read,
// the rule just resolves against rsync's transfer root instead of the merge
// file's own directory. git anchors it to the .gitignore's directory.
//
// The failure is silent and unbounded: on one repo it put 50,619 entries and
// 5.2 GB of build output onto a remote root volume, and the only reason anyone
// noticed was the disk filling. Every anchored rule in every non-root
// .gitignore in every repo dross syncs was affected. The / filter modifier
// (:-/ and dir-merge,-/) does NOT fix it; both were measured and still leak.
//
// So where there is a git work tree, ask git — it is the authority on what this
// repo ignores, and the whole class of mismatch goes away. Where there is NOT
// one, fall back to the per-directory merge: dross roots on .dross rather than
// .git, so a non-git root is a supported case, and with no git to be
// authoritative rsync's own reading of .gitignore is the only answer available.
// That path is unchanged, so this fix regresses nothing.
//
// Note what deliberately still crosses in the git case: .git itself (SyncArgs
// explains why that is load-bearing), tracked files that happen to match an
// ignore rule (--others lists only untracked paths, and a tracked file is not
// ignored), and uncommitted work.
//
// Residual gap, stated rather than left to be discovered: --directory collapses
// a WHOLLY untracked directory into a single entry, so ignored files inside an
// untracked, non-ignored directory are not listed and will cross. It is kept
// because it prunes whole subtrees — the alternative lists every ignored file
// individually (55,985 entries against 5,346 on one real repo) and makes rsync
// match every pattern against every file. The gap fails toward sending too
// much, never toward deleting, and it is strictly narrower than the anchoring
// bug it replaces.
//
// The test for this MUST run against an empty destination. --itemize-changes
// lists what would CHANGE, so a dry run against a populated target reports
// nothing when the files are already identical — which reads as "the filter
// works", and is how the broken rule survived measurement once already.
func ignoreRule(root string) (string, func(), error) {
	noop := func() {}
	if !isGitWorkTree(root) {
		return gitignoreMergeRule, noop, nil
	}
	// -z because git C-style-quotes paths with unusual characters otherwise,
	// and a quoted path is not the path rsync needs to match.
	out, err := exec.Command("git", "-C", root, "ls-files",
		"--others", "--ignored", "--exclude-standard",
		"--directory", "--no-empty-directory", "-z").Output()
	if err != nil {
		return "", noop, fmt.Errorf("listing ignored paths in %s: %w", root, err)
	}
	f, err := os.CreateTemp("", "dross-rsync-exclude-*")
	if err != nil {
		return "", noop, err
	}
	cleanup := func() { os.Remove(f.Name()) }
	var b strings.Builder
	for _, p := range strings.Split(string(out), "\x00") {
		if p == "" {
			continue
		}
		// Leading / anchors each pattern at the transfer root, which is the
		// repo root — so a bare `build/` cannot match somewhere else. It also
		// guarantees no line starts with # or ;, which rsync would read as a
		// comment and drop.
		b.WriteString("/")
		b.WriteString(p)
		b.WriteString("\n")
	}
	if _, err := f.WriteString(b.String()); err != nil {
		f.Close()
		cleanup()
		return "", noop, err
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", noop, err
	}
	return "--exclude-from=" + f.Name(), cleanup, nil
}

// gitignoreMergeRule is the per-directory merge rule used only when there is no
// git work tree to ask. Written without shell quotes because there is no shell
// here — the rule is one argv element and quoting it would make the quotes part
// of the rule, so rsync would look for a file literally named "'- .gitignore'".
const gitignoreMergeRule = "--filter=:- .gitignore"

// isGitWorkTree reports whether root is inside a git work tree. Used to choose
// between asking git and falling back, so a plain directory is not an error.
func isGitWorkTree(root string) bool {
	out, err := exec.Command("git", "-C", root, "rev-parse", "--is-inside-work-tree").Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// FetchArgs returns the argv that copies remoteRel — a path relative to the
// target's workdir — back to localPath.
//
// Per the locked report_fetch decision this is called once per package,
// immediately after that package's run, so a package with no report means
// exactly what it means for a local run: nothing was learned about it.
func FetchArgs(t Target, remoteRel, local string) ([]string, error) {
	sub, err := t.In(remoteRel)
	if err != nil {
		return nil, err
	}
	dst, err := localPath(local, "local destination")
	if err != nil {
		return nil, err
	}
	return []string{"rsync", "-a", t.Host + ":" + sub.Workdir, dst}, nil
}

// localPath cleans and checks a path on THIS machine before it becomes an
// rsync operand. rsync has no end-of-options token, so a leading dash is an
// option rather than a path — the same reason argfence lists it as Reject.
func localPath(p, field string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("remote: empty %s: %w", field, ErrUnsafeTarget)
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("remote: %s %q is not absolute: %w", field, p, ErrUnsafeTarget)
	}
	return filepath.Clean(p), nil
}

// shellQuote wraps s so a remote POSIX shell reads it as one literal word.
// Single quotes suspend every expansion; the only character they cannot carry
// is a single quote, which is closed, escaped and reopened.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ExitError is a remote command's failure, carrying the exit code so callers
// branch on the NUMBER rather than on stderr prose. Stderr text varies by ssh
// version, locale and remote shell; the code does not.
type ExitError struct {
	Bin  string // "ssh" or "rsync" — which leg failed
	Host string
	Code int
	kind error // one of ErrTransport / ErrPartial / ErrRemoteCommand
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("remote %s on %s exited %d: %v", e.Bin, e.Host, e.Code, e.kind)
}

// Unwrap exposes the class, so callers use errors.Is(err, ErrTransport) rather
// than re-deriving the mapping from the code at every site.
func (e *ExitError) Unwrap() error { return e.kind }

// ExitCode exposes the remote program's own status under the same interface
// *exec.ExitError satisfies locally.
//
// The class (Unwrap) answers "did this run at all"; the NUMBER answers "what
// did the runner say", and a caller that declared what its runner's
// collected-nothing code is needs the number. Without this method a run on a
// granted host reaches errors.As with nothing to match, and the same runner
// exiting the same way would be a miss locally and a red suite remotely.
func (e *ExitError) ExitCode() int { return e.Code }

// Classify maps an exit code onto the class its caller branches on.
//
// 255 is ssh's own "I could not do this" code — reserved by ssh precisely so a
// transport failure is distinguishable from whatever the remote program
// returns. rsync relays it, and adds a small set of its own stream failures
// that mean the same thing. Everything else non-zero is the remote PROGRAM
// speaking, which is not this package's business to interpret.
func Classify(bin, host string, code int) error {
	if code == 0 {
		return nil
	}
	kind := ErrRemoteCommand
	switch {
	case code == 255:
		kind = ErrTransport
	case bin == "rsync" && (code == 10 || code == 12 || code == 30 || code == 35):
		// socket I/O error, protocol stream error, I/O timeout, connection
		// timeout — the connection, not the payload.
		kind = ErrTransport
	case bin == "rsync" && (code == 23 || code == 24):
		// partial transfer due to error / due to vanished source files.
		kind = ErrPartial
	}
	return &ExitError{Bin: bin, Host: host, Code: code, kind: kind}
}

// commandFn builds the *exec.Cmd for a fully-built remote argv plus the script
// piped to its stdin. It is a var so tests can substitute a fake and pin both
// without a live host.
//
// stdin is part of the seam rather than set by the caller afterwards, because
// for an ssh command it now carries EVERYTHING that distinguishes one remote
// invocation from another — the argv is the same four elements every time.
var commandFn = buildCommand

// buildCommand turns a built argv into a command.
//
// Written as Command(argv[0]) plus an explicit Args assignment rather than the
// usual Command(argv[0], argv[1:]...) spread, ON PURPOSE: the subprocess argv
// audit skips any call carrying a `...` spread, because a spread hides its
// elements from an AST walk. The spread form would therefore pass that gate by
// accident. This form is evaluated by the audit and is accepted by a named
// entry with a reason in internal/cmd/subprocargs_audit_test.go — accommodation
// that someone had to write down, rather than a silence nobody chose.
func buildCommand(argv []string, stdin string) *exec.Cmd {
	cmd := exec.Command(argv[0])
	cmd.Args = argv
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	return cmd
}

// run executes a built argv and returns its stdout, classifying any failure.
// stdin, when non-empty, is the script the remote shell reads.
func run(host string, argv []string, stdin string) (string, error) {
	cmd := commandFn(argv, stdin)
	out, err := cmd.Output()
	if err == nil {
		return string(out), nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return string(out), Classify(argv[0], host, ee.ExitCode())
	}
	// The local binary is missing, or could not be started at all. That is a
	// transport failure by any useful definition: nothing ran on the remote.
	return string(out), fmt.Errorf("remote %s on %s: %w: %v", argv[0], host, ErrTransport, err)
}

// Exec runs argv on the target and returns its stdout. It is the single place
// a remote command is spawned from this package.
func Exec(t Target, argv []string) (string, error) {
	full, err := SSHArgs(t)
	if err != nil {
		return "", err
	}
	script, err := Script(t, argv)
	if err != nil {
		return "", err
	}
	return run(t.Host, full, script)
}

// ExecScript runs a script this package already built — DetachScript's or
// StatusScript's — on the target, returning its output.
//
// Separate from Exec because Exec builds Script from an argv, and the detached
// scripts are not one argv: they are a sequence with redirections and a
// backgrounded job, which no argv can express. Both go down the same ssh
// transport and classify failures the same way, so the difference is only in
// who composed the text.
func ExecScript(t Target, script string) (string, error) {
	full, err := SSHArgs(t)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(script) == "" {
		return "", fmt.Errorf("remote: empty script for host %q", t.Host)
	}
	return run(t.Host, full, script)
}

// Readiness is what a probe learned about a host.
type Readiness struct {
	// Cores is the host's online core count, read from the host itself.
	Cores int
	// Missing names the requested tools the host does not have, in the order
	// they were asked for. Empty means every one resolved.
	Missing []string
}

// Probe reads the target's core count and checks which of tools are present.
//
// The core count comes from the REMOTE machine and is the only number the
// worker default may derive from — reading runtime.NumCPU() here is the exact
// bug the remote_workers decision names, and it would size a 32-core host's run
// by a laptop's core count.
//
// getconf _NPROCESSORS_ONLN rather than nproc: it is POSIX, present on Linux
// and macOS alike, and prints a bare integer.
//
// A tool that is absent is recorded, not returned as an error — doctor wants
// the whole list in one pass. A TRANSPORT failure aborts immediately, because
// "the host is unreachable" must never be reported as "every tool is missing".
func Probe(t Target, tools []string) (Readiness, error) {
	out, err := Exec(t, []string{"getconf", "_NPROCESSORS_ONLN"})
	if err != nil {
		return Readiness{}, err
	}
	cores, err := parseCores(t.Host, out)
	if err != nil {
		return Readiness{}, err
	}
	r := Readiness{Cores: cores}
	for _, tool := range tools {
		if _, err := Exec(t, []string{"command", "-v", tool}); err != nil {
			if errors.Is(err, ErrRemoteCommand) {
				r.Missing = append(r.Missing, tool)
				continue
			}
			return Readiness{}, err
		}
	}
	return r, nil
}

// parseCores reads the probe's output. Unparseable or non-positive output is an
// error rather than a fallback: a silent fallback is how the local core count
// gets used for a remote run.
func parseCores(host, out string) (int, error) {
	s := strings.TrimSpace(out)
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("remote %s: unreadable core count %q: %w", host, s, ErrRemoteCommand)
	}
	if n <= 0 {
		return 0, fmt.Errorf("remote %s: reported %d cores: %w", host, n, ErrRemoteCommand)
	}
	return n, nil
}

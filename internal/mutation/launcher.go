package mutation

// The remote seam the three adapters share.
//
// Gremlins, Stryker and Stryker.NET already shared one knob — Prefix, the
// runtime command prefix — and each carried its own near-copy of the two-line
// buildCmd that applies it. Remoting adds four more steps (push the tree, clear
// the stale report, wrap the tool argv in ssh, fetch the report back) with an
// ORDER that matters, and three near-copies of an ordered sequence is three
// places for the order to drift. This file is that sequence, written once.
//
// The adapters hold the remote as a NAMED field (`Remote *remote.Target`) and
// keep their own `Prefix string`. That shape is deliberate: Go's keyed struct
// literals do not promote through an embedded field, so folding Prefix into an
// embedded Launcher would break every existing `&Stryker{Prefix: …}` site — in
// verify.go, survivor_drain.go and the adapters' own tests — and leave this
// change with no green commit to land on.
//
// What the launcher does NOT do is decide policy. Whether a remote is
// configured is read in internal/cmd; what a remote failure means for a verify
// report is decided in internal/verify. Here there is only: which machine does
// this command run on, and in what order.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"

	"github.com/Rivil/dross/internal/remote"
)

// Launcher owns where a mutation adapter's commands run.
//
// Prefix and Target are mutually exclusive and refused together at
// construction. The refusal is a defensive invariant rather than live
// behaviour: per the locked docker_prefix_under_remote decision the wiring
// DROPS the docker prefix when a remote is granted, so nothing upstream ever
// builds both. It stays because "unreachable" is a claim about today's callers,
// and a prefix silently wrapping an ssh invocation would run the mutation tool
// inside the local dev container against a tree that is not there.
type Launcher struct {
	// Prefix is the local runtime command prefix ("docker compose exec app").
	// Empty means run natively.
	Prefix string
	// Target is the machine to delegate to. Nil means run locally, and every
	// remote step below becomes a no-op — a local run's argv is byte-identical
	// to what it was before this file existed.
	Target *remote.Target
	// Adapter is the adapter's Name(), which keys the remote-report table. An
	// adapter absent from that table cannot run remotely.
	Adapter string
	// ProjectRoot is the local tree pushed to the remote. It is the rsync
	// source verbatim, not a directory derived from it.
	ProjectRoot string
	// Workdir is the repo-relative directory the TOOL runs in — Stryker's
	// monorepo knob. Every remote path in the run is relative to it, because
	// cmd.Dir on the local ssh process says nothing about the remote cwd.
	Workdir string

	// pushed records that the one-shot tree sync has happened, so the
	// per-package loop cannot re-push between packages. rsync is the expensive
	// step and a second push mid-run would also overwrite reports fetched from
	// earlier packages.
	pushed bool
}

// remoteReportPaths is the CLOSED table of where each adapter's report lands on
// the remote, relative to the tool's workdir.
//
// Closed, and an absent adapter is an error rather than a skipped step, because
// the failure mode of a silent no-op here is invisible and wrong in the worst
// direction: no rm means a stale remote report survives the run, and no fetch
// means the local report the adapter reads is whatever a previous run left. The
// score would then be computed from numbers nobody measured this time.
//
// The value is a function because gremlins' report path is a function of the
// package it just ran, which is only known inside the loop.
var remoteReportPaths = map[string]func(key string) string{
	"gremlins": func(pkg string) string {
		return path.Join("reports", "gremlins", sanitizePkg(pkg)+".json")
	},
	"stryker": func(string) string {
		return path.Join("reports", "mutation", "mutation.json")
	},
	"stryker-net": func(outDir string) string {
		if outDir == "" {
			return "StrykerOutput"
		}
		return outDir
	},
}

// launcherCommand is the single process-construction seam for every command a
// REMOTE run spawns — the rsync push, the ssh rm, the ssh tool run and the
// rsync fetch alike. One seam rather than one per step is what lets a test
// assert the ORDER, which is the property most of this file exists to hold.
//
// Local runs keep going through each adapter's own buildCmd seam, untouched, so
// the existing argv-pinning tests keep measuring exactly what they measured.
var launcherCommand = func(argv []string) *exec.Cmd {
	return exec.Command(argv[0], argv[1:]...)
}

// newLauncher validates the combination before anything can be spawned.
func newLauncher(adapter, prefix string, target *remote.Target, projectRoot, workdir string) (*Launcher, error) {
	if prefix != "" && target != nil {
		return nil, fmt.Errorf(
			"mutation: adapter %q has both a runtime prefix (%q) and a remote host (%q) — "+
				"a prefix wraps the LOCAL ssh process, so the tool would run in this machine's "+
				"container against a tree that is not there",
			adapter, prefix, target.Host)
	}
	if target != nil {
		if err := target.Validate(); err != nil {
			return nil, err
		}
		if _, ok := remoteReportPaths[adapter]; !ok {
			return nil, fmt.Errorf(
				"mutation: adapter %q has no entry in the remote report table — a remote run "+
					"would clear no stale report and fetch no new one, and would score whatever "+
					"the last run left behind", adapter)
		}
	}
	return &Launcher{
		Prefix:      prefix,
		Target:      target,
		Adapter:     adapter,
		ProjectRoot: projectRoot,
		Workdir:     workdir,
	}, nil
}

// remoteRun reports whether this launcher delegates.
func (l *Launcher) remoteRun() bool { return l != nil && l.Target != nil }

// toolTarget is the remote rooted at the directory the TOOL runs in.
func (l *Launcher) toolTarget() (remote.Target, error) {
	return l.Target.In(l.Workdir)
}

// reportRel resolves the adapter's remote report path through the closed table.
func (l *Launcher) reportRel(key string) (string, error) {
	fn, ok := remoteReportPaths[l.Adapter]
	if !ok {
		return "", fmt.Errorf("mutation: adapter %q has no entry in the remote report table", l.Adapter)
	}
	return fn(key), nil
}

// runRemote runs a fully-built remote argv, classifying the failure by exit
// code rather than by stderr prose — stderr varies by ssh version, locale and
// remote shell; the code does not.
func (l *Launcher) runRemote(argv []string) error {
	err := launcherCommand(argv).Run()
	if err == nil {
		return nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return remote.Classify(argv[0], l.Target.Host, ee.ExitCode())
	}
	// The local ssh/rsync binary is missing or could not start. Nothing ran on
	// the remote, which is a transport failure by any useful definition.
	return fmt.Errorf("remote %s on %s: %w: %v", argv[0], l.Target.Host, remote.ErrTransport, err)
}

// ensurePushed syncs the working tree to the remote, exactly once per Run.
//
// It is called at the top of every remote step rather than from one place, so
// the push cannot be skipped by a step that runs first — the rm is the first
// remote command in the per-package loop, and an rm against an unsynced workdir
// would be operating on the previous run's tree.
func (l *Launcher) ensurePushed() error {
	if !l.remoteRun() || l.pushed {
		return nil
	}
	argv, err := remote.SyncArgs(*l.Target, l.ProjectRoot)
	if err != nil {
		return err
	}
	// Set before the exec, not after: a failed push must not be retried by the
	// next step, which would turn one transport failure into one per package.
	l.pushed = true
	return l.runRemote(argv)
}

// toolCmd builds the command that runs the adapter's own argv.
//
// Local, it defers to the adapter's existing seam and the result is what it
// always was. Remote, argv[0] becomes "ssh" and the adapter's binary never
// reaches a local exec — which is what "the local machine spawns no compile or
// test process" means in practice.
func (l *Launcher) toolCmd(argv []string, local func([]string) *exec.Cmd) (*exec.Cmd, error) {
	if !l.remoteRun() {
		return local(argv), nil
	}
	if err := l.ensurePushed(); err != nil {
		return nil, err
	}
	t, err := l.toolTarget()
	if err != nil {
		return nil, err
	}
	full, err := remote.SSHArgs(t, argv)
	if err != nil {
		return nil, err
	}
	return launcherCommand(full), nil
}

// clearReport removes the report location on BOTH machines before the tool runs.
//
// rsync --delete cannot do the remote half: the locked sync_mechanism filter is
// `:- .gitignore`, which PROTECTS ignored paths from deletion, and every report
// location is gitignored. So a stale remote report survives the push, and only
// this explicit rm removes it.
//
// The local half matters for the same reason in reverse. If the remote produces
// no report this run, the fetch brings nothing back and the adapter would read
// whatever the previous run left at the local path — scoring yesterday's
// numbers as today's.
func (l *Launcher) clearReport(key, localPath string) error {
	if !l.remoteRun() {
		return nil
	}
	if err := l.ensurePushed(); err != nil {
		return err
	}
	rel, err := l.reportRel(key)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(localPath); err != nil {
		return fmt.Errorf("clear local report %s: %w", localPath, err)
	}
	t, err := l.toolTarget()
	if err != nil {
		return err
	}
	argv, err := remote.SSHArgs(t, []string{"rm", "-rf", rel})
	if err != nil {
		return err
	}
	return l.runRemote(argv)
}

// fetchReport copies the report the tool just wrote back to the local path the
// adapter reads it from.
//
// Per the locked report_fetch decision this happens immediately after the
// package's own run rather than in one batch at the end, so "no report for this
// package" keeps meaning exactly what it means for a local run: nothing was
// learned about it.
func (l *Launcher) fetchReport(key, localDest string) error {
	if !l.remoteRun() {
		return nil
	}
	rel, err := l.reportRel(key)
	if err != nil {
		return err
	}
	t, err := l.toolTarget()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(localDest), 0o755); err != nil {
		return fmt.Errorf("prepare local report dir: %w", err)
	}
	argv, err := remote.FetchArgs(t, rel, localDest)
	if err != nil {
		return err
	}
	return l.runRemote(argv)
}

// launcherWorkers is the worker default, read from the machine the run actually
// happens on.
//
// The halving rule is unchanged and stays deliberate — a clean 6-worker run
// measured 0 timeouts where 14 workers produced 539 false timeouts that masked
// real survivors. What changes under a remote is only WHICH machine's core
// count it halves. Reading runtime.NumCPU() for a remote run is the exact bug
// the locked remote_workers decision names: it would size a 32-core host's run
// by this laptop.
func launcherWorkers(t *remote.Target) int {
	if t != nil && t.Cores > 0 {
		if w := t.Cores / 2; w > 0 {
			return w
		}
		return 1
	}
	return defaultWorkers()
}

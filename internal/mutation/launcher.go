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
	"sort"
	"strconv"
	"strings"

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

	// CacheVars are the environment variable names this stack's toolchain reads
	// for its build cache, declared by the stack profile (stack.MutationCache).
	// Empty leaves the run exactly as it was before scratch caches existed.
	CacheVars []string

	// PackageManager is the project's own package manager (stack.package_manager),
	// which keys the Node restore. It is deliberately NOT defaulted: installing
	// with the wrong manager produces a tree the tool resolves differently, and
	// pnpm's symlinked layout is load-bearing — stryker.conf.json has to declare
	// its plugins explicitly BECAUSE that layout defeats auto-discovery.
	PackageManager string

	// remoteScratchGone records that Close already wiped the host side, so a
	// deferred Close and an explicit one do not each pay for an ssh round trip.
	remoteScratchGone bool

	// remoteSpaceChecked makes the host-side free-space probe once-per-run: it
	// is one ssh, and asking again before every package would pay a round trip
	// to re-learn an answer that cannot have changed enough to matter.
	remoteSpaceChecked bool

	// scratch is this run's private build cache, created on first use and
	// wiped by Close. Nil until something needs it.
	scratch *scratch

	// pushed records that the one-shot tree sync has happened, so the
	// per-package loop cannot re-push between packages. rsync is the expensive
	// step and a second push mid-run would also overwrite reports fetched from
	// earlier packages.
	pushed bool
	// restored records the one-shot dependency restore, for the same reason.
	restored bool
}

// remoteRestores is the CLOSED table of how each adapter restores its language's
// dependencies on the remote, before the tool runs.
//
// A nil argv means the language needs none — Go builds from the synced source.
// An adapter ABSENT from the table is an error rather than a skipped restore,
// on the same reasoning as remoteReportPaths: the failure mode of a silent
// no-op is a run that reports against a dependency-less tree, and every mutant
// dying for the wrong reason looks like a very good test suite.
var remoteRestores = map[string]func(pm string) ([]string, error){
	"gremlins": func(string) ([]string, error) {
		// The synced tree is the source; `go test` resolves modules itself.
		return nil, nil
	},
	"stryker": nodeRestoreArgv,
	"stryker-net": func(string) ([]string, error) {
		return []string{"dotnet", "restore"}, nil
	},
}

// nodeRestores maps a package manager to its LOCKFILE-RESPECTING install.
//
// The lockfile-respecting form, never the loose one. `npm install`, bare `pnpm
// install` and `yarn install` without --immutable all resolve fresh versions
// when the lockfile and manifest disagree — which measures a dependency tree
// the lockfile does not describe, on a machine nobody is looking at.
var nodeRestores = map[string][]string{
	"pnpm": {"pnpm", "install", "--frozen-lockfile"},
	"npm":  {"npm", "ci"},
	"yarn": {"yarn", "install", "--immutable"},
	"bun":  {"bun", "install", "--frozen-lockfile"},
}

// nodeRestoreArgv resolves the Node restore from the project's OWN package
// manager. It refuses to guess.
//
// Guessing npm is not a harmless default. pnpm's symlinked node_modules layout
// is load-bearing — a stryker.conf.json in a pnpm repo declares its plugins
// explicitly precisely BECAUSE that layout defeats auto-discovery — so
// installing with the wrong manager produces a tree stryker resolves
// differently. And `npm ci` in a pnpm repo with no package-lock.json simply
// fails, which is the concrete case that motivated this table.
func nodeRestoreArgv(pm string) ([]string, error) {
	if pm == "" {
		return nil, fmt.Errorf(
			"stack.package_manager is not set, so a remote run does not know how to restore " +
				"dependencies. Set it (`dross project set stack.package_manager pnpm`) — dross will " +
				"not guess, because installing with the wrong manager produces a tree stryker " +
				"resolves differently")
	}
	argv, ok := nodeRestores[pm]
	if !ok {
		known := make([]string, 0, len(nodeRestores))
		for name := range nodeRestores {
			known = append(known, name)
		}
		sort.Strings(known)
		return nil, fmt.Errorf(
			"stack.package_manager = %q has no remote restore command (known: %s) — "+
				"a skipped restore would report against a dependency-less tree",
			pm, strings.Join(known, ", "))
	}
	return argv, nil
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
// stdin, when non-empty, is the script the remote shell reads — it carries the
// workdir, the tool argv and every environment value, none of which appear in
// argv. The seam takes both so a test can assert on either, and so nothing can
// reach the remote through a channel the recorder does not see.
var launcherCommand = func(argv []string, stdin string) *exec.Cmd {
	cmd := exec.Command(argv[0], argv[1:]...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	return cmd
}

// newLauncher validates the combination before anything can be spawned.
func newLauncher(adapter, prefix string, target *remote.Target, projectRoot, workdir string, cacheVars []string) (*Launcher, error) {
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
		if _, ok := remoteRestores[adapter]; !ok {
			return nil, fmt.Errorf(
				"mutation: adapter %q has no entry in the remote restore table — a remote run "+
					"would skip the dependency install and report against a tree that has none",
				adapter)
		}
	}
	return &Launcher{
		Prefix:      prefix,
		Target:      target,
		Adapter:     adapter,
		ProjectRoot: projectRoot,
		Workdir:     workdir,
		CacheVars:   append([]string(nil), cacheVars...),
	}, nil
}

// remoteRun reports whether this launcher delegates.
func (l *Launcher) remoteRun() bool { return l != nil && l.Target != nil }

// toolTarget is the remote rooted at the directory the TOOL runs in.
func (l *Launcher) toolTarget() (remote.Target, error) {
	t, err := l.Target.In(l.Workdir)
	if err != nil {
		return t, err
	}
	// The remote scratch sits under the GRANTED WORKDIR, never the host's
	// default temp (the locked scratch_location decision). helicon's /tmp is a
	// 32 GB RAM-backed tmpfs shared with a running LLM, so a build cache there
	// is an outage rather than a slowdown.
	//
	// TMPDIR rides along with the declared cache vars because gremlins copies
	// the whole module via os.MkdirTemp, which honours TMPDIR and not GOTMPDIR
	// — pointing the Go toolchain at a big volume covers the compiler and not
	// the harness wrapping it.
	if len(l.CacheVars) > 0 {
		dir := remoteScratchDir(l.Target.Workdir)
		for _, a := range remoteAssignments(dir, append(append([]string(nil), l.CacheVars...), "TMPDIR")) {
			name, value, _ := strings.Cut(a, "=")
			t.Env = append(t.Env, remote.EnvVar{Name: name, Value: value})
		}
	}
	return t, nil
}

// localCacheEnv creates this run's scratch on first use and returns its
// assignments. Nil when the stack declared no cache vars, so nothing about such
// a run changes.
func (l *Launcher) localCacheEnv() ([]string, error) {
	if len(l.CacheVars) == 0 {
		return nil, nil
	}
	if l.scratch == nil {
		// Beside the project — not inside it, and not in os.TempDir(). See
		// scratchDirFor: a cache inside the tree shows up in git status and in
		// the adapters' file scans, and one in the machine's temp is the
		// RAM-backed volume this exists to avoid.
		s, err := newScratch(scratchDirFor(l.ProjectRoot), l.CacheVars, os.Stderr)
		if err != nil {
			return nil, err
		}
		l.scratch = s
	}
	return l.scratch.Assignments(), nil
}

// Close wipes anything this launcher created. Every adapter defers it, so it
// runs on a clean finish, an adapter failure and an early return alike; it is
// idempotent because a deferred call must not fight an explicit one.
func (l *Launcher) Close() error {
	if l == nil {
		return nil
	}
	if err := l.removeRemoteScratch(); err != nil {
		// Same policy as the local wipe (locked wipe_policy): reported, never
		// fatal. A completed measurement must not be lost to a cleanup error,
		// and a leak nobody sees is what filled the disk in the first place.
		fmt.Fprintf(os.Stderr, "mutation: could not remove the remote scratch build cache: %v\n", err)
	}
	l.remoteScratchGone = true
	return l.scratch.Remove()
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
func (l *Launcher) runRemote(argv []string, stdin string) error {
	err := launcherCommand(argv, stdin).Run()
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
	// Before the push, not after. Refusing here costs one df; refusing after
	// the sync has already written the whole working tree onto the volume the
	// check exists to protect.
	if err := l.checkRemoteScratchSpace(); err != nil {
		return err
	}
	argv, cleanup, err := remote.SyncArgs(*l.Target, l.ProjectRoot)
	if err != nil {
		return err
	}
	// The exclude list is a temp file rsync reads; it must outlive the spawn
	// and not outlive this call.
	defer cleanup()
	// Set before the exec, not after: a failed push must not be retried by the
	// next step, which would turn one transport failure into one per package.
	l.pushed = true
	return l.runRemote(argv, "")
}

// toolCmd builds the command that runs the adapter's own argv.
//
// Local, it defers to the adapter's existing seam and the result is what it
// always was. Remote, argv[0] becomes "ssh" and the adapter's binary never
// reaches a local exec — which is what "the local machine spawns no compile or
// test process" means in practice.
func (l *Launcher) toolCmd(argv []string, local func([]string) *exec.Cmd) (*exec.Cmd, error) {
	if !l.remoteRun() {
		cmd := local(argv)
		// The scratch is applied HERE rather than inside each adapter's own
		// buildCmd seam, because this is the one construction point both
		// transports pass through — an adapter that grew its own would be an
		// adapter whose runs quietly kept using the shared cache.
		assignments, err := l.localCacheEnv()
		if err != nil {
			return nil, err
		}
		if len(assignments) > 0 {
			// os.Environ() first so the assignments WIN: exec resolves a
			// repeated name to the last occurrence, which is what lets a
			// scratch override an ambient GOCACHE the developer exported.
			cmd.Env = append(append(os.Environ(), cmd.Env...), assignments...)
		}
		return cmd, nil
	}
	if err := l.ensurePushed(); err != nil {
		return nil, err
	}
	if err := l.ensureRestored(); err != nil {
		return nil, err
	}
	t, err := l.toolTarget()
	if err != nil {
		return nil, err
	}
	full, err := remote.SSHArgs(t)
	if err != nil {
		return nil, err
	}
	// mkdir FIRST, in the same script. The exported TMPDIR points here and
	// gremlins copies the module through os.MkdirTemp, which fails outright
	// against a directory that does not exist — "impossible to create the
	// workdir" and the run ends before measuring anything. Exporting a path
	// without creating it is not a redirection, it is a broken run.
	cmds := [][]string{argv}
	if dir := l.remoteScratch(); dir != "" {
		cmds = [][]string{{"mkdir", "-p", dir}, argv}
	}
	script, err := remote.ScriptAll(t, cmds)
	if err != nil {
		return nil, err
	}
	return launcherCommand(full, script), nil
}

// remoteScratch is this run's scratch directory ON THE HOST, or "" when there
// is nothing to redirect.
func (l *Launcher) remoteScratch() string {
	if !l.remoteRun() || len(l.CacheVars) == 0 {
		return ""
	}
	if base := l.Target.ScratchBase; base != "" {
		return path.Join(base, ".dross-cache", path.Base(path.Clean(l.Target.Workdir)))
	}
	return remoteScratchDir(l.Target.Workdir)
}

// checkRemoteScratchSpace refuses a remote run whose host-side scratch volume is
// already too full, once per run.
//
// This is the half that matters most: the volume that filled was the HOST's,
// and a check that only ever measured the laptop would have missed the incident
// this exists for entirely. It asks the host, because only the host knows —
// /home there turned out to be part of a 75 GB root LV while the 300 GB volume
// provisioned for the work sat elsewhere, and nothing local could have seen
// that.
//
// One cheap `df` before the first mutant, not a campaign. A df that fails for
// any reason returns nil: an unanswerable check must not become a new way for a
// run not to happen.
func (l *Launcher) checkRemoteScratchSpace() error {
	dir := l.remoteScratch()
	if dir == "" || l.remoteSpaceChecked {
		return nil
	}
	l.remoteSpaceChecked = true
	floor := scratchMinFreeBytes()
	if floor == 0 {
		return nil
	}
	t, err := l.toolTarget()
	if err != nil {
		return nil //nolint:nilerr // an unanswerable check is not a refusal
	}
	// The base, not the leaf: the leaf does not exist yet, and df on a missing
	// path answers nothing. Both are on the same volume.
	base := path.Dir(dir)
	out, err := remoteSpaceProbe(t, []string{"df", "-Pk", base})
	if err != nil {
		return nil //nolint:nilerr // see above
	}
	free, ok := parseDfAvailBytes(out)
	if !ok || free >= floor {
		return nil
	}
	return fmt.Errorf(
		"mutation: refusing to start on %s — the scratch build cache would land on %s, which has %.1f GB free (floor %.0f GB).\n\n"+
			"A mutation run writes tens of gigabytes there; one measured run used ~60 GB at ~1.3 GB/min and came within minutes of filling the host.\n\n"+
			"  put the host's scratch elsewhere:  dross local set remote_scratch_base /path/on/a/bigger/volume\n"+
			"  lower or disable the floor:        %s=<gb>   (0 disables)",
		t.Host, base, float64(free)/bytesPerGB, float64(floor)/bytesPerGB, ScratchMinFreeEnv)
}

// remoteSpaceProbe is the seam the free-space check spawns through. remote.Exec
// captures stdout, which is what a probe wants and what the streaming spawner
// deliberately is not. Production never reassigns it.
var remoteSpaceProbe = remote.Exec

// parseDfAvailBytes reads the Available column out of `df -Pk` output.
//
// -P is the portable output format, which guarantees one line per filesystem
// and stops a long device name wrapping onto its own line — the wrap is exactly
// what makes ad-hoc df parsing wrong on the hosts where it matters.
func parseDfAvailBytes(out string) (uint64, bool) {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 {
		return 0, false
	}
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 4 {
		return 0, false
	}
	kib, err := strconv.ParseUint(fields[3], 10, 64)
	if err != nil {
		return 0, false
	}
	return kib * 1024, true
}

// removeRemoteScratch wipes the host-side scratch.
//
// The path is derived, never supplied, and is checked against that derivation
// before an `rm -rf` is built from it: a removal command assembled from a value
// this function did not compute is the one shape worth being paranoid about,
// even though Target.Validate already constrains the workdir.
func (l *Launcher) removeRemoteScratch() error {
	dir := l.remoteScratch()
	if dir == "" || l.remoteScratchGone {
		return nil
	}
	// Two independent checks, because this builds an `rm -rf`. The first is the
	// real one: the path must be exactly what scratchDirFor derives from the
	// granted workdir, so a Target swapped underneath this launcher cannot aim
	// the removal somewhere else. The second is a floor on depth — no removal
	// of "/", "/home" or any two-segment path — which costs nothing and bounds
	// the damage if the derivation itself is ever changed carelessly.
	if dir != remoteScratchDir(l.Target.Workdir) {
		return fmt.Errorf("mutation: refusing to remove %q — not this run's scratch for %q", dir, l.Target.Workdir)
	}
	if !filepath.IsAbs(dir) || len(strings.Split(strings.Trim(dir, "/"), "/")) < 3 {
		return fmt.Errorf("mutation: refusing to remove %q — too close to the filesystem root", dir)
	}
	t, err := l.toolTarget()
	if err != nil {
		return err
	}
	full, err := remote.SSHArgs(t)
	if err != nil {
		return err
	}
	script, err := remote.Script(t, []string{"rm", "-rf", dir})
	if err != nil {
		return err
	}
	return l.runRemote(full, script)
}

// restoreArgv resolves this adapter's dependency-restore command, or nil when
// the language needs none.
//
// Pure and idempotent, so callers may resolve it EARLY — before anything is
// spawned — to turn an unset or unrecognised package manager into a refusal
// rather than a failure discovered after the tree has been pushed.
func (l *Launcher) restoreArgv() ([]string, error) {
	if !l.remoteRun() {
		return nil, nil
	}
	fn, ok := remoteRestores[l.Adapter]
	if !ok {
		return nil, fmt.Errorf("mutation: adapter %q has no entry in the remote restore table", l.Adapter)
	}
	return fn(l.PackageManager)
}

// ensureRestored installs the language's dependencies on the remote, once,
// immediately before the first tool command.
//
// It runs in the SAME joined directory the tool will — for a Stryker with
// Workdir="web", both cd into <remote workdir>/web. Restoring at the repo root
// is how a monorepo run installs against the wrong lockfile, or none.
//
// A failed restore is fatal and named. A restore that fell through would leave
// the tool measuring a tree with no dependencies, where every mutant dies
// because nothing imports — which scores as an excellent test suite.
func (l *Launcher) ensureRestored() error {
	if !l.remoteRun() || l.restored {
		return nil
	}
	argv, err := l.restoreArgv()
	if err != nil {
		return err
	}
	l.restored = true
	if len(argv) == 0 {
		return nil
	}
	t, terr := l.toolTarget()
	if terr != nil {
		return terr
	}
	full, serr := remote.SSHArgs(t)
	if serr != nil {
		return serr
	}
	script, scerr := remote.Script(t, argv)
	if scerr != nil {
		return scerr
	}
	if rerr := l.runRemote(full, script); rerr != nil {
		return fmt.Errorf("remote dependency restore `%s` failed on %s: %w",
			strings.Join(argv, " "), l.Target.Host, rerr)
	}
	return nil
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
	// UNCONDITIONAL, and it did not used to be: this whole function returned
	// early for a local run, so a local tool that wrote no report left the
	// adapter reading whatever the PREVIOUS run had left at this path.
	//
	// Observed 2026-08-26 while proving the --mutate escaping fix: a run that
	// instrumented zero files and died with "No tests were executed" returned
	// a nil error and a Report parsed from a mutation.json two days old. The
	// doc above already described this hazard for the local half of a REMOTE
	// run; it is identical for a wholly local one, which had no such excuse.
	if err := os.RemoveAll(localPath); err != nil {
		return fmt.Errorf("clear local report %s: %w", localPath, err)
	}
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
	t, err := l.toolTarget()
	if err != nil {
		return err
	}
	argv, err := remote.SSHArgs(t)
	if err != nil {
		return err
	}
	// The mkdir is the other half of the rm, and it is what makes a remote run
	// able to produce a report at all.
	//
	// reports/ is gitignored, and the locked `:- .gitignore` sync filter keeps
	// ignored paths off the wire — deliberately, so node_modules and build
	// output do not cross. The consequence is that the report DIRECTORY never
	// arrives either. Gremlins does not create parent dirs for --output: it
	// fails on the first mutant write with "impossible to write file" and
	// leaves nothing behind. The fetch then exits 23, which is correctly read
	// as UnmeasuredMissing, and every package reports "no report — gremlins
	// gathered no covered mutants". A fully working remote run scores 0/0 and
	// reads as nothing-to-measure.
	//
	// The local path has always done this (os.MkdirAll in gremlins.go); the
	// remote had no equivalent.
	script, serr := remote.ScriptAll(t, [][]string{
		{"rm", "-rf", rel},
		{"mkdir", "-p", path.Dir(rel)},
	})
	if serr != nil {
		return serr
	}
	return l.runRemote(argv, script)
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
	return l.runRemote(argv, "")
}

// remoteExitFatal decides whether a mutation tool's non-zero exit ends the run.
//
// The distinction it draws is c-4's whole content, and today's adapters cannot
// draw it: they tolerate ANY *exec.ExitError, on the reasoning that a mutation
// tool exits non-zero when mutants survive — "ran, bad results". That reasoning
// is sound locally and wrong over ssh, because ssh relays the remote program's
// code through the SAME channel it reports its own failures on. Under the old
// rule an unreachable host and an uninstalled remote toolchain both become
// "gremlins wrote no report", which is UnmeasuredMissing — a non-fatal skip the
// score excludes. The leg then reports clean because nothing ran.
//
//   - 255 is ssh's own reserved code, chosen precisely so a transport failure is
//     distinguishable from whatever the remote program returned. Fatal.
//   - 126/127 are the remote SHELL saying it could not execute the command:
//     permission denied, or not found. The tool never ran, so any report present
//     is from an earlier run. Fatal, and named — "install gremlins on helicon"
//     is an action; "no report" is not.
//   - Everything else is the remote PROGRAM speaking. gremlins exits 1 when
//     mutants survive, and that is a successful measurement with bad results —
//     tolerated exactly as it is locally.
//
// Local runs keep today's behaviour unchanged: nil for every ExitError.
func (l *Launcher) remoteExitFatal(bin string, code int) error {
	if !l.remoteRun() || code == 0 {
		return nil
	}
	switch code {
	case 255:
		return fmt.Errorf("remote %s on %s: ssh exited 255 — the host is unreachable or the connection failed, so nothing was measured: %w",
			bin, l.Target.Host, remote.ErrTransport)
	case 126, 127:
		return fmt.Errorf("%s not found on %s (the remote shell exited %d) — install it there; `dross doctor` reports remote toolchains before a run: %w",
			bin, l.Target.Host, code, remote.ErrRemoteCommand)
	}
	return nil
}

// reportlessExitFatal closes the gap remoteExitFatal leaves: a tool that exited
// non-zero AND produced no report.
//
// remoteExitFatal judges the exit code alone, and it is right to let a non-zero
// code through — gremlins exits 1 when mutants survive, which is a successful
// measurement with bad results. But that judgement is only sound WITH a report
// in hand. Without one the same code means the tool failed before writing
// anything, and today that lands in UnmeasuredMissing and prints as "gremlins
// gathered no covered mutants" — a sentence that says the package was fine to
// skip.
//
// Found by running this phase against a real host: the remote tree failed
// `go test ./internal/cmd` (six git-dependent tests, see SyncArgs), so gremlins
// could not gather coverage, exited 1 and wrote nothing. Every layer below
// behaved exactly as designed and the run still reported a clean 0.95 with the
// package holding most of the phase's code silently unmeasured.
//
// The two states that already read correctly are preserved: exit 0 with no
// report stays a benign skip (gremlins found no covered mutants), and any exit
// WITH a report stays tolerated. Local runs are untouched.
func (l *Launcher) reportlessExitFatal(bin string, code int) error {
	if !l.remoteRun() || code == 0 {
		return nil
	}
	return fmt.Errorf("remote %s on %s exited %d and wrote no report — it failed before measuring anything, so this package is unmeasured rather than clean; run it there to see why: %w",
		bin, l.Target.Host, code, remote.ErrRemoteCommand)
}

// fetchFatal reports whether a failed report fetch ends the run.
//
// Only ErrPartial is survivable, and only because it is what rsync reports when
// the source file is not there — which is the one remote failure that genuinely
// means what a missing local report means: the tool wrote nothing, so nothing
// was learned about this package. Everything else (a dead connection mid-copy, a
// refused host) is a fetch that tells us nothing about whether a report exists,
// and treating it as "no report" would score the package as unmeasured when it
// may have been measured fine.
func fetchFatal(err error) bool {
	return err != nil && !errors.Is(err, remote.ErrPartial)
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

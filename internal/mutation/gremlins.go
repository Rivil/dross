package mutation

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/Rivil/dross/internal/argfence"
	"github.com/Rivil/dross/internal/remote"
)

// DefaultTimeoutCoefficient is the dross-chosen override for gremlins'
// --timeout-coefficient flag. Gremlins' built-in default is ~3, which
// scales poorly with fast Go test suites (a 75ms baseline gives a
// 0.22s budget per mutant — most mutants TIME OUT before they can be
// killed). 30 mirrors the manual workaround verified during the
// chess-master dogfood session and keeps every mutant inside Go's
// 1–2s compile-and-test cycle.
const DefaultTimeoutCoefficient = 30

// DefaultTestCPU pins each mutant's test run to a single CPU (--test-cpu)
// so the worker count alone governs total CPU use; see Gremlins.TestCPU.
const DefaultTestCPU = 1

// defaultWorkers returns the parallelism gremlins uses when Workers is
// unset: half the machine's CPUs (at least 1). Half leaves headroom so
// concurrent test runs don't oversubscribe the box and time out
// spuriously — a clean 6-worker run measured 0 timeouts where 14 workers
// produced 539 false timeouts that masked real survivors.
func defaultWorkers() int {
	if w := runtime.NumCPU() / 2; w > 0 {
		return w
	}
	return 1
}

// Gremlins adapter for Go mutation testing via go-gremlins/gremlins.
//
// Gremlins works at the package level (not file level), so Run() derives
// unique package dirs from the input files and passes those to gremlins.
// Output is written to <project>/reports/gremlins/output.json.
//
// Install: `go install github.com/go-gremlins/gremlins/cmd/gremlins@latest`
type Gremlins struct {
	Prefix      string // runtime command prefix, e.g. "docker compose exec app"
	ProjectRoot string // host cwd; gremlins JSON is read from here

	// TimeoutCoefficient overrides gremlins' --timeout-coefficient flag.
	// Zero or negative values fall back to DefaultTimeoutCoefficient.
	TimeoutCoefficient int

	// Workers caps how many mutants gremlins runs in parallel (--workers).
	// Zero or negative falls back to defaultWorkers() (NumCPU/2).
	Workers int

	// TestCPU caps the CPUs each mutant's test run may use (--test-cpu).
	// Zero or negative falls back to DefaultTestCPU (1).
	TestCPU int

	// Remote delegates the run to another machine. Nil runs locally, and a
	// local run is byte-identical to what it was before remoting existed.
	//
	// A NAMED field rather than an embedded Launcher: Go's keyed struct
	// literals do not promote, so embedding would break every existing
	// &Gremlins{Prefix: …} construction site. See launcher.go.
	Remote *remote.Target
	// CacheVars are the environment variable names this stack's toolchain reads
	// for its build cache (stack profile mutation_cache.vars). A run is pointed
	// at a scratch copy and the scratch is wiped when the run ends; empty leaves
	// the run exactly as it was.
	CacheVars []string

	// Unmeasured records every package Run excluded from the merged score,
	// with why. It is set by each Run, replacing whatever the previous Run
	// left, so it always describes the most recent invocation.
	//
	// The reasons were previously a local slice printed to stderr and thrown
	// away. A caller that has to decide whether "no survivors" means "clean"
	// or "never measured" cannot read stderr — and the two are the opposite
	// of each other, so the distinction has to survive the call.
	Unmeasured []Unmeasured
}

// UnmeasuredKind is why a package contributed nothing to the merged score.
// The kinds are deliberately distinguishable as data rather than by matching
// on Message: a caller that greps prose breaks the moment the prose is
// reworded, and these three call for opposite handling.
type UnmeasuredKind string

const (
	// UnmeasuredMissing — gremlins wrote no report at all for the package,
	// so nothing is known about it. Absence of survivors here is absence of
	// evidence, and a drain must treat it as fatal rather than as clean.
	UnmeasuredMissing UnmeasuredKind = "missing"

	// UnmeasuredUnreadable — a report exists but could not be parsed. Like
	// missing, it yields no rows: nothing was learned about the package.
	UnmeasuredUnreadable UnmeasuredKind = "unreadable"

	// UnmeasuredUncovered — the report parsed fine, but every mutant in it is
	// NOT COVERED. The package IS measured; it just has no usable coverage,
	// so its rows are real survivors to classify even though excluding them
	// keeps a coverage blind spot from masquerading as a score.
	UnmeasuredUncovered UnmeasuredKind = "uncovered"
)

// Unmeasured is one package left out of the merged score, and why.
type Unmeasured struct {
	// Package is the gremlins package path Run invoked, e.g. "./internal/cmd".
	Package string
	// Kind classifies the exclusion; see UnmeasuredKind.
	Kind UnmeasuredKind
	// Message is the human-readable line, "<package> (<why>)".
	Message string
}

// String renders the entry as it is printed to stderr.
func (u Unmeasured) String() string { return u.Message }

func (g *Gremlins) Name() string { return "gremlins" }

func (g *Gremlins) Supports(file string) bool {
	return strings.HasSuffix(strings.ToLower(file), ".go")
}

// Run invokes gremlins once per touched package, then merges the
// per-package JSON reports into a single normalised Report.
//
// Gremlins is invoked per concrete package — not over one collapsed
// `./<ancestor>/...` path — because a broad recursive scope makes
// gremlins gather empty coverage and exit with "No results to report",
// writing no file. That previously hard-failed the entire verify. Per
// package, the packages gremlins CAN cover yield real results; the ones
// it can't — no report written, or a report with zero covered mutants —
// are excluded from the score and noted, never fatal. Only a failure to
// execute gremlins at all (e.g. not installed) is fatal.
func (g *Gremlins) Run(files []string) (*Report, error) {
	if len(files) == 0 {
		return &Report{Tool: g.Name()}, nil
	}

	// Built before anything is spawned: a refused combination (a prefix AND a
	// remote, an unvalidatable target) must produce no *exec.Cmd at all.
	// Gremlins runs at the repo root, so the launcher carries no workdir.
	lr, err := newLauncher(g.Name(), g.Prefix, g.Remote, g.ProjectRoot, "", g.CacheVars)
	if err != nil {
		return nil, err
	}

	defer func() { _ = lr.Close() }()

	pkgs := packagesFromFilesFn(files)

	// Gremlins has no end-of-options token, so a derived value that begins with
	// a dash cannot be fenced — it is refused before anything is spawned. The
	// policy lives in argfence's table rather than here, so this call site and
	// the audit gate read the same map.
	//
	// Today packagesFromFiles always emits "." or "./<dir>", which is
	// prefix-constant and could not produce a dash. That is exactly why the
	// guard is here: the safety is a property of one derivation function, one
	// refactor away from not holding, and nothing downstream would notice.
	if _, err := argfence.Fence("gremlins", "package", pkgs...); err != nil {
		return nil, err
	}

	reportDir := filepath.Join(g.ProjectRoot, "reports", "gremlins")
	// Gremlins won't create parent dirs for --output; it errors on the
	// first mutant write and leaves no report behind. Mkdir up front so
	// every invocation has somewhere to land.
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return nil, fmt.Errorf("prepare gremlins report dir: %w", err)
	}

	// Cleared up front so a Run that returns an error leaves no trace of the
	// PREVIOUS run's exclusions behind. A caller reading Unmeasured after a
	// failed Run would otherwise be reading a different run's answer.
	g.Unmeasured = nil

	merged := &Report{Tool: g.Name()}
	var unmeasured []Unmeasured
	skip := func(pkg string, kind UnmeasuredKind, why string) {
		unmeasured = append(unmeasured, Unmeasured{Package: pkg, Kind: kind, Message: pkg + " (" + why + ")"})
	}

	for _, pkg := range pkgs {
		reportAbs := GremlinsReportPath(g.ProjectRoot, pkg)
		reportRel := filepath.Join("reports", "gremlins", filepath.Base(reportAbs))
		// A stale report from a prior run must not be re-read if gremlins
		// writes nothing this time.
		_ = os.Remove(reportAbs)
		// The remote half of the same guarantee, and it is not optional: rsync
		// --delete cannot clear the remote report because the locked
		// `:- .gitignore` filter protects ignored paths, and reports/ is
		// gitignored. No-op for a local run.
		if err := lr.clearReport(pkg, reportAbs); err != nil {
			return nil, err
		}

		args, err := g.buildUnleashArgs(reportRel, []string{pkg})
		if err != nil {
			return nil, err
		}
		cmd, err := lr.toolCmd(args, func(a []string) *exec.Cmd { return gremlinsBuildCmd(g, a) })
		if err != nil {
			return nil, err
		}
		cmd.Stdout = os.Stderr // streamed; not captured (long-running)
		cmd.Stderr = os.Stderr

		// Echo the exact invocation before running — cheap diagnostic for
		// copy-paste re-runs.
		invocation := strings.Join(cmd.Args, " ")
		fmt.Fprintf(os.Stderr, "gremlins: %s\n", invocation)

		// Remembered so the report-absent branch below can tell "gremlins found
		// nothing to cover" (exit 0) from "gremlins failed before writing"
		// (non-zero) — indistinguishable once the report is merely missing.
		toolExit := 0
		if err := cmd.Run(); err != nil {
			// Gremlins exits non-zero when threshold flags fail or surviving
			// mutants exist — both are "ran, bad results"; read the report
			// regardless. Only a failure to START the process (binary not
			// found, etc.) is fatal.
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				if lr.remoteRun() {
					// The LOCAL ssh could not be started, so nothing reached the
					// remote. Naming gremlins here would send the user to check
					// an installation that was never consulted.
					return nil, fmt.Errorf("remote ssh for gremlins on %s could not be started: %w: %v\n  invocation: %s",
						g.Remote.Host, remote.ErrTransport, err, invocation)
				}
				return nil, fmt.Errorf("gremlins invocation failed: %w\n  invocation: %s\n  (is gremlins installed? `go install github.com/go-gremlins/gremlins/cmd/gremlins@latest`)", err, invocation)
			}
			// Over ssh the SAME exit channel carries the remote program's code
			// and ssh's own transport failures, so the tolerance above has to be
			// narrowed by code or an unreachable host becomes a clean-looking
			// skip. No-op locally.
			if fatal := lr.remoteExitFatal("gremlins", exitErr.ExitCode()); fatal != nil {
				return nil, fmt.Errorf("%w\n  invocation: %s", fatal, invocation)
			}
			toolExit = exitErr.ExitCode()
		}

		// Fetched per package, immediately after its own run (the locked
		// report_fetch decision), so "no report for this package" keeps meaning
		// what it means locally.
		//
		// Only rsync's "source file is not there" survives as a skip — that one
		// genuinely means the tool wrote nothing. A dead connection tells us
		// nothing about whether a report exists, and calling it "no report"
		// would mark a package unmeasured that may have measured fine.
		if ferr := lr.fetchReport(pkg, reportAbs); ferr != nil {
			if fetchFatal(ferr) {
				return nil, fmt.Errorf("fetch gremlins report for %s: %w", pkg, ferr)
			}
			fmt.Fprintf(os.Stderr, "gremlins: fetch %s: %v\n", pkg, ferr)
		}

		b, err := os.ReadFile(reportAbs)
		if err != nil {
			// Absent report plus a non-zero exit is not a skip — the tool fell
			// over before measuring, and calling that "no covered mutants"
			// reports the package as fine to ignore.
			if fatal := lr.reportlessExitFatal("gremlins", toolExit); fatal != nil {
				return nil, fmt.Errorf("%w\n  invocation: %s", fatal, invocation)
			}
			// No report — gremlins gathered no covered mutants for this
			// package and exited without writing. Exclude, don't fail: only
			// a caller that needs evidence (the drain) turns absence fatal.
			skip(pkg, UnmeasuredMissing, "no report — gremlins gathered no covered mutants")
			continue
		}
		rep, err := ParseGremlinsJSON(b)
		if err != nil {
			skip(pkg, UnmeasuredUnreadable, "unreadable report: "+err.Error())
			continue
		}
		// The package identity exists only here, in the loop — the payload
		// itself names files by bare basename. Re-prefix before merging so
		// every path downstream is repo-relative.
		RePrefixGremlinsFiles(rep, pkg)
		// Drop what cannot be a real result BEFORE the merge, so an
		// inapplicable mutant never reaches the denominator, the survivor
		// list, the lifecycle or the accept store. Filtering it later would
		// leave the score computed over mutants the survivor list no longer
		// showed — the same disagreement in a new place.
		DropInapplicable(rep, g.ProjectRoot)
		if !hasCoverage(rep) {
			// Report exists but every mutant is NOT COVERED — gremlins
			// instrumented zero usable coverage here (a coverage-tool blind
			// spot, not a test-quality signal). Exclude from the SCORE only:
			// the package was measured, and its rows are real survivors that
			// still have to be killed or accepted.
			skip(pkg, UnmeasuredUncovered, "zero covered mutants — coverage blind spot")
			continue
		}
		mergeInto(merged, rep)
	}

	g.Unmeasured = unmeasured
	for _, u := range unmeasured {
		fmt.Fprintf(os.Stderr, "gremlins: skipped %s\n", u)
	}

	return merged, nil
}

// hasCoverage reports whether gremlins instrumented at least one mutant
// it actually ran a test against (killed, timed out, or LIVED). A report
// with only NOT COVERED mutants (or none at all) means gremlins gathered
// no usable coverage for the package — excluded from the merged score so
// a coverage blind spot doesn't masquerade as theatrical tests.
func hasCoverage(r *Report) bool {
	lived := r.Survived - r.NotCovered
	return r.Killed+r.Timeout+lived > 0
}

// mergeInto accumulates src into dst (counts + surviving mutants) and
// recomputes dst's score from the running totals — same convention as
// ParseGremlinsJSON: killed / (killed + survived + timeout).
func mergeInto(dst, src *Report) {
	dst.Killed += src.Killed
	dst.Survived += src.Survived
	dst.Timeout += src.Timeout
	dst.Errors += src.Errors
	dst.NotCovered += src.NotCovered
	dst.Surviving = append(dst.Surviving, src.Surviving...)
	// Per-file rows accumulate by path, so two packages that each report a
	// bare `x.go` stay distinct (they were re-prefixed before merging) while
	// two reports naming the same repo-relative file sum into one row.
	for name, s := range src.Files {
		dst.addFile(name, s)
	}
	dst.Score = PooledScore(dst.Killed, dst.Survived, dst.Timeout)
}

// RePrefixGremlinsFiles rewrites a single package's gremlins report so its
// paths are repo-relative, both in Surviving[].File and in the per-file rows.
//
// Gremlins names a file by its basename ("changes.go") and carries the package
// identity only in the invocation — ParseGremlinsJSON takes bytes and no
// package argument, so it cannot do this. Left un-prefixed, every Go mutant
// fails to match a repo-relative change set and the whole leg filters out as
// out-of-scope: a vacuous 0/0 that reads like a clean run.
//
// pkg is the package path Run invoked gremlins with ("./internal/changes", or
// "." for the module root). Paths already carrying the prefix are left alone,
// so applying this twice is a no-op.
func RePrefixGremlinsFiles(r *Report, pkg string) {
	dir := strings.TrimPrefix(filepath.ToSlash(pkg), "./")
	if dir == "" || dir == "." {
		return
	}
	prefix := dir + "/"
	rename := func(p string) string {
		p = filepath.ToSlash(p)
		if strings.HasPrefix(p, prefix) {
			return p
		}
		return prefix + p
	}
	for i := range r.Surviving {
		r.Surviving[i].File = rename(r.Surviving[i].File)
	}
	if len(r.Files) == 0 {
		return
	}
	files := make(map[string]FileStat, len(r.Files))
	for name, s := range r.Files {
		k := rename(name)
		files[k] = files[k].plus(s)
	}
	r.Files = files
}

// GremlinsReportPath is where Run writes pkg's RAW per-package report, given
// the same ProjectRoot the adapter ran with.
//
// Exported because a caller that must classify every survivor — rather than
// score them — has to read those raw reports: Run's merged report deliberately
// drops zero-coverage packages, so a caller reading the merge would see a
// coverage blind spot as a package with nothing to answer for. Deriving the
// path here rather than re-implementing the stem rule keeps the two from
// drifting into disagreeing about which file to read.
func GremlinsReportPath(projectRoot, pkg string) string {
	return filepath.Join(projectRoot, "reports", "gremlins", sanitizePkg(pkg)+".json")
}

// sanitizePkg turns a gremlins package path into a filesystem-safe report
// filename stem, so per-package reports don't clobber each other.
// e.g. "./internal/cmd" → "internal_cmd"; "." → "root".
func sanitizePkg(pkg string) string {
	s := strings.TrimPrefix(pkg, "./")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, ".", "_")
	if s == "" || s == "_" {
		return "root"
	}
	return s
}

// buildUnleashArgs assembles the gremlins invocation for the configured
// timeout coefficient and package set. Extracted from Run() so the flag
// shape is unit-testable without shelling out.
// It returns an error rather than a best-effort argv when a value would be read
// as an option: gremlins has no `--`, so there is nothing to fall back to.
func (g *Gremlins) buildUnleashArgs(reportRel string, pkgs []string) ([]string, error) {
	// --output's value is an option ARGUMENT, not a positional, so a leading
	// dash there is not the same vector as a dash package. It is checked anyway
	// because the cost is one line and the alternative is a reader having to
	// re-derive which of the two slots is safe today.
	if _, err := argfence.Fence("gremlins", "output report", reportRel); err != nil {
		return nil, err
	}
	fenced, err := argfence.Fence("gremlins", "package", pkgs...)
	if err != nil {
		return nil, err
	}
	coef := g.TimeoutCoefficient
	if coef <= 0 {
		coef = DefaultTimeoutCoefficient
	}
	workers := g.Workers
	if workers <= 0 {
		// Derived from the machine the run happens ON. For a remote run that is
		// the probed host, not this laptop — see launcherWorkers.
		workers = launcherWorkers(g.Remote)
	}
	testCPU := g.TestCPU
	if testCPU <= 0 {
		testCPU = DefaultTestCPU
	}
	args := []string{
		"gremlins", "unleash",
		"--output", reportRel,
		"--timeout-coefficient", strconv.Itoa(coef),
		"--workers", strconv.Itoa(workers),
		"--test-cpu", strconv.Itoa(testCPU),
	}
	return append(args, fenced...), nil
}

// PackageStep is one package's share of a run: the report it writes, and the
// argv that produces it.
//
// Reports are named per package because gremlins is invoked per package —
// ReportRel is where THIS step's report lands, relative to the project root, so
// a caller that never spawns the argv still knows what to collect afterwards.
// ExitRel is where THIS step's exit code lands, beside its report. The
// detached sequence records one code per package because the run as a whole
// records only one — the last package's, since the steps are joined with `;`
// so a failure does not truncate the rest. Without a per-package code, a
// package that died before writing anything is indistinguishable at collect
// time from one that simply had no covered mutants, which is the false-green
// ReportlessExit exists to refuse.
type PackageStep struct {
	Package   string
	ReportRel string
	ExitRel   string
	Argv      []string
}

// GremlinsExitPath is where a detached step records pkg's exit code, given the
// project root. It sits beside the report and shares its per-package stem, so
// the two are fetched, cleared and read as a pair.
func GremlinsExitPath(projectRoot, pkg string) string {
	return filepath.Join(projectRoot, "reports", "gremlins", sanitizePkg(pkg)+".exit")
}

// DetachSteps returns the per-package work a run would do, without spawning any
// of it or touching the filesystem.
//
// It exists because a gremlins run is not one command. Run loops over packages,
// clearing each stale report and invoking the tool once per package, and a
// caller that wants that sequence to execute somewhere else — a host, detached,
// outliving this process — needs the sequence as DATA rather than as the side
// effects of a loop it cannot enter.
//
// Deriving it here rather than in the caller keeps ONE definition of what a run
// is. A second derivation would drift the moment a flag is added to
// buildUnleashArgs, and the detached run would then measure something slightly
// different from the attached one while reporting through the same artefacts —
// the exact indistinguishability c-2 promises, quietly broken.
//
// The same argfence guard Run applies is applied here, before any argv is
// handed out: a refused package must not become a line in a script that runs
// unattended for two hours.
func (g *Gremlins) DetachSteps(files []string) ([]PackageStep, error) {
	if len(files) == 0 {
		return nil, nil
	}
	pkgs := packagesFromFilesFn(files)
	if _, err := argfence.Fence("gremlins", "package", pkgs...); err != nil {
		return nil, err
	}
	steps := make([]PackageStep, 0, len(pkgs))
	for _, pkg := range pkgs {
		reportAbs := GremlinsReportPath(g.ProjectRoot, pkg)
		reportRel := filepath.Join("reports", "gremlins", filepath.Base(reportAbs))
		args, err := g.buildUnleashArgs(reportRel, []string{pkg})
		if err != nil {
			return nil, err
		}
		exitRel := filepath.Join("reports", "gremlins", filepath.Base(GremlinsExitPath(g.ProjectRoot, pkg)))
		steps = append(steps, PackageStep{Package: pkg, ReportRel: reportRel, ExitRel: exitRel, Argv: args})
	}
	return steps, nil
}

// Collect merges per-package reports that are ALREADY on disk into one Report,
// running nothing.
//
// It is the second half of Run — everything after each package's report has
// landed — reachable on its own so a detached run collected hours later goes
// through the same parse, the same re-prefixing, the same inapplicable filter,
// the same coverage rule and the same merge as an attached one.
//
// It shares every one of those helpers with Run rather than restating them, and
// TestCollectMatchesRunForTheSameReports pins that the two agree for identical
// inputs. That equivalence is the guarantee: c-2 promises a detached verdict is
// indistinguishable from an attached one, and the only way that stays true as
// this code changes is for a test to fail when the two paths diverge.
//
// A missing report is a skip, exactly as it is in Run — gremlins gathered no
// covered mutants for that package and wrote nothing — UNLESS that package
// recorded a non-zero exit, in which case it failed before measuring anything
// and ReportlessExit refuses it. Run applies the same rule at the same point
// (reportlessExitFatal), through the same constructor.
//
// host names the machine that MEASURED these reports, and it is a parameter
// rather than something read off g.Remote deliberately: a detached collect must
// name the host recorded at dispatch, not whatever today's grant resolves to.
// Reaching for an ambient host here would quietly undo c-5.
func (g *Gremlins) Collect(steps []PackageStep, host string) (*Report, error) {
	merged := &Report{Tool: g.Name()}
	var unmeasured []Unmeasured
	skip := func(pkg string, kind UnmeasuredKind, why string) {
		unmeasured = append(unmeasured, Unmeasured{Package: pkg, Kind: kind, Message: pkg + " (" + why + ")"})
	}

	for _, s := range steps {
		reportAbs := GremlinsReportPath(g.ProjectRoot, s.Package)
		b, err := os.ReadFile(reportAbs)
		if err != nil {
			// No report. Whether that is benign turns on the code the package
			// exited with, which is why the sequence records one PER PACKAGE:
			// the run's own exit code is the last package's, so a package that
			// failed anywhere but last is invisible in it by construction.
			//
			// An absent or unreadable exit file is NOT read as success — it
			// means the step never got to record one (killed mid-package, host
			// rebooted), and inventing a 0 there would restore the very
			// false-green this guard removes. It stays a skip, as it was
			// before there were exit files at all, and the run-level guard in
			// collectDetached remains the backstop.
			if code, ok := g.stepExit(s); ok {
				if fatal := ReportlessExit("gremlins", host, code); fatal != nil {
					return nil, fmt.Errorf("package %s: %w", s.Package, fatal)
				}
			}
			skip(s.Package, UnmeasuredMissing, "no report — gremlins gathered no covered mutants")
			continue
		}
		rep, err := ParseGremlinsJSON(b)
		if err != nil {
			skip(s.Package, UnmeasuredUnreadable, "unreadable report: "+err.Error())
			continue
		}
		RePrefixGremlinsFiles(rep, s.Package)
		DropInapplicable(rep, g.ProjectRoot)
		if !hasCoverage(rep) {
			skip(s.Package, UnmeasuredUncovered, "zero covered mutants — coverage blind spot")
			continue
		}
		mergeInto(merged, rep)
	}

	g.Unmeasured = unmeasured
	return merged, nil
}

// stepExit reads the exit code a detached step recorded for its package.
//
// ok is false whenever no code can be trusted — the step predates exit files
// and carries no ExitRel, the file is absent because the step never reached the
// line that writes it, or its contents are not an integer. None of those is
// evidence of success, and none is evidence of failure either: the caller keeps
// its pre-existing skip in that case rather than inventing a verdict from a
// file that is not there.
func (g *Gremlins) stepExit(s PackageStep) (int, bool) {
	if s.ExitRel == "" {
		return 0, false
	}
	b, err := os.ReadFile(filepath.Join(g.ProjectRoot, s.ExitRel))
	if err != nil {
		return 0, false
	}
	code, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0, false
	}
	return code, true
}

// gremlinsBuildCmd is the process-construction seam. Rejection tests substitute
// one that fails the test if it is called, so "refused BEFORE exec" is asserted
// rather than assumed — a refusal that still spawned gremlins would otherwise
// look identical to one that didn't.
var gremlinsBuildCmd = (*Gremlins).buildCmd

// packagesFromFilesFn is the package-derivation seam, overridable so a test can
// prove Run's guard fires. Today's derivation cannot produce a dash package;
// substituting one that does is the only way to exercise the guard without
// waiting for the refactor that makes it reachable for real.
var packagesFromFilesFn = packagesFromFiles

func (g *Gremlins) buildCmd(args []string) *exec.Cmd {
	if g.Prefix == "" {
		return exec.Command(args[0], args[1:]...)
	}
	prefix := strings.Fields(g.Prefix)
	full := append(prefix, args...)
	return exec.Command(full[0], full[1:]...)
}

// packagesFromFiles derives one gremlins package path per unique
// directory among the touched files. Gremlins is invoked once per
// package (see Run) rather than once over a collapsed shared-ancestor
// path, because a broad recursive scope makes gremlins gather empty
// coverage and report nothing. A file at the module root maps to ".".
// The result is deduped and sorted for deterministic invocation order;
// nil for no files.
//
// ONLY directories carrying at least one .go file in this list yield a
// package. Every directory used to, and both failure modes were live on the
// phase that added this: gremlins appends `/...` to every package argument, so
// a changed README.md derived "." and mutated the WHOLE module — a 196-file,
// 5444-mutant run whose survivors then had to be filtered back down to the 15
// files the phase touched — while a changed assets/prompts/verify.md derived
// "./assets/prompts", where gremlins exits 1 and writes no report at all.
//
// The filter reads the input list rather than the filesystem, which keeps this
// pure and the seam's signature unchanged. It loses nothing: a survivor is
// scored only when its file is in the phase's change set, so a directory whose
// changed files are all non-Go can produce nothing but out-of-scope mutants.
// Mutating it is waste by construction, not a coverage trade.
//
// Test sources count. A task that only adds _test.go files still owes its
// package a run — those tests are what kill its mutants.
func packagesFromFiles(files []string) []string {
	if len(files) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var pkgs []string
	for _, f := range files {
		if filepath.Ext(f) != ".go" {
			continue
		}
		dir := filepath.ToSlash(filepath.Dir(f))
		pkg := "."
		if dir != "" && dir != "." {
			pkg = "./" + dir
		}
		if !seen[pkg] {
			seen[pkg] = true
			pkgs = append(pkgs, pkg)
		}
	}
	sort.Strings(pkgs)
	return pkgs
}

// gremlinsReport mirrors the JSON schema from
// github.com/go-gremlins/gremlins/internal/report/internal/structure.go
type gremlinsReport struct {
	GoModule          string         `json:"go_module"`
	Files             []gremlinsFile `json:"files"`
	TestEfficacy      float64        `json:"test_efficacy"`
	MutationsCoverage float64        `json:"mutations_coverage"`
	MutantsTotal      int            `json:"mutants_total"`
	MutantsKilled     int            `json:"mutants_killed"`
	MutantsLived      int            `json:"mutants_lived"`
	MutantsNotViable  int            `json:"mutants_not_viable"`
	MutantsNotCovered int            `json:"mutants_not_covered"`
	ElapsedTime       float64        `json:"elapsed_time"`
}

type gremlinsFile struct {
	Filename  string             `json:"file_name"`
	Mutations []gremlinsMutation `json:"mutations"`
}

type gremlinsMutation struct {
	Type   string `json:"type"`
	Status string `json:"status"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// ParseGremlinsJSON converts a gremlins output.json payload into the
// normalised Report shape.
//
// Status mapping:
//
//	KILLED       → killed
//	LIVED        → survived (with snippet)
//	NOT COVERED  → survived (mutant the tests never even ran)
//	TIMED OUT    → timeout
//	NOT VIABLE   → not counted (compile error, not a test-quality signal)
//	SKIPPED      → not counted
//	RUNNABLE     → not counted (state, not a result)
//
// Score uses the same convention as Stryker: killed/(killed+survived+timeout).
// Note this differs from gremlins' own `test_efficacy`, which is killed/
// (killed+lived) and doesn't penalise NOT COVERED mutants. We treat
// NOT COVERED as survival because the whole point of dross verify is
// catching tests that don't actually exercise the code they claim to.
func ParseGremlinsJSON(data []byte) (*Report, error) {
	var raw gremlinsReport
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode gremlins report: %w", err)
	}
	r := &Report{Tool: "gremlins"}
	for _, f := range raw.Files {
		for _, m := range f.Mutations {
			switch m.Status {
			case "KILLED":
				r.Killed++
				r.addFile(f.Filename, FileStat{Killed: 1})
			case "LIVED", "NOT COVERED":
				r.Survived++
				if m.Status == "NOT COVERED" {
					r.NotCovered++
					r.addFile(f.Filename, FileStat{Survived: 1, NotCovered: 1})
				} else {
					r.addFile(f.Filename, FileStat{Survived: 1})
				}
				r.Surviving = append(r.Surviving, Mutant{
					File: f.Filename,
					Line: m.Line,
					Op:   m.Type,
					// gremlins doesn't surface the source replacement, only the
					// mutator type (e.g. CONDITIONALS_NEGATION). That's still
					// enough for verify to reason about.
				})
			case "TIMED OUT":
				r.Timeout++
				r.addFile(f.Filename, FileStat{Timeout: 1})
			case "NOT VIABLE", "SKIPPED", "RUNNABLE", "":
				// not counted
			default:
				// future statuses — surface as errors so they're visible
				r.Errors++
				r.addFile(f.Filename, FileStat{Errors: 1})
			}
		}
	}
	r.Score = PooledScore(r.Killed, r.Survived, r.Timeout)
	return r, nil
}

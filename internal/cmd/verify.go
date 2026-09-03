package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/changes"
	"github.com/Rivil/dross/internal/mutation"
	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/remote"
	"github.com/Rivil/dross/internal/stack"
	"github.com/Rivil/dross/internal/survivor"
	"github.com/Rivil/dross/internal/telemetry"
	"github.com/Rivil/dross/internal/testlane"
	"github.com/Rivil/dross/internal/verify"
)

// Verify registers `dross verify <phase>`.
//
// What this command does (mechanical only — LLM does criterion mapping):
//  1. Read project.toml + phases/<id>/spec.toml + phases/<id>/changes.json
//  2. Group touched files by language
//  3. For each language with an adapter: run mutation testing
//  4. Aggregate into tests.json
//  5. Write a verify.toml skeleton (verdict = pending) for /dross-verify
//     to fill in criterion-to-test mappings + final verdict.
func Verify() *cobra.Command {
	var skipMutation bool
	var detach bool
	var detachAt string
	c := &cobra.Command{
		Use:   "verify <phase-id>",
		Short: "Run mutation testing per language and write tests.json + verify.toml skeleton",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			// First, before any I/O. verify is the command that actually spawns
			// the suite (configuredAdapters -> gremlins -> the repo's go test), so
			// a refusal that had already written tests.json would have done the
			// work it was declining to authorize.
			if err := requireExecConsent(); err != nil {
				return err
			}
			phaseID := args[0]
			root, err := FindRoot()
			if err != nil {
				return err
			}
			proj, err := project.Load(filepath.Join(root, project.File))
			if err != nil {
				return err
			}
			specPath := filepath.Join(phase.Dir(root, phaseID), "spec.toml")
			spec, err := phase.LoadSpec(specPath)
			if err != nil {
				return fmt.Errorf("read spec: %w (run /dross-spec first)", err)
			}

			changesPath := changes.FilePath(root, phaseID)
			ch, err := changes.Load(changesPath, phaseID)
			if err != nil {
				return err
			}
			filesByTask := map[string][]string{}
			for taskID, rec := range ch.Tasks {
				filesByTask[taskID] = rec.Files
			}
			recorded := verify.FilesFromChanges(filesByTask)

			// Scoping is unconditional — there is no flag for it. It is a
			// correctness fix, not a mode: without it a survivor in an
			// untouched file of the same package gates this phase, and a
			// neighbour's kills inflate its score.
			scope := phaseScope(filepath.Dir(root), ch.Base, recorded)

			// The UNION is what gets mutated, not just the recorded files. A
			// file git saw change but no task recorded would otherwise never
			// be mutated at all, so its survivors could not gate anything —
			// the same escape hatch this phase closes on the attribution side.
			// Widening happens here, at dispatch; the post-Report filter never
			// narrows it back.
			files, gone := mutationCandidates(filepath.Dir(root), scope.Files)
			if len(files) == 0 && len(gone) == 0 {
				Print("verify: no changes recorded for this phase and nothing changed since the base.")
				Print("Run /dross-execute first, or record changes manually with `dross changes record`.")
				return nil
			}

			// A refusal or an unreachable remote aborts HERE, before
			// RunScoped — so neither tests.json nor verify.toml is written,
			// and the run never falls back to a local-only adapter list.
			adapters, tuning, err := configuredAdaptersFn(proj, root, skipMutation)
			if err != nil {
				return err
			}

			if detach {
				// Refused rather than run locally. The whole point of the flag
				// is that the run outlives this process, and a local run cannot
				// — falling back would hold the session for the full leg while
				// having been asked not to, which is the one outcome the flag
				// exists to prevent.
				if err := detachRequiresAHost(tuning); err != nil {
					return err
				}
				notBefore, err := parseDetachAt(detachAt, time.Now())
				if err != nil {
					return err
				}
				g, err := gremlinsAdapter(adapters)
				if err != nil {
					return err
				}
				steps, err := g.DetachSteps(files)
				if err != nil {
					return err
				}
				if len(steps) == 0 {
					return fmt.Errorf("nothing to mutate for phase %s — no Go packages in scope", phaseID)
				}
				return dispatchDetached(root, phaseID, steps, tuning.Target, notBefore)
			}

			if tuning.FellBackFrom != "" {
				// Announced before the run, not buried in the artefact. The
				// operator watching this decides whether to wait for the host.
				Printf("remote: %s\n", tuning.FallbackWhy)
			}
			t, err := verify.RunScoped(phaseID, files, adapters, scope)
			if err != nil {
				return err
			}
			// Stamped from the adapters the run actually used, not from the
			// grant on disk: a --local run has a grant and ignores it, and a
			// fallback has one it could not reach. Reading config here would
			// label both as remote measurements.
			return finishVerify(root, phaseID, spec, t, measuredOnOf(adapters, tuning), gone)
		},
	}
	c.Flags().BoolVar(&skipMutation, "skip-mutation", false,
		"do not run mutation tests (record what would have been mutated, skip execution)")
	c.Flags().BoolVar(&detach, "detach", false,
		"start the run on the granted host and return immediately; collect it later with `dross verify results <phase>`")
	c.Flags().StringVar(&detachAt, "at", "",
		"with --detach, start the run at HH:MM (next occurrence) or an RFC3339 instant, on the host's clock")
	c.AddCommand(verifyFinalize())
	c.AddCommand(verifyResults())
	c.AddCommand(verifyStatus())
	return c
}

// detachSpawn is the seam every detached dispatch goes through, swapped in
// tests so "returned without waiting" is asserted rather than assumed.
var detachSpawn = remote.ExecScript

// detachSync is the tree push, held separately from the spawn so a test can
// assert the ORDER: a run started against an unsynced tree measures whatever
// the host had left over from last time and reports it as this phase's.
var detachSync = func(t remote.Target, localRoot string) error {
	argv, cleanup, err := remote.SyncArgs(t, localRoot)
	if err != nil {
		return err
	}
	defer cleanup()
	return runDetachArgv(argv)
}

var runDetachArgv = func(argv []string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	return cmd.Run()
}

// detachRequiresAHost refuses a detached run that has nowhere to detach to.
//
// A local fallback is exactly wrong here. Everywhere else in verify, falling
// back to this machine is the right call — the numbers still get measured. With
// --detach the user has said the one thing they will not accept is holding this
// session for the length of the leg, and a local run does precisely that while
// reporting success. Refusing is the only honest answer.
func detachRequiresAHost(tuning mutationTuning) error {
	if tuning.Target != nil {
		return nil
	}
	why := "no remote host is granted"
	if tuning.FellBackFrom != "" {
		why = fmt.Sprintf("%s could not be reached: %s", tuning.FellBackFrom, tuning.FallbackWhy)
	}
	return fmt.Errorf("--detach needs a reachable granted host, but %s.\n"+
		"Grant one with `dross remote grant`, or run without --detach", why)
}

// newRunID mints the identity a detached run is found by afterwards.
//
// Time-based and second-resolution: it is read by a human in `verify status`
// hours later, so it has to be legible, and one dispatch per phase per second
// is not a collision anyone can produce — the one-run-per-phase guard refuses
// the second one regardless.
func newRunID(now time.Time) string {
	return "r-" + now.UTC().Format("20060102-150405")
}

// detachSequence renders the per-package steps as one shell line.
//
// Joined with `;` rather than `&&` deliberately, mirroring the attached loop:
// a package whose mutation run fails does not stop the packages after it, and
// its absent report is read as "nothing was learned about this package" — the
// same reading the local path gives. `&&` would silently truncate the run at
// the first failure and report the remainder as unmeasured.
//
// The stale report is removed immediately before its package runs, so a report
// left by a previous dispatch can never be collected as this one's.
// Each step records its OWN exit code, immediately after its argv and before
// anything else can overwrite `$?`. The run as a whole records only one code —
// the last package's, precisely because of the `;` above — so without a
// per-package code a package that failed anywhere but last is invisible at
// collect time, and its absent report reads as "nothing to measure here"
// rather than "this failed before it measured anything". That is the
// false-green ReportlessExit refuses, and it needs this line to have a code to
// refuse on.
func detachSequence(steps []mutation.PackageStep) string {
	var parts []string
	for _, s := range steps {
		var b strings.Builder
		b.WriteString("rm -f " + testlane.ShellQuote(s.ReportRel) + "; ")
		if s.ExitRel != "" {
			// Cleared with the report and for the same reason: a code left by
			// a previous dispatch would be read as this run's.
			b.WriteString("rm -f " + testlane.ShellQuote(s.ExitRel) + "; ")
		}
		for i, a := range s.Argv {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(testlane.ShellQuote(a))
		}
		if s.ExitRel != "" {
			b.WriteString("; printf '%s\\n' \"$?\" > " + testlane.ShellQuote(s.ExitRel))
		}
		parts = append(parts, b.String())
	}
	return "mkdir -p reports/gremlins; " + strings.Join(parts, "; ")
}

// dispatchDetached starts a phase's mutation run on the granted host and
// returns without waiting for it.
//
// The ORDER here is the contract. The record is written LAST, after the host
// has accepted the script: a record written first would name a run that never
// started, and the one-run-per-phase guard would then refuse the retry that
// would have fixed it. A dispatch that fails leaves nothing behind to clean up.
func dispatchDetached(root, phaseID string, steps []mutation.PackageStep, target *remote.Target, notBefore time.Time) error {
	repoDir := filepath.Dir(root)

	// Checked before the push, which is the expensive part: a phase that
	// already has a run in flight must not rsync a tree to a host first.
	existing, err := findDetachedRun(root, repoDir, phaseID)
	if err != nil {
		return err
	}
	if existing != nil {
		return fmt.Errorf(
			"phase %q already has a detached run in flight: %s on %s (dispatched %s).\n"+
				"Collect it with `dross verify results %s`, or cancel it before dispatching another",
			phaseID, existing.RunID, existing.Host,
			existing.DispatchedAt.Format(time.RFC3339), phaseID)
	}

	runID := newRunID(time.Now())
	runDir, err := remote.RunDir(runID)
	if err != nil {
		return err
	}

	if err := detachSync(*target, repoDir); err != nil {
		return fmt.Errorf("push the tree to %s: %w", target.Host, err)
	}

	script, err := remote.DetachScript(*target, runDir,
		[]string{"bash", "-c", detachSequence(steps)}, notBefore)
	if err != nil {
		return err
	}
	if _, err := detachSpawn(*target, script); err != nil {
		return fmt.Errorf("start the detached run on %s: %w", target.Host, err)
	}

	state := "running"
	if !notBefore.IsZero() {
		state = "scheduled"
	}
	rec := detachedRun{
		Phase:        phaseID,
		RunID:        runID,
		Host:         target.Host,
		Workdir:      target.Workdir,
		RunDir:       runDir,
		DispatchedAt: time.Now().UTC(),
		ScheduledFor: notBefore,
		State:        state,
	}
	if err := recordDetachedRun(root, repoDir, rec); err != nil {
		return err
	}

	Printf("detached: %s dispatched to %s as %s\n", phaseID, target.Host, runID)
	if !notBefore.IsZero() {
		Printf("  starts:  %s (host clock)\n", notBefore.Format(time.RFC3339))
	}
	return nil
}

// parseDetachAt reads --at.
//
// Accepts a full RFC3339 instant or a bare HH:MM, which resolves to the NEXT
// occurrence of that time — 02:00 typed at 23:00 means tomorrow morning, which
// is the only reading that is ever useful for an off-hours run. A bare time
// that resolved to today would schedule a run four hours in the past and start
// it immediately, which is the opposite of what was asked.
func parseDetachAt(s string, now time.Time) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	if ts, err := time.Parse(time.RFC3339, s); err == nil {
		return ts, nil
	}
	hm, err := time.Parse("15:04", s)
	if err != nil {
		return time.Time{}, fmt.Errorf(
			"--at %q is neither HH:MM nor an RFC3339 instant", s)
	}
	at := time.Date(now.Year(), now.Month(), now.Day(), hm.Hour(), hm.Minute(), 0, 0, now.Location())
	if !at.After(now) {
		at = at.AddDate(0, 0, 1)
	}
	return at, nil
}

// gremlinsAdapter picks the Go leg out of the configured adapters.
//
// Detach is gremlins-only for now, and says so by name rather than silently
// dispatching a partial run: a phase whose changes are half TypeScript would
// otherwise get a detached run measuring only its Go half, collected later as
// though it were the whole thing.
func gremlinsAdapter(adapters []mutation.Adapter) (*mutation.Gremlins, error) {
	for _, a := range adapters {
		if g, ok := a.(*mutation.Gremlins); ok {
			return g, nil
		}
	}
	return nil, fmt.Errorf(
		"--detach currently supports the gremlins (Go) leg only, and this phase " +
			"configured none.\nRun it attached, or narrow the phase to its Go files")
}

// Exit codes for `dross verify results`, one per state.
//
// Distinct codes rather than prose, so a caller can poll without parsing
// output — and so the two that mean "no verdict was produced" (still waiting,
// could not reach the host) can never be mistaken by a script for the one that
// means the artefacts are on disk.
const (
	exitResultsScheduled   = 10
	exitResultsRunning     = 11
	exitResultsFailed      = 12
	exitResultsUnreachable = 13
	exitResultsGone        = 14
)

// detachStatus reads a run's host-side state. Swapped in tests.
var detachStatus = func(t remote.Target, runDir string) (remote.RunStatus, error) {
	script, err := remote.StatusScript(t, runDir)
	if err != nil {
		return remote.RunStatus{}, err
	}
	out, err := remote.ExecScript(t, script)
	if err != nil {
		return remote.RunStatus{}, err
	}
	return remote.ParseStatus(out)
}

// detachFetchReports pulls the run's reports back, one file per package.
// Swapped in tests.
//
// PER FILE, not as a directory, and each local copy is REMOVED first. Both
// halves are load-bearing, and a live collection from helicon proved it:
//
//   - Fetching the directory nests it. FetchArgs passes the source through
//     Target.In, which cleans the path and so strips the trailing slash rsync
//     needs to mean "the contents of"; the reports landed in
//     reports/gremlins/gremlins/. SyncArgs carries a comment about that exact
//     slash — FetchArgs had only ever been used per-file, where it does not
//     arise. Fetching per file is also what the attached path does, so the two
//     now agree.
//
//   - Removing the local copy first turns a failed fetch into a MISSING report
//     rather than a stale one. Collect reads local paths, so without this a
//     fetch that silently did nothing is read as a complete run of whatever was
//     last left on this machine. That is precisely what happened: a report from
//     nine days earlier was parsed as this run's, producing a plausible 0.96
//     over a file this phase had since grown by 238 lines. A missing report is
//     recorded as unmeasured and is visible; a stale one is a wrong answer that
//     looks exactly like a right one.
var detachFetchReports = func(t remote.Target, localRoot string, steps []mutation.PackageStep) error {
	for _, s := range steps {
		local := filepath.Join(localRoot, s.ReportRel)
		if err := os.Remove(local); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("clear stale report %s: %w", s.ReportRel, err)
		}
		argv, err := remote.FetchArgs(t, s.ReportRel, local)
		if err != nil {
			return err
		}
		// A package that produced no report is not a fetch failure — the
		// attached path reads that as "gremlins gathered no covered mutants"
		// and so must this one. Only the ABSENCE is tolerated; the local file
		// is already gone, so nothing stale can survive it.
		if ferr := classifyFetch(t.Host, runDetachArgv(argv)); ferr != nil {
			if !errors.Is(ferr, remote.ErrPartial) {
				return fmt.Errorf("fetching %s's report from %s: %w", s.Package, t.Host, ferr)
			}
			Printf("collect: no report fetched for %s (%v)\n", s.Package, ferr)
		}

		// The exit code travels with the report, and its absence is tolerated
		// the same way: a step killed before it recorded one leaves nothing to
		// fetch. It is cleared first for the same reason the report is — a
		// code left from a previous collect would be read as this run's, and
		// this one decides whether an absent report is benign.
		if s.ExitRel == "" {
			continue
		}
		localExit := filepath.Join(localRoot, s.ExitRel)
		if err := os.Remove(localExit); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("clear stale exit code %s: %w", s.ExitRel, err)
		}
		exitArgv, err := remote.FetchArgs(t, s.ExitRel, localExit)
		if err != nil {
			return err
		}
		if ferr := classifyFetch(t.Host, runDetachArgv(exitArgv)); ferr != nil {
			if !errors.Is(ferr, remote.ErrPartial) {
				return fmt.Errorf("fetching %s's exit code from %s: %w", s.Package, t.Host, ferr)
			}
			// Absent: the step never recorded one. Collect reads that as a
			// skip, which is the pre-exit-file reading and stays correct.
		}
	}
	return nil
}

// classifyFetch turns runDetachArgv's raw error into a transport judgement, so
// a collect can tell "the file is not there" from "the connection died".
//
// That distinction is c-6: a transport failure at fetch time must read as "the
// run did not report", never as a package with nothing to measure. Without it
// a connection dropping mid-collect scores whichever packages arrived first and
// says nothing about the rest — the same false-green as a package that failed
// before measuring, arriving through a different door.
//
// An error that is already classified passes through, so a test can hand this
// an rsync verdict without constructing an *exec.ExitError. An error that is
// neither classified nor an exit status means rsync did not run at all, which
// is not an absent report either.
func classifyFetch(host string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, remote.ErrPartial) || errors.Is(err, remote.ErrTransport) ||
		errors.Is(err, remote.ErrRemoteCommand) {
		return err
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return remote.Classify("rsync", host, ee.ExitCode())
	}
	return err
}

// verifyResults registers `dross verify results <phase>`.
func verifyResults() *cobra.Command {
	return &cobra.Command{
		Use:   "results <phase-id>",
		Short: "Collect a detached mutation run and write tests.json + verify.toml",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return collectDetached(args[0])
		},
	}
}

// collectDetached is `verify results`: read the record, ask the host it names,
// and — only when the run actually finished — turn its reports into this
// phase's artefacts through the same pipeline an attached run uses.
//
// Nothing is written for any state but finished. A verify.toml produced from a
// half-finished run would carry a score computed over the packages that
// happened to be done, and it would look exactly like a complete one.
func collectDetached(phaseID string) error {
	root, err := FindRoot()
	if err != nil {
		return err
	}
	repoDir := filepath.Dir(root)

	rec, err := findDetachedRun(root, repoDir, phaseID)
	if err != nil {
		return err
	}
	if rec == nil {
		return fmt.Errorf("phase %q has no detached run recorded on this machine.\n"+
			"Start one with `dross verify %s --detach`", phaseID, phaseID)
	}

	// The host comes from the RECORD, never from today's grant. That is c-5:
	// a pool reordered, a grant edited, or a host that came back up between
	// dispatch and now must not redirect this fetch to a machine that measured
	// nothing — or, worse, one holding another run's report.
	target := remote.Target{Host: rec.Host, Workdir: rec.Workdir}

	st, err := detachStatus(target, rec.RunDir)
	if err != nil {
		// An unreachable host is not a verdict. The run may well be finishing
		// perfectly well on a machine this laptop currently cannot see.
		return &ExitCodeError{Code: exitResultsUnreachable, Err: fmt.Errorf(
			"could not reach %s to read run %s — the run's state is unknown, not failed: %w",
			rec.Host, rec.RunID, err)}
	}
	if !st.DirExists {
		return &ExitCodeError{Code: exitResultsGone, Err: fmt.Errorf(
			"run %s is gone from %s: its directory %s no longer exists.\n"+
				"Nothing was collected. Clear the record and dispatch again",
			rec.RunID, rec.Host, rec.RunDir)}
	}
	if !st.HasExit {
		if rec.Scheduled() && st.State == "scheduled" {
			return &ExitCodeError{Code: exitResultsScheduled, Err: fmt.Errorf(
				"run %s on %s has not started yet — scheduled for %s",
				rec.RunID, rec.Host, rec.ScheduledFor.Format(time.RFC3339))}
		}
		return &ExitCodeError{Code: exitResultsRunning, Err: fmt.Errorf(
			"run %s on %s is still running (dispatched %s)",
			rec.RunID, rec.Host, rec.DispatchedAt.Format(time.RFC3339))}
	}

	proj, err := project.Load(filepath.Join(root, project.File))
	if err != nil {
		return err
	}
	spec, err := phase.LoadSpec(filepath.Join(phase.Dir(root, phaseID), "spec.toml"))
	if err != nil {
		return fmt.Errorf("read spec: %w", err)
	}
	ch, err := changes.Load(changes.FilePath(root, phaseID), phaseID)
	if err != nil {
		return err
	}

	// The scope is REBUILT from the same inputs the attached path uses —
	// changes.json plus the base — rather than carried across in the record.
	// It is a pure function of what the phase did, so reconstructing it is
	// exact, and it keeps the detached path from owning a second scoping
	// implementation that could drift from the one the flag allowlist pins.
	filesByTask := map[string][]string{}
	for taskID, r := range ch.Tasks {
		filesByTask[taskID] = r.Files
	}
	scope := phaseScope(repoDir, ch.Base, verify.FilesFromChanges(filesByTask))
	files, gone := mutationCandidates(repoDir, scope.Files)

	adapters, _, err := configuredAdaptersFn(proj, root, false)
	if err != nil {
		return err
	}
	g, err := gremlinsAdapter(adapters)
	if err != nil {
		return err
	}
	steps, err := g.DetachSteps(files)
	if err != nil {
		return err
	}

	if err := detachFetchReports(target, repoDir, steps); err != nil {
		return &ExitCodeError{Code: exitResultsUnreachable, Err: fmt.Errorf(
			"could not fetch run %s's reports from %s: %w", rec.RunID, rec.Host, err)}
	}

	// rec.Host, not the grant: Collect names the machine that MEASURED these
	// reports when it refuses one, and c-5 turns on that being the recorded
	// host rather than whatever the pool resolves to now.
	report, err := g.Collect(steps, rec.Host)
	if err != nil {
		return err
	}
	if report.Killed+report.Survived+report.Timeout == 0 && st.ExitCode != 0 {
		// The tool exited non-zero and left nothing measurable. Calling that a
		// clean run of zero mutants is the false-green the whole seam exists
		// to prevent.
		return &ExitCodeError{Code: exitResultsFailed, Err: fmt.Errorf(
			"run %s on %s exited %d and produced no measurable report — nothing was collected",
			rec.RunID, rec.Host, st.ExitCode)}
	}

	t := &verify.Tests{
		Phase:       phaseID,
		GeneratedAt: time.Now().UTC(),
		Scope:       scope,
	}
	kept, dropped := verify.FilterReport(report, scope, "go")
	t.OutOfScope = append(t.OutOfScope, dropped...)
	t.Languages = append(t.Languages, verify.LanguageRun{
		Name: "go",
		Tool: "gremlins",
		// rec.Host, not the grant: this leg's numbers came off the machine the
		// dispatch record named, and re-deriving it here would stamp today's
		// pool onto a report measured hours ago somewhere else.
		MeasuredOn: verify.MeasuredOnHost(rec.Host),
		Files:      files,
		Mutation:   kept,
	})

	if err := finishVerify(root, phaseID, spec, t, verify.MeasuredOnHost(rec.Host), gone); err != nil {
		return err
	}
	if _, err := clearDetachedRun(root, repoDir, phaseID); err != nil {
		return err
	}
	Printf("collected run %s from %s\n", rec.RunID, rec.Host)
	return nil
}

// detachCancel tears a run down on its host. Swapped in tests.
//
// The kill targets the process GROUP (`kill -- -PID`), because setsid made the
// detached job a group leader: signalling the pid alone would leave gremlins
// and the `go test` processes it spawned running, holding the host's cores for
// an hour after the user was told the run was cancelled.
var detachCancel = func(t remote.Target, runDir, pidFile string) error {
	// Through remoteExecFn, not remote.Exec directly: the package's one remote
	// seam, so a test can assert the argv that would reach the host without
	// stubbing detachCancel itself and losing sight of the line it builds.
	_, err := remoteExecFn(t, []string{"bash", "-c", cancelLine(runDir, pidFile)})
	return err
}

// cancelLine is the teardown command text, split out from detachCancel so it is
// assertable without a host. detachCancel is a swapped seam in tests, so
// everything inside it is invisible to the suite — including the `-- -` that
// makes the kill target the process GROUP. Extracting the string is what lets a
// test fail when that regresses, rather than the host quietly keeping its cores
// busy for an hour after a reported cancellation.
func cancelLine(runDir, pidFile string) string {
	return "kill -- -$(cat " + testlane.ShellQuote(pidFile) + " 2>/dev/null) 2>/dev/null; " +
		"rm -rf " + testlane.ShellQuote(runDir)
}

// verifyStatus registers `dross verify status`.
func verifyStatus() *cobra.Command {
	var cancel string
	c := &cobra.Command{
		Use:   "status",
		Short: "List detached mutation runs this machine dispatched",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if cancel != "" {
				return cancelDetached(cancel)
			}
			return printDetachedStatus()
		},
	}
	c.Flags().StringVar(&cancel, "cancel", "",
		"cancel the named phase's detached run: kill it on the host and drop the record")
	return c
}

// printDetachedStatus lists what this machine has in flight.
//
// The recorded fields are printed WITHOUT asking the host, and the live state
// is added only when the host answers. A status that needed the network would
// be useless in the one situation it is most wanted — a laptop somewhere else,
// wondering whether last night's run is worth waiting for.
func printDetachedStatus() error {
	root, err := FindRoot()
	if err != nil {
		return err
	}
	runs, err := readDetachedRuns(root, filepath.Dir(root))
	if err != nil {
		return err
	}
	if len(runs) == 0 {
		Print("no detached runs dispatched from this machine.")
		return nil
	}
	for _, r := range runs {
		Printf("%s  %s on %s  dispatched %s\n", r.Phase, r.RunID, r.Host,
			r.DispatchedAt.Format(time.RFC3339))
		if r.Scheduled() {
			Printf("  scheduled for %s (host clock)\n", r.ScheduledFor.Format(time.RFC3339))
		}
		st, serr := detachStatus(remote.Target{Host: r.Host, Workdir: r.Workdir}, r.RunDir)
		switch {
		case serr != nil:
			// Recorded state, explicitly labelled as stale. Printing the
			// recorded value unqualified would present a dispatch-time guess
			// as an observation.
			Printf("  state    %s (recorded — %s did not answer)\n", r.State, r.Host)
		case !st.DirExists:
			Printf("  state    gone (the run directory is no longer on %s)\n", r.Host)
		case st.HasExit:
			Printf("  state    finished (exit %d) — collect with `dross verify results %s`\n", st.ExitCode, r.Phase)
		default:
			Printf("  state    %s\n", st.State)
		}
	}
	return nil
}

// cancelDetached kills a run on its host and drops the record.
//
// The record is cleared only after the host teardown is attempted, and the
// teardown reaches the host it was DISPATCHED to for the same reason the fetch
// does — cancelling on whichever host is preferred today would leave the real
// run alive while reporting it stopped.
func cancelDetached(phaseID string) error {
	root, err := FindRoot()
	if err != nil {
		return err
	}
	repoDir := filepath.Dir(root)
	rec, err := findDetachedRun(root, repoDir, phaseID)
	if err != nil {
		return err
	}
	if rec == nil {
		// An error rather than a silent success: a mistyped phase id would
		// otherwise report a cancellation that never happened, while the real
		// run keeps burning the host's night.
		return fmt.Errorf("phase %q has no detached run to cancel on this machine", phaseID)
	}

	target := remote.Target{Host: rec.Host, Workdir: rec.Workdir}
	cerr := detachCancel(target, rec.RunDir, path.Join(rec.RunDir, "pid"))

	removed, err := clearDetachedRun(root, repoDir, phaseID)
	if err != nil {
		return err
	}
	if !removed {
		return fmt.Errorf("phase %q had a run recorded but it could not be cleared", phaseID)
	}
	if cerr != nil {
		// The record is gone either way — keeping it would block a re-dispatch
		// over a host that may simply be down — but the user is told plainly
		// that something may still be running there.
		return fmt.Errorf("dropped the record for %s, but could not reach %s to stop run %s: %w\n"+
			"If that host comes back, %s may still be running there",
			phaseID, rec.Host, rec.RunID, cerr, rec.RunID)
	}
	Printf("cancelled %s (%s on %s)\n", phaseID, rec.RunID, rec.Host)
	return nil
}

// finishVerify turns a completed mutation run into this phase's artefacts:
// stamps where it was measured, records the files that no longer exist,
// classifies every survivor's lifecycle, writes tests.json and the verify.toml
// skeleton, and reports the run.
//
// It is a SEAM rather than a tidy-up. The attached run reaches it having just
// blocked on RunScoped; a detached run reaches it hours later, in a different
// session, holding a report fetched off a host. Everything after the mutants
// stop moving must be identical for the two, because a detached verdict that
// differed from an attached one in any of these steps would be a second-class
// artefact the /dross-verify judgement step could not read the same way — and
// the difference would be invisible, since both produce a plausible file.
//
// measuredOn is passed IN rather than derived here. The attached path knows it
// from the adapters that actually ran; the detached path knows it from the
// record written at dispatch, which is the host that measured the report — and
// re-deriving it at fetch time would read today's grant rather than the machine
// whose numbers these are.
func finishVerify(root, phaseID string, spec *phase.Spec, t *verify.Tests, measuredOn string, gone []string) error {
	t.MeasuredOn = measuredOn
	// Deleted paths stay in the record — they are part of what the phase did —
	// but as a skip with an honest reason rather than as an argument to a
	// mutation tool.
	for _, f := range gone {
		t.Skipped = append(t.Skipped, verify.SkippedFile{
			File:   f,
			Reason: "file no longer exists in the working tree",
		})
	}

	// Lifecycle classification runs BEFORE tests.json is written, so the
	// persisted record carries each survivor's key and state. The store is read
	// from the repo root, never from the phase dir: an acceptance recorded
	// during one phase has to keep suppressing in the next one.
	store, err := survivor.Load(survivor.Path(root))
	if err != nil {
		return err
	}
	accepted, err := acceptedReasons(store)
	if err != nil {
		return err
	}
	routed, err := routedSurvivors(root)
	if err != nil {
		return err
	}
	repoRoot := filepath.Dir(root)
	lc := verify.ApplyLifecycle(t, accepted, routed, workTreeIdentifier{repoRoot: repoRoot})

	testsPath, verifyPath := verify.FilePaths(root, phaseID)
	if err := t.Save(testsPath); err != nil {
		return err
	}

	ids := make([]string, 0, len(spec.Criteria))
	for _, c := range spec.Criteria {
		ids = append(ids, c.ID)
	}
	v := verify.Skeleton(t, ids)
	appendStalenessNotes(v, repoRoot, store)
	if err := v.Save(verifyPath); err != nil {
		return err
	}

	printVerifySummary(t, v)
	printLifecycleSummary(lc, v.Summary.UnclassifiedInScope)
	recordVerifyOutcome(t, v)
	return nil
}

// verifyFinalize records a telemetry outcome event with the resolved
// verdict from a verify.toml that the LLM (via /dross-verify) has
// filled in. The mechanical `dross verify <phase>` always emits a
// pending-verdict event; this is the second event closing the loop.
//
// Verdict must be pass | partial | fail. Pending or unknown verdicts
// are rejected so the slash command can't accidentally finalize a
// half-filled skeleton.
func verifyFinalize() *cobra.Command {
	return &cobra.Command{
		Use:   "finalize <phase-id>",
		Short: "Record the resolved verdict from verify.toml as a telemetry outcome event",
		Long: "Reads .dross/phases/<phase>/verify.toml after /dross-verify (the LLM step) " +
			"has written the final verdict, and emits a telemetry outcome event so " +
			"`dross stats` and downstream gates can see the pass/partial/fail resolution.\n\n" +
			"Verdict must be pass | partial | fail. Pending or unknown is rejected — " +
			"finalize the verify.toml first.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			phaseID := args[0]
			root, err := FindRoot()
			if err != nil {
				return err
			}
			recorded, verdict, err := finalizeVerify(root, phaseID)
			if err != nil {
				return err
			}
			if recorded {
				Printf("verify finalize: %s — verdict=%s recorded\n", phaseID, verdict)
			} else {
				Printf("verify finalize: %s — verdict=%s already recorded\n", phaseID, verdict)
			}
			return nil
		},
	}
}

// finalizeVerify records the resolved verdict from a phase's
// verify.toml as a telemetry outcome event, exactly once. The
// `finalized` marker in verify.toml is the idempotency guard: a second
// call (manual re-run, or a downstream gate healing after a manual
// finalize) is a no-op. Callers: `dross verify finalize`, plus the
// auto-heal in `dross ship` and `dross phase complete`.
//
// Returns recorded=true when this call emitted the event, false when
// it was already recorded. Errors on a missing verify.toml or an
// unresolved verdict — healing must never invent a verdict.
func finalizeVerify(root, phaseID string) (recorded bool, verdict string, err error) {
	testsPath, verifyPath := verify.FilePaths(root, phaseID)
	v, err := verify.LoadVerify(verifyPath)
	if err != nil {
		return false, "", fmt.Errorf("read verify.toml: %w", err)
	}
	if v == nil {
		return false, "", fmt.Errorf("verify.toml not found at %s — run `dross verify %s` first", verifyPath, phaseID)
	}
	switch v.Verify.Verdict {
	case "pass", "partial", "fail":
		// ok — accepted final verdicts
	case "pending", "":
		return false, v.Verify.Verdict, fmt.Errorf("verify.toml verdict is %q — fill in pass | partial | fail before finalizing", v.Verify.Verdict)
	default:
		return false, v.Verify.Verdict, fmt.Errorf("verify.toml verdict %q is not one of pass | partial | fail", v.Verify.Verdict)
	}
	if v.Verify.Finalized {
		return false, v.Verify.Verdict, nil
	}
	t, _ := verify.LoadTests(testsPath) // optional — may be absent under --skip-mutation manual cleanup
	recordVerifyOutcome(t, v)
	v.Verify.Finalized = true
	v.Verify.FinalizedAt = time.Now().UTC()
	if err := v.Save(verifyPath); err != nil {
		return true, v.Verify.Verdict, fmt.Errorf("write finalized marker to verify.toml: %w", err)
	}
	return true, v.Verify.Verdict, nil
}

// configuredAdaptersFn is the seam a refusal test substitutes to prove verify
// did not shell out. It is what makes "refused" different from "refused after
// spawning gremlins" — and gremlins runs the untrusted repo's Go tests, which is
// the code execution the consent gate exists to prevent.
var configuredAdaptersFn = configuredAdapters

// mutationTuning is the machine-local half of every adapter's construction:
// WHERE the run happens, and how parallel it is.
//
// It exists because there are two construction sites — configuredAdapters here
// and runGremlinsOverPackages in the drain — and a knob added to one of them
// only is a run that behaves differently depending on which command you reached
// it through. One table read once, applied at both.
type mutationTuning struct {
	// Prefix is the local runtime prefix, and is EMPTY whenever Target is set.
	Prefix string
	// Target is the granted remote, with Cores filled in by the probe. Nil runs
	// locally.
	Target *remote.Target
	// Workers and TestCPU are the machine-local overrides. Zero means unset,
	// which the adapters read as "apply your own default" — not as zero.
	Workers int
	TestCPU int
	// FellBackFrom names the host this run meant to use and could not reach;
	// FallbackWhy is the reason. Both empty on an ordinary run of either kind.
	//
	// They are carried rather than dropped because a fallback's numbers were
	// measured HERE while a remote measurement was expected — and a record that
	// says only "local" loses the fact that the expectation went unmet.
	FellBackFrom string
	FallbackWhy  string
}

// gremlins is the single Gremlins constructor. Both sites go through it, so a
// knob can only be added in one place.
func (mt mutationTuning) gremlins(projectRoot string, p *project.Project, cacheVars []string) *mutation.Gremlins {
	return &mutation.Gremlins{
		CacheVars:          cacheVars,
		Prefix:             mt.Prefix,
		ProjectRoot:        projectRoot,
		TimeoutCoefficient: p.Mutation.Gremlins.TimeoutCoefficient,
		Workers:            mt.Workers,
		TestCPU:            mt.TestCPU,
		Remote:             mt.Target,
	}
}

// resolveMutationTuning reads the grant and the tuning knobs, and probes the
// remote once for the core count the worker default derives from.
//
// The probe is unconditional rather than only-when-workers-is-unset, and that is
// the point: it doubles as the reachability pre-flight. A grant that cannot be
// reached must abort the command HERE, before a tree is pushed and before any
// adapter runs, rather than surfacing as an empty report the run cannot
// distinguish from "nothing to measure".
//
// A grant DROPS the docker prefix (the locked docker_prefix_under_remote
// decision) rather than refusing on it. dockerPrefix gates on runtime.mode,
// which describes the DEV stack and says nothing about where mutation runs — so
// aborting on the combination would refuse every docker-mode repo that grants a
// remote, which is the common case and the one this exists for. Shedding the
// prefix is also exactly right: the point is to run on the remote's OWN
// toolchain, and whether that toolchain is present is doctor's question.
func resolveMutationTuning(p *project.Project, root string) (mutationTuning, error) {
	targets, err := readRemoteGrants(root, filepath.Dir(root))
	if err != nil {
		return mutationTuning{}, err
	}
	workers, testCPU, err := readMutationTuning(root)
	if err != nil {
		return mutationTuning{}, err
	}
	mt := mutationTuning{Workers: workers, TestCPU: testCPU}
	if len(targets) == 0 {
		mt.Prefix = dockerPrefix(p)
		return mt, nil
	}
	// Walks the authorized hosts in order and takes the first that answers.
	// With one candidate this is exactly the previous behaviour.
	target, pool, perr := selectRemoteTarget(targets, nil)
	if perr != nil {
		return mutationTuning{}, fmt.Errorf(
			"remote mutation host %s is not usable: %w\n"+
				"Nothing was measured. Check ssh access, run `dross doctor`, or withdraw the grant with `dross mutation remote revoke`.",
			targets[0].Host, perr)
	}
	if target == nil {
		// A host we could not REACH gives no answer, and the local machine
		// still can. Aborting here is what forced `dross remote revoke` as a
		// workaround when helicon was unreachable for hours — the fallback is
		// per-run and touches no config, so the next run probes again.
		mt.Prefix = dockerPrefix(p)
		// The LAST candidate's reason: with one host it is that host's, and
		// with several it is why the final attempt failed, after each earlier
		// skip was already printed.
		mt.FellBackFrom, mt.FallbackWhy = targets[len(targets)-1].Host, pool.Why
		return mt, nil
	}
	target.Cores = pool.Candidates[0].Ready.Cores
	mt.Target = target
	return mt, nil
}

// measuredOnOf resolves a run's provenance from the adapters it used and the
// tuning that produced them.
//
// The adapter walk lives in internal/verify (MeasuredOnAdapters), where the
// per-leg stamp is also taken. Asking the same question twice, from two type
// switches, is how a run's summary comes to disagree with its own legs — and
// the disagreement is silent, since both produce a plausible string.
//
// A fallback is the case the adapters alone cannot express: they are local, so
// the adapter walk would call it a plain local run and lose the fact that a
// remote measurement was expected and did not happen. The tuning is the only
// thing that still remembers.
func measuredOnOf(adapters []mutation.Adapter, mt mutationTuning) string {
	if mt.FellBackFrom != "" {
		return verify.MeasuredAfterFallback(mt.FellBackFrom, mt.FallbackWhy)
	}
	return verify.MeasuredOnAdapters(adapters)
}

// profileCacheVars reads the toolchain cache variables the detected stack
// profile declares, so the mutation runner never holds a per-language table of
// its own (the locked cache_var_source decision).
//
// Every failure path returns nil rather than an error, deliberately. This is
// disk hygiene: a repo whose stack does not resolve, or whose user profile
// directory is unreadable, must still be verifiable. Failing a verify because
// the build cache could not be redirected would trade a bounded disk problem
// for an unusable command.
func profileCacheVars(p *project.Project, repoDir string) []string {
	if p == nil {
		return nil
	}
	profiles, err := stack.LoadAll()
	if err != nil {
		return nil
	}
	// The recorded id wins when there is one. Falling back to DETECTION when
	// there is not is what keeps this from being dead config: stack.profile is
	// newer than most project.toml files — dross's own does not carry it — and
	// a feature that silently does nothing for every repo onboarded before the
	// field existed is indistinguishable from one that was never wired up.
	id := strings.TrimSpace(p.Stack.Profile)
	if id == "" && repoDir != "" {
		id = stack.Detect(repoDir, profiles)
	}
	if id == "" || id == stack.Unsupported {
		return nil
	}
	sp := stack.ByID(profiles, id)
	if sp == nil {
		return nil
	}
	return sp.MutationCache.Vars
}

// configuredAdapters returns the list of mutation adapters appropriate
// for the project, with the runtime prefix or the granted remote applied,
// plus the tuning it resolved — the caller needs the latter to record where
// the run's numbers actually came from.
func configuredAdapters(p *project.Project, root string, skip bool) ([]mutation.Adapter, mutationTuning, error) {
	if skip {
		return nil, mutationTuning{}, nil // verify still runs — files end up in Skipped
	}
	mt, err := resolveMutationTuning(p, root)
	if err != nil {
		return nil, mutationTuning{}, err
	}
	// Project root for stryker is the runtime's cwd — host cwd for native,
	// or the host cwd for docker (we read the report via bind-mounted fs).
	// If docker volume layout diverges, this is where we'd surface config.
	cwd, _ := os.Getwd()
	cacheVars := profileCacheVars(p, filepath.Dir(root))
	all := []mutation.Adapter{
		&mutation.Stryker{
			Prefix:      mt.Prefix,
			ProjectRoot: cwd,
			Workdir:     p.Mutation.Stryker.Workdir,
			Remote:      mt.Target,
			CacheVars:   cacheVars,
			// Only consulted for a remote run, where the host has to install
			// dependencies before stryker can resolve anything. Passed rather
			// than defaulted: installing with the wrong manager produces a tree
			// stryker resolves differently.
			PackageManager: p.Stack.PackageManager,
		},
		mt.gremlins(cwd, p, cacheVars),
		&mutation.StrykerNet{Prefix: mt.Prefix, ProjectRoot: cwd, Remote: mt.Target, CacheVars: cacheVars},
	}
	if len(p.Mutation.Adapters) == 0 {
		return all, mt, nil
	}
	// [mutation] adapters = [...] allowlist: files whose adapter is filtered
	// out fall into verify's existing Skipped path downstream.
	allowed := map[string]bool{}
	for _, name := range p.Mutation.Adapters {
		allowed[name] = true
	}
	var out []mutation.Adapter
	for _, a := range all {
		if allowed[a.Name()] {
			out = append(out, a)
		}
	}
	return out, mt, nil
}

// dockerPrefix returns the runtime command prefix for docker mode.
// For native, returns "". For docker, derives from runtime.test_command
// (which already has the right shape: "docker compose exec app pnpm test").
//
// We strip the trailing runner+args to get the prefix. Field-based
// (not substring) so a container name that happens to match a runner
// name (e.g. "docker compose exec node node test.js") doesn't fool us.
func dockerPrefix(p *project.Project) string {
	if p.Runtime.Mode != "docker" {
		return ""
	}
	tc := p.Runtime.TestCommand
	fields := strings.Fields(tc)
	// The prefix's leading binary must be EXACTLY "docker" — not merely a
	// string starting with "docker" (HasPrefix would accept "dockerevil",
	// promoting an arbitrary PATH binary into the exec prefix built below).
	// project.toml is a committed file, so under clone-and-run this is the
	// difference between a bounded `docker` invocation and arbitrary code.
	if len(fields) == 0 || fields[0] != "docker" {
		return "docker compose exec app"
	}
	runners := map[string]bool{
		"pnpm": true, "npm": true, "yarn": true, "bun": true,
		"node": true, "deno": true,
		"go": true, "make": true,
	}
	// We need at minimum [docker, compose, exec, <service>] before any
	// runner, so start scanning from index 4.
	for i := 4; i < len(fields); i++ {
		if runners[fields[i]] {
			return strings.Join(fields[:i], " ")
		}
	}
	return "docker compose exec app"
}

// mutationCandidates splits the scope's file set into what may be handed to a
// mutation adapter and what may not. The scope itself keeps every path —
// filtering a report has to recognise them all — but two kinds are held back:
//
//   - gone: paths no longer in the working tree. A deleted file cannot be
//     mutated, and passing one to `stryker --mutate` fails the whole leg. The
//     caller still records them, as skips with a reason.
//   - .dross/ bookkeeping: dropped entirely, from both lists. Every phase's
//     git diff necessarily contains its own spec.toml, plan.toml and
//     changes.json, so recording them as skips would seed four to six
//     permanent NOTEs into every verify.toml — a standing backlog no phase can
//     ever drain, which is what rule r-02 forbids.
func mutationCandidates(repoDir string, files []string) (dispatch, gone []string) {
	for _, f := range files {
		if f == ".dross" || strings.HasPrefix(f, ".dross/") {
			continue
		}
		if _, err := os.Stat(filepath.Join(repoDir, f)); err != nil {
			gone = append(gone, f)
			continue
		}
		dispatch = append(dispatch, f)
	}
	return dispatch, gone
}

// recordVerifyOutcome writes a telemetry outcome event capturing the
// shape of this verify run — verdict, mutation score, file/criterion
// counts. Never logs file paths or criterion text.
//
// t may be nil (e.g. when finalizing after tests.json was cleaned up
// or never written). When nil, falls back to the summary block in
// verify.toml, which the LLM populates during /dross-verify.
func recordVerifyOutcome(t *verify.Tests, v *verify.Verify) {
	counts := map[string]int{
		"criteria": len(v.Criteria),
		"findings": len(v.Findings),
	}
	nums := map[string]float64{}

	if t != nil {
		counts["languages"] = len(t.Languages)
		counts["skipped"] = len(t.Skipped)
		files := 0
		killed := 0
		survived := 0
		timeouts := 0
		for _, lr := range t.Languages {
			files += len(lr.Files)
			if lr.Mutation != nil {
				killed += lr.Mutation.Killed
				survived += lr.Mutation.Survived
				timeouts += lr.Mutation.Timeout
			}
		}
		counts["files"] = files
		counts["mutants_killed"] = killed
		counts["mutants_survived"] = survived
		// Both counts are already post-filter — Tests carries the in-scope
		// reports — so this IS the in-scope score, and it is computed by the
		// same mutation.PooledScore that writes verify.toml's number. The two
		// used to diverge as soon as a run had two legs or a single timeout,
		// which meant a phase's judgement depended on which surface you read.
		counts["mutants_in_scope"] = v.Summary.MutantsInScope
		counts["out_of_scope"] = len(t.OutOfScope)
		counts["mutants_timeout"] = timeouts
		if score := mutation.PooledScore(killed, survived, timeouts); score > 0 {
			nums["mutation_score"] = score
		}
	} else {
		counts["mutants_killed"] = v.Summary.MutantsKilled
		counts["mutants_survived"] = v.Summary.MutantsSurvived
		if v.Summary.MutationScore > 0 {
			nums["mutation_score"] = v.Summary.MutationScore
		}
	}

	tags := map[string]string{
		"verdict": v.Verify.Verdict,
	}
	if v.Summary.MutationStatus != "" {
		tags["mutation_status"] = v.Summary.MutationStatus
	}
	// Which sources the scope came from, so a run of degraded verifies is
	// visible in aggregate. A source name, never a path.
	if t != nil && t.Scope != nil {
		tags["scope_source"] = t.Scope.Source
	}
	recordVerifyPhaseOutcome(v.Verify.Phase, counts, nums, tags)
}

// recordVerifyPhaseOutcome mirrors RecordOutcomeEvent but stamps the
// phase id on the event. Verify events (mechanical pending + resolved
// finalize) need phase identity so `dross stats` can match a later
// resolved event to the pending one it closes; without it every
// mechanical run inflates the pending count forever. Same
// swallow-on-error guarantee as RecordOutcomeEvent.
func recordVerifyPhaseOutcome(phaseID string, counts map[string]int, numbers map[string]float64, tags map[string]string) {
	if !telemetryEnabled() {
		return
	}
	repoHash := ""
	if root, _, err := LocateRoot(); err == nil {
		repoHash = telemetry.HashRepo(filepath.Dir(root))
	}
	_ = telemetry.Append(telemetryPath(), telemetry.Event{
		Kind:     "outcome",
		Command:  "verify",
		Phase:    phaseID,
		Counts:   counts,
		Numbers:  numbers,
		Tags:     tags,
		RepoHash: repoHash,
	})
}

func printVerifySummary(t *verify.Tests, v *verify.Verify) {
	Printf("verify: phase %s\n", t.Phase)
	if len(t.Languages) == 0 && len(t.Skipped) == 0 {
		Print("  (nothing to mutation-test)")
	}
	for _, lr := range t.Languages {
		if lr.Error != "" {
			Printf("  %s (%s): %d files — adapter FAILED: %s\n", lr.Name, lr.Tool, len(lr.Files), lr.Error)
			continue
		}
		if lr.Mutation == nil {
			Printf("  %s (%s): %d files — no mutation report\n", lr.Name, lr.Tool, len(lr.Files))
			continue
		}
		m := lr.Mutation
		Printf("  %s (%s): %d files — killed=%d survived=%d (not_covered=%d) timeout=%d errors=%d score=%.2f\n",
			lr.Name, lr.Tool, len(lr.Files), m.Killed, m.Survived, m.NotCovered, m.Timeout, m.Errors, m.Score)
		if m.NotCovered > 0 {
			// Show the gremlins-style efficacy (ignores NOT COVERED) when it
			// diverges meaningfully from dross's score. Often signals a
			// coverage blind spot — e.g. Go's package-init code in top-level
			// var arrays — rather than weak tests.
			efficacyDenom := m.Killed + (m.Survived - m.NotCovered)
			if efficacyDenom > 0 {
				efficacy := float64(m.Killed) / float64(efficacyDenom)
				Printf("    note: %d/%d mutants NOT COVERED — tests never ran them; efficacy excluding them = %.2f\n",
					m.NotCovered, m.Killed+m.Survived+m.Timeout, efficacy)
			}
		}
	}
	for _, s := range t.Skipped {
		Printf("  skipped %s — %s\n", s.File, s.Reason)
	}
	printScopeSummary(t, v)
	printOverallScore(v)
	switch v.Summary.MutationStatus {
	case verify.MutationOutOfScope:
		Print("  mutation status: out-of-scope — the adapters found mutants, but every one of them " +
			"landed in a file this phase never touched. Nothing here measures these changes; " +
			"/dross-verify will base the verdict on criterion coverage alone.")
	case verify.MutationUnmeasurable:
		Print("  mutation status: unmeasurable — adapter ran but instrumented 0 mutants " +
			"(likely the project's mutation scope excludes every touched file). " +
			"Score is 0/0 — /dross-verify will base the verdict on criterion coverage alone.")
	case verify.MutationSkipped:
		Print("  mutation status: skipped — no adapter ran. " +
			"Score is 0/0 — /dross-verify will base the verdict on criterion coverage alone.")
	}
	Printf("\nWrote tests.json + verify.toml (verdict=%s — /dross-verify will fill criterion mappings).\n", v.Verify.Verdict)
}

// printOverallScore states the number the phase is judged on, with what it was
// computed over.
//
// A bare ratio is not a measurement anyone can act on: 0.90 over 10 mutants and
// 0.90 over 400 are the same number and not the same evidence. And a survivor
// the tooling cannot reach is a different fact from one the tests missed — so
// the uncoverable count is named too, which is what makes "0.90" and "1.00 on
// everything reachable" both visible at once.
//
// Both distinctions were being written into verify.toml notes BY HAND on every
// run of this milestone. A convention that has to be re-typed each time is one
// that belongs in the code.
func printOverallScore(v *verify.Verify) {
	if v.Summary.MutationStatus != verify.MutationMeasured {
		// The other statuses print their own line explaining that the score is
		// 0/0 and why. Adding a denominator to a number that means nothing
		// would dress it up as a measurement.
		return
	}
	Printf("  score: %.2f over %d in-scope mutant(s) — killed=%d survived=%d\n",
		v.Summary.MutationScore, v.Summary.MutantsInScope,
		v.Summary.MutantsKilled, v.Summary.MutantsSurvived)
	// Only when there are any. A line that is always present stops being read,
	// and "0 uncoverable" is not news.
	if v.Summary.MutantsNotCovered > 0 {
		reachable := v.Summary.MutantsInScope - v.Summary.MutantsNotCovered
		Printf("    of which %d uncoverable by construction (gremlins attributes no coverage block to them) — "+
			"efficacy over the %d reachable = %.2f\n",
			v.Summary.MutantsNotCovered, reachable,
			mutation.PooledScore(v.Summary.MutantsKilled, v.Summary.MutantsSurvived-v.Summary.MutantsNotCovered, 0))
	}
}

// scopeFileListCap bounds how many scoped files are named before the line
// collapses to a count. Naming them is the point — a reader has to be able to
// see the scope was what they expected — but a fifty-file phase should not
// bury the score under its own file list.
const scopeFileListCap = 12

// printScopeSummary reports what the run scoped to, how much it filtered, and
// whether that view was complete.
//
// The in-scope mutant count sits beside the score deliberately: 0.50 over two
// mutants and 0.50 over two hundred are the same number and not the same
// evidence, and the locked decision was to surface the sample size rather than
// add a threshold that could launder a real survivor.
func printScopeSummary(t *verify.Tests, v *verify.Verify) {
	if t.Scope == nil {
		return
	}
	names := t.Scope.Files
	suffix := ""
	if len(names) > scopeFileListCap {
		suffix = fmt.Sprintf(" (+%d more)", len(names)-scopeFileListCap)
		names = names[:scopeFileListCap]
	}
	base := "no base resolved"
	if t.Scope.Base != "" {
		base = "base " + short(t.Scope.Base)
	}
	Printf("  scope: %d file(s) from %s, %s — %s%s\n",
		len(t.Scope.Files), t.Scope.Source, base, strings.Join(names, ", "), suffix)
	Printf("  in-scope mutants: %d (score %.2f)\n", v.Summary.MutantsInScope, v.Summary.MutationScore)
	if n := len(t.OutOfScope); n > 0 {
		Printf("  filtered %d out-of-scope survivor(s) — see `out_of_scope` in tests.json\n", n)
	}
	for _, d := range t.Scope.Degraded {
		Printf("  scope degraded: %s\n", d)
	}
}

// workTreeIdentifier resolves a survivor's identity against the working tree.
//
// It reads through repoRoot but keys on the path the tool reported, so the key
// a verify run computes is the same one `dross survivor accept` recorded from
// any working directory. Resolving on an absolute path would mint a key nothing
// could ever match again.
type workTreeIdentifier struct{ repoRoot string }

func (w workTreeIdentifier) Identify(file string, line int, op string) (string, bool, error) {
	rel := file
	if filepath.IsAbs(rel) {
		if r, err := filepath.Rel(w.repoRoot, rel); err == nil {
			rel = r
		}
	}
	rel = filepath.ToSlash(rel)
	res, err := survivor.ResolveAt(w.repoRoot, rel, line, op)
	if err != nil {
		return "", false, err
	}
	return res.Key, res.Ambiguous, nil
}

// acceptedReasons flattens the store into the key→reason map the classifier
// takes. Resolution failures are surfaced rather than swallowed: an acceptance
// whose reason cannot be resolved must not silently suppress a survivor.
func acceptedReasons(store *survivor.Store) (map[string]string, error) {
	out := map[string]string{}
	for _, a := range store.Accepted {
		reason, err := store.ReasonFor(a)
		if err != nil {
			return nil, fmt.Errorf("load survivors: %w", err)
		}
		out[a.Key] = reason
	}
	return out, nil
}

// routedSurvivors builds the key→target map from every phase's [[deferred]]
// entries. It walks all specs (via collectDeferred) rather than just the
// current phase's: a survivor routed while phase A was current is still routed
// when phase B runs, and a phase-local read would resurrect it as unclassified
// debt the moment the phase changed.
//
// Dismissed entries are skipped — a dismissed item is triaged away, not parked
// at a destination — as are entries with no target, which are "someday" and
// therefore still unclassified.
func routedSurvivors(root string) (map[string]string, error) {
	entries, err := collectDeferred(root)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, e := range entries {
		if e.Survivor == "" || e.Target == "" || e.Dismissed {
			continue
		}
		out[e.Survivor] = e.Target
	}
	return out, nil
}

// appendStalenessNotes reports acceptances whose subject is gone (c-5) as
// NOTEs. Deliberately NOT findings that gate: a stale acceptance is bookkeeping
// to clean up, and failing a phase over one would punish the phase that
// happened to run next — the same reasoning as a degraded scope.
func appendStalenessNotes(v *verify.Verify, repoRoot string, store *survivor.Store) {
	rep := survivor.StaleAcceptances(repoRoot, store)
	for _, s := range rep.Stale {
		v.Findings = append(v.Findings, verify.Finding{
			Severity: "NOTE",
			Text: fmt.Sprintf("stale acceptance: %s (%s) — %s — retire it from .dross/%s",
				s.File, s.Key, s.Reason, survivor.StoreFile),
		})
	}
	for _, u := range rep.Unverifiable {
		v.Findings = append(v.Findings, verify.Finding{
			Severity: "NOTE",
			Text:     fmt.Sprintf("acceptance could not be checked: %s (%s) — %v", u.File, u.Key, u.Err),
		})
	}
}

// printLifecycleSummary prints the one line that makes the drain measurable:
// how many survivors are this phase's own, how many have a destination, how
// many are accepted, and how many still need a decision. The four counts sum to
// the run's survivor total by construction.
//
// gateCount is verify.toml's summary.unclassified_in_scope — the number the
// verdict actually fails on — and it is PASSED IN rather than recomputed here.
// That is the whole point of this signature: the two surfaces used to derive
// the same fact separately and disagree about it, so a run with four
// undispositioned in-diff survivors printed "0 unclassified" on screen while
// the file next to it recorded unclassified_in_scope = 4. The line a human
// reads said all-clear while the gate was open. Sharing the value makes that
// disagreement unrepresentable.
func printLifecycleSummary(lc *verify.Lifecycle, gateCount int) {
	if lc == nil || lc.Total() == 0 {
		return
	}
	Printf("  survivors: %d in-diff, %d routed, %d accepted, %d out-of-diff unclassified\n",
		len(lc.InDiff), len(lc.Routed), len(lc.Accepted), len(lc.Unclassified))
	// The gate line, always printed when the lifecycle has anything to say —
	// including when it is zero, because "the gate is clear" is the fact a
	// reader is looking for and its absence is not the same statement.
	if gateCount > 0 {
		Printf("    ↳ %d undispositioned in scope — THE VERDICT GATE IS OPEN. "+
			"Clear each with `dross survivor accept <file>:<line> --op OP --reason ...` "+
			"or `dross survivor route <file>:<line> --op OP --target <phase>`\n", gateCount)
	} else {
		Print("    ↳ 0 undispositioned in scope — the verdict gate is clear")
	}
}

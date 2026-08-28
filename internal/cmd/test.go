package cmd

// `dross test` — the one place dross runs this repo's test suite.
//
// Until this command, dross never ran `runtime.test_command` at all. It hashed
// it for consent (trust.go) and the prompts told the AGENT to type it into
// Bash. That left the consent gate covering a command nothing executed, and —
// the reason this phase exists — left no execution site to point at another
// machine. You cannot delegate a run that no code performs.
//
// So this is deliberately the whole surface: consent, selector, transport and
// exit status resolve here, and execute.md / quick.md / verify.md call this
// instead of interpolating the raw command. One site to gate, one site to
// delegate.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/argfence"

	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/remote"
	"github.com/Rivil/dross/internal/testlane"
)

// Exit codes. They are a contract, not an implementation detail: a caller
// deciding whether to commit reacts differently to "your code is broken" and
// "the run never happened", and collapsing the two is how a dead transport
// gets read as a clean suite.
//
// The suite's OWN exit status is deliberately not propagated. A suite is free
// to exit 3, and a raw passthrough would make its status collide with the
// transport band below — after which the codes would mean nothing. Pass/fail
// is what a caller gates on; the detail is in the message.
const (
	// exitSuiteFailed: the suite ran and went red. Ordinary failure.
	exitSuiteFailed = 1
	// exitTransport: the run never happened — host unreachable, ssh refused,
	// stream died. Nothing was measured.
	exitTransport = 3
	// exitPartial: the connection held but the transfer did not complete, so
	// what ran (if anything) ran against an incomplete tree.
	exitPartial = 4
	// exitBadFileSet: the --files argv named a path outside this repository.
	// A caller mistake, not a configuration one — the file set was never
	// resolved and no lane ran. Kept apart from exitNothingMeasured because
	// the two send the reader to different files: this one to their own
	// command line, that one to project.toml.
	exitBadFileSet = 2
	// exitNothingMeasured: the file set was well-formed and matched no
	// declared lane, so the run would have measured nothing. Reported instead
	// of exiting 0, which is the false-green c-8 exists to prevent.
	exitNothingMeasured = 5
	// exitLaneRefused: at least one matched lane's command is untrusted or
	// stale on this machine, so that lane did not run. Distinct from a red
	// suite: the lane's code was never measured, and a caller that read this
	// as "your code is broken" would go looking for a bug that is not there.
	exitLaneRefused = 6
	// exitPrepareFailed: a matched lane's prepare command failed, so that
	// lane's test command never ran. Distinct from a red suite for the same
	// reason exitLaneRefused is: a bootstrap that failed measured nothing
	// about the code, and a caller that read this as "your tests are broken"
	// would go hunting a bug in code that was never executed.
	exitPrepareFailed = 7
)

// exitRank orders the outcomes of a multi-lane run so the WORST one decides
// the run's status.
//
// The order is not severity-of-inconvenience, it is how badly each outcome
// misleads a caller who is deciding whether to commit:
//
//	transport (3) > partial (4) > prepare (7) > red (1) > refused (6) > nothing measured (5)
//
// Transport and partial outrank everything because they mean the tree that ran
// was not the tree on disk — any verdict from that run, green or red, is about
// something else. A failed prepare sits just under them and above red for a
// narrower version of the same reason: the lane it belongs to measured nothing,
// so reporting a neighbour's red would tell the user their code is broken while
// leaving the lane that never ran invisible. Red outranks refused because a failing test is a fact about
// the code and an ungranted lane is a fact about this machine; reporting the
// consent problem while a suite is broken would send the user to `dross trust`
// and let them commit a red change once they got there. Nothing-measured is
// last because it only ever co-occurs as the absence of the others.
//
// A zero rank is success; unknown codes rank above it so an outcome added later
// cannot silently be swallowed by a green.
func exitRank(code int) int {
	switch code {
	case 0:
		return 0
	case exitTransport:
		return 6
	case exitPartial:
		return 5
	case exitPrepareFailed:
		return 4
	case exitSuiteFailed:
		return 3
	case exitLaneRefused:
		return 2
	case exitNothingMeasured:
		return 1
	}
	return 3
}

// worseOutcome keeps whichever of two lane results should decide the run.
//
// Worst-wins rather than first-wins or last-wins: with lanes running in
// declaration order, first-wins would let a lane's position in project.toml
// decide what the run reports, and last-wins would let a green lane declared
// after a red one mask it.
func worseOutcome(a, b error) error {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if exitRank(ExitCode(b)) > exitRank(ExitCode(a)) {
		return b
	}
	return a
}

// ExitCodeError carries the process exit status a failure should produce.
// main.go reads it through ExitCode; anything without one exits 1 as before.
type ExitCodeError struct {
	Code int
	Err  error
}

func (e *ExitCodeError) Error() string { return e.Err.Error() }
func (e *ExitCodeError) Unwrap() error { return e.Err }

// ExitCode maps an error to the process exit status. A nil error is 0, an
// untagged error is 1 — the behaviour every other command already had.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var ec *ExitCodeError
	if errors.As(err, &ec) {
		return ec.Code
	}
	return 1
}

// spawnLocal is the local-execution seam. Tests replace it to record the argv
// without running anything; production never reassigns it.
var spawnLocal = runLocalCommand

// runLocalCommand runs one shell command line in dir, streaming its output to
// the given writers as it arrives.
//
// Streaming rather than capturing is the point. The suite takes minutes; a
// command that prints nothing until it finishes is indistinguishable from a
// hang, and the agent driving it reads the tail as it goes. os/exec writes
// straight through when Stdout is set, so this is buffer-free by construction
// rather than by a flush discipline someone has to maintain.
func runLocalCommand(dir, line string, stdout, stderr io.Writer) error {
	return runLocalCommandCtx(context.Background(), dir, line, stdout, stderr)
}

// spawnLocalCtx is the same seam with a deadline. Tests replace it to record
// the argv and the cwd without running anything; production never reassigns it.
var spawnLocalCtx = runLocalCommandCtx

// runLocalCommandCtx is runLocalCommand with a cancellable context, added for
// the red-proof replay: an unbounded spawn there would turn a hung proof into a
// hung repoint, and a repoint that never returns is worse than one that refuses.
//
// WaitDelay is what makes the kill actually terminate the call. Killing `sh`
// leaves its children holding the pipe ends, so Wait would block on a copy that
// never ends — the delay closes the descriptors and returns instead.
func runLocalCommandCtx(ctx context.Context, dir, line string, stdout, stderr io.Writer) error {
	argv, err := shArgv(line)
	if err != nil {
		return err
	}
	c := exec.CommandContext(ctx, "sh", argv...)
	c.Dir = dir
	c.Stdout = stdout
	c.Stderr = stderr
	c.Stdin = nil
	c.WaitDelay = 5 * time.Second
	return c.Run()
}

// shArgv is the fenced builder for an `sh -c` invocation, in the same shape
// every other spawn site in this repo uses: the fence lives in the builder and
// the caller spreads the result.
//
// sh reads options before -c and honours no end-of-options token, so a command
// line beginning with a dash would be taken as a shell option (`-i`, `-x`)
// rather than as the script — which is why argfence's policy for sh is Reject
// rather than Separator. The line here is the user's own consented
// runtime.test_command and is not a derived value today; the fence is what
// keeps that true the first time a caller passes one.
func shArgv(line string) ([]string, error) {
	return shArgvFor("runtime.test_command", line)
}

// shArgvFor is shArgv with the field label the refusal should name. `dross run`
// spawns a different [runtime] key per slot, and a fence refusal that always
// blamed test_command would point at the wrong line to edit.
func shArgvFor(field, line string) ([]string, error) {
	if err := argfence.RejectLeadingDash("sh", field, line); err != nil {
		return nil, err
	}
	return []string{"-c", line}, nil
}

// testCommandLine appends a package/path selector to the consented command.
//
// With no selector the line is byte-identical to runtime.test_command — the
// string the user consented to. That identity matters: `dross test` with no
// arguments must be exactly what `dross trust` showed them, or the gate is
// approving one command and running another.
//
// Selector arguments are shell-quoted because the line goes to `sh -c`. The
// consented command is the user's own text and stays verbatim; the arguments
// come from an agent's argv and do not.
func testCommandLine(base string, selector []string) string {
	line := strings.TrimSpace(base)
	for _, s := range selector {
		line += " " + shellQuoteArg(s)
	}
	return line
}

// shellQuoteArg single-quotes an argument for `sh -c`, the same allowlist-free
// approach internal/remote uses: wrap in single quotes and escape any embedded
// single quote, which is total rather than a metacharacter blocklist.
func shellQuoteArg(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Test registers `dross test`.
func Test() *cobra.Command {
	var local bool
	var files []string
	c := &cobra.Command{
		Use:   "test [selector...]",
		Short: "Run this repo's test suite",
		Long: "Runs runtime.test_command — the command `dross trust` consented to, and\n" +
			"nothing else. Trailing arguments are appended as a package/path selector,\n" +
			"so a targeted re-run after a fix costs a package rather than the suite.\n\n" +
			"--files <path> (repeatable) resolves the given repo-relative paths against\n" +
			"the declared [[runtime.test_lane]] blocks and runs only the lanes they hit.\n" +
			"A repo with no lanes ignores it and runs the whole suite, unchanged.\n\n" +
			"Output streams as it arrives and the exit status reports the suite, not\n" +
			"the runner.",
		SilenceUsage: true,
		// Stated explicitly because this command has a subcommand now.
		// cobra's default arg policy refuses any positional on a command with
		// subcommands whose first argument names none of them — so the
		// selector, which is the whole point of the trailing arguments, would
		// come back as `unknown command "./internal/..."`. Declaring the
		// policy also makes the rule independent of where this command sits in
		// a tree, which the default is not.
		//
		// It does not weaken the `lane` verb: cobra resolves args[0] against
		// subcommands BEFORE any arg validation runs, so `dross test lane`
		// still reaches the subcommand.
		Args: cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			if err := refuseFilesWithSelector(files, args); err != nil {
				return err
			}
			root, err := FindRoot()
			if err != nil {
				return err
			}
			proj, err := project.Load(filepath.Join(root, project.File))
			if err != nil {
				return err
			}
			repoDir := filepath.Dir(root)
			// Lanes are opt-in, and this is where that is enforced (locked
			// bare_test_run): with none declared, --files changes nothing and
			// the command runs runtime.test_command exactly as it always did.
			//
			// On the lane path the per-lane grants REPLACE requireExecConsent
			// rather than stacking on it. Stacking would make a lanes-only
			// repo — no runtime.test_command at all, which is the shape lanes
			// most exist to serve — refuse every lane run at a gate guarding a
			// command it never spawns. Each lane's own command still passes
			// through its own grant below, so nothing is ungated; the gate
			// just moved to the line that actually runs.
			if len(files) > 0 && len(proj.Runtime.TestLane) > 0 {
				return runTestLanes(root, repoDir, proj, files, local)
			}
			// Before any spawn: a refusal that had already run the suite would
			// have done the thing it was refusing to authorize.
			if err := requireExecConsent(); err != nil {
				return err
			}
			line := testCommandLine(proj.Runtime.TestCommand, args)
			return runTest(root, repoDir, line, local)
		},
	}
	c.Flags().BoolVar(&local, "local", false, "run on this machine even when a remote is granted")
	c.Flags().StringArrayVar(&files, "files", nil, "repo-relative path to resolve against the declared test lanes (repeatable)")
	c.AddCommand(testLane())
	return c
}

// refuseFilesWithSelector rejects the one combination that cannot mean
// anything coherent.
//
// A selector is APPENDED to the command line. A lane's command has to run
// byte-identically to the line its consent grant fingerprinted, so nothing may
// be appended to it — and silently dropping the positionals instead would run
// something narrower than the caller asked for while reporting success.
func refuseFilesWithSelector(files, args []string) error {
	if len(files) == 0 || len(args) == 0 {
		return nil
	}
	return fmt.Errorf(
		"--files and a positional selector cannot be combined (got --files %s and selector %s).\n\n"+
			"A selector is appended to the command line, but a lane's command must run\n"+
			"byte-identically to the line its consent grant fingerprinted, so nothing may\n"+
			"be appended to it.\n\n"+
			"Pick one:\n\n"+
			"    dross test --files %s\n"+
			"    dross test %s",
		strings.Join(files, " "), strings.Join(args, " "),
		strings.Join(files, " --files "), strings.Join(args, " "))
}

// runTestLanes resolves a file set against the declared lanes.
//
// It deliberately spawns NOTHING yet — resolution and the per-lane consent gate
// land in separate commits, and this is the earlier one. Splitting them there
// is what keeps this commit from ever executing a command line out of
// project.toml that no fingerprint covers.
func runTestLanes(root, repoDir string, proj *project.Project, files []string, local bool) error {
	globs := make([][]string, len(proj.Runtime.TestLane))
	for i, lane := range proj.Runtime.TestLane {
		globs[i] = lane.Match
	}
	sel := testlane.Select(globs, files)

	// Checked FIRST, and it poisons the whole set. Resolving the in-tree half
	// of a half-broken argv would report on a subset the caller never asked
	// for — and they would read the result as covering everything they listed.
	if len(sel.OutOfTree) > 0 {
		return &ExitCodeError{Code: exitBadFileSet, Err: fmt.Errorf(
			"refusing to run: these paths are OUTSIDE THIS REPOSITORY — %s\n\n"+
				"--files takes repo-relative paths. A lane's globs are written against the\n"+
				"repo, so a path from elsewhere can never match one; this is an argv problem,\n"+
				"not a lane-configuration problem. Nothing was resolved and no lane ran.",
			strings.Join(sel.OutOfTree, " "))}
	}

	if len(sel.Lanes) == 0 {
		// The whole reason c-8 exists. Exiting 0 here would report a green
		// run to a caller deciding whether to commit, having measured nothing
		// at all — which is worse than any red.
		return &ExitCodeError{Code: exitNothingMeasured, Err: fmt.Errorf(
			"refusing to report a run that measured nothing: no declared test lane matches %s\n\n"+
				"Declared lanes: %s\n\n"+
				"Either widen a lane's match globs (`dross test lane list`), or run the whole\n"+
				"suite with a bare `dross test`.",
			strings.Join(sel.Unmatched, " "), strings.Join(laneNames(proj), ", "))}
	}

	// A PARTIAL miss is reported and the matched lanes still run (locked
	// unmatched_files): a task that edited Go code and a README must not drag
	// in the full suite, but the README must not vanish from the transcript
	// either, or the run silently covers less than the caller listed.
	if len(sel.Unmatched) > 0 {
		Printf("no lane matches: %s\n", strings.Join(sel.Unmatched, " "))
	}

	// The lane's INDEX travels with it all the way to the spawn, because
	// sel.Matched is keyed by it: a lane carried around as a bare TestLane
	// loses the only handle on the paths that selected it, and the selector
	// would have to be re-derived from the whole file set — which is exactly
	// the c-4 leak of one lane's paths into another lane's command line.
	matched := make([]matchedLane, 0, len(sel.Lanes))
	for _, i := range sel.Lanes {
		matched = append(matched, matchedLane{index: i, lane: proj.Runtime.TestLane[i]})
	}

	// Fenced up front, every matched lane, before any of them runs. Checking
	// inside the loop would discover a malformed lane line halfway through a
	// multi-lane run, with earlier lanes already executed — and the fence
	// exists precisely to stop a line from reaching a shell.
	//
	// shArgvFor rather than shArgv: the label is what the refusal tells the
	// user to go and edit, and blaming runtime.test_command for a lane's
	// command would send them to a line that is perfectly fine.
	for _, m := range matched {
		if _, err := shArgvFor(laneField(m.lane.Name), m.lane.Command); err != nil {
			return err
		}
		// The prepare goes through the SAME fence, in the SAME up-front sweep.
		// A bootstrap line is a line reaching a shell exactly as a command is,
		// and checking it inside the run loop would refuse a malformed prepare
		// on the second lane with the first lane's suite already run — the one
		// thing an up-front fence exists to make impossible.
		if m.lane.Prepare != "" {
			if _, err := shArgvFor(laneField(m.lane.Name), m.lane.Prepare); err != nil {
				return err
			}
		}
		// The style is checked here, in the same up-front sweep and against
		// no paths, because it is a property of project.toml rather than of
		// this file set. Discovering it inside the run loop would refuse a
		// lane with earlier lanes already spawned, and the point of a fence
		// is that nothing ran before it.
		if _, err := testlane.Derive(m.lane.Selector, nil); err != nil {
			return fmt.Errorf("%s: %w", laneField(m.lane.Name), err)
		}
	}

	// Consent is resolved for EVERY matched lane before any of them spawns, so
	// the transcript names every refusal up front rather than interleaving them
	// with test output, where a refusal scrolls past under a passing suite.
	var worst error
	runnable := make([]matchedLane, 0, len(matched))
	for _, m := range matched {
		// Resolved against lane.Command, never against the derived line
		// (locked selector_consent). The grant covers the line the user was
		// shown and approved; the selector is machine-derived repo-relative
		// paths appended to it, exactly as testCommandLine already does for
		// `dross test <selector>` against runtime.test_command. Fingerprinting
		// the derived line instead would go stale on every new file set and
		// refuse practically every scoped run.
		state, cerr := LaneConsented(root, repoDir, m.lane.Name, laneConsentLine(m.lane))
		if cerr != nil {
			// Printed AND folded into the outcome. Returning it alone would
			// lose it whenever another lane goes red and outranks it, and a
			// consent problem the user never sees is one they never fix.
			refusal := laneConsentRefusal(m.lane, state, cerr)
			Printf("%v\n\n", refusal)
			worst = worseOutcome(worst, &ExitCodeError{Code: exitLaneRefused, Err: refusal})
			continue
		}
		runnable = append(runnable, m)
	}
	if len(runnable) == 0 {
		// Nothing spawns, and the status is the refusal. A run where every
		// lane was refused measured exactly as much as one that matched
		// nothing, and must not read as green.
		return worst
	}

	// The host is chosen ONCE for the whole run, and the tree is pushed once.
	// Per-lane sync would re-push an unchanged tree for every lane, and — worse
	// — would make a mid-run transport failure look like it belonged to
	// whichever lane happened to be next.
	target, err := resolveTestTarget(root, repoDir, local)
	if err != nil {
		return err
	}
	if target != nil {
		if err := syncTreeTo(*target, repoDir); err != nil {
			return err
		}
	}

	// Misses are counted, never folded into worst. A lane that collected no
	// tests is not a verdict about the code — it is the absence of one — and
	// exitRank puts exitNothingMeasured above nil, so folding a single miss
	// through worseOutcome would fail a run whose other lanes all passed.
	misses := 0
	for _, m := range runnable {
		line, selector, ok := laneRunLine(repoDir, m.lane, sel.Matched[m.index])
		if !ok {
			// The lane declares a selector and every path that selected it
			// has since been deleted, so there is nothing to scope the run
			// to (locked missing_paths). It does not spawn: `go test
			// ./gone/...` is a hard runner error, which would read as a
			// failing gate for code the task deliberately removed.
			Printf("selector miss: lane %q — every path matching it is gone, so its %s selector scoped to nothing\n",
				m.lane.Name, m.lane.Selector)
			misses++
			continue
		}
		// The header precedes the lane's own output, always. A transcript
		// where the header trailed the run cannot attribute a failure to a
		// runner, which is the entire point of printing it.
		//
		// It prints the line that is about to be SPAWNED, selector included —
		// the same string, not a second one built the same way. A header
		// showing lane.Command while a derived line ran would be a transcript
		// that lies about what was measured.
		// The prepare runs FIRST, and is announced first — after the tree
		// sync above and before this lane's own header, which is the order it
		// actually happens in. A transcript that showed the bootstrap after
		// the suite it bootstrapped could not be read as a sequence.
		//
		// Per lane, never batched and never deduplicated across lanes (locked
		// prepare_scope): idempotence is the declared contract, so a repeat is
		// the no-op the user promised, while a dedup cache would make a lane's
		// spawn set depend on which neighbours happened to match.
		if m.lane.Prepare != "" {
			Printf("lane %s prepare: %s\n", m.lane.Name, m.lane.Prepare)
			// Spawned through the same runOneLane, so it lands on the same
			// host and the same transport as the command it precedes (locked
			// prepare_locality). A lane that bootstrapped only on the remote
			// would measure different things depending on where it landed.
			//
			// No selector is appended: the derived paths scope the suite, and
			// a bootstrap handed this file set's paths would be a different
			// command on every run.
			if err := runOneLane(target, repoDir, m.lane, m.lane.Prepare); err != nil {
				// The lane's own command does NOT run. A bootstrap that failed
				// measured nothing about the code, and running the suite
				// anyway would report the consequence as a verdict.
				// `continue`, not `return`: the run's other lanes are
				// unaffected. t-5 gives this its own exit code.
				worst = worseOutcome(worst, err)
				continue
			}
		}
		Printf("lane %s: %s\n", m.lane.Name, line)
		err := runOneLane(target, repoDir, m.lane, line)
		if code, miss := selectorMissCode(err, m.lane.EmptyExit); miss {
			Printf("selector miss: lane %q collected no tests for %s (exit %d)\n",
				m.lane.Name, strings.Join(selector, " "), code)
			misses++
			continue
		}
		worst = worseOutcome(worst, err)
	}

	// Only when EVERY lane that got as far as a spawn decision was a miss. One
	// miss beside a green lane is information; a run where nothing collected
	// anything measured exactly as much as a run that matched no lane at all,
	// and must not read as green.
	//
	// Folded through worseOutcome rather than returned, so it cannot outrank a
	// consent refusal or a red lane that this run also produced — exitRank
	// already puts nothing-measured last for precisely that reason.
	if misses > 0 && misses == len(runnable) {
		worst = worseOutcome(worst, &ExitCodeError{Code: exitNothingMeasured, Err: fmt.Errorf(
			"refusing to report a run that measured nothing: every matched lane's selector collected no tests")})
	}
	return worst
}

// selectorMissCode reports whether a lane's failure was its runner saying "I
// collected no tests", and which declared code said so.
//
// Two guards, both load-bearing. With no declared codes there is no miss at
// all: without a declaration only the empty-selector filter can produce one
// (locked empty_detection), so a lane that never opted in keeps every non-zero
// status as the red suite it has always been. And only a suite failure is
// eligible — a transport failure or an incomplete transfer carries an exit code
// too, and a run that never happened must never be relabelled as one that found
// nothing.
//
// The output is never read. Matching a runner's wording would make dross track
// every framework's phrasing across every version of it, which is the inference
// empty_detection exists to forbid.
func selectorMissCode(err error, codes []int) (int, bool) {
	if err == nil || len(codes) == 0 || ExitCode(err) != exitSuiteFailed {
		return 0, false
	}
	var ec exitCoder
	if !errors.As(err, &ec) {
		return 0, false
	}
	for _, c := range codes {
		if c == ec.ExitCode() {
			return c, true
		}
	}
	return 0, false
}

// matchedLane pairs a lane with its index in project.toml, which is the key
// testlane.Selection.Matched records its paths under.
type matchedLane struct {
	index int
	lane  project.TestLane
}

// laneRunLine builds the one command line a lane will spawn, and reports
// whether there is anything to spawn at all.
//
// ok is false only for a lane that declares a selector whose paths have all
// been deleted. A lane with no selector is always ok: its line is its command,
// byte-for-byte, and the existence filter never touches it — every lane written
// before this feature must behave exactly as it did, including when a caller
// names a path that is not there.
//
// The derived arguments come back alongside the line because the miss report
// names them: "collected no tests" is only actionable if the reader can see
// what it was looking in.
func laneRunLine(repoDir string, lane project.TestLane, paths []string) (string, []string, bool) {
	if lane.Selector == "" {
		return lane.Command, nil, true
	}
	// Filtered before translation (locked missing_paths): a task that deleted
	// a file would otherwise derive a package that no longer exists, and
	// `go test ./gone/...` is a hard runner error — a failing gate for work
	// the task did on purpose.
	live := make([]string, 0, len(paths))
	for _, p := range paths {
		if _, err := os.Stat(filepath.Join(repoDir, p)); err == nil {
			live = append(live, p)
		}
	}
	if len(live) == 0 {
		return "", nil, false
	}
	// The error was already raised by the up-front fence, against this exact
	// style. Reaching it here would mean the fence was skipped, and spawning
	// the unscoped command would be the silent whole-suite run the selector
	// exists to replace — so the lane does not run.
	args, err := testlane.Derive(lane.Selector, live)
	if err != nil || len(args) == 0 {
		return "", nil, false
	}
	return testCommandLine(lane.Command, args), args, true
}

// laneField is the project.toml key a lane's refusals name.
func laneField(name string) string {
	return fmt.Sprintf("runtime.test_lane[%s]", name)
}

// runOneLane runs one already-built lane line, here or on the already-synced
// host.
//
// The line arrives built rather than assembled here, and both transports get
// the same string: a lane whose selector reached only the local spawn would run
// scoped on this machine and whole on a remote, which is the same code
// measuring two different things depending on where it ran.
//
// The line is lane.Command with the lane's derived selector appended and
// shell-quoted — never a rewrite of the command itself. The consent
// fingerprint covers lane.Command, which is the string the grant was issued
// against and the string a refusal names (locked selector_consent).
func runOneLane(target *remote.Target, repoDir string, lane project.TestLane, line string) error {
	if target == nil {
		if err := spawnLocal(repoDir, line, os.Stdout, os.Stderr); err != nil {
			return &ExitCodeError{Code: exitSuiteFailed, Err: fmt.Errorf("test lane %q failed: %w", lane.Name, err)}
		}
		return nil
	}
	if err := runRemoteLine(*target, line); err != nil {
		return err
	}
	return nil
}

// laneNames is every declared lane's name, in declaration order, for a refusal
// that has to tell the user what they could have matched.
func laneNames(proj *project.Project) []string {
	names := make([]string, 0, len(proj.Runtime.TestLane))
	for _, lane := range proj.Runtime.TestLane {
		names = append(names, lane.Name)
	}
	return names
}

// spawnRemote is the remote-execution seam: a fully-built argv plus the script
// piped to its stdin, streamed the same way a local run is. Tests replace it to
// record what would have been spawned without touching a network.
//
// It is separate from internal/remote's own Exec because that one CAPTURES
// stdout — right for a probe, wrong for a suite whose output is the thing the
// caller is reading. internal/remote stays the argv builder; execution and
// streaming live here.
var spawnRemote = runRemoteCommand

// runRemoteCommand spawns a built remote argv, streaming output through.
//
// Written as Command(argv[0]) plus an explicit Args assignment rather than a
// `...` spread, for the same reason internal/remote's buildCommand is: the
// subprocess argv audit skips spreads, so the spread form would pass that gate
// by accident. This form is evaluated, and accepted by a named entry with a
// reason in subprocargs_audit_test.go.
func runRemoteCommand(argv []string, stdin string, stdout, stderr io.Writer) error {
	c := exec.Command(argv[0])
	c.Args = argv
	if stdin != "" {
		c.Stdin = strings.NewReader(stdin)
	}
	c.Stdout = stdout
	c.Stderr = stderr
	return c.Run()
}

// testTarget resolves which machine this run happens on. A nil target means
// here.
//
// Remote is the default whenever a grant exists (locked remote_by_default): the
// point of granting a host is that runs leave the laptop, and an opt-in flag
// would leave the grant unused except when someone remembered it. --local is
// the escape for a remote that is down or a tree mid-edit.
func testTarget(root, repoDir string, local bool) ([]*remote.Target, error) {
	if local {
		return nil, nil
	}
	return readRemoteGrants(root, repoDir)
}

// resolveTestTarget picks the machine a run happens on, announcing a fallback.
//
// Extracted from runTest so a multi-lane run can choose the host ONCE and then
// push the tree once — with the choice fused into the run, every lane would
// re-probe and re-sync, and a lane that landed mid-outage would be reported as
// a different kind of failure from its neighbours.
//
// A nil target means here.
func resolveTestTarget(root, repoDir string, local bool) (*remote.Target, error) {
	targets, err := testTarget(root, repoDir, local)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, nil
	}
	// BEFORE the sync, not after. Probing after the tree is pushed discovers
	// an unreachable host having already paid for the transfer, and — worse —
	// a transport failure at that point is indistinguishable from the suite
	// itself dying.
	//
	// With more than one candidate this walks them in order and takes the
	// first that answers; with one it is exactly the previous behaviour.
	chosen, pf, perr := selectRemoteTarget(targets, nil)
	if perr != nil {
		return nil, perr
	}
	if pf.Fallback {
		// Announced, never silent. A fallback the output does not mention
		// leaves a local result indistinguishable from a remote one, which is
		// the state that made `dross remote revoke` the workaround when
		// helicon was down.
		Printf("remote: %s\n", pf.Why)
		return nil, nil
	}
	return chosen, nil
}

// runTest executes one test run, here or on the granted host.
func runTest(root, repoDir, line string, local bool) error {
	target, err := resolveTestTarget(root, repoDir, local)
	if err != nil {
		return err
	}
	if target == nil {
		if err := spawnLocal(repoDir, line, os.Stdout, os.Stderr); err != nil {
			return &ExitCodeError{Code: exitSuiteFailed, Err: fmt.Errorf("test suite failed: %w", err)}
		}
		return nil
	}
	return runTestRemotely(*target, repoDir, line)
}

// runTestRemotely pushes the tree, then runs the suite over ssh.
//
// The order is the correctness property, not a sequence of steps: running
// before the sync measures whatever the remote tree happened to hold, which is
// the previous run's code.
//
// The two legs are separate functions because a multi-lane run needs them
// apart: one sync for the tree, then one ssh per lane. Fused, every lane would
// re-push an unchanged checkout, and the wall-clock cost of lanes would scale
// with the number of lanes rather than with the code they cover.
func runTestRemotely(t remote.Target, repoDir, line string) error {
	if err := syncTreeTo(t, repoDir); err != nil {
		return err
	}
	return runRemoteLine(t, line)
}

// syncTreeTo pushes the working tree to the target. The argv builder validates
// the target and returns an error INSTEAD of an argv, so an unsafe host never
// reaches a shell.
func syncTreeTo(t remote.Target, repoDir string) error {
	root, err := filepath.Abs(repoDir)
	if err != nil {
		return err
	}
	sync, cleanup, err := remote.SyncArgs(t, root)
	if err != nil {
		return err
	}
	// The exclude list is a temp file rsync reads; it must outlive the spawn
	// and not outlive this call.
	defer cleanup()
	if err := spawnRemote(sync, "", os.Stdout, os.Stderr); err != nil {
		return remoteFailure("rsync", t.Host, err)
	}
	return nil
}

// runRemoteLine runs one command line on the already-synced target.
func runRemoteLine(t remote.Target, line string) error {
	ssh, err := remote.SSHArgs(t)
	if err != nil {
		return err
	}
	// The consented line runs under a shell on the far side too, so the
	// selector and the command mean there exactly what they mean here.
	script, err := remote.Script(t, []string{"sh", "-c", line})
	if err != nil {
		return err
	}
	if err := spawnRemote(ssh, script, os.Stdout, os.Stderr); err != nil {
		return remoteFailure("ssh", t.Host, err)
	}
	return nil
}

// exitCoder is what both *exec.ExitError and a test double expose: the status
// the spawned process ended with. Matched as an interface rather than as the
// concrete type so this classification is reachable from a test without a live
// host — a rule about network failures that can only be exercised over a
// network is a rule nothing checks.
type exitCoder interface{ ExitCode() int }

// remoteFailure turns a spawn failure into a tagged error, keeping the two
// outcomes apart all the way to the exit code.
//
// They mean opposite things to whoever is deciding whether to commit. A red
// suite is information ABOUT THE CODE. An unreachable host is information about
// nothing — the run did not happen, nothing was measured, and treating it as a
// verdict is the false-green this whole seam exists to prevent.
//
// The message names which leg failed and on what host, because "command
// failed" sends the reader to the source when the answer is that a machine is
// down.
func remoteFailure(bin, host string, err error) error {
	var ec exitCoder
	if errors.As(err, &ec) {
		classified := remote.Classify(bin, host, ec.ExitCode())
		switch {
		case errors.Is(classified, remote.ErrPartial):
			// The connection held but the tree did not all arrive, so whatever
			// ran, ran against an incomplete checkout. Not a verdict either.
			return &ExitCodeError{Code: exitPartial, Err: fmt.Errorf("incomplete transfer to %s — the suite did not run against the full tree: %w", host, classified)}
		case errors.Is(classified, remote.ErrRemoteCommand):
			// The remote program spoke: this is the suite, and it is red.
			return &ExitCodeError{Code: exitSuiteFailed, Err: fmt.Errorf("test suite failed on %s: %w", host, classified)}
		default:
			return &ExitCodeError{Code: exitTransport, Err: fmt.Errorf("could not reach %s — the suite did not run: %w", host, classified)}
		}
	}
	// The local binary is missing or could not start: nothing ran on the
	// remote, which is a transport failure by any useful definition.
	return &ExitCodeError{Code: exitTransport, Err: fmt.Errorf("could not reach %s: remote %s did not start: %w: %v", host, bin, remote.ErrTransport, err)}
}

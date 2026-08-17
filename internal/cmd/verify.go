package cmd

import (
	"fmt"
	"os"
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
			t.MeasuredOn = measuredOnOf(adapters, tuning)
			// Deleted paths stay in the record — they are part of what the
			// phase did — but as a skip with an honest reason rather than as
			// an argument to a mutation tool.
			for _, f := range gone {
				t.Skipped = append(t.Skipped, verify.SkippedFile{
					File:   f,
					Reason: "file no longer exists in the working tree",
				})
			}

			// Lifecycle classification runs BEFORE tests.json is written, so
			// the persisted record carries each survivor's key and state. The
			// store is read from the repo root, never from the phase dir: an
			// acceptance recorded during one phase has to keep suppressing in
			// the next one, which is the whole point of c-3.
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
		},
	}
	c.Flags().BoolVar(&skipMutation, "skip-mutation", false,
		"do not run mutation tests (record what would have been mutated, skip execution)")
	c.AddCommand(verifyFinalize())
	return c
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
	target, pf, perr := selectRemoteTarget(targets, nil)
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
		mt.FellBackFrom, mt.FallbackWhy = targets[len(targets)-1].Host, pf.Why
		return mt, nil
	}
	target.Cores = pf.Ready.Cores
	mt.Target = target
	return mt, nil
}

// measuredOnFromAdapters reads a run's provenance off the adapters it is about
// to use, rather than off the grant on disk.
//
// The distinction is the whole point of the field. A grant answers "is a host
// authorized", which is not the same question as "where did these numbers come
// from": a run can hold a grant it was told to ignore, or one it could not
// reach. Only the adapters know which machine they were pointed at, so they are
// what gets asked.
//
// A skipped mutation leg has no adapters at all and reports local — nothing ran
// anywhere, and claiming a host would be worse than saying nothing.
func measuredOnFromAdapters(adapters []mutation.Adapter) string {
	for _, a := range adapters {
		var target *remote.Target
		switch v := a.(type) {
		case *mutation.Gremlins:
			target = v.Remote
		case *mutation.Stryker:
			target = v.Remote
		case *mutation.StrykerNet:
			target = v.Remote
		}
		if target != nil {
			return verify.MeasuredOnHost(target.Host)
		}
	}
	return verify.MeasuredLocally()
}

// measuredOnOf resolves a run's provenance from the adapters it used and the
// tuning that produced them.
//
// A fallback is the case the adapters alone cannot express: they are local, so
// measuredOnFromAdapters would call it a plain local run and lose the fact that
// a remote measurement was expected and did not happen. The tuning is the only
// thing that still remembers.
func measuredOnOf(adapters []mutation.Adapter, mt mutationTuning) string {
	if mt.FellBackFrom != "" {
		return verify.MeasuredAfterFallback(mt.FellBackFrom, mt.FallbackWhy)
	}
	return measuredOnFromAdapters(adapters)
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

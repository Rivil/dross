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

			adapters := configuredAdaptersFn(proj, root, skipMutation)
			t, err := verify.RunScoped(phaseID, files, adapters, scope)
			if err != nil {
				return err
			}
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
			printLifecycleSummary(lc)
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

// configuredAdapters returns the list of mutation adapters appropriate
// for the project, with runtime prefixes applied (docker compose exec ...).
func configuredAdapters(p *project.Project, _ string, skip bool) []mutation.Adapter {
	if skip {
		return nil // verify still runs — files end up in Skipped
	}
	prefix := dockerPrefix(p)
	// Project root for stryker is the runtime's cwd — host cwd for native,
	// or the host cwd for docker (we read the report via bind-mounted fs).
	// If docker volume layout diverges, this is where we'd surface config.
	cwd, _ := os.Getwd()
	all := []mutation.Adapter{
		&mutation.Stryker{Prefix: prefix, ProjectRoot: cwd, Workdir: p.Mutation.Stryker.Workdir},
		&mutation.Gremlins{
			Prefix:             prefix,
			ProjectRoot:        cwd,
			TimeoutCoefficient: p.Mutation.Gremlins.TimeoutCoefficient,
		},
		&mutation.StrykerNet{Prefix: prefix, ProjectRoot: cwd},
	}
	if len(p.Mutation.Adapters) == 0 {
		return all
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
	return out
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
		for _, lr := range t.Languages {
			files += len(lr.Files)
			if lr.Mutation != nil {
				killed += lr.Mutation.Killed
				survived += lr.Mutation.Survived
			}
		}
		counts["files"] = files
		counts["mutants_killed"] = killed
		counts["mutants_survived"] = survived
		// Both counts are already post-filter — Tests carries the in-scope
		// reports — so the score below is the in-scope score, the same
		// fraction verify.toml reports. They agree exactly on a single-leg,
		// zero-timeout run; wider than that the two sides use different
		// conventions (verify.toml means across legs, this pools; Report.Score
		// puts timeouts in the denominator, this does not). Reconciling them
		// changes the number every phase's thresholds apply to and is
		// deliberately out of this phase's diff.
		counts["mutants_in_scope"] = v.Summary.MutantsInScope
		counts["out_of_scope"] = len(t.OutOfScope)
		if total := killed + survived; total > 0 {
			nums["mutation_score"] = float64(killed) / float64(total)
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
func printLifecycleSummary(lc *verify.Lifecycle) {
	if lc == nil || lc.Total() == 0 {
		return
	}
	Printf("  survivors: %d in-diff, %d routed, %d accepted, %d unclassified\n",
		len(lc.InDiff), len(lc.Routed), len(lc.Accepted), len(lc.Unclassified))
	if n := len(lc.Unclassified); n > 0 {
		Printf("    ↳ %d unclassified — `dross survivor accept <file>:<line> --op OP --reason ...` "+
			"or `dross survivor route <file>:<line> --op OP --target <phase>`\n", n)
	}
}

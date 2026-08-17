// Package verify aggregates per-language mutation reports into
// .dross/phases/<id>/tests.json and writes a verdict skeleton to
// verify.toml. The criterion-to-test mapping (the actual judgement
// of "does this test cover this acceptance criterion?") is filled
// in by the LLM via /dross-verify; the Go side handles the
// mechanical aggregation only.
package verify

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/Rivil/dross/internal/mutation"
	"github.com/Rivil/dross/internal/remote"
)

const (
	TestsFile  = "tests.json"
	VerifyFile = "verify.toml"
)

// MutationStatus values describe whether the phase's changes could be
// mutation-tested at all. Distinct from the score so a zero score on an
// unmeasurable phase doesn't trip the < 0.60 fail threshold.
const (
	// MutationMeasured — adapter ran and instrumented at least one mutant.
	// Score is meaningful, apply thresholds.
	MutationMeasured = "measured"
	// MutationUnmeasurable — adapter ran but instrumented zero mutants
	// (e.g. project Stryker scope excludes all touched files, or the
	// changes are purely non-code). Score is 0/0; don't apply thresholds.
	MutationUnmeasurable = "unmeasurable"
	// MutationSkipped — no adapter ran. Either `--skip-mutation` was
	// passed, or none of the touched files matched any configured adapter.
	MutationSkipped = "skipped"
	// MutationOutOfScope — adapters ran and found mutants, but every one of
	// them landed in a file this phase never touched. Distinct from
	// unmeasurable (nothing to measure) and from measured-at-0.00 (a real
	// score): the tool worked, the phase simply owns none of the result.
	// Collapsing it into either of those is what makes a vacuous pass look
	// like a clean one.
	MutationOutOfScope = "out-of-scope"
)

// Tests is the machine-written aggregation of mutation/coverage results.
type Tests struct {
	Phase       string        `json:"phase"`
	GeneratedAt time.Time     `json:"generated_at"`
	Languages   []LanguageRun `json:"languages"`
	Skipped     []SkippedFile `json:"skipped,omitempty"`

	// OutOfScope collects every survivor filtered out for landing in a file
	// this phase never touched, across all legs. Top-level rather than
	// per-language because it is read as one list; each entry carries its own
	// language so a merged read still says which adapter produced it.
	OutOfScope []OutOfScopeMutant `json:"out_of_scope,omitempty"`

	// Scope records exactly what the run scoped to: the file set, the
	// resolved base, which sources contributed, and any reason the view was
	// incomplete. Persisted so a mis-scoped run is diagnosable after the fact
	// — without it, a scope that silently narrowed is indistinguishable from
	// a phase that genuinely had little to measure.
	Scope *Scope `json:"scope,omitempty"`

	// MeasuredOn names the machine the mutation run executed on. See
	// MeasuredLocally / MeasuredOnHost / MeasuredAfterFallback for the shapes
	// it takes; Skeleton copies it onto the summary so it survives into
	// verify.toml. Empty on a record written before the field existed.
	MeasuredOn string `json:"measured_on,omitempty"`
}

// The measurement-provenance strings live here, in one place, because they are
// read by a human comparing two runs and written by a caller that knows only
// the target. A caller composing its own phrasing would drift from the one the
// last run wrote, and the field's whole job is to make two runs comparable.
const measuredLocal = "local"

// MeasuredLocally is the provenance of a run that never involved a host.
func MeasuredLocally() string { return measuredLocal }

// MeasuredOnHost is the provenance of a run that executed on host.
func MeasuredOnHost(host string) string {
	if strings.TrimSpace(host) == "" {
		return measuredLocal
	}
	return host
}

// MeasuredAfterFallback is the provenance of a run that meant to use host,
// could not reach it, and measured here instead.
//
// It names BOTH machines deliberately. Recording it as a plain local run would
// lose the fact that a remote measurement was expected and did not happen,
// which is the state board-task-mirror hit: helicon was down for hours, the
// workaround was to revoke the grant, and nothing in the resulting numbers said
// they came from a different machine than the run before.
func MeasuredAfterFallback(host, why string) string {
	if strings.TrimSpace(host) == "" {
		return measuredLocal
	}
	s := measuredLocal + " (fell back from " + host
	if w := strings.TrimSpace(why); w != "" {
		s += ": " + w
	}
	return s + ")"
}

type LanguageRun struct {
	Name     string           `json:"name"` // "typescript" | "go" | ...
	Tool     string           `json:"tool"` // "stryker" | "gremlins" | ...
	Files    []string         `json:"files"`
	Mutation *mutation.Report `json:"mutation,omitempty"`
	// Error records an adapter failure for this language leg. One broken
	// adapter must not discard the other legs' finished reports — the run
	// is recorded with Mutation nil and the error text, and Skeleton
	// surfaces it as a FLAG finding.
	Error string `json:"error,omitempty"`
	// RemoteTransport marks an Error that was the remote never running at all —
	// an unreachable host, a failed sync, a dead connection.
	//
	// It is captured HERE, at the moment the error value is still live, because
	// Error is a string and errors.Is cannot be re-run against a string. The
	// distinction is worth carrying: a misconfigured stryker is a FLAG someone
	// reads and fixes, while a leg that never ran means this phase has no
	// evidence about that language at all — which must not be verifiable past.
	RemoteTransport bool `json:"remote_transport,omitempty"`
}

type SkippedFile struct {
	File   string `json:"file"`
	Reason string `json:"reason"` // why no adapter ran on it
}

// OutOfScopeMutant is a survivor the tool found in a file this phase never
// touched. It is filtered out of the score and out of the phase's findings —
// but kept, in full, so the filtering is auditable and a later survivor-
// lifecycle pass can route it somewhere. Discarding them would trade one
// silent mis-attribution for another.
// The lifecycle fields mirror mutation.Mutant's, so an out-of-scope survivor
// carries the same state as an in-scope one — c-7's "same lifecycle" is a
// property of the data, not of a report-time special case. Their json tags are
// lowercase here and absent (hence capitalised) on mutation.Mutant; that split
// is pre-existing on these two types and is pinned by tests at both ends rather
// than left to drift.
type OutOfScopeMutant struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Op       string `json:"op,omitempty"`
	Language string `json:"language,omitempty"`

	// Key is the survivor's cross-run identity (internal/survivor).
	Key string `json:"key,omitempty"`
	// Lifecycle is one of the four states; see lifecycle.go.
	Lifecycle string `json:"lifecycle,omitempty"`
	// Note explains a state that needs explaining — destination, ambiguity,
	// or the reason identity could not be resolved.
	Note string `json:"note,omitempty"`
}

// FilterReport splits a mutation report against the phase's scope: the mutants
// in files the phase touched, and the survivors in files it didn't.
//
// The counts and the score are recomputed from the in-scope per-file rows
// alone. Pruning only the Surviving slice would look right in the findings and
// still leave a neighbour's kills inflating the numerator — the failure mode
// where a phase passes on code it never wrote.
//
// Score follows mutation.Report's live convention, killed/(killed+survived+
// timeout). The struct doc at adapter.go's Score field claims
// killed/(killed+survived) and is simply wrong; a recomputation written from
// that comment scores a timeout-carrying report too high.
//
// A nil scope means scoping is not configured and the report passes through
// untouched — the seam that lets this land before its call site is wired up.
// Scoping being unconditional is enforced where verify is invoked, not here.
//
// r is never mutated; the kept report is a new value.
func FilterReport(r *mutation.Report, s *Scope, language string) (*mutation.Report, []OutOfScopeMutant) {
	if r == nil || s == nil {
		return r, nil
	}

	kept := &mutation.Report{Tool: r.Tool}
	// Counts come from the per-file table, not the aggregates: it is the only
	// view that can be split by file. An adapter that populates aggregates but
	// no rows therefore contributes nothing to the score — visibly zero rather
	// than silently whole-package. mutation's drift tests keep adapters honest.
	for file, st := range r.Files {
		if IsTestdataPath(file) || !s.Contains(file) {
			continue
		}
		kept.Killed += st.Killed
		kept.Survived += st.Survived
		kept.Timeout += st.Timeout
		kept.Errors += st.Errors
		kept.NotCovered += st.NotCovered
		if kept.Files == nil {
			kept.Files = map[string]mutation.FileStat{}
		}
		// Keys are already unique in the source map, so this copies rather
		// than accumulates — and it keeps the tool's own spelling of the path
		// rather than the scope's normalised form, so the kept report still
		// reads as the tool wrote it.
		kept.Files[file] = st
	}
	if denom := kept.Killed + kept.Survived + kept.Timeout; denom > 0 {
		kept.Score = float64(kept.Killed) / float64(denom)
	}

	var dropped []OutOfScopeMutant
	for _, m := range r.Surviving {
		// A testdata fixture is nobody's debt, so it leaves through neither
		// exit: not kept (it is not this phase's to answer for) and not
		// dropped (an OutOfScopeMutant is backlog — it resurfaces as an
		// unclassified FLAG for someone to accept or route, and there is
		// nothing to accept). `dross survivor drain` already skips these at
		// readRawReport; the two commands must read the same repo state the
		// same way, which is the whole reason IsTestdataPath is shared.
		if IsTestdataPath(m.File) {
			continue
		}
		if !s.Contains(m.File) {
			dropped = append(dropped, OutOfScopeMutant{
				File:     m.File,
				Line:     m.Line,
				Op:       m.Op,
				Language: language,
			})
			continue
		}
		// Every kept survivor carries how it relates to the diff. Both tags
		// stay in the denominator and in Surviving — the tag weights the
		// evidence, it does not gate (the scope_granularity lock).
		m.Origin = s.Origin(m.File, m.Line)
		kept.Surviving = append(kept.Surviving, m)
	}
	return kept, dropped
}

// IsTestdataPath reports whether a path lies under a testdata/ directory.
//
// Gremlins walks the package DIRECTORY, not the Go package, so a fixture under
// testdata/ lands in its parent's report — and it is always reported NOT
// COVERED there, because the parent's tests do not run it. That is not debt: Go
// excludes testdata from `./...` by construction, the code is never compiled
// into the binary, and a fixture exists to be measured by its own recorded run
// rather than covered by its neighbour's tests.
//
// This is a SCOPE rule — which files a run answers for — not a mutant-class
// filter. The adapter still emits these mutants; filtering mutants out of the
// report itself remains mutation-score-truth's contract to change, not this
// one's.
//
// It lives here rather than beside either caller because it is the ONE
// implementation: `dross survivor drain` and `dross verify` classified the same
// repo state differently while each owned a copy (the drain read 0 unclassified
// where verify read 24, all of them this repo's own ceiling fixture). A second
// copy is the bug, not a convenience.
//
// The match is on a path SEGMENT: "internal/testdatabase/store.go" and
// "internal/mytestdata.go" are ordinary code and stay in scope.
func IsTestdataPath(file string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(file), "/") {
		if seg == "testdata" {
			return true
		}
	}
	return false
}

// Verify is the human-readable + LLM-mappable verdict.
type Verify struct {
	Verify   VerifyMeta        `toml:"verify"`
	Summary  VerifySummary     `toml:"summary"`
	Criteria []CriterionResult `toml:"criterion,omitempty"`
	Findings []Finding         `toml:"finding,omitempty"`
}

type VerifyMeta struct {
	Phase       string    `toml:"phase"`
	GeneratedAt time.Time `toml:"generated_at"`
	Verdict     string    `toml:"verdict"` // pass | fail | partial | pending
	// Finalized marks that the resolved verdict has been recorded as a
	// telemetry outcome event (by `dross verify finalize` or a
	// downstream gate's auto-heal). It makes finalization idempotent
	// without scanning the telemetry log — which may be opted out of or
	// rotated away.
	Finalized   bool      `toml:"finalized,omitempty"`
	FinalizedAt time.Time `toml:"finalized_at,omitempty"`
}

type VerifySummary struct {
	// MutationStatus is measured | unmeasurable | skipped. Read this
	// before the score: when status != measured the score is a 0/0
	// artifact, not a signal, and the verdict must be derived from
	// criterion coverage alone.
	MutationStatus string `toml:"mutation_status"`
	// MeasuredOn names the machine that produced these numbers — a host for a
	// remote run, "local" for one that ran here, and both when a run meant for
	// a host fell back. Omitted when empty so a verify.toml written before the
	// field round-trips unchanged.
	//
	// Without it a local score and a remote one are indistinguishable after the
	// fact, and the two are not interchangeable evidence: they run different
	// toolchain versions on different core counts.
	MeasuredOn      string  `toml:"measured_on,omitempty"`
	MutationScore   float64 `toml:"mutation_score"`
	MutantsKilled   int     `toml:"mutants_killed"`
	MutantsSurvived int     `toml:"mutants_survived"`
	// MutantsNotCovered is a subset of MutantsSurvived: mutants the test
	// suite never even executed. Surfaced for /dross-verify so the LLM
	// can distinguish weak assertions ("test ran, didn't catch") from
	// coverage blind spots ("test never ran the line"). Omitted when
	// zero — only gremlins currently reports this status.
	MutantsNotCovered int `toml:"mutants_not_covered,omitempty"`
	// MutantsInScope is the denominator the score was computed over:
	// killed + survived + timeout, counting only mutants in files this phase
	// touched. Always written, including as 0, because it is the sample size
	// — a 0.50 over 2 mutants and a 0.50 over 200 are the same number and not
	// the same evidence. It is the signal the small_denominator_gate lock
	// requires in place of moving the threshold.
	MutantsInScope int `toml:"mutants_in_scope"`
	// UnclassifiedInScope counts in-scope survivors carrying no disposition:
	// neither accepted with a reason nor routed to a destination. It is the
	// mutation leg's fail lever, in place of the score.
	//
	// A ratio cannot express "this phase left debt behind" — a phase that adds
	// 200 killed mutants can bury a live one and still clear any cutoff, and
	// the cutoff itself re-opens the arbitrary-number argument every phase. The
	// absolute threshold is zero, with no tolerance band (the locked
	// unclassified_gate decision): the score stays reported as evidence, but a
	// single unclassified survivor inside the phase's own diff fails it.
	//
	// Out-of-scope survivors are deliberately excluded — counting the standing
	// backlog here would fail every phase in the repo for debt it did not
	// create. They stay individually FLAGged for draining.
	UnclassifiedInScope int `toml:"unclassified_in_scope"`
	CriteriaTotal       int `toml:"criteria_total"`
	CriteriaCovered     int `toml:"criteria_covered"`
	CriteriaUncovered   int `toml:"criteria_uncovered"`
}

type CriterionResult struct {
	ID     string   `toml:"id"`
	Status string   `toml:"status"` // covered | weak | uncovered | unknown
	Tests  []string `toml:"tests,omitempty"`
	Notes  string   `toml:"notes,omitempty"`
}

type Finding struct {
	Severity string `toml:"severity"` // BLOCKING | FLAG | NOTE
	Text     string `toml:"text"`
}

// FilePaths returns canonical paths for tests.json and verify.toml.
func FilePaths(root, phaseID string) (tests, verify string) {
	dir := filepath.Join(root, "phases", phaseID)
	return filepath.Join(dir, TestsFile), filepath.Join(dir, VerifyFile)
}

// Run executes the configured adapters against the given files, grouped
// by language, and returns the aggregated Tests struct. It does NOT
// write to disk — caller decides when/where to persist.
//
// adapters is a list of mutation.Adapter implementations; the first
// matching adapter (by Supports()) is used for each file. Files with
// no matching adapter end up in Skipped.
func Run(phaseID string, files []string, adapters []mutation.Adapter) (*Tests, error) {
	return RunScoped(phaseID, files, adapters, nil)
}

// RunScoped is Run with diff scoping applied to each leg's report. A nil scope
// is the unscoped behaviour, which is what Run passes.
//
// Filtering happens AFTER each adapter returns. What the adapter was
// dispatched to mutate is not narrowed here — narrowing the dispatch would
// change which mutants exist, and this is only about which of them this phase
// is answerable for.
func RunScoped(phaseID string, files []string, adapters []mutation.Adapter, scope *Scope) (*Tests, error) {
	t := &Tests{
		Phase:       phaseID,
		GeneratedAt: time.Now().UTC(),
		Scope:       scope,
	}

	// Group files by adapter.
	byAdapter := map[string][]string{}
	adapterByName := map[string]mutation.Adapter{}
	for _, f := range files {
		a := mutation.Dispatch(f, adapters)
		if a == nil {
			t.Skipped = append(t.Skipped, SkippedFile{
				File:   f,
				Reason: "no mutation adapter for " + filepath.Ext(f),
			})
			continue
		}
		byAdapter[a.Name()] = append(byAdapter[a.Name()], f)
		adapterByName[a.Name()] = a
	}

	// Stable order for output.
	names := make([]string, 0, len(byAdapter))
	for n := range byAdapter {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		a := adapterByName[name]
		report, err := a.Run(byAdapter[name])
		if err != nil {
			// Record-and-continue: adapters run in sorted-name order, so a
			// failing early adapter (e.g. stryker misconfigured) must not
			// throw away a finished gremlins report.
			t.Languages = append(t.Languages, LanguageRun{
				Name:  adapterLanguage(name),
				Tool:  name,
				Files: byAdapter[name],
				Error: err.Error(),
				// Classified while the error VALUE is still live. Everything
				// downstream sees only Error, a string, and errors.Is cannot be
				// re-run against prose.
				RemoteTransport: errors.Is(err, remote.ErrTransport),
			})
			continue
		}
		language := adapterLanguage(name)
		kept, dropped := FilterReport(report, scope, language)
		t.OutOfScope = append(t.OutOfScope, dropped...)
		t.Languages = append(t.Languages, LanguageRun{
			Name:     language,
			Tool:     name,
			Files:    byAdapter[name],
			Mutation: kept,
		})
	}

	return t, nil
}

// adapterLanguage maps adapter names to the language label users see.
func adapterLanguage(name string) string {
	switch name {
	case "stryker":
		return "typescript"
	case "stryker.net":
		return "csharp"
	case "gremlins":
		return "go"
	}
	return name
}

// Save writes Tests as JSON, creating parent dir if needed.
func (t *Tests) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// LoadTests reads tests.json. Missing file = nil, no error (verify
// hasn't run yet).
func LoadTests(path string) (*Tests, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var t Tests
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// Save writes verify.toml.
func (v *Verify) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := toml.NewEncoder(f)
	enc.Indent = "  "
	return enc.Encode(v)
}

func LoadVerify(path string) (*Verify, error) {
	var v Verify
	_, err := toml.DecodeFile(path, &v)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// Skeleton builds a verify.toml seeded from machine results, leaving
// the criterion-to-test mapping for the LLM to fill in. Verdict is
// "pending" until the LLM marks it.
func Skeleton(t *Tests, criteriaIDs []string) *Verify {
	v := &Verify{
		Verify: VerifyMeta{
			Phase:       t.Phase,
			GeneratedAt: t.GeneratedAt,
			Verdict:     "pending",
		},
		Summary: VerifySummary{
			CriteriaTotal:  len(criteriaIDs),
			MutationStatus: MutationSkipped, // upgraded below if any adapter ran
			// Carried through from the run rather than re-derived from config:
			// what matters is where the numbers actually came from, which a
			// grant on disk does not answer (a --local run has one and ignores
			// it, and a fallback has one it could not use).
			MeasuredOn: t.MeasuredOn,
		},
	}
	// Totals across legs, POOLED — the score is computed once from them rather
	// than averaged leg by leg. A mean over legs of unequal size hands the
	// smaller leg the louder vote: 1/1 beside 0/9 is a suite that killed one
	// mutant in ten, and the mean called it 0.50.
	var timeouts int
	for _, lr := range t.Languages {
		if lr.Mutation == nil {
			continue
		}
		v.Summary.MutantsKilled += lr.Mutation.Killed
		v.Summary.MutantsSurvived += lr.Mutation.Survived
		v.Summary.MutantsNotCovered += lr.Mutation.NotCovered
		timeouts += lr.Mutation.Timeout
		// Any adapter that produced a report bumps us off `skipped`. We
		// downgrade to `unmeasurable` until we see a non-zero mutant
		// count, then we promote to `measured`.
		if v.Summary.MutationStatus == MutationSkipped {
			v.Summary.MutationStatus = MutationUnmeasurable
		}
		inScope := lr.Mutation.Killed + lr.Mutation.Survived + lr.Mutation.Timeout
		v.Summary.MutantsInScope += inScope
		if inScope > 0 {
			v.Summary.MutationStatus = MutationMeasured
		}
	}
	// Every mutant the tools produced landed outside this phase's files. The
	// adapters worked and found plenty; none of it is this phase's to answer
	// for. Reporting that as unmeasurable would claim there was nothing to
	// measure, and as measured-0.00 would fail the phase for a neighbour's
	// debt — both read as a settled result where the honest answer is that
	// this run measured nothing about these changes.
	v.Summary.MutationScore = mutation.PooledScore(v.Summary.MutantsKilled, v.Summary.MutantsSurvived, timeouts)
	if v.Summary.MutantsInScope == 0 && len(t.OutOfScope) > 0 {
		v.Summary.MutationStatus = MutationOutOfScope
	}
	for _, id := range criteriaIDs {
		v.Criteria = append(v.Criteria, CriterionResult{
			ID:     id,
			Status: "unknown", // LLM fills this in
		})
	}
	for _, lr := range t.Languages {
		if lr.Error != "" {
			// A leg whose REMOTE never ran is BLOCKING, not a FLAG. The two
			// look the same from here — an adapter returned an error — but they
			// mean opposite things about what this run knows. A misconfigured
			// stryker is a problem with the tool, and its FLAG is read and
			// fixed. An unreachable mutation host means nothing was measured
			// for that language at all: there are no survivors to gate on
			// precisely because nothing looked, and a phase must not be
			// verifiable past a leg that never ran.
			//
			// lr.Error is carried into the text whole because it already names
			// the host — every transport error from internal/remote does.
			severity := "FLAG"
			if lr.RemoteTransport {
				severity = "BLOCKING"
			}
			v.Findings = append(v.Findings, Finding{
				Severity: severity,
				Text:     fmt.Sprintf("mutation adapter %s failed: %s", lr.Tool, lr.Error),
			})
		}
		if lr.Mutation == nil {
			continue
		}
		for _, m := range lr.Mutation.Surviving {
			// An accepted survivor is the only one that earns silence. A
			// routed one keeps a NOTE naming where it went — debt with a
			// home stays visible.
			//
			// Everything else is an IN-SCOPE survivor with no disposition:
			// this phase's own diff, no reason, nowhere to go. That is
			// BLOCKING, not a FLAG, and it is counted — including a survivor
			// whose identity failed to resolve, which must not be able to fall
			// out of the count by being unreadable, and one with no state at
			// all (a run from before lifecycle classification).
			switch m.Lifecycle {
			case LifecycleAccepted:
				continue
			case LifecycleRouted:
				v.Findings = append(v.Findings, Finding{
					Severity: "NOTE",
					Text: fmt.Sprintf("%s mutant survived: %s:%d (%s) — %s",
						lr.Tool, m.File, m.Line, m.Op, m.Note),
				})
				continue
			}
			v.Summary.UnclassifiedInScope++
			text := fmt.Sprintf("%s mutant survived: %s:%d (%s)",
				lr.Tool, m.File, m.Line, m.Op)
			if m.Note != "" {
				text += " — " + m.Note
			}
			v.Findings = append(v.Findings, Finding{Severity: "BLOCKING", Text: text})
		}
	}
	for _, s := range t.Skipped {
		v.Findings = append(v.Findings, Finding{
			Severity: "NOTE",
			Text:     fmt.Sprintf("skipped %s: %s", s.File, s.Reason),
		})
	}
	// Out-of-scope survivors get the same lifecycle as in-scope ones (c-7).
	// An accepted one is silent, a routed one is a NOTE naming its
	// destination, and one with no state is an individually-actionable FLAG:
	// there are exactly two ways to clear it, and naming both is what turns a
	// write-only audit list into a backlog that drains.
	remainder := 0
	for _, o := range t.OutOfScope {
		switch o.Lifecycle {
		case LifecycleAccepted:
			continue
		case LifecycleRouted:
			remainder++
			v.Findings = append(v.Findings, Finding{
				Severity: "NOTE",
				Text: fmt.Sprintf("out-of-scope mutant survived: %s:%d (%s) — %s",
					o.File, o.Line, o.Op, o.Note),
			})
			continue
		case LifecycleUnclassified:
			remainder++
			v.Findings = append(v.Findings, Finding{
				Severity: "FLAG",
				Text: fmt.Sprintf("unclassified out-of-scope survivor: %s:%d (%s) — accept it with "+
					"`dross survivor accept` or route it with `dross survivor route --target`",
					o.File, o.Line, o.Op),
			})
			continue
		default:
			// No classification ran (pre-lifecycle run, or identity
			// unavailable): keep the pre-phase behaviour of counting it into
			// the one-line summary rather than inventing a state for it.
			remainder++
		}
	}
	// ONE line for the whole remaining filtered set. The count is the
	// post-lifecycle remainder — accepted survivors are gone from it, which is
	// what makes the number shrink as the backlog is drained instead of
	// standing still forever.
	if remainder > 0 {
		v.Findings = append(v.Findings, Finding{
			Severity: "NOTE",
			Text: fmt.Sprintf("filtered %d out-of-scope survivor(s) in files this phase did not touch "+
				"— listed under `out_of_scope` in tests.json, not counted in the score", remainder),
		})
	}
	// A degraded scope is a NOTE, never a FLAG: the run may have measured
	// less than it should have, but a missing base or an unrecorded task is a
	// bookkeeping gap, and failing a phase for one would punish the wrong
	// thing. Recording it is what keeps a narrowed run from reading as clean.
	if t.Scope != nil && len(t.Scope.Degraded) > 0 {
		v.Findings = append(v.Findings, Finding{
			Severity: "NOTE",
			Text: fmt.Sprintf("mutation scope is degraded (source: %s) — %s",
				t.Scope.Source, strings.Join(t.Scope.Degraded, "; ")),
		})
	}
	return v
}

// FilesFromChanges flattens changes.json's per-task file lists into
// a deduped, sorted slice. Used by `dross verify` to know what to
// mutation-test.
func FilesFromChanges(filesByTask map[string][]string) []string {
	seen := map[string]bool{}
	for _, fs := range filesByTask {
		for _, f := range fs {
			seen[f] = true
		}
	}
	out := make([]string, 0, len(seen))
	for f := range seen {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// SplitFiles separates files Stryker handles from "other" files
// (HTML/CSS/etc). Useful for the prompt to surface what was
// mutation-tested vs what was just snapshot-checked.
func SplitFiles(files []string) (mutable, snapshot []string) {
	for _, f := range files {
		ext := strings.ToLower(filepath.Ext(f))
		switch ext {
		case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".svelte",
			".go", ".cs":
			mutable = append(mutable, f)
		default:
			snapshot = append(snapshot, f)
		}
	}
	return
}

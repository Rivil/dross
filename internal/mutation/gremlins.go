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
}

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

	pkgs := packagesFromFiles(files)

	reportDir := filepath.Join(g.ProjectRoot, "reports", "gremlins")
	// Gremlins won't create parent dirs for --output; it errors on the
	// first mutant write and leaves no report behind. Mkdir up front so
	// every invocation has somewhere to land.
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return nil, fmt.Errorf("prepare gremlins report dir: %w", err)
	}

	merged := &Report{Tool: g.Name()}
	var unmeasured []string

	for _, pkg := range pkgs {
		reportRel := filepath.Join("reports", "gremlins", sanitizePkg(pkg)+".json")
		reportAbs := filepath.Join(g.ProjectRoot, reportRel)
		// A stale report from a prior run must not be re-read if gremlins
		// writes nothing this time.
		_ = os.Remove(reportAbs)

		args := g.buildUnleashArgs(reportRel, []string{pkg})
		cmd := g.buildCmd(args)
		cmd.Stdout = os.Stderr // streamed; not captured (long-running)
		cmd.Stderr = os.Stderr

		// Echo the exact invocation before running — cheap diagnostic for
		// copy-paste re-runs.
		invocation := strings.Join(cmd.Args, " ")
		fmt.Fprintf(os.Stderr, "gremlins: %s\n", invocation)

		if err := cmd.Run(); err != nil {
			// Gremlins exits non-zero when threshold flags fail or surviving
			// mutants exist — both are "ran, bad results"; read the report
			// regardless. Only a failure to START the process (binary not
			// found, etc.) is fatal.
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				return nil, fmt.Errorf("gremlins invocation failed: %w\n  invocation: %s\n  (is gremlins installed? `go install github.com/go-gremlins/gremlins/cmd/gremlins@latest`)", err, invocation)
			}
		}

		b, err := os.ReadFile(reportAbs)
		if err != nil {
			// No report — gremlins gathered no covered mutants for this
			// package and exited without writing. Exclude, don't fail.
			unmeasured = append(unmeasured, pkg+" (no report — gremlins gathered no covered mutants)")
			continue
		}
		rep, err := ParseGremlinsJSON(b)
		if err != nil {
			unmeasured = append(unmeasured, pkg+" (unreadable report: "+err.Error()+")")
			continue
		}
		if !hasCoverage(rep) {
			// Report exists but every mutant is NOT COVERED — gremlins
			// instrumented zero usable coverage here (a coverage-tool blind
			// spot, not a test-quality signal). Exclude from the score.
			unmeasured = append(unmeasured, pkg+" (zero covered mutants — coverage blind spot)")
			continue
		}
		mergeInto(merged, rep)
	}

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
	denom := dst.Killed + dst.Survived + dst.Timeout
	if denom > 0 {
		dst.Score = float64(dst.Killed) / float64(denom)
	}
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
func (g *Gremlins) buildUnleashArgs(reportRel string, pkgs []string) []string {
	coef := g.TimeoutCoefficient
	if coef <= 0 {
		coef = DefaultTimeoutCoefficient
	}
	workers := g.Workers
	if workers <= 0 {
		workers = defaultWorkers()
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
	return append(args, pkgs...)
}

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
func packagesFromFiles(files []string) []string {
	if len(files) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var pkgs []string
	for _, f := range files {
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
			case "LIVED", "NOT COVERED":
				r.Survived++
				if m.Status == "NOT COVERED" {
					r.NotCovered++
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
			case "NOT VIABLE", "SKIPPED", "RUNNABLE", "":
				// not counted
			default:
				// future statuses — surface as errors so they're visible
				r.Errors++
			}
		}
	}
	denom := r.Killed + r.Survived + r.Timeout
	if denom > 0 {
		r.Score = float64(r.Killed) / float64(denom)
	}
	return r, nil
}

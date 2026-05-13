package mutation

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
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
}

func (g *Gremlins) Name() string { return "gremlins" }

func (g *Gremlins) Supports(file string) bool {
	return strings.HasSuffix(strings.ToLower(file), ".go")
}

// Run invokes gremlins on the packages containing the given files,
// then parses the JSON report.
func (g *Gremlins) Run(files []string) (*Report, error) {
	if len(files) == 0 {
		return &Report{Tool: g.Name()}, nil
	}

	// Derive unique package paths from files. Gremlins takes packages,
	// not files. e.g. ["internal/api/tags.go", "internal/db/users.go"]
	// → ["./internal/api/...", "./internal/db/..."]
	pkgs := packagesFromFiles(files)

	reportRel := filepath.Join("reports", "gremlins", "output.json")
	reportAbs := filepath.Join(g.ProjectRoot, reportRel)

	// Gremlins won't create parent dirs for --output; it errors on the
	// first mutant write and leaves no report behind. Mkdir up front so
	// every invocation has somewhere to land.
	if err := os.MkdirAll(filepath.Dir(reportAbs), 0o755); err != nil {
		return nil, fmt.Errorf("prepare gremlins report dir: %w", err)
	}

	args := g.buildUnleashArgs(reportRel, pkgs)
	cmd := g.buildCmd(args)
	cmd.Stdout = os.Stderr // streamed; not captured (long-running)
	cmd.Stderr = os.Stderr

	// Echo the exact invocation before running. Cheap diagnostic — when
	// gremlins finishes without writing output (path-scope mismatch,
	// zero covered mutants, threshold gates, etc.) the user can copy
	// this line and re-run manually to see what happened.
	invocation := strings.Join(cmd.Args, " ")
	fmt.Fprintf(os.Stderr, "gremlins: %s\n", invocation)

	if err := cmd.Run(); err != nil {
		// Gremlins exits non-zero when threshold flags fail or surviving
		// mutants exist. Both are "ran successfully with bad results"
		// outcomes — try to read the report regardless.
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return nil, fmt.Errorf("gremlins invocation failed: %w\n  invocation: %s\n  (is gremlins installed? `go install github.com/go-gremlins/gremlins/cmd/gremlins@latest`)", err, invocation)
		}
	}

	b, err := os.ReadFile(reportAbs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("gremlins did not write a report at %s\n  invocation: %s\n  likely causes: (a) the path scope contains no covered mutants — try a narrower per-package path; (b) gremlins hit an internal error — re-run the invocation manually to see its output; (c) gremlins exited before writing output", reportAbs, invocation)
		}
		return nil, fmt.Errorf("read gremlins report: %w (invocation: %s)", err, invocation)
	}
	return ParseGremlinsJSON(b)
}

// buildUnleashArgs assembles the gremlins invocation for the configured
// timeout coefficient and package set. Extracted from Run() so the flag
// shape is unit-testable without shelling out.
func (g *Gremlins) buildUnleashArgs(reportRel string, pkgs []string) []string {
	coef := g.TimeoutCoefficient
	if coef <= 0 {
		coef = DefaultTimeoutCoefficient
	}
	args := []string{
		"gremlins", "unleash",
		"--output", reportRel,
		"--timeout-coefficient", strconv.Itoa(coef),
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

// packagesFromFiles derives a single gremlins-compatible package path
// from the list of source files touched in this phase. Gremlins accepts
// exactly one positional path arg (`gremlins unleash [path]`), so when
// files span multiple packages we walk up to the deepest shared
// directory and pass `./<ancestor>/...` so gremlins recurses into every
// touched package in one invocation. Files at the module root, or
// files split across top-level dirs, degenerate to `./...`.
//
// Returns nil when given no files.
func packagesFromFiles(files []string) []string {
	if len(files) == 0 {
		return nil
	}
	var prefix []string
	first := true
	for _, f := range files {
		dir := filepath.ToSlash(filepath.Dir(f))
		if dir == "" || dir == "." {
			// File lives at the module root — no narrower scope possible.
			prefix = nil
			first = false
			continue
		}
		parts := strings.Split(dir, "/")
		if first {
			prefix = parts
			first = false
			continue
		}
		prefix = commonPrefix(prefix, parts)
		if len(prefix) == 0 {
			break
		}
	}
	if len(prefix) == 0 {
		return []string{"./..."}
	}
	return []string{"./" + strings.Join(prefix, "/") + "/..."}
}

// commonPrefix returns the leading shared elements of two slices.
// Used to find the deepest directory common to every touched file.
func commonPrefix(a, b []string) []string {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			break
		}
		out = append(out, a[i])
	}
	return out
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
//   KILLED       → killed
//   LIVED        → survived (with snippet)
//   NOT COVERED  → survived (mutant the tests never even ran)
//   TIMED OUT    → timeout
//   NOT VIABLE   → not counted (compile error, not a test-quality signal)
//   SKIPPED      → not counted
//   RUNNABLE     → not counted (state, not a result)
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

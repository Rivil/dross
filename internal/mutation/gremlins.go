package mutation

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

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

	args := append([]string{
		"gremlins", "unleash",
		"--output", reportRel,
	}, pkgs...)
	cmd := g.buildCmd(args)
	cmd.Stdout = os.Stderr // streamed; not captured (long-running)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// Gremlins exits non-zero when threshold flags fail or surviving
		// mutants exist. Both are "ran successfully with bad results"
		// outcomes — try to read the report regardless.
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return nil, fmt.Errorf("gremlins invocation failed: %w (is gremlins installed? `go install github.com/go-gremlins/gremlins/cmd/gremlins@latest`)", err)
		}
	}

	b, err := os.ReadFile(reportAbs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("gremlins did not write a report at %s — check that --output worked", reportAbs)
		}
		return nil, fmt.Errorf("read gremlins report: %w", err)
	}
	return ParseGremlinsJSON(b)
}

func (g *Gremlins) buildCmd(args []string) *exec.Cmd {
	if g.Prefix == "" {
		return exec.Command(args[0], args[1:]...)
	}
	prefix := strings.Fields(g.Prefix)
	full := append(prefix, args...)
	return exec.Command(full[0], full[1:]...)
}

// packagesFromFiles converts a list of .go files into a deduped, sorted
// list of `./<dir>/...` package paths suitable for `gremlins unleash`.
//
// Why `...`: gremlins doesn't support file-level granularity. The
// closest we get is package-level. Sub-packages are implicit via /...
// — if the user only wants one package, they can pass the dir directly.
func packagesFromFiles(files []string) []string {
	seen := map[string]bool{}
	for _, f := range files {
		dir := filepath.Dir(f)
		if dir == "" || dir == "." {
			seen["./..."] = true
			continue
		}
		seen["./"+filepath.ToSlash(dir)] = true
	}
	pkgs := make([]string, 0, len(seen))
	for p := range seen {
		pkgs = append(pkgs, p)
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

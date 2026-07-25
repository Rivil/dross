package mutation

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Stryker adapter for TS/JS/Svelte mutation testing via stryker-mutator.
//
// Stryker writes its report to <project>/reports/mutation/mutation.json
// in the mutation-testing-report-schema format. We invoke stryker via
// the project's runtime (docker or native), then parse the report.
type Stryker struct {
	// Prefix is the runtime command prefix (e.g. "docker compose exec app").
	// Empty means run natively in cwd.
	Prefix string
	// ProjectRoot is the absolute path the runtime sees as the project root.
	// For native, this is the cwd. For docker, this is the cwd on the host
	// (we still read reports from the host filesystem).
	ProjectRoot string
	// Workdir is the repo-relative package that hosts stryker and its config
	// in a monorepo (e.g. "web" when vitest + stryker.config.json live in
	// web/, not the repo root). Empty means the repo root. The adapter runs
	// stryker there, strips the prefix from --mutate paths, and re-prefixes
	// report paths so tests.json stays repo-relative.
	Workdir string
}

func (s *Stryker) Name() string { return "stryker" }

func (s *Stryker) Supports(file string) bool {
	switch strings.ToLower(filepath.Ext(file)) {
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".svelte":
		return true
	}
	return false
}

// Run invokes stryker on the given files, then parses the JSON report.
func (s *Stryker) Run(files []string) (*Report, error) {
	if len(files) == 0 {
		return &Report{Tool: s.Name()}, nil
	}

	cmd := s.buildCmd(s.runArgs(files))
	cmd.Dir = s.workDir()
	cmd.Stdout = os.Stderr // streamed to user, not captured
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// Stryker exits non-zero when surviving mutants exist —
		// that's a successful run with bad results, not an adapter
		// failure. We still try to read the report.
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return nil, fmt.Errorf("stryker invocation failed: %w (is stryker installed in the project? `npm i -D @stryker-mutator/core` or equivalent)", err)
		}
	}

	reportPath := s.reportPath()
	b, err := os.ReadFile(reportPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("stryker did not write a report at %s — check stryker config", reportPath)
		}
		return nil, fmt.Errorf("read stryker report: %w", err)
	}
	report, err := ParseStrykerJSON(b)
	if err != nil {
		return nil, err
	}
	s.rePrefixFiles(report)
	return report, nil
}

// runArgs builds the stryker invocation. The scoped package name matters:
// bare "stryker" on the npm registry is the ancient pre-scoped package and
// crashes on modern Node (MODULE_NOT_FOUND); npx resolves a project-local
// @stryker-mutator/core first and --yes fetches the right fallback.
// --mutate paths are workdir-relative because stryker runs in Workdir.
func (s *Stryker) runArgs(files []string) []string {
	mutate := make([]string, 0, len(files))
	for _, f := range files {
		if s.Workdir != "" {
			f = strings.TrimPrefix(f, s.Workdir+"/")
		}
		mutate = append(mutate, f)
	}
	return []string{"npx", "--yes", "@stryker-mutator/core", "run",
		"--mutate", strings.Join(mutate, ","),
		"--reporters", "json"}
}

// workDir is the directory stryker runs in — ProjectRoot, or the monorepo
// package under it.
func (s *Stryker) workDir() string {
	if s.Workdir == "" {
		return s.ProjectRoot
	}
	return filepath.Join(s.ProjectRoot, s.Workdir)
}

// reportPath is where stryker's json reporter writes, under the workdir.
func (s *Stryker) reportPath() string {
	return filepath.Join(s.workDir(), "reports", "mutation", "mutation.json")
}

// rePrefixFiles restores repo-relative paths in a report parsed from a
// workdir run, so tests.json speaks the same paths as changes.json.
func (s *Stryker) rePrefixFiles(r *Report) {
	if s.Workdir == "" {
		return
	}
	for i := range r.Surviving {
		r.Surviving[i].File = s.Workdir + "/" + r.Surviving[i].File
	}
}

// buildCmd returns an exec.Cmd that respects s.Prefix.
// If Prefix is empty, runs args natively. Otherwise prepends Prefix
// (split on whitespace) to args.
func (s *Stryker) buildCmd(args []string) *exec.Cmd {
	if s.Prefix == "" {
		return exec.Command(args[0], args[1:]...)
	}
	prefix := strings.Fields(s.Prefix)
	full := append(prefix, args...)
	return exec.Command(full[0], full[1:]...)
}

// strykerReport mirrors the subset of the Stryker JSON schema we care about.
// Schema: github.com/stryker-mutator/mutation-testing-elements
// (mutation-testing-report-schema)
type strykerReport struct {
	SchemaVersion string                 `json:"schemaVersion"`
	Files         map[string]strykerFile `json:"files"`
}

type strykerFile struct {
	Language string          `json:"language"`
	Source   string          `json:"source"`
	Mutants  []strykerMutant `json:"mutants"`
}

type strykerMutant struct {
	ID           string          `json:"id"`
	MutatorName  string          `json:"mutatorName"`
	Replacement  string          `json:"replacement"`
	Status       string          `json:"status"`
	StatusReason string          `json:"statusReason,omitempty"`
	Location     strykerLocation `json:"location"`
	Description  string          `json:"description,omitempty"`
}

type strykerLocation struct {
	Start strykerPos `json:"start"`
	End   strykerPos `json:"end"`
}

type strykerPos struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// ParseStrykerJSON converts a Stryker mutation.json payload into the
// normalised Report shape verify uses.
//
// Status mapping (Stryker → Report):
//
//	Killed                  → killed
//	Survived                → survived (recorded with snippet)
//	Timeout                 → timeout
//	RuntimeError, CompileError → errors
//	NoCoverage              → survived (test never even ran the mutant)
//	Pending, Ignored        → ignored (not counted)
//
// Score uses Stryker's convention: killed / (killed + survived + timeout)
// — the "mutation score" excluding errors and ignored mutants.
func ParseStrykerJSON(data []byte) (*Report, error) {
	var raw strykerReport
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode stryker report: %w", err)
	}
	r := &Report{Tool: "stryker"}
	for path, f := range raw.Files {
		for _, m := range f.Mutants {
			switch m.Status {
			case "Killed":
				r.Killed++
			case "Survived", "NoCoverage":
				r.Survived++
				r.Surviving = append(r.Surviving, Mutant{
					File:    path,
					Line:    m.Location.Start.Line,
					Op:      m.MutatorName,
					Snippet: m.Replacement,
				})
			case "Timeout":
				r.Timeout++
			case "RuntimeError", "CompileError":
				r.Errors++
			case "Pending", "Ignored", "":
				// not counted
			default:
				// future statuses — count as errors so they're visible
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

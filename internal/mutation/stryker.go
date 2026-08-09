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

	"github.com/Rivil/dross/internal/argfence"
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

	args, err := s.runArgs(files)
	if err != nil {
		return nil, err
	}
	cmd := strykerBuildCmd(s, args)
	cmd.Dir = s.workDir()
	cmd.Stdout = os.Stderr // streamed to user, not captured
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// Stryker exits non-zero when surviving mutants exist —
		// that's a successful run with bad results, not an adapter
		// failure. We still try to read the report.
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return nil, fmt.Errorf("stryker invocation failed: %w (is stryker installed in the project? `npm i -D %s` or equivalent)", err, strykerPin)
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

// strykerPin is the exact @stryker-mutator/core version dross invokes.
//
// `npx --yes @stryker-mutator/core` with no version is a supply-chain hole, not
// a convenience: --yes suppresses the install prompt, so an unpinned spec means
// dross silently downloads and executes whatever the registry serves as latest
// at that moment — on a developer machine, inside the repo, with the repo's own
// test command wired in. That is the shape the 2025–2026 npm registry
// compromises took. A pinned version narrows it to one artifact that can be
// reviewed before it changes, and bumping it becomes a visible diff.
//
// Bump deliberately: read the release notes, then update this one constant.
// Both the invocation and the install hint read it, and TestStrykerHintUsesSamePin
// fails if they ever drift apart.
const strykerPin = "@stryker-mutator/core@9.6.1"

// runArgs builds the stryker invocation. The scoped package name matters:
// bare "stryker" on the npm registry is the ancient pre-scoped package and
// crashes on modern Node (MODULE_NOT_FOUND); npx resolves a project-local
// @stryker-mutator/core first and --yes fetches the right fallback.
// --mutate paths are workdir-relative because stryker runs in Workdir.
//
// npx has no end-of-options token — it consumes leading-dash arguments itself
// before the wrapped binary ever sees them — so a derived value beginning with
// a dash is refused rather than fenced. Both derived inputs are checked: the
// Workdir, which comes straight from project.toml, and each --mutate entry
// AFTER the prefix trim, because it is the trimmed form that lands in the argv.
func (s *Stryker) runArgs(files []string) ([]string, error) {
	if _, err := argfence.Fence("npx", "workdir", s.Workdir); err != nil {
		return nil, err
	}
	mutate := make([]string, 0, len(files))
	for _, f := range files {
		if s.Workdir != "" {
			f = strings.TrimPrefix(f, s.Workdir+"/")
		}
		mutate = append(mutate, f)
	}
	if _, err := argfence.Fence("npx", "mutate path", mutate...); err != nil {
		return nil, err
	}
	return []string{"npx", "--yes", strykerPin, "run",
		"--mutate", strings.Join(mutate, ","),
		"--reporters", "json"}, nil
}

// strykerBuildCmd is the process-construction seam — see gremlinsBuildCmd.
var strykerBuildCmd = (*Stryker).buildCmd

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

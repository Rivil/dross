package mutation

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// StrykerNet adapter for C# / .NET mutation testing via stryker-mutator/stryker-net.
//
// Stryker.NET writes the same mutation-testing-report-schema JSON as
// Stryker.JS, so ParseStrykerJSON handles the report decode. The
// per-tool wrapper here owns invocation and report-file discovery.
//
// Defaults assume the `dotnet stryker` global tool is installed in the
// runtime ("dotnet tool install -g dotnet-stryker"). The runtime
// prefix (Prefix) is honored the same way as the JS Stryker adapter.
type StrykerNet struct {
	// Prefix is the runtime command prefix (e.g. "docker compose exec app").
	// Empty means run natively in cwd.
	Prefix string
	// ProjectRoot is the absolute path the runtime sees as the project root.
	// Reports are read from the host filesystem, so callers in docker mode
	// must arrange a shared mount — same constraint as the JS adapter.
	ProjectRoot string
	// OutputDir is the directory passed to `dotnet stryker --output`.
	// Defaults to "StrykerOutput" relative to ProjectRoot if empty.
	OutputDir string
}

func (s *StrykerNet) Name() string { return "stryker-net" }

func (s *StrykerNet) Supports(file string) bool {
	return strings.EqualFold(filepath.Ext(file), ".cs")
}

// Run invokes Stryker.NET, then parses the JSON report.
//
// Stryker.NET requires running from a directory that contains exactly
// one solution or csproj. We don't try to scope the run to a subset of
// files (Stryker.NET's --mutate flag is project-level, not file-level
// granular); instead, the adapter shells out and lets Stryker discover
// the project layout. The verify caller decides whether files in this
// phase warrant a full project-scope run.
func (s *StrykerNet) Run(files []string) (*Report, error) {
	if len(files) == 0 {
		return &Report{Tool: s.Name()}, nil
	}
	outDir := s.outputDir()

	args := []string{"dotnet", "stryker",
		"--reporter", "json",
		"--output", outDir,
	}
	cmd := s.buildCmd(args)
	cmd.Stdout = os.Stderr // streamed; not captured
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// Surviving mutants → non-zero exit, same as Stryker.JS. The
		// adapter only fails when invocation itself failed (binary
		// missing, project not found).
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return nil, fmt.Errorf("stryker.net invocation failed: %w (is stryker-net installed? `dotnet tool install -g dotnet-stryker`)", err)
		}
	}

	reportPath, err := findReport(outDir)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(reportPath)
	if err != nil {
		return nil, fmt.Errorf("read stryker.net report: %w", err)
	}
	report, perr := ParseStrykerJSON(b)
	if perr != nil {
		return nil, perr
	}
	report.Tool = s.Name()
	return report, nil
}

func (s *StrykerNet) outputDir() string {
	dir := s.OutputDir
	if dir == "" {
		dir = "StrykerOutput"
	}
	if filepath.IsAbs(dir) {
		return dir
	}
	if s.ProjectRoot != "" {
		return filepath.Join(s.ProjectRoot, dir)
	}
	return dir
}

func (s *StrykerNet) buildCmd(args []string) *exec.Cmd {
	if s.Prefix == "" {
		return exec.Command(args[0], args[1:]...)
	}
	prefix := strings.Fields(s.Prefix)
	full := append(prefix, args...)
	return exec.Command(full[0], full[1:]...)
}

// findReport walks outDir looking for a file named "mutation-report.json".
// Stryker.NET writes to varying paths between versions:
//   - newer: <outDir>/reports/mutation-report.json
//   - older: <outDir>/<timestamp>/reports/mutation-report.json
//   - sometimes: <outDir>/mutation-report.json
//
// We pick the most recently modified match so re-runs don't return a
// stale report from a prior cycle.
func findReport(outDir string) (string, error) {
	type candidate struct {
		path string
		mod  int64
	}
	var found []candidate
	err := filepath.WalkDir(outDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, fs.ErrNotExist) {
				return walkErr
			}
			return nil // tolerate read errors mid-walk
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() != "mutation-report.json" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		found = append(found, candidate{path: path, mod: info.ModTime().Unix()})
		return nil
	})
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("stryker.net output dir %s does not exist — invocation may have failed", outDir)
		}
		return "", fmt.Errorf("walk %s: %w", outDir, err)
	}
	if len(found) == 0 {
		return "", fmt.Errorf("stryker.net wrote no mutation-report.json under %s — check stryker config", outDir)
	}
	sort.Slice(found, func(i, j int) bool { return found[i].mod > found[j].mod })
	return found[0].path, nil
}

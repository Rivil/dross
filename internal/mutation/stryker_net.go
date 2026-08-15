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

	"github.com/Rivil/dross/internal/argfence"
	"github.com/Rivil/dross/internal/remote"
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
	// Workdir is the repo-relative directory holding the solution or project
	// (e.g. "src"), the .NET counterpart of Stryker.Workdir. It is only
	// consulted when a report path cannot be made repo-relative by stripping
	// ProjectRoot — an older Stryker.NET writing project-relative paths, or a
	// containerised run whose absolute paths belong to the container's
	// filesystem rather than the host's.
	Workdir string

	// Remote delegates the run to another machine. Nil runs locally, and a
	// local run is byte-identical to what it was before remoting existed.
	//
	// A NAMED field rather than an embedded Launcher — see launcher.go.
	Remote *remote.Target
}

// strykerNetRoot picks out the one top-level field ParseStrykerJSON ignores.
//
// Stryker.NET keys `files` by each component's absolute FullPath and writes
// the mutated tree's root alongside as `projectRoot` (JsonReport.cs:
// `ProjectRoot = mutationReport.FullPath`). Both are needed to recover a
// repo-relative path, and the shared parser has no business knowing about a
// field only the .NET emitter writes.
type strykerNetRoot struct {
	ProjectRoot string `json:"projectRoot"`
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

	// dotnet and stryker.net both parse options positionally with no
	// end-of-options token, so a dash output dir is refused before the process
	// is built. Both forms are checked: the raw config value, which is the line
	// the user has to fix, and the resolved path, which is what lands in argv —
	// outputDir() joins ProjectRoot when one is set, so the two differ.
	if _, err := argfence.Fence("dotnet", "output dir", s.OutputDir, outDir); err != nil {
		return nil, err
	}

	lr, err := newLauncher(s.Name(), s.Prefix, s.Remote, s.ProjectRoot, "")
	if err != nil {
		return nil, err
	}
	// .NET's restore is `dotnet restore` regardless of any package manager, so
	// this cannot refuse — resolved early anyway so every adapter's remote
	// pre-flight has the same shape.
	if _, err := lr.restoreArgv(); err != nil {
		return nil, err
	}
	// The remote output dir is workdir-relative by construction. An absolute
	// one names a path on THIS machine that has no meaning on the remote, and
	// silently rooting it under the remote workdir would be dross deciding the
	// user meant something they did not write.
	if lr.remoteRun() && filepath.IsAbs(s.OutputDir) {
		return nil, fmt.Errorf(
			"stryker.net: output dir %q is absolute, which cannot be resolved on remote host %q — "+
				"use a repo-relative output dir for a remote run", s.OutputDir, s.Remote.Host)
	}

	// --output takes the LOCAL absolute path for a local run and the
	// workdir-relative one for a remote run. outputDir() joins ProjectRoot,
	// which names a directory on THIS machine; handing that to the remote would
	// make stryker write outside the synced tree, where the fetch does not look
	// — a run that appears to succeed and produces no report.
	argOut := outDir
	if lr.remoteRun() {
		argOut, err = lr.reportRel(s.OutputDir)
		if err != nil {
			return nil, err
		}
		if _, ferr := argfence.Fence("dotnet", "remote output dir", argOut); ferr != nil {
			return nil, ferr
		}
	}
	args := []string{"dotnet", "stryker",
		"--reporter", "json",
		"--output", argOut,
	}
	// The WHOLE output tree is cleared, not one file: findReport walks it and
	// picks the most recently modified mutation-report.json, so a surviving
	// subtree from a previous run is a report this run did not produce.
	if err := lr.clearReport(s.OutputDir, outDir); err != nil {
		return nil, err
	}
	cmd, err := lr.toolCmd(args, func(a []string) *exec.Cmd { return strykerNetBuildCmd(s, a) })
	if err != nil {
		return nil, err
	}
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

	// Fetched to the output dir's PARENT: the remote source is the tree itself,
	// and rsync recreates it by name under the destination. Landing it at
	// outDir would nest a second StrykerOutput inside the first.
	if ferr := lr.fetchReport(s.OutputDir, filepath.Dir(outDir)); ferr != nil {
		fmt.Fprintf(os.Stderr, "stryker.net: fetch report: %v\n", ferr)
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
	var meta strykerNetRoot
	// A report that omits projectRoot is not a parse failure — older
	// Stryker.NET wrote project-relative paths and no root at all, which the
	// Workdir path below handles.
	_ = json.Unmarshal(b, &meta)
	s.rePrefixFiles(report, meta.ProjectRoot)
	return report, nil
}

// rePrefixFiles rewrites the report's paths to be repo-relative, in the
// per-file rows and in Surviving alike.
//
// Stryker.NET keys its report by absolute FullPath. Left alone, none of those
// paths matches a repo-relative change set, so every C# mutant filters out as
// out-of-scope and the leg reports a vacuous 0/0 — a whole language silently
// exempt from the gate while looking measured.
//
// Two routes, in order of trustworthiness:
//
//  1. The path lies under ProjectRoot, the repo root dross itself resolved.
//     Stripping it yields a repo-relative path by construction — no config,
//     no guessing.
//  2. Otherwise strip the report's own projectRoot, which leaves a path
//     relative to the mutated tree, and apply Workdir to place that tree in
//     the repo. This is the same operation Stryker.rePrefixFiles performs for
//     its Workdir, so both adapters hand identically-shaped paths to the
//     filter.
//
// A path that survives both routes still absolute belongs to a tree dross
// cannot locate. It is left as-is rather than having Workdir glued onto an
// absolute path: an honestly unmatched path gets reported as out-of-scope,
// where a fabricated one would silently match the wrong file.
func (s *StrykerNet) rePrefixFiles(r *Report, reportRoot string) {
	if r == nil {
		return
	}
	for i := range r.Surviving {
		r.Surviving[i].File = s.repoRelative(r.Surviving[i].File, reportRoot)
	}
	if len(r.Files) == 0 {
		return
	}
	files := make(map[string]FileStat, len(r.Files))
	for name, st := range r.Files {
		k := s.repoRelative(name, reportRoot)
		files[k] = files[k].plus(st)
	}
	r.Files = files
}

func (s *StrykerNet) repoRelative(p, reportRoot string) string {
	// Windows-produced reports carry backslashes; the scope speaks slashes.
	p = strings.ReplaceAll(p, `\`, "/")
	if rel, ok := underRoot(p, s.ProjectRoot); ok {
		return rel
	}
	if rel, ok := underRoot(p, reportRoot); ok {
		p = rel
	}
	if strings.HasPrefix(p, "/") {
		return p
	}
	return s.withWorkdir(p)
}

// underRoot strips root from p when p sits beneath it, reporting whether it did.
func underRoot(p, root string) (string, bool) {
	root = strings.TrimSuffix(strings.ReplaceAll(root, `\`, "/"), "/")
	if root == "" || !strings.HasPrefix(p, root+"/") {
		return p, false
	}
	return strings.TrimPrefix(p, root+"/"), true
}

// withWorkdir applies the configured repo-relative prefix, idempotently: a
// path that already carries it is returned unchanged rather than doubled.
func (s *StrykerNet) withWorkdir(p string) string {
	if s.Workdir == "" {
		return p
	}
	prefix := strings.TrimSuffix(strings.ReplaceAll(s.Workdir, `\`, "/"), "/") + "/"
	if prefix == "/" || strings.HasPrefix(p, prefix) {
		return p
	}
	return prefix + p
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

// strykerNetBuildCmd is the process-construction seam — see gremlinsBuildCmd.
var strykerNetBuildCmd = (*StrykerNet).buildCmd

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

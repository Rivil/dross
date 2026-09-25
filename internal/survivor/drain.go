package survivor

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Rivil/dross/internal/mutation"
)

// drain.go holds `dross survivor drain`'s toolchain spawns and its raw-report
// reading. Both spawns — package discovery and the coverage pass — run the
// repo's own toolchain over its own packages, so every caller checks consent
// first; the exec-consent audit proves each site here is reached only through
// the gated drain command.

// GoListDirs lists every package directory in the module at repoRoot.
func GoListDirs(repoRoot string) ([]string, error) {
	cmd := exec.Command("go", "list", "-f", "{{.Dir}}", "./...")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list ./...: %w", err)
	}
	var dirs []string
	//dross:taint-cleared go list -f {{.Dir}} prints one package directory per line; each becomes a gremlins package path, and nothing else of go's output is kept
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			dirs = append(dirs, line)
		}
	}
	return dirs, nil
}

// RunCoverageProfile runs `go test -coverprofile` over pkgs and parses the
// result. A failure is not fatal: coverage is EVIDENCE, and a drain that
// refused to run without it would be less useful than one that reports
// unknown — as long as unknown never reads as "not covered", which is
// Profile's contract.
//
// The profile is written to a fresh temp dir per run. A fixed path would let a
// run that never produced a profile — no `go` on PATH — parse the one a
// previous run left behind, and report stale coverage as this run's.
func RunCoverageProfile(repoRoot string, pkgs []string) *Profile {
	dir, err := os.MkdirTemp("", "dross-drain-cover-")
	if err != nil {
		return nil
	}
	defer func() { _ = os.RemoveAll(dir) }()
	out := filepath.Join(dir, "cover.out")
	args := append([]string{"test", "-count=1", "-coverprofile=" + out}, pkgs...)
	cmd := exec.Command("go", args...)
	cmd.Dir = repoRoot
	// Output is discarded: a failing suite still produces a usable profile for
	// the packages that did run, and the drain is not a test runner.
	_ = cmd.Run()
	prof, err := ParseProfile(out)
	if err != nil {
		return nil
	}
	return prof
}

// RawMutant is one survivor read out of a raw gremlins report, with what the
// tool said about its coverage.
type RawMutant struct {
	File string
	Line int
	Op   string
	// NotCovered is the tool's own NOT COVERED status. Kept because the
	// attribution ceiling is the DISAGREEMENT between this and go-cover, and a
	// deriver that only saw one of the two could not detect it.
	NotCovered bool
}

// ReadRawReport parses one gremlins report and returns its survivors with
// repo-relative paths. pkg may be empty, in which case paths are left as the
// tool wrote them. Which of them are anybody's debt — the testdata scope rule —
// is the caller's to apply.
func ReadRawReport(path, pkg string) ([]RawMutant, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	rep, err := mutation.ParseGremlinsJSON(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if pkg != "" {
		mutation.RePrefixGremlinsFiles(rep, pkg)
	}
	// Ceiling eligibility turns on whether the TOOL called this exact mutant
	// NOT COVERED, so the status is read per mutant from the raw payload.
	// mutation.Report folds LIVED and NOT COVERED into one Surviving list, and
	// a file-granular approximation would let one uncovered mutant grant the
	// ceiling to every other survivor in its file — accepting killable code.
	notCovered, err := notCoveredPositions(b, pkg)
	if err != nil {
		return nil, err
	}

	out := make([]RawMutant, 0, len(rep.Surviving))
	for _, m := range rep.Surviving {
		out = append(out, RawMutant{
			File: m.File, Line: m.Line, Op: m.Op,
			NotCovered: notCovered[mutantPos(m.File, m.Line, m.Op)],
		})
	}
	return out, nil
}

// mutantPos keys a mutant by the triple that identifies it within a report.
func mutantPos(file string, line int, op string) string {
	return file + ":" + strconv.Itoa(line) + ":" + op
}

// rawGremlinsPayload is the minimal view of the report needed to recover each
// mutant's STATUS, which mutation.Report deliberately does not carry (it folds
// LIVED and NOT COVERED into one Surviving list, because for scoring they are
// the same thing). For deciding accept-vs-kill they are opposites.
type rawGremlinsPayload struct {
	Files []struct {
		Filename  string `json:"file_name"`
		Mutations []struct {
			Type   string `json:"type"`
			Status string `json:"status"`
			Line   int    `json:"line"`
		} `json:"mutations"`
	} `json:"files"`
}

// notCoveredPositions returns the set of mutants the tool reported NOT COVERED,
// keyed the same way the survivor list is, with pkg applied so the paths match.
func notCoveredPositions(payload []byte, pkg string) (map[string]bool, error) {
	var raw rawGremlinsPayload
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("read mutant statuses: %w", err)
	}
	prefix := strings.TrimPrefix(filepath.ToSlash(pkg), "./")
	out := map[string]bool{}
	for _, f := range raw.Files {
		name := filepath.ToSlash(f.Filename)
		if prefix != "" && prefix != "." && !strings.HasPrefix(name, prefix+"/") {
			name = prefix + "/" + name
		}
		for _, m := range f.Mutations {
			if m.Status == "NOT COVERED" {
				out[mutantPos(name, m.Line, m.Type)] = true
			}
		}
	}
	return out, nil
}

package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/mod/modfile"
)

// floorPercent is the minimum own-package statement coverage for an in-scope
// file. A constant, not a knob.
const floorPercent = 50

// fileCov is one in-scope file's statement totals, the path module-relative.
type fileCov struct {
	File           string
	Covered, Total int
}

func (f fileCov) pct() float64 {
	if f.Total == 0 {
		return 100
	}
	return 100 * float64(f.Covered) / float64(f.Total)
}

// belowFloor compares in integers so no float rounding can move a file across
// the line.
func belowFloor(covered, total int) bool { return covered*100 < total*floorPercent }

// run is main with its inputs as parameters. dir is where go.mod is read.
func run(dir string, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		fmt.Fprintln(stderr, "usage: coverfloor <coverprofile>")
		return 2
	}
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		fmt.Fprintf(stderr, "coverfloor: %v\n", err)
		return 2
	}
	module := modfile.ModulePath(data)
	if module == "" {
		fmt.Fprintln(stderr, "coverfloor: go.mod declares no module path")
		return 2
	}
	blocks, err := parseProfile(args[0])
	if err != nil {
		fmt.Fprintf(stderr, "coverfloor: %v\n", err)
		return 2
	}
	files := measure(blocks, module)
	if len(files) == 0 {
		fmt.Fprintf(stderr, "coverfloor: %s holds no file under %s/internal/ outside internal/cmd — nothing was measured\n", args[0], module)
		return 2
	}

	var low []fileCov
	for _, f := range files {
		if belowFloor(f.Covered, f.Total) {
			low = append(low, f)
		}
	}
	if len(low) > 0 {
		fmt.Fprintf(stdout, "coverfloor: %d of %d files below the %d%% own-package statement floor:\n", len(low), len(files), floorPercent)
		for _, f := range low {
			fmt.Fprintf(stdout, "  %s %.1f%% (%d/%d)\n", f.File, f.pct(), f.Covered, f.Total)
		}
		return 1
	}
	lowest := files[0]
	for _, f := range files[1:] {
		if f.Covered*lowest.Total < lowest.Covered*f.Total {
			lowest = f
		}
	}
	fmt.Fprintf(stdout, "coverfloor: %d files measured, lowest %s %.1f%%\n", len(files), lowest.File, lowest.pct())
	return 0
}

// profileBlock is one go test -coverprofile line:
// <file>:<startLine>.<startCol>,<endLine>.<endCol> <numStmt> <count>
var profileBlock = regexp.MustCompile(`^(.+):(\d+\.\d+,\d+\.\d+) (\d+) (\d+)$`)

// stmtBlock is one source span's statement count and whether any run reached
// it.
type stmtBlock struct {
	stmts   int
	covered bool
}

// parseProfile reads a coverprofile into per-file blocks keyed by span.
// golang.org/x/tools/cover would do this, but x/tools is a test-only import in
// this module (the loader_mechanism lock TestSourceScansStayOutOfTheBinary
// holds), so the format is parsed here. A span listed more than once — two
// packages' runs both reporting it — is merged: its statements count once, and
// as covered if any run reached them.
func parseProfile(path string) (map[string]map[string]stmtBlock, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if !strings.HasPrefix(lines[0], "mode: ") {
		return nil, fmt.Errorf("%s: first line %q is not a mode line", path, lines[0])
	}
	files := map[string]map[string]stmtBlock{}
	for i, line := range lines[1:] {
		m := profileBlock.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("%s:%d: %q is not a coverage block", path, i+2, line)
		}
		file, span := m[1], m[2]
		stmts, err := strconv.Atoi(m[3])
		if err != nil {
			return nil, fmt.Errorf("%s:%d: statement count: %w", path, i+2, err)
		}
		// Any non-zero count is covered; comparing digits rather than parsing
		// keeps an atomic-mode count too large for an int from erroring.
		covered := strings.TrimLeft(m[4], "0") != ""
		spans := files[file]
		if spans == nil {
			spans = map[string]stmtBlock{}
			files[file] = spans
		}
		if prev, ok := spans[span]; ok {
			if prev.stmts != stmts {
				return nil, fmt.Errorf("%s:%d: %s:%s lists %d statements, earlier %d", path, i+2, file, span, stmts, prev.stmts)
			}
			covered = covered || prev.covered
		}
		spans[span] = stmtBlock{stmts: stmts, covered: covered}
	}
	return files, nil
}

// measure sums statements per in-scope file, sorted by path.
func measure(blocks map[string]map[string]stmtBlock, module string) []fileCov {
	scope := module + "/internal/"
	excluded := module + "/internal/cmd/"
	var out []fileCov
	for name, spans := range blocks {
		if !strings.HasPrefix(name, scope) || strings.HasPrefix(name, excluded) {
			continue
		}
		f := fileCov{File: strings.TrimPrefix(name, module+"/")}
		for _, b := range spans {
			f.Total += b.stmts
			if b.covered {
				f.Covered += b.stmts
			}
		}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].File < out[j].File })
	return out
}

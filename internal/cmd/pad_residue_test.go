package cmd

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The line-pad heuristic is retired, and so is every description of it. This
// is the gate that keeps it retired: a scan over the docs that describe range
// provenance and every non-test Go file dross ships, for the heuristic's own
// spellings. A doc that still says "padded" or a struct that grows a `pad`
// field back reads as a fact about the tool — and it would be a lie about
// what a run measured.
//
// The scanner is calibrated (TestPadResidueScanTripsOnItsFixture) so a
// needle that drifts cannot leave the gate green by matching nothing.

// padResidueNeedles are the retired heuristic's spellings: its constant, its
// function, its persisted keys, and the prose the docs used for it.
var padResidueNeedles = []string{
	"hunkContextLines",
	"padAndMerge",
	`json:"pad"`,
	`toml:"pad`,
	"(pad ",
	"post-pad",
	"padded hunk",
}

// padResidueDocs are the documents that describe range provenance. They must
// carry the NEW closed reason set in full, not merely drop the pad.
var padResidueDocs = []string{"ARCHITECTURE.md", "assets/prompts/verify.md", "README.md"}

var wholeFileReasonNames = []string{
	"adapter-lacks-range-runner",
	"scope-has-no-hunks",
	"file-absent-from-hunks",
	"malformed-range",
	"ast-unavailable",
}

// padResidueHit is one needle found on one line.
type padResidueHit struct {
	File   string
	Line   int
	Needle string
}

// scanPadResidue walks the given files for the needles, line by line.
func scanPadResidue(paths []string) ([]padResidueHit, error) {
	var hits []padResidueHit
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			return nil, err
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1024*1024), 1024*1024)
		for n := 1; sc.Scan(); n++ {
			for _, needle := range padResidueNeedles {
				if strings.Contains(sc.Text(), needle) {
					hits = append(hits, padResidueHit{File: p, Line: n, Needle: needle})
				}
			}
		}
		f.Close()
		if err := sc.Err(); err != nil {
			return nil, err
		}
	}
	return hits, nil
}

// shippedGoFiles is every non-test .go file under internal/ and cmd/. This
// test file is excluded by the _test.go rule like every other; the needles
// it carries are its own.
func shippedGoFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "testdata" || d.Name() == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
				out = append(out, path)
			}
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	return out
}

func TestNoPadHeuristicResidue(t *testing.T) {
	root := repoRootForDocs(t)
	paths := shippedGoFiles(t, root)
	for _, doc := range padResidueDocs {
		paths = append(paths, filepath.Join(root, doc))
	}
	hits, err := scanPadResidue(paths)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		rel, _ := filepath.Rel(root, h.File)
		t.Errorf("%s:%d still carries the retired pad heuristic (%q)", rel, h.Line, h.Needle)
	}

	// The docs must describe the new closed set, not just stop describing
	// the old heuristic.
	for _, doc := range []string{"assets/prompts/verify.md", "README.md"} {
		body, err := os.ReadFile(filepath.Join(root, doc))
		if err != nil {
			t.Fatal(err)
		}
		for _, reason := range wholeFileReasonNames {
			if !strings.Contains(string(body), reason) {
				t.Errorf("%s does not name the whole-file reason %q", doc, reason)
			}
		}
	}
}

// A scanner whose needles rotted would pass the gate above by matching
// nothing. This pins that the same scanner still trips on the heuristic's
// own spelling.
func TestPadResidueScanTripsOnItsFixture(t *testing.T) {
	root := repoRootForDocs(t)
	fixture := filepath.Join(root, "internal/cmd/testdata/pad_residue/fixture.txt")
	hits, err := scanPadResidue([]string{fixture})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatalf("the residue scanner reported nothing on a fixture that spells the retired constant — the needles have rotted")
	}
	if hits[0].Needle != "hunkContextLines" {
		t.Errorf("first hit = %+v, want the constant", hits[0])
	}
}

// ARCHITECTURE.md describes the construct-range design by its symbols.
func TestArchitectureDescribesConstructRanges(t *testing.T) {
	root := repoRootForDocs(t)
	body, err := os.ReadFile(filepath.Join(root, "ARCHITECTURE.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"expandToConstructs", "ast-unavailable", "ConstructResolver", "resolveConstructs"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("ARCHITECTURE.md does not name %q", want)
		}
	}
}

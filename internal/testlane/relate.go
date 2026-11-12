package testlane

import (
	"fmt"
	"strings"

	"github.com/Rivil/dross/internal/pathfence"
)

// Relation is what one relate pass said about the paths it was asked.
//
// Only Tests comes from the stream. Covered and Missed are strings of the asked
// list, so nothing relate printed can reach a record or a refusal as a path.
type Relation struct {
	// Covered are the asked paths relate mapped to at least one test, in asked
	// order and asked spelling.
	Covered []string
	// Missed are the asked paths relate listed nothing for, in asked order and
	// asked spelling. A path with no test is a result, never a refusal.
	Missed []string
	// Tests are the distinct tests relate named, normalized, in first-seen
	// order. Each one is a regular file in the tree.
	Tests []string
}

// ParseRelate reads relate's stdout, one `<path><TAB><test>` per line, against
// the paths it was asked about.
//
// It is fail-closed over the whole output: one bad line refuses everything,
// because a stream that is wrong once cannot be trusted to say which paths its
// other lines measured. Empty output is no refusal, it is a Relation with every
// asked path Missed, and the run decides what that means.
//
// A refusal names the line number and none of the line's text, since relate's
// stdout is a tool stream and an error travels further than the terminal.
//
// The tree is read only to ask whether a named test exists. A test that does
// not is refused here rather than left for the existence filter at the run
// site, which would drop it silently and leave its path counted as covered.
func ParseRelate(repoDir string, asked []string, stdout string) (Relation, error) {
	wanted := make(map[string]bool, len(asked))
	for _, a := range asked {
		if norm, inTree := normalize(a); inTree && norm != "" {
			wanted[norm] = true
		}
	}

	var rel Relation
	listed := map[string]bool{}
	seen := map[string]bool{}
	for i, line := range relateLines(stdout) {
		n := i + 1
		rawPath, rawTest, ok := splitRelateLine(line)
		if !ok {
			return Relation{}, relateRefusal(n, "is not <path><TAB><test>")
		}
		p, _ := normalize(rawPath)
		test, inTree := normalize(rawTest)
		if p == "" || test == "" {
			return Relation{}, relateRefusal(n, "has an empty field")
		}
		if !wanted[p] {
			return Relation{}, relateRefusal(n, "names a path nobody asked about")
		}
		if !inTree {
			return Relation{}, relateRefusal(n, "names a test outside the repository")
		}
		if !testInTree(repoDir, test) {
			return Relation{}, relateRefusal(n, "names a test that is not in the tree")
		}
		listed[p] = true
		if !seen[test] {
			seen[test] = true
			rel.Tests = append(rel.Tests, test)
		}
	}

	done := map[string]bool{}
	for _, a := range asked {
		norm, _ := normalize(a)
		if norm == "" || done[norm] {
			continue
		}
		done[norm] = true
		if listed[norm] {
			rel.Covered = append(rel.Covered, a)
		} else {
			rel.Missed = append(rel.Missed, a)
		}
	}
	return rel, nil
}

// BatchArgs turns the tests of a Relation into the arguments of the lane's one
// batched line: distinct, option-safe, and in the order they arrived. Derive is
// not the route here, because its sort is for a file set and these are a list.
func BatchArgs(tests []string) []string {
	return collapse(tests, func(p string) string { return p })
}

// relateLines splits stdout into lines. One trailing newline ends the last line
// rather than opening an empty one, and a blank line anywhere else stays in, to
// be refused for carrying no tab.
func relateLines(stdout string) []string {
	if stdout == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
}

// splitRelateLine cuts a line at its one tab. A second tab is no third field to
// ignore: it means the line is not the shape relate promised.
func splitRelateLine(line string) (string, string, bool) {
	if strings.Count(line, "\t") != 1 {
		return "", "", false
	}
	return strings.Cut(line, "\t")
}

// testInTree reports whether a normalized in-tree test path is a regular file.
// A directory does not count: handed to a runner it selects whatever lies under
// it, which is not the test relate said measures the path.
func testInTree(repoDir, test string) bool {
	c, err := pathfence.Contain(repoDir, "relate output", test)
	if err != nil {
		return false
	}
	info, err := pathfence.Stat(c)
	return err == nil && info.Mode().IsRegular()
}

// relateRefusal is the one shape a refusal takes. It takes a reason rather than
// a field so that no caller can hand it the stream's text.
func relateRefusal(line int, reason string) error {
	return fmt.Errorf("relate output refused: line %d %s", line, reason)
}

package testlane

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/Rivil/dross/internal/configenum"
)

// Derive turns the paths that matched one lane into the selector arguments
// appended to that lane's command.
//
// It is the whole of the translation, and it is deliberately a closed set of
// SHAPES rather than a per-runner engine: `path` hands the runner the files,
// `dir` their directories, `go-package` the ./<dir>/... form Go's tooling
// takes. A runner is configured by pointing its lane at whichever shape it
// accepts.
//
// Pure, like the rest of this package: no filesystem, so a path that no longer
// exists is the caller's to filter before calling — see the locked
// missing_paths decision, which drops them at the run site so a deleted file
// cannot turn into `go test ./gone/...`.
//
// An empty style returns no arguments and no error. That is a value in this
// function rather than a branch every caller has to remember: a lane declaring
// no selector spawns its command untouched, and the way to get that is to
// append the nil slice Derive returns.
//
// An unrecognised style returns an error and NO arguments. Both halves matter —
// a caller that ignored the error and spawned anyway would run the lane's whole
// suite, which is the unscoped run this feature exists to avoid, silently.
func Derive(style string, paths []string) ([]string, error) {
	// Deduplicated and sorted BEFORE translation (locked selector_derivation),
	// so the derived line depends on the file set and not on argv order — the
	// line lands in the transcript beside the consented command, where it has
	// to diff cleanly run to run.
	sorted := dedupeSorted(paths)

	switch configenum.Normalize(style) {
	case "":
		return nil, nil
	case "path":
		return collapse(sorted, func(p string) string { return p }), nil
	case "dir":
		return collapse(sorted, path.Dir), nil
	case "go-package":
		return collapse(sorted, goPackage), nil
	default:
		return nil, fmt.Errorf("selector style %q is not a selector style — expected %s", style, configenum.SelectorStyles.List())
	}
}

// goPackage renders one file path as the package pattern Go's tooling takes.
//
// A root-level file yields "." — the root package — and deliberately not
// "./...", which is the whole module. Deriving the entire module from one
// touched file is exactly the unscoped run this translation exists to replace,
// and it would be the silent kind: a green whole-suite run wearing a scoped
// lane's name.
func goPackage(p string) string {
	dir := path.Dir(p)
	if dir == "." {
		return "."
	}
	return "./" + dir + "/..."
}

// dedupeSorted returns the distinct paths in lexicographic order. Blank entries
// are dropped: they name nothing, and an empty argument reaching a runner is
// either ignored or read as the current directory, neither of which is what a
// matched path meant.
func dedupeSorted(paths []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// collapse maps each path through fn and drops repeats, keeping the sorted
// order it was handed. The collapse is the point for `dir` and `go-package`:
// three files in one package must derive one selector argument, not three
// copies of it.
func collapse(sorted []string, fn func(string) string) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range sorted {
		v := optionSafe(fn(p))
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// optionSafe keeps a derived argument from reaching the runner as an option.
//
// A file named `-x.go` derives the argument `-x.go`, which every runner ever
// written reads as a flag. Prefixing it with ./ names the same file and cannot
// be read as anything else — a matched path must never become an option dross
// did not choose to pass.
func optionSafe(arg string) string {
	if strings.HasPrefix(arg, "-") {
		return "./" + arg
	}
	return arg
}

package memadvise

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Rivil/dross/internal/gitrun"
	"github.com/Rivil/dross/internal/phase"
)

// Report is one advisor run, ready to render.
type Report struct {
	Found       bool // the repo has a memory dir; false renders nothing
	Dir         string
	Suggestions []Suggestion
	Integrity   Integrity
	Turns       TurnCounts
}

// Gather resolves everything the advisor reads for the repo at root — the
// memory dir, the tracked docs, the phases a "next" clause may name, the turn
// counts — and builds the suggestions. It only ever reads the memory dir: the
// one file it may write is the turn cache under <root>/.dross/memory/ (c-3).
//
// A repo with no memory dir is a zero Report and a nil error. A root that is
// not a git checkout has no tracked docs, which only means no duplicates.
func Gather(root string, now time.Time) (Report, error) {
	dir, ok, err := Locate(root)
	if err != nil || !ok {
		return Report{}, err
	}
	c, err := Load(dir)
	if err != nil {
		return Report{}, err
	}
	turns, err := Turns(root, now)
	if err != nil {
		return Report{}, err
	}
	sugg, err := Advise(Inputs{
		Dir:   dir,
		Docs:  trackedDocs(root),
		Today: now,
		Known: knownPhases(root, c),
		Turns: turns,
	})
	if err != nil {
		return Report{}, err
	}
	return Report{Found: true, Dir: dir, Suggestions: sugg, Integrity: c.Integrity(), Turns: turns}, nil
}

// trackedDocs shingles the doc candidates git tracks. An untracked doc is
// scratch, not documentation, so a memory restating it is not redundant.
func trackedDocs(root string) *Docs {
	out, err := gitrun.Raw(root, "ls-files", "-z", "--", "CLAUDE.md", "ARCHITECTURE.md", "README*", "docs")
	if err != nil {
		return nil
	}
	var tracked []string
	//dross:taint-cleared git ls-files -z prints NUL-separated repo-relative paths; each one is kept as a path and nothing else of git's output is
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			tracked = append(tracked, p)
		}
	}
	paths := DocCandidates(tracked)
	content := map[string]string{}
	for _, p := range paths {
		if data, ok := readRegular(filepath.Join(root, filepath.FromSlash(p))); ok {
			content[p] = string(data)
		}
	}
	return NewDocs(paths, content)
}

// knownPhases is the set of slugs the index's "next" clauses name that are
// already scaffolded or shipped — only those, so status never walks every
// phase to price a hook.
func knownPhases(root string, c *Corpus) map[string]bool {
	drossRoot := filepath.Join(root, ".dross")
	if _, err := os.Stat(drossRoot); err != nil {
		return nil
	}
	known := map[string]bool{}
	for _, l := range c.Lines {
		for _, slug := range NextSlugs(l.Hook) {
			if phase.DirExists(drossRoot, slug) || phase.Done(drossRoot, slug) {
				known[slug] = true
			}
		}
	}
	return known
}

package verify

import (
	"fmt"
	"path"
	"path/filepath"
	"sort"

	"github.com/Rivil/dross/internal/mutation"
)

// LegLines are the lines printed beneath a mutation leg's counts line: one row
// per package (the directory of each file in Mutation.Files) and then each
// in-scope survivor as file:line, grouped by file. They replace the tool's own
// per-mutant stream, which goes to the run log, so the conversation still sees
// where the score came from and what is left to answer for.
//
// Survived counts NOT COVERED mutants too, the way the leg line and tests.json
// count it, so each column sums to the leg's own total. A leg that failed or
// has no report gets no lines: its counts line already says so.
func LegLines(lr LanguageRun) []string {
	m := lr.Mutation
	if lr.Error != "" || m == nil {
		return nil
	}
	rows := map[string]mutation.FileStat{}
	for file, st := range m.Files {
		dir := path.Dir(filepath.ToSlash(file))
		r := rows[dir]
		r.Killed += st.Killed
		r.Survived += st.Survived
		r.NotCovered += st.NotCovered
		r.Timeout += st.Timeout
		rows[dir] = r
	}
	dirs := make([]string, 0, len(rows))
	for d := range rows {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)

	var out []string
	for _, d := range dirs {
		r := rows[d]
		out = append(out, fmt.Sprintf("    %s: killed=%d survived=%d (not_covered=%d) timeout=%d",
			d, r.Killed, r.Survived, r.NotCovered, r.Timeout))
	}
	return append(out, survivorLines(m.Surviving)...)
}

// survivorLines lists each survivor's file:line once, sorted by file then
// line, marking a line whose survivors no test ever executed.
func survivorLines(ms []mutation.Mutant) []string {
	type site struct {
		file string
		line int
	}
	// A line is NOT COVERED only when every survivor on it is: one that a
	// test ran and missed makes the line a test gap, not a coverage blind spot.
	notCovered := map[site]bool{}
	for _, m := range ms {
		s := site{filepath.ToSlash(m.File), m.Line}
		prev, seen := notCovered[s]
		notCovered[s] = m.NotCovered && (!seen || prev)
	}
	sites := make([]site, 0, len(notCovered))
	for s := range notCovered {
		sites = append(sites, s)
	}
	sort.Slice(sites, func(i, j int) bool {
		if sites[i].file != sites[j].file {
			return sites[i].file < sites[j].file
		}
		return sites[i].line < sites[j].line
	})
	out := make([]string, 0, len(sites)+1)
	if len(sites) > 0 {
		out = append(out, fmt.Sprintf("    survivors (%d):", len(sites)))
	}
	for _, s := range sites {
		l := fmt.Sprintf("      %s:%d", s.file, s.line)
		if notCovered[s] {
			l += " NOT COVERED"
		}
		out = append(out, l)
	}
	return out
}

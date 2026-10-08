package diag

import (
	"fmt"

	"github.com/Rivil/dross/internal/mutationcfg"
	"github.com/Rivil/dross/internal/project"
)

// MutationToolchain reports whether the LOCAL toolchain each configured
// mutation adapter needs is actually present.
//
// The timing is the point. Without it a non-Go stack discovers the gap only
// when a verify run comes back having measured nothing — and an empty
// measurement does not announce itself: the phase scores over zero mutants and
// no line says why. Doctor asks the same question before any of that.
//
// ADVISORY, never an issue: every line is a Warn. Most repos are single-stack:
// failing a Go-only clone for lacking Node would be a check people learn to
// ignore, and a check people ignore protects nothing. It is also scoped to the
// adapters this project actually configures — a warning about a toolchain the
// project never needed is noise that trains the reader to skim past the ones
// that matter. No gaps means no lines, so the caller prints no section.
func MutationToolchain(p *project.Project, lookPath func(string) (string, error)) []Line {
	var lines []Line
	for _, gap := range mutationcfg.Missing(p, lookPath) {
		lines = append(lines,
			warn(fmt.Sprintf("%s is not installed — the %s adapter needs it to measure %s files here.", gap.Tool, gap.Adapter, gap.Language)),
			note(fmt.Sprintf("    Without it a verify run reports nothing measured rather than a bad score. Fix: %s", gap.Install)))
	}
	return lines
}

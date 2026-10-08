// Package diag holds the structured diagnostic checks `dross doctor` composes:
// red-proof pin verdicts, roadmap duplicates, remote/board combination
// warnings, config trust (branch names, API hosts, the machine-local store,
// git version, exec consent, lane consent) and the local mutation toolchain.
//
// Every check returns Lines rather than printing: the level is carried so the
// caller decides what a finding costs — a cannot-determine red proof must not
// move the exit code, and a line that printed its own glyph would make that
// decision unreadable. Nothing here spawns a process; the inputs a check needs
// from git or the machine arrive pre-read from internal/cmd.
package diag

// Level is how much a Line costs. OK and Warn never move doctor's exit code;
// Issue does. Note is a verbatim continuation line — indentation included —
// under the finding above it, so a Fix hint or a quoted command line renders
// exactly as the inline code printed it.
type Level int

const (
	OK Level = iota
	Warn
	Issue
	Note
)

func (l Level) String() string {
	switch l {
	case OK:
		return "ok"
	case Warn:
		return "warn"
	case Issue:
		return "issue"
	case Note:
		return "note"
	}
	return "unknown"
}

// Line is one rendered check result.
type Line struct {
	Level Level
	Text  string
}

// Section is one headed block of a doctor report.
type Section struct {
	Heading string
	Lines   []Line
}

// Issues counts the Issue lines across sections — the number doctor's exit
// code gates on.
func Issues(sections []Section) int {
	n := 0
	for _, s := range sections {
		n += CountIssues(s.Lines)
	}
	return n
}

// CountIssues counts the Issue lines in one list.
func CountIssues(lines []Line) int {
	n := 0
	for _, l := range lines {
		if l.Level == Issue {
			n++
		}
	}
	return n
}

func ok(text string) Line    { return Line{OK, text} }
func warn(text string) Line  { return Line{Warn, text} }
func issue(text string) Line { return Line{Issue, text} }
func note(text string) Line  { return Line{Note, text} }

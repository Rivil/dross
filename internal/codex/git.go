package codex

import (
	"strings"

	"github.com/Rivil/dross/internal/gitrun"
)

// recentLog returns up to 5 recent commit subjects that touched files
// under dir. Format: "<short-sha> <subject>".
//
// Used as ambient context for the LLM ("here's what's been going on
// in this neighbourhood lately"). Best-effort — returns empty on any
// git failure (no repo, network drive, permissions).
func recentLog(dir string) ([]string, error) {
	// dir is caller-derived, so it goes behind "--" as a pathspec — git reads a
	// leading dash as an option wherever it appears, not only in ref position.
	// The token is spelled out here rather than imported: internal/codex must
	// not depend on internal/cmd, and one shared constant is not worth
	// inverting that dependency. A log is content, so it goes through
	// gitrun.Read, which clears nothing.
	out, err := gitrun.Read(dir,
		"log",
		"--max-count=5",
		"--no-merges",
		"--pretty=format:%h %s",
		"--", // end of options; everything after is a pathspec
		dir,
	)
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines, nil
}

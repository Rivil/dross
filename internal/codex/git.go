package codex

import (
	"os/exec"
	"strings"
)

// recentLog returns up to 5 recent commit subjects that touched files
// under dir. Format: "<short-sha> <subject>".
//
// Used as ambient context for the LLM ("here's what's been going on
// in this neighbourhood lately"). Best-effort — returns empty on any
// git failure (no repo, network drive, permissions).
func recentLog(dir string) ([]string, error) {
	cmd := exec.Command("git", "-C", dir,
		"log",
		"--max-count=5",
		"--no-merges",
		"--pretty=format:%h %s",
		"--",
		dir,
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		l = strings.TrimSpace(l)
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines, nil
}

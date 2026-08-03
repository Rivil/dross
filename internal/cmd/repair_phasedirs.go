package cmd

import (
	"path/filepath"
	"strings"
)

// detectMissingPhaseDirs reports phase ids that exist as directories on
// origin/<mainBranch> but are absent from the current working tree. This
// closes the gap guardLiveState (switchbranch.go) leaves open: that guard
// only protects the single tracked state.json copy on legacy branches, but
// a checkout replacing the working tree can just as easily wipe any phase
// directory the target ref doesn't carry, with nothing catching it.
//
// A missing origin/<mainBranch> ref (no remote configured, never pushed) or
// one with no .dross/phases tree yet is not an error — there is nothing
// known to compare against, so nothing is flagged.
func detectMissingPhaseDirs(repoDir, root, mainBranch string) ([]string, error) {
	ref := "origin/" + mainBranch
	if !gitRefExists(repoDir, ref) {
		return nil, nil
	}
	out, err := gitTrim(repoDir, "ls-tree", "--name-only", "-d", ref+":.dross/phases")
	if err != nil {
		// The ref exists but carries no .dross/phases tree — nothing known.
		return nil, nil
	}
	if out == "" {
		return nil, nil
	}
	var missing []string
	for _, id := range strings.Split(out, "\n") {
		if id == "" {
			continue
		}
		if !isDir(filepath.Join(root, "phases", id)) {
			missing = append(missing, id)
		}
	}
	return missing, nil
}

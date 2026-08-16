package cmd

// `dross phase red-proof set` — the writer half of the red-proof pin.
//
// The pin is a machine artefact: something checks it on every run, so it is
// recorded through a verb that validates what it writes rather than by hand-
// editing changes.json. A hand-edited pin is how the c-5 one ended up naming a
// commit nobody else could reach — nothing was in a position to object.

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/argfence"
	"github.com/Rivil/dross/internal/changes"
	"github.com/Rivil/dross/internal/phase"
)

func phaseRedProof() *cobra.Command {
	c := &cobra.Command{
		Use:   "red-proof",
		Short: "Record the commit a phase's red proof is pinned to",
	}
	c.AddCommand(phaseRedProofSet())
	c.AddCommand(phaseRedProofRepoint())
	return c
}

func phaseRedProofSet() *cobra.Command {
	var sha, doc, replay string
	c := &cobra.Command{
		Use:   "set <phase-id>",
		Short: "Pin a phase's red proof to a commit and the doc that replays it",
		Long: "Record the commit a phase's red proof was captured at, plus the\n" +
			"repo-relative path of the doc whose `base commit:` line replays it.\n" +
			"`dross doctor` then checks that commit is still reachable from origin.\n" +
			"--replay records the command that replays the proof, so a repoint can\n" +
			"re-run it at the proposed commit instead of guessing.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			phaseID := args[0]
			root, err := FindRoot()
			if err != nil {
				return err
			}
			repoDir := filepath.Dir(root)

			if !isDir(phase.Dir(root, phaseID)) {
				return fmt.Errorf("no phase %q under %s — a pin keyed to a phase that does not exist names no fork point to repoint to", phaseID, filepath.Join(root, "phases"))
			}
			full, err := resolvePinnedCommit(repoDir, sha)
			if err != nil {
				return err
			}
			cleanDoc, err := checkRedProofDoc(repoDir, doc)
			if err != nil {
				return err
			}
			cleanReplay, err := checkRedProofReplay(replay, cmd.Flags().Changed("replay"))
			if err != nil {
				return err
			}

			// Load-set-save, so the phase's base, fork point, PR, status and
			// task records all survive: this verb pins a proof, it does not
			// author the record.
			path := changes.FilePath(root, phaseID)
			ch, err := changes.Load(path, phaseID)
			if err != nil {
				return err
			}
			// A set that does not mention --replay leaves a recorded command
			// alone. Re-pinning a proof and silently dropping the only thing
			// that can verify the next repoint would be a quiet downgrade —
			// the pin still checks out, but nothing can be checked against it.
			if !cmd.Flags().Changed("replay") && ch.RedProof != nil {
				cleanReplay = ch.RedProof.Replay
			}
			ch.RedProof = &changes.RedProof{SHA: full, Doc: cleanDoc, Replay: cleanReplay}
			if err := ch.Save(path); err != nil {
				return err
			}
			Printf("pinned %s's red proof to %s (%s)\n", phaseID, short(full), cleanDoc)
			if cleanReplay != "" {
				Printf("  replay: %s\n", cleanReplay)
			}
			return nil
		},
	}
	c.Flags().StringVar(&sha, "sha", "", "the commit the red proof was captured at")
	c.Flags().StringVar(&doc, "doc", "", "repo-relative path of the doc that replays the proof")
	c.Flags().StringVar(&replay, "replay", "", "command that replays the proof, run at the proposed commit when the pin is repointed")
	_ = c.MarkFlagRequired("sha")
	_ = c.MarkFlagRequired("doc")
	return c
}

// resolvePinnedCommit refuses anything that is not a commit in this repo and
// expands it to the full SHA. Abbreviations are convenient to type and a bad
// thing to store: they grow ambiguous as history grows, and the doc this pin is
// cross-checked against carries the full one.
func resolvePinnedCommit(repoDir, sha string) (string, error) {
	sha = strings.TrimSpace(sha)
	if sha == "" {
		return "", fmt.Errorf("--sha is required")
	}
	if err := validateGitRef("red-proof commit", sha); err != nil {
		return "", err
	}
	full, err := gitTrim(repoDir, gitRefArgs("rev-parse", []string{"--verify", "--quiet"}, sha+"^{commit}")...)
	if err != nil || full == "" {
		return "", fmt.Errorf("%s does not resolve to a commit in this repo — pin the commit the proof was actually captured at", sha)
	}
	return full, nil
}

// checkRedProofReplay validates the replay command before it reaches the
// record. Two refusals, both about what a repoint would later do with it:
//
//   - A blank command reads as "recorded" to the verified/unverified split but
//     would be spawned as nothing, so a repoint would claim the proof was
//     re-checked when it ran no proof at all.
//   - A command beginning with "-" is refused because it is ultimately handed
//     to `sh -c`, which honours no end-of-options token — argfence's reject
//     side of the table, not its fence side.
//
// An absent --replay is not an empty one: it means "leave whatever is
// recorded", which the caller handles.
func checkRedProofReplay(replay string, provided bool) (string, error) {
	if !provided {
		return "", nil
	}
	clean := strings.TrimSpace(replay)
	if clean == "" {
		return "", fmt.Errorf("--replay is empty — a blank command would be recorded as a replay and then spawned as nothing; omit the flag if there is no replay")
	}
	if err := argfence.RejectLeadingDash("sh", "red-proof replay command", clean); err != nil {
		return "", err
	}
	return clean, nil
}

// checkRedProofDoc refuses a doc path that does not resolve to a file inside
// the repo, and returns it slash-normalised for storage. A pin naming a doc
// that isn't there is a pin no reader can follow.
func checkRedProofDoc(repoDir, doc string) (string, error) {
	doc = strings.TrimSpace(doc)
	if doc == "" {
		return "", fmt.Errorf("--doc is required")
	}
	if filepath.IsAbs(doc) {
		return "", fmt.Errorf("--doc %q must be repo-relative: an absolute path is one machine's layout, not a path the next reader has", doc)
	}
	clean := filepath.Clean(doc)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("--doc %q escapes the repository", doc)
	}
	if isDir(filepath.Join(repoDir, clean)) {
		return "", fmt.Errorf("--doc %q is a directory, not the doc that replays the proof", doc)
	}
	if !fileExists(filepath.Join(repoDir, clean)) {
		return "", fmt.Errorf("--doc %q does not exist under %s", doc, repoDir)
	}
	return filepath.ToSlash(clean), nil
}

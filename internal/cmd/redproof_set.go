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

	"github.com/Rivil/dross/internal/changes"
	"github.com/Rivil/dross/internal/phase"
)

func phaseRedProof() *cobra.Command {
	c := &cobra.Command{
		Use:   "red-proof",
		Short: "Record the commit a phase's red proof is pinned to",
	}
	c.AddCommand(phaseRedProofSet())
	return c
}

func phaseRedProofSet() *cobra.Command {
	var sha, doc string
	c := &cobra.Command{
		Use:   "set <phase-id>",
		Short: "Pin a phase's red proof to a commit and the doc that replays it",
		Long: "Record the commit a phase's red proof was captured at, plus the\n" +
			"repo-relative path of the doc whose `base commit:` line replays it.\n" +
			"`dross doctor` then checks that commit is still reachable from origin.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
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

			// Load-set-save, so the phase's base, fork point, PR, status and
			// task records all survive: this verb pins a proof, it does not
			// author the record.
			path := changes.FilePath(root, phaseID)
			ch, err := changes.Load(path, phaseID)
			if err != nil {
				return err
			}
			ch.RedProof = &changes.RedProof{SHA: full, Doc: cleanDoc}
			if err := ch.Save(path); err != nil {
				return err
			}
			Printf("pinned %s's red proof to %s (%s)\n", phaseID, short(full), cleanDoc)
			return nil
		},
	}
	c.Flags().StringVar(&sha, "sha", "", "the commit the red proof was captured at")
	c.Flags().StringVar(&doc, "doc", "", "repo-relative path of the doc that replays the proof")
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

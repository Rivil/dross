package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/architecture"
)

// Architecture registers `dross architecture {check}` — inspect and repair the
// symbol links in ARCHITECTURE.md.
func Architecture() *cobra.Command {
	c := &cobra.Command{
		Use:   "architecture",
		Short: "Inspect and repair ARCHITECTURE.md symbol links",
	}
	c.AddCommand(architectureCheck())
	return c
}

func architectureCheck() *cobra.Command {
	var fix bool
	c := &cobra.Command{
		Use:   "check",
		Short: "Report stale ARCHITECTURE.md symbol links; --fix repoints moved ones",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			repoDir := filepath.Dir(root)
			path := filepath.Join(repoDir, architecture.File)
			body, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read %s: %w", architecture.File, err)
			}
			content := string(body)

			var moved, unresolved, ambiguous int
			updated := content
			for _, r := range architecture.ResolveAllIn(content, repoDir) {
				switch r.Status {
				case architecture.StatusMoved:
					moved++
					if fix {
						updated = rewriteMovedLine(updated, r)
					} else {
						Printf("  moved      %s — %s:%d → :%d\n", r.Link.Symbol, r.Link.File, r.Link.Line, r.NewLine)
					}
				case architecture.StatusUnresolved:
					unresolved++
					Printf("  unresolved %s — %s:%d (no such symbol — left as-is)\n", r.Link.Symbol, r.Link.File, r.Link.Line)
				case architecture.StatusAmbiguous:
					ambiguous++
					Printf("  ambiguous  %s — %s:%d (multiple matches — left as-is)\n", r.Link.Symbol, r.Link.File, r.Link.Line)
				}
			}

			if fix {
				if updated != content {
					if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
						return fmt.Errorf("write %s: %w", architecture.File, err)
					}
					Printf("Repointed %d moved link(s) in %s.\n", moved, architecture.File)
				} else {
					Print("No moved links to repoint.")
				}
				if unresolved+ambiguous > 0 {
					Printf("%d link(s) need manual attention — unresolved/ambiguous are never auto-repointed.\n", unresolved+ambiguous)
				}
				return nil
			}

			// Report-only: never writes.
			if moved+unresolved+ambiguous == 0 {
				Print("All ARCHITECTURE.md symbol links resolve.")
				return nil
			}
			Printf("%d moved, %d unresolved, %d ambiguous. Run with --fix to repoint moved links.\n", moved, unresolved, ambiguous)
			return nil
		},
	}
	c.Flags().BoolVar(&fix, "fix", false, "rewrite moved links' :line in place (unresolved/ambiguous left untouched)")
	return c
}

// rewriteMovedLine repoints exactly the `:line` suffix of one Moved bullet,
// leaving every other byte of the document identical. It swaps `file:oldline`
// for `file:newline` inside that single bullet's original text, then replaces
// just that bullet line back into the document — a deleted/renamed symbol
// (Unresolved) or a duplicate name (Ambiguous) never reaches here, so it is
// never repointed to a guessed line.
func rewriteMovedLine(content string, r architecture.Resolution) string {
	oldLoc := fmt.Sprintf("%s:%d", r.Link.File, r.Link.Line)
	newLoc := fmt.Sprintf("%s:%d", r.Link.File, r.NewLine)
	newBullet := strings.Replace(r.Link.Raw, oldLoc, newLoc, 1)
	if newBullet == r.Link.Raw {
		return content // location text not found in the bullet — leave untouched
	}
	return strings.Replace(content, r.Link.Raw, newBullet, 1)
}

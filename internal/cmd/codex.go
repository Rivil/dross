package cmd

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/codex"
)

// Codex registers `dross codex <files...>`. Prints a compact rendering
// of symbols, cross-file references, sibling files, and recent git
// activity for the given target files. Designed to be piped into the
// LLM's context as ambient code awareness.
func Codex() *cobra.Command {
	return &cobra.Command{
		Use:   "codex <file> [file...]",
		Short: "Polyglot code insight — symbols, refs, siblings, recent activity",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("at least one target file is required")
			}
			res, err := codex.Index(args)
			if err != nil {
				return err
			}
			renderCodex(res)
			return nil
		},
	}
}

func renderCodex(res *codex.Result) {
	Printf("# codex — %d target file(s)\n", len(res.TargetFiles))
	for _, f := range res.TargetFiles {
		Printf("  %s\n", f)
	}
	Print("")

	if len(res.Symbols) > 0 {
		Print("## symbols")
		for _, s := range res.Symbols {
			Printf("  %s:%d  %s  %s\n", s.File, s.Line, s.Kind, s.Name)
		}
		Print("")
	}

	if len(res.Callers) > 0 {
		Print("## refs (best-effort cross-file mentions)")
		for _, c := range res.Callers {
			Printf("  %s:%d  → %s\n", c.File, c.Line, c.Name)
		}
		Print("")
	}

	if len(res.Siblings) > 0 {
		Print("## siblings")
		for _, s := range res.Siblings {
			Printf("  %s\n", s)
		}
		Print("")
	}

	if len(res.RecentLog) > 0 {
		Print("## recent activity")
		for _, l := range res.RecentLog {
			Printf("  %s\n", l)
		}
		Print("")
	}

	if len(res.Errors) > 0 {
		Print("## errors (non-fatal)")
		for _, e := range res.Errors {
			Printf("  %s\n", e)
		}
	}
}

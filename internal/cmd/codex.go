package cmd

import (
	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/codex"
)

// Codex is a stub for v0. Wire-up only — the real tree-sitter-backed
// indexer is on the roadmap.
func Codex() *cobra.Command {
	return &cobra.Command{
		Use:   "codex [files...]",
		Short: "Polyglot code insight (stub — not implemented yet)",
		RunE: func(_ *cobra.Command, args []string) error {
			res, err := codex.Index(args)
			if err != nil {
				return err
			}
			Printf("(codex stub) target files: %v\n", res.TargetFiles)
			Print("Real indexer will print symbols / callers / sibling patterns / recent log here.")
			return nil
		},
	}
}

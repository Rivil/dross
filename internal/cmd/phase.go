package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rivil/dross/internal/phase"
)

func Phase() *cobra.Command {
	c := &cobra.Command{
		Use:   "phase",
		Short: "Manage phase directories under .dross/phases/",
	}
	c.AddCommand(phaseList(), phaseCreate(), phaseShow())
	return c
}

func phaseList() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List phases",
		RunE: func(_ *cobra.Command, _ []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			ids, err := phase.List(root)
			if err != nil {
				return err
			}
			if len(ids) == 0 {
				Print("(no phases)")
				return nil
			}
			for _, id := range ids {
				Print(id)
			}
			return nil
		},
	}
}

// phaseCreate makes the directory NN-slug. Spec/plan are written by
// /dross-spec and /dross-plan slash commands.
func phaseCreate() *cobra.Command {
	return &cobra.Command{
		Use:   "create <title>",
		Short: "Create the next phase directory (auto-numbered)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			title := strings.Join(args, " ")
			root, err := FindRoot()
			if err != nil {
				return err
			}
			n, err := nextPhaseNumber(root)
			if err != nil {
				return err
			}
			id := fmt.Sprintf("%02d-%s", n, phase.Slugify(title))
			dir := phase.Dir(root, id)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			Printf("created %s\n", dir)
			Print("Next: /dross-spec to write spec.toml, then /dross-plan")
			return nil
		},
	}
}

func phaseShow() *cobra.Command {
	return &cobra.Command{
		Use:   "show <phase-id>",
		Short: "Print the spec.toml and plan.toml for a phase",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			dir := phase.Dir(root, args[0])
			for _, name := range []string{"spec.toml", "plan.toml"} {
				path := filepath.Join(dir, name)
				b, err := os.ReadFile(path)
				if err != nil {
					Printf("# %s — (missing)\n\n", path)
					continue
				}
				Printf("# %s\n%s\n", path, string(b))
			}
			return nil
		},
	}
}

func nextPhaseNumber(root string) (int, error) {
	entries, err := os.ReadDir(filepath.Join(root, "phases"))
	if err != nil && !os.IsNotExist(err) {
		return 0, err
	}
	max := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		parts := strings.SplitN(e.Name(), "-", 2)
		if len(parts) < 1 {
			continue
		}
		n, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		if n > max {
			max = n
		}
	}
	sort.Ints([]int{max})
	return max + 1, nil
}

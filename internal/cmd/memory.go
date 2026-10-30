package cmd

import (
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/memadvise"
)

// Memory is the memory-prune advisor's command group: `advise` lists every
// suggestion `dross status` shows the top of; `apply` carries one out.
func Memory() *cobra.Command {
	c := &cobra.Command{
		Use:   "memory",
		Short: "Memory-prune advisor — suggest and apply prunes to this repo's Claude memory dir",
	}
	c.AddCommand(memoryAdvise(), memoryApply())
	return c
}

// memoryRepo is the repo top level the advisor resolves the memory dir from.
func memoryRepo() (string, error) {
	root, err := FindRoot()
	if err != nil {
		return "", err
	}
	return filepath.Dir(root), nil
}

func memoryAdvise() *cobra.Command {
	return &cobra.Command{
		Use:   "advise",
		Short: "List every memory-prune suggestion with its $/month, the index integrity and the turn share; read-only",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			repo, err := memoryRepo()
			if err != nil {
				return err
			}
			r, err := memadvise.Gather(repo, time.Now())
			if err != nil {
				return fmt.Errorf("memory advise: %w", err)
			}
			Printf("%s", memadvise.Render(r, 0))
			// Status stays quiet about a partial count; advise is where the
			// user asked, so a figure resting on skipped transcripts says so.
			if n := r.Turns.Unreadable; r.Found && n > 0 {
				projects := "<claude>/projects"
				if claude, err := memadvise.ClaudeDir(); err == nil {
					projects = filepath.Join(claude, "projects")
				}
				return fmt.Errorf("memory advise: %d transcript dir(s) or file(s) under %s could not be read; the turn counts above leave them out", n, projects)
			}
			return nil
		},
	}
}

func memoryApply() *cobra.Command {
	var yes bool
	c := &cobra.Command{
		Use:   "apply <id>",
		Short: "Carry out one memory-prune suggestion by its id (needs --yes); snapshots the memory dir first",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			id := args[0]
			if !yes {
				return fmt.Errorf("memory apply %s changes your Claude memory dir; re-run with --yes to confirm (dross memory apply %s --yes)", id, id)
			}
			repo, err := memoryRepo()
			if err != nil {
				return err
			}
			now := time.Now()
			r, err := memadvise.Gather(repo, now)
			if err != nil {
				return fmt.Errorf("memory apply: %w", err)
			}
			if !r.Found {
				return fmt.Errorf("memory apply: this repo has no Claude memory dir")
			}
			var matches []memadvise.Suggestion
			for _, s := range r.Suggestions {
				if s.ID == id {
					matches = append(matches, s)
				}
			}
			switch len(matches) {
			case 0:
				return fmt.Errorf("memory apply: %s is not a current suggestion; run `dross memory advise` for the list", id)
			case 1:
			default:
				return fmt.Errorf("memory apply: %s names %d suggestions; edit MEMORY.md by hand", id, len(matches))
			}
			s := matches[0]
			if s.ReportOnly {
				return fmt.Errorf("memory apply: %s is report-only — index %s in MEMORY.md or remove it by hand", id, s.File)
			}
			res, err := memadvise.Apply(repo, r.Dir, s, now)
			if res.Snapshot != "" {
				// Snapshot always lands directly under SnapshotsDir; naming it
				// that way keeps the line repo-relative without path math.
				Printf("snapshot: %s/%s\n", memadvise.SnapshotsDir, filepath.Base(res.Snapshot))
			}
			if err != nil {
				return fmt.Errorf("memory apply: %w", err)
			}
			Printf("applied %s: %s %s\n", s.ID, s.Action, s.File)
			printRecheck(res)
			return nil
		},
	}
	c.Flags().BoolVar(&yes, "yes", false, "confirm the change to the memory dir")
	return c
}

// printRecheck reports the index after the apply: what now points nowhere and
// what nothing points at, so a delete that left a dangling [[link]] says so.
func printRecheck(res memadvise.ApplyResult) {
	var dangling []string
	for slug := range res.Dangling {
		dangling = append(dangling, slug)
	}
	sort.Strings(dangling)
	Printf("recheck: %d dead index link(s), %d orphan(s), %d dangling [[link]](s)\n",
		len(res.DeadLinks), len(res.Orphans), len(dangling))
	for _, l := range res.DeadLinks {
		Printf("  dead index link: %s\n", l.Target)
	}
	for _, f := range res.Orphans {
		Printf("  orphan: %s\n", f)
	}
	for _, slug := range dangling {
		Printf("  dangling [[%s]] in %v\n", slug, res.Dangling[slug])
	}
}

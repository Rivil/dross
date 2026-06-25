package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/milestone"
	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/state"
)

// phaseMigrate converts a repo's legacy NN-slug phase identity to bare slugs:
// it renames each phases/NN-slug dir to phases/<slug>, rewrites the [phase].id
// in its spec.toml/plan.toml, and strips the NN- prefix from every milestone
// phases array entry. Order now lives solely in those arrays, so the ordinal
// prefix is redundant.
//
// Invariants:
//   - The in-flight phase (state.current_phase) is never touched — its dir, id,
//     and array entry stay as-is so its phase/<id> branch keeps resolving for
//     ship. It migrates on a later run, once it is no longer current.
//   - Idempotent: an already-bare tree is left byte-for-byte unchanged.
//   - Two legacy dirs that strip to the same slug are disambiguated (foo,
//     foo-2); a collision with a pre-existing unrelated dir is refused, never
//     overwritten.
//   - Git branches are never referenced.
func phaseMigrate() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Convert legacy NN-slug phases to bare-slug identity",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			s, err := state.Load(filepath.Join(root, state.File))
			if err != nil {
				return err
			}
			current := s.CurrentPhase

			phasesDir := filepath.Join(root, "phases")
			entries, err := os.ReadDir(phasesDir)
			if err != nil {
				if os.IsNotExist(err) {
					Print("no phases to migrate")
					return nil
				}
				return err
			}

			// Deterministic order so foo/foo-2 disambiguation is stable.
			var dirs []string
			for _, e := range entries {
				if e.IsDir() {
					dirs = append(dirs, e.Name())
				}
			}
			sort.Strings(dirs)

			renames := map[string]string{} // old dir id -> new slug
			claimed := map[string]bool{}   // slugs assigned this run
			for _, old := range dirs {
				if old == current {
					continue // never migrate the in-flight phase
				}
				base := phase.StripLegacyPrefix(old)
				if base == old {
					continue // already a bare slug (no NN- prefix)
				}
				slug := base
				switch {
				case claimed[base]:
					// Another migrated dir already took this slug — disambiguate.
					slug = freeSlug(phasesDir, base, claimed)
				case isDir(filepath.Join(phasesDir, base)):
					// A pre-existing, unrelated dir occupies the target. Refuse
					// rather than clobber — the user must resolve it.
					return fmt.Errorf("migrate: cannot rename %s -> %s: target already exists; resolve the collision manually", old, base)
				}
				if err := os.Rename(filepath.Join(phasesDir, old), filepath.Join(phasesDir, slug)); err != nil {
					return fmt.Errorf("rename %s -> %s: %w", old, slug, err)
				}
				if err := rewritePhaseID(filepath.Join(phasesDir, slug), slug); err != nil {
					return err
				}
				renames[old] = slug
				claimed[slug] = true
			}

			// Strip ordinal prefixes from milestone arrays. Use the rename map
			// where a dir was actually renamed (handles foo/foo-2), and fall
			// back to a plain strip for stale entries that have no dir.
			if versions, err := milestone.List(root); err == nil {
				for _, v := range versions {
					mPath := milestone.FilePath(root, v)
					m, err := milestone.Load(mPath)
					if err != nil {
						continue
					}
					changed := false
					for i, p := range m.Phases {
						if p == current {
							continue
						}
						if newSlug, ok := renames[p]; ok {
							if newSlug != p {
								m.Phases[i] = newSlug
								changed = true
							}
							continue
						}
						if stripped := phase.StripLegacyPrefix(p); stripped != p {
							m.Phases[i] = stripped
							changed = true
						}
					}
					if changed {
						if err := m.Save(mPath); err != nil {
							return fmt.Errorf("rewrite milestone %s: %w", v, err)
						}
					}
				}
			}

			if len(renames) == 0 {
				Print("phases already migrated — nothing to do")
				return nil
			}
			Printf("migrated %d phase(s) to bare-slug identity\n", len(renames))
			for _, old := range dirs {
				if slug, ok := renames[old]; ok {
					Printf("  %s -> %s\n", old, slug)
				}
			}
			return nil
		},
	}
}

// freeSlug returns base, or base-2/base-3/… — the first not on disk under
// phasesDir and not already claimed this run.
func freeSlug(phasesDir, base string, claimed map[string]bool) string {
	for i := 2; ; i++ {
		cand := fmt.Sprintf("%s-%d", base, i)
		if !claimed[cand] && !isDir(filepath.Join(phasesDir, cand)) {
			return cand
		}
	}
}

// rewritePhaseID sets [phase].id to slug in a phase dir's spec.toml and
// plan.toml, skipping whichever file is absent. validate requires plan.phase.id
// to match the dir name, so this is what keeps a migrated tree valid.
func rewritePhaseID(dir, slug string) error {
	specPath := filepath.Join(dir, "spec.toml")
	if isFile(specPath) {
		spec, err := phase.LoadSpec(specPath)
		if err != nil {
			return fmt.Errorf("load %s: %w", specPath, err)
		}
		spec.Phase.ID = slug
		if err := spec.Save(specPath); err != nil {
			return fmt.Errorf("save %s: %w", specPath, err)
		}
	}
	planPath := filepath.Join(dir, "plan.toml")
	if isFile(planPath) {
		plan, err := phase.LoadPlan(planPath)
		if err != nil {
			return fmt.Errorf("load %s: %w", planPath, err)
		}
		plan.Phase.ID = slug
		if err := plan.Save(planPath); err != nil {
			return fmt.Errorf("save %s: %w", planPath, err)
		}
	}
	return nil
}

// isFile reports whether path exists and is a regular file.
func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

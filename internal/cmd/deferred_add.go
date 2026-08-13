package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/state"
)

// deferredHome resolves where a newly filed item should live, per the locked
// storage_home decision: the current phase's spec.toml when a current phase is
// set AND that spec loads, otherwise the project-level store.
//
// The fallback covers an *unusable* home, not merely an absent one — a
// current_phase whose spec.toml is missing or unreadable falls back rather than
// erroring. That is deliberately unlike `survivor route`, which errors: the
// whole point of this verb is that filing a finding never fails for want of a
// home, so there is no arm here that returns "nowhere to put it".
//
// note is a human-readable line explaining a fallback, empty when the item
// landed in the current phase.
func deferredHome(root string) (path, source, note string, err error) {
	s, err := state.Load(filepath.Join(root, state.File))
	if err != nil {
		return deferredStorePath(root), projectStoreSlug,
			fmt.Sprintf("state.json unreadable (%v) — filed under %s", err, projectStoreSlug), nil
	}
	if s.CurrentPhase == "" {
		return deferredStorePath(root), projectStoreSlug,
			fmt.Sprintf("no current phase — filed under %s", projectStoreSlug), nil
	}
	specPath := filepath.Join(phase.Dir(root, s.CurrentPhase), "spec.toml")
	if _, err := phase.LoadSpec(specPath); err != nil {
		return deferredStorePath(root), projectStoreSlug,
			fmt.Sprintf("current phase %q has no loadable spec.toml — filed under %s", s.CurrentPhase, projectStoreSlug), nil
	}
	return specPath, s.CurrentPhase, "", nil
}

// mintDeferredID assigns an id unique across every deferred item in the project,
// so two items filed into the same phase can never collide onto one board link.
func mintDeferredID(root string) (string, error) {
	entries, err := collectDeferred(root)
	if err != nil {
		return "", err
	}
	used := map[string]bool{}
	for _, e := range entries {
		if e.ID != "" {
			used[e.ID] = true
		}
	}
	return newDeferredID(used)
}

// deferredAdd files a new deferred item from the command line — the verb that
// turns "I found something that isn't this phase's problem" into a tracked item
// in one command, instead of a bullet in a handoff note nothing ever reads.
func deferredAdd() *cobra.Command {
	var why, target string
	c := &cobra.Command{
		Use:   "add <text>",
		Short: "File a new deferred item (current phase, or the project store when there's no home)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			text := strings.TrimSpace(args[0])
			if text == "" {
				return fmt.Errorf("deferred text is required — an empty item says nothing about what was found")
			}
			root, err := FindRoot()
			if err != nil {
				return err
			}
			// Validate the destination before anything is written or pushed: an
			// item stamped with a typo'd target is a silently unreachable
			// finding, which is the exact failure this verb exists to prevent.
			if target != "" {
				if err := validDeferredTarget(root, target); err != nil {
					return err
				}
			}

			path, source, note, err := deferredHome(root)
			if err != nil {
				return err
			}
			spec, err := loadDeferredHome(root, path, source)
			if err != nil {
				return err
			}
			id, err := mintDeferredID(root)
			if err != nil {
				return err
			}
			spec.Deferred = append(spec.Deferred, phase.Deferred{
				ID:     id,
				Text:   text,
				Why:    why,
				Target: target,
			})
			idx := len(spec.Deferred) - 1
			if err := spec.Save(path); err != nil {
				return fmt.Errorf("save %s: %w", path, err)
			}

			if note != "" {
				Printf("note: %s\n", note)
			}
			dest := "(someday)"
			if target != "" {
				dest = target
			}
			Printf("added %s deferred[%d] → %s\n", source, idx, dest)

			// The local write above is authoritative; the mirror below is
			// best-effort and never turns a filed finding into a failed command
			// (locked board_push).
			mirrorDeferredAdd(root, deferredEntry{
				Source: source,
				Index:  idx,
				ID:     id,
				Text:   text,
				Why:    why,
				Target: target,
			})
			return nil
		},
	}
	c.Flags().StringVar(&why, "why", "", "why this was parked (optional)")
	c.Flags().StringVar(&target, "target", "", "route it to a destination phase slug in the same command")
	return c
}

// mirrorDeferredAdd pushes the just-filed item onto the issue board, so filing
// a finding and having it appear on the tracker are one command rather than two
// (c-6). It never returns an error: the locked board_push decision makes the
// local TOML authoritative, so a network blip warns and leaves the local write
// standing rather than failing the command that just recorded a real finding.
//
// A disabled board or an absent current milestone is a silent no-op — there is
// nothing to attach a backlog item to, and that is not a failure.
//
// The push is unconditional on --target, so a routed add IS mirrored. That is
// knowingly asymmetric with backlog-sync, which skips routed items: the issue is
// created once here and never updated by a later sync. The staleness is recorded
// in this phase's [[deferred]] and left to board-sync-truth.
func mirrorDeferredAdd(root string, d deferredEntry) {
	ctx, enabled, err := openBoard()
	if err != nil {
		Printf("warning: board mirror skipped — %v\n", err)
		return
	}
	if !enabled {
		return
	}
	s, err := state.Load(filepath.Join(root, state.File))
	if err != nil || s.CurrentMilestone == "" {
		return
	}
	it := deferredBacklogItem(d)
	// A brand-new item has no pre-id board link, so there is nothing to migrate
	// — and leaving the positional key set would let it consult a stale link
	// recorded for a long-gone item at the same index.
	it.legacyKey = ""
	if _, _, err := pushBacklogItems(ctx, s.CurrentMilestone, []backlogItem{it}); err != nil {
		Printf("warning: board mirror failed — %v\n", err)
		Printf("the item is filed locally; `dross issue backlog-sync %s` will mirror it later\n", s.CurrentMilestone)
	}
}

// loadDeferredHome reads the resolved home, treating an absent file as an empty
// one. The project store is created on first write, so its absence is the normal
// case rather than an error.
func loadDeferredHome(root, path, source string) (*phase.Spec, error) {
	if source == projectStoreSlug {
		return loadDeferredStore(root)
	}
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return phase.LoadSpec(path)
}

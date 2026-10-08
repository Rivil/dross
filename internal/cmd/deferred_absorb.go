package cmd

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/deferred"
	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/state"
)

// deferredAbsorb records that a spec criterion takes in a parked item routed to
// its phase (locked absorption_record). The criterion's `deferred` list of item
// ids is the one piece of evidence backlog sync and reap may close a routed
// item on: an absorbed item's card closes once the absorbing phase completes,
// and one nobody recorded stays open.
//
// The item is addressed by its `<source> <idx>` handle, as every deferred verb
// addresses it; the id it records is the item's stable internal identity, never
// printed (locked deferred_identity). An item filed before ids existed is
// stamped with one in its own spec first.
func deferredAbsorb() *cobra.Command {
	var criterion, phaseFlag string
	c := &cobra.Command{
		Use:   "absorb <source> <idx>",
		Short: "Record that a spec criterion absorbs a deferred item routed to its phase",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			if criterion == "" {
				return errors.New("--criterion is required: name the criterion that takes the item in")
			}
			root, err := FindRoot()
			if err != nil {
				return err
			}
			phaseID := phaseFlag
			if phaseID == "" {
				s, err := state.Load(filepath.Join(root, state.File))
				if err != nil {
					return err
				}
				phaseID = s.CurrentPhase
			}
			if phaseID == "" {
				return errors.New("no --phase and no current phase: name the phase whose criterion absorbs the item")
			}
			source := args[0]
			idx, err := strconv.Atoi(args[1])
			if err != nil {
				return fmt.Errorf("idx must be an integer: %w", err)
			}

			// Everything is checked before anything is written, so a refusal
			// leaves every spec byte-identical.
			src, srcPath, err := deferred.ResolveSource(root, source)
			if err != nil {
				return err
			}
			if err := deferred.Index(source, src, idx); err != nil {
				return err
			}
			item := src.Deferred[idx]
			switch {
			case item.Dismissed:
				return fmt.Errorf("%s deferred[%d] is dismissed — a dismissed item cannot be absorbed", source, idx)
			case item.Target == "":
				return fmt.Errorf("%s deferred[%d] is unrouted (someday) — route it to %s first", source, idx, phaseID)
			case item.Target != phaseID:
				return fmt.Errorf("%s deferred[%d] is routed to %s, not %s — only the phase an item is routed to can absorb it", source, idx, item.Target, phaseID)
			}
			specPath := filepath.Join(phase.Dir(root, phaseID), "spec.toml")
			spec := src
			if specPath != srcPath {
				if spec, err = phase.LoadSpec(specPath); err != nil {
					return err
				}
			}
			ci := slices.IndexFunc(spec.Criteria, func(c phase.Criterion) bool { return c.ID == criterion })
			if ci < 0 {
				return fmt.Errorf("%s has no criterion %s", specPath, criterion)
			}

			id := item.ID
			if id == "" {
				if id, err = mintDeferredID(root); err != nil {
					return err
				}
				src.Deferred[idx].ID = id
				if specPath != srcPath {
					if err := src.Save(srcPath); err != nil {
						return err
					}
				}
			}
			if slices.Contains(spec.Criteria[ci].Deferred, id) {
				Printf("%s deferred[%d] is already absorbed by %s %s\n", source, idx, phaseID, criterion)
				return nil
			}
			spec.Criteria[ci].Deferred = append(spec.Criteria[ci].Deferred, id)
			if err := spec.Save(specPath); err != nil {
				return err
			}
			Printf("absorbed %s deferred[%d] into %s %s\n", source, idx, phaseID, criterion)
			return nil
		},
	}
	c.Flags().StringVar(&criterion, "criterion", "", "criterion id that absorbs the item, e.g. c-3 (required)")
	c.Flags().StringVar(&phaseFlag, "phase", "", "phase whose spec absorbs the item (default: the current phase)")
	return c
}

package cmd

import (
	"github.com/spf13/cobra"
)

// deferredNewID mints a deferred-item id without filing anything. A spec being
// written by hand needs an id before the item exists — an [[edges]] record that
// defers a class names its [[deferred]] entry by id (locked deferred_link), and
// board sync only backfills ids long after the spec is written.
//
// It goes through mintDeferredID, not deferred.NewID, so the id is unique
// across every phase spec and the _project store — the same set `add` checks.
// The verb is read-only: nothing is written, not even the store.
func deferredNewID() *cobra.Command {
	return &cobra.Command{
		Use:   "new-id",
		Short: "Print a fresh deferred-item id, unique across the project (writes nothing)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			id, err := mintDeferredID(root)
			if err != nil {
				return err
			}
			// The bare id and nothing else, so a prompt can capture it verbatim.
			Print(id)
			return nil
		},
	}
}

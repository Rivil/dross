package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/findings"
)

// FindingsTool is the per-tool descriptor that parameterizes the shared
// `findings` command group. The security and quality wirings each supply one;
// the command logic here is tool-agnostic, so list/reconcile/state-set are
// implemented and tested once rather than mirrored across both audits.
type FindingsTool struct {
	// Name labels the tool in messages ("security" / "quality").
	Name string
	// StatePath returns the absolute path to the tool's durable state.toml,
	// given the resolved .dross root.
	StatePath func(root string) string
	// ItemsForRun loads a run dir's findings ledger into reconcile items plus
	// the run id — the input to `findings reconcile <run-dir>`.
	ItemsForRun func(runDir string) (items []findings.Item, runID string, err error)
	// ResolveID maps a per-run finding id (e.g. "f-3", from the most recent
	// run) to its Item, so `findings <id> --state` can derive the fingerprint.
	ResolveID func(root, id string) (findings.Item, error)
}

// newFindingsCmd builds the `findings` group: `findings list`, `findings
// reconcile <run-dir>`, and the bare `findings <id> --state ...` form for
// setting a finding's lifecycle state. The state-set is the group's own RunE so
// an id positional that matches no subcommand falls through to it.
func newFindingsCmd(tool FindingsTool) *cobra.Command {
	var state string
	c := &cobra.Command{
		Use:   "findings [id]",
		Short: "List tracked findings, set a finding's state, or reconcile a run against prior state",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return runFindingsSetState(tool, args[0], state)
		},
	}
	c.Flags().StringVar(&state, "state", "", "new lifecycle state: tracked|resolved|dismissed")
	c.AddCommand(newFindingsListCmd(tool), newFindingsReconcileCmd(tool))
	return c
}

// runFindingsSetState validates the requested state, resolves the per-run id to
// its fingerprint, and persists the new state under that fingerprint — so the
// decision survives even after the originating run dir is pruned.
func runFindingsSetState(tool FindingsTool, id, state string) error {
	st := findings.State(state)
	if !st.Valid() {
		return fmt.Errorf("invalid --state %q: want tracked, resolved, or dismissed", state)
	}
	root, err := FindRoot()
	if err != nil {
		return err
	}
	item, err := tool.ResolveID(root, id)
	if err != nil {
		return fmt.Errorf("resolve finding %q: %w", id, err)
	}
	fp := item.Fingerprint()
	path := tool.StatePath(root)
	store, err := findings.LoadStore(path)
	if err != nil {
		return err
	}
	rec, ok := store.Get(fp)
	if !ok {
		rec = findings.Record{Fingerprint: fp, Title: item.Title, File: item.File, Class: item.Class}
	}
	rec.State = st
	store.Put(rec)
	if err := findings.SaveStore(path, store); err != nil {
		return err
	}
	Printf("%s finding %s → %s\n", tool.Name, id, st)
	return nil
}

func newFindingsListCmd(tool FindingsTool) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List tracked findings and their current lifecycle state across runs",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			store, err := findings.LoadStore(tool.StatePath(root))
			if err != nil {
				return err
			}
			if len(store.Records) == 0 {
				Printf("no tracked %s findings\n", tool.Name)
				return nil
			}
			for _, r := range store.Records {
				marker := ""
				if r.Regressed {
					marker = "  [REGRESSED]"
				}
				Printf("%s  %-9s  %s  (%s)%s\n", r.Fingerprint, r.State, r.Title, r.File, marker)
			}
			return nil
		},
	}
}

func newFindingsReconcileCmd(tool FindingsTool) *cobra.Command {
	return &cobra.Command{
		Use:   "reconcile <run-dir>",
		Short: "Fold a completed run's findings against prior durable state (strictly post-scan)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			items, runID, err := tool.ItemsForRun(args[0])
			if err != nil {
				return err
			}
			path := tool.StatePath(root)
			store, err := findings.LoadStore(path)
			if err != nil {
				return err
			}
			res := findings.Reconcile(store, items, runID)
			if err := findings.SaveStore(path, store); err != nil {
				return err
			}
			Printf("%s reconcile %s: %d new, %d folded, %d regressed\n",
				tool.Name, runID, len(res.New), len(res.Folded), len(res.Regressed))
			return nil
		},
	}
}

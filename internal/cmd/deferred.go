package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/milestone"
	"github.com/Rivil/dross/internal/phase"
)

// deferredEntry is one [[deferred]] item flattened with its provenance: the
// originating phase (Source) and the position within that phase's [[deferred]]
// array (Index) — the stable handle `dross deferred route` addresses it by.
type deferredEntry struct {
	Source string `json:"source"`
	Index  int    `json:"index"`
	Text   string `json:"text"`
	Why    string `json:"why,omitempty"`
	Target string `json:"target,omitempty"`
}

// Deferred inspects and routes deferred items captured across phase specs.
func Deferred() *cobra.Command {
	c := &cobra.Command{
		Use:   "deferred",
		Short: "Inspect and route deferred items across phase specs",
	}
	c.AddCommand(deferredList(), deferredRoute())
	return c
}

// collectDeferred flattens every .dross/phases/*/spec.toml [[deferred]] entry,
// tagging each with its source phase and per-phase index.
func collectDeferred(root string) ([]deferredEntry, error) {
	ids, err := phase.List(root)
	if err != nil {
		return nil, err
	}
	entries := []deferredEntry{}
	for _, id := range ids {
		specPath := filepath.Join(phase.Dir(root, id), "spec.toml")
		if _, err := os.Stat(specPath); err != nil {
			continue // no spec yet
		}
		spec, err := phase.LoadSpec(specPath)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", specPath, err)
		}
		for i, d := range spec.Deferred {
			entries = append(entries, deferredEntry{
				Source: id,
				Index:  i,
				Text:   d.Text,
				Why:    d.Why,
				Target: d.Target,
			})
		}
	}
	return entries, nil
}

func deferredList() *cobra.Command {
	var (
		target    string
		someday   bool
		routed    bool
		milestVer string
		asJSON    bool
	)
	c := &cobra.Command{
		Use:   "list",
		Short: "List deferred items across phase specs (filter + JSON for triage)",
		RunE: func(_ *cobra.Command, _ []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			entries, err := collectDeferred(root)
			if err != nil {
				return err
			}

			// --milestone scopes to entries whose source phase sits in that
			// milestone's phases array.
			if milestVer != "" {
				inM := map[string]bool{}
				if m, err := milestone.Load(milestone.FilePath(root, milestVer)); err == nil {
					for _, ph := range m.Phases {
						inM[ph] = true
					}
				}
				entries = filterDeferred(entries, func(e deferredEntry) bool { return inM[e.Source] })
			}
			if someday {
				entries = filterDeferred(entries, func(e deferredEntry) bool { return e.Target == "" })
			}
			if routed {
				entries = filterDeferred(entries, func(e deferredEntry) bool { return e.Target != "" })
			}
			if target != "" {
				entries = filterDeferred(entries, func(e deferredEntry) bool { return e.Target == target })
			}

			if asJSON {
				out, err := json.Marshal(entries)
				if err != nil {
					return err
				}
				Print(string(out))
				return nil
			}
			if len(entries) == 0 {
				Print("(no deferred items)")
				return nil
			}
			Printf("%-24s %4s %-20s %s\n", "SOURCE", "IDX", "TARGET", "TEXT")
			for _, e := range entries {
				tgt := e.Target
				if tgt == "" {
					tgt = "(someday)"
				}
				Printf("%-24s %4d %-20s %s\n", e.Source, e.Index, tgt, e.Text)
			}
			return nil
		},
	}
	c.Flags().StringVar(&target, "target", "", "only items routed to this phase slug")
	c.Flags().BoolVar(&someday, "someday", false, "only unrouted (no target) items")
	c.Flags().BoolVar(&routed, "routed", false, "only routed (target set) items")
	c.Flags().StringVar(&milestVer, "milestone", "", "only items from phases in this milestone's phases array")
	c.Flags().BoolVar(&asJSON, "json", false, "emit a JSON array (for prompt consumption)")
	return c
}

func deferredRoute() *cobra.Command {
	var target string
	c := &cobra.Command{
		Use:   "route <phase> <idx>",
		Short: "Stamp a target on a phase's Nth deferred item",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			if target == "" {
				return fmt.Errorf("--target is required")
			}
			root, err := FindRoot()
			if err != nil {
				return err
			}
			phaseID := args[0]
			idx, err := strconv.Atoi(args[1])
			if err != nil {
				return fmt.Errorf("idx must be an integer: %w", err)
			}
			specPath := filepath.Join(phase.Dir(root, phaseID), "spec.toml")
			spec, err := phase.LoadSpec(specPath)
			if err != nil {
				return err
			}
			if idx < 0 || idx >= len(spec.Deferred) {
				return fmt.Errorf("deferred index %d out of range (phase %s has %d deferred item(s))", idx, phaseID, len(spec.Deferred))
			}
			spec.Deferred[idx].Target = target
			if err := spec.Save(specPath); err != nil {
				return err
			}
			Printf("routed %s deferred[%d] → %s\n", phaseID, idx, target)
			return nil
		},
	}
	c.Flags().StringVar(&target, "target", "", "destination phase slug to stamp (required)")
	return c
}

func filterDeferred(in []deferredEntry, keep func(deferredEntry) bool) []deferredEntry {
	out := []deferredEntry{}
	for _, e := range in {
		if keep(e) {
			out = append(out, e)
		}
	}
	return out
}

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
	// ID carries the item's stable internal identity (phase.Deferred.ID) for
	// in-process consumers — syncBacklog keys its board entry on it. It is
	// json:"-" on purpose: the locked deferred_identity decision keeps the id
	// out of every user-facing surface, so `deferred list --json` (which
	// prompts consume) shows only the `<source> <idx>` handle.
	ID        string `json:"-"`
	Text      string `json:"text"`
	Why       string `json:"why,omitempty"`
	Target    string `json:"target,omitempty"`
	Dismissed bool   `json:"dismissed,omitempty"`
	// Survivor is the survivor identity key when this entry is a routed
	// surviving mutant. omitempty so ordinary deferred items don't carry a
	// meaningless empty key through the JSON a prompt consumes.
	Survivor string `json:"survivor,omitempty"`
}

// Deferred inspects and routes deferred items captured across phase specs.
func Deferred() *cobra.Command {
	c := &cobra.Command{
		Use:   "deferred",
		Short: "Inspect and route deferred items across phase specs",
	}
	c.AddCommand(deferredList(), deferredRoute(), deferredDismiss(), deferredUnroute(), deferredAdd())
	return c
}

// collectDeferred flattens every .dross/phases/*/spec.toml [[deferred]] entry
// plus the project-level store, tagging each with its source and per-source
// index.
func collectDeferred(root string) ([]deferredEntry, error) {
	ids, err := phase.List(root)
	if err != nil {
		return nil, err
	}
	entries := []deferredEntry{}
	for _, id := range ids {
		// A phases/_project directory would collide with the reserved store
		// slug, making `_project 0` ambiguous between two sources. Skip it here
		// and report it as a validation problem instead (validate.go).
		if id == projectStoreSlug {
			continue
		}
		specPath := filepath.Join(phase.Dir(root, id), "spec.toml")
		if _, err := os.Stat(specPath); err != nil {
			continue // no spec yet
		}
		spec, err := phase.LoadSpec(specPath)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", specPath, err)
		}
		entries = append(entries, flattenDeferred(id, spec.Deferred)...)
	}
	store, err := loadDeferredStore(root)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", deferredStorePath(root), err)
	}
	entries = append(entries, flattenDeferred(projectStoreSlug, store.Deferred)...)
	return entries, nil
}

// flattenDeferred tags one source's [[deferred]] array with its provenance.
func flattenDeferred(source string, items []phase.Deferred) []deferredEntry {
	out := make([]deferredEntry, 0, len(items))
	for i, d := range items {
		out = append(out, deferredEntry{
			Source:    source,
			Index:     i,
			ID:        d.ID,
			Text:      d.Text,
			Why:       d.Why,
			Target:    d.Target,
			Dismissed: d.Dismissed,
			Survivor:  d.Survivor,
		})
	}
	return out
}

func deferredList() *cobra.Command {
	var (
		target    string
		someday   bool
		routed    bool
		dismissed bool
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
			// Dismissed items are hidden from every view unless --dismissed is
			// set; this also keeps --someday (no target) and the default unrouted
			// listing from surfacing a dismissed item.
			if dismissed {
				entries = filterDeferred(entries, func(e deferredEntry) bool { return e.Dismissed })
			} else {
				entries = filterDeferred(entries, func(e deferredEntry) bool { return !e.Dismissed })
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
				switch {
				case e.Dismissed:
					tgt = "(dismissed)"
				case tgt == "":
					tgt = "(someday)"
				}
				Printf("%-24s %4d %-20s %s\n", e.Source, e.Index, tgt, e.Text)
			}
			return nil
		},
	}
	c.Flags().StringVar(&target, "target", "", "only items routed to this phase slug")
	c.Flags().BoolVar(&someday, "someday", false, "only unrouted (no target, not dismissed) items")
	c.Flags().BoolVar(&routed, "routed", false, "only routed (target set) items")
	c.Flags().BoolVar(&dismissed, "dismissed", false, "only dismissed items (hidden from every other view)")
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
			// Validate before anything is read or written: a stamped typo is a
			// silently unreachable destination, and a rejected route must leave
			// every file byte-identical.
			if err := validDeferredTarget(root, target); err != nil {
				return err
			}
			phaseID := args[0]
			idx, err := strconv.Atoi(args[1])
			if err != nil {
				return fmt.Errorf("idx must be an integer: %w", err)
			}
			spec, specPath, err := resolveDeferredSource(root, phaseID)
			if err != nil {
				return err
			}
			if err := deferredIndex(phaseID, spec, idx); err != nil {
				return err
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

func deferredDismiss() *cobra.Command {
	var undo bool
	c := &cobra.Command{
		Use:   "dismiss <phase> <idx>",
		Short: "Retire a phase's Nth deferred item to a dismissed state (someday-only; --undo to reverse)",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			phaseID := args[0]
			idx, err := strconv.Atoi(args[1])
			if err != nil {
				return fmt.Errorf("idx must be an integer: %w", err)
			}
			spec, specPath, err := resolveDeferredSource(root, phaseID)
			if err != nil {
				return err
			}
			if err := deferredIndex(phaseID, spec, idx); err != nil {
				return err
			}
			d := &spec.Deferred[idx]
			if undo {
				d.Dismissed = false
				if err := spec.Save(specPath); err != nil {
					return err
				}
				Printf("undismissed %s deferred[%d] → someday\n", phaseID, idx)
				return nil
			}
			// Someday-only: dismiss and route are distinct intents, so a routed
			// item must be un-routed before it can be dismissed.
			if d.Target != "" {
				return fmt.Errorf("deferred %s[%d] is routed to %q; un-route before dismissing", phaseID, idx, d.Target)
			}
			d.Dismissed = true
			if err := spec.Save(specPath); err != nil {
				return err
			}
			Printf("dismissed %s deferred[%d]\n", phaseID, idx)
			return nil
		},
	}
	c.Flags().BoolVar(&undo, "undo", false, "clear the dismissed state (back to someday)")
	return c
}

// deferredUnroute is the inverse of route: it clears a routed item's Target back
// to "" (someday). It is the missing complement that lets a routed item be
// dropped without hand-editing the spec — `route --target ""` is rejected by
// design, so un-routing gets its own verb.
func deferredUnroute() *cobra.Command {
	c := &cobra.Command{
		Use:   "unroute <phase> <idx>",
		Short: "Clear a routed deferred item's target, returning it to someday",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			phaseID := args[0]
			idx, err := strconv.Atoi(args[1])
			if err != nil {
				return fmt.Errorf("idx must be an integer: %w", err)
			}
			spec, specPath, err := resolveDeferredSource(root, phaseID)
			if err != nil {
				return err
			}
			if err := deferredIndex(phaseID, spec, idx); err != nil {
				return err
			}
			d := &spec.Deferred[idx]
			// Dismissed items already have an empty Target, so the someday check
			// below would mis-report them as "already someday". Catch them first
			// and point at the right verb — routing and dismissal are distinct
			// intents (see deferredDismiss).
			if d.Dismissed {
				return fmt.Errorf("deferred %s[%d] is dismissed; use 'dismiss --undo' to revive it, not unroute", phaseID, idx)
			}
			if d.Target == "" {
				Printf("%s deferred[%d] already someday — nothing to un-route\n", phaseID, idx)
				return nil
			}
			d.Target = ""
			if err := spec.Save(specPath); err != nil {
				return err
			}
			Printf("unrouted %s deferred[%d] → someday\n", phaseID, idx)
			return nil
		},
	}
	return c
}

// resolveDeferredSource loads the [[deferred]]-carrying file for a source slug
// and returns it with its path. It is what makes `_project <idx>` behave
// identically to `<phase> <idx>` in route, unroute and dismiss: every verb
// resolves through deferredStore rather than building a phases/<id>/spec.toml
// path of its own, so the project store is a first-class source rather than a
// place only `add` can write.
func resolveDeferredSource(root, source string) (*phase.Spec, string, error) {
	path, err := deferredStore(root, source)
	if err != nil {
		return nil, "", err
	}
	if source == projectStoreSlug {
		spec, err := loadDeferredStore(root)
		return spec, path, err
	}
	spec, err := phase.LoadSpec(path)
	return spec, path, err
}

// deferredIndex bounds-checks an index against a source's items, naming the
// source and its count. Sources are interchangeable here on purpose: a store
// index out of range reads the same as a phase index out of range.
func deferredIndex(source string, spec *phase.Spec, idx int) error {
	if idx < 0 || idx >= len(spec.Deferred) {
		return fmt.Errorf("deferred index %d out of range (%s has %d deferred item(s))", idx, source, len(spec.Deferred))
	}
	return nil
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

// repointDeferredTarget rewrites every [[deferred]] entry across all phase specs
// AND the project store whose Target is oldSlug to newSlug, so a
// `dross phase rename` doesn't leave a dangling routing target. Entries pointing
// at any other slug are left exactly as they are.
//
// The store is walked alongside the specs on purpose: it holds real routed items
// now, and a walk over phase.List alone would leave every one of them pointing
// at the dead slug.
func repointDeferredTarget(root, oldSlug, newSlug string) error {
	ids, err := phase.List(root)
	if err != nil {
		return err
	}
	paths := []string{}
	for _, id := range ids {
		if id == projectStoreSlug {
			continue // reserved; validate reports the directory as a problem
		}
		specPath := filepath.Join(phase.Dir(root, id), "spec.toml")
		if _, err := os.Stat(specPath); err != nil {
			continue // no spec yet
		}
		paths = append(paths, specPath)
	}
	if storePath := deferredStorePath(root); fileExists(storePath) {
		paths = append(paths, storePath)
	}
	for _, specPath := range paths {
		spec, err := phase.LoadSpec(specPath)
		if err != nil {
			return fmt.Errorf("%s: %w", specPath, err)
		}
		changed := false
		for i := range spec.Deferred {
			if spec.Deferred[i].Target == oldSlug {
				spec.Deferred[i].Target = newSlug
				changed = true
			}
		}
		if changed {
			if err := spec.Save(specPath); err != nil {
				return fmt.Errorf("save %s: %w", specPath, err)
			}
		}
	}
	return nil
}

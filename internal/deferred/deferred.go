// Package deferred is the ledger of [[deferred]] items captured across phase
// specs and the project-level store: how they are collected, addressed by
// `<source> <idx>`, given stable ids, filtered and re-pointed. The cobra verbs
// that expose it live in internal/cmd; nothing here knows about a command.
package deferred

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Rivil/dross/internal/phase"
)

// ProjectStoreSlug is the reserved source slug for the project-level deferred
// store. It is the home an item lands in when no phase spec can hold it — no
// current phase, or a current phase whose spec.toml is missing or unreadable —
// so filing a finding never fails for want of a home (the locked storage_home
// decision).
//
// The leading underscore is what makes the slug safe to reserve: phase.Slugify
// only ever emits [a-z0-9-] runes, so no phase title can ever be slugified into
// `_project` and shadow the store.
const ProjectStoreSlug = "_project"

// Entry is one [[deferred]] item flattened with its provenance: the
// originating phase (Source) and the position within that phase's [[deferred]]
// array (Index) — the stable handle `dross deferred route` addresses it by.
type Entry struct {
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

// StorePath returns the project-level store's file: .dross/deferred.toml.
// The file is phase.Spec-shaped, so phase.LoadSpec / Spec.Save read and write it
// unchanged and every [[deferred]] field behaves identically on both arms.
func StorePath(root string) string {
	return filepath.Join(root, "deferred.toml")
}

// Store resolves a deferred source slug to the file holding its [[deferred]]
// array: the project store for `_project`, otherwise that phase's spec.toml.
// Every verb builds its path through here, which is what makes
// `_project <idx>` addressable exactly like `<phase> <idx>`.
//
// A slug with no phase directory is an error rather than a creatable path — a
// typo must not silently mint a new home under phases/.
func Store(root, source string) (string, error) {
	if source == ProjectStoreSlug {
		return StorePath(root), nil
	}
	dir := phase.Dir(root, source)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("unknown deferred source %q: no phase directory at %s (and %q is the only non-phase source)", source, dir, ProjectStoreSlug)
	}
	return filepath.Join(dir, "spec.toml"), nil
}

// LoadStore reads the project store, returning an empty spec when the file
// doesn't exist yet — the store is created on first write, not on init, so a
// project that never files a homeless item never grows the file.
func LoadStore(root string) (*phase.Spec, error) {
	path := StorePath(root)
	if _, err := os.Stat(path); err != nil {
		return &phase.Spec{Phase: phase.SpecPhase{ID: ProjectStoreSlug, Title: "project-level deferred store"}}, nil
	}
	spec, err := phase.LoadSpec(path)
	if err != nil {
		return nil, err
	}
	return spec, nil
}

// Collect flattens every .dross/phases/*/spec.toml [[deferred]] entry plus the
// project-level store, tagging each with its source and per-source index.
func Collect(root string) ([]Entry, error) {
	ids, err := phase.List(root)
	if err != nil {
		return nil, err
	}
	entries := []Entry{}
	for _, id := range ids {
		// A phases/_project directory would collide with the reserved store
		// slug, making `_project 0` ambiguous between two sources. Skip it here
		// and report it as a validation problem instead (validate.go).
		if id == ProjectStoreSlug {
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
		entries = append(entries, flatten(id, spec.Deferred)...)
	}
	store, err := LoadStore(root)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", StorePath(root), err)
	}
	entries = append(entries, flatten(ProjectStoreSlug, store.Deferred)...)
	return entries, nil
}

// flatten tags one source's [[deferred]] array with its provenance.
func flatten(source string, items []phase.Deferred) []Entry {
	out := make([]Entry, 0, len(items))
	for i, d := range items {
		out = append(out, Entry{
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

// ResolveSource loads the [[deferred]]-carrying file for a source slug and
// returns it with its path. It is what makes `_project <idx>` behave
// identically to `<phase> <idx>` in route, unroute and dismiss: every verb
// resolves through Store rather than building a phases/<id>/spec.toml path of
// its own, so the project store is a first-class source rather than a place
// only `add` can write.
func ResolveSource(root, source string) (*phase.Spec, string, error) {
	path, err := Store(root, source)
	if err != nil {
		return nil, "", err
	}
	if source == ProjectStoreSlug {
		spec, err := LoadStore(root)
		return spec, path, err
	}
	spec, err := phase.LoadSpec(path)
	return spec, path, err
}

// Index bounds-checks an index against a source's items, naming the source and
// its count. Sources are interchangeable here on purpose: a store index out of
// range reads the same as a phase index out of range.
func Index(source string, spec *phase.Spec, idx int) error {
	if idx < 0 || idx >= len(spec.Deferred) {
		return fmt.Errorf("deferred index %d out of range (%s has %d deferred item(s))", idx, source, len(spec.Deferred))
	}
	return nil
}

// Filter keeps the entries keep accepts, preserving order.
func Filter(in []Entry, keep func(Entry) bool) []Entry {
	out := []Entry{}
	for _, e := range in {
		if keep(e) {
			out = append(out, e)
		}
	}
	return out
}

// RepointTarget rewrites every [[deferred]] entry across all phase specs AND
// the project store whose Target is oldSlug to newSlug, so a `dross phase
// rename` doesn't leave a dangling routing target. Entries pointing at any
// other slug are left exactly as they are.
//
// The store is walked alongside the specs on purpose: it holds real routed items
// now, and a walk over phase.List alone would leave every one of them pointing
// at the dead slug.
func RepointTarget(root, oldSlug, newSlug string) error {
	ids, err := phase.List(root)
	if err != nil {
		return err
	}
	paths := []string{}
	for _, id := range ids {
		if id == ProjectStoreSlug {
			continue // reserved; validate reports the directory as a problem
		}
		specPath := filepath.Join(phase.Dir(root, id), "spec.toml")
		if _, err := os.Stat(specPath); err != nil {
			continue // no spec yet
		}
		paths = append(paths, specPath)
	}
	if storePath := StorePath(root); fileExists(storePath) {
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

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// LegacyBacklogKey is the positional key dross used before ids existed.
func LegacyBacklogKey(source string, idx int) string {
	return fmt.Sprintf("someday:%s#%d", source, idx)
}

// NewID mints a stable id for a deferred item, retrying on collision with an
// already-assigned one. A collision would silently merge two items' board
// links, so it is checked rather than assumed away.
func NewID(used map[string]bool) (string, error) {
	for attempt := 0; attempt < 8; attempt++ {
		var b [8]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "", fmt.Errorf("generate deferred id: %w", err)
		}
		id := hex.EncodeToString(b[:])
		if !used[id] {
			return id, nil
		}
	}
	return "", fmt.Errorf("could not mint a unique deferred id after 8 attempts")
}

// EnsureIDs backfills an id into every [[deferred]] item that lacks one,
// writing it back to the owning spec or the project store, and returns the
// re-collected entries. Specs authored before ids existed are id-less, so the
// first sync after an upgrade is what gives them a durable identity; a second
// run must find them already stamped and churn nothing.
func EnsureIDs(root string) ([]Entry, error) {
	entries, err := Collect(root)
	if err != nil {
		return nil, err
	}
	used := map[string]bool{}
	missing := map[string][]int{}
	for _, e := range entries {
		if e.ID != "" {
			used[e.ID] = true
			continue
		}
		missing[e.Source] = append(missing[e.Source], e.Index)
	}
	if len(missing) == 0 {
		return entries, nil
	}
	for source, idxs := range missing {
		path, err := Store(root, source)
		if err != nil {
			return nil, err
		}
		spec, err := phase.LoadSpec(path)
		if err != nil {
			return nil, err
		}
		for _, i := range idxs {
			if i < 0 || i >= len(spec.Deferred) {
				continue
			}
			id, err := NewID(used)
			if err != nil {
				return nil, err
			}
			spec.Deferred[i].ID = id
			used[id] = true
		}
		if err := spec.Save(path); err != nil {
			return nil, fmt.Errorf("save %s: %w", path, err)
		}
	}
	return Collect(root)
}

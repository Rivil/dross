package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Rivil/dross/internal/phase"
)

// projectStoreSlug is the reserved source slug for the project-level deferred
// store. It is the home an item lands in when no phase spec can hold it — no
// current phase, or a current phase whose spec.toml is missing or unreadable —
// so filing a finding never fails for want of a home (the locked storage_home
// decision).
//
// The leading underscore is what makes the slug safe to reserve: phase.Slugify
// only ever emits [a-z0-9-] runes, so no phase title can ever be slugified into
// `_project` and shadow the store.
const projectStoreSlug = "_project"

// deferredStorePath returns the project-level store's file: .dross/deferred.toml.
// The file is phase.Spec-shaped, so phase.LoadSpec / Spec.Save read and write it
// unchanged and every [[deferred]] field behaves identically on both arms.
func deferredStorePath(root string) string {
	return filepath.Join(root, "deferred.toml")
}

// deferredStore resolves a deferred source slug to the file holding its
// [[deferred]] array: the project store for `_project`, otherwise that phase's
// spec.toml. Every verb builds its path through here, which is what makes
// `_project <idx>` addressable exactly like `<phase> <idx>`.
//
// A slug with no phase directory is an error rather than a creatable path — a
// typo must not silently mint a new home under phases/.
func deferredStore(root, source string) (string, error) {
	if source == projectStoreSlug {
		return deferredStorePath(root), nil
	}
	dir := phase.Dir(root, source)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("unknown deferred source %q: no phase directory at %s (and %q is the only non-phase source)", source, dir, projectStoreSlug)
	}
	return filepath.Join(dir, "spec.toml"), nil
}

// loadDeferredStore reads the project store, returning an empty spec when the
// file doesn't exist yet — the store is created on first write, not on init, so
// a project that never files a homeless item never grows the file.
func loadDeferredStore(root string) (*phase.Spec, error) {
	path := deferredStorePath(root)
	if _, err := os.Stat(path); err != nil {
		return &phase.Spec{Phase: phase.SpecPhase{ID: projectStoreSlug, Title: "project-level deferred store"}}, nil
	}
	spec, err := phase.LoadSpec(path)
	if err != nil {
		return nil, err
	}
	return spec, nil
}

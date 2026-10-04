package boardsync

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"

	"github.com/Rivil/dross/internal/changes"
	"github.com/Rivil/dross/internal/deferred"
	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/survivor"
	"github.com/Rivil/dross/internal/verify"
)

// Disposed reports whether a routed item has a disposition record on disk, and
// names the record when it does. It is the one rule backlog sync and reap close
// a routed mirror on (c-6): never on the destination finishing alone — a route
// can land on a phase that already shipped, or a target can be rescoped away
// from an item, and both left feastahead with 527 cards closed on no evidence
// (47a5c93).
//
// It reads disk only and takes no board client, so no verdict can come from a
// card's own state. Anything it cannot read is "not disposed": an unreadable
// record is not evidence.
//
// A routed survivor is disposed when survivors.toml accepts its key, or when the
// destination's own verify run measured its file and no longer lists it — the
// run finalized, newer than the run that found it, its leg free of error, the
// file inside its scope, and the key in neither its survivors nor its
// out-of-scope list. Mutation runs are diff-scoped, so plain absence from a run
// that never mutated the file proves nothing.
//
// Any other routed item is disposed when its destination is complete and one of
// that destination's criteria absorbed it (locked absorption_record).
func Disposed(root string, e deferred.Entry) (bool, string) {
	if e.Survivor != "" {
		return survivorDisposed(root, e)
	}
	return absorbedDisposed(root, e)
}

// survivorText is the text `dross survivor route` files a routed survivor with.
var survivorText = regexp.MustCompile(`^survivor (.+):(\d+) \(([^)]+)\)$`)

func survivorDisposed(root string, e deferred.Entry) (bool, string) {
	if store, err := survivor.Load(survivor.Path(root)); err == nil {
		if _, ok := store.Get(e.Survivor); ok {
			return true, fmt.Sprintf("%s accepts survivor %s", survivor.StoreFile, e.Survivor)
		}
	}
	if e.Target == "" {
		return false, ""
	}
	m := survivorText.FindStringSubmatch(e.Text)
	if m == nil {
		return false, ""
	}
	file, op := m[1], m[3]

	targetDir := phase.Dir(root, e.Target)
	run, err := verify.LoadTests(filepath.Join(targetDir, verify.TestsFile))
	if err != nil || run == nil {
		return false, ""
	}
	v, err := verify.LoadVerify(filepath.Join(targetDir, verify.VerifyFile))
	if err != nil || v == nil || !v.Verify.Finalized {
		return false, ""
	}
	// The run that found the survivor: a destination run from before it
	// cannot speak for a survivor it never saw. With no such run on disk the
	// order cannot be shown, and what cannot be shown is not evidence.
	found, err := verify.LoadTests(filepath.Join(phase.Dir(root, e.Source), verify.TestsFile))
	if err != nil || found == nil || !run.GeneratedAt.After(found.GeneratedAt) {
		return false, ""
	}
	if run.Scope == nil || !slices.Contains(run.Scope.Files, file) {
		return false, ""
	}
	leg := -1
	for i, l := range run.Languages {
		if slices.Contains(l.Files, file) {
			leg = i
			break
		}
	}
	if leg < 0 || run.Languages[leg].Error != "" || run.Languages[leg].Mutation == nil {
		return false, ""
	}
	for _, s := range run.Languages[leg].Mutation.Surviving {
		// A survivor whose identity did not resolve carries no key. One in
		// the same file under the same operator may be this very mutant, so
		// it counts as still surviving rather than as gone.
		if s.Key == e.Survivor || (s.Key == "" && s.File == file && s.Op == op) {
			return false, ""
		}
	}
	for _, s := range run.OutOfScope {
		if s.Key == e.Survivor {
			return false, ""
		}
	}
	return true, fmt.Sprintf("phases/%s/%s measured %s and no longer lists survivor %s", e.Target, verify.TestsFile, file, e.Survivor)
}

func absorbedDisposed(root string, e deferred.Entry) (bool, string) {
	if e.ID == "" || e.Target == "" || !changes.Complete(root, e.Target) {
		return false, ""
	}
	spec, err := phase.LoadSpec(filepath.Join(phase.Dir(root, e.Target), "spec.toml"))
	if err != nil {
		return false, ""
	}
	for _, c := range spec.Criteria {
		if slices.Contains(c.Deferred, e.ID) {
			return true, fmt.Sprintf("phases/%s/spec.toml criterion %s absorbed it", e.Target, c.ID)
		}
	}
	return false, ""
}

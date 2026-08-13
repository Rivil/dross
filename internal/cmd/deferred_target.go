package cmd

import (
	"fmt"

	"github.com/Rivil/dross/internal/milestone"
	"github.com/Rivil/dross/internal/phase"
)

// deferredTargetSet builds the set of slugs a deferred item may legitimately be
// routed to: any existing phase directory, plus any slug parked in ANY
// milestone's phases array. The second arm matters — routing a finding at a
// phase scoped in an earlier, still-open milestone is legitimate, and a
// current-milestone-only rule would reject it.
//
// The reserved project-store slug is excluded: the store is a home for items,
// never a destination for them.
//
// Errors are swallowed rather than propagated, matching what `dross validate`
// has always done: an unreadable milestone simply contributes no slugs, and
// validate reports the underlying fault through its own checks.
func deferredTargetSet(root string) map[string]bool {
	valid := map[string]bool{}
	ids, err := phase.List(root)
	if err == nil {
		for _, id := range ids {
			valid[id] = true
		}
	}
	if versions, err := milestone.List(root); err == nil {
		for _, v := range versions {
			if m, err := milestone.Load(milestone.FilePath(root, v)); err == nil {
				for _, ph := range m.Phases {
					valid[ph] = true
				}
			}
		}
	}
	delete(valid, projectStoreSlug)
	return valid
}

// validDeferredTarget is the single gate every stamping path goes through —
// `deferred route --target`, `deferred add --target`, and validate's
// dangling-target check all judge a slug by this one rule (the locked
// target_validation decision). Two implementations of "is this target real" is
// the hazard: a slug one path accepts and the other calls dangling is a
// silently unreachable destination.
//
// An unknown slug is an error naming both ways to make it valid. It is never
// auto-appended to a milestone's phases array — that would turn a typo into a
// permanent fake phase.
func validDeferredTarget(root, slug string) error {
	if deferredTargetSet(root)[slug] {
		return nil
	}
	return deferredTargetError(slug)
}

// deferredTargetError is the shared rejection. It is deliberately phrased
// without naming the verb that raised it, so `route --target typo` and
// `add --target typo` fail with byte-identical text and cannot drift apart.
func deferredTargetError(slug string) error {
	return fmt.Errorf("--target %q names no phase directory and no milestone phases entry; either scaffold a phase for it (dross phase create) or add the slug to a milestone's phases array", slug)
}

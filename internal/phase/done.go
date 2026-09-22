package phase

import (
	"os"
	"path/filepath"

	"github.com/Rivil/dross/internal/changes"
)

// This file is the single doneness reader. `dross status`, `dross milestone
// progress` and `dross phase list` all answer "is this phase done?" through
// Done, so the three surfaces cannot drift into three different answers over
// the same phase directory — the divergence v1.4 shipped with, where the
// status bar counted verify verdicts and milestone progress counted completion
// records.

// Done is the entry point: it resolves scaffolded-ness itself, so a caller
// holding only a slug never has to know that an unscaffolded slug is a separate
// arm. Callers already holding that answer (buildMilestoneProgress computes it
// for its own reporting) go straight to IsDone.
func Done(root, slug string) bool {
	return IsDone(root, slug, DirExists(root, slug))
}

// IsDone answers the locked phases_done_test: a slug counts done only when it
// has a phase directory AND that directory's own record says it finished.
//
// The sole authority is changes.json's status marker, written by `dross ship`,
// `dross phase complete` and `dross phase backfill`. There is deliberately no
// state-history fallback: history is a capped 50-entry window, not a record, so
// a phase read done off a breadcrumb silently flips back to not-done once fifty
// further actions age that breadcrumb out. Records written before the status
// field existed are closed by `dross phase backfill`, which writes the durable
// marker from commit evidence — a one-time sweep, not a permanent read path.
//
// An unscaffolded slug is never done. A roadmap entry with no phase directory
// is work that was listed and never built, and closing it would let a milestone
// finish with unbuilt criteria still on its roadmap.
//
// A verify verdict of "pass" is deliberately not evidence either: verified is
// not shipped, and a phase can pass verification and never open a PR.
func IsDone(root, slug string, scaffolded bool) bool {
	if !scaffolded {
		return false
	}
	c, err := changes.Load(changes.FilePath(root, slug), slug)
	if err != nil {
		return false
	}
	switch c.Status {
	case changes.StatusComplete, changes.StatusShipped:
		return true
	}
	return false
}

// DirExists reports whether the slug has a directory under .dross/phases/.
func DirExists(root, slug string) bool {
	fi, err := os.Stat(filepath.Join(root, "phases", slug))
	return err == nil && fi.IsDir()
}

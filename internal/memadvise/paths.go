package memadvise

// The advisor's own state lives under the repo's .dross dir, never under the
// Claude memory dir it reads: c-3 holds that `dross status` and `dross memory
// advise` leave the memory dir byte-identical, and the only safe way to keep a
// cache from landing there is never to build a path into it.
//
// The paths are repo-root-relative and slash-separated, the form the
// .gitignore seed matches against; callers join them with filepath.FromSlash.
const (
	// StateDir holds every advisor file. It is machine-local and gitignored.
	StateDir = ".dross/memory"
	// TurnCachePath is the per-transcript turn-count cache (locked turn_counts).
	TurnCachePath = StateDir + "/turns.json"
	// SnapshotsDir holds one copy of the memory dir per `dross memory apply`.
	SnapshotsDir = StateDir + "/snapshots"
)

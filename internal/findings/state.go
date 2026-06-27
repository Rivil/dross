package findings

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

// State is the durable, operator-assigned lifecycle state of a tracked finding.
// It is distinct from a finding's severity/risk: severity is how bad the issue
// is, State is what the human decided to do about it across runs.
type State string

const (
	StateTracked   State = "tracked"   // surfaced and carried forward; not yet triaged away
	StateResolved  State = "resolved"  // fixed; reappearance is a regression
	StateDismissed State = "dismissed" // intentionally accepted; folded out of "new" on re-run
)

// Valid reports whether s is one of the three known states. An empty or unknown
// state is rejected so the CLI can't persist garbage into the ledger.
func (s State) Valid() bool {
	switch s {
	case StateTracked, StateResolved, StateDismissed:
		return true
	}
	return false
}

// Record is one durable entry in the state store, keyed by Fingerprint. The
// display fields (Title/File/Class) are denormalized so `findings list` can
// render an entry without re-reading the originating run dir — which may have
// been pruned. LastRun is the run id in which the finding was last seen.
type Record struct {
	Fingerprint string `toml:"fingerprint"`
	State       State  `toml:"state"`
	Regressed   bool   `toml:"regressed"`
	Title       string `toml:"title"`
	File        string `toml:"file"`
	Class       string `toml:"class"`
	LastRun     string `toml:"last_run"`
}

// Store is the durable, gitignored state ledger (state.toml) keyed by
// fingerprint. It persists across runs separately from the timestamped per-run
// dirs, so state survives run-dir pruning.
//
// LastRun is the store-level timestamp of the area's most recent run — distinct
// from a Record's run-id LastRun. It is declared before Records so the TOML
// encoder emits the scalar before the [[finding]] array tables (a scalar after
// an array-of-tables header would be mis-attributed to the last table). A zero
// LastRun means the area has never run, the signal status uses to rank areas by
// staleness.
type Store struct {
	LastRun time.Time `toml:"last_run"`
	Records []Record  `toml:"finding"`
}

// NeverRun reports whether this area has no recorded run — its store-level
// LastRun is the zero time, which is the case for both a missing state.toml and
// a store that exists but was never stamped.
func (s *Store) NeverRun() bool { return s.LastRun.IsZero() }

// Get returns the record for a fingerprint and whether it was present.
func (s *Store) Get(fp string) (Record, bool) {
	for _, r := range s.Records {
		if r.Fingerprint == fp {
			return r, true
		}
	}
	return Record{}, false
}

// Put inserts r, or replaces the existing record with the same fingerprint —
// the fingerprint is the identity, so a finding is never stored twice.
func (s *Store) Put(r Record) {
	for i := range s.Records {
		if s.Records[i].Fingerprint == r.Fingerprint {
			s.Records[i] = r
			return
		}
	}
	s.Records = append(s.Records, r)
}

// LoadStore reads state.toml. A missing file is the first-run case and returns
// an empty store with no error; a present-but-garbled file returns an error
// (never a panic) so a corrupt ledger fails cleanly rather than crashing a run.
func LoadStore(path string) (*Store, error) {
	var s Store
	if _, err := toml.DecodeFile(path, &s); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Store{}, nil
		}
		return nil, fmt.Errorf("load state store %s: %w", path, err)
	}
	return &s, nil
}

// SaveStore writes the store to path atomically: it encodes to a temp file in
// the same directory and renames it over path. A failure during encode or write
// leaves the prior state.toml untouched (never truncated), so an interrupted
// save can't lose accumulated state.
func SaveStore(path string, s *Store) error {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(s); err != nil {
		return fmt.Errorf("encode state store: %w", err)
	}
	return saveAtomic(path, buf.Bytes())
}

// StampLastRun records that this area ran at now: it loads the store at path,
// sets the store-level LastRun, and saves it back — preserving every finding
// Record (a merge, not an overwrite). The first stamp on a missing state.toml
// creates it. This is the signal status reads to render "last run <when>".
func StampLastRun(path string, now time.Time) error {
	s, err := LoadStore(path)
	if err != nil {
		return err
	}
	s.LastRun = now
	return SaveStore(path, s)
}

// saveAtomic writes data to path via a same-directory temp file + rename, so a
// reader never observes a partial write. The temp file is cleaned up on any
// early return; after a successful rename the deferred remove is a harmless
// no-op (the temp name no longer exists).
func saveAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".state-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp for %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp for %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp for %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp into %s: %w", path, err)
	}
	return nil
}

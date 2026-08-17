package survivor

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// StoreFile is the store's name under .dross/. The locked acceptance_store
// decision puts it at the repo root rather than under a phase dir, and keeps it
// tracked: an acceptance is a durable engineering claim about the codebase, so
// it belongs in history where a reviewer sees the reason, and only a tracked
// file survives the phase branch's squash-merge.
const StoreFile = "survivors.toml"

// RootDirName is the directory the store lives in, relative to the repo root.
const RootDirName = ".dross"

// ErrNoStoreRoot reports that no .dross directory was found walking up from the
// start directory, so there is no repo-level store to resolve.
var ErrNoStoreRoot = errors.New("no .dross directory found")

// Acceptance is one accepted survivor: a survivor a human has declared
// permanently unkillable, with the reason why. It is keyed by the survivor's
// cross-run identity, one entry per accepted survivor — never a wildcard —
// so acceptance can never blanket-suppress a survivor that does not exist yet.
type Acceptance struct {
	// Key is the identity from KeyFor. It is the store's primary key.
	Key string `toml:"key"`

	// File, Op and Text denormalize the key's inputs so staleness detection
	// can look for the subject without re-deriving it from a line number
	// that has since moved, and so a reader can tell what an entry is about
	// without reversing a hash.
	File string `toml:"file"`
	Op   string `toml:"op"`
	Text string `toml:"text"`

	// Line is the line the survivor sat on when it was accepted. Purely
	// informational — nothing matches on it, by design (c-4).
	Line int `toml:"line,omitempty"`

	// Reason is this entry's own prose. Exactly one of Reason or Category is
	// persisted: an entry naming a Category carries no Reason of its own, so
	// shared prose is written once.
	Reason string `toml:"reason,omitempty"`

	// Category names a shared reason. The locked acceptance_granularity
	// decision keeps the entries individual and lets only the prose be
	// shared, so a bulk drain stays individually accounted.
	Category string `toml:"category,omitempty"`

	// AcceptedIn records the phase current when the acceptance was made.
	// Provenance only — the record deliberately outlives that phase (c-3).
	AcceptedIn string `toml:"accepted_in,omitempty"`
}

// Category is a shared reason, written once and referenced by name.
type Category struct {
	Name   string `toml:"name"`
	Reason string `toml:"reason"`
}

// Store is the repo-level accepted-survivor registry — .dross/survivors.toml.
// It holds accepted survivors only: the locked routed_state_source decision
// keeps routed survivors in the deferred machinery, because debt with a home
// should stay visible and only an accepted survivor earns silence.
//
// The store keys on an opaque string and never resolves identity itself, so it
// is usable — and testable — without the resolver.
type Store struct {
	Categories []Category   `toml:"category"`
	Accepted   []Acceptance `toml:"accepted"`
}

// Path returns the store's path given the .dross directory.
func Path(drossDir string) string { return filepath.Join(drossDir, StoreFile) }

// LocatePath walks up from startDir to the first .dross directory and returns
// the store path inside it. The store is repo-level, so running from a nested
// subdirectory must reach the same file the repo root would (c-3): a
// cwd-relative store would silently fork into one registry per working
// directory.
func LocatePath(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, RootDirName)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return Path(candidate), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ErrNoStoreRoot
		}
		dir = parent
	}
}

// Get returns the acceptance for a key and whether it was present.
func (s *Store) Get(key string) (Acceptance, bool) {
	for _, a := range s.Accepted {
		if a.Key == key {
			return a, true
		}
	}
	return Acceptance{}, false
}

// CategoryReason returns a named category's prose and whether it exists with
// prose. A category with an empty reason is treated as absent — it would
// otherwise be a reasonless acceptance wearing a name.
func (s *Store) CategoryReason(name string) (string, bool) {
	for _, c := range s.Categories {
		if c.Name == name && c.Reason != "" {
			return c.Reason, true
		}
	}
	return "", false
}

// ReasonFor resolves the effective reason for an acceptance: its own prose, or
// the prose of the category it names. An acceptance that resolves to nothing is
// an error — a suppression with no stated reason is the exact thing c-2 exists
// to prevent, so the failure is loud at both ends rather than a silent "".
func (s *Store) ReasonFor(a Acceptance) (string, error) {
	if a.Reason != "" {
		return a.Reason, nil
	}
	if a.Category == "" {
		return "", fmt.Errorf("survivor %s: acceptance has no reason and names no category", a.Key)
	}
	reason, ok := s.CategoryReason(a.Category)
	if !ok {
		return "", fmt.Errorf("survivor %s: category %q is missing or has no reason", a.Key, a.Category)
	}
	return reason, nil
}

// Add validates and upserts an acceptance in memory. The key is the identity,
// so adding a key already present replaces it rather than duplicating it.
//
// An acceptance naming a category AND carrying its own prose registers that
// prose on the category and stores only the reference, which is what keeps a
// shared reason encoded exactly once no matter how many entries share it.
func (s *Store) Add(a Acceptance) error {
	if a.Key == "" {
		return errors.New("acceptance has no survivor key")
	}
	switch {
	case a.Category != "" && a.Reason != "":
		s.putCategory(a.Category, a.Reason)
		a.Reason = ""
	case a.Category != "":
		if _, ok := s.CategoryReason(a.Category); !ok {
			return fmt.Errorf("category %q is missing or has no reason", a.Category)
		}
	case a.Reason == "":
		return fmt.Errorf("survivor %s: a reason is required to accept a survivor", a.Key)
	}
	for i := range s.Accepted {
		if s.Accepted[i].Key == a.Key {
			s.Accepted[i] = a
			return nil
		}
	}
	s.Accepted = append(s.Accepted, a)
	return nil
}

// Remove deletes the acceptance with key and reports whether it was present.
//
// A category left with no members is dropped in the same pass: an orphaned
// [[category]] block is prose nothing references, and leaving it behind would
// let a later acceptance silently inherit a reason that was written — and
// reviewed — for an entry that has since been retired.
func (s *Store) Remove(key string) bool {
	idx := -1
	for i := range s.Accepted {
		if s.Accepted[i].Key == key {
			idx = i
			break
		}
	}
	if idx < 0 {
		return false
	}
	s.Accepted = append(s.Accepted[:idx], s.Accepted[idx+1:]...)
	s.pruneCategories()
	return true
}

// pruneCategories drops every category no remaining acceptance references.
func (s *Store) pruneCategories() {
	used := make(map[string]bool, len(s.Accepted))
	for _, a := range s.Accepted {
		if a.Category != "" {
			used[a.Category] = true
		}
	}
	kept := s.Categories[:0]
	for _, c := range s.Categories {
		if used[c.Name] {
			kept = append(kept, c)
		}
	}
	s.Categories = kept
}

func (s *Store) putCategory(name, reason string) {
	for i := range s.Categories {
		if s.Categories[i].Name == name {
			s.Categories[i].Reason = reason
			return
		}
	}
	s.Categories = append(s.Categories, Category{Name: name, Reason: reason})
}

// Load reads the store. A missing file is the first-run case and yields an
// empty store with no error; every other failure is an error, never a silently
// empty store — a store that fails open would re-flood the backlog the drain
// exists to shrink, or worse, be re-saved without the entries it failed to read.
//
// Validation runs at load as well as at write: a hand-edited reason = "" entry
// must not load and silently suppress. The store is a tracked, hand-editable
// file, so the write path is not the only way in.
func Load(path string) (*Store, error) {
	var s Store
	if _, err := toml.DecodeFile(path, &s); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Store{}, nil
		}
		return nil, fmt.Errorf("load survivors %s: %w", path, err)
	}
	if err := s.validate(); err != nil {
		return nil, fmt.Errorf("load survivors %s: %w", path, err)
	}
	return &s, nil
}

func (s *Store) validate() error {
	seen := make(map[string]bool, len(s.Accepted))
	for _, a := range s.Accepted {
		if a.Key == "" {
			return errors.New("acceptance has no survivor key")
		}
		if seen[a.Key] {
			return fmt.Errorf("duplicate survivor key %s", a.Key)
		}
		seen[a.Key] = true
		if _, err := s.ReasonFor(a); err != nil {
			return err
		}
	}
	return nil
}

// Save writes the store atomically: encode to a temp file in the same directory,
// then rename over path. A failed encode or write leaves the prior file
// untouched rather than truncated, so an interrupted save cannot lose
// acceptances that took real judgement to record.
func Save(path string, s *Store) error {
	if err := s.validate(); err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	enc.Indent = "  "
	if err := enc.Encode(s); err != nil {
		return fmt.Errorf("encode survivors: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	return saveAtomic(path, buf.Bytes())
}

// Accept is the read-modify-write the CLI uses: load the store at path, add the
// acceptance, save it back. Entries the caller never touched are preserved, and
// a rejected acceptance writes nothing at all — the file is byte-identical after
// a failed accept.
func Accept(path string, a Acceptance) error {
	s, err := Load(path)
	if err != nil {
		return err
	}
	if err := s.Add(a); err != nil {
		return err
	}
	return Save(path, s)
}

// Retire is the read-modify-write behind `dross survivor retire`: load the store
// at path, drop every named key, save it back. It is the CLI's way out of the
// store, so retiring an acceptance never means hand-editing survivors.toml.
//
// Every key is checked BEFORE any is removed, so a multi-key invocation naming
// one absent key writes nothing at all: a half-applied retirement would leave a
// store nobody asked for and no way to tell which half landed.
func Retire(path string, keys ...string) error {
	if len(keys) == 0 {
		return errors.New("no acceptance keys given to retire")
	}
	s, err := Load(path)
	if err != nil {
		return err
	}
	for _, k := range keys {
		if _, ok := s.Get(k); !ok {
			return fmt.Errorf("no acceptance with key %s", k)
		}
	}
	for _, k := range keys {
		s.Remove(k)
	}
	return Save(path, s)
}

// saveAtomic writes data to path via a same-directory temp file + rename, so a
// reader never observes a partial write. The temp file is removed on any early
// return; after a successful rename the deferred remove is a harmless no-op.
func saveAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".survivors-*.tmp")
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

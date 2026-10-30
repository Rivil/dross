package memadvise

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// CacheTTL is how long a turn count is reused before the transcripts are
// read again (locked turn_counts). Transcripts run to gigabytes across every
// project, so status reads them at most once a day, and then only the bytes
// appended since.
const CacheTTL = 24 * time.Hour

// turnCacheVersion is bumped whenever turnCache changes shape; a cache from
// another version is recounted rather than misread.
const turnCacheVersion = 1

// stateIgnore is what .dross/memory/.gitignore holds. A repo onboarded before
// the root .gitignore seed covered .dross/memory/ would otherwise stage the
// advisor's state with its next `git add .dross`, so the dir ignores itself.
const stateIgnore = "# machine-local dross memory-advisor state — never committed\n*\n"

// turnCache is .dross/memory/turns.json.
type turnCache struct {
	Version   int                  `json:"version"`
	CountedAt time.Time            `json:"counted_at"`
	Files     map[string]fileEntry `json:"files"`
}

// Turns returns the assistant-turn counts for the repo whose top level is
// root: its own, every project's, and how old they are. Under CacheTTL the
// cached counts are reused without touching a transcript; past it, only the
// bytes appended since are read and the cache is rewritten.
//
// The cache lives at <root>/.dross/memory/turns.json, never under the Claude
// memory dir (c-3). A failed cache write still returns the fresh counts, with
// the error beside them.
func Turns(root string, now time.Time) (TurnCounts, error) {
	claude, err := ClaudeDir()
	if err != nil {
		return TurnCounts{}, err
	}
	slug := Slug(root)
	cache, ok := loadCache(filepath.Join(root, filepath.FromSlash(TurnCachePath)), now)
	if ok && now.Sub(cache.CountedAt) < CacheTTL {
		repo, all := tally(cache.Files, slug, now)
		return TurnCounts{Repo: repo, All: all, Age: now.Sub(cache.CountedAt)}, nil
	}
	var prev map[string]fileEntry
	if ok {
		prev = cache.Files
	}
	files, scanned, unreadable := recount(filepath.Join(claude, "projects"), prev, now)
	repo, all := tally(files, slug, now)
	counts := TurnCounts{Repo: repo, All: all, Scanned: scanned, Unreadable: unreadable}
	return counts, saveCache(root, turnCache{Version: turnCacheVersion, CountedAt: now, Files: files})
}

// loadCache reads the cache at path. ok is false for anything not to be
// trusted: missing, undecodable, another version, dated after now (a clock
// that ran ahead would otherwise freeze the counts until it caught up), or an
// entry no scan could have produced. An untrusted cache is recounted from
// nothing — none of its entries are reused either.
func loadCache(path string, now time.Time) (turnCache, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return turnCache{}, false
	}
	var c turnCache
	if err := json.Unmarshal(b, &c); err != nil {
		return turnCache{}, false
	}
	if c.Version != turnCacheVersion || c.CountedAt.IsZero() || c.CountedAt.After(now) {
		return turnCache{}, false
	}
	for _, e := range c.Files {
		if e.Offset < 0 || e.Offset > e.Size {
			return turnCache{}, false
		}
		for _, n := range e.Days {
			if n < 0 {
				return turnCache{}, false
			}
		}
	}
	if c.Files == nil {
		c.Files = map[string]fileEntry{}
	}
	return c, true
}

// saveCache writes c to <root>/.dross/memory/turns.json atomically, after
// making sure the dir ignores itself.
func saveCache(root string, c turnCache) error {
	dir := filepath.Join(root, filepath.FromSlash(StateDir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", StateDir, err)
	}
	// MkdirAll is satisfied by a symlink to a dir; a linked state dir could
	// land the cache anywhere — the memory dir included — so it is refused.
	info, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("stat %s: %w", StateDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory; refusing to write advisor state through it", StateDir)
	}
	if err := ensureStateIgnored(dir); err != nil {
		return err
	}
	data, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("encode %s: %w", TurnCachePath, err)
	}
	return writeAtomic(filepath.Join(root, filepath.FromSlash(TurnCachePath)), data)
}

// ensureStateIgnored writes dir/.gitignore unless it already holds exactly
// stateIgnore, and refuses one that is not a regular file — a directory or a
// symlink there must not stand in for the ignore.
func ensureStateIgnored(dir string) error {
	p := filepath.Join(dir, ".gitignore")
	info, err := os.Lstat(p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return fmt.Errorf("read %s/.gitignore: %w", StateDir, err)
	case !info.Mode().IsRegular():
		return fmt.Errorf("%s/.gitignore is not a regular file; it must be the `*` ignore that keeps advisor state out of git", StateDir)
	default:
		if b, err := os.ReadFile(p); err == nil && string(b) == stateIgnore {
			return nil
		}
	}
	if err := os.WriteFile(p, []byte(stateIgnore), 0o644); err != nil {
		return fmt.Errorf("write %s/.gitignore: %w", StateDir, err)
	}
	return nil
}

// writeAtomic replaces path with data through a temp file and a rename, so a
// reader — or a status run racing this one — never sees half a cache.
func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return fmt.Errorf("close %s: %w", path, err)
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return fmt.Errorf("rename %s: %w", path, err)
	}
	return nil
}

package memadvise

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// TurnWindow is how far back an assistant turn counts toward the $/month
// estimate (c-2: this repo's turns in the last 30 days). It is also the mtime
// cut-off: a transcript last written before it cannot hold a turn inside it,
// so it is never opened.
const TurnWindow = 30 * 24 * time.Hour

// tailIDs is how many of a file's most recent message ids its cache entry
// keeps. Claude Code writes one line per content block, so one turn is several
// lines sharing a message.id; an incremental scan that resumes between two of
// them must still see the earlier half. The lines of one turn sit together,
// with at most a tool result or two between them, so a short tail bridges the
// resume point without the cache carrying every id a transcript ever held.
const tailIDs = 32

// markLen is how many bytes at each end of a file's read prefix its Mark
// covers. Two 4 KiB reads per changed transcript are noise next to the scan.
const markLen = 4 << 10

// fileEntry is what the cache keeps per transcript: the stat it was last read
// at, how far it was read, and the turns found so far bucketed by UTC day.
type fileEntry struct {
	Size    int64 `json:"size"`
	ModTime int64 `json:"mtime"` // unix nanoseconds
	// Offset is the byte just past the last complete line read. A trailing
	// line with no newline yet is still being written: it may be counted, but
	// the next scan re-reads it, and Tail keeps it from counting twice.
	Offset int64 `json:"offset"`
	// Mark fingerprints the bytes before Offset (see mark). An append leaves
	// them as they were; a transcript rewritten in place to the same or a
	// larger size does not, and the stat alone cannot tell the two apart.
	Mark uint64         `json:"mark,omitempty"`
	Days map[string]int `json:"days,omitempty"`
	Tail []string       `json:"tail,omitempty"`
}

// TurnCounts is the turn count the $/month estimate multiplies by.
type TurnCounts struct {
	Repo int // this repo's assistant turns in the window
	All  int // every project's assistant turns in the window
	// Age is how old the counts are: zero when this call recounted, the
	// cache's age when it reused one. Status prints it (locked turn_counts).
	Age time.Duration
	// Scanned is the transcript bytes this call scanned for turns — zero on
	// a cache hit, only the appended bytes on an incremental recount. The
	// few bytes read to check a Mark are not scanned and do not count.
	Scanned int64
	// Unreadable is how many transcripts or project dirs could not be read.
	// They are skipped rather than failing the count, and the cache keeps the
	// number, so a reused count is as partial as the one it was saved from.
	Unreadable int
}

// RepoShare is this repo's fraction of all turns. It is display only: the
// estimate uses Repo, never the share (locked cost_model).
func (c TurnCounts) RepoShare() float64 {
	if c.All == 0 {
		return 0
	}
	return float64(c.Repo) / float64(c.All)
}

// transcriptLine is the slice of a transcript line a turn is judged by.
type transcriptLine struct {
	Type        string `json:"type"`
	IsSidechain bool   `json:"isSidechain"`
	Timestamp   string `json:"timestamp"`
	Message     struct {
		ID    string `json:"id"`
		Model string `json:"model"`
	} `json:"message"`
}

// syntheticModel is the model Claude Code stamps on lines it writes itself
// (an interrupted request, an API error) rather than receives from the API.
const syntheticModel = "<synthetic>"

// windowStart is the first UTC day inside the window ending at now. Day
// buckets are compared as strings: the DateOnly layout sorts as it reads.
func windowStart(now time.Time) string {
	return now.Add(-TurnWindow).UTC().Format(time.DateOnly)
}

// recount brings every transcript under projectsDir up to date against prev,
// returning the new per-file entries keyed "<slug>/<file>.jsonl". Only the
// top level of each project dir is read: subagents/ and every other subdir
// hold sidechain transcripts, which are not turns of the main loop.
func recount(projectsDir string, prev map[string]fileEntry, now time.Time) (map[string]fileEntry, int64, int) {
	next := map[string]fileEntry{}
	var scanned int64
	unreadable := 0
	projects, err := os.ReadDir(projectsDir)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			unreadable++
		}
		return next, 0, unreadable
	}
	cutoff := now.Add(-TurnWindow)
	start := windowStart(now)
	for _, p := range projects {
		if !p.IsDir() {
			continue
		}
		dir := filepath.Join(projectsDir, p.Name())
		files, err := os.ReadDir(dir)
		if err != nil {
			unreadable++
			continue
		}
		for _, f := range files {
			if !f.Type().IsRegular() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			info, err := f.Info()
			if err != nil {
				unreadable++
				continue
			}
			// Untouched for the whole window: every turn it holds is older
			// than the window, so it is not even opened.
			if info.ModTime().Before(cutoff) {
				continue
			}
			key := p.Name() + "/" + f.Name()
			old, had := prev[key]
			e, n, err := update(filepath.Join(dir, f.Name()), old, had, info, start)
			scanned += n
			if err != nil {
				unreadable++
				continue
			}
			next[key] = e
		}
	}
	return next, scanned, unreadable
}

// update brings one entry up to date with the file's current stat. A file the
// cache has not seen is read from 0; an unchanged one is not opened; a shrunk
// one was rewritten and is recounted from 0. Any other change is read from
// Offset only when the bytes before it still match Mark — a file rewritten in
// place to the same or a larger size fails that check and is recounted from 0
// too, rather than resumed at an offset that no longer means anything.
func update(path string, e fileEntry, had bool, info os.FileInfo, start string) (fileEntry, int64, error) {
	size, mtime := info.Size(), info.ModTime().UnixNano()
	switch {
	case !had || size < e.Offset:
		e = fileEntry{}
	case size == e.Size && mtime == e.ModTime:
		return prune(e, start), 0, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return e, 0, err
	}
	defer f.Close()
	if e.Offset > 0 {
		m, err := mark(f, e.Offset)
		if err != nil {
			return e, 0, err
		}
		if m != e.Mark {
			e = fileEntry{}
		}
	}
	e.Size, e.ModTime = size, mtime
	n, err := scan(f, &e, start)
	if err == nil {
		e.Mark, err = mark(f, e.Offset)
	}
	return prune(e, start), n, err
}

// mark fingerprints the first off bytes of f by hashing their first and last
// markLen bytes; 64 bits of the hash are plenty to tell a rewrite from an
// append. Appending to f never changes it; rewriting what was already read
// almost always does, unless the rewrite leaves both ends byte-identical. It
// reads with ReadAt, so f's own offset is left where it was.
func mark(f *os.File, off int64) (uint64, error) {
	h := sha256.New()
	head := min(off, markLen)
	tail := max(head, off-markLen)
	for _, span := range [][2]int64{{0, head}, {tail, off}} {
		if _, err := io.Copy(h, io.NewSectionReader(f, span[0], span[1]-span[0])); err != nil {
			return 0, err
		}
	}
	return binary.BigEndian.Uint64(h.Sum(nil)), nil
}

// scan reads f from e.Offset up to e.Size, adding each new turn to e. It
// reads through a bufio.Reader rather than a Scanner: a tool result is one
// line and can run to megabytes, past any Scanner's token cap.
func scan(f *os.File, e *fileEntry, start string) (int64, error) {
	if _, err := f.Seek(e.Offset, io.SeekStart); err != nil {
		return 0, err
	}
	// Read only what the stat saw, so Size and Offset describe one snapshot
	// even while Claude Code appends.
	r := bufio.NewReaderSize(io.LimitReader(f, e.Size-e.Offset), 64<<10)
	seen := map[string]bool{}
	for _, id := range e.Tail {
		seen[id] = true
	}
	if e.Days == nil {
		e.Days = map[string]int{}
	}
	var read int64
	for {
		line, err := r.ReadBytes('\n')
		read += int64(len(line))
		if len(line) > 0 {
			countLine(line, e, seen, start)
		}
		if err == nil {
			e.Offset += int64(len(line))
			continue
		}
		if errors.Is(err, io.EOF) {
			return read, nil
		}
		return read, err
	}
}

// assistantMark is a cheap pre-filter: a line without it cannot be an
// assistant turn, so it is never handed to the JSON decoder.
var assistantMark = []byte(`"assistant"`)

// countLine adds line to e when it is a new main-loop assistant turn. A line
// that does not decode is skipped: one torn write must not cost the rest.
func countLine(line []byte, e *fileEntry, seen map[string]bool, start string) {
	if !bytes.Contains(line, assistantMark) {
		return
	}
	var l transcriptLine
	if err := json.Unmarshal(line, &l); err != nil {
		return
	}
	if l.Type != "assistant" || l.IsSidechain || l.Message.Model == syntheticModel || l.Message.ID == "" {
		return
	}
	if seen[l.Message.ID] {
		return
	}
	ts, err := time.Parse(time.RFC3339Nano, l.Timestamp)
	if err != nil {
		return
	}
	seen[l.Message.ID] = true
	e.Tail = append(e.Tail, l.Message.ID)
	if len(e.Tail) > tailIDs {
		e.Tail = e.Tail[len(e.Tail)-tailIDs:]
	}
	if day := ts.UTC().Format(time.DateOnly); day >= start {
		e.Days[day]++
	}
}

// prune drops the day buckets that have left the window, so the cache only
// ever holds about 30 small counts per live transcript.
func prune(e fileEntry, start string) fileEntry {
	for day := range e.Days {
		if day < start {
			delete(e.Days, day)
		}
	}
	return e
}

// tally sums the in-window buckets: this repo's (files under slug) and all.
func tally(files map[string]fileEntry, slug string, now time.Time) (repo, all int) {
	start := windowStart(now)
	for key, e := range files {
		n := 0
		for day, c := range e.Days {
			if day >= start {
				n += c
			}
		}
		all += n
		if strings.HasPrefix(key, slug+"/") {
			repo += n
		}
	}
	return repo, all
}

package memadvise

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ApplyResult is what an apply did and what the index looks like after it.
type ApplyResult struct {
	Snapshot  string              // the copy taken before anything changed
	DeadLinks []IndexLine         // index lines that point at no memory, after
	Orphans   []string            // memories no index line points at, after
	Dangling  map[string][]string // [[links]] to a slug no memory declares, after
}

// Apply carries out one suggestion against the memory dir: a delete removes
// the memory and its index line (a dead link's index line alone); a reword
// replaces that line's hook. It is the only code that writes the memory dir,
// and it is reached only through a confirmed `dross memory apply` (c-9).
//
// In order: refuse report-only advice; read the index as the plan; snapshot
// the dir, aborting if that fails; re-check the guard on the live file; make
// sure the index is still the one planned against; rewrite MEMORY.md through
// a temp file and a rename; remove the memory file; re-run Integrity.
func Apply(root, memDir string, s Suggestion, now time.Time) (ApplyResult, error) {
	if s.ReportOnly {
		return ApplyResult{}, fmt.Errorf("%s is report-only: index the file or remove it by hand", s.ID)
	}
	if s.Action != ActionDelete && s.Action != ActionReword {
		return ApplyResult{}, fmt.Errorf("%s: %q is not an action apply carries out", s.ID, s.Action)
	}
	planned, ok := readIndex(filepath.Join(memDir, IndexName))
	if !ok {
		return ApplyResult{}, fmt.Errorf("%s is missing or not a regular file", IndexName)
	}
	return applyPlanned(root, memDir, s, planned, now)
}

// applyPlanned is Apply against index bytes read earlier — the plan. Split out
// so a test can hand it a plan the live file no longer matches, which is the
// concurrent edit the final check exists for.
func applyPlanned(root, memDir string, s Suggestion, planned []byte, now time.Time) (ApplyResult, error) {
	indexPath := filepath.Join(memDir, IndexName)
	c, err := Load(memDir)
	if err != nil {
		return ApplyResult{}, err
	}
	line, found := plannedLine(parseIndex(planned), s.IndexLine)
	if !found {
		return ApplyResult{}, fmt.Errorf("%s: its index line is no longer in %s; re-run dross memory advise", s.ID, IndexName)
	}
	// A delete of a live memory removes its file too; a dead link has none.
	var memFile string
	if s.Action == ActionDelete {
		if m := c.Resolve(line.Target); m != nil {
			memFile = m.File
		}
	}

	snap, err := Snapshot(root, memDir, now)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("snapshot failed, nothing changed: %w", err)
	}
	res := ApplyResult{Snapshot: snap}

	if memFile != "" {
		if err := checkDeletable(memDir, memFile); err != nil {
			return res, err
		}
	}

	rewritten, err := rewriteIndex(planned, line, s)
	if err != nil {
		return res, err
	}
	// The plan is only good for the index it was made against: a session that
	// saved a memory meanwhile must not lose its line to this rename.
	if live, ok := readIndex(indexPath); !ok || !bytes.Equal(live, planned) {
		return res, fmt.Errorf("%s changed while applying; nothing written — re-run dross memory advise", IndexName)
	}
	if err := replaceIndex(indexPath, rewritten); err != nil {
		return res, err
	}
	if memFile != "" {
		if err := os.Remove(filepath.Join(memDir, memFile)); err != nil {
			return res, fmt.Errorf("index updated but %s not removed: %w", memFile, err)
		}
	}

	after, err := Load(memDir)
	if err != nil {
		return res, err
	}
	integ := after.Integrity()
	res.DeadLinks, res.Orphans, res.Dangling = integ.DeadLinks, integ.Orphans, integ.Dangling
	return res, nil
}

// readIndex reads MEMORY.md when it is a regular file — never through a link.
func readIndex(path string) ([]byte, bool) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, false
	}
	return readRegular(path)
}

// plannedLine finds the index line the suggestion was made from, by its text.
func plannedLine(lines []IndexLine, raw string) (IndexLine, bool) {
	for _, l := range lines {
		if l.Raw == raw {
			return l, true
		}
	}
	return IndexLine{}, false
}

// checkDeletable refuses anything but a direct-child regular *.md that is not
// the index, and a memory that is guarded now — the file may have gained a
// guard phrase since the advice was made.
func checkDeletable(memDir, file string) error {
	if file != filepath.Base(file) || filepath.Ext(file) != ".md" || file == IndexName {
		return fmt.Errorf("refusing to delete %q: not a memory file in the memory dir", file)
	}
	path := filepath.Join(memDir, file)
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("refusing to delete %s: %w", file, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to delete %s: not a regular file", file)
	}
	data, ok := readRegular(path)
	if !ok {
		return fmt.Errorf("refusing to delete %s: unreadable", file)
	}
	if Guarded(data) {
		return fmt.Errorf("refusing to delete %s: it carries a guard phrase", file)
	}
	return nil
}

// rewriteIndex returns index with line removed (a delete) or its hook replaced
// (a reword), every other byte kept: each line keeps its own ending, so a
// mixed LF/CRLF index changes only on the target line, and a missing trailing
// newline stays missing. Lines are counted as SplitLines counts them.
func rewriteIndex(index []byte, line IndexLine, s Suggestion) ([]byte, error) {
	segs := strings.SplitAfter(string(index), "\n")
	if line.LineNo >= len(segs) {
		return nil, errors.New("index line moved since it was read")
	}
	seg := segs[line.LineNo]
	body := strings.TrimSuffix(strings.TrimSuffix(seg, "\n"), "\r")
	if body != line.Raw {
		return nil, errors.New("index line moved since it was read")
	}
	switch s.Action {
	case ActionDelete:
		segs[line.LineNo] = ""
	case ActionReword:
		if strings.TrimSpace(s.NewHook) == "" {
			return nil, fmt.Errorf("%s: refusing to reword a hook to nothing", s.ID)
		}
		i := strings.LastIndex(line.Raw, line.Hook)
		if line.Hook == "" || i < 0 {
			return nil, fmt.Errorf("%s: its index line has no hook to reword", s.ID)
		}
		segs[line.LineNo] = line.Raw[:i] + s.NewHook + line.Raw[i+len(line.Hook):] + seg[len(body):]
	}
	return []byte(strings.Join(segs, "")), nil
}

// replaceIndex writes MEMORY.md through a temp file in the memory dir and a
// rename, so a session reading the index mid-apply sees the old or the new
// one, never half of either. It is apply.go's own: the turn cache's writer
// claims only advisor state, and this is the user's memory dir.
func replaceIndex(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+IndexName+".*.tmp")
	if err != nil {
		return fmt.Errorf("write %s: %w", IndexName, err)
	}
	name := tmp.Name()
	// CreateTemp makes the file 0600; the index keeps the mode it had.
	if info, err := os.Lstat(path); err == nil {
		if err := tmp.Chmod(info.Mode().Perm()); err != nil {
			tmp.Close()
			os.Remove(name)
			return fmt.Errorf("write %s: %w", IndexName, err)
		}
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return fmt.Errorf("write %s: %w", IndexName, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return fmt.Errorf("write %s: %w", IndexName, err)
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return fmt.Errorf("replace %s: %w", IndexName, err)
	}
	return nil
}

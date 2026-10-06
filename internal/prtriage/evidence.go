package prtriage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Rivil/dross/internal/pathfence"
)

// Evidence is what one resolution rests on: a file:line locator in the repo,
// or a command and a digest of the output it produced — exactly one form. The output itself is never kept — only its sha256 and
// length — so nothing a command printed is persisted.
type Evidence struct {
	At           string `toml:"at,omitempty"`
	Cmd          string `toml:"cmd,omitempty"`
	OutputSHA256 string `toml:"output_sha256,omitempty"`
	OutputBytes  int    `toml:"output_bytes,omitempty"`
}

// ErrNoEvidence is a resolution with nothing to rest on.
var ErrNoEvidence = errors.New("evidence is required: a file:line locator (--at <path:N[-M]>) or command output (--cmd <command> --output-file <path|->)")

// SplitAt parses "path:N" or "path:N-M", splitting on the LAST colon so a
// path may hold one, and refuses an absolute or escaping path. It reads
// nothing: ParseAt is the check that the lines exist.
func SplitAt(at string) (file string, start, end int, err error) {
	path, lines, ok := cutLast(at, ":")
	if !ok || path == "" {
		return "", 0, 0, fmt.Errorf("evidence %q is not a path:line locator", at)
	}
	from, to, isRange := strings.Cut(lines, "-")
	start, err = strconv.Atoi(from)
	if err != nil || start < 1 {
		return "", 0, 0, fmt.Errorf("evidence %q: the line must be a number from 1", at)
	}
	end = start
	if isRange {
		end, err = strconv.Atoi(to)
		if err != nil || end < start {
			return "", 0, 0, fmt.Errorf("evidence %q: a range must run forward, N-M with M >= N", at)
		}
	}
	clean, inTree := pathfence.InTree(path)
	if !inTree {
		return "", 0, 0, fmt.Errorf("evidence %q: the path must stay inside the repository", at)
	}
	if clean == "" || clean == "." {
		return "", 0, 0, fmt.Errorf("evidence %q names no file", at)
	}
	return clean, start, end, nil
}

// ParseAt checks a locator against the repository at repoRoot: the path stays
// inside it, names a regular file, and the lines exist. It returns the
// evidence with the locator in its cleaned form.
func ParseAt(repoRoot, at string) (Evidence, error) {
	file, start, end, err := SplitAt(at)
	if err != nil {
		return Evidence{}, err
	}
	c, err := pathfence.Contain(repoRoot, "evidence", file)
	if err != nil {
		return Evidence{}, fmt.Errorf("evidence %q: the path must stay inside the repository", at)
	}
	info, err := pathfence.Stat(c)
	if err != nil {
		return Evidence{}, fmt.Errorf("evidence %q: %s does not exist", at, c.Rel())
	}
	if !info.Mode().IsRegular() {
		return Evidence{}, fmt.Errorf("evidence %q: %s is not a regular file", at, c.Rel())
	}
	data, err := pathfence.ReadFile(c)
	if err != nil {
		return Evidence{}, fmt.Errorf("evidence %q: %s cannot be read", at, c.Rel())
	}
	if n := lineCount(data); end > n {
		return Evidence{}, fmt.Errorf("evidence %q: %s has %d lines", at, c.Rel(), n)
	}
	loc := c.Rel() + ":" + strconv.Itoa(start)
	if end != start {
		loc += "-" + strconv.Itoa(end)
	}
	return Evidence{At: loc}, nil
}

// FromOutput records that cmd produced output: the command, the output's
// sha256 and its length — never the output. Neither may be blank.
func FromOutput(cmd, output string) (Evidence, error) {
	if strings.TrimSpace(cmd) == "" {
		return Evidence{}, errors.New("evidence: the command is blank")
	}
	if strings.TrimSpace(output) == "" {
		return Evidence{}, errors.New("evidence: the command output is blank")
	}
	sum := sha256.Sum256([]byte(output))
	return Evidence{Cmd: cmd, OutputSHA256: hex.EncodeToString(sum[:]), OutputBytes: len(output)}, nil
}

// Require refuses evidence that is not exactly one of a locator and command
// output: none at all — blank counts as none — is ErrNoEvidence, a locator
// must be a well-formed path:line (SplitAt; it reads nothing) with no digest
// beside it, and command evidence must carry its output's sha256 and length.
// It is the check a hand-edited record gets, so it trusts nothing ParseAt or
// FromOutput would have refused.
func Require(e Evidence) error {
	hasAt, hasCmd := strings.TrimSpace(e.At) != "", strings.TrimSpace(e.Cmd) != ""
	switch {
	case !hasAt && !hasCmd:
		return ErrNoEvidence
	case hasAt && hasCmd:
		return errors.New("evidence must be exactly one of a locator (at) or command output (cmd), not both")
	case hasAt && (e.OutputSHA256 != "" || e.OutputBytes != 0):
		return errors.New("locator evidence carries no output digest; that belongs to command evidence")
	case hasAt:
		if _, _, _, err := SplitAt(e.At); err != nil {
			return err
		}
	case !isSHA256Hex(e.OutputSHA256) || e.OutputBytes <= 0:
		return errors.New("command evidence must record its output's sha256 and length")
	}
	return nil
}

// isSHA256Hex is a lower-case hex sha256, as FromOutput writes it.
func isSHA256Hex(s string) bool {
	if len(s) != sha256.Size*2 || strings.ToLower(s) != s {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// cutLast is strings.Cut at the last occurrence of sep.
func cutLast(s, sep string) (before, after string, found bool) {
	i := strings.LastIndex(s, sep)
	if i < 0 {
		return s, "", false
	}
	return s[:i], s[i+len(sep):], true
}

// lineCount is the number of lines in data; a final line without a newline
// still counts.
func lineCount(data []byte) int {
	n := bytes.Count(data, []byte("\n"))
	if len(data) > 0 && data[len(data)-1] != '\n' {
		n++
	}
	return n
}

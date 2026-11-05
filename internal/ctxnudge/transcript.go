// Package ctxnudge decides when a Claude Code session has grown past the
// context threshold and renders the one-line checkpoint nudge a hook injects.
//
// Everything here runs on a hook's hot path — once per tool call — so the
// transcript is read from its tail, bounded, and every failure is silence: a
// nudge that cannot be computed is a nudge not sent, never a blocked call.
package ctxnudge

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"syscall"
)

const (
	// chunkSize is how much of the transcript one backwards read takes in.
	chunkSize = 64 << 10
	// readCap bounds the whole backwards scan. A main-agent turn further from
	// EOF than this is not found, which reads as "unknown", never as zero.
	readCap = 8 << 20
)

// LatestMainContext returns the context size of the newest main-agent turn in
// the transcript at path: input + cache-read + cache-creation tokens. ok is
// false when no such turn lies within the last readCap bytes, or when path is
// missing, unreadable or not a regular file.
//
// The open is non-blocking so a FIFO planted at the path returns at once
// instead of hanging the hook until a writer appears.
func LatestMainContext(path string) (tokens int64, ok bool) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return 0, false
	}
	return latestMain(f, info.Size())
}

// latestMain scans r backwards from size in chunkSize reads, newest line
// first, and returns the first main-agent turn it can read. A line longer than
// a chunk is kept as fragments until its start is found, so a multi-megabyte
// tool result costs one copy, not one per chunk.
func latestMain(r io.ReaderAt, size int64) (int64, bool) {
	var frags [][]byte // the pieces of the line whose start is not yet read, in file order
	pos, read := size, int64(0)
	for pos > 0 && read < readCap {
		n := min(int64(chunkSize), pos, readCap-read)
		pos -= n
		buf := make([]byte, n)
		if _, err := r.ReadAt(buf, pos); err != nil && !errors.Is(err, io.EOF) {
			return 0, false
		}
		read += n
		end := len(buf)
		for {
			i := bytes.LastIndexByte(buf[:end], '\n')
			if i < 0 {
				break
			}
			if tokens, ok := mainContext(joinLine(buf[i+1:end], frags)); ok {
				return tokens, true
			}
			frags = nil
			end = i
		}
		frags = append([][]byte{buf[:end]}, frags...)
	}
	if pos == 0 {
		return mainContext(joinLine(nil, frags))
	}
	return 0, false
}

// joinLine concatenates a line's head with the fragments read before it.
func joinLine(head []byte, frags [][]byte) []byte {
	if len(frags) == 0 {
		return head
	}
	return bytes.Join(append([][]byte{head}, frags...), nil)
}

// usageMarker is a cheap pre-filter: a line without it cannot carry usage, so
// it is skipped without a JSON decode.
var usageMarker = []byte(`"usage"`)

// entry is the narrow view of a transcript line this package reads. Content
// fields are never decoded.
type entry struct {
	Type        string `json:"type"`
	IsSidechain bool   `json:"isSidechain"`
	Message     *struct {
		Model string `json:"model"`
		Usage *struct {
			Input         int64 `json:"input_tokens"`
			CacheRead     int64 `json:"cache_read_input_tokens"`
			CacheCreation int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// mainContext reads one transcript line. It counts only an assistant turn of
// the main agent (not a subagent's sidechain, not a synthetic placeholder)
// whose three input fields sum to more than zero.
func mainContext(line []byte) (int64, bool) {
	if !bytes.Contains(line, usageMarker) {
		return 0, false
	}
	var e entry
	if json.Unmarshal(line, &e) != nil {
		return 0, false
	}
	if e.Type != "assistant" || e.IsSidechain || e.Message == nil || e.Message.Usage == nil || e.Message.Model == "<synthetic>" {
		return 0, false
	}
	u := e.Message.Usage
	sum := satAdd(satAdd(nonNeg(u.Input), nonNeg(u.CacheRead)), nonNeg(u.CacheCreation))
	return sum, sum > 0
}

func nonNeg(n int64) int64 { return max(n, 0) }

// satAdd adds two non-negative counts, pinning at MaxInt64 instead of
// wrapping to a negative that would read as "under every threshold".
func satAdd(a, b int64) int64 {
	if b > math.MaxInt64-a {
		return math.MaxInt64
	}
	return a + b
}

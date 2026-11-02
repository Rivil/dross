package memadvise

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Action is what a suggestion asks for.
type Action string

const (
	// ActionDelete removes a memory and its index line — or, for a dead index
	// link, the index line alone.
	ActionDelete Action = "delete"
	// ActionReword replaces an index hook with NewHook.
	ActionReword Action = "reword hook"
	// ActionIndex asks for an orphan to be indexed by hand. It is the orphan
	// advice for a guarded memory, which is never suggested for deletion.
	ActionIndex Action = "index"
)

// idPrefix is the action's word in a suggestion ID.
var idPrefix = map[Action]string{ActionDelete: "delete", ActionReword: "reword", ActionIndex: "index"}

// Suggestion is one prune the advisor proposes.
type Suggestion struct {
	ID     string // action + file stem: stable across runs and unrelated edits
	File   string // the memory file, or a dead link's target as written
	Reason string
	Action Action
	// ReportOnly marks advice Apply refuses: an orphan has no index line to
	// remove, and deleting an unindexed file is a call for a human.
	ReportOnly bool
	NewHook    string   // the reworded hook, for ActionReword
	Doc        string   // the tracked doc a duplicate restates
	Inbound    []string // memories that [[link]] this one
	IndexLine  string   // the MEMORY.md line the suggestion edits or removes
	BytesSaved int      // index bytes the change removes from every turn
	MonthlyUSD float64
}

// Inputs is everything Advise reads besides the memory dir itself.
type Inputs struct {
	Dir   string // the memory dir
	Docs  *Docs  // tracked docs a memory may duplicate; nil for none
	Today time.Time
	// Known holds the phase slugs that are scaffolded or shipped, for the
	// "next <phase>" clauses (see NextSlugs).
	Known map[string]bool
	Turns TurnCounts
}

// Advise returns at most one suggestion per memory, plus one per dead index
// link, most valuable first. A missing dir, a missing MEMORY.md or an empty
// dir is nothing to advise on: an empty slice and a nil error.
//
// Per memory, an expired hook clause wins: the fix is a reword even when the
// memory also duplicates a doc, because the clause is what is certainly wrong
// and a delete is the move the reword exists to avoid. A guarded memory is
// never offered a delete from any source.
func Advise(in Inputs) ([]Suggestion, error) {
	c, err := Load(in.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return []Suggestion{}, nil
	}
	if err != nil {
		return nil, err
	}
	if !c.HasIndex {
		return []Suggestion{}, nil
	}
	integ := c.Integrity()
	turns := in.Turns.Repo

	out := []Suggestion{}
	var dead []IndexLine
	var order []*Memory
	linesOf := map[string][]IndexLine{}
	for _, l := range c.Lines {
		m := c.Resolve(l.Target)
		if m == nil {
			dead = append(dead, l)
			continue
		}
		if linesOf[m.File] == nil {
			order = append(order, m)
		}
		linesOf[m.File] = append(linesOf[m.File], l)
	}
	for _, m := range order {
		if s, ok := adviseIndexed(m, linesOf[m.File], in, integ, turns); ok {
			out = append(out, s)
		}
	}

	for _, f := range integ.Orphans {
		m := c.Memories[f]
		inbound := inboundOf(m, integ)
		action, reason := ActionDelete, "orphan: not in MEMORY.md — index it or remove it by hand"
		if Guarded(m.Raw) {
			action, reason = ActionIndex, "orphan: not in MEMORY.md, and guarded — index it by hand"
		}
		out = append(out, Suggestion{
			ID:         id(action, f),
			File:       f,
			Reason:     withInbound(reason, inbound),
			Action:     action,
			ReportOnly: true,
			Inbound:    inbound,
		})
	}

	// A dead link's id carries its own line's content address on top of the
	// stem, so it never names a memory: a live x.md that later turns up
	// takes delete-x without inheriting a stale dead-link id, and reordering
	// the index never moves an id to another line.
	taken := map[string]bool{}
	for _, s := range out {
		taken[s.ID] = true
	}
	// Content order: the -N fallback below (byte-identical lines, whose
	// deletes are indistinguishable, or a hash that collides) is then
	// independent of where the lines sit.
	sort.SliceStable(dead, func(i, j int) bool { return dead[i].Raw < dead[j].Raw })
	for _, l := range dead {
		sid := DeadLinkID(l.Target, l.Raw)
		for n, first := 2, sid; taken[sid]; n++ {
			sid = fmt.Sprintf("%s-%d", first, n)
		}
		taken[sid] = true
		lineBytes := len(l.Raw) + 1 // the line and its newline go together
		out = append(out, Suggestion{
			ID:         sid,
			File:       l.Target,
			Reason:     "dead index link: " + l.Target + " is not a memory in this dir",
			Action:     ActionDelete,
			IndexLine:  l.Raw,
			BytesSaved: lineBytes,
			MonthlyUSD: MonthlyUSD(lineBytes, turns),
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].MonthlyUSD != out[j].MonthlyUSD {
			return out[i].MonthlyUSD > out[j].MonthlyUSD
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// adviseIndexed is the one suggestion for an indexed memory, if any. Every
// index line of it is checked for an expired clause before a delete is
// considered, so a reword on any of its lines wins.
func adviseIndexed(m *Memory, lines []IndexLine, in Inputs, integ Integrity, turns int) (Suggestion, bool) {
	inbound := inboundOf(m, integ)
	for _, l := range lines {
		if reworded, ok := Expired(l.Hook, in.Today, in.Known); ok {
			saved := len(l.Hook) - len(reworded)
			return Suggestion{
				ID:         id(ActionReword, m.File),
				File:       m.File,
				Reason:     withInbound("a time-bound clause in its hook has expired", inbound),
				Action:     ActionReword,
				NewHook:    reworded,
				Inbound:    inbound,
				IndexLine:  l.Raw,
				BytesSaved: saved,
				MonthlyUSD: MonthlyUSD(saved, turns),
			}, true
		}
	}
	if Guarded(m.Raw) {
		return Suggestion{}, false
	}
	doc, ok := in.Docs.Duplicates(m.Body)
	if !ok {
		return Suggestion{}, false
	}
	lineBytes := len(lines[0].Raw) + 1
	return Suggestion{
		ID:         id(ActionDelete, m.File),
		File:       m.File,
		Reason:     withInbound("duplicates "+doc, inbound),
		Action:     ActionDelete,
		Doc:        doc,
		Inbound:    inbound,
		IndexLine:  lines[0].Raw,
		BytesSaved: lineBytes,
		MonthlyUSD: MonthlyUSD(lineBytes, turns),
	}, true
}

// id is the action word and the file's stem: it names the memory, not its
// position, so adding or reordering index lines never renames a memory's
// suggestion. Dead links take DeadLinkID instead.
func id(a Action, file string) string {
	return idPrefix[a] + "-" + strings.TrimSuffix(filepath.Base(file), ".md")
}

// DeadLinkID is the suggestion id for deleting the dead index line raw that
// links target: delete-<stem>-<lineHash(raw)>. It names the line by content,
// never by position, and can match a memory's id only if that memory's stem
// itself ends in the same six hex digits (Advise's -N fallback still keeps
// the two apart).
func DeadLinkID(target, raw string) string {
	return id(ActionDelete, target) + "-" + lineHash(raw)
}

// lineHash is a short content address for an index line: the first six hex
// digits of its SHA-256.
func lineHash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:3])
}

func inboundOf(m *Memory, integ Integrity) []string {
	if m == nil || m.Name == "" {
		return nil
	}
	return integ.Inbound[m.Name]
}

// withInbound says who links here: removing a linked memory leaves their
// [[links]] dangling, which the reader should weigh before applying.
func withInbound(reason string, inbound []string) string {
	if len(inbound) == 0 {
		return reason
	}
	return reason + "; linked from " + strings.Join(inbound, ", ")
}

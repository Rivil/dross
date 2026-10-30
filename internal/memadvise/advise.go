package memadvise

import (
	"errors"
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
	seen := map[string]bool{}
	for _, l := range c.Lines {
		m := c.Resolve(l.Target)
		if m == nil {
			lineBytes := len(l.Raw) + 1 // the line and its newline go together
			out = append(out, Suggestion{
				ID:         id(ActionDelete, l.Target),
				File:       l.Target,
				Reason:     "dead index link: " + l.Target + " is not a memory in this dir",
				Action:     ActionDelete,
				IndexLine:  l.Raw,
				BytesSaved: lineBytes,
				MonthlyUSD: MonthlyUSD(lineBytes, turns),
			})
			continue
		}
		if seen[m.File] {
			continue
		}
		inbound := inboundOf(m, integ)
		if reworded, ok := Expired(l.Hook, in.Today, in.Known); ok {
			seen[m.File] = true
			saved := len(l.Hook) - len(reworded)
			out = append(out, Suggestion{
				ID:         id(ActionReword, m.File),
				File:       m.File,
				Reason:     withInbound("a time-bound clause in its hook has expired", inbound),
				Action:     ActionReword,
				NewHook:    reworded,
				Inbound:    inbound,
				IndexLine:  l.Raw,
				BytesSaved: saved,
				MonthlyUSD: MonthlyUSD(saved, turns),
			})
			continue
		}
		if Guarded(m.Raw) {
			continue
		}
		if doc, ok := in.Docs.Duplicates(m.Body); ok {
			seen[m.File] = true
			lineBytes := len(l.Raw) + 1
			out = append(out, Suggestion{
				ID:         id(ActionDelete, m.File),
				File:       m.File,
				Reason:     withInbound("duplicates "+doc, inbound),
				Action:     ActionDelete,
				Doc:        doc,
				Inbound:    inbound,
				IndexLine:  l.Raw,
				BytesSaved: lineBytes,
				MonthlyUSD: MonthlyUSD(lineBytes, turns),
			})
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

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].MonthlyUSD != out[j].MonthlyUSD {
			return out[i].MonthlyUSD > out[j].MonthlyUSD
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// id is the action word and the file's stem: it names the memory, not its
// position, so adding or reordering index lines never renames a suggestion.
func id(a Action, file string) string {
	return idPrefix[a] + "-" + strings.TrimSuffix(filepath.Base(file), ".md")
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

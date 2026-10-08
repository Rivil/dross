package verify

import (
	"sort"

	"github.com/Rivil/dross/internal/mutation"
)

// ConstructHunk is the construct label of a range that stayed the raw hunk:
// the changed lines fell under no top-level construct (a comment between
// declarations, a blank line, a change past the last node). It is a label,
// not an absence — every persisted range names what it was widened to, and
// "nothing" is an answer the record must be able to give.
const ConstructHunk = "hunk"

// expandToConstructs widens raw hunks to the full span of every top-level
// construct they touch. Pure: the constructs come from a resolver, the hunks
// from the scope, and the output is what the tool is told.
//
// Every hunk line that lies inside a construct maps to that construct's FULL
// span, labelled by Label(); two hunks in one construct yield one range. Every
// maximal run of hunk lines that no construct covers becomes one bare range
// labelled ConstructHunk — a hunk that straddles a construct's edge splits
// into the construct span plus the uncovered remainder. The result is sorted
// by Start and pairwise non-overlapping, which is what makes a --mutate list
// honest: two overlapping specs claim a scope the argv does not have.
//
// Constructs are taken as sorted by Start and disjoint (the resolver's
// contract); hunks may arrive in any order and may overlap each other.
func expandToConstructs(hunks []Range, cs []mutation.Construct) []EffectiveRange {
	if len(hunks) == 0 {
		return nil
	}
	var out []EffectiveRange
	seen := make(map[[2]int]bool)
	// bare collects uncovered lines; they are coalesced into runs below.
	var bare []int
	for _, h := range hunks {
		for line := h.Start; line <= h.End; line++ {
			c, ok := enclosing(cs, line)
			if !ok {
				bare = append(bare, line)
				continue
			}
			key := [2]int{c.Start, c.End}
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, EffectiveRange{Start: c.Start, End: c.End, Construct: c.Label()})
		}
	}
	out = append(out, runs(bare)...)
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}

// enclosing finds the construct whose span contains line. Constructs are
// sorted and disjoint, so a binary search on Start is enough.
func enclosing(cs []mutation.Construct, line int) (mutation.Construct, bool) {
	i := sort.Search(len(cs), func(i int) bool { return cs[i].Start > line })
	if i == 0 {
		return mutation.Construct{}, false
	}
	c := cs[i-1]
	if line >= c.Start && line <= c.End {
		return c, true
	}
	return mutation.Construct{}, false
}

// runs coalesces bare line numbers into maximal contiguous ranges. Lines may
// repeat (overlapping hunks) and arrive unsorted.
func runs(lines []int) []EffectiveRange {
	if len(lines) == 0 {
		return nil
	}
	sort.Ints(lines)
	var out []EffectiveRange
	cur := EffectiveRange{Start: lines[0], End: lines[0], Construct: ConstructHunk}
	for _, l := range lines[1:] {
		switch {
		case l == cur.End || l == cur.End+1:
			cur.End = l
		default:
			out = append(out, cur)
			cur = EffectiveRange{Start: l, End: l, Construct: ConstructHunk}
		}
	}
	return append(out, cur)
}

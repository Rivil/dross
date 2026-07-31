package cmd

import (
	"encoding/json"
	"strings"
)

// dotLookup resolves one dotted path to its value. A scalar comes back as a
// string, a list as a []string; an unknown path returns the caller's own
// unknown-field error, so each config surface keeps the wording it already had.
type dotLookup func(path string) (any, error)

// renderMultiGet is the shared shape of `project|milestone|state get`
// (locked multi_get_shape).
//
// One path prints its bare value exactly as it always has — a scalar on its
// own line, a list one entry per line — because every existing caller and
// prompt passes one path and must keep working byte-identically. Two or more
// paths emit a single keyed JSON object instead, so a script can read the
// whole set in one call.
//
// Every path is resolved before anything is written, so an unknown path in any
// position errors with nothing on stdout rather than a partial object.
func renderMultiGet(paths []string, lookup dotLookup) error {
	// Dedup preserving argument order: a path passed twice is one key, not a
	// duplicate JSON key.
	ordered := make([]string, 0, len(paths))
	seen := map[string]bool{}
	for _, p := range paths {
		if !seen[p] {
			seen[p] = true
			ordered = append(ordered, p)
		}
	}

	values := make([]any, len(ordered))
	for i, p := range ordered {
		v, err := lookup(p)
		if err != nil {
			return err
		}
		// A nil list is still a list — it must serialise as [] below, never
		// as null.
		if list, ok := v.([]string); ok && list == nil {
			v = []string{}
		}
		values[i] = v
	}

	if len(ordered) == 1 {
		switch v := values[0].(type) {
		case []string:
			for _, entry := range v {
				Print(entry)
			}
		case string:
			Print(v)
		default:
			Printf("%v\n", v)
		}
		return nil
	}

	// Assembled by hand rather than through a map, because a map marshals in
	// sorted key order and the contract is argument order.
	var b strings.Builder
	b.WriteByte('{')
	for i, p := range ordered {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := json.Marshal(p)
		if err != nil {
			return err
		}
		v, err := json.Marshal(values[i])
		if err != nil {
			return err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	Print(b.String())
	return nil
}

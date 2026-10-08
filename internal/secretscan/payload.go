package secretscan

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ScanPayload screens a request body before it is encoded and sent.
//
// body is marshalled exactly as the transport would marshal it and walked as
// generic JSON, so every string leaf is scanned regardless of the Go type
// that produced it — a struct with json tags, a nested map, a slice, or a
// bare string. surface names the call (`POST /issue`); a hit's Location is
// that surface plus the JSON path to the leaf (`POST /issue:fields.description`),
// so the user learns which field carried the value without the value.
//
// A nil body is nothing to scan and returns nil.
func ScanPayload(surface string, body any) error {
	if body == nil {
		return nil
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("secret scan %s: %w", surface, err)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return fmt.Errorf("secret scan %s: %w", surface, err)
	}
	var hits []Hit
	walkLeaves(v, "", func(path, leaf string) {
		loc := surface
		if path != "" {
			loc += ":" + path
		}
		hits = append(hits, ScanString(loc, leaf)...)
	})
	if len(hits) > 0 {
		return &ErrHit{Hits: hits}
	}
	return nil
}

// walkLeaves visits every string leaf of a decoded JSON value with its path
// in `a.b[0].c` form; the root has the empty path.
func walkLeaves(v any, path string, visit func(path, leaf string)) {
	switch x := v.(type) {
	case string:
		visit(path, x)
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys) // stable report order across runs
		for _, k := range keys {
			p := k
			if path != "" {
				p = path + "." + k
			}
			walkLeaves(x[k], p, visit)
		}
	case []any:
		for i, child := range x {
			walkLeaves(child, path+"["+strconv.Itoa(i)+"]", visit)
		}
	}
}

// ScanArgv screens a command line before it is executed. Each element is
// scanned on its own; a hit's Location is `<tool> <flag>` when the element is
// the value of the immediately preceding `--flag` (or the value half of a
// `--flag=value` element), and `<tool> argv[i]` otherwise. A bare `--` is a
// separator, not a flag, so a positional after it reports by index.
func ScanArgv(tool string, args []string) error {
	var hits []Hit
	for i, a := range args {
		loc := tool + " argv[" + strconv.Itoa(i) + "]"
		switch {
		case isFlag(a) && strings.Contains(a, "="):
			loc = tool + " " + a[:strings.Index(a, "=")]
		case i > 0 && isFlag(args[i-1]) && !strings.Contains(args[i-1], "="):
			loc = tool + " " + args[i-1]
		}
		hits = append(hits, ScanString(loc, a)...)
	}
	if len(hits) > 0 {
		return &ErrHit{Hits: hits}
	}
	return nil
}

// isFlag reports whether a is a named long flag — `--name`, never the bare
// `--` separator.
func isFlag(a string) bool {
	return len(a) > 2 && strings.HasPrefix(a, "--")
}

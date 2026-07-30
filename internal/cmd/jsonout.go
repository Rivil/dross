package cmd

import (
	"encoding/json"
	"os"
)

// emitJSON writes v as the bare JSON document — no envelope, no wrapper key,
// and none of the `# <path>` header line the toml rendering prints (locked
// json_shape).
//
// Bare means there is no second schema to keep in sync with the structs: the
// payload is exactly what the toml decoder filled, so `show --json` and the
// document on disk cannot describe different shapes. Prompt consumers read the
// payload directly instead of unwrapping an envelope.
//
// The header line is dropped rather than commented out or moved into a field
// because a `#` line is not JSON — anything downstream would have to strip it
// before parsing, which is the one thing --json exists to avoid.
func emitJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// jsonFlagUsage is the one usage string every `show --json` registers, so the
// tree-walk gate in json_show_test.go sees a uniform flag rather than nine
// hand-written variants.
const jsonFlagUsage = "emit the bare JSON document instead of toml (no `# <path>` header)"

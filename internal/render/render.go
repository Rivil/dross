// Package render is the one place CLI output crosses a codec: every `--json`
// document and every TOML `show` rendering internal/cmd prints goes through it,
// so the command tree holds no encoding/json or BurntSushi/toml import of its
// own.
//
// Each function reproduces exactly one shape the commands already emitted —
// byte for byte, pinned by render_test's differentials and cmd's output
// goldens — rather than normalising them into one. A caller choosing between
// JSON and MarshalJSON is choosing between two documents a consumer already
// parses.
package render

import (
	"encoding/json"
	"io"

	"github.com/BurntSushi/toml"
)

// JSON writes v to w as a two-space-indented document followed by a newline —
// json.Encoder with SetIndent("", "  "). HTML-significant characters are
// escaped, as the encoder does by default.
func JSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// MarshalJSON is json.Marshal: compact, HTML-escaped, no trailing newline.
func MarshalJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}

// MarshalJSONIndent is json.MarshalIndent(v, "", "  "): two-space indent, no
// trailing newline.
func MarshalJSONIndent(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

// TOML writes v to w through toml.NewEncoder, whose default indent is two
// spaces.
func TOML(w io.Writer, v any) error {
	return toml.NewEncoder(w).Encode(v)
}

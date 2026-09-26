// Package hooks owns dross's edits to the user-level Claude Code settings.json.
//
// MergeHook merges dross-managed hook entries (PreCompact, SessionStart)
// without disturbing foreign entries; it is bytes in, bytes out, and its
// caller owns the file I/O. ReadSettings and MutateSettings are the env
// block's read and read-modify-write: the whole document parses before
// anything is written, and the write is a temp file renamed into place, so a
// parse failure can never half-write a user's config either way.
package hooks

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// ReadSettings decodes the settings.json at path. A missing or empty file is
// an empty document, not an error — a fresh machine has none.
func ReadSettings(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		return map[string]any{}, nil
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return doc, nil
}

// MutateSettings reads, applies fn, writes back atomically with mode
// 0o600 since the file holds tokens. JSON map order is alphabetical
// after marshal — acceptable since JSON has no ordering semantics.
func MutateSettings(path string, fn func(map[string]any)) error {
	doc, err := ReadSettings(path)
	if err != nil {
		return err
	}
	fn(doc)
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Hook events dross wires. Values are Claude Code settings.json hook keys.
const (
	EventPreCompact   = "PreCompact"
	EventSessionStart = "SessionStart"
)

// hookCommand is one command entry inside a hook group.
type hookCommand struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// hookGroup is one matcher group under hooks.<event>. dross entries omit the
// matcher so they fire on every trigger of the event.
type hookGroup struct {
	Hooks []hookCommand `json:"hooks"`
}

// MergeHook returns settings.json with a dross hook ensured under
// hooks.<event>: a matcher-less group whose single command is command.
// Idempotent — if any existing group under the event already runs command, the
// input bytes are returned unchanged, so a second install is byte-identical.
// Foreign groups and every other key survive verbatim and unreordered.
// Malformed JSON (root, hooks, or the event array) returns an error and no
// output — never a partial rewrite. Empty/whitespace input is treated as an
// empty object so a missing settings.json is created.
func MergeHook(settings []byte, event, command string) ([]byte, error) {
	root, err := parseOrdered(settings)
	if err != nil {
		return nil, fmt.Errorf("parse settings.json: %w", err)
	}

	hooksObj, err := parseOrdered(rawValue(root, "hooks"))
	if err != nil {
		// hooks exists but isn't an object — refuse rather than clobber.
		return nil, fmt.Errorf("parse settings.json hooks: %w", err)
	}

	var groups []json.RawMessage
	if raw, ok := hooksObj.get(event); ok {
		if err := json.Unmarshal(raw, &groups); err != nil {
			return nil, fmt.Errorf("parse settings.json hooks.%s: %w", event, err)
		}
	}

	for _, g := range groups {
		if groupRunsCommand(g, command) {
			return settings, nil // already ensured — byte-stable no-op
		}
	}

	entry, err := json.Marshal(hookGroup{Hooks: []hookCommand{{Type: "command", Command: command}}})
	if err != nil {
		return nil, fmt.Errorf("encode hook entry: %w", err)
	}
	groups = append(groups, entry)

	hooksObj.set(event, compactArray(groups))
	root.set("hooks", hooksObj.compact())
	return root.marshalIndented()
}

// groupRunsCommand reports whether a matcher group contains a command entry
// running command. Unparseable groups are foreign config — never a match.
func groupRunsCommand(group json.RawMessage, command string) bool {
	var g hookGroup
	if err := json.Unmarshal(group, &g); err != nil {
		return false
	}
	for _, h := range g.Hooks {
		if h.Command == command {
			return true
		}
	}
	return false
}

// compactArray renders elements verbatim as a single-line JSON array.
func compactArray(elems []json.RawMessage) []byte {
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i, e := range elems {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(e)
	}
	buf.WriteByte(']')
	return buf.Bytes()
}

// rawValue returns the raw bytes for key, or nil if absent.
func rawValue(o *orderedObject, key string) []byte {
	if raw, ok := o.get(key); ok {
		return raw
	}
	return nil
}

// orderedObject is a JSON object that preserves top-level key insertion order
// and keeps each value's bytes verbatim, so a merge touches only the keys it
// means to and never reorders or rewrites unrelated config. Final formatting is
// normalized to 2-space indentation by json.Indent at emit time. (Local copy of
// internal/statusline's unexported core — worth a shared package if a third
// settings.json writer appears.)
type orderedObject struct {
	keys []string
	m    map[string]json.RawMessage
}

func newOrdered() *orderedObject {
	return &orderedObject{m: map[string]json.RawMessage{}}
}

// parseOrdered decodes a JSON object preserving key order. Empty/whitespace
// input yields an empty object (a missing settings.json). Non-object input is
// an error.
func parseOrdered(data []byte) (*orderedObject, error) {
	o := newOrdered()
	if len(bytes.TrimSpace(data)) == 0 {
		return o, nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, fmt.Errorf("expected JSON object, got %v", tok)
	}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, fmt.Errorf("expected string key, got %v", keyTok)
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		o.set(key, raw)
	}
	return o, nil
}

func (o *orderedObject) get(key string) (json.RawMessage, bool) {
	v, ok := o.m[key]
	return v, ok
}

func (o *orderedObject) set(key string, raw json.RawMessage) {
	if _, ok := o.m[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.m[key] = raw
}

// compact renders the object as a single-line `{"k":v,...}` with values verbatim.
func (o *orderedObject) compact() []byte {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		buf.Write(kb)
		buf.WriteByte(':')
		buf.Write(o.m[k])
	}
	buf.WriteByte('}')
	return buf.Bytes()
}

// marshalIndented renders the object 2-space indented with a trailing newline,
// normalizing all whitespace deterministically (so merges are idempotent).
func (o *orderedObject) marshalIndented() ([]byte, error) {
	var out bytes.Buffer
	if err := json.Indent(&out, o.compact(), "", "  "); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

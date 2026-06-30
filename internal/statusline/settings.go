package statusline

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// ErrStatusLineClobber is returned by MergeStatusline when settings.json already
// has a statusLine.command that is NOT dross's, and force is false. Wiring the
// status line must never silently overwrite a status line the user configured
// themselves — the caller turns this into an explicit consent prompt.
var ErrStatusLineClobber = errors.New("settings.json already has a different statusLine.command")

const statusLineKey = "statusLine"

// MergeStatusline returns settings.json with statusLine wired to invoke command
// (the absolute installed-binary status-line command, e.g. `/abs/dross statusline`).
// It preserves every other top-level key and order, sets statusLine.type="command"
// and statusLine.command=command (keeping any other statusLine sub-keys such as
// refreshInterval), and is idempotent. If a DIFFERENT statusLine.command is already
// present it refuses with ErrStatusLineClobber unless force is true, returning the
// input bytes unchanged. Empty/whitespace input is treated as an empty object so a
// missing settings.json is created. Output is 2-space indented with a trailing newline.
func MergeStatusline(settings []byte, command string, force bool) ([]byte, error) {
	root, err := parseOrdered(settings)
	if err != nil {
		return nil, fmt.Errorf("parse settings.json: %w", err)
	}

	if raw, ok := root.get(statusLineKey); ok {
		switch existing := commandOf(raw); {
		case existing == command:
			return settings, nil // already wired to us — byte-stable no-op
		case existing != "" && !force:
			return settings, ErrStatusLineClobber
		}
	}

	// Start from the existing statusLine object (preserving its other sub-keys) or
	// a fresh one, then set type + command.
	sl, err := parseOrdered(rawValue(root, statusLineKey))
	if err != nil {
		sl = newOrdered() // existing statusLine wasn't an object — replace wholesale
	}
	sl.set("type", json.RawMessage(`"command"`))
	cmdJSON, err := json.Marshal(command)
	if err != nil {
		return nil, fmt.Errorf("encode command: %w", err)
	}
	sl.set("command", cmdJSON)
	root.set(statusLineKey, sl.compact())

	return root.marshalIndented()
}

// RemoveStatusline returns settings.json with dross's statusLine entry removed,
// preserving every other key. It is a no-op (input returned unchanged) when there is
// no statusLine or when the existing statusLine.command is NOT command — it never
// removes a status line the user configured themselves.
func RemoveStatusline(settings []byte, command string) ([]byte, error) {
	root, err := parseOrdered(settings)
	if err != nil {
		return nil, fmt.Errorf("parse settings.json: %w", err)
	}
	raw, ok := root.get(statusLineKey)
	if !ok || commandOf(raw) != command {
		return settings, nil // nothing of ours to remove
	}
	root.delete(statusLineKey)
	return root.marshalIndented()
}

// commandOf extracts the .command string from a statusLine value, or "" if the
// value is not an object or has no command.
func commandOf(raw json.RawMessage) string {
	var v struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return ""
	}
	return v.Command
}

// rawValue returns the raw bytes for key, or nil if absent.
func rawValue(o *orderedObject, key string) []byte {
	if raw, ok := o.get(key); ok {
		return raw
	}
	return nil
}

// orderedObject is a JSON object that preserves top-level key insertion order and
// keeps each value's bytes verbatim, so a merge touches only the keys it means to
// and never reorders or rewrites unrelated config. Final formatting is normalized
// to 2-space indentation by json.Indent at emit time.
type orderedObject struct {
	keys []string
	m    map[string]json.RawMessage
}

func newOrdered() *orderedObject {
	return &orderedObject{m: map[string]json.RawMessage{}}
}

// parseOrdered decodes a JSON object preserving key order. Empty/whitespace input
// yields an empty object (a missing settings.json). Non-object input is an error.
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

func (o *orderedObject) delete(key string) {
	if _, ok := o.m[key]; !ok {
		return
	}
	delete(o.m, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			break
		}
	}
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

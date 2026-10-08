package project

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"
)

// encodeFresh is the package's one BurntSushi encoder call site. Everything
// that needs TOML text — a whole project.toml for a path that does not exist
// yet, the canonical form a patched document is verified against, a single
// value the patcher splices in — asks here, so the escaping and layout are
// one thing and a second encoder configuration can never drift from it.
func encodeFresh(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	enc.Indent = "  "
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// renderValue renders one TOML value exactly as the encoder would write it
// after `key = `: it encodes a one-key document and strips the key, so
// quoting, escaping and array spacing are byte-for-byte the encoder's own.
// Tables (maps, slices of maps) are refused — they are written as headers,
// one key per op, never as a single spliced value.
func renderValue(v any) (string, error) {
	out, err := encodeFresh(map[string]any{"k": v})
	if err != nil {
		return "", fmt.Errorf("render value: %w", err)
	}
	s := string(out)
	const prefix = "k = "
	if !strings.HasPrefix(s, prefix) {
		return "", fmt.Errorf("a %T does not render as a single value; write it as a table, one key per op", v)
	}
	s = strings.TrimSuffix(strings.TrimPrefix(s, prefix), "\n")
	if strings.Contains(s, "\n") {
		return "", fmt.Errorf("a %T renders across lines; not a single value", v)
	}
	return s, nil
}

// renderKey spells a key the way the encoder does: bare when every byte is
// bare-key legal, otherwise basic-quoted with the encoder's own escaping.
func renderKey(key string) (string, error) {
	if key == "" {
		return "", fmt.Errorf("empty key")
	}
	bare := true
	for i := 0; i < len(key); i++ {
		if !isBareKeyByte(key[i]) {
			bare = false
			break
		}
	}
	if bare {
		return key, nil
	}
	return renderValue(key)
}

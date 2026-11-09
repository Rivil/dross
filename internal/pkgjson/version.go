package pkgjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// SyncVersion sets the top-level "version" of the package.json at path
// to want, rewriting only the bytes of that string so the file's formatting
// survives. A file with no top-level string version is left alone — the sync
// keeps a number in step, it never adds one — as is a file already at want,
// and a path missing from the work tree. A version written with JSON escapes
// is refused rather than rewritten, since its raw bytes are not its value.
func SyncVersion(path, want string) error {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	start, end, ok, err := topLevelVersionSpan(b)
	if err != nil || !ok || string(b[start:end]) == want {
		return err
	}
	out := append(append(append([]byte{}, b[:start]...), want...), b[end:]...)
	return os.WriteFile(path, out, info.Mode().Perm())
}

// topLevelVersionSpan returns the byte span of the top-level "version" string
// value, quotes excluded. ok is false when the document has no top-level
// string version.
func topLevelVersionSpan(b []byte) (start, end int, ok bool, err error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	tok, err := dec.Token()
	if err != nil {
		return 0, 0, false, err
	}
	if d, isDelim := tok.(json.Delim); !isDelim || d != '{' {
		return 0, 0, false, nil
	}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return 0, 0, false, err
		}
		if key, _ := keyTok.(string); key != "version" {
			if err := skipJSONValue(dec); err != nil {
				return 0, 0, false, err
			}
			continue
		}
		from := dec.InputOffset()
		val, err := dec.Token()
		if err != nil {
			return 0, 0, false, err
		}
		s, isString := val.(string)
		if !isString {
			return 0, 0, false, nil
		}
		to := int(dec.InputOffset())
		q := bytes.IndexByte(b[from:to], '"')
		if q < 0 {
			return 0, 0, false, fmt.Errorf("version value at offset %d is not a quoted string", from)
		}
		start, end = int(from)+q+1, to-1
		if string(b[start:end]) != s {
			return 0, 0, false, errors.New("top-level version uses JSON escapes; fix it by hand")
		}
		return start, end, true, nil
	}
	return 0, 0, false, nil
}

// skipJSONValue consumes one value — a scalar, or an object/array with
// everything inside it.
func skipJSONValue(dec *json.Decoder) error {
	depth := 0
	for {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		if d, isDelim := tok.(json.Delim); isDelim {
			if d == '{' || d == '[' {
				depth++
			} else {
				depth--
			}
		}
		if depth == 0 {
			return nil
		}
	}
}

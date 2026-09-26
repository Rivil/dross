package render

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/BurntSushi/toml"
)

// The differentials: each render function against the exact call it replaced,
// over one value carrying nested tables, an omitempty field on both sides of
// empty, a slice of tables, and "<&>" — the characters json escapes by default
// and toml does not. A render function that drifted from its call in any of
// those dimensions shows here as unequal bytes.

type inner struct {
	Name  string `json:"name" toml:"name"`
	Empty string `json:"empty,omitempty" toml:"empty,omitempty"`
}

type sample struct {
	Title  string            `json:"title" toml:"title"`
	Count  int               `json:"count" toml:"count"`
	Note   string            `json:"note,omitempty" toml:"note,omitempty"`
	Skip   string            `json:"skip,omitempty" toml:"skip,omitempty"`
	Tags   []string          `json:"tags" toml:"tags"`
	Inner  inner             `json:"inner" toml:"inner"`
	Labels map[string]string `json:"labels" toml:"labels"`
	Rows   []inner           `json:"rows" toml:"rows"`
}

func sampleValue() sample {
	return sample{
		Title:  "a <b> & c",
		Count:  3,
		Note:   "kept",
		Tags:   []string{"x", "<y>"},
		Inner:  inner{Name: "n & m"},
		Labels: map[string]string{"z": "last", "a": "first"},
		Rows:   []inner{{Name: "r1", Empty: "set"}, {Name: "r2"}},
	}
}

// escapedLT and escapedGT are json's HTML escapes for < and >, spelled out as
// the six bytes they are on the wire.
const (
	escapedLT = "\\u003c"
	escapedGT = "\\u003e"
)

func TestJSONMatchesIndentedEncoder(t *testing.T) {
	v := sampleValue()
	var want bytes.Buffer
	enc := json.NewEncoder(&want)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	var got bytes.Buffer
	if err := JSON(&got, v); err != nil {
		t.Fatal(err)
	}
	if got.String() != want.String() {
		t.Errorf("JSON drifted from the indented encoder:\n--- got\n%s--- want\n%s", got.String(), want.String())
	}
	if !bytes.Contains(got.Bytes(), []byte(escapedLT+"b"+escapedGT)) || !bytes.HasSuffix(got.Bytes(), []byte("}\n")) {
		t.Errorf("JSON lost HTML escaping or its trailing newline:\n%s", got.String())
	}
}

func TestMarshalJSONMatchesMarshal(t *testing.T) {
	v := sampleValue()
	want, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	got, err := MarshalJSON(v)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("MarshalJSON drifted from json.Marshal:\n got  %s\n want %s", got, want)
	}
	if !bytes.Contains(got, []byte(escapedLT)) || bytes.Contains(got, []byte("<")) {
		t.Errorf("MarshalJSON stopped escaping HTML: %s", got)
	}
}

func TestMarshalJSONIndentMatchesMarshalIndent(t *testing.T) {
	v := sampleValue()
	want, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got, err := MarshalJSONIndent(v)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("MarshalJSONIndent drifted from json.MarshalIndent:\n--- got\n%s\n--- want\n%s", got, want)
	}
}

// TestTOMLMatchesTheEncoderEitherWay: the default encoder and one with an
// explicit two-space Indent produce the same bytes, so collapsing stack show's
// explicit Indent onto TOML changes nothing.
func TestTOMLMatchesTheEncoderEitherWay(t *testing.T) {
	v := sampleValue()
	var byDefault, explicit, got bytes.Buffer
	if err := toml.NewEncoder(&byDefault).Encode(v); err != nil {
		t.Fatal(err)
	}
	enc := toml.NewEncoder(&explicit)
	enc.Indent = "  "
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	if err := TOML(&got, v); err != nil {
		t.Fatal(err)
	}
	if got.String() != byDefault.String() {
		t.Errorf("TOML drifted from the default encoder:\n--- got\n%s--- want\n%s", got.String(), byDefault.String())
	}
	if got.String() != explicit.String() {
		t.Errorf("TOML differs from an explicit two-space Indent:\n--- got\n%s--- want\n%s", got.String(), explicit.String())
	}
	if !bytes.Contains(got.Bytes(), []byte("[inner]\n  name = ")) {
		t.Errorf("the nested table is not indented two spaces:\n%s", got.String())
	}
}

package prtriage_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/Rivil/dross/internal/prtriage"
)

// evidenceRepo is a repository root holding a 4-line x.go, a file whose name
// holds a colon, and a directory.
func evidenceRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range map[string]string{
		"x.go":            "package x\n\nfunc F() {}\n// end\n",
		"a:b.go":          "one\ntwo\nthree\n",
		"internal/y.go":   "package internal\n",
		"no-newline.go":   "a\nb",
		"internal/doc.md": "doc\n",
	} {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestParseAtRefuses(t *testing.T) {
	root := evidenceRepo(t)
	for _, at := range []string{
		"x.go", "x.go:0", "x.go:abc", "x.go:5", "x.go:3-2", "x.go:3-", "x.go:-3", "../out.go:1",
		"/etc/passwd:1", "missing.go:1", "internal:1", "", ":3", "no-newline.go:3",
	} {
		_, err := prtriage.ParseAt(root, at)
		if err == nil {
			t.Errorf("ParseAt(%q) accepted", at)
			continue
		}
		if !strings.Contains(err.Error(), `"`+at+`"`) {
			t.Errorf("ParseAt(%q) error does not name the value: %v", at, err)
		}
	}
	for at, want := range map[string]string{
		"x.go:3":          "x.go:3",
		"x.go:2-4":        "x.go:2-4",
		"x.go:4":          "x.go:4",
		"a:b.go:3":        "a:b.go:3",
		"./x.go:1":        "x.go:1",
		"internal/y.go:1": "internal/y.go:1",
		"no-newline.go:2": "no-newline.go:2",
	} {
		ev, err := prtriage.ParseAt(root, at)
		if err != nil {
			t.Errorf("ParseAt(%q): %v", at, err)
			continue
		}
		if ev != (prtriage.Evidence{At: want}) {
			t.Errorf("ParseAt(%q) = %+v, want At %q only", at, ev, want)
		}
	}
}

func TestSplitAtReadsNothing(t *testing.T) {
	file, start, end, err := prtriage.SplitAt("a:b.go:3-7")
	if err != nil || file != "a:b.go" || start != 3 || end != 7 {
		t.Errorf("SplitAt = %q %d %d %v, want a:b.go 3 7", file, start, end, err)
	}
	// Nothing at this path exists; SplitAt is lexical, so it is accepted.
	if _, _, _, err := prtriage.SplitAt("no/such/file.go:9"); err != nil {
		t.Errorf("SplitAt opened something: %v", err)
	}
	for _, at := range []string{"../x.go:1", "/abs.go:1", "x.go:0", "x.go:2-1", "x.go", " :3", ".:3", "./:3"} {
		if _, _, _, err := prtriage.SplitAt(at); err == nil {
			t.Errorf("SplitAt(%q) accepted", at)
		}
	}
}

func TestFromOutputNeverStoresOutput(t *testing.T) {
	const sentinel = "SENTINEL-OUTPUT-7f3a"
	output := "ok  \tgithub.com/x\n" + sentinel + "\n"
	ev, err := prtriage.FromOutput("go test ./x", output)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(output))
	if ev.OutputSHA256 != hex.EncodeToString(sum[:]) || ev.OutputBytes != len(output) || ev.Cmd != "go test ./x" || ev.At != "" {
		t.Errorf("evidence = %+v", ev)
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(ev); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), sentinel) {
		t.Errorf("the output was persisted:\n%s", buf.String())
	}
	for _, c := range [][2]string{{"", "out"}, {" \t", "out"}, {"go test", ""}, {"go test", "\n\t\n"}} {
		if _, err := prtriage.FromOutput(c[0], c[1]); err == nil {
			t.Errorf("FromOutput(%q, %q) accepted", c[0], c[1])
		}
	}
}

func TestRequireEvidence(t *testing.T) {
	for _, none := range []prtriage.Evidence{{}, {At: "  ", Cmd: "\t"}} {
		err := prtriage.Require(none)
		if !errors.Is(err, prtriage.ErrNoEvidence) || !strings.Contains(err.Error(), "is required") ||
			!strings.Contains(err.Error(), "--at") || !strings.Contains(err.Error(), "--cmd") {
			t.Errorf("Require(%+v) = %v, want the is-required error naming --at and --cmd", none, err)
		}
	}
	out, _ := prtriage.FromOutput("go vet", "clean\n")
	for _, ok := range []prtriage.Evidence{{At: "x.go:1"}, out} {
		if err := prtriage.Require(ok); err != nil {
			t.Errorf("Require(%+v) refused: %v", ok, err)
		}
	}
	for name, ev := range map[string]prtriage.Evidence{
		"both":            {At: "x.go:1", Cmd: "go vet", OutputSHA256: out.OutputSHA256, OutputBytes: 6},
		"cmd, no digest":  {Cmd: "go vet", OutputBytes: 6},
		"cmd, no length":  {Cmd: "go vet", OutputSHA256: out.OutputSHA256},
		"cmd, short hash": {Cmd: "go vet", OutputSHA256: "abc", OutputBytes: 6},
		"digest, no cmd":  {OutputSHA256: out.OutputSHA256, OutputBytes: 6},
		"blank cmd":       {Cmd: "  ", OutputSHA256: out.OutputSHA256, OutputBytes: 6},
		"at with digest":  {At: "x.go:1", OutputSHA256: out.OutputSHA256},
		"at with length":  {At: "x.go:1", OutputBytes: 6},
		"at, no line":     {At: "x.go"},
		"at escaping":     {At: "../x.go:1"},
		"at, no file":     {At: " :3"},
		"at, dot":         {At: "./:3"},
		"non-hex digest":  {Cmd: "go vet", OutputSHA256: strings.Repeat("z", 64), OutputBytes: 6},
		"upper-case hex":  {Cmd: "go vet", OutputSHA256: strings.ToUpper(out.OutputSHA256), OutputBytes: 6},
	} {
		if err := prtriage.Require(ev); err == nil {
			t.Errorf("%s: Require accepted %+v", name, ev)
		}
	}
}

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fixtureModule is a throwaway module whose packages each produce one shape
// the reducer must handle, as the real toolchain emits it — not a hand-written
// guess at test2json's output. The sources live here, inline, and are written
// to a temp dir: a tracked .go file that does not compile would break every
// sweep that parses the tree.
const fixtureModule = "example.com/fx"

var fixtureSources = map[string]string{
	"go.mod": "module " + fixtureModule + "\n\ngo 1.21\n",

	// one passing and one failing test, plus a parent with a failing subtest
	"mixed/mixed_test.go": `package mixed

import "testing"

func TestPasses(t *testing.T) { t.Log("pass-noise") }

func TestFails(t *testing.T) { t.Log("before"); t.Error("boom-marker") }

func TestParent(t *testing.T) {
	t.Run("ok", func(t *testing.T) { t.Log("sub-pass-noise") })
	t.Run("bad", func(t *testing.T) { t.Error("sub-boom") })
}
`,

	// a test blocked past -timeout, after one that passed
	"hang/hang_test.go": `package hang

import (
	"testing"
	"time"
)

func TestQuick(t *testing.T) { t.Log("quick-noise") }

func TestBlocks(t *testing.T) { time.Sleep(time.Hour) }
`,

	// a compile error
	"broken/broken.go":      "package broken\n\nfunc F() int { return \"x\" }\n",
	"broken/broken_test.go": "package broken\n\nimport \"testing\"\n\nfunc TestB(t *testing.T) {}\n",

	// no test files: go1.27.1 ends this package with "Action":"skip"
	"notests/notests.go": "package notests\n\nfunc N() {}\n",

	// all green
	"passing/passing_test.go": "package passing\n\nimport \"testing\"\n\nfunc TestFine(t *testing.T) { t.Log(\"fine-noise\") }\n",
}

var (
	fixtureOnce   sync.Once
	fixtureLines  []string
	fixtureErr    error
	fixtureStderr string
)

// fixtureStream is the fixture module's `go test -json` stream, restricted to
// the named packages. The run happens once per test binary; a failed run is
// re-raised to every caller rather than handing a later one an empty stream.
func fixtureStream(t *testing.T, pkgs ...string) string {
	t.Helper()
	fixtureOnce.Do(func() { fixtureLines, fixtureStderr, fixtureErr = runFixture(t.TempDir()) })
	if fixtureErr != nil {
		t.Fatalf("fixture go test: %v\nstderr:\n%s", fixtureErr, fixtureStderr)
	}
	want := map[string]bool{}
	for _, p := range pkgs {
		want[fixtureModule+"/"+p] = true
	}
	var b strings.Builder
	for _, line := range fixtureLines {
		if want[linePackage(line)] {
			b.WriteString(line + "\n")
		}
	}
	if b.Len() == 0 {
		t.Fatalf("the fixture stream holds no events for %v", pkgs)
	}
	return b.String()
}

// runFixture writes the module to dir and runs the real toolchain over it:
// GOTOOLCHAIN=local so it is the go compiling this test, GOWORK and GOPROXY
// off so nothing outside dir is consulted, GOFLAGS cleared so the caller's
// flags don't leak in.
func runFixture(dir string) (lines []string, stderr string, err error) {
	for rel, src := range fixtureSources {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return nil, "", err
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			return nil, "", err
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "-timeout", "1s", "-json", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off", "GOFLAGS=")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	runErr := cmd.Run()
	// go test exits 1 here by design — the module has failing packages — so
	// the run is judged by its stream, not its exit status.
	for _, l := range strings.Split(out.String(), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	if !strings.Contains(out.String(), `"Action":"start"`) {
		return nil, errb.String(), fmt.Errorf("no package started (exit: %v)", runErr)
	}
	return lines, errb.String(), nil
}

// linePackage is the package a stream line belongs to: Package for a test2json
// event, the first field of ImportPath (`<pkg> [<pkg>.test]`) for a build event.
func linePackage(line string) string {
	var e struct{ Package, ImportPath string }
	if json.Unmarshal([]byte(line), &e) != nil {
		return ""
	}
	if e.Package != "" {
		return e.Package
	}
	pkg, _, _ := strings.Cut(e.ImportPath, " ")
	return pkg
}

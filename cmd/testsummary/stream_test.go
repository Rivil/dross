package main

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// reduce runs the command over stream and returns its stdout and exit code.
func reduce(t *testing.T, stream string) (string, int) {
	t.Helper()
	var out strings.Builder
	code := run(strings.NewReader(stream), &out, func(string) string { return "" })
	return out.String(), code
}

// ev renders one synthetic test2json event line.
func ev(action, pkg, test, output string) string {
	b, err := json.Marshal(map[string]any{"Action": action, "Package": pkg, "Test": test, "Output": output})
	if err != nil {
		panic(err)
	}
	return string(b) + "\n"
}

func mustContain(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("output lacks %q:\n%s", w, out)
		}
	}
}

func mustNotContain(t *testing.T, out string, nots ...string) {
	t.Helper()
	for _, n := range nots {
		if strings.Contains(out, n) {
			t.Errorf("output carries %q:\n%s", n, out)
		}
	}
}

func TestReducerRealFailingModule(t *testing.T) {
	out, code := reduce(t, fixtureStream(t, "mixed"))
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	mustContain(t, out, "--- FAIL: TestFails", "boom-marker", "--- FAIL: TestParent/bad", "sub-boom",
		"FAIL\t"+fixtureModule+"/mixed")
	mustNotContain(t, out, "pass-noise", "=== RUN   TestPasses", "sub-pass-noise", "=== RUN   TestParent/ok")
}

func TestReducerRealTimeout(t *testing.T) {
	out, code := reduce(t, fixtureStream(t, "hang"))
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	mustContain(t, out, "panic: test timed out", "TestBlocks", "FAIL\t"+fixtureModule+"/hang")
	mustNotContain(t, out, "quick-noise", "=== RUN   TestQuick")
}

func TestReducerRealBuildFailure(t *testing.T) {
	out, code := reduce(t, fixtureStream(t, "broken"))
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	mustContain(t, out, "broken/broken.go:3:", "[build failed]")
}

// TestReducerNoTestFilesPackage: a test-less package ends in "skip", which is a
// finished package — under `set -o pipefail` treating it as cut off would
// redden CI on a green run.
func TestReducerNoTestFilesPackage(t *testing.T) {
	out, code := reduce(t, fixtureStream(t, "notests", "passing"))
	if code != 0 {
		t.Errorf("exit = %d, want 0:\n%s", code, out)
	}
	mustContain(t, out, "[no test files]", "ok  \t"+fixtureModule+"/passing")
	mustNotContain(t, out, "fine-noise", "FAIL")
}

func TestReducerVerdict(t *testing.T) {
	const pkg = "example.com/p"
	pass := ev("start", pkg, "", "") +
		ev("run", pkg, "TestA", "") +
		ev("output", pkg, "TestA", "=== RUN   TestA\n") +
		ev("pass", pkg, "TestA", "") +
		ev("output", pkg, "", "PASS\n") +
		ev("output", pkg, "", "ok  \t"+pkg+"\t0.1s\n") +
		ev("pass", pkg, "", "")

	t.Run("all pass exits 0", func(t *testing.T) {
		out, code := reduce(t, pass)
		if code != 0 {
			t.Errorf("exit = %d, want 0:\n%s", code, out)
		}
		mustContain(t, out, "ok  \t"+pkg)
		mustNotContain(t, out, "=== RUN")
	})
	t.Run("empty stream exits non-zero", func(t *testing.T) {
		out, code := reduce(t, "")
		if code == 0 {
			t.Error("an empty stream exited 0")
		}
		mustContain(t, out, "no test events")
	})
	t.Run("non-JSON only exits non-zero", func(t *testing.T) {
		out, code := reduce(t, "go: something went wrong\n")
		if code == 0 {
			t.Error("a stream with no events exited 0")
		}
		mustContain(t, out, "no test events", "go: something went wrong")
	})
	t.Run("cut mid-package flushes and exits non-zero", func(t *testing.T) {
		cut := ev("start", pkg, "", "") +
			ev("run", pkg, "TestA", "") +
			ev("output", pkg, "TestA", "=== RUN   TestA\n") +
			ev("output", pkg, "TestA", "    a_test.go:3: halfway-marker\n")
		out, code := reduce(t, cut)
		if code == 0 {
			t.Errorf("a cut stream exited 0:\n%s", out)
		}
		mustContain(t, out, "halfway-marker", "FAIL\t"+pkg)
	})
	t.Run("a second, cut package fails an otherwise green stream", func(t *testing.T) {
		out, code := reduce(t, pass+ev("start", "example.com/q", "", ""))
		if code == 0 {
			t.Errorf("exit 0 with a package that never finished:\n%s", out)
		}
	})
}

func TestReducerLongAndNonJSONLines(t *testing.T) {
	const pkg = "example.com/p"
	long := strings.Repeat("x", 1<<20)
	body := func(testEnd, pkgEnd string) string {
		return "plain-marker: not JSON\n" +
			ev("start", pkg, "", "") +
			ev("run", pkg, "TestLong", "") +
			ev("output", pkg, "TestLong", long+"\n") +
			ev(testEnd, pkg, "TestLong", "") +
			ev(pkgEnd, pkg, "", "")
	}

	out, code := reduce(t, body("fail", "fail"))
	if code != 1 {
		t.Errorf("failing stream: exit = %d, want 1", code)
	}
	if !strings.Contains(out, long) {
		t.Errorf("the 1 MiB output line of a failed test was not printed whole (output is %d bytes)", len(out))
	}
	mustContain(t, out, "plain-marker: not JSON")

	out, code = reduce(t, body("pass", "pass"))
	if code != 0 {
		t.Errorf("passing stream with a 1 MiB line: exit = %d, want 0:\n%.300s", code, out)
	}
	mustContain(t, out, "plain-marker: not JSON")
}

// TestStdlibOnly holds the timing_tooling decision: the summary is built
// in-repo from the standard library. A path whose first element has a dot is
// a module from somewhere else.
func TestStdlibOnly(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	checked := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(".", name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			first, _, _ := strings.Cut(path, "/")
			if strings.Contains(first, ".") {
				t.Errorf("%s imports %s — cmd/testsummary is stdlib-only", name, path)
			}
		}
	}
	if checked < 2 {
		t.Fatalf("checked %d non-test files, want main.go and stream.go at least", checked)
	}
}

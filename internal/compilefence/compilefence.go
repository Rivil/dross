// Package compilefence asserts that a Go source fixture does NOT compile.
//
// Some guarantees are carried by the type system rather than by a runtime
// check: pathfence.Contained has unexported fields, so a caller that skipped
// the containment check has no value to pass and fails to BUILD. An assertion
// like that cannot be written in ordinary Go — the fixture would have to be
// valid source in the test package to be referenced at all — so it needs a real
// `go build` over a throwaway module.
//
// WHY ITS OWN NON-TEST PACKAGE. An identifier defined in a _test.go file is
// visible only inside that package's own test binary. This helper is called
// from internal/pathfence, internal/security, internal/quality and
// internal/cmd, so living in pathfence_test.go would make it unreachable from
// three of its four callers. A plain .go file in its own package is importable
// by all of them; it takes testing.TB rather than *testing.T so it needs no
// test-only build context, and it imports nothing from internal/, so it cannot
// form a cycle with any caller. Nothing in production imports it, and a guard
// test asserts that stays true.
//
// PRECEDENT AND DIVERGENCE. The shape mirrors
// internal/mutation.assertStringMinusDoesNotCompile, which writes a temp module
// and runs `go build ./...`. The go.mod is deliberately NOT copied from it: see
// Module below and the -mod=mod literal in build. Getting either wrong makes
// every fixture fail for the wrong reason, which a negative-only assertion
// reads as success — the one way this whole mechanism silently becomes theatre.
package compilefence

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Module is the import path of the generated temp module.
//
// It MUST sit under github.com/Rivil/dross/. A module outside that prefix may
// not import github.com/Rivil/dross/internal/... at all — the build dies with
// "use of internal package ... not allowed" before it ever type-checks the
// fixture. internal/mutation's helper declares `module mutantproof` and gets
// away with it only because its fixtures import nothing from this repo.
const Module = "github.com/Rivil/dross/tmpfence"

// AssertDoesNotCompile requires src to fail to build, with wantMsg somewhere in
// the compiler output.
//
// wantMsg is not optional politeness: without it, any build failure passes —
// including the two failure modes this package exists to avoid. Pass the
// compiler's actual wording. Note that Go distinguishes them:
//
//	Contained{rel: "x"} -> "cannot refer to unexported field rel"
//	Contained{p: "x"}   -> "unknown field p"     (also fires if p were EXPORTED)
//
// so a fixture naming a field that does not exist asserts nothing about
// unexported-ness.
func AssertDoesNotCompile(t testing.TB, src, wantMsg string) {
	t.Helper()
	out, err := build(t, src)
	if err == nil {
		t.Fatalf("fixture COMPILED but should not have.\nwant a failure naming %q\nsource:\n%s", wantMsg, src)
	}
	if !strings.Contains(out, wantMsg) {
		t.Fatalf("fixture failed to build for the WRONG reason.\nwant output naming %q\ngot:\n%s\nsource:\n%s",
			wantMsg, out, src)
	}
}

// AssertCompiles requires src to build clean.
//
// This is the positive control, and it is the line that catches the two
// mechanics that would otherwise make every AssertDoesNotCompile a false
// positive: a module path outside the dross prefix (internal-package
// visibility) and an unresolvable go.mod. Both produce a failing build that a
// negative-only fence reads as success. Call it before relying on any negative
// assertion.
func AssertCompiles(t testing.TB, src string) {
	t.Helper()
	if out, err := build(t, src); err != nil {
		t.Fatalf("fixture did NOT compile but should have — the compile fence itself is broken, "+
			"so every AssertDoesNotCompile in this suite is passing for the wrong reason.\ngot:\n%s\nsource:\n%s",
			out, src)
	}
}

// build writes src into a temp module and runs `go build ./...`, returning the
// combined output.
func build(t testing.TB, src string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	root := repoRoot(t)

	gomod := "module " + Module + "\n\ngo " + goVersion(t) + "\n\n" +
		"require github.com/Rivil/dross v0.0.0\n\n" +
		"replace github.com/Rivil/dross => " + root + "\n"
	write(t, filepath.Join(dir, "go.mod"), gomod)
	write(t, filepath.Join(dir, "fixture.go"), src)

	// -mod=mod stops a go.mod carrying only require+replace failing module
	// resolution ("updates to go.mod needed") before it compiles anything —
	// another wrong-reason failure. It is a LITERAL rather than a named
	// constant because the subprocargs audit resolves argv syntactically: `go`
	// has no end-of-options token, so an identifier reads as a derived
	// positional it cannot fence, while a string literal is provably
	// unreachable by any caller.
	//dross:exec-exempt test-only compile fence: every argv element is a string literal ("go", "build", "-mod=mod", "./..."), the working directory is a t.TempDir the helper just wrote, and no repo-authored value reaches the command line — the fixture under test is passed as a FILE, never as an argument
	cmd := exec.Command("go", "build", "-mod=mod", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// repoRoot locates the dross checkout from this source file's own path, so the
// replace directive points at the tree under test rather than at whatever the
// working directory happens to be.
func repoRoot(t testing.TB) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate compilefence source path")
	}
	// <root>/internal/compilefence/compilefence.go -> <root>
	return filepath.Dir(filepath.Dir(filepath.Dir(self)))
}

// goVersion reads the go directive from the repo's own go.mod, so the temp
// module never declares a newer toolchain than the tree it replaces in.
func goVersion(t testing.TB) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, found := strings.CutPrefix(strings.TrimSpace(line), "go "); found {
			return strings.TrimSpace(v)
		}
	}
	t.Fatal("no go directive in go.mod")
	return ""
}

func write(t testing.TB, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

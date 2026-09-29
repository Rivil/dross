package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

// fixtureCoverFlags are the go test flags CI's test job passes alongside
// -coverprofile. The self-test below runs the fixture with exactly these, so
// what it proves about the real toolchain is what CI's profile holds; the CI
// wiring test pins the workflow's go test line to this same list.
var fixtureCoverFlags = []string{"-race", "-count=1", "-json"}

// fixtureModulePath is the self-test's throwaway module. Its sources live here,
// inline, and are written to a temp dir, so no tracked package ever sits below
// the floor on purpose.
const fixtureModulePath = "example.com/floorfx"

var floorFixture = map[string]string{
	"go.mod": "module " + fixtureModulePath + "\n\ngo 1.21\n",

	// 1 of 4 statements: 25%.
	"internal/low/low.go":      "package low\n\nfunc A() int { return 1 }\nfunc B() int { return 2 }\nfunc C() int { return 3 }\nfunc D() int { return 4 }\n",
	"internal/low/low_test.go": "package low\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) { _ = A() }\n",

	// No test files at all: 0%, and still in the profile.
	"internal/untested/u.go": "package untested\n\nfunc U() int { return 1 }\n",

	// Fully exercised — but only from internal/user's tests, which a
	// per-package run does not attribute here: 0% own-package.
	"internal/lib/lib.go":        "package lib\n\nfunc L() int { return 1 }\n",
	"internal/user/user.go":      "package user\n\nimport \"" + fixtureModulePath + "/internal/lib\"\n\nfunc Use() int { return lib.L() }\n",
	"internal/user/user_test.go": "package user\n\nimport \"testing\"\n\nfunc TestUse(t *testing.T) { _ = Use() }\n",

	// Shares internal/cmd's prefix without its trailing slash: in scope, 0%.
	"internal/cmdline/x.go": "package cmdline\n\nfunc X() int { return 1 }\n",

	// Exactly on the floor: 1 of 2.
	"internal/edge/edge.go":      "package edge\n\nfunc E() int { return 1 }\nfunc F() int { return 2 }\n",
	"internal/edge/edge_test.go": "package edge\n\nimport \"testing\"\n\nfunc TestE(t *testing.T) { _ = E() }\n",

	// The one exclusion: 0%, not reported.
	"internal/cmd/c.go": "package cmd\n\nfunc C() int { return 1 }\n",
}

// TestFixtureBelowFloorFailsTheCheck is c-4's self-test: a below-floor file
// produced by the real toolchain, with CI's own flags, fails the check. A
// synthetic profile could agree with the checker's assumptions about the
// format and still miss what go test actually writes — a package with no test
// files, say.
func TestFixtureBelowFloorFailsTheCheck(t *testing.T) {
	dir := t.TempDir()
	for rel, src := range floorFixture {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	prof := filepath.Join(dir, "cover.out")
	args := append([]string{"test"}, fixtureCoverFlags...)
	args = append(args, "-coverprofile="+prof, "./...")
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	// GOTOOLCHAIN=local so it is the go compiling this test; GOWORK and
	// GOPROXY off so nothing outside dir is consulted; GOFLAGS cleared so the
	// caller's flags don't leak in.
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off", "GOFLAGS=")
	var gout bytes.Buffer
	cmd.Stdout, cmd.Stderr = &gout, &gout
	if err := cmd.Run(); err != nil {
		t.Fatalf("fixture go test: %v\n%s", err, gout.String())
	}

	var out, errb bytes.Buffer
	if code := run(dir, []string{prof}, &out, &errb); code != 1 {
		t.Fatalf("exit %d, want 1\nstdout:\n%s\nstderr:\n%s", code, out.String(), errb.String())
	}
	want := []string{"internal/cmdline/x.go", "internal/lib/lib.go", "internal/low/low.go", "internal/untested/u.go"}
	if got := offendersIn(out.String()); !reflect.DeepEqual(got, want) {
		t.Errorf("offenders = %v, want %v\nstdout:\n%s", got, want, out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("internal/low/low.go 25.0% (1/4)")) {
		t.Errorf("low.go is not reported at 25%%:\n%s", out.String())
	}
}

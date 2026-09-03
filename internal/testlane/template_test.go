package testlane

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// TestExpandRepeatsTheTemplatePerPath is the cargo shape: `--package {path}`
// over two paths has to become two flag/value pairs, not one flag with two
// values. A runner that takes a repeated flag cannot be reached any other way,
// which is the whole reason {path} exists beside {paths}.
func TestExpandRepeatsTheTemplatePerPath(t *testing.T) {
	got, err := Expand("--package {path}", "", []string{"crates/a", "crates/b"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--package 'crates/a'", "--package 'crates/b'"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestExpandSubstitutesAllPathsIntoOneInstance: {paths} with no join yields a
// single fragment carrying separately-quoted tokens — the trailing path list a
// runner that takes files at the end of its line wants.
func TestExpandSubstitutesAllPathsIntoOneInstance(t *testing.T) {
	got, err := Expand("--only {paths}", "", []string{"a.go", "b.go"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--only 'a.go' 'b.go'"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestExpandJoinsPathsIntoOneArgument is the ctest shape and the reason
// selector_join is a declared field: a regex alternation is unreachable without
// a separator, and the joined string must land as ONE argument — a quote
// boundary between the members would hand ctest two arguments and the
// alternation would never form.
func TestExpandJoinsPathsIntoOneArgument(t *testing.T) {
	got, err := Expand("-R {paths}", "|", []string{"tests/a", "tests/b"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-R 'tests/a|tests/b'"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	if strings.Count(got[0], "'") != 2 {
		t.Errorf("joined paths must be one quoted argument, got %q", got[0])
	}
}

// TestExpandQuotesAPathCarryingShellMetacharacters is c-2's injection half. The
// fragment goes to `sh -c`, so a path spelled with a semicolon must stay one
// argument and can never become a second command.
func TestExpandQuotesAPathCarryingShellMetacharacters(t *testing.T) {
	got, err := Expand("--package {path}", "", []string{"a; rm -rf /"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{`--package 'a; rm -rf /'`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}

	// The embedded-quote escape is the half a naive wrapper gets wrong: a path
	// carrying its own single quote must not be able to close the quoting and
	// open a command.
	got, err = Expand("--package {path}", "", []string{`a'; rm -rf /; '`})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got[0], `'; rm -rf /; '`) && !strings.Contains(got[0], `'\''`) {
		t.Errorf("an embedded single quote escaped the quoting: %q", got[0])
	}
}

// TestExpandKeepsASubstitutedPathOptionSafe: a file named `-x.go` substituted
// verbatim reads as a flag to every runner ever written. `./-x.go` names the
// same file and cannot be read as anything else — the same treatment the plain
// append path already gives a derived argument.
func TestExpandKeepsASubstitutedPathOptionSafe(t *testing.T) {
	got, err := Expand("{path}", "", []string{"-x.go"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"'./-x.go'"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestExpandEmitsTemplateTextVerbatim is the fence half of the locked
// template_fence decision: the template is the user's own consented text, so
// its metacharacters, quotes and regex syntax reach the line unchanged. Quoting
// or rewriting it would break every legitimate flag and alternation in it while
// fencing a line the user has already read and granted.
func TestExpandEmitsTemplateTextVerbatim(t *testing.T) {
	const tmpl = `-R "^a{2,3}$|{path}" --flag='x y'`
	got, err := Expand(tmpl, "", []string{"t"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{`-R "^a{2,3}$|'t'" --flag='x y'`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("template text was not emitted verbatim:\n got %q\nwant %q", got, want)
	}
}

// TestExpandRefusesATemplateWithNoPlaceholder: a placeholder-less template
// substitutes nothing, so honouring it would spawn the lane's whole command
// under a scoped lane's name. It must be an error AND yield no fragments — a
// caller ignoring the error and spawning anyway is exactly the silent unscoped
// run the feature exists to avoid. This error is what the run site's up-front
// fence surfaces before any lane spawns.
func TestExpandRefusesATemplateWithNoPlaceholder(t *testing.T) {
	for _, tmpl := range []string{"", "--package", "a{2,3}"} {
		got, err := Expand(tmpl, "", []string{"a.go"})
		if err == nil {
			t.Errorf("Expand(%q) returned fragments %q instead of an error", tmpl, got)
		}
		if got != nil {
			t.Errorf("Expand(%q) returned %q alongside its error", tmpl, got)
		}
	}
}

// TestExpandWithNoPathsIsAPureFence: the run site checks a template against no
// paths before spawning anything, so a well-formed template must come back
// empty and clean while a malformed one still refuses.
func TestExpandWithNoPathsIsAPureFence(t *testing.T) {
	got, err := Expand("--package {path}", "", nil)
	if err != nil {
		t.Fatalf("a well-formed template refused on the empty fence: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("the empty fence produced fragments: %q", got)
	}
	if _, err := Expand("--package", "", nil); err == nil {
		t.Error("the empty fence accepted a placeholder-less template")
	}
}

// TestShellQuoteIsTheOnlyQuoterInTheRepo is the moved-not-copied assertion. The
// point of moving the quoter into this package is that template text and
// substituted path are indistinguishable once they are one string, so there can
// only be ONE answer to "what is safe" — and a second implementation is the
// cheapest way to satisfy the compiler after a move, which defeats it.
//
// internal/cmd is scanned rather than trusted: `dross run` and `dross test`
// both quote, and either could quietly regrow a local copy.
func TestShellQuoteIsTheOnlyQuoterInTheRepo(t *testing.T) {
	entries, err := filepath.Glob(filepath.Join("..", "cmd", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("scanned no files in internal/cmd — the assertion would pass vacuously")
	}
	// The shape of a single-quoting implementation: an escape of the embedded
	// single quote. Matched rather than a function name, since a copy is as
	// likely to be renamed as to keep the old one.
	quoter := regexp.MustCompile(`ReplaceAll\([^)]*"'"`)
	for _, path := range entries {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if quoter.Match(src) {
			t.Errorf("%s carries its own shell quoter — `dross run` and `dross test` must quote through testlane.ShellQuote", path)
		}
	}
}

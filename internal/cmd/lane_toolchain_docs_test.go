package cmd

// The three documents that describe `dross test`'s toolchain surface, checked
// against the code rather than against each other.
//
// A doc test earns its place only where drift is silent. Every claim asserted
// here is one a reader ACTS on: an exit code they gate a commit on, a flag they
// type, and the distinction between a lane that fell back and a host that never
// answered — which decides whether they go and fix a machine or ignore a line.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func readDoc(t *testing.T, parts ...string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(append([]string{repoRootForDocs(t)}, parts...)...))
	if err != nil {
		t.Fatalf("read %v: %v", parts, err)
	}
	return string(b)
}

// readmeRow returns the README table row whose first cell starts with cmd.
// Asserted per ROW because the README is one long table: "the README mentions
// --toolchain" would be satisfied by a mention three commands away.
func readmeRow(t *testing.T, cmd string) string {
	t.Helper()
	for _, line := range strings.Split(readDoc(t, "README.md"), "\n") {
		if strings.HasPrefix(line, "| `"+cmd) {
			return line
		}
	}
	t.Fatalf("no README row for %q", cmd)
	return ""
}

// TestReadmeTestRowCarriesExitEight: the exit codes are the contract a caller
// gates a commit on, and the README row is where they are enumerated. A code
// the table does not list is one nobody knows not to treat as a red suite.
func TestReadmeTestRowCarriesExitEight(t *testing.T) {
	row := readmeRow(t, "dross test [selector...]")
	if !strings.Contains(row, "`8`") {
		t.Errorf("the `dross test` row does not name exit 8:\n%s", row)
	}
	if !strings.Contains(row, "neither") {
		t.Errorf("the `dross test` row does not say the binary is on neither machine:\n%s", row)
	}
	if !strings.Contains(row, "toolchain > red") {
		t.Errorf("the rank chain in the README does not place toolchain above red:\n%s", row)
	}
}

// TestReadmeLaneRowNamesToolchain: the flag is how a user fixes a lane pinned
// to local by a first token that is not a binary. Undocumented, the fix exists
// and nobody finds it.
func TestReadmeLaneRowNamesToolchain(t *testing.T) {
	row := readmeRow(t, "dross test lane {add,list,edit,remove}")
	if !strings.Contains(row, "--toolchain") {
		t.Errorf("the `dross test lane` row does not name --toolchain:\n%s", row)
	}
	if !strings.Contains(row, "derived") {
		t.Errorf("the row does not say the list is derived when the flag is omitted:\n%s", row)
	}
}

// TestOptionsNamesToolchainAndTheSplit: options.md is the settings surface, so
// it carries the flag AND the distinction that decides what a reader does
// about a line — a lane that fell back needs nothing, a host that never
// answered needs looking at.
func TestOptionsNamesToolchainAndTheSplit(t *testing.T) {
	prompt := readDoc(t, "assets", "prompts", "options.md")
	if !strings.Contains(prompt, "--toolchain") {
		t.Error("options.md does not name `dross test lane add|edit --toolchain`")
	}
	if !strings.Contains(prompt, "could not reach") {
		t.Error("options.md does not distinguish a toolchain fallback from an unreachable host")
	}
	if !strings.Contains(prompt, "exits `8`") {
		t.Error("options.md does not name the exit code a lane no machine can run takes")
	}
}

// TestExecuteListsEveryExitCode walks the const block rather than a hand-typed
// list. A code added to test.go without a line in execute.md is a status the
// agent driving the loop has no rule for, and the default reading of a
// non-zero exit is "your code is broken".
func TestExecuteListsEveryExitCode(t *testing.T) {
	prompt := readDoc(t, "assets", "prompts", "execute.md")
	for _, code := range declaredExitValues(t) {
		if code == 0 {
			continue
		}
		if !strings.Contains(prompt, "- **"+strconv.Itoa(code)+"** — ") {
			t.Errorf("execute.md's exit-code list has no entry for %d", code)
		}
	}
}

// TestExecuteSummaryNamesEveryNonRedCode: the list above passes on an entry
// alone, and the sentence that follows it is the one an agent skims. Left
// stale, it says in so many words that 8 IS a red suite.
func TestExecuteSummaryNamesEveryNonRedCode(t *testing.T) {
	prompt := readDoc(t, "assets", "prompts", "execute.md")
	idx := strings.Index(prompt, " all mean the run did not happen")
	if idx < 0 {
		t.Fatal("execute.md no longer carries the run-did-not-happen sentence")
	}
	sentence := prompt[strings.LastIndex(prompt[:idx], "\n")+1:][:idx-strings.LastIndex(prompt[:idx], "\n")-1]
	for _, code := range declaredExitValues(t) {
		if code == 0 || code == exitSuiteFailed {
			continue
		}
		if !strings.Contains(sentence, strconv.Itoa(code)) {
			t.Errorf("the summary sentence does not name %d: %q", code, sentence)
		}
	}
	if strings.Contains(sentence, " "+strconv.Itoa(exitSuiteFailed)+",") {
		t.Errorf("the summary sentence lists the red-suite code, which DID happen: %q", sentence)
	}
}

// TestExecuteEightRemedyDoesNotOverstep: installing the binary on the remote is
// the deferred remote-toolchain-install phase, not this one. A prompt telling
// the agent to install it would have it do something dross has no verb for and
// the user never authorized.
func TestExecuteEightRemedyDoesNotOverstep(t *testing.T) {
	prompt := readDoc(t, "assets", "prompts", "execute.md")
	start := strings.Index(prompt, "- **8** — ")
	if start < 0 {
		t.Fatal("execute.md has no entry for exit 8")
	}
	end := strings.Index(prompt[start:], "\n\n")
	if end < 0 {
		t.Fatal("execute.md's exit-8 entry does not end")
	}
	entry := prompt[start : start+end]
	for _, verb := range []string{"install ", "installing", "apt", "brew", "dross remote bootstrap"} {
		if strings.Contains(strings.ToLower(entry), verb) {
			t.Errorf("the exit-8 remedy reaches for %q, which is the deferred install phase:\n%s", verb, entry)
		}
	}
	if !strings.Contains(entry, "neither") {
		t.Errorf("the exit-8 entry does not say both machines lack it:\n%s", entry)
	}
}

// declaredExitValues reads the exit* constants' VALUES out of test.go. Untyped
// int constants leave nothing behind at run time to enumerate, and a hand-typed
// list here would be the very drift these tests exist to catch.
func declaredExitValues(t *testing.T) []int {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "test.go", nil, 0)
	if err != nil {
		t.Fatalf("parse test.go: %v", err)
	}
	var codes []int
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs := spec.(*ast.ValueSpec)
			for i, name := range vs.Names {
				if !strings.HasPrefix(name.Name, "exit") || i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.INT {
					continue
				}
				n, cerr := strconv.Atoi(lit.Value)
				if cerr != nil {
					continue
				}
				codes = append(codes, n)
			}
		}
	}
	if len(codes) == 0 {
		t.Fatal("found no exit* constants in test.go — the parse walked the wrong file")
	}
	return codes
}

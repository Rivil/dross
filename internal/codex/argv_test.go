package codex

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/argfence"
)

// The Separator half of the locked analyzer_arg_policy decision.
//
// ast-grep honours an end-of-options token, so it gets the stronger of the two
// defences: the file operand goes behind `--` and is a path by construction,
// whatever it contains. --lang is the exception — its value is read by
// ast-grep's own parser ahead of any separator, so it is checked against the
// known set instead.

// refuseAstGrepExec installs a spawn seam that fails the test if reached, so
// "rejected with no process spawned" is asserted rather than assumed.
func refuseAstGrepExec(t *testing.T) {
	t.Helper()
	prev := astGrepOutput
	astGrepOutput = func(argv []string) ([]byte, error) {
		t.Fatalf("ast-grep was spawned despite a value that should have been refused: %v", argv)
		return nil, nil
	}
	t.Cleanup(func() { astGrepOutput = prev })
}

// TestAstGrepArgvSeparator asserts BY INDEX, not by substring search. A search
// for "--" cannot tell a separator in the right place from one in the wrong
// place, and placement is the whole property: a `--` after the file fences
// nothing, and one before the flags demotes them into operands.
func TestAstGrepArgvSeparator(t *testing.T) {
	argv, err := astGrepArgv("src/App.svelte", "svelte", "function $NAME($_) { $$$ }")
	if err != nil {
		t.Fatalf("astGrepArgv: %v", err)
	}
	want := []string{
		"ast-grep", "run",
		"--lang", "svelte",
		"--pattern", "function $NAME($_) { $$$ }",
		"--json=compact",
		"--", "src/App.svelte",
	}
	if !reflect.DeepEqual(argv, want) {
		t.Fatalf("argv = %v\nwant  %v", argv, want)
	}
	// The separator must sit in the slot IMMEDIATELY before the operand.
	if argv[len(argv)-2] != "--" {
		t.Errorf("separator is not immediately before the file operand: %v", argv)
	}

	t.Run("a file named like a flag still lands behind it", func(t *testing.T) {
		argv, err := astGrepArgv("-rf", "ts", "p")
		if err != nil {
			t.Fatalf("a fenced operand must not be rejected: %v", err)
		}
		if argv[len(argv)-2] != "--" || argv[len(argv)-1] != "-rf" {
			t.Fatalf("a file named %q did not land behind the separator: %v", "-rf", argv)
		}
	})
}

func TestAstGrepRejectsUnknownLang(t *testing.T) {
	refuseAstGrepExec(t)

	for _, lang := range []string{"--config=/tmp/x", "-x", "rust", ""} {
		if _, err := astGrepArgv("a.ts", lang, "p"); err == nil {
			t.Errorf("lang %q was accepted", lang)
		}
	}
	// Driven through the real entry point too: a rejection that only held in
	// the builder would still spawn if runAstGrepFn stopped calling it.
	if _, err := runAstGrepFn("a.ts", "--config=/tmp/x", "p"); err == nil {
		t.Error("runAstGrepFn accepted an unknown lang")
	}

	// Every known lang must still pass — over-rejection here silently disables
	// an indexer rather than failing loudly.
	for lang := range astGrepLangs {
		if _, err := astGrepArgv("a.x", lang, "p"); err != nil {
			t.Errorf("known lang %q was rejected: %v", lang, err)
		}
	}
}

// TestAstGrepLangsCoversEveryIndexer keeps the closed set honest: a language
// added in languages.go without a matching entry here would have its indexer
// silently return nothing, which no other test would notice.
func TestAstGrepLangsCoversEveryIndexer(t *testing.T) {
	seen := 0
	for _, idx := range allIndexers() {
		ag, ok := idx.(*AstGrepIndexer)
		if !ok {
			continue
		}
		seen++
		if !astGrepLangs[ag.LangName] {
			t.Errorf("indexer %s uses lang %q, which astGrepLangs does not list — its Symbols() would silently return nothing",
				ag.Name(), ag.LangName)
		}
	}
	if seen == 0 {
		t.Fatal("found no AstGrepIndexers — the walk is broken, not the registry")
	}
	if seen != len(astGrepLangs) {
		t.Errorf("astGrepLangs has %d entries but %d indexers are registered — a stale entry rots the same way an exception list does",
			len(astGrepLangs), seen)
	}
}

func TestAstGrepRejectionNamesToolAndValue(t *testing.T) {
	_, err := astGrepArgv("a.ts", "--config=/tmp/x", "p")
	if err == nil {
		t.Fatal("expected a rejection")
	}
	if !strings.Contains(err.Error(), "ast-grep") {
		t.Errorf("rejection does not name the tool: %v", err)
	}
	if !strings.Contains(err.Error(), "--config=/tmp/x") {
		t.Errorf("rejection does not name the value: %v", err)
	}
}

// TestAstGrepReadsTheSharedPolicy pins that the fencing comes from argfence's
// table rather than a hand-rolled "--" here. If ast-grep's policy is ever
// flipped to Reject, this call site must follow it, not diverge from it.
func TestAstGrepReadsTheSharedPolicy(t *testing.T) {
	rule, ok := argfence.PolicyFor("ast-grep")
	if !ok {
		t.Fatal("ast-grep has no argfence policy")
	}
	if rule.Kind != argfence.Separator {
		t.Fatalf("ast-grep's policy is %v — this call site assumes Separator", rule.Kind)
	}
	argv, err := astGrepArgv("a.ts", "ts", "p")
	if err != nil {
		t.Fatal(err)
	}
	if argv[len(argv)-2] != rule.Token {
		t.Errorf("argv uses %q, but the table says %q", argv[len(argv)-2], rule.Token)
	}
	// And the shared sentinel still governs the Reject direction, so a policy
	// flip surfaces as a wrapped error rather than a silent pass-through.
	if err := argfence.RejectLeadingDash("ast-grep", "file", "-rf"); !errors.Is(err, argfence.ErrLeadingDash) {
		t.Errorf("argfence.RejectLeadingDash no longer wraps ErrLeadingDash: %v", err)
	}
}

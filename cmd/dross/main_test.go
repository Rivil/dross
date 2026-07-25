package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// readmeCmdRef matches a `dross <word>` reference inside a backtick code
// span in the README — the command the docs advertise. We check the first
// word after `dross ` against the real cobra tree.
var readmeCmdRef = regexp.MustCompile("`dross ([a-z][a-z-]*)")

// TestReadmeAdvertisesOnlyRealCommands is the README truth-pass guard
// (readme-truth-pass c-1): every top-level `dross <cmd>` the README
// advertises must exist in the assembled command tree. Catches the
// over-claim failure mode — a renamed or removed command still documented
// — which is the lie a first-time reader would hit. (Under-claiming, i.e.
// an internal command the table omits, is allowed and not checked.)
func TestReadmeAdvertisesOnlyRealCommands(t *testing.T) {
	real := map[string]bool{}
	for _, c := range newRoot().Commands() {
		real[c.Name()] = true
	}

	b, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}

	// `dross <word>` forms that aren't real top-level commands but appear
	// in prose as sub-verbs or placeholders — not command claims.
	ignore := map[string]bool{"phase-id": true}

	seen := map[string]bool{}
	for _, m := range readmeCmdRef.FindAllStringSubmatch(string(b), -1) {
		name := m[1]
		if ignore[name] || seen[name] {
			continue
		}
		seen[name] = true
		if !real[name] {
			t.Errorf("README advertises `dross %s` but no such command exists in the cobra tree", name)
		}
	}
	if len(seen) == 0 {
		t.Fatal("parsed zero `dross <cmd>` references from README — the regex or path is wrong")
	}
}

// TestReadmeStatusNotStale pins the version framing: once the repo is at
// v1.0 the status line must not still advertise a v0.x milestone series.
func TestReadmeStatusNotStale(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "**Status:** v0.10") {
		t.Error("README status line still says v0.10.x — stale after the v1.0 milestone")
	}
}

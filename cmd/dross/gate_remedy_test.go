package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// gateRemedyRefs lifts every `dross <verb> [<sub>]` out of internal/gate's
// string literals — the refusals and hints Claude reads in place of a call's
// result. TestNarratedCommandsResolveAgainstTheTree scans internal/cmd only.
func gateRemedyRefs(t *testing.T) []narratedRef {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "internal", "gate", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	var refs []narratedRef
	for _, f := range files {
		if !strings.HasSuffix(f, "_test.go") {
			refs = append(refs, narratedCmdRefs(t, f)...)
		}
	}
	return refs
}

// TestGateRemediesResolve: a refusal that tells Claude — or the human — to run
// a command that no longer exists sends them hunting for a way around the gate
// instead. Every command a gate's remedy or hint names must resolve against the
// assembled tree, and renaming one of them must turn this red.
func TestGateRemediesResolve(t *testing.T) {
	refs := gateRemedyRefs(t)
	seen := map[string]bool{}
	for _, r := range refs {
		seen[r.String()] = true
	}
	for _, want := range []string{"dross gate off", "dross task edit"} {
		if !seen[want] {
			t.Fatalf("no gate remedy names `%s` — the literal walk or the regex is wrong (found %d refs)", want, len(refs))
		}
	}
	msgs, _ := unresolvedNarrations(topLevelIndex(newRoot()), refs)
	for _, m := range msgs {
		t.Error(m)
	}

	// The guard bites: the same refs against a tree with `gate off` or
	// `task edit` renamed each fail, naming the dead command.
	for _, path := range [][2]string{{"gate", "off"}, {"task", "edit"}} {
		root := newRoot()
		sub, _, err := root.Find(path[:])
		if err != nil || sub == root {
			t.Fatalf("no command %v in the live tree: %v", path, err)
		}
		sub.Use = "renamed" + strings.TrimPrefix(sub.Use, sub.Name())
		msgs, _ := unresolvedNarrations(topLevelIndex(root), refs)
		dead := "dross " + path[0] + " " + path[1]
		if !strings.Contains(strings.Join(msgs, "\n"), dead) {
			t.Errorf("renaming %v left the remedies resolving: %v", path, msgs)
		}
	}
}

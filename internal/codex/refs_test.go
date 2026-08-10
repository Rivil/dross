package codex

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFindCallersStripsMethodReceivers covers the receiver-stripping branch:
// a symbol defined as "Client.Fetch" is called as `obj.Fetch(...)`, so the scan
// has to look for the bare name. Left un-stripped, every method in the codebase
// would report zero callers — the index would look complete and be silently
// useless for exactly the symbols most worth tracing.
func TestFindCallersStripsMethodReceivers(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "client.ts")
	caller := filepath.Join(root, "consumer.ts")

	if err := os.WriteFile(target, []byte("class Client { Fetch() {} }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(caller, []byte("const c = new Client();\nc.Fetch();\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := findCallers([]string{target}, []Symbol{{Name: "Client.Fetch", File: target, Line: 1}})

	found := false
	for _, s := range got {
		if filepath.Base(s.File) == "consumer.ts" {
			found = true
		}
	}
	if !found {
		t.Errorf("the `obj.Fetch()` call site was not found for a symbol defined as Client.Fetch: %+v", got)
	}

	// The defining file is never reported as its own caller.
	for _, s := range got {
		if filepath.Base(s.File) == "client.ts" {
			t.Errorf("the defining file was reported as a caller: %+v", s)
		}
	}
}

// TestFindCallersSkipsShortNames pins the other arm of the same loop: names
// under three characters are ref-spam magnets and are skipped, so the stripping
// branch is genuinely conditional rather than always taken.
func TestFindCallersSkipsShortNames(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "a.ts")
	caller := filepath.Join(root, "b.ts")
	if err := os.WriteFile(target, []byte("function at() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(caller, []byte("at();\nat();\nat();\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := findCallers([]string{target}, []Symbol{{Name: "X.at", File: target, Line: 1}}); len(got) != 0 {
		t.Errorf("a two-character name must be skipped as ref-spam, got %+v", got)
	}
}

// TestFindCallersEmptyDefinedSetIsNil is the early return: nothing defined
// means nothing to scan for, and scanning anyway would walk the tree for no
// reason.
func TestFindCallersEmptyDefinedSetIsNil(t *testing.T) {
	if got := findCallers([]string{"a.ts"}, nil); got != nil {
		t.Errorf("findCallers with no defined symbols = %+v, want nil", got)
	}
}

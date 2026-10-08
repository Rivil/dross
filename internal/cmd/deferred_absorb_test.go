package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/phase"
)

// absorbFixture parks four items in phase src — aaa → tgt, an id-less legacy
// item → tgt, one → other, one unrouted — and gives tgt two criteria. tgt is
// the current phase.
func absorbFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustRunSet(t, "project.name", "test-app")
	mustRunSet(t, "runtime.mode", "native")
	phases := filepath.Join(dir, ".dross", "phases")
	mustWrite(t, filepath.Join(phases, "other", ".keep"), "")
	mustWrite(t, filepath.Join(phases, "src", "spec.toml"), `[phase]
id = "src"
title = "Src"

[[criteria]]
id = "c-1"
text = "x"

[[deferred]]
id = "aaa"
text = "routed to tgt"
target = "tgt"

[[deferred]]
text = "legacy, no id yet"
target = "tgt"

[[deferred]]
id = "ccc"
text = "routed elsewhere"
target = "other"

[[deferred]]
id = "ddd"
text = "someday"

[[deferred]]
id = "eee"
text = "dismissed, yet routed"
target = "tgt"
dismissed = true
`)
	mustWrite(t, filepath.Join(phases, "tgt", "spec.toml"), `[phase]
id = "tgt"
title = "Tgt"

[[criteria]]
id = "c-1"
text = "takes aaa"

[[criteria]]
id = "c-2"
text = "takes the legacy item"
`)
	if err := runCmd(t, State(), "set", "current_phase", "tgt"); err != nil {
		t.Fatal(err)
	}
	return dir
}

func tgtSpec(t *testing.T, dir string) *phase.Spec {
	t.Helper()
	s, err := phase.LoadSpec(filepath.Join(dir, ".dross", "phases", "tgt", "spec.toml"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestDeferredAbsorbRecordsTheID: the criterion gains the item's stable id —
// never its <source> <idx> handle — validate then passes, the id is never
// printed, and a second identical run writes nothing.
func TestDeferredAbsorbRecordsTheID(t *testing.T) {
	dir := absorbFixture(t)
	var err error
	out := captureStdout(t, func() { err = runCmd(t, Deferred(), "absorb", "src", "0", "--criterion", "c-1") })
	if err != nil {
		t.Fatal(err)
	}
	if got := tgtSpec(t, dir).Criteria[0].Deferred; len(got) != 1 || got[0] != "aaa" {
		t.Fatalf("c-1 deferred = %v, want [aaa]", got)
	}
	if strings.Contains(out, "aaa") {
		t.Errorf("absorb printed the internal id: %q", out)
	}
	if err := runCmd(t, Validate()); err != nil {
		t.Fatalf("validate after absorb: %v", err)
	}

	tgtPath := filepath.Join(dir, ".dross", "phases", "tgt", "spec.toml")
	past := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(tgtPath, past, past); err != nil {
		t.Fatal(err)
	}
	before := drossTreeHash(t, dir)
	if err := runCmd(t, Deferred(), "absorb", "src", "0", "--criterion", "c-1"); err != nil {
		t.Fatal(err)
	}
	if drossTreeHash(t, dir) != before {
		t.Error("a second identical absorb changed a file")
	}
	if st, _ := os.Stat(tgtPath); !st.ModTime().Equal(past) {
		t.Error("a second identical absorb rewrote tgt's spec")
	}
}

// TestDeferredAbsorbStampsLegacyID: an item filed before ids existed gets one
// stamped in its own spec, and that same id lands on the criterion.
func TestDeferredAbsorbStampsLegacyID(t *testing.T) {
	dir := absorbFixture(t)
	if err := runCmd(t, Deferred(), "absorb", "src", "1", "--criterion", "c-2"); err != nil {
		t.Fatal(err)
	}
	src, err := phase.LoadSpec(filepath.Join(dir, ".dross", "phases", "src", "spec.toml"))
	if err != nil {
		t.Fatal(err)
	}
	id := src.Deferred[1].ID
	if id == "" {
		t.Fatal("the legacy item was not stamped with an id")
	}
	if got := tgtSpec(t, dir).Criteria[1].Deferred; len(got) != 1 || got[0] != id {
		t.Errorf("c-2 deferred = %v, want [%s]", got, id)
	}
	if err := runCmd(t, Validate()); err != nil {
		t.Fatalf("validate after absorbing a legacy item: %v", err)
	}
}

// TestDeferredAbsorbRefusals: each wrong handle is refused by name, and
// nothing under .dross changes.
func TestDeferredAbsorbRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"routed elsewhere", []string{"src", "2", "--criterion", "c-1"}, "routed to other"},
		{"unrouted", []string{"src", "3", "--criterion", "c-1"}, "unrouted"},
		{"dismissed", []string{"src", "4", "--criterion", "c-1"}, "dismissed"},
		{"legacy item, unknown criterion", []string{"src", "1", "--criterion", "c-9"}, "c-9"},
		{"unknown criterion", []string{"src", "0", "--criterion", "c-9"}, "c-9"},
		{"idx out of range", []string{"src", "9", "--criterion", "c-1"}, "9"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := absorbFixture(t)
			before := drossTreeHash(t, dir)
			err := runCmd(t, Deferred(), append([]string{"absorb"}, tc.args...)...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want a refusal naming %q", err, tc.want)
			}
			if drossTreeHash(t, dir) != before {
				t.Error("a refused absorb changed a file")
			}
		})
	}
}

// TestDeferredAbsorbExplicitPhase: --phase names the absorbing phase whatever
// current_phase says — /dross-spec runs absorb before §6 sets it.
func TestDeferredAbsorbExplicitPhase(t *testing.T) {
	dir := absorbFixture(t)
	if err := runCmd(t, State(), "set", "current_phase", "other"); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Deferred(), "absorb", "src", "0", "--criterion", "c-1"); err == nil {
		t.Fatal("absorb defaulted to current_phase other and accepted an item routed to tgt")
	}
	if err := runCmd(t, Deferred(), "absorb", "src", "0", "--criterion", "c-1", "--phase", "tgt"); err != nil {
		t.Fatalf("absorb --phase tgt: %v", err)
	}
	if got := tgtSpec(t, dir).Criteria[0].Deferred; len(got) != 1 || got[0] != "aaa" {
		t.Errorf("c-1 deferred = %v, want [aaa]", got)
	}
}

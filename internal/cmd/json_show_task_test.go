package cmd

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// Own test file rather than sharing json_show_test.go with t-7: task/changes
// and phase/stats are peers in the same wave, and two tasks writing one file
// cannot run in parallel.

// jsonTaskPlan populates every field taskShow's aligned rendering can print, so
// the field-by-field comparison below has something to compare on every row.
// t-2 is deliberately left without a status — that is the orPending case.
const jsonTaskPlan = `
[phase]
id = "01-auth"

[[task]]
id = "t-1"
wave = 2
title = "schema"
files = ["db/schema.sql", "db/migrate.go"]
covers = ["c-1", "c-2"]
depends_on = ["t-0"]
test_contract = ["a migration applies cleanly", "a rollback restores the prior shape"]
description = """
Two lines of description
so the multi-line path is exercised.
"""
status = "done"

[[task]]
id = "t-2"
wave = 2
title = "handler"
files = ["http/handler.go"]
`

func writeJSONTaskFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "01-auth", "spec.toml"),
		"[phase]\nid = \"01-auth\"\ntitle = \"Auth\"\n")
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "01-auth", "plan.toml"), jsonTaskPlan)
	trustFixture(t)
	return dir
}

// TestTaskShowJSONCarriesEveryRenderedField proves c-5 for `task show`: every
// field the aligned rendering prints is in the payload, under its toml name.
func TestTaskShowJSONCarriesEveryRenderedField(t *testing.T) {
	writeJSONTaskFixture(t)

	out := captureStdout(t, func() {
		if err := runCmd(t, Task(), "show", "01-auth", "t-1", "--json"); err != nil {
			t.Fatalf("task show --json: %v", err)
		}
	})
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("payload does not parse: %v\n%s", err, out)
	}

	// Every row the text rendering prints must have a payload key. Listed
	// explicitly rather than derived, so dropping a field from both at once
	// still fails here.
	for _, key := range []string{"id", "title", "wave", "status", "files", "covers", "depends_on", "test_contract", "description"} {
		if _, ok := got[key]; !ok {
			t.Errorf("payload has no %q key, but the aligned rendering prints that row:\n%s", key, out)
		}
	}

	if got["id"] != "t-1" || got["title"] != "schema" || got["status"] != "done" {
		t.Errorf("scalar fields wrong: id=%v title=%v status=%v", got["id"], got["title"], got["status"])
	}
	if w, _ := got["wave"].(float64); int(w) != 2 {
		t.Errorf("wave = %v, want 2", got["wave"])
	}
	for _, tc := range []struct {
		key  string
		want []string
	}{
		{"files", []string{"db/schema.sql", "db/migrate.go"}},
		{"covers", []string{"c-1", "c-2"}},
		{"depends_on", []string{"t-0"}},
		{"test_contract", []string{"a migration applies cleanly", "a rollback restores the prior shape"}},
	} {
		raw, _ := got[tc.key].([]any)
		if len(raw) != len(tc.want) {
			t.Errorf("%s: got %v, want %v", tc.key, raw, tc.want)
			continue
		}
		for i, v := range raw {
			if v != tc.want[i] {
				t.Errorf("%s[%d] = %v, want %s", tc.key, i, v, tc.want[i])
			}
		}
	}
	if d, _ := got["description"].(string); !strings.Contains(d, "multi-line path") {
		t.Errorf("description = %q, want the fixture text", d)
	}

	// The text rendering must still print the same rows — --json is an
	// addition, not a replacement.
	text := captureStdout(t, func() {
		if err := runCmd(t, Task(), "show", "01-auth", "t-1"); err != nil {
			t.Fatalf("task show: %v", err)
		}
	})
	for _, row := range []string{"id:", "title:", "wave:", "status:", "files:", "covers:", "depends_on:", "test_contract:", "description:"} {
		if !strings.Contains(text, row) {
			t.Errorf("aligned rendering lost the %q row:\n%s", row, text)
		}
	}
}

// TestTaskShowJSONNormalizesEmptyStatus is the omitempty trap.
//
// phase.Task.Status is toml:"status,omitempty", so the mirrored json tag is
// omitempty too — a bare marshal of a task with no status drops the key
// entirely, while the text path prints "pending" via orPending. The two
// renderings have to agree, so --json marshals a copy with Status normalized.
func TestTaskShowJSONNormalizesEmptyStatus(t *testing.T) {
	writeJSONTaskFixture(t)

	out := captureStdout(t, func() {
		if err := runCmd(t, Task(), "show", "01-auth", "t-2", "--json"); err != nil {
			t.Fatalf("task show --json: %v", err)
		}
	})
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("payload does not parse: %v\n%s", err, out)
	}
	status, present := got["status"]
	if !present {
		t.Fatalf(`"status" is absent for a task with no status — omitempty dropped it, but the text path prints "pending":\n%s`, out)
	}
	if status != "pending" {
		t.Errorf(`status = %v, want "pending"`, status)
	}

	text := captureStdout(t, func() {
		if err := runCmd(t, Task(), "show", "01-auth", "t-2"); err != nil {
			t.Fatalf("task show: %v", err)
		}
	})
	if !strings.Contains(text, "status:       pending") {
		t.Errorf("the text rendering disagrees:\n%s", text)
	}
}

// TestTaskShowJSONUnknownTaskStillErrors pins that --json does not turn a
// missing task into a document.
func TestTaskShowJSONUnknownTaskStillErrors(t *testing.T) {
	writeJSONTaskFixture(t)

	var err error
	out := captureStdout(t, func() {
		err = runCmd(t, Task(), "show", "01-auth", "t-99", "--json")
	})
	if err == nil {
		t.Fatal("task show t-99 --json must fail")
	}
	if !strings.Contains(err.Error(), "task not found: t-99") {
		t.Errorf("error %q lost the existing wording", err)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("a failed show printed a document anyway:\n%s", out)
	}
}

// TestChangesShowJSONIsAcceptedAndIdentical pins the symmetry flag: `changes
// show` already emitted JSON (the record is a .json file, not a toml
// document), so --json changes nothing about the output and exists only so a
// caller can pass it uniformly across every `show` — the same bargain `state
// show` already makes.
func TestChangesShowJSONIsAcceptedAndIdentical(t *testing.T) {
	writeJSONTaskFixture(t)

	if err := runCmd(t, Changes(), "record", "01-auth", "t-1",
		"--files", "db/schema.sql", "--commit", "abc1234"); err != nil {
		t.Fatalf("changes record: %v", err)
	}

	plain := captureStdout(t, func() {
		if err := runCmd(t, Changes(), "show", "01-auth"); err != nil {
			t.Fatalf("changes show: %v", err)
		}
	})
	withFlag := captureStdout(t, func() {
		if err := runCmd(t, Changes(), "show", "01-auth", "--json"); err != nil {
			t.Fatalf("changes show --json: %v", err)
		}
	})
	if plain != withFlag {
		t.Errorf("--json changed the output:\nplain:\n%s\n--json:\n%s", plain, withFlag)
	}
	if !json.Valid([]byte(plain)) {
		t.Fatalf("changes show output is not valid JSON:\n%s", plain)
	}
	if !strings.Contains(plain, "abc1234") {
		t.Errorf("recorded commit missing from the output:\n%s", plain)
	}
}

// TestChangesShowJSONOnEmptyRecord pins that a phase with no changes.json
// yet emits the empty record rather than erroring — the behaviour the plain
// rendering already had, which --json must not change.
func TestChangesShowJSONOnEmptyRecord(t *testing.T) {
	writeJSONTaskFixture(t)

	out := captureStdout(t, func() {
		if err := runCmd(t, Changes(), "show", "01-auth", "--json"); err != nil {
			t.Fatalf("changes show --json with no changes.json: %v", err)
		}
	})
	if !json.Valid([]byte(out)) {
		t.Fatalf("output is not valid JSON:\n%s", out)
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(out), &rec); err != nil {
		t.Fatalf("payload does not parse: %v\n%s", err, out)
	}
	if len(rec) == 0 {
		t.Errorf("empty record carries no keys at all — it should still identify the phase:\n%s", out)
	}
}

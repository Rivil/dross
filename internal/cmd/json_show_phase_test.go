package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/Rivil/dross/internal/telemetry"
)

// Own test file rather than sharing json_show_test.go with t-8: phase/stats and
// task/changes are peers in the same wave, and two tasks writing one file
// cannot run in parallel.

const jsonPhaseSpec = `
[phase]
id = "01-auth"
title = "Auth middleware"
milestone = "v1.1"

[[criteria]]
id = "c-1"
text = "login works"

[[decisions]]
key = "session_store"
choice = "cookie"
why = "no server state"
locked = true

[[deferred]]
text = "oauth"
why = "not this phase"
`

const jsonPhasePlan = `
task_seq = 2

[phase]
id = "01-auth"

[[task]]
id = "t-1"
wave = 1
title = "schema"
files = ["db/schema.sql"]
covers = ["c-1"]
test_contract = ["a migration applies cleanly"]
status = "done"
`

// writeJSONPhaseFixture scaffolds a repo with one fully-populated phase and
// returns the repo dir.
func writeJSONPhaseFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "01-auth", "spec.toml"), jsonPhaseSpec)
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "01-auth", "plan.toml"), jsonPhasePlan)
	return dir
}

// TestPhaseShowJSONShape proves c-5's phase half: exactly the two document
// keys, and a missing plan is null rather than absent or empty.
func TestPhaseShowJSONShape(t *testing.T) {
	dir := writeJSONPhaseFixture(t)

	out := captureStdout(t, func() {
		if err := runCmd(t, Phase(), "show", "01-auth", "--json"); err != nil {
			t.Fatalf("phase show --json: %v", err)
		}
	})
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("payload does not parse: %v\n%s", err, out)
	}
	if len(doc) != 2 || doc["spec"] == nil || doc["plan"] == nil {
		keys := make([]string, 0, len(doc))
		for k := range doc {
			keys = append(keys, k)
		}
		t.Fatalf("payload keys = %v, want exactly [spec plan]", keys)
	}
	if string(doc["plan"]) == "null" {
		t.Error("plan is null but plan.toml exists")
	}

	// Delete the plan: "plan" must still be a key, carrying null. Absent would
	// make a consumer's lookup ambiguous with a typo, and {} would claim an
	// empty plan exists.
	if err := os.Remove(filepath.Join(dir, ".dross", "phases", "01-auth", "plan.toml")); err != nil {
		t.Fatal(err)
	}
	out = captureStdout(t, func() {
		if err := runCmd(t, Phase(), "show", "01-auth", "--json"); err != nil {
			t.Fatalf("phase show --json (no plan): %v", err)
		}
	})
	doc = map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("payload does not parse: %v\n%s", err, out)
	}
	raw, present := doc["plan"]
	if !present {
		t.Error(`"plan" key is absent with plan.toml deleted — it must be present and null`)
	}
	if got := string(raw); got != "null" {
		t.Errorf(`"plan" = %s, want null`, got)
	}
	if string(doc["spec"]) == "null" {
		t.Error("spec went null too — a missing plan must not take the spec with it")
	}
}

// TestPhaseShowUnknownIDFailsInBothRenderings pins the behaviour change: an
// unknown phase id used to exit 0 after printing two "(missing)" lines, which
// reads as "this phase has no spec yet" rather than "there is no such phase".
func TestPhaseShowUnknownIDFailsInBothRenderings(t *testing.T) {
	writeJSONPhaseFixture(t)

	for _, args := range [][]string{
		{"show", "no-such-phase"},
		{"show", "no-such-phase", "--json"},
	} {
		var err error
		out := captureStdout(t, func() { err = runCmd(t, Phase(), args...) })
		label := strings.Join(args, " ")
		if err == nil {
			t.Errorf("phase %s exited 0 for an unknown id; out:\n%s", label, out)
		}
		if err != nil && !strings.Contains(err.Error(), "no-such-phase") {
			t.Errorf("phase %s: error %q does not name the unknown id", label, err)
		}
		if strings.Contains(out, "(missing)") {
			t.Errorf("phase %s printed a (missing) line instead of failing:\n%s", label, out)
		}
		if strings.Contains(out, "{") {
			t.Errorf("phase %s printed a JSON document for an unknown id:\n%s", label, out)
		}
	}
}

// TestPhaseShowJSONRoundTripsTheDocuments is the c-5 completeness gate for the
// one show whose two renderings read different sources.
//
// The default rendering os.ReadFiles the raw TOML — it displays everything on
// disk — while --json goes through phase.LoadSpec / LoadPlan and can only
// display what the structs model. Any top-level key the schema does not carry
// is therefore visible in one rendering and missing from the other, which is a
// direct c-5 violation. This fails the day such a key is added.
func TestPhaseShowJSONRoundTripsTheDocuments(t *testing.T) {
	writeJSONPhaseFixture(t)

	out := captureStdout(t, func() {
		if err := runCmd(t, Phase(), "show", "01-auth", "--json"); err != nil {
			t.Fatalf("phase show --json: %v", err)
		}
	})
	var payload struct {
		Spec map[string]any `json:"spec"`
		Plan map[string]any `json:"plan"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("payload does not parse: %v\n%s", err, out)
	}

	for _, tc := range []struct {
		name   string
		source string
		got    map[string]any
	}{
		{"spec.toml", jsonPhaseSpec, payload.Spec},
		{"plan.toml", jsonPhasePlan, payload.Plan},
	} {
		var onDisk map[string]any
		if _, err := toml.Decode(tc.source, &onDisk); err != nil {
			t.Fatalf("decode %s fixture: %v", tc.name, err)
		}
		for key := range onDisk {
			if _, ok := tc.got[key]; !ok {
				t.Errorf("%s carries top-level key %q that --json drops — the toml rendering shows it and the payload does not (add it to the struct, or --json is lossy)", tc.name, key)
			}
		}
	}
}

// --- stats show --json ---

// TestStatsShowJSONCoversEveryRenderedSection proves c-5 for stats: the payload
// carries the data of all five sections statsShow renders — header, commands,
// errors, overrides, outcomes — compared against the parsed table output.
//
// Section-by-section rather than spot-checked, because statsSummary could drop
// force flags or outcomes entirely and still satisfy a top-commands assertion.
func TestStatsShowJSONCoversEveryRenderedSection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".claude", "dross", "telemetry.jsonl")

	for _, ev := range []telemetry.Event{
		{Kind: "cli", Command: "init", DurationMS: 10, ExitCode: 0},
		{Kind: "cli", Command: "verify", DurationMS: 50, ExitCode: 0},
		{Kind: "cli", Command: "verify", DurationMS: 60, ExitCode: 1, ErrorClass: "missing"},
		{Kind: "cli", Command: "ship", DurationMS: 5, ExitCode: 1, ErrorClass: "other", ErrorDetail: "some unclassified shape"},
		{Kind: "outcome", Command: "verify", Phase: "01-auth", Tags: map[string]string{"verdict": "pass", "force": "true"}, Numbers: map[string]float64{"mutation_score": 0.9}},
		{Kind: "outcome", Command: "ship", Tags: map[string]string{"result": "opened"}},
		{Kind: "outcome", Command: "doctor", Tags: map[string]string{"result": "issues_found"}, Counts: map[string]int{"issues": 3}},
	} {
		if err := telemetry.Append(path, ev); err != nil {
			t.Fatal(err)
		}
	}

	table := captureStdout(t, func() {
		if err := runCmd(t, Stats(), "show"); err != nil {
			t.Fatalf("stats show: %v", err)
		}
	})
	jsonOut := captureStdout(t, func() {
		if err := runCmd(t, Stats(), "show", "--json"); err != nil {
			t.Fatalf("stats show --json: %v", err)
		}
	})

	var got statsSummary
	if err := json.Unmarshal([]byte(jsonOut), &got); err != nil {
		t.Fatalf("payload does not parse: %v\n%s", err, jsonOut)
	}
	if strings.Contains(jsonOut, "\n#") || strings.HasPrefix(jsonOut, "#") {
		t.Errorf("payload carries a `#` header line:\n%s", jsonOut)
	}

	// 1. header
	if got.Events != 7 {
		t.Errorf("events = %d, want 7", got.Events)
	}
	if !strings.Contains(table, "events:  7") {
		t.Errorf("table header disagrees:\n%s", table)
	}
	if got.Span == nil {
		t.Error("span is null but there are events — the header renders a span")
	}

	// 2. commands — every row the table prints must be in the payload with the
	// same numbers.
	rowRE := regexp.MustCompile(`(?m)^  (\S+)\s+(\d+)\s+(\d+)\s+(\d+)$`)
	byCmd := map[string]statsCommand{}
	for _, c := range got.Commands {
		byCmd[c.Command] = c
	}
	rows := 0
	for _, m := range rowRE.FindAllStringSubmatch(table, -1) {
		cmd := m[1]
		c, ok := byCmd[cmd]
		if !ok {
			t.Errorf("table renders command %q, payload has no such entry", cmd)
			continue
		}
		rows++
		calls, _ := strconv.Atoi(m[2])
		errs, _ := strconv.Atoi(m[3])
		if c.Calls != calls || c.Errors != errs {
			t.Errorf("%s: payload calls=%d errors=%d, table calls=%d errors=%d", cmd, c.Calls, c.Errors, calls, errs)
		}
	}
	if rows == 0 {
		t.Fatal("parsed no command rows out of the table — the comparison above proved nothing")
	}

	// 3. errors — every class in the table, plus the `other` tail detail.
	if got.Errors.Total != 2 {
		t.Errorf("errors.total = %d, want 2", got.Errors.Total)
	}
	for _, class := range []string{"missing", "other"} {
		found := false
		for _, c := range got.Errors.Classes {
			if c.Class == class {
				found = true
			}
		}
		if !found {
			t.Errorf("payload has no %q error class, but the table renders it:\n%s", class, table)
		}
		if !strings.Contains(table, class) {
			t.Errorf("table lost the %q class:\n%s", class, table)
		}
	}
	if len(got.Errors.OtherTail) == 0 || got.Errors.OtherTail[0].Detail != "some unclassified shape" {
		t.Errorf("payload dropped the `other` tail shapes: %+v", got.Errors.OtherTail)
	}
	if !strings.Contains(table, "some unclassified shape") {
		t.Errorf("table lost the other-tail shape:\n%s", table)
	}

	// 4. overrides
	if got.ForceFlags != 1 {
		t.Errorf("force_flag_invocations = %d, want 1", got.ForceFlags)
	}
	if !strings.Contains(table, "force-flag invocations: 1") {
		t.Errorf("table disagrees on force flags:\n%s", table)
	}

	// 5. outcomes
	if got.Outcomes.VerifyVerdicts["pass"] != 1 {
		t.Errorf("verify_verdicts.pass = %d, want 1", got.Outcomes.VerifyVerdicts["pass"])
	}
	if got.Outcomes.ShipResults["opened"] != 1 {
		t.Errorf("ship_results.opened = %d, want 1", got.Outcomes.ShipResults["opened"])
	}
	if got.Outcomes.DoctorResults["issues_found"] != 1 || got.Outcomes.DoctorIssues != 3 {
		t.Errorf("doctor outcomes = %+v / issues %d, want issues_found=1 / 3", got.Outcomes.DoctorResults, got.Outcomes.DoctorIssues)
	}
	if got.Outcomes.MutationScoreN != 1 {
		t.Errorf("mutation_score_n = %d, want 1", got.Outcomes.MutationScoreN)
	}
	for _, want := range []string{"pass=1", "opened=1", "issues_found=1", "mutation score"} {
		if !strings.Contains(table, want) {
			t.Errorf("table lost %q:\n%s", want, table)
		}
	}
}

// TestStatsShowJSONHonoursSince pins that --since filters the payload, not just
// the table — a consumer reading JSON must get the same window.
func TestStatsShowJSONHonoursSince(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".claude", "dross", "telemetry.jsonl")

	now := time.Now()
	for _, ev := range []telemetry.Event{
		{Kind: "cli", Command: "ancient", Timestamp: now.Add(-30 * 24 * time.Hour)},
		{Kind: "cli", Command: "recent", Timestamp: now.Add(-1 * time.Hour)},
	} {
		if err := telemetry.Append(path, ev); err != nil {
			t.Fatal(err)
		}
	}

	out := captureStdout(t, func() {
		if err := runCmd(t, Stats(), "show", "--since", "7d", "--json"); err != nil {
			t.Fatalf("stats show --since 7d --json: %v", err)
		}
	})
	var got statsSummary
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("payload does not parse: %v\n%s", err, out)
	}
	if got.Events != 1 {
		t.Errorf("events = %d, want 1 — --since must filter the payload too", got.Events)
	}
	for _, c := range got.Commands {
		if c.Command == "ancient" {
			t.Error("a 30-day-old event survived --since 7d in the payload")
		}
	}
}

// TestStatsShowJSONOnEmptyLogEmitsADocument pins that a consumer asking for
// JSON always gets JSON. The table's "(no telemetry events …)" sentence is
// prose, and a caller that parses stdout would choke on it.
func TestStatsShowJSONOnEmptyLogEmitsADocument(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	out := captureStdout(t, func() {
		if err := runCmd(t, Stats(), "show", "--json"); err != nil {
			t.Fatalf("stats show --json on an empty log: %v", err)
		}
	})
	if strings.Contains(out, "no telemetry events") {
		t.Errorf("--json emitted the prose sentence:\n%s", out)
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("output is not valid JSON:\n%s", out)
	}
	var got statsSummary
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("payload does not parse: %v\n%s", err, out)
	}
	if got.Events != 0 {
		t.Errorf("events = %d, want 0", got.Events)
	}
	if got.Span != nil {
		t.Errorf("span = %+v, want null on an empty log", got.Span)
	}
	// Empty sections are [] / {}, never null — a consumer should be able to
	// range over them without a nil check.
	if !strings.Contains(out, `"commands": []`) {
		t.Errorf("empty commands is not an empty array:\n%s", out)
	}
}

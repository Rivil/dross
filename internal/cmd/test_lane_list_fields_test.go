package cmd

// `dross test lane list` and the selector template.
//
// The template and the join shape a lane's derived line exactly as the selector
// style does, and until this file they were visible only at the moment the lane
// was declared. A user reading a listing back could see `selector: dir` and
// reconstruct a line the lane would never run — which is the gap c-4 closes.

import (
	"strings"
	"testing"
)

// The two template shapes, declared as two lanes rather than one because
// project.toml refuses a join with no {paths} to collapse: a template repeating
// per path and a template naming the whole set are different fields, and no
// single valid lane carries the pair.
//
// cargoLaneArgs is the repeat shape — `--package {path}` once per package, each
// its own argument. ctestLaneArgs is the joined shape — one `-R` regex
// alternation, which is the only thing a join exists for.
var cargoLaneArgs = []string{
	"cargo", "--match", "src/**", "--command", "cargo test",
	"--selector", "dir",
	"--selector-template", "--package {path}",
}

var ctestLaneArgs = []string{
	"ctest", "--match", "tests/**", "--command", "ctest",
	"--selector", "path",
	"--selector-template", "-R {paths}",
	"--selector-join", "|",
}

// TestLaneListPrintsTemplateFields is c-4: every field that shapes the derived
// line is readable from the listing.
//
// Both fields are asserted, because either one alone misdescribes the other: a
// template with no join renders its paths as separate arguments, and a join
// with no template names a separator for something that is never joined.
func TestLaneListPrintsTemplateFields(t *testing.T) {
	laneFixture(t)
	mustAddLane(t, cargoLaneArgs...)
	mustAddLane(t, ctestLaneArgs...)

	var out string
	if err := runCmdCapturing(t, &out, Test(), "lane", "list"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "selector-template: --package {path}") {
		t.Errorf("the listing does not print the repeat template:\n%s", out)
	}
	if !strings.Contains(out, "selector-template: -R {paths}") {
		t.Errorf("the listing does not print the joined template:\n%s", out)
	}
	if !strings.Contains(out, "selector-join: |") {
		t.Errorf("the listing does not print the selector join:\n%s", out)
	}
}

// TestListOmitsTemplateFieldsForALaneWithout asserts ABSENCE, which is what
// keeps the fields opt-in.
//
// A `selector-template: -` on every lane written before templates existed reads
// as a field the user is expected to go and set, and the listing's whole job is
// to describe what a lane IS rather than what it could be.
func TestListOmitsTemplateFieldsForALaneWithout(t *testing.T) {
	laneFixture(t)
	mustAddLane(t, "go", "--match", "internal/**", "--command", "go test", "--selector", "go-package")

	var out string
	if err := runCmdCapturing(t, &out, Test(), "lane", "list"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "selector: go-package") {
		t.Fatalf("the fixture's own selector is missing, so absence proves nothing:\n%s", out)
	}
	if strings.Contains(out, "selector-template") {
		t.Errorf("a lane declaring no template listed one:\n%s", out)
	}
	if strings.Contains(out, "selector-join") {
		t.Errorf("a lane declaring no join listed one:\n%s", out)
	}
}

// templateLines pulls the template lines out of a transcript, trimmed, in the
// order they appeared.
func templateLines(out string) []string {
	var got []string
	for _, line := range strings.Split(out, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "selector-template:") || strings.HasPrefix(t, "selector-join:") {
			got = append(got, t)
		}
	}
	return got
}

// TestListAndAddRenderTemplatesIdentically compares the two transcripts against
// EACH OTHER rather than each against a literal.
//
// A list-local renderer would satisfy two separate literal assertions and still
// be a second answer to "what does this lane's template look like" — and the
// one a user reads back from the listing is the one nothing else validates.
// Identity is the only assertion that fails on a copy.
func TestListAndAddRenderTemplatesIdentically(t *testing.T) {
	laneFixture(t)

	var added string
	if err := runCmdCapturing(t, &added, Test(), append([]string{"lane", "add"}, ctestLaneArgs...)...); err != nil {
		t.Fatalf("lane add: %v", err)
	}
	var listed string
	if err := runCmdCapturing(t, &listed, Test(), "lane", "list"); err != nil {
		t.Fatal(err)
	}

	fromAdd := templateLines(added)
	fromList := templateLines(listed)
	if len(fromAdd) != 2 {
		t.Fatalf("lane add echoed %d template line(s), want 2:\n%s", len(fromAdd), added)
	}
	if strings.Join(fromAdd, "\n") != strings.Join(fromList, "\n") {
		t.Errorf("lane list renders the template differently from lane add:\n add: %q\nlist: %q", fromAdd, fromList)
	}
}

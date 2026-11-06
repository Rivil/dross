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

// vouchListFixture declares one lane that vouches and one that does not.
func vouchListFixture(t *testing.T) {
	t.Helper()
	vouchFixture(t)
	mustAddLane(t, "web", "--match", "src/**", "--command", "npx vitest run", "--selector", "path")
	mustAddLane(t, "docs", "--match", "docs/", "--command", "markdownlint docs")
	mustEditLane(t, "web", "--vouch", "--relate", testRelate)
}

// TestLaneListPrintsVouchOnlyWhereSet: a `vouch: false` on every other lane
// would read as a field each is expected to go and set.
func TestLaneListPrintsVouchOnlyWhereSet(t *testing.T) {
	vouchListFixture(t)

	var out string
	if err := runCmdCapturing(t, &out, Test(), "lane", "list"); err != nil {
		t.Fatal(err)
	}
	if web := laneBlock(t, out, "web"); !strings.Contains(web, "  vouch: true") {
		t.Errorf("the vouching lane does not list vouch:\n%s", web)
	}
	if docs := laneBlock(t, out, "docs"); strings.Contains(docs, "vouch") {
		t.Errorf("a lane that does not vouch listed it:\n%s", docs)
	}
}

// TestLaneEditRelateRoundTripsThroughList: relate decides what counts as covered,
// so the line a user reads back has to be the one last written, and only a lane
// that sets one may show it.
func TestLaneEditRelateRoundTripsThroughList(t *testing.T) {
	vouchListFixture(t)
	const rewritten = "node scripts/relate-v2.mjs --json"
	mustEditLane(t, "web", "--relate", rewritten)

	var out string
	if err := runCmdCapturing(t, &out, Test(), "lane", "list"); err != nil {
		t.Fatal(err)
	}
	if web := laneBlock(t, out, "web"); !strings.Contains(web, "  relate: "+rewritten) {
		t.Errorf("the listing does not print the rewritten relate:\n%s", web)
	}
	if docs := laneBlock(t, out, "docs"); strings.Contains(docs, "relate") {
		t.Errorf("a lane with no relate listed one:\n%s", docs)
	}
}

// TestLaneListShowsADeadRelate: printed on its own field, so the listing can
// explain the validate problem a hand-edited relate raises.
func TestLaneListShowsADeadRelate(t *testing.T) {
	dir := laneFixture(t)
	appendLanes(t, dir, `[[runtime.test_lane]]
  name = "web"
  match = ["src/**"]
  command = "npx vitest run"
  relate = "node scripts/relate.mjs"`)

	var out string
	if err := runCmdCapturing(t, &out, Test(), "lane", "list"); err != nil {
		t.Fatal(err)
	}
	web := laneBlock(t, out, "web")
	if !strings.Contains(web, "  relate: node scripts/relate.mjs") {
		t.Errorf("the listing hid a relate the lane carries:\n%s", web)
	}
	if strings.Contains(web, "vouch") {
		t.Errorf("a lane that does not vouch listed it:\n%s", web)
	}
}

// TestLaneAddEchoesVouchLikeList: identity rather than two literals, for the
// reason TestListAndAddRenderTemplatesIdentically gives.
func TestLaneAddEchoesVouchLikeList(t *testing.T) {
	vouchFixture(t)

	var added string
	if err := runCmdCapturing(t, &added, Test(), "lane", "add", "web", "--match", "src/**",
		"--command", "npx vitest run", "--selector", "path", "--vouch", "--relate", testRelate); err != nil {
		t.Fatalf("lane add: %v", err)
	}
	var listed string
	if err := runCmdCapturing(t, &listed, Test(), "lane", "list"); err != nil {
		t.Fatal(err)
	}
	vouchLines := func(out string) []string {
		var got []string
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(line, "  vouch:") || strings.HasPrefix(line, "  relate:") {
				got = append(got, line)
			}
		}
		return got
	}
	fromAdd, fromList := vouchLines(added), vouchLines(listed)
	if len(fromAdd) != 2 {
		t.Fatalf("lane add echoed %d vouch line(s), want 2:\n%s", len(fromAdd), added)
	}
	if strings.Join(fromAdd, "\n") != strings.Join(fromList, "\n") {
		t.Errorf("lane list renders vouch differently from lane add:\n add: %q\nlist: %q", fromAdd, fromList)
	}
}

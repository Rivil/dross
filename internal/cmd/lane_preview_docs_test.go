package cmd

// The README's lane row and the preview verb.
//
// Every assertion here is PER-ROW rather than over the whole document. The
// README is one long table of commands, so a whole-body `strings.Contains` for
// "preview" or "--json" is satisfied by a mention three commands away — an
// assertion that can never fail is worse than none, because it reads as
// coverage.

import (
	"strings"
	"testing"
)

// laneRow is the single README row documenting the lane verbs.
func laneRow(t *testing.T) string {
	t.Helper()
	return readmeRowContaining(t, readmeBody(t), "dross test lane")
}

// TestReadmeDocumentsThePreviewVerb: a verb nobody can find is a verb that does
// not exist.
//
// The two flags are named alongside it because they are the ones that change
// what preview COSTS and what it emits — `--no-probe` is the difference between
// an ssh round trip and an offline read, and `--json` is the difference between
// a transcript and something a script can consume.
func TestReadmeDocumentsThePreviewVerb(t *testing.T) {
	row := laneRow(t)
	for _, want := range []string{"preview", "--no-probe", "--json"} {
		if !strings.Contains(row, want) {
			t.Errorf("the lane row does not name %q", want)
		}
	}
	// Appended rather than inserted: the toolchain row's own assertion pins the
	// verb-list PREFIX, so a verb spliced into the middle of the group reads as
	// that row going missing.
	if !strings.Contains(row, "{add,list,edit,remove,install,preview}") {
		t.Errorf("the row's own heading does not list preview as a lane verb:\n%s", firstCell(row))
	}
}

// TestReadmeDocumentsTheListingTemplateFields: the two fields `lane list` gained
// are only useful if a reader knows the listing shows them.
//
// Until this phase the template was visible ONLY at the moment a lane was
// declared, so a user reading a listing back could see `selector: dir` and
// reconstruct a line the lane would never run.
func TestReadmeDocumentsTheListingTemplateFields(t *testing.T) {
	row := laneRow(t)
	if !strings.Contains(row, "lane list` prints `selector-template:` and `selector-join:`") {
		t.Errorf("the row does not say `lane list` prints the template fields:\n%s", row)
	}
}

// TestPreviewExitPolicyIsDocumented is the sentence that stops preview being
// wired as a CI gate.
//
// Locked preview_exit_status: a verdict in the exit status would duplicate
// `dross test`'s own refusal in a surface that never ran a test — and a reader
// who does not know that will reach for `preview && commit` precisely because
// it looks like a cheap gate.
func TestPreviewExitPolicyIsDocumented(t *testing.T) {
	row := laneRow(t)
	if !strings.Contains(row, "always exits 0") {
		t.Errorf("the row does not state preview's exit policy:\n%s", row)
	}
	if !strings.Contains(row, "`no lane matches`") {
		t.Errorf("the row does not name `no lane matches` as a finding rather than a verdict:\n%s", row)
	}
}

// firstCell is a row's first table cell, for a message that names the row
// without repeating a paragraph of it.
func firstCell(row string) string {
	parts := strings.SplitN(strings.TrimPrefix(row, "|"), "|", 2)
	return strings.TrimSpace(parts[0])
}

package cmd

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/changes"
)

// One fixture, three commands, one answer.
//
// v1.4 shipped with two readings of "is this phase done" live at once: `dross
// status` counted verify.toml verdicts and `dross milestone progress` counted
// changes.json completion records. Over eleven phases that carried a completion
// record and no verify.toml, the status bar read 0/11 while progress read
// 11/11 — both from the same directories, on the same machine, in the same
// second.
//
// The guard c-5 names is this file: any command that re-derives doneness from
// the verdict inverts the A/B fixture below and disagrees with the other two.
// A per-command test cannot catch that, because each one passes alone; only
// asking all three the same question does.
//
// Its sibling is reentry_signal_truth_test.go, which runs the same shape over
// the four RE-ENTRY surfaces — the watch drift classifier, the reconcile count,
// status's shipped/waiting line and the SessionStart re-entry line. The two are
// deliberately not merged: truthFixture here is git-free and askAllThree
// asserts on an N/M count regex, and none of those four print a count or work
// without a real branch and origin. So c-5's "one fixture" means one fixture per
// surface family, not one file.

// doneCount is one command's answer, kept with the command's name so a
// disagreement can say who disagreed.
type doneCount struct {
	command string
	done    int
	total   int
}

var (
	statusPhaseCount = regexp.MustCompile(`(\d+)/(\d+) phases`)
	listFooterCount  = regexp.MustCompile(`(?m)^(\d+)/(\d+) done$`)
)

// askAllThree runs `dross status`, `dross milestone progress` and `dross phase
// list --milestone` over the current fixture and returns what each reported.
func askAllThree(t *testing.T, version string) []doneCount {
	t.Helper()

	statusOut := captureStdout(t, func() {
		if err := runCmd(t, Status()); err != nil {
			t.Fatalf("status: %v", err)
		}
	})
	sd, st := mustMatchCount(t, statusPhaseCount, statusOut, "dross status")

	rep := progressJSON(t, version, "--json")

	listOut := captureStdout(t, func() {
		if err := runCmd(t, Phase(), "list", "--milestone", version); err != nil {
			t.Fatalf("phase list --milestone: %v", err)
		}
	})
	ld, lt := mustMatchCount(t, listFooterCount, listOut, "dross phase list --milestone")

	return []doneCount{
		{"dross status", sd, st},
		{"dross milestone progress", rep.Done, rep.Total},
		{"dross phase list --milestone", ld, lt},
	}
}

func mustMatchCount(t *testing.T, re *regexp.Regexp, out, command string) (int, int) {
	t.Helper()
	m := re.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("%s printed no done/total count:\n%s", command, out)
	}
	done, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("%s: %v", command, err)
	}
	total, err := strconv.Atoi(m[2])
	if err != nil {
		t.Fatalf("%s: %v", command, err)
	}
	return done, total
}

// assertAllAgree fails naming every command whose answer differs from want.
func assertAllAgree(t *testing.T, got []doneCount, wantDone, wantTotal int) {
	t.Helper()
	for _, c := range got {
		if c.done != wantDone || c.total != wantTotal {
			t.Errorf("%s reports %d/%d, want %d/%d — the three commands disagree over one fixture",
				c.command, c.done, c.total, wantDone, wantTotal)
		}
	}
}

// truthFixture builds a repo with `version` as the current milestone and the
// given phases on its roadmap, and returns its .dross root.
func truthFixture(t *testing.T, version string, phases ...string) string {
	t.Helper()
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, State(), "set", "current_milestone", version); err != nil {
		t.Fatal(err)
	}
	quoted := make([]string, 0, len(phases))
	for _, p := range phases {
		quoted = append(quoted, `"`+p+`"`)
	}
	mustWrite(t, filepath.Join(dir, ".dross", "milestones", version+".toml"),
		fmt.Sprintf("phases = [%s]\n\n[milestone]\nversion = %q\nstatus = \"active\"\n",
			strings.Join(quoted, ", "), version))
	return filepath.Join(dir, ".dross")
}

// TestThreeCommandsAgreeOnRecordVersusVerdict inverts the two readings against
// each other: two phases are done with no verify.toml at all, and the third has
// a passing verdict and no completion record. The truth is 2/3 and a
// verdict-reading command reports 1/3 — from the opposite phase.
//
// The count is deliberately asymmetric. A one-done/one-verified pair would make
// a verdict-reader report 1/2, the same NUMBER as the truth over the wrong
// phase, and every total-based assertion would pass on an inverted answer.
// `dross status` prints only a count, so a symmetric fixture cannot catch it
// there at all.
func TestThreeCommandsAgreeOnRecordVersusVerdict(t *testing.T) {
	root := truthFixture(t, "v1.5", "record-complete", "record-shipped", "verdict-no-record")
	recordPhaseStatus(t, "record-complete", changes.StatusComplete)
	recordPhaseStatus(t, "record-shipped", changes.StatusShipped)
	recordPhaseStatus(t, "verdict-no-record", "")
	mustWrite(t, filepath.Join(root, "phases", "verdict-no-record", "verify.toml"),
		"[summary]\n  verdict = \"pass\"\n")

	assertAllAgree(t, askAllThree(t, "v1.5"), 2, 3)

	// Which phase, not just how many. The listing is where doneness is
	// attributable per phase, and progress names what is left.
	listOut := captureStdout(t, func() {
		if err := runCmd(t, Phase(), "list", "--milestone", "v1.5"); err != nil {
			t.Fatalf("phase list --milestone: %v", err)
		}
	})
	if !strings.Contains(listOut, "✓ record-complete") || !strings.Contains(listOut, "✓ record-shipped") {
		t.Errorf("both completion records must read done:\n%s", listOut)
	}
	if strings.Contains(listOut, "✓ verdict-no-record") {
		t.Errorf("a passing verdict is not doneness — verified is not shipped:\n%s", listOut)
	}
	rep := progressJSON(t, "v1.5", "--json")
	if !hasSlug(rep.Remaining, "verdict-no-record") {
		t.Errorf("progress remaining = %v, want the verdict-only phase outstanding", rep.Remaining)
	}
}

// TestThreeCommandsAgreeOnAFullMilestone is v1.4's actual shape: eleven phases,
// every one carrying a completion record, not one carrying a verify.toml. This
// is the fixture the status bar read 0/11 on. All three commands must read
// 11/11, and the failure names whichever one does not.
func TestThreeCommandsAgreeOnAFullMilestone(t *testing.T) {
	var phases []string
	for i := 1; i <= 11; i++ {
		phases = append(phases, fmt.Sprintf("phase-%02d", i))
	}
	truthFixture(t, "v1.4", phases...)
	for _, p := range phases {
		recordPhaseStatus(t, p, changes.StatusComplete)
	}

	assertAllAgree(t, askAllThree(t, "v1.4"), 11, 11)
}

// TestThreeCommandsAgreeOnNothingDone is the floor of the same guard: with a
// roadmap where nothing is done — one phase scaffolded but unrecorded, one
// never built — no command may invent progress. A reader that counts scaffolded
// directories, or one that treats a missing record as "probably finished",
// fails here while both tests above still pass.
func TestThreeCommandsAgreeOnNothingDone(t *testing.T) {
	truthFixture(t, "v1.5", "started", "never-built")
	recordPhaseStatus(t, "started", "")

	assertAllAgree(t, askAllThree(t, "v1.5"), 0, 2)
}

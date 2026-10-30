package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/changes"
	"github.com/Rivil/dross/internal/killswitch"
)

// The trial phase's last task completed at ksAnchor; a run is "later" only
// after it.
var (
	ksAnchor = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	ksLater  = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	ksBefore = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
)

const (
	ksPending = "[trial]\n  status = \"pending\"\n"
	// ksAfterRecorded has the [after] leg mined but [trial] not yet flipped.
	ksAfterRecorded = ksPending + "\n[[after.task]]\n  phase = \"later\"\n  task = \"t-1\"\n  peak_context = 1\n"
	ksDecided       = "[trial]\n  status = \"pass\"\n"
)

// ksRepo is a git repo with an empty .dross/phases, not chdir'd.
func ksRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitInit(t, dir, "https://example.invalid/r.git")
	if err := os.MkdirAll(filepath.Join(dir, ".dross", "phases"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// ksTaskCommit commits name at size bytes, then a one-byte edit to it, and
// returns the edit's sha: the task commit whose parent dispatch measured.
func ksTaskCommit(t *testing.T, dir, name string, size int) string {
	t.Helper()
	mustWrite(t, filepath.Join(dir, name), strings.Repeat("x", size))
	mustGit(t, dir, "add", name)
	mustGit(t, dir, "commit", "-qm", "base "+name)
	mustWrite(t, filepath.Join(dir, name), strings.Repeat("x", size+1))
	mustGit(t, dir, "commit", "-qam", "task "+name)
	return mustGit(t, dir, "rev-parse", "HEAD")
}

// ksTrial writes the trialling phase: its kill-switch.toml and one recorded
// task completed at ksAnchor.
func ksTrial(t *testing.T, dir, ksBody string) {
	t.Helper()
	root := filepath.Join(dir, ".dross")
	mustWrite(t, filepath.Join(root, "phases", "trial", killswitch.File), ksBody)
	c := changes.New("trial")
	c.Tasks["t-1"] = changes.TaskRecord{Files: []string{"trial.go"}, Commit: "abc1234", CompletedAt: ksAnchor}
	if err := c.Save(changes.FilePath(root, "trial")); err != nil {
		t.Fatal(err)
	}
}

// ksRun describes the later phase's run; the zero-value overrides give a
// completed solo run whose one task a writer took.
type ksRun struct {
	file       string
	size       int
	at         time.Time
	pair       bool // no review recorded
	unfinished bool // a second task still pending
	gone       bool // the task's commit is no longer in git
	noCommit   bool // the task recorded no commit
	alsoGone   bool // a reviewed t-0, listed first, whose commit is gone
}

// ksGoneSHA names a commit no fixture repo has: what a squash-merged, deleted
// and gc'd phase branch leaves behind in changes.json.
const ksGoneSHA = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"

func ksLaterRun(t *testing.T, dir string, r ksRun) {
	t.Helper()
	if r.file == "" {
		r.file = "big.go"
	}
	if r.size == 0 {
		r.size = writerSurfaceBytes + 10000
	}
	if r.at.IsZero() {
		r.at = ksLater
	}
	sha := ksTaskCommit(t, dir, r.file, r.size)
	switch {
	case r.gone:
		sha = ksGoneSHA
	case r.noCommit:
		sha = ""
	}
	root := filepath.Join(dir, ".dross")
	plan := "[phase]\n  id = \"later\"\n"
	if r.alsoGone {
		plan += "\n[[task]]\n  id = \"t-0\"\n  wave = 1\n  title = \"zero\"\n  files = [\"gone.go\"]\n  status = \"done\"\n"
	}
	plan += "\n[[task]]\n  id = \"t-1\"\n  wave = 1\n  title = \"one\"\n  files = [\"" + r.file + "\"]\n  status = \"done\"\n"
	if r.unfinished {
		plan += "\n[[task]]\n  id = \"t-2\"\n  wave = 1\n  title = \"two\"\n  files = [\"other.go\"]\n  status = \"pending\"\n"
	}
	mustWrite(t, filepath.Join(root, "phases", "later", "plan.toml"), plan)
	c := changes.New("later")
	c.Tasks["t-1"] = changes.TaskRecord{Files: []string{r.file}, Commit: sha, CompletedAt: r.at}
	if r.alsoGone {
		c.Tasks["t-0"] = changes.TaskRecord{Files: []string{"gone.go"}, Commit: ksGoneSHA, CompletedAt: r.at}
	}
	if !r.pair {
		c.SetReview("t-1", changes.TaskReview{Outcome: "pass", Rounds: 1})
		if r.alsoGone {
			c.SetReview("t-0", changes.TaskReview{Outcome: "pass", Rounds: 1})
		}
	}
	if err := c.Save(changes.FilePath(root, "later")); err != nil {
		t.Fatal(err)
	}
}

// TestDoctorKillSwitchAfterDue: a pending trial with a later, completed solo
// run in which a writer took a task names the trial's phase dir, the run and
// the mining step — and doctor prints it as an advisory that leaves the exit
// state alone.
func TestDoctorKillSwitchAfterDue(t *testing.T) {
	t.Run("helper", func(t *testing.T) {
		dir := ksRepo(t)
		ksTrial(t, dir, ksPending)
		ksLaterRun(t, dir, ksRun{})
		got := killSwitchWarnings(filepath.Join(dir, ".dross"), dir)
		if len(got) != 1 {
			t.Fatalf("warnings = %q, want exactly one", got)
		}
		for _, want := range []string{".dross/phases/trial", "later", "[before] filter", "[[after.task]]"} {
			if !strings.Contains(got[0], want) {
				t.Errorf("warning %q does not name %q", got[0], want)
			}
		}
	})

	t.Run("doctor", func(t *testing.T) {
		dir := residueFixture(t)
		ksTrial(t, dir, ksPending)
		var control string
		controlErr := runCmdCapturing(t, &control, Doctor())
		if strings.Contains(control, "Kill switch:") {
			t.Fatalf("fixture: the control run has no later run and must be silent:\n%s", control)
		}

		ksLaterRun(t, dir, ksRun{})
		// Pushed, so the run's commits are not phase commits ahead of origin —
		// a separate doctor issue that would move the exit state on its own.
		mustGit(t, dir, "push", "-q", "origin", "main")
		var out string
		err := runCmdCapturing(t, &out, Doctor())
		if !strings.Contains(out, "Kill switch:") {
			t.Fatalf("doctor stayed silent with the [after] leg due:\n%s", out)
		}
		if !strings.Contains(out, ".dross/phases/trial") || !strings.Contains(out, "Advisory only") {
			t.Errorf("the section must name the phase dir and say it is advisory:\n%s", out)
		}
		if (err == nil) != (controlErr == nil) {
			t.Errorf("the kill-switch warning changed doctor's exit state: control=%v due=%v", controlErr, err)
		}
	})

	t.Run("gate boundary warns", func(t *testing.T) {
		// TestDoctorKillSwitchQuiet's under-gate case is one byte below the
		// gate; this pins the boundary from the other side.
		dir := ksRepo(t)
		ksTrial(t, dir, ksPending)
		ksLaterRun(t, dir, ksRun{size: writerSurfaceBytes})
		if got := killSwitchWarnings(filepath.Join(dir, ".dross"), dir); len(got) != 1 {
			t.Errorf("a task exactly at the gate went to a writer; warnings = %q", got)
		}
	})

	t.Run("undecodable record is named", func(t *testing.T) {
		dir := ksRepo(t)
		ksTrial(t, dir, "[trial\n")
		got := killSwitchWarnings(filepath.Join(dir, ".dross"), dir)
		if len(got) != 1 || !strings.Contains(got[0], "does not decode") {
			t.Errorf("warnings = %q, want one naming the undecodable record", got)
		}
	})

	t.Run("unreadable record is named", func(t *testing.T) {
		// A directory where the record should be: present, so not skipped as
		// absent, and not readable as a file.
		dir := ksRepo(t)
		if err := os.MkdirAll(filepath.Join(dir, ".dross", "phases", "trial", killswitch.File), 0o755); err != nil {
			t.Fatal(err)
		}
		got := killSwitchWarnings(filepath.Join(dir, ".dross"), dir)
		if len(got) != 1 || !strings.Contains(got[0], ".dross/phases/trial/"+killswitch.File) {
			t.Errorf("warnings = %q, want one naming the unreadable record", got)
		}
	})
}

// TestDoctorKillSwitchCommitGone: once a later solo run ships, its task
// commits are squash-merged away and its writer use can no longer be sized.
// That is the normal post-ship state, so it must read as "may be due", never
// as no run at all.
func TestDoctorKillSwitchCommitGone(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  ksRun
	}{
		{name: "commit gone", run: ksRun{gone: true}},
		{name: "commit gone, under the size gate", run: ksRun{gone: true, file: "small.go", size: writerSurfaceBytes - 1}},
		{name: "no commit recorded", run: ksRun{noCommit: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := ksRepo(t)
			ksTrial(t, dir, ksPending)
			ksLaterRun(t, dir, tc.run)
			got := killSwitchWarnings(filepath.Join(dir, ".dross"), dir)
			if len(got) != 1 {
				t.Fatalf("warnings = %q, want exactly one", got)
			}
			for _, want := range []string{".dross/phases/trial", "later", "could not be confirmed", "may be due", "[[after.task]]"} {
				if !strings.Contains(got[0], want) {
					t.Errorf("warning %q does not name %q", got[0], want)
				}
			}
		})
	}

	t.Run("a proven writer task outranks a gone one", func(t *testing.T) {
		dir := ksRepo(t)
		ksTrial(t, dir, ksPending)
		ksLaterRun(t, dir, ksRun{alsoGone: true})
		got := killSwitchWarnings(filepath.Join(dir, ".dross"), dir)
		if len(got) != 1 || strings.Contains(got[0], "could not be confirmed") || !strings.Contains(got[0], "with a writer") {
			t.Errorf("warnings = %q, want only the confirmed-writer warning", got)
		}
	})

	t.Run("a gone commit under an unproven run stays unknown", func(t *testing.T) {
		dir := ksRepo(t)
		ksTrial(t, dir, ksPending)
		ksLaterRun(t, dir, ksRun{alsoGone: true, file: "small.go", size: writerSurfaceBytes - 1})
		got := killSwitchWarnings(filepath.Join(dir, ".dross"), dir)
		if len(got) != 1 || !strings.Contains(got[0], "could not be confirmed") {
			t.Errorf("warnings = %q, want one unconfirmed-run warning", got)
		}
	})
}

// TestDoctorKillSwitchQuiet: each case flips one thing from the due fixture,
// and none of them is a mined-and-waiting [after] leg. A warning here would
// nag a trial that has nothing to measure yet, or one already measured.
func TestDoctorKillSwitchQuiet(t *testing.T) {
	cases := []struct {
		name string
		ks   string
		run  *ksRun
	}{
		{name: "no later run", ks: ksPending},
		{name: "after recorded, trial still pending", ks: ksAfterRecorded, run: &ksRun{}},
		{name: "trial decided", ks: ksDecided, run: &ksRun{}},
		{name: "pair run, no review", ks: ksPending, run: &ksRun{pair: true}},
		{name: "solo run under the size gate", ks: ksPending, run: &ksRun{file: "small.go", size: writerSurfaceBytes - 1}},
		{name: "solo run before the trial", ks: ksPending, run: &ksRun{at: ksBefore}},
		{name: "solo run at the anchor", ks: ksPending, run: &ksRun{at: ksAnchor}},
		{name: "solo run unfinished", ks: ksPending, run: &ksRun{unfinished: true}},
		{name: "commit gone, after recorded", ks: ksAfterRecorded, run: &ksRun{gone: true}},
		{name: "commit gone, trial decided", ks: ksDecided, run: &ksRun{gone: true}},
		{name: "commit gone, pair run", ks: ksPending, run: &ksRun{gone: true, pair: true}},
		{name: "commit gone, before the trial", ks: ksPending, run: &ksRun{gone: true, at: ksBefore}},
		{name: "commit gone, run unfinished", ks: ksPending, run: &ksRun{gone: true, unfinished: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := ksRepo(t)
			ksTrial(t, dir, tc.ks)
			if tc.run != nil {
				ksLaterRun(t, dir, *tc.run)
			}
			if got := killSwitchWarnings(filepath.Join(dir, ".dross"), dir); len(got) != 0 {
				t.Errorf("warnings = %q, want none", got)
			}
		})
	}

	t.Run("no anchor", func(t *testing.T) {
		dir := ksRepo(t)
		mustWrite(t, filepath.Join(dir, ".dross", "phases", "trial", killswitch.File), ksPending)
		ksLaterRun(t, dir, ksRun{})
		if got := killSwitchWarnings(filepath.Join(dir, ".dross"), dir); len(got) != 0 {
			t.Errorf("a trial phase with no recorded task shipped no writer; warnings = %q", got)
		}
	})

}

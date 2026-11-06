package cmd

// The relate half of a vouching lane's `dross test --files` run.
//
// A vouching lane cannot know its tests until something has asked: relate runs
// once over the lane's paths, and what it prints decides the one batched line
// (lanePlan builds the relate line, laneBatchLine the batched one). Everything
// here sits between the two, and it records nothing. This file says what a run
// measured; turning that into a scoped green is the recorder's job.

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/remote"
	"github.com/Rivil/dross/internal/testlane"
)

// scopedRun is what one run's vouching lanes said about their paths, held until
// the end of the run because a lane can only speak for itself: a path one lane's
// relate omitted may be a path another lane's relate covered.
type scopedRun struct {
	// covered are the paths a relate mapped to at least one test.
	covered []string
	// uncovered are the paths some lane could not cover: what a relate omitted,
	// and the build config kept off a relate line.
	uncovered []string
}

// withhold notes the build config lanePlan kept off a lane's relate line, and
// reports whether the lane has anything left to ask about. A lane scoped to
// nothing with no config in hand is a deleted-paths lane, which stays in the run
// to be told as the miss it always was.
func (s *scopedRun) withhold(pl plannedLane) bool {
	s.uncovered = append(s.uncovered, pl.Withheld...)
	return !pl.ScopedToNothing || len(pl.Withheld) == 0
}

// note folds one relate's answer into the run.
func (s *scopedRun) note(rel testlane.Relation) {
	s.covered = append(s.covered, rel.Covered...)
	s.uncovered = append(s.uncovered, rel.Missed...)
}

// report names, once, the paths this run could not cover. It answers through the
// classifier the commit gate uses, so a path covered by a second lane is not
// named, an exempt path is no gap, and build config is named whatever an exempt
// glob says of it.
//
// It reports the asked list, not a commit, and it is silent for a run with no
// vouching lane, whose transcript has nothing of this in it.
func (s *scopedRun) report(tr *testRun, exempt []string) {
	if gaps := testlane.NeedsFullGreen(s.uncovered, s.covered, exempt); len(gaps) > 0 {
		tr.say("uncovered: %s\n", strings.Join(gaps, " "))
	}
}

// runLaneRelate spawns a vouching lane's relate on the lane's own site and reads
// what it printed.
//
// It spawns as aux because its output is a map rather than a test run: the log
// keeps it, and no parser is asked to claim it. Only stdout is read. Stderr is
// where a tool's progress and warnings go, and one that happens to look like a
// mapping must not cover a path.
//
// Every failure here is exitPrepareFailed, or the transport's own status when the
// run never reached the host. Both leave the lane's command unspawned.
func runLaneRelate(tr *testRun, target *remote.Target, repoDir string, pl plannedLane) (testlane.Relation, error) {
	var stdout bytes.Buffer
	if target == nil {
		tr.spawn(pl.Relate, true)
		if err := spawnLocal(repoDir, pl.Relate, io.MultiWriter(tr.out, &stdout), tr.out); err != nil {
			tr.failed()
			return testlane.Relation{}, relateFailure(pl.lane, err)
		}
	} else if err := runRemoteLineTee(tr, *target, pl.Relate, true, &stdout); err != nil {
		// Only a command-exit failure is re-tagged, as runLanePrepare does: an
		// unreachable host says nothing about relate.
		if ExitCode(err) != exitSuiteFailed {
			return testlane.Relation{}, err
		}
		return testlane.Relation{}, relateFailure(pl.lane, err)
	}
	//dross:taint-cleared relate's stdout passes ParseRelate's shape check: covered and missed are strings of the asked list, tests are in-tree files, and a refusal holds a line number and none of the line
	rel, err := testlane.ParseRelate(repoDir, pl.Asked, stdout.String())
	if err != nil {
		tr.failed()
		return testlane.Relation{}, relateFailure(pl.lane, err)
	}
	return rel, nil
}

// relateFailure is the message a failed relate produces. It shares the prepare's
// exit because it means the same thing to a caller, and it names relate because
// a reader sent to the lane's prepare line would find nothing wrong there.
func relateFailure(lane project.TestLane, err error) error {
	return &ExitCodeError{Code: exitPrepareFailed, Err: fmt.Errorf(
		"test lane %q relate failed, so its tests did not run — %s: %w", lane.Name, lane.Relate, err)}
}

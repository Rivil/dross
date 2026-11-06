package cmd

// The relate half of a vouching lane's `dross test --files` run.
//
// A vouching lane cannot know its tests until something has asked: relate runs
// once over the lane's paths, and what it prints decides the one batched line
// (lanePlan builds the relate line, laneBatchLine the batched one). scopedRun
// says what a run measured, and only scopedRecorder turns that into a scoped
// green, once the run's verdict is in.

import (
	"bytes"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/Rivil/dross/internal/consent"
	"github.com/Rivil/dross/internal/gatestate"
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/remote"
	"github.com/Rivil/dross/internal/testlane"
)

// scopedRun is what one run's vouching lanes said about their paths, held until
// the end of the run because a lane can only speak for itself: a path one lane's
// relate omitted may be a path another lane's relate covered.
type scopedRun struct {
	// covered are the paths a relate mapped to at least one test, once that
	// lane's batched line has passed.
	covered []string
	// uncovered are the paths some lane could not cover: everything a relate
	// answered for, and the build config kept off a relate line. A path a passing
	// batch went on to cover is in covered too, and covered wins.
	uncovered []string
	// lanes are the consent.Fingerprint of each lane that filled covered. Only a
	// vouching lane can: a gate sinks a record naming any other.
	lanes []string
	// red is set by any lane that went red, vouching or not. The run's exit
	// cannot carry it, because a failed prepare beside a red outranks the red.
	red bool
}

// withhold notes the build config lanePlan kept off a lane's relate line, and
// reports whether the lane has anything left to ask about. A lane scoped to
// nothing with no config in hand is a deleted-paths lane, which stays in the run
// to be told as the miss it always was.
func (s *scopedRun) withhold(pl plannedLane) bool {
	s.uncovered = append(s.uncovered, pl.Withheld...)
	return !pl.ScopedToNothing || len(pl.Withheld) == 0
}

// note folds one relate's answer into the run. Nothing is covered yet: a path
// relate mapped still has to see its tests pass, so until settle says so it is
// as uncovered as one relate omitted.
func (s *scopedRun) note(rel testlane.Relation) {
	s.uncovered = append(s.uncovered, rel.Missed...)
	s.uncovered = append(s.uncovered, rel.Covered...)
}

// settle folds one lane's finished line into the run: err is what it returned,
// and rel what relate said, which is nothing on a lane that does not vouch. A
// declared empty_exit never gets here, because a batch that collected nothing
// covered nothing.
//
// A lane with no LaneLine has no fingerprint to stand on. consent.Fingerprint
// would hash the empty string happily, so it is refused here rather than left to
// name a lane that can never run.
func (s *scopedRun) settle(lane project.TestLane, rel testlane.Relation, err error) {
	s.red = s.red || isRed(err)
	line := consent.LaneLine(lane)
	if err != nil || line == "" || len(rel.Covered) == 0 {
		return
	}
	s.covered = append(s.covered, rel.Covered...)
	s.lanes = append(s.lanes, consent.Fingerprint(line))
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

// scopedRecorder writes .dross/gate/scoped.json for a `--files` run: the record
// commit-green admits a commit against when every changed path is in it
// (scoped_gate_optin). tree is the fingerprint half it shares with the full
// run's recorder, so the two cannot disagree on what a moved tree is.
//
// Each run that records replaces the last, so runs never union. A red anywhere in
// the run clears a record on its tree, and a run that measured nothing leaves it.
//
// Like greenRecorder, it never changes the run's outcome.
type scopedRecorder struct {
	tree *greenRecorder
	tr   *testRun
	run  scopedRun
}

// startScoped fingerprints the tree when a lane in this repo vouches. Every other
// repo has no use for the record and pays for no fingerprint.
func startScoped(tr *testRun, repoDir string, proj *project.Project) *scopedRecorder {
	vouches := slices.ContainsFunc(proj.Runtime.TestLane, func(l project.TestLane) bool { return l.Vouch })
	return &scopedRecorder{tree: startGreen(repoDir, vouches), tr: tr}
}

// finish records the run's verdict against the tree it measured.
func (r *scopedRecorder) finish(runErr error) {
	if !r.tree.active {
		return
	}
	if r.run.red {
		r.tree.clearScoped()
		return
	}
	if runErr != nil || len(r.run.covered) == 0 {
		return
	}
	tree, ok := r.tree.settled()
	if !ok {
		return
	}
	// Two lanes can cover one path, and the count below is of paths.
	slices.Sort(r.run.covered)
	rec := gatestate.Scoped{
		Tree:    tree,
		Covered: slices.Compact(r.run.covered),
		Lanes:   r.run.lanes,
		At:      time.Now().UTC(),
		Runner:  r.tree.sites.String(),
	}
	if err := gatestate.SaveScoped(r.tree.repoDir, rec); err != nil {
		r.tree.warn(err)
		return
	}
	r.tr.say("scoped green: %d covered\n", len(rec.Covered))
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

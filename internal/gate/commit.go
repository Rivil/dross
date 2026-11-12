package gate

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/Rivil/dross/internal/consent"
	"github.com/Rivil/dross/internal/gatestate"
	"github.com/Rivil/dross/internal/localstore"
	"github.com/Rivil/dross/internal/pathfence"
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/shellscan"
	"github.com/Rivil/dross/internal/testlane"
	"github.com/Rivil/dross/internal/treefp"
)

// commit-green (c-4): a `git commit` in a dross repo is admitted only when the
// tree it would record is the tree the last full green `dross test` ran on.
// The candidate is the real index plus the `git add`s chained ahead of the
// commit (and -a); a commit that changes nothing outside .dross/ — dross's own
// bookkeeping — passes ungated. A raw `go test` records nothing, so it never
// counts. A repo with neither a test command nor a test lane can never record
// a green, so the gate stays silent there rather than refuse every commit
// (no_test_repo).
//
// A repo with a vouching lane has a second way in (scoped_gate_optin): a scoped
// green for the exact tree, from lanes that still vouch, when every changed path
// outside .dross/ is covered or exempt (judgeVouched). Every other repo keeps the
// full-green rule, whatever a scoped.json or a test_exempt list says.
func init() {
	Register(Gate{
		Name: "commit-green", Scope: Workflow, Liftable: true,
		Claims: claimsCommit, Judge: judgeCommit,
	})
}

func isCommit(cmd shellscan.Command) (shellscan.GitCall, bool) {
	g, ok := cmd.Git()
	return g, ok && g.Sub == "commit"
}

func claimsCommit(c *Call) bool {
	if c.ToolName != "Bash" {
		return false
	}
	s := c.Script()
	for _, cmd := range s.Commands {
		if _, ok := isCommit(cmd); ok {
			return true
		}
	}
	if s.Partial {
		toks := rawTokens(c.Command())
		for i := 0; i+1 < len(toks); i++ {
			if path.Base(toks[i]) == "git" && toks[i+1] == "commit" {
				return true
			}
		}
	}
	return false
}

const stagePlainly = "stage with `git add`, then run a plain `git commit` (no pathspec, -p, --only or --include)"

// commitValueOpts are `git commit` options whose value is the next word.
var commitValueOpts = map[string]bool{
	"-m": true, "-F": true, "-C": true, "-c": true, "-t": true,
	"--message": true, "--file": true, "--reuse-message": true, "--reedit-message": true,
	"--template": true, "--author": true, "--date": true, "--cleanup": true,
	"--fixup": true, "--squash": true, "--trailer": true,
}

// commitShortValue are the short options that take a value, attached or next.
const commitShortValue = "mFCct"

// unjudgeableCommit are the forms that record something other than the index.
var unjudgeableCommit = map[string]bool{
	"--patch": true, "--interactive": true, "--only": true, "--include": true,
}

// commitShape reads a commit's arguments: whether it is -a, and whether it
// records something other than the staged index (a pathspec, -p, -o, -i).
func commitShape(args []string) (all, unjudgeable bool) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			return all, unjudgeable || i+1 < len(args)
		case a == "--all":
			all = true
		case unjudgeableCommit[a] || strings.HasPrefix(a, "--pathspec-from-file"):
			unjudgeable = true
		case commitValueOpts[a]:
			i++
		case strings.HasPrefix(a, "--"):
		case strings.HasPrefix(a, "-") && len(a) > 1:
			for j := 1; j < len(a); j++ {
				switch ch := a[j]; {
				case ch == 'a':
					all = true
				case ch == 'p' || ch == 'o' || ch == 'i':
					unjudgeable = true
				case strings.IndexByte(commitShortValue, ch) >= 0:
					if j == len(a)-1 {
						i++ // the value is the next word
					}
					j = len(a)
				}
			}
		default:
			unjudgeable = true // a pathspec
		}
	}
	return all, unjudgeable
}

// interactiveAdd reports whether a `git add` waits for a human or edits hunks.
func interactiveAdd(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if a == "--patch" || a == "--interactive" || a == "--edit" ||
			(len(a) > 1 && a[0] == '-' && a[1] != '-' && strings.ContainsAny(a[1:], "pie")) {
			return true
		}
	}
	return false
}

func judgeCommit(c *Call) (*Refusal, error) {
	s := c.Script()
	at, g := commitAt(s)
	// Whose commit is it? Outside a dross repo, or in one that can never
	// record a green, the gate has nothing to say — decided before anything
	// else, so neither case ever spawns git.
	if at >= 0 {
		root, err := LocateRoot(g.Dir)
		if err != nil || root == "" {
			return nil, err
		}
		gated, err := testsDeclared(root)
		if err != nil || !gated {
			return nil, err
		}
	}
	snap, ref, err := commitCandidate(s, at, g)
	if ref != nil || err != nil || onlyDross(snap.Changed) {
		return ref, err
	}
	return judgeTree(g, snap)
}

// judgeTree picks the rule for a commit that touches code: the scoped ladder
// where a lane vouches, the full-green rule everywhere else. Declaring vouch is
// what opts a repo in; whether a lane still vouches is judged per record, so a
// repo whose lanes went stale stays on the ladder and names what is uncovered.
func judgeTree(g shellscan.GitCall, snap treefp.Snapshot) (*Refusal, error) {
	root, err := LocateRoot(g.Dir)
	if err != nil {
		return nil, err
	}
	p, err := declaredProject(root)
	if err != nil {
		return nil, err
	}
	lanes := slices.DeleteFunc(slices.Clone(p.Runtime.TestLane), func(l project.TestLane) bool { return !l.Vouch })
	if len(lanes) == 0 {
		return judgeGreen(g, snap)
	}
	return judgeVouched(g, snap, root, lanes, p.Runtime.TestExempt)
}

// commitAt finds the line's `git commit`: its index among the line's commands
// and its git call, or -1.
func commitAt(s shellscan.Script) (int, shellscan.GitCall) {
	for i, cmd := range s.Commands {
		if gc, ok := isCommit(cmd); ok {
			return i, gc
		}
	}
	return -1, shellscan.GitCall{}
}

// commitCandidate is the one reading of what a claimed line's `git commit`
// would record — the real index plus the `git add`s chained ahead of it (and
// -a) — shared by every gate that judges a commit, so no two of them can
// disagree about the tree. A line it cannot predict is a refusal.
func commitCandidate(s shellscan.Script, at int, g shellscan.GitCall) (treefp.Snapshot, *Refusal, error) {
	if s.Partial || at < 0 {
		r, err := NewRefusal(
			fmt.Sprintf("this line runs `git commit`, but its shell could not be read (%s), so what it would commit is unknown", s.Problem),
			"fix the quoting so the line parses, then retry")
		return treefp.Snapshot{}, r, err
	}
	var adds []treefp.Add
	for _, cmd := range s.Commands[:at] {
		if cmd.Substituted {
			continue // a message's $(cat <<EOF …) and the like belong to their command
		}
		pg, isGit := cmd.Git()
		switch {
		case isGit && pg.Sub == "add":
			if interactiveAdd(pg.Args) {
				r, err := NewRefusal("`git add -p`/`-i`/`-e` stages hunks the gate cannot predict", stagePlainly)
				return treefp.Snapshot{}, r, err
			}
			adds = append(adds, treefp.Add{Dir: pg.Dir, Args: pg.Args})
		case cmd.Name() == "cd" || cmd.Name() == "pushd":
		default:
			r, err := NewRefusal(
				fmt.Sprintf("`%s` runs ahead of `git commit` in the same line, so the tree the commit records cannot be predicted", cmd.Name()),
				"run the other commands in their own call first; chain only `git add` and `cd` ahead of `git commit`")
			return treefp.Snapshot{}, r, err
		}
	}
	all, unjudgeable := commitShape(g.Args)
	if unjudgeable {
		r, err := NewRefusal("this `git commit` records something other than the staged index, which the gate cannot fingerprint", stagePlainly)
		return treefp.Snapshot{}, r, err
	}
	snap, err := treefp.Candidate(g.Dir, adds, all)
	return snap, nil, err
}

// judgeGreen compares what the commit would record with the last green.
func judgeGreen(g shellscan.GitCall, snap treefp.Snapshot) (*Refusal, error) {
	root, err := LocateRoot(g.Dir)
	if err != nil {
		return nil, err
	}
	green, err := gatestate.LoadGreen(root)
	if err != nil {
		return nil, err
	}
	const rerun = "run a bare `dross test` (no selector; in a laned repo no --files) and commit once it is green — a raw `go test` records nothing and does not count"
	if green == nil {
		return NewRefusal("no full green `dross test` is recorded for this repo, so nothing vouches for the tree this commit records", rerun)
	}
	if green.Tree == snap.Tree {
		return nil, nil
	}
	if r, err := leavesOut(g, snap, green.Tree, "full green"); r != nil || err != nil {
		return r, err
	}
	return NewRefusal("the tree changed since the last full green `dross test`, so the commit would record code no full run has measured", rerun)
}

// leavesOut is the refusal for a commit that records part of a tested work tree,
// or nil when tested is not the work tree: re-running the tests cannot converge
// on that, staging or cleaning the named paths can. what names the run.
func leavesOut(g shellscan.GitCall, snap treefp.Snapshot, tested, what string) (*Refusal, error) {
	work, err := treefp.WorkingTree(g.Dir)
	if err != nil || work != tested {
		return nil, err
	}
	paths, err := treefp.Diff(g.Dir, snap.Tree, tested)
	if err != nil {
		return nil, err
	}
	return NewRefusal(
		fmt.Sprintf("the last %s ran on the work tree, but this commit would record a different tree — these paths differ: %s", what, strings.Join(paths, " ")),
		"stage or clean them (`git add` what belongs in the commit; remove or ignore what does not), then commit")
}

// judgeVouched is the ladder for a repo where a lane vouches. A full green on
// the candidate tree settles it first, whatever scoped.json says. Then a commit
// of nothing but exempt paths needs no run at all, and anything else needs a
// scoped green on its exact tree that leaves no path uncovered. Only a human's
// grant makes the exempt globs count, because the list is tracked: without one
// they are ordinary paths, and the refusal says who can clear them.
func judgeVouched(g shellscan.GitCall, snap treefp.Snapshot, root string, lanes []project.TestLane, globs []string) (*Refusal, error) {
	green, err := gatestate.LoadGreen(root)
	if err != nil {
		return nil, err
	}
	if green != nil && green.Tree == snap.Tree {
		return nil, nil
	}
	store := localstore.GrantStore(filepath.Join(root, ".dross"))
	state, cerr := consent.ExemptConsented(store, root, globs)
	if state == consent.Refused {
		return nil, cerr
	}
	var exempt []string
	if state == consent.Granted {
		exempt = globs
	}
	code := codePaths(snap.Changed)
	uncovered := testlane.NeedsFullGreen(code, nil, exempt)
	if len(uncovered) == 0 {
		return nil, nil
	}

	rec, err := gatestate.LoadScoped(root)
	if err != nil {
		return nil, err
	}
	cover, err := vouchedCover(store, root, lanes, rec)
	if err != nil {
		return nil, err
	}
	if len(cover) > 0 {
		if rec.Tree == snap.Tree {
			uncovered = testlane.NeedsFullGreen(code, cover, exempt)
			if len(uncovered) == 0 {
				return nil, nil
			}
		} else if r, err := leavesOut(g, snap, rec.Tree, "scoped green"); r != nil || err != nil {
			return r, err
		}
	}
	if green != nil {
		if r, err := leavesOut(g, snap, green.Tree, "full green"); r != nil || err != nil {
			return r, err
		}
	}

	names := strings.Join(uncovered, " ")
	// Nothing left but what a current grant would exempt: no test run clears
	// that, so a remedy pointing at one would send the agent round in a circle.
	if len(testlane.NeedsFullGreen(uncovered, nil, globs)) == 0 {
		return NewRefusal(
			fmt.Sprintf("these paths commit with no test run only under a current test_exempt grant: %s\n\n%v", names, consent.ExemptRefusal(globs, state, cerr)),
			"a human runs `dross trust --test-exempt` in their own terminal; no test run clears this")
	}
	// Every path is named, not only the ones that moved: a run replaces the last
	// record, so a re-run over fewer paths would drop the others from cover.
	return NewRefusal(
		fmt.Sprintf("no green vouches for these paths in this commit: %s", names),
		"run `dross test --files` over every path in the commit and commit once it is green; build config, a deletion or a path with no related test needs a bare `dross test` — a raw `go test` records nothing and does not count")
}

// vouchedCover is the part of rec's Covered the lanes as they stand still speak
// for. It is nothing unless every lane the record names is a declared vouching
// lane whose line is the one that ran, granted on this machine: Covered does not
// say which lane reached a path, so one lane gone stale sinks the whole record
// rather than leave its paths standing on a lane that may never have reached
// them. A lane that did not run vouches for nothing, and match is outside the
// grant, so each path must still match a lane that did.
func vouchedCover(store consent.Store, root string, lanes []project.TestLane, rec *gatestate.Scoped) ([]string, error) {
	if rec == nil {
		return nil, nil
	}
	var ran []project.TestLane
	live := map[string]bool{}
	for _, lane := range lanes {
		line := consent.LaneLine(lane)
		fp := consent.Fingerprint(line)
		if !slices.Contains(rec.Lanes, fp) {
			continue
		}
		state, cerr := consent.LaneConsented(store, root, lane.Name, line)
		if state == consent.Refused {
			return nil, cerr
		}
		if state != consent.Granted {
			return nil, nil
		}
		ran = append(ran, lane)
		live[fp] = true
	}
	for _, fp := range rec.Lanes {
		if !live[fp] {
			return nil, nil
		}
	}
	var cover []string
	for _, p := range rec.Covered {
		if slices.ContainsFunc(ran, func(l project.TestLane) bool { return testlane.MatchAny(l.Match, p) }) {
			cover = append(cover, p)
		}
	}
	return cover, nil
}

// testsDeclared reports whether the repo can record a green at all: a
// runtime.test_command or a test lane.
func testsDeclared(root string) (bool, error) {
	p, err := declaredProject(root)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(p.Runtime.TestCommand) != "" || len(p.Runtime.TestLane) > 0, nil
}

// declaredProject decodes root's project.toml; a missing file declares nothing.
func declaredProject(root string) (*project.Project, error) {
	f, err := pathfence.Contain(root, "dross project", ".dross/project.toml")
	if err != nil {
		return nil, err
	}
	b, err := pathfence.ReadFile(f)
	if errors.Is(err, fs.ErrNotExist) {
		return &project.Project{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", f.Rel(), err)
	}
	var p project.Project
	if _, err := toml.Decode(string(b), &p); err != nil {
		return nil, fmt.Errorf("decode %s: %w", f.Rel(), err)
	}
	return &p, nil
}

func isDross(p string) bool { return strings.HasPrefix(p, ".dross/") }

func onlyDross(paths []string) bool {
	return !slices.ContainsFunc(paths, func(p string) bool { return !isDross(p) })
}

// codePaths drops dross's own bookkeeping: it is outside every fingerprint, so
// it needs no cover and a refusal has no reason to name it.
func codePaths(paths []string) []string {
	return slices.DeleteFunc(slices.Clone(paths), isDross)
}

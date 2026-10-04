package gate

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/Rivil/dross/internal/gatestate"
	"github.com/Rivil/dross/internal/pathfence"
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/shellscan"
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
	return judgeGreen(g, snap)
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
	// The tested tree may still be the work tree, with the commit leaving
	// some of it out: re-running the suite cannot converge on that, staging
	// or cleaning the named paths can.
	work, err := treefp.WorkingTree(g.Dir)
	if err != nil {
		return nil, err
	}
	if work == green.Tree {
		paths, err := treefp.Diff(g.Dir, snap.Tree, green.Tree)
		if err != nil {
			return nil, err
		}
		return NewRefusal(
			fmt.Sprintf("the last full green ran on the work tree, but this commit would record a different tree — these paths differ: %s", strings.Join(paths, " ")),
			"stage or clean them (`git add` what belongs in the commit; remove or ignore what does not), then commit")
	}
	return NewRefusal("the tree changed since the last full green `dross test`, so the commit would record code no full run has measured", rerun)
}

// testsDeclared reports whether the repo can record a green at all: a
// runtime.test_command or a test lane.
func testsDeclared(root string) (bool, error) {
	f, err := pathfence.Contain(root, "dross project", ".dross/project.toml")
	if err != nil {
		return false, err
	}
	b, err := pathfence.ReadFile(f)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read %s: %w", f.Rel(), err)
	}
	var p project.Project
	if _, err := toml.Decode(string(b), &p); err != nil {
		return false, fmt.Errorf("decode %s: %w", f.Rel(), err)
	}
	return strings.TrimSpace(p.Runtime.TestCommand) != "" || len(p.Runtime.TestLane) > 0, nil
}

func onlyDross(paths []string) bool {
	for _, p := range paths {
		if !strings.HasPrefix(p, ".dross/") {
			return false
		}
	}
	return true
}

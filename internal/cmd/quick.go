package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/changes"
	"github.com/Rivil/dross/internal/gatestate"
	"github.com/Rivil/dross/internal/gitrun"
	"github.com/Rivil/dross/internal/state"
	"github.com/Rivil/dross/internal/testlane"
)

// Quick is `dross quick`: the state /dross-quick keeps through the CLI.
func Quick() *cobra.Command {
	c := &cobra.Command{
		Use:   "quick",
		Short: "Bookkeeping /dross-quick keeps through the CLI",
	}
	c.AddCommand(quickBegin(), quickEnd())
	return c
}

// requireReviewer refuses a solo begin whose reviewer could not run as
// shipped: missing, stale, or shadowed by a repo-level definition. A solo run
// reviews every task before its commit, so without the shipped reviewer it
// would implement every task only to fail each one at review.
func requireReviewer(repoDir string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home: %w", err)
	}
	r := reviewerStatusIn(userAgentsDir(home), repoDir)
	if r.State == reviewerOK {
		return nil
	}
	return fmt.Errorf("refusing a solo run: %s (%s) — %s. A solo run has every task reviewed before its commit; "+
		"without the reviewer dross ships, each task would fail its review", r.Problem(), r.State, r.Remedy())
}

// quickBegin records the mode a /dross-quick runs in, its stated description
// (the solo reviewer's only spec source), the HEAD it began at and when — a
// fresh begin is a fresh review attempt. It writes nothing else.
func quickBegin() *cobra.Command {
	var solo bool
	c := &cobra.Command{
		Use:   "begin <description>",
		Short: "Record the mode /dross-quick runs in (pair unless --solo) and what it sets out to do",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			desc := args[0]
			if strings.TrimSpace(desc) == "" {
				return errors.New("a quick needs its stated description: the solo reviewer checks the change against it")
			}
			root, err := FindRoot()
			if err != nil {
				return err
			}
			repoDir := filepath.Dir(root)
			mode := "pair"
			if solo {
				mode = "solo"
				if err := requireReviewer(repoDir); err != nil {
					return err
				}
			}
			head, err := gitrun.Trim(repoDir, "rev-parse", "--verify", "--quiet", "HEAD")
			if err != nil && gitrun.ExitCode(err) != 1 {
				return fmt.Errorf("read HEAD: %w", err)
			}
			if err := gatestate.SaveQuick(repoDir, gatestate.Quick{Mode: mode, Description: desc, Head: head, At: time.Now().UTC()}); err != nil {
				return err
			}
			Printf("quick: %s (recorded)\n", mode)
			return nil
		},
	}
	c.Flags().BoolVar(&solo, "solo", false, "record solo mode: the quick is reviewed by the solo reviewer before its commit")
	return c
}

// quickEnd ends a /dross-quick and removes its record. When HEAD has moved
// past the begin's, the work commit landed and it also does the quick's
// bookkeeping (finishQuick). An abort committed nothing, so it only clears the
// record; no record at all is not an error and writes nothing, so every abort
// path can run it unconditionally and a repeat end cannot bump twice.
func quickEnd() *cobra.Command {
	return &cobra.Command{
		Use:   "end",
		Short: "End a /dross-quick: clear its record and, after its commit, bump internal and record the quick",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			repoDir := filepath.Dir(root)
			rec, err := gatestate.LoadQuick(repoDir)
			if err != nil {
				// Clear it all the same, as an abort path needs, but say so:
				// with no record to read there is nothing to bump against.
				if rmErr := gatestate.RemoveQuick(repoDir); rmErr != nil {
					return rmErr
				}
				return fmt.Errorf("quick record unreadable, removed it; nothing was bumped: %w", err)
			}
			if rec == nil {
				Printf("quick: ended (no record — nothing bumped)\n")
				return nil
			}
			head, err := gitrun.Trim(repoDir, "rev-parse", "--verify", "--quiet", "HEAD")
			if err != nil && gitrun.ExitCode(err) != 1 {
				return fmt.Errorf("read HEAD: %w", err)
			}
			if head == rec.Head {
				if err := gatestate.RemoveQuick(repoDir); err != nil {
					return err
				}
				Printf("quick: ended (no commit since begin — nothing bumped)\n")
				return nil
			}
			return finishQuick(root, rec, head)
		},
	}
}

// exitQuickBoardFailed is a quick end whose bookkeeping was written but whose
// board issue could not be opened: the version and the records stand, so it
// must not read as a usage error (1).
const exitQuickBoardFailed = 17

// finishQuick does a committed quick's bookkeeping in an order a failure can
// re-run: the changes record first, keyed by the version about to be written
// so a repeat overwrites it; then the version (project.toml before state.json,
// so a half-write re-derives the same number); then the quick record, whose
// removal is what makes a repeat end a no-op. The board comes last, and its
// failure keeps everything before it.
func finishQuick(root string, rec *gatestate.Quick, head string) error {
	repoDir := filepath.Dir(root)
	statePath := filepath.Join(root, state.File)
	s, err := state.Load(statePath)
	if err != nil {
		return err
	}
	next, internal, err := quickVersion(s.Version)
	if err != nil {
		return err
	}
	title := "quick: " + quickSummary(rec.Description)

	// A shipped phase's branch is about to be deleted, so its quick runs
	// standalone and records nothing there.
	changesLine := "changes: none (standalone)"
	switch {
	case s.CurrentPhase != "" && s.CurrentPhaseStatus != "shipped":
		files, err := quickFiles(repoDir, rec.Head)
		if err != nil {
			return err
		}
		id := "quick-" + strconv.Itoa(internal)
		// absorbs: dross changes record <phase> $QUICK_ID --files <touched-files (csv)> --commit $SHA --notes "quick: $ARGUMENTS"
		path := changes.FilePath(root, s.CurrentPhase)
		c, err := changes.Load(path, s.CurrentPhase)
		if err != nil {
			return err
		}
		c.Record(id, files, short(head), "quick: "+rec.Description, nil)
		if err := c.Save(path); err != nil {
			return err
		}
		changesLine = fmt.Sprintf("changes: recorded %s/%s (%d files, commit %s)", s.CurrentPhase, id, len(files), short(head))
	case s.CurrentPhase != "":
		changesLine = fmt.Sprintf("changes: none (phase %s is shipped — standalone)", s.CurrentPhase)
	}

	prev := s.Version
	// absorbs: dross state bump internal
	if err := writeVersion(root, s, next); err != nil {
		return err
	}
	s.Touch(fmt.Sprintf("bump internal %s → %s", prev, next))
	// absorbs: dross state touch "quick: <one-line summary of the task>"
	s.Touch(title)
	if err := s.Save(statePath); err != nil {
		return err
	}
	if err := gatestate.RemoveQuick(repoDir); err != nil {
		return err
	}
	Printf("quick: ended\nversion: %s\n%s\n", next, changesLine)
	// absorbs: dross issue quick $NEW_VERSION "quick: <one-line summary>"
	return quickBoard(next, title)
}

// quickVersion is version with its internal segment bumped, and that segment.
// It is bumpInternal rebuilt from the four numbers rather than sliced from the
// text: the result keys a changes record and a board link, and a value built
// from numbers carries none of state.json's text into what those records feed
// (the path taint scan's clearance_model). A segment with leading zeros comes
// back without them.
func quickVersion(version string) (string, int, error) {
	if err := validateVersion(version); err != nil {
		return "", 0, err
	}
	segs := make([]string, 4)
	internal := 0
	for i, p := range strings.Split(version, ".") {
		n, _ := strconv.Atoi(p)
		if i == 3 {
			n++
			internal = n
		}
		segs[i] = strconv.Itoa(n)
	}
	return strings.Join(segs, "."), internal, nil
}

// quickSummary is the one-line form of a quick's description: its first
// non-blank line.
func quickSummary(desc string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(desc), "\n")
	return strings.TrimSpace(line)
}

// quickFiles lists the paths the quick's commits touched, begin..HEAD, minus
// .dross/: bookkeeping is not the quick's work. A begin on an unborn branch
// has nothing to diff against, so every path in HEAD counts. -z keeps git from
// quoting a path; --no-renames lists both sides of a rename.
func quickFiles(repoDir, begin string) ([]string, error) {
	args := gitRefArgs("diff", []string{"--name-only", "--no-renames", "-z"}, begin, "HEAD")
	if begin == "" {
		args = gitRefArgs("ls-tree", []string{"-r", "--name-only", "-z"}, "HEAD")
	}
	names, err := gitrun.Raw(repoDir, args...)
	if err != nil {
		return nil, fmt.Errorf("list the quick's files: %w", err)
	}
	files := []string{}
	//dross:taint-cleared diff/ls-tree --name-only -z prints NUL-separated repo paths, and f is one of them
	for _, f := range strings.Split(names, "\x00") {
		if f != "" && !underDross(f) {
			files = append(files, f)
		}
	}
	return files, nil
}

// quickBoard opens the quick's board issue, keyed by its new version, on a
// `board:` line. Board sync off says so; a failure prints the retry and exits
// exitQuickBoardFailed.
func quickBoard(version, title string) error {
	ctx, enabled, err := openBoard()
	if err == nil && !enabled {
		Printf("board: off\n")
		return nil
	}
	var key string
	if err == nil {
		key, err = openQuickIssue(ctx, version, title)
	}
	if err != nil {
		Printf("board: FAILED: %v — the version and records are written; retry with `dross issue quick %s %s`\n",
			err, version, testlane.ShellQuote(title))
		return &ExitCodeError{Code: exitQuickBoardFailed, Err: fmt.Errorf("board quick issue: %w", err)}
	}
	Printf("board: quick %s -> board %s\n", version, key)
	return nil
}

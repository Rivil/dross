package cmd

// `dross test lane preview` — what would the gate run for these files?
//
// It is the gate with the spawn taken out. Every line it prints comes from
// lanePlan, the function `dross test --files` resolves through, so the two
// cannot disagree about what would run (c-1). What it adds is the half the gate
// has no room for: the gate refuses, exits and says nothing more, while preview
// NAMES each non-running outcome and keeps going (c-2).
//
// Locked preview_exit_status: it always exits 0 except on a real fault — an
// unreadable project.toml, an unknown `--lane` name. "No lane matches" and
// "every lane scoped to nothing" are printed findings, not a verdict. A verdict
// in the exit status would make a verb that spawns nothing wireable as a CI
// gate, duplicating `dross test`'s own refusal in a surface that never ran a
// test.

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
)

// previewReport is everything one preview learned. It is the single source for
// both renderings: the human one below, and the JSON one, which serializes this
// struct rather than re-walking the plan.
type previewReport struct {
	// FilesTaken is how many paths the preview resolved, so a bare invocation
	// can say what it picked up off the working tree.
	FilesTaken int `json:"files_taken"`
	// Lanes are the lanes the file set hit, in declaration order.
	Lanes []previewLaneReport `json:"lanes"`
	// Unmatched are in-tree paths no declared lane matches.
	Unmatched []string `json:"unmatched"`
	// OutOfTree are paths naming something outside the repo. A finding here,
	// exit 2 at the gate.
	OutOfTree []string `json:"out_of_tree"`
}

// previewLaneReport is one lane's answer.
type previewLaneReport struct {
	Name string `json:"name"`
	// Line is what the gate would spawn, empty when the lane would not spawn.
	Line string `json:"line"`
	// Selector is the derived arguments Line carries.
	Selector []string `json:"selector,omitempty"`
	// Dropped are this lane's paths that are not on disk.
	Dropped []string `json:"dropped,omitempty"`
	// ScopedToNothing marks a lane whose every selector path is gone.
	ScopedToNothing bool `json:"scoped_to_nothing"`
	// FenceErr is the lane's own project.toml problem, rendered.
	FenceErr string `json:"fence_err,omitempty"`
	// Consent is the lane's grant state — granted, stale, absent — reported and
	// never acted on.
	Consent string `json:"consent,omitempty"`
	// Locality is where the lane would run, or that it is unresolved.
	Locality string `json:"locality,omitempty"`
}

// testLanePreview is the subcommand.
//
// cobra.ArbitraryArgs and no lane positional, per locked preview_invocation:
// preview's question is "what would the gate do with these files", not "what
// would this lane do with files it does not match", so trailing paths JOIN the
// --files set and `--lane` narrows the answer. An argument read as a lane name
// would make `preview go` mean something the gate has no equivalent of.
func testLanePreview() *cobra.Command {
	var files []string
	var lane string
	c := &cobra.Command{
		Use:   "preview [paths...]",
		Short: "Show the command line each lane would spawn for a file set, without running anything",
		Long: "Resolves a file set against the declared lanes exactly as `dross test --files`\n" +
			"does and prints the line each hit lane would spawn — and nothing else happens:\n" +
			"no lane command, no prepare, no tree sync.\n\n" +
			"With no --files and no paths, the file set is the working tree's uncommitted\n" +
			"changes. Exits 0 for every finding, including \"no lane matches\": preview\n" +
			"describes, it does not judge.",
		Args: cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			return runLanePreview(files, args, lane, true)
		},
	}
	c.Flags().StringArrayVar(&files, "files", nil, "repo-relative path to resolve against the declared lanes (repeatable)")
	c.Flags().StringVar(&lane, "lane", "", "narrow the preview to one declared lane")
	return c
}

// runLanePreview is the verb's body, with the flags already parsed.
//
// probe is threaded through rather than read from a flag here so the locality
// task can turn it off without this function growing a second entry point.
func runLanePreview(files, args []string, lane string, probe bool) error {
	root, proj, err := loadProjectForLanes()
	if err != nil {
		return err
	}
	repoDir := filepath.Dir(root) // root is .dross; parent is the repo

	// Positionals JOIN --files rather than replacing them, so c-1's literal
	// `--files a.go b.go` takes both paths. Repeated rather than
	// comma-joined for the reason --files is: a comma is legal in a path.
	set := append(append([]string{}, files...), args...)
	bare := len(set) == 0
	if bare {
		// Locked bare_preview_default. Inferring the INPUT, never the lanes:
		// the milestone's declared-not-inferred non-goal is about lane
		// configuration, and "what would the gate run for the work I have
		// right now" is the question this verb was opened to ask.
		set, err = worktreeChangedFiles(repoDir)
		if err != nil {
			return err
		}
	}

	// A real fault, unlike every finding below: a misspelled lane name is an
	// argv mistake, and printing an empty preview for it would answer a
	// question the user did not ask.
	if lane != "" {
		if _, err := findLane(proj, lane); err != nil {
			return err
		}
	}

	plan := lanePlan(repoDir, proj, set)
	report := previewReport{
		FilesTaken: len(set),
		Unmatched:  plan.Unmatched,
		OutOfTree:  plan.OutOfTree,
	}
	for _, pl := range plan.Lanes {
		if lane != "" && pl.lane.Name != lane {
			continue
		}
		report.Lanes = append(report.Lanes, previewLaneReport{
			Name:            pl.lane.Name,
			Line:            pl.Line,
			Selector:        pl.Selector,
			Dropped:         pl.Dropped,
			ScopedToNothing: pl.ScopedToNothing,
			FenceErr:        renderedErr(pl.FenceErr),
		})
	}

	printPreview(report, bare)
	return nil
}

// renderedErr renders an error into the report's string field, empty for nil.
func renderedErr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// printPreview is the human rendering.
//
// Findings first, lanes second. The findings are the paths that got here
// without a lane to run in, and a reader scanning for "why is my file not
// covered" must not have to read past a wall of derived lines to find them.
func printPreview(r previewReport, bare bool) {
	if bare {
		// Said out loud, always — including for zero. A preview that silently
		// took nothing off a clean tree is indistinguishable from one that
		// took files and matched nothing.
		Printf("preview: %s from the working tree\n", countedFiles(r.FilesTaken))
	} else {
		Printf("preview: %s\n", countedFiles(r.FilesTaken))
	}

	// Every non-running outcome is NAMED with its reason (c-2). Emitting no
	// line for these is the whole failure this verb exists to fix: silence
	// reads identically to "nothing was wrong".
	for _, p := range r.OutOfTree {
		Printf("  %s — outside this repository, so no lane's globs can match it\n", p)
	}
	for _, p := range r.Unmatched {
		Printf("  %s — no declared lane matches it\n", p)
	}

	if len(r.Lanes) == 0 {
		Printf("no lane would run.\n")
		return
	}
	for _, l := range r.Lanes {
		printPreviewLane(l)
	}
}

// printPreviewLane renders one lane: its line, or the reason it has none.
func printPreviewLane(l previewLaneReport) {
	switch {
	case l.FenceErr != "":
		// A malformed lane is a FINDING, not a fault. The gate refuses the
		// whole run over it; preview names the problem and carries on to the
		// lanes that are fine, which are the ones the user can still act on.
		Printf("lane %s — would not run: %s\n", l.Name, l.FenceErr)
	case l.ScopedToNothing:
		Printf("lane %s — every path matching it is gone, so its selector scoped to nothing\n", l.Name)
	default:
		Printf("lane %s: %s\n", l.Name, l.Line)
	}
	for _, p := range l.Dropped {
		// Named rather than silently shortening the line, which is what the
		// gate does. The deleted path is the case a user is most likely to be
		// previewing, and a line that quietly got shorter explains nothing.
		Printf("  dropped %s — not on disk, so it was filtered before deriving\n", p)
	}
}

// countedFiles renders the size of the file set preview took.
func countedFiles(n int) string {
	if n == 1 {
		return "1 file"
	}
	return fmt.Sprintf("%d files", n)
}

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
// in the exit status would make a verb that runs no TEST wireable as a CI gate,
// duplicating `dross test`'s own refusal in a surface that measured nothing.
//
// Preview runs no test, but it is not free of subprocesses: --probe (the
// default) opens an ssh to the granted host through the run's own
// pickRemoteTarget. That reaches a machine, which is authority, so the probe is
// gated on the same per-lane grant the run is — an ungranted lane's probe is
// refused HERE, before the connection, and reported as an unresolved host
// carrying the reason. The exit status is untouched by that refusal, which is
// what keeps the locked decision above true.

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/project"
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
	// Host is the granted host this preview's locality is about, empty when
	// no host is granted at all.
	Host string `json:"host,omitempty"`
	// HostState is what the host told us — probed, unprobed, unresolved — or
	// `none`. It is a report-level fact rather than a per-lane one: every lane
	// in one preview was answered by the same probe, or by the same silence.
	HostState string `json:"host_state"`
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
	// Consent is the lane's grant state — granted, stale, absent. It is
	// reported per lane and never stops a lane being previewed; the one thing
	// it does decide is whether the host may be probed at all (probeBlockedBy).
	Consent string `json:"consent,omitempty"`
	// Locality is where the lane would run, or that it is unresolved.
	Locality string `json:"locality,omitempty"`
	// Fallback is the run's own fallback line — lane, binary, host and remedy
	// — set only when a probe PROVED the host lacks a tool.
	Fallback string `json:"fallback,omitempty"`
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
	var noProbe bool
	var asJSON bool
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
			return runLanePreview(files, args, lane, !noProbe, asJSON)
		},
	}
	c.Flags().StringArrayVar(&files, "files", nil, "repo-relative path to resolve against the declared lanes (repeatable)")
	c.Flags().StringVar(&lane, "lane", "", "narrow the preview to one declared lane")
	c.Flags().BoolVar(&noProbe, "no-probe", false, "do not contact the granted host; report its locality as unresolved")
	addPreviewJSONFlag(c, &asJSON)
	return c
}

// runLanePreview is the verb's body, with the flags already parsed.
//
// probe is threaded through rather than read from a flag here so the locality
// task can turn it off without this function growing a second entry point.
func runLanePreview(files, args []string, lane string, probe, asJSON bool) error {
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
	var shown []matchedLane
	for _, pl := range plan.Lanes {
		if lane != "" && pl.lane.Name != lane {
			continue
		}
		shown = append(shown, pl.matchedLane)
		report.Lanes = append(report.Lanes, previewLaneReport{
			Name:            pl.lane.Name,
			Line:            pl.Line,
			Selector:        pl.Selector,
			Dropped:         pl.Dropped,
			ScopedToNothing: pl.ScopedToNothing,
			FenceErr:        renderedErr(pl.FenceErr),
			// The ANNOTATION is still just a report — a lane's grant state
			// makes no line stop being previewed, which is what makes a line
			// that WOULD be refused visible before anyone runs it (c-3). What
			// the same state now also decides is whether the host may be
			// contacted at all; see probeBlockedBy in the locality file.
			Consent: previewConsent(root, repoDir, pl.lane).String(),
		})
	}

	loc, err := previewHost(root, repoDir, shown, probe)
	if err != nil {
		return err
	}
	report.Host, report.HostState = loc.Host, string(loc.State)
	for i := range report.Lanes {
		report.Lanes[i].Locality = loc.Lanes[i].Text
		report.Lanes[i].Fallback = loc.Lanes[i].Note
	}

	if asJSON {
		return emitPreviewJSON(report)
	}
	printPreview(report, bare)
	return nil
}

// previewConsent is the lane's grant state, and ONLY that.
//
// The error LaneConsented returns beside it is deliberately discarded. It is
// the RUN's refusal — the thing that stops an ungranted lane from spawning its
// tests — and preview spawns no tests, so rendering it would tell the user to
// go and run `dross trust` about a run that is not happening.
//
// Discarding the error is not discarding the authority. The state itself is
// read by probeBlockedBy below, which is what stops preview opening an ssh on
// behalf of a lane nobody granted; the refusal preview renders for that is its
// own, about the probe, in the host line.
func previewConsent(root, repoDir string, lane project.TestLane) ConsentState {
	state, _ := LaneConsented(root, repoDir, lane.Name, laneConsentLine(lane))
	return state
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
	// One host line for the whole report, because one probe (or one silence)
	// answered for every lane in it. Repeating the state per lane would read
	// as several independent answers.
	if r.Host != "" {
		Printf("host: %s (%s)\n", r.Host, r.HostState)
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
	if l.Consent != "" {
		Printf("  consent: %s\n", l.Consent)
	}
	if l.Locality != "" {
		Printf("  runs on: %s\n", l.Locality)
	}
	if l.Fallback != "" {
		Printf("  %s\n", l.Fallback)
	}
}

// countedFiles renders the size of the file set preview took.
func countedFiles(n int) string {
	if n == 1 {
		return "1 file"
	}
	return fmt.Sprintf("%d files", n)
}

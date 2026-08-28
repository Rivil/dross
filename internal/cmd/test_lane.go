package cmd

// `dross test lane` — the declaration surface for [[runtime.test_lane]].
//
// It exists because the alternative is telling the user to hand-edit
// project.toml, and every hand-edited lane is a lane that can be malformed in a
// way `dross validate` then has to report. The CLI writes the three required
// fields or refuses; nothing half-written reaches disk.
//
// `lane` is nested UNDER `dross test`, which deliberately shadows one spelling
// of the positional selector: cobra resolves args[0] against subcommands
// first, so `dross test lane` can no longer mean "run the suite against a
// package called lane". That is accepted knowingly — the verb reads far better
// nested than as a top-level `dross lane`, and a top-level `dross lane` remains
// the escape hatch if a real package named `lane` ever collides.

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/configenum"
	"github.com/Rivil/dross/internal/project"
)

// testLane registers the `lane` verb group.
func testLane() *cobra.Command {
	c := &cobra.Command{
		Use:   "lane",
		Short: "Declare, list, edit and remove [[runtime.test_lane]] entries",
		Long: "A test lane is a name, a list of match globs and the command that tests\n" +
			"the files they match. `dross test --files <paths>` runs only the lanes a\n" +
			"file set actually hits.\n\n" +
			"Each lane's command carries its own consent grant — `dross trust --lane\n" +
			"<name>` — so a lane that changes refuses only itself.",
	}
	c.AddCommand(testLaneAdd(), testLaneList(), testLaneEdit(), testLaneRemove())
	return c
}

// loadProjectForLanes is the shared open half of all three verbs.
func loadProjectForLanes() (root string, p *project.Project, err error) {
	root, err = FindRoot()
	if err != nil {
		return "", nil, err
	}
	p, err = project.Load(filepath.Join(root, project.File))
	if err != nil {
		return "", nil, err
	}
	return root, p, nil
}

// laneSelectorRefusal returns the CLI's refusal for a proposed lane's selector
// fields, or nil when they are usable.
//
// It reads the SAME laneSelectorProblems `dross validate` reports through, and
// that is the point rather than a convenience: the two surfaces cannot drift,
// so the CLI can never write a lane validate would then turn round and reject.
// The problems are quoted verbatim, project.toml prefix included, because they
// are exactly what the user would read on the next validate run.
func laneSelectorRefusal(name string, lane project.TestLane) error {
	problems := laneSelectorProblems(laneLabel(0, name), lane)
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("lane %q would not validate:\n  %s", name, strings.Join(problems, "\n  "))
}

func testLaneAdd() *cobra.Command {
	var match []string
	var command string
	var prepare string
	var selector string
	var emptyExit []int
	c := &cobra.Command{
		Use:   "add <name>",
		Short: "Declare a test lane",
		Long: "Writes one [[runtime.test_lane]] block. --match is repeatable; a trailing\n" +
			"slash means everything beneath a directory.\n\n" +
			"The lane starts UNGRANTED: declaring a command is not consenting to run\n" +
			"it. Follow with `dross trust --lane <name>`, which prints the line first.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := strings.TrimSpace(args[0])
			if name == "" {
				return fmt.Errorf("a lane needs a name — it is the key its consent grant is stored under")
			}
			// Every refusal below happens BEFORE the load-modify-save, so a
			// rejected add leaves project.toml byte-for-byte unchanged rather
			// than rewritten with the same content by a round trip.
			if len(match) == 0 {
				return fmt.Errorf("lane %q needs at least one --match glob — a lane matching no path can never be selected", name)
			}
			if strings.TrimSpace(command) == "" {
				return fmt.Errorf("lane %q needs a --command — a lane with no command line has nothing to run and nothing to consent to", name)
			}
			// Normalized before it is checked and before it is written, so
			// list, validate and the run site all read back the one spelling
			// the user's typing resolved to.
			proposed := project.TestLane{
				Name:    name,
				Match:   match,
				Command: strings.TrimSpace(command),
				// A whitespace-only prepare normalizes to absent HERE, before
				// the write, rather than being carried and read as empty
				// later: a `prepare = "   "` is non-empty for the consent
				// fingerprint and empty for every reader, so the one shape
				// that can disagree with itself never reaches disk.
				Prepare:   strings.TrimSpace(prepare),
				Selector:  configenum.Normalize(selector),
				EmptyExit: emptyExit,
			}
			if err := laneSelectorRefusal(name, proposed); err != nil {
				return err
			}
			root, p, err := loadProjectForLanes()
			if err != nil {
				return err
			}
			for _, lane := range p.Runtime.TestLane {
				if lane.Name == name {
					// Names key the consent store, so a duplicate would give
					// one grant authority over two different commands.
					return fmt.Errorf("lane %q already exists (matches %s, runs %q) — remove it first, or pick another name",
						name, strings.Join(lane.Match, " "), lane.Command)
				}
			}
			p.Runtime.TestLane = append(p.Runtime.TestLane, proposed)
			if err := p.Save(filepath.Join(root, project.File)); err != nil {
				return err
			}
			Printf("lane %q added: %s\n", name, strings.Join(match, " "))
			// The prepare is printed above the command, in the order they
			// run. Consent covers both lines, so a summary that showed only
			// the command would name less than the grant authorizes.
			if proposed.Prepare != "" {
				Printf("  prepare: %s\n", proposed.Prepare)
			}
			Printf("  %s\n\n", proposed.Command)
			Printf("It will not run until this machine trusts it:\n\n    dross trust --lane %s\n", name)
			return nil
		},
	}
	c.Flags().StringArrayVar(&match, "match", nil, "glob this lane matches (repeatable)")
	c.Flags().StringVar(&command, "command", "", "the command line this lane runs")
	c.Flags().StringVar(&prepare, "prepare", "", "optional bootstrap line run before this lane's command, on the same host; covered by the same consent grant")
	c.Flags().StringVar(&selector, "selector", "", "shape the matched paths take when appended to the command ("+configenum.SelectorStyles.List()+"); omitted runs the command untouched")
	c.Flags().IntSliceVar(&emptyExit, "empty-exit", nil, "exit code this lane's runner uses for \"collected no tests\" (repeatable); requires --selector")
	return c
}

func testLaneList() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Print the declared test lanes",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			_, p, err := loadProjectForLanes()
			if err != nil {
				return err
			}
			if len(p.Runtime.TestLane) == 0 {
				// Exit 0. A repo with no lanes is the supported default, not
				// a misconfiguration — `dross test` runs runtime.test_command
				// exactly as it always did.
				Print("no test lanes configured — `dross test` runs runtime.test_command.")
				Print("Declare one with `dross test lane add <name> --match <glob> --command \"<cmd>\"`.")
				return nil
			}
			for _, lane := range p.Runtime.TestLane {
				Printf("%s\n", lane.Name)
				Printf("  match:   %s\n", strings.Join(lane.Match, " "))
				// The command is printed, always. It is the value consent
				// binds to, so a listing that hid it would leave the user
				// unable to see what they are being asked to trust.
				Printf("  command: %s\n", lane.Command)
				// Printed only when declared. Every field below is opt-in,
				// and a `selector: -` on every pre-existing lane would read
				// as something the user is expected to go and set.
				if lane.Prepare != "" {
					Printf("  prepare: %s\n", lane.Prepare)
				}
				if lane.Selector != "" {
					Printf("  selector: %s\n", lane.Selector)
				}
				if len(lane.EmptyExit) > 0 {
					Printf("  empty-exit: %s\n", joinInts(lane.EmptyExit))
				}
			}
			return nil
		},
	}
}

// testLaneEdit is `dross test lane edit <name> --prepare "<cmd>"`: the ONE
// field a lane can change in place.
//
// It supersedes lane-selector-translation's lane_edit_surface decision for
// prepare alone. That lock's reasoning was that remove-then-re-add is an
// adequate workaround, and prepare is where it stops being one: removing a lane
// drops its consent grant, so the workaround silently discards trust the user
// granted and re-adds the lane as if it had never been read. Match, command,
// selector and empty_exit keep the remove-then-re-add path.
//
// The grant is KEPT, deliberately, even though the fingerprint no longer
// matches. Revoking would report a lane the user has trusted before as one they
// never have — collapsing STALE into ABSENT, which is the distinction the
// consent ladder exists to preserve and the one that tells a rewrite apart from
// a first run.
func testLaneEdit() *cobra.Command {
	var prepare string
	c := &cobra.Command{
		Use:   "edit <name> --prepare \"<cmd>\"",
		Short: "Set or clear a declared lane's prepare line, keeping its grant",
		Long: "Changes a lane's prepare in place, leaving its match globs, command and\n" +
			"position in project.toml exactly as they were.\n\n" +
			"The lane's consent grant is kept but goes STALE: `dross trust --lane\n" +
			"<name>` will say the line CHANGED rather than that it was never trusted.\n\n" +
			"Only prepare is editable. Changing a lane's match, command, selector or\n" +
			"empty_exit is still remove-then-re-add.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Read from Changed, not from the value. An omitted --prepare and
			// an explicitly empty one are different requests — "leave it
			// alone" and "clear it" — and a check on emptiness alone would
			// collapse them into one clearing behaviour, so `lane edit go`
			// with no flag at all would silently drop an existing prepare.
			if !cmd.Flags().Changed("prepare") {
				return fmt.Errorf("nothing to change: `dross test lane edit %s` needs --prepare.\n\n"+
					"Pass a command to set one, or --prepare \"\" to clear it. Every other lane\n"+
					"field is changed by removing the lane and re-adding it", args[0])
			}
			name := args[0]
			root, p, err := loadProjectForLanes()
			if err != nil {
				return err
			}
			idx := -1
			var names []string
			for i, lane := range p.Runtime.TestLane {
				names = append(names, lane.Name)
				if lane.Name == name {
					idx = i
				}
			}
			if idx < 0 {
				// Refused BEFORE the save, so an unknown name leaves
				// project.toml byte-for-byte unchanged rather than rewritten
				// with the same content by a round trip.
				if len(names) == 0 {
					return fmt.Errorf("no lane %q: this repo declares none", name)
				}
				return fmt.Errorf("no lane %q; declared: %s", name, strings.Join(names, ", "))
			}
			// Mutated IN PLACE rather than removed and appended: the lane
			// keeps its position, so an edit does not reorder the document and
			// a diff shows the one line that changed.
			before := laneConsentLine(p.Runtime.TestLane[idx])
			p.Runtime.TestLane[idx].Prepare = strings.TrimSpace(prepare)
			lane := p.Runtime.TestLane[idx]
			if err := p.Save(filepath.Join(root, project.File)); err != nil {
				return err
			}
			if lane.Prepare == "" {
				Printf("lane %q now declares no prepare.\n", name)
			} else {
				Printf("lane %q prepare: %s\n", name, lane.Prepare)
			}
			// Only when the consent line actually moved. A trust instruction
			// printed on a no-op re-set teaches the user that the message
			// carries no information, and the next real one gets skimmed.
			if laneConsentLine(lane) != before {
				Printf("\nIts grant is now stale — the lane will refuse until you re-read it:\n\n    dross trust --lane %s\n", name)
			}
			return nil
		},
	}
	c.Flags().StringVar(&prepare, "prepare", "", "the lane's bootstrap line; pass \"\" to clear it")
	return c
}

func testLaneRemove() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Delete a test lane and drop its consent grant",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			root, p, err := loadProjectForLanes()
			if err != nil {
				return err
			}
			kept := make([]project.TestLane, 0, len(p.Runtime.TestLane))
			found := false
			for _, lane := range p.Runtime.TestLane {
				if lane.Name == name {
					found = true
					continue
				}
				kept = append(kept, lane)
			}
			if !found {
				// An error, not a silent no-op: "removed" reported for a name
				// that was never there leaves the user believing a lane is
				// gone while it still runs.
				var names []string
				for _, lane := range p.Runtime.TestLane {
					names = append(names, lane.Name)
				}
				if len(names) == 0 {
					return fmt.Errorf("no lane %q: this repo declares none", name)
				}
				return fmt.Errorf("no lane %q; declared: %s", name, strings.Join(names, ", "))
			}
			if len(kept) == 0 {
				// nil rather than an empty slice so omitempty leaves the key
				// out entirely and a repo returns to its lane-less shape.
				kept = nil
			}
			p.Runtime.TestLane = kept
			if err := p.Save(filepath.Join(root, project.File)); err != nil {
				return err
			}
			// The grant goes with the lane. Left behind, it would sit in
			// local.toml keyed by a name nothing declares — until someone
			// re-added a lane under that name, which would then start
			// GRANTED, authorized by a fingerprint issued for whatever the
			// deleted lane used to run.
			if err := RevokeLaneConsent(root, name); err != nil {
				return err
			}
			Printf("lane %q removed; its consent grant was dropped.\n", name)
			return nil
		},
	}
}

// joinInts renders the empty-exit codes for the listing.
func joinInts(v []int) string {
	out := make([]string, len(v))
	for i, n := range v {
		out[i] = strconv.Itoa(n)
	}
	return strings.Join(out, " ")
}

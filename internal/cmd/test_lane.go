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
	"github.com/Rivil/dross/internal/testlane"
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
			"<name>` — so a lane that changes refuses only itself. A lane's optional\n" +
			"install line is granted apart from it, with `dross trust --lane-install`.",
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

// laneToolchainRefusal returns the CLI's refusal for a proposed lane's
// toolchain override, or nil when every entry is usable.
//
// Quoted through the SAME laneToolchainProblems `dross validate` reports, for
// the reason laneSelectorRefusal is: the CLI must never be able to write a lane
// validate would then reject, and a second copy of the rules here would drift
// until it could.
func laneToolchainRefusal(name string, lane project.TestLane) error {
	problems := laneToolchainProblems(laneLabel(0, name), lane)
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("lane %q would not validate:\n  %s", name, strings.Join(problems, "\n  "))
}

// clearsToolchain reports whether a --toolchain argv means "go back to
// derived".
//
// `--toolchain ""` is the clear gesture, spelled exactly as `--prepare ""` is,
// and only a lone blank counts. A blank BESIDE a real entry is a typo rather
// than a request — clearing on it would silently discard the entry the user did
// mean, so it falls through to laneToolchainProblems and is refused by name.
func clearsToolchain(v []string) bool {
	return len(v) == 0 || (len(v) == 1 && strings.TrimSpace(v[0]) == "")
}

// laneToolchainLine renders one lane's EFFECTIVE toolchain for the listing,
// with where it came from.
//
// Printed for every lane rather than only for overridden ones (c-7). The
// derived list is what the run actually probes, and a listing that showed only
// declared overrides would leave the normal case — every lane written before
// this phase — with no way to see its probe set short of reading project.toml
// and re-deriving it by hand, which is the thing this flag exists to replace.
func laneToolchainLine(lane project.TestLane) string {
	origin := "derived"
	if len(lane.Toolchain) > 0 {
		origin = "overridden"
	}
	tools := testlane.Toolchain(lane.Command, lane.Prepare, lane.Toolchain)
	return fmt.Sprintf("%s (%s)", strings.Join(tools, " "), origin)
}

func testLaneAdd() *cobra.Command {
	var match []string
	var command string
	var prepare string
	var selector string
	var toolchain []string
	var install string
	var emptyExit []int
	c := &cobra.Command{
		Use:   "add <name>",
		Short: "Declare a test lane",
		Long: "Writes one [[runtime.test_lane]] block. --match is repeatable; a trailing\n" +
			"slash means everything beneath a directory.\n\n" +
			"The lane starts UNGRANTED: declaring a command is not consenting to run\n" +
			"it. Follow with `dross trust --lane <name>`, which prints the line first.\n\n" +
			"--install declares how this lane's toolchain is installed, replacing dross's\n" +
			"built-in recipe. It carries its OWN grant — `dross trust --lane-install\n" +
			"<name>` — because installing changes a machine and running a suite does not.",
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
				Prepare:  strings.TrimSpace(prepare),
				Selector: configenum.Normalize(selector),
				// Verbatim, not trimmed and not filtered. A blank entry here is a
				// mistake with no other reading — there is nothing to clear on a
				// lane being declared — and dropping it silently would write an
				// override the user cannot see they got wrong.
				Toolchain: toolchain,
				// Normalized to absent exactly as Prepare is, and before the
				// write for the same reason: an `install = "   "` is non-empty
				// for its consent fingerprint and empty for every reader, so
				// the one shape that can disagree with itself never reaches
				// disk from this verb.
				Install:   strings.TrimSpace(install),
				EmptyExit: emptyExit,
			}
			if err := laneSelectorRefusal(name, proposed); err != nil {
				return err
			}
			if err := laneToolchainRefusal(name, proposed); err != nil {
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
			Printf("  %s\n", proposed.Command)
			// The probe set, always — derived or not. It is what decides which
			// machine the lane runs on, and the moment to see it is the moment
			// the lane is declared.
			Printf("  toolchain: %s\n", laneToolchainLine(proposed))
			// Printed only when declared, unlike the toolchain above it: a
			// lane always HAS an effective toolchain, and an `install: -` on
			// every lane would read as a field the user is expected to set.
			if proposed.Install != "" {
				Printf("  install: %s\n", proposed.Install)
			}
			Printf("\n")
			Printf("It will not run until this machine trusts it:\n\n    dross trust --lane %s\n", name)
			// A SECOND grant, named separately. The command grant does not
			// cover the install line, so a user told only about `--lane` would
			// declare an install line and find it refused with nothing having
			// pointed at the verb that grants it.
			if proposed.Install != "" {
				Printf("\nIts install line is granted separately — installing is not running:\n\n    dross trust --lane-install %s\n", name)
			}
			return nil
		},
	}
	c.Flags().StringArrayVar(&match, "match", nil, "glob this lane matches (repeatable)")
	c.Flags().StringVar(&command, "command", "", "the command line this lane runs")
	c.Flags().StringVar(&prepare, "prepare", "", "optional bootstrap line run before this lane's command, on the same host; covered by the same consent grant")
	c.Flags().StringVar(&selector, "selector", "", "shape the matched paths take when appended to the command ("+configenum.SelectorStyles.List()+"); omitted runs the command untouched")
	c.Flags().StringArrayVar(&toolchain, "toolchain", nil, "binary this lane needs on the host that runs it (repeatable); omitted derives it from the first token of --command and --prepare")
	c.Flags().StringVar(&install, "install", "", "optional line that installs this lane's toolchain; replaces dross's built-in recipe and carries its own consent grant, separate from the lane's")
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
				// Unconditional, unlike every opt-in field around it: a lane
				// ALWAYS has an effective toolchain, because an omitted override
				// derives one. Hiding it for the un-overridden case would hide it
				// for exactly the lanes whose probe set nobody has looked at.
				Printf("  toolchain: %s\n", laneToolchainLine(lane))
				if lane.Install != "" {
					Printf("  install: %s\n", lane.Install)
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

// testLaneEdit is `dross test lane edit <name>`: the fields a lane can change
// in place.
//
// It supersedes lane-selector-translation's lane_edit_surface decision for
// prepare and toolchain. That lock's reasoning was that remove-then-re-add is
// an adequate workaround, and these two are where it stops being one: removing
// a lane drops its consent grant, so the workaround silently discards trust the
// user granted and re-adds the lane as if it had never been read. Match,
// command, selector and empty_exit keep the remove-then-re-add path.
//
// Toolchain is the sharper case of the two: it is not part of the consent line
// at all — it names binaries, not a command — so remove-then-re-add would drop
// a grant in order to change a field the grant does not cover.
//
// The grant is KEPT, deliberately, even though the fingerprint no longer
// matches. Revoking would report a lane the user has trusted before as one they
// never have — collapsing STALE into ABSENT, which is the distinction the
// consent ladder exists to preserve and the one that tells a rewrite apart from
// a first run.
func testLaneEdit() *cobra.Command {
	var prepare string
	var toolchain []string
	var install string
	c := &cobra.Command{
		Use:   "edit <name>",
		Short: "Set or clear a declared lane's prepare line, toolchain or install line, keeping its grant",
		Long: "Changes a lane's --prepare, --toolchain or --install in place, leaving its\n" +
			"match globs, command and position in project.toml exactly as they were.\n\n" +
			"--prepare changes the consent line, so the lane's grant is kept but goes\n" +
			"STALE: `dross trust --lane <name>` will say the line CHANGED rather than\n" +
			"that it was never trusted. --toolchain is not part of the consent line —\n" +
			"it names binaries, not a command — so changing it leaves the grant alone.\n" +
			"--install is not part of it either: it carries its OWN grant, so adding\n" +
			"one to a lane that already runs green never refuses its test runs.\n\n" +
			"--toolchain is repeatable and REPLACES the derived list wholesale; pass\n" +
			"--toolchain \"\" to go back to deriving it, and --install \"\" to drop the\n" +
			"install line. Changing a lane's match, command, selector or empty_exit is\n" +
			"still remove-then-re-add.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Read from Changed, not from the value. An omitted --prepare and
			// an explicitly empty one are different requests — "leave it
			// alone" and "clear it" — and a check on emptiness alone would
			// collapse them into one clearing behaviour, so `lane edit go`
			// with no flag at all would silently drop an existing prepare.
			setsPrepare := cmd.Flags().Changed("prepare")
			setsToolchain := cmd.Flags().Changed("toolchain")
			setsInstall := cmd.Flags().Changed("install")
			if !setsPrepare && !setsToolchain && !setsInstall {
				return fmt.Errorf("nothing to change: `dross test lane edit %s` needs --prepare, --toolchain or --install.\n\n"+
					"Pass a command to set a prepare, or --prepare \"\" to clear it; pass one or\n"+
					"more --toolchain binaries to override the derived list, or --toolchain \"\"\n"+
					"to go back to deriving it; pass an --install line to override dross's\n"+
					"built-in install recipe, or --install \"\" to drop it. Every other lane\n"+
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
			// Each field is written only when its OWN flag was passed. An
			// unconditional write would let `--toolchain go` clear a prepare the
			// user never mentioned — the same omitted-means-clear collapse the
			// Changed guard above exists to prevent, one flag deeper.
			proposed := p.Runtime.TestLane[idx]
			if setsPrepare {
				proposed.Prepare = strings.TrimSpace(prepare)
			}
			if setsToolchain {
				if clearsToolchain(toolchain) {
					// nil rather than an empty slice, so omitempty leaves the key
					// out and the lane returns to the derived shape it had before
					// the override was ever written.
					proposed.Toolchain = nil
				} else {
					proposed.Toolchain = toolchain
				}
			}
			if setsInstall {
				// Trimmed to absent on the `lane add` precedent, so both verbs
				// write the one spelling for the same input — two that
				// disagreed would put a lane on disk validate then rejects.
				proposed.Install = strings.TrimSpace(install)
			}
			// Checked BEFORE the mutation lands, so a refused edit leaves
			// project.toml byte-for-byte unchanged — and checked through the same
			// problems validate reports, so the CLI cannot write a lane validate
			// would then reject.
			if err := laneToolchainRefusal(name, proposed); err != nil {
				return err
			}
			p.Runtime.TestLane[idx] = proposed
			lane := p.Runtime.TestLane[idx]
			if err := p.Save(filepath.Join(root, project.File)); err != nil {
				return err
			}
			if setsPrepare {
				if lane.Prepare == "" {
					Printf("lane %q now declares no prepare.\n", name)
				} else {
					Printf("lane %q prepare: %s\n", name, lane.Prepare)
				}
			}
			if setsToolchain {
				Printf("lane %q toolchain: %s\n", name, laneToolchainLine(lane))
			}
			if setsInstall {
				if lane.Install == "" {
					Printf("lane %q now declares no install line.\n", name)
				} else {
					Printf("lane %q install: %s\n", name, lane.Install)
					// Named here and not below: the install grant is its own,
					// so this instruction is correct whether the line is new or
					// rewritten, and it must not be confused with the `--lane`
					// staleness message the command grant prints.
					Printf("\nIts install grant is separate and now needs reading:\n\n    dross trust --lane-install %s\n", name)
				}
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
	c.Flags().StringArrayVar(&toolchain, "toolchain", nil, "binary this lane needs on the host that runs it (repeatable); replaces the derived list, pass \"\" to go back to deriving it")
	c.Flags().StringVar(&install, "install", "", "the line that installs this lane's toolchain; replaces dross's built-in recipe, pass \"\" to drop it")
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
			Printf("lane %q removed; its consent grants — command and install — were dropped.\n", name)
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

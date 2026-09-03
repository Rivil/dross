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
	c.AddCommand(testLaneAdd(), testLaneList(), testLanePreview(), testLaneEdit(), testLaneRemove(), testLaneInstall())
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

// laneToolchainRefusal returns the CLI's refusal for a proposed lane's
// toolchain override, or nil when every entry is usable.
//
// Quoted through the SAME laneToolchainProblems `dross validate` reports, for
// the reason laneRefusal is: the CLI must never be able to write a lane
// validate would then reject, and a second copy of the rules here would drift
// until it could.
func laneToolchainRefusal(name string, lane project.TestLane) error {
	problems := laneToolchainProblems(laneLabel(0, name), lane)
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("lane %q would not validate:\n  %s", name, strings.Join(problems, "\n  "))
}

// clearsList reports whether a repeatable string flag's argv means "clear this
// field".
//
// `--toolchain ""` is the clear gesture, spelled exactly as `--prepare ""` is,
// and only a lone blank counts. A blank BESIDE a real entry is a typo rather
// than a request — clearing on it would silently discard the entry the user did
// mean, so it falls through to the refusal gate and is named there.
//
// Shared by every repeatable field the edit verb can write — toolchain, match
// and empty_exit — so one gesture means one thing across the surface rather
// than each flag inventing its own spelling of "unset".
func clearsList(v []string) bool {
	return len(v) == 0 || (len(v) == 1 && strings.TrimSpace(v[0]) == "")
}

// parseEmptyExit turns an --empty-exit argv into the codes the lane declares.
//
// Taken as strings rather than through cobra's IntSliceVar so the field has a
// CLEAR gesture at all: an int slice cannot express `--empty-exit ""`, and
// without it there would be no way to drop a code short of removing the lane —
// which is the remove-then-re-add this verb exists to end.
//
// A non-numeric entry is refused BY NAME. Dropping it silently would write a
// lane missing the very code the user was declaring, and reporting only a count
// would leave them re-reading their own argv to find which one.
func parseEmptyExit(name string, v []string) ([]int, error) {
	if clearsList(v) {
		return nil, nil
	}
	out := make([]int, 0, len(v))
	for _, raw := range v {
		n, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf("lane %q: --empty-exit %q is not a number — the field lists a runner's exit codes, so every entry must be one", name, raw)
		}
		out = append(out, n)
	}
	return out, nil
}

// laneRefusal returns the CLI's refusal for a WHOLE proposed lane, or nil when
// every field is usable.
//
// It reads the same laneProblems `dross validate` reports through, which is the
// documented invariant rather than a convenience: the CLI can never write a
// lane validate would then turn round and reject. laneToolchainRefusal covers
// one field group and was enough while those were the only fields the CLI could
// write; once match, command and the globs are editable, the faults they can
// introduce — an empty match list, a blank command, a glob that does not
// compile — are reported only here.
//
// It validates a SYNTHETIC one-lane project holding ONLY the proposed lane.
// Feeding it the whole modified project would refuse an edit because some OTHER
// hand-edited lane is broken — and this verb is the tool for fixing exactly
// that lane. Duplicate-name detection therefore stays where it is, on the add
// path, since a one-lane project can never collide with itself.
func laneRefusal(name string, lane project.TestLane) error {
	synthetic := &project.Project{}
	synthetic.Runtime.TestLane = []project.TestLane{lane}
	problems := laneProblems(synthetic)
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("lane %q would not validate:\n  %s", name, strings.Join(problems, "\n  "))
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
	var selectorTemplate string
	var selectorJoin string
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
				// Verbatim, never trimmed: the template is fenced by consent
				// alone (the locked template_fence decision), so its own text
				// reaches the line exactly as typed — a trailing space inside
				// a regex is the user's to mean, and normalizing it away here
				// would silently spawn a line other than the one granted.
				SelectorTemplate: selectorTemplate,
				SelectorJoin:     selectorJoin,
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
			// The WHOLE lane, through the same problems validate reports, so
			// this verb cannot write a lane the next `dross validate` rejects
			// — a glob that does not compile among them, which the two
			// field-group refusals below never covered.
			if err := laneRefusal(name, proposed); err != nil {
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
			// Echoed at declaration time, in the same shape `lane list` will
			// print it back: the summary and the listing are the same lines
			// because they are the same renderer.
			printLaneTemplate(proposed)
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
			printLaneWholeTreeWarning(name, proposed)
			return nil
		},
	}
	c.Flags().StringArrayVar(&match, "match", nil, "glob this lane matches (repeatable)")
	c.Flags().StringVar(&command, "command", "", "the command line this lane runs")
	c.Flags().StringVar(&prepare, "prepare", "", "optional bootstrap line run before this lane's command, on the same host; covered by the same consent grant")
	c.Flags().StringVar(&selector, "selector", "", "shape the matched paths take when appended to the command ("+configenum.SelectorStyles.List()+"); omitted runs the command untouched")
	c.Flags().StringVar(&selectorTemplate, "selector-template", "", "where the matched paths land on the command line, for a runner the selector styles cannot shape; {path} repeats the template per path, {paths} substitutes them into one instance. Requires --selector")
	c.Flags().StringVar(&selectorJoin, "selector-join", "", "join a {paths} expansion into ONE argument with this separator (\"|\" for `ctest -R`); omitted expands to separate arguments")
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
				// Straight after the selector, because the three fields are one
				// answer: the style decides what the derived arguments ARE, the
				// template decides where they land and the join decides how they
				// are separated. A listing that showed only the style would let a
				// reader reconstruct the wrong line from it.
				//
				// The same renderer `lane add` and `lane edit` use, called rather
				// than copied — two renderers would be two answers to "what does
				// this lane's template look like", and the one a user reads back
				// would be the one nothing else validates.
				printLaneTemplate(lane)
				if len(lane.EmptyExit) > 0 {
					Printf("  empty-exit: %s\n", joinInts(lane.EmptyExit))
				}
			}
			return nil
		},
	}
}

// testLaneEdit is `dross test lane edit <name>`: every field a lane can change
// in place.
//
// It supersedes lane-selector-translation's lane_edit_surface decision
// outright. That lock's reasoning was that remove-then-re-add is an adequate
// workaround for the rest of a lane's fields, and it is not one: removing a
// lane DROPS its consent grant, so the workaround discards trust the user
// granted and re-adds the lane as if it had never been read — collapsing STALE
// into ABSENT, which is the distinction the consent ladder exists to preserve
// and the one that tells a rewrite apart from a first run.
//
// So match, command, selector, empty_exit, selector_template and selector_join
// join prepare, toolchain and install here. Nothing about a lane is
// remove-then-re-add any more except its NAME, which keys the grant store and
// therefore cannot move without the grant moving with it.
//
// Toolchain and install remain the two fields OUTSIDE the command grant —
// toolchain names binaries rather than a command, and install carries its own
// grant — so changing either leaves the lane's test runs granted.
//
// The grant is KEPT whenever it goes stale, deliberately, even though the
// fingerprint no longer matches: revoking would report a lane the user has
// trusted before as one they never have.
func testLaneEdit() *cobra.Command {
	var match []string
	var command string
	var prepare string
	var selector string
	var selectorTemplate string
	var selectorJoin string
	var toolchain []string
	var install string
	var emptyExit []string
	c := &cobra.Command{
		Use:   "edit <name>",
		Short: "Change a declared lane's fields in place, keeping its grant",
		Long: "Changes a lane's --match, --command, --prepare, --selector,\n" +
			"--selector-template, --selector-join, --empty-exit, --toolchain or\n" +
			"--install in place, leaving its name and its position in project.toml\n" +
			"exactly as they were. Only the lane's NAME is still remove-then-re-add:\n" +
			"it keys the consent store, so it cannot move without the grant moving.\n\n" +
			"--match, --command, --prepare, --selector-template and --selector-join\n" +
			"change the consent line, so the lane's grant is kept but goes STALE:\n" +
			"`dross trust --lane <name>` will say the line CHANGED rather than that it\n" +
			"was never trusted. --toolchain is not part of the consent line — it names\n" +
			"binaries, not a command — so changing it leaves the grant alone.\n" +
			"--install is not part of it either: it carries its OWN grant, so adding\n" +
			"one to a lane that already runs green never refuses its test runs.\n\n" +
			"--match, --toolchain and --empty-exit are repeatable and REPLACE the\n" +
			"declared list wholesale; pass \"\" to any of them to clear it, and\n" +
			"--prepare \"\", --selector \"\" or --install \"\" to drop those.\n\n" +
			"An edit that would leave the lane malformed is refused before anything is\n" +
			"written, through the same rules `dross validate` reports.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Read from Changed, not from the value. An omitted --prepare and
			// an explicitly empty one are different requests — "leave it
			// alone" and "clear it" — and a check on emptiness alone would
			// collapse them into one clearing behaviour, so `lane edit go`
			// with no flag at all would silently drop an existing prepare.
			setsMatch := cmd.Flags().Changed("match")
			setsCommand := cmd.Flags().Changed("command")
			setsPrepare := cmd.Flags().Changed("prepare")
			setsSelector := cmd.Flags().Changed("selector")
			setsTemplate := cmd.Flags().Changed("selector-template")
			setsJoin := cmd.Flags().Changed("selector-join")
			setsEmptyExit := cmd.Flags().Changed("empty-exit")
			setsToolchain := cmd.Flags().Changed("toolchain")
			setsInstall := cmd.Flags().Changed("install")
			if !setsMatch && !setsCommand && !setsPrepare && !setsSelector &&
				!setsTemplate && !setsJoin && !setsEmptyExit && !setsToolchain && !setsInstall {
				return fmt.Errorf("nothing to change: `dross test lane edit %s` needs one of --match, --command,\n"+
					"--prepare, --selector, --selector-template, --selector-join, --empty-exit,\n"+
					"--toolchain or --install.\n\n"+
					"Pass \"\" to any of them to clear the field. Only the lane's NAME is changed\n"+
					"by removing the lane and re-adding it — it keys the consent grant", args[0])
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
			if setsMatch {
				if clearsList(match) {
					// Cleared to nil rather than written as a lone blank glob.
					// The two are different lanes — one matches nothing and is
					// refused below by name, the other matches the empty
					// string and validates clean while never selecting — and
					// the refusal is the honest reading of `--match ""`.
					proposed.Match = nil
				} else {
					proposed.Match = match
				}
			}
			if setsCommand {
				proposed.Command = strings.TrimSpace(command)
			}
			if setsPrepare {
				proposed.Prepare = strings.TrimSpace(prepare)
			}
			if setsSelector {
				proposed.Selector = configenum.Normalize(selector)
			}
			if setsTemplate {
				// Verbatim on the `lane add` precedent: the template is fenced
				// by consent alone, so its own text reaches the line exactly as
				// typed. Two verbs that normalized differently would put a lane
				// on disk whose spawned line depended on which one wrote it.
				proposed.SelectorTemplate = selectorTemplate
			}
			if setsJoin {
				proposed.SelectorJoin = selectorJoin
			}
			if setsEmptyExit {
				codes, err := parseEmptyExit(name, emptyExit)
				if err != nil {
					return err
				}
				proposed.EmptyExit = codes
			}
			if setsToolchain {
				if clearsList(toolchain) {
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
			// project.toml byte-for-byte unchanged — and checked through the
			// WHOLE lane's problems, not one field group's, because the newly
			// editable fields can introduce faults only laneProblems reports:
			// an empty match list, a blank command, a glob that does not
			// compile.
			if err := laneRefusal(name, proposed); err != nil {
				return err
			}
			if err := laneToolchainRefusal(name, proposed); err != nil {
				return err
			}
			p.Runtime.TestLane[idx] = proposed
			lane := p.Runtime.TestLane[idx]
			if err := p.Save(filepath.Join(root, project.File)); err != nil {
				return err
			}
			if setsMatch {
				Printf("lane %q match: %s\n", name, strings.Join(lane.Match, " "))
			}
			if setsCommand {
				Printf("lane %q command: %s\n", name, lane.Command)
			}
			if setsPrepare {
				if lane.Prepare == "" {
					Printf("lane %q now declares no prepare.\n", name)
				} else {
					Printf("lane %q prepare: %s\n", name, lane.Prepare)
				}
			}
			if setsSelector {
				if lane.Selector == "" {
					Printf("lane %q now declares no selector.\n", name)
				} else {
					Printf("lane %q selector: %s\n", name, lane.Selector)
				}
			}
			if setsTemplate || setsJoin {
				// Echoed for the reason `lane add` echoes it: `lane list` does
				// not render a template, so this is the one place a declared
				// one is visible without reading project.toml back.
				if lane.SelectorTemplate == "" && lane.SelectorJoin == "" {
					Printf("lane %q now declares no selector template.\n", name)
				} else {
					Printf("lane %q:\n", name)
					printLaneTemplate(lane)
				}
			}
			if setsEmptyExit {
				if len(lane.EmptyExit) == 0 {
					Printf("lane %q now declares no empty-exit codes.\n", name)
				} else {
					Printf("lane %q empty-exit: %s\n", name, joinInts(lane.EmptyExit))
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
			printLaneWholeTreeWarning(name, lane)
			return nil
		},
	}
	c.Flags().StringArrayVar(&match, "match", nil, "glob this lane matches (repeatable); replaces the declared list, pass \"\" to clear it")
	c.Flags().StringVar(&command, "command", "", "the command line this lane runs")
	c.Flags().StringVar(&prepare, "prepare", "", "the lane's bootstrap line; pass \"\" to clear it")
	c.Flags().StringVar(&selector, "selector", "", "shape the matched paths take ("+configenum.SelectorStyles.List()+"); pass \"\" to clear it")
	c.Flags().StringVar(&selectorTemplate, "selector-template", "", "where the matched paths land on the command line; {path} repeats the template per path, {paths} substitutes them into one instance. Pass \"\" to clear it")
	c.Flags().StringVar(&selectorJoin, "selector-join", "", "join a {paths} expansion into ONE argument with this separator; pass \"\" to clear it")
	c.Flags().StringArrayVar(&emptyExit, "empty-exit", nil, "exit code this lane's runner uses for \"collected no tests\" (repeatable); replaces the declared list, pass \"\" to clear it")
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

// printLaneWholeTreeWarning prints the c-4 whole-tree warning at declaration
// time, and nothing when there is none.
//
// It reads the SAME laneWholeTreeWarning validate reports through, so the two
// surfaces cannot drift into warning about different things. The lane is
// already written by the time this runs: it is a warning, not a refusal, and
// declaring it is a legitimate thing to do.
func printLaneWholeTreeWarning(name string, lane project.TestLane) {
	if w := laneWholeTreeWarning(laneLabel(0, name), lane); w != "" {
		Printf("\n⚠ %s\n", w)
	}
}

// printLaneTemplate echoes a lane's declared selector scoping, and prints
// nothing for a lane that declares none.
//
// Shared by `lane add`, `lane edit` and `lane list`, so none of them can echo a
// field another hides. One renderer is the point: a listing that reconstructed
// the template itself would be a second answer to "what does this lane look
// like", and the two would diverge on the first field either gained.
func printLaneTemplate(lane project.TestLane) {
	if lane.SelectorTemplate != "" {
		Printf("  selector-template: %s\n", lane.SelectorTemplate)
	}
	if lane.SelectorJoin != "" {
		Printf("  selector-join: %s\n", lane.SelectorJoin)
	}
}

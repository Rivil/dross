package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/project"
)

// `dross run <name>` is the verb the [runtime] block was always missing.
//
// Eight of its keys — dev, stop, test_watch, lint, migrate, seed, shell, logs —
// were settable, documented, written by `dross onboard`, and read by nothing.
// They were formally recorded as inert, each with the same note: "no verb runs
// it yet". A value a tool invites you to configure and then ignores is the same
// lie config-value-truth was about, just spread across eight keys instead of
// one.
//
// This is deliberately NOT a second `dross test`. test is the ONE site that
// runs the suite, gated by one grant, defaulting to the remote when a grant
// exists. run is the general verb, local, with a per-slot grant — see
// runSlots for why each difference is there.

// runSlot is one runtime command dross can execute.
type runSlot struct {
	Name string
	// Field is the project.toml key, for the message that names the fix.
	Field string
	Get   func(*project.Project) string
	// Interactive slots inherit os.Stdin. A shell without stdin is not a
	// shell; a migration WITH stdin is one that can silently block forever on
	// a prompt nobody sees, so this is granted per-slot rather than to all.
	Interactive bool
	// LongRunning slots are expected to be ended by the user rather than to
	// finish. Ctrl-C is how you stop them, so an interrupt is success.
	LongRunning bool
	Short       string
}

// runSlots is the closed set, declared here rather than inferred, so a slot
// cannot silently join or leave. Order is the order `dross run` lists them.
var runSlots = []runSlot{
	{Name: "dev", Field: "runtime.dev_command", Short: "start the development server",
		Get: func(p *project.Project) string { return p.Runtime.DevCommand }, LongRunning: true},
	{Name: "stop", Field: "runtime.stop_command", Short: "stop what dev started",
		Get: func(p *project.Project) string { return p.Runtime.StopCommand }},
	{Name: "build", Field: "runtime.build_command", Short: "build the project",
		Get: func(p *project.Project) string { return p.Runtime.BuildCommand }},
	{Name: "lint", Field: "runtime.lint_command", Short: "run the linter",
		Get: func(p *project.Project) string { return p.Runtime.LintCommand }},
	{Name: "typecheck", Field: "runtime.typecheck_command", Short: "run the type checker",
		Get: func(p *project.Project) string { return p.Runtime.TypecheckCommand }},
	{Name: "format", Field: "runtime.format_command", Short: "run the formatter",
		Get: func(p *project.Project) string { return p.Runtime.FormatCommand }},
	{Name: "e2e", Field: "runtime.e2e_command", Short: "run the end-to-end suite",
		Get: func(p *project.Project) string { return p.Runtime.E2ECommand }},
	{Name: "test-watch", Field: "runtime.test_watch", Short: "re-run tests on change",
		Get: func(p *project.Project) string { return p.Runtime.TestWatch }, LongRunning: true},
	{Name: "migrate", Field: "runtime.migrate_command", Short: "run database migrations",
		Get: func(p *project.Project) string { return p.Runtime.MigrateCommand }},
	{Name: "seed", Field: "runtime.seed_command", Short: "seed the database",
		Get: func(p *project.Project) string { return p.Runtime.SeedCommand }},
	{Name: "shell", Field: "runtime.shell_command", Short: "open the project shell",
		Get: func(p *project.Project) string { return p.Runtime.ShellCommand }, Interactive: true, LongRunning: true},
	{Name: "logs", Field: "runtime.logs_command", Short: "tail the logs",
		Get: func(p *project.Project) string { return p.Runtime.LogsCommand }, LongRunning: true},
}

// runtime.test_command is deliberately absent from runSlots: `dross test` is
// the one site that runs the suite, and a second door to it would be a second
// place to gate, with its own grant. `dross run` says so rather than pretending
// the slot does not exist.
const testSlotRedirect = "test"

func findRunSlot(name string) (runSlot, bool) {
	for _, s := range runSlots {
		if s.Name == name {
			return s, true
		}
	}
	return runSlot{}, false
}

// Run registers `dross run [name] [args...]`.
func Run() *cobra.Command {
	c := &cobra.Command{
		Use:   "run [name] [args...]",
		Short: "Run a configured [runtime] command",
		Long: "Run one of project.toml's [runtime] commands.\n\n" +
			"With no name, lists the slots and what each is configured to run.\n" +
			"Trailing arguments are appended to the configured line.\n\n" +
			"Each slot needs its own consent: `dross trust --run <name>`. A grant for\n" +
			"one slot never authorizes another, and editing a command revokes its own\n" +
			"grant.",
		Args: cobra.ArbitraryArgs,
		// Flags after the slot name belong to the command being run, not to
		// dross. Without this, `dross run migrate --step 2` fails on an unknown
		// flag and the wrapper is useless for anything that takes options.
		DisableFlagParsing: false,
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			proj, err := project.Load(filepath.Join(root, project.File))
			if err != nil {
				return err
			}
			if len(args) == 0 {
				printRunSlots(proj)
				return nil
			}
			return runSlotNamed(root, proj, args[0], args[1:])
		},
	}
	c.Flags().SetInterspersed(false)
	return c
}

func printRunSlots(proj *project.Project) {
	Print("runtime commands:")
	names := make([]string, 0, len(runSlots))
	for _, s := range runSlots {
		names = append(names, s.Name)
	}
	width := 0
	for _, n := range names {
		if len(n) > width {
			width = len(n)
		}
	}
	for _, s := range runSlots {
		line := strings.TrimSpace(s.Get(proj))
		if line == "" {
			Printf("  %-*s  (not set — %s)\n", width, s.Name, s.Short)
			continue
		}
		Printf("  %-*s  %s\n", width, s.Name, line)
	}
	Printf("\n`dross run <name>` runs one. The suite is `dross test`, not `dross run %s`.\n", testSlotRedirect)
}

func runSlotNamed(root string, proj *project.Project, name string, extra []string) error {
	if name == testSlotRedirect {
		return errors.New("the test suite is run by `dross test`, which is the single execution site for it — `dross run` covers the other [runtime] commands")
	}
	slot, ok := findRunSlot(name)
	if !ok {
		return fmt.Errorf("unknown runtime command %q; known: %s", name, strings.Join(runSlotNames(), ", "))
	}
	base := strings.TrimSpace(slot.Get(proj))
	if base == "" {
		// Refused, never a silent success. "The tool said OK and nothing
		// happened" is the exact failure config-value-truth removed, and the
		// message names the one line that fixes it.
		return fmt.Errorf("%s is not set, so there is nothing to run.\n\n  set it:  dross project set %s \"<command>\"",
			slot.Field, slot.Field)
	}

	line := runCommandLine(base, extra)
	consented, err := RunConsented(root, line)
	if err != nil {
		return err
	}
	if !consented {
		return runConsentRefusal(slot, line)
	}

	repoDir := filepath.Dir(root)
	return spawnRunSlot(repoDir, slot, line)
}

func runSlotNames() []string {
	names := make([]string, 0, len(runSlots))
	for _, s := range runSlots {
		names = append(names, s.Name)
	}
	sort.Strings(names)
	return names
}

// runCommandLine appends arguments to the configured line. With none the line
// is byte-identical to what project.toml holds — the string the grant was taken
// over — for the same reason `dross test` keeps that identity: otherwise the
// gate approves one command and runs another.
func runCommandLine(base string, extra []string) string {
	line := strings.TrimSpace(base)
	for _, a := range extra {
		line += " " + shellQuoteArg(a)
	}
	return line
}

func runConsentRefusal(slot runSlot, line string) error {
	return fmt.Errorf("`dross run %s` has no consent on this machine.\n\n"+
		"  it would run:  %s\n\n"+
		"Read that line, then grant it:\n\n"+
		"  dross trust --run %s\n\n"+
		"Consent is per command: granting this one does not authorize any other slot, "+
		"and editing the command revokes its own grant.", slot.Name, line, slot.Name)
}

// spawnRunSlot runs the line, streaming straight to the terminal.
//
// A long-running slot is ended by the user, not by itself, so SIGINT/SIGTERM is
// success: Ctrl-C is the documented way to stop a dev server or a log tail, and
// reporting it as a failed command would make the ordinary case look red and
// the exit code useless for the slots that do terminate.
var spawnRunSlot = func(repoDir string, slot runSlot, line string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var stdin *os.File
	if slot.Interactive {
		stdin = os.Stdin
	}
	err := runSlotCommand(ctx, repoDir, line, stdin)
	if err != nil && ctx.Err() != nil {
		// The context is cancelled only because we were signalled, so the
		// command did not fail — it was stopped, which is how it ends.
		Printf("\n%s stopped\n", slot.Name)
		return nil
	}
	if err != nil {
		return &ExitCodeError{Code: 1, Err: fmt.Errorf("run %s: %w", slot.Name, err)}
	}
	return nil
}

// runSlotCommand is the spawn. It mirrors runLocalCommandCtx (test.go) with two
// deliberate differences: stdin is wired for an interactive slot, and the
// fence is labelled with the slot's own field so a refusal names the key the
// user would edit.
func runSlotCommand(ctx context.Context, dir, line string, stdin *os.File) error {
	argv, err := shArgvFor("runtime command", line)
	if err != nil {
		return err
	}
	c := exec.CommandContext(ctx, "sh", argv...)
	c.Dir = dir
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if stdin != nil {
		c.Stdin = stdin
	}
	// Killing `sh` leaves its children holding the pipe ends, so Wait would
	// block on a copy that never ends. The delay closes the descriptors and
	// returns — without it, Ctrl-C on a dev server hangs the terminal it was
	// meant to give back.
	c.WaitDelay = 5 * time.Second
	return c.Run()
}

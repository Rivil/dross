package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/gate"
)

// The hook commands hooks wiring writes into the user-level settings.json:
// PreToolUse runs the check, PostToolUse the recorders. Exactly these strings
// — `dross gate check` is how doctor recognises the wiring, too.
const (
	GateCheckHook  = "dross gate check"
	GateRecordHook = "dross gate record"
)

// GateEngine is the seam between the hook verbs and internal/gate. Tests swap
// it — including from package main, which is why it is exported — to inject a
// failure outside any gate; production never reassigns it.
var GateEngine = struct {
	Check  func([]byte, gate.Env) gate.Result
	Record func([]byte, gate.Env) []string
}{gate.Check, gate.Record}

// maxGatePayload caps what a hook verb reads from stdin. A payload past it is
// truncated, fails to decode, and is allowed with a warning — never blocked.
const maxGatePayload = 64 << 20

// gateNow is the clock lifts are judged by; tests move it.
var gateNow = time.Now

// gateLiftCap bounds `dross gate off --for`: a lift is a temporary way past a
// misbehaving gate, never a way to switch one off for good.
const gateLiftCap = 24 * time.Hour

// gateEnv is the environment the check runs in: the user's home, gateNow, and
// the gates a human lifted. With no home there is no override store to read —
// never one resolved against the working directory, where a repo could ship it.
func gateEnv() gate.Env {
	env := gate.Env{Now: gateNow}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		env.Home = home
		env.Lifted = gate.LiftedBy(home, gateNow())
	}
	return env
}

// Gate is `dross gate`.
func Gate() *cobra.Command {
	c := &cobra.Command{
		Use:   "gate",
		Short: "Tool-call gates run by Claude Code's PreToolUse/PostToolUse hooks",
	}
	c.AddCommand(gateCheck(), gateRecord(), gateOff(), gateOn(), gateStatus())
	return c
}

// gateCheck is the PreToolUse hook. Its exit status is the whole protocol:
// 2 blocks the tool call and hands stderr to Claude; anything else lets the
// call run. So a refusal is exit 2 with the refusal on stderr once (main
// prints the error; SilenceErrors keeps cobra from printing it again), an
// allowed call is exit 0 and silence, and every failure that is not a gate's
// verdict — an unreadable payload, a panic anywhere outside a gate's own
// recovery — is exit 0 with one warning. A Go panic left to the runtime exits
// 2, which here would block every tool call.
func gateCheck() *cobra.Command {
	return &cobra.Command{
		Use:           "check",
		Short:         "PreToolUse hook: judge the tool call on stdin (exit 2 refuses it)",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(c *cobra.Command, _ []string) (err error) {
			defer func() {
				if v := recover(); v != nil {
					fmt.Fprintf(c.ErrOrStderr(), "warning: dross gate check failed internally (%v) — allowing the call\n", v)
					err = nil
				}
			}()
			in, rerr := io.ReadAll(io.LimitReader(c.InOrStdin(), maxGatePayload))
			if rerr != nil {
				fmt.Fprintf(c.ErrOrStderr(), "warning: dross gate check could not read the hook payload (%v) — allowing the call\n", rerr)
				return nil
			}
			res := GateEngine.Check(in, gateEnv())
			for _, w := range res.Warnings {
				fmt.Fprintln(c.ErrOrStderr(), w)
			}
			if res.Allowed() {
				return nil
			}
			return &ExitCodeError{Code: 2, Err: errors.New(res.Text())}
		},
	}
}

// gateRecord is the PostToolUse hook. It never blocks: whatever happens, it
// exits 0, and a recorder that could not record is a warning.
func gateRecord() *cobra.Command {
	return &cobra.Command{
		Use:           "record",
		Short:         "PostToolUse hook: record what the finished tool call on stdin established",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(c *cobra.Command, _ []string) (err error) {
			defer func() {
				if v := recover(); v != nil {
					fmt.Fprintf(c.ErrOrStderr(), "warning: dross gate record failed internally (%v)\n", v)
					err = nil
				}
			}()
			in, rerr := io.ReadAll(io.LimitReader(c.InOrStdin(), maxGatePayload))
			if rerr != nil {
				fmt.Fprintf(c.ErrOrStderr(), "warning: dross gate record could not read the hook payload (%v)\n", rerr)
				return nil
			}
			for _, w := range GateEngine.Record(in, gate.Env{}) {
				fmt.Fprintln(c.ErrOrStderr(), w)
			}
			return nil
		},
	}
}

// lookupGate resolves a gate name; an unknown one is refused listing the real
// names, so a typo never reads as "lifted".
func lookupGate(name string) (gate.Gate, error) {
	if g, ok := gate.Lookup(name); ok {
		return g, nil
	}
	var names []string
	for _, g := range gate.All() {
		names = append(names, g.Name)
	}
	return gate.Gate{}, fmt.Errorf("no gate named %q — the gates are: %s", name, strings.Join(names, ", "))
}

// gateScope is where a lift of g applies: the dross repo the working directory
// is in for a workflow gate, machine-wide ("") for an always-on one — which is
// also what the output says, so a human never lifts more than they meant to.
func gateScope(g gate.Gate) (root, where string, err error) {
	if g.Scope == gate.AlwaysOn {
		return "", "machine-wide", nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", "", err
	}
	root, err = gate.LocateRoot(wd)
	if err != nil {
		return "", "", err
	}
	if root == "" {
		return "", "", fmt.Errorf("gate %s is lifted per repo, and %s is not inside a dross repo — run this from the repo it should be lifted in", g.Name, wd)
	}
	return root, "for " + root + " only", nil
}

// gateOff lifts a gate for a while. It is the human's escape from a gate that
// is wrong here; gate-off-guard refuses it from the agent's Bash tool, so only
// the human's own terminal reaches it.
func gateOff() *cobra.Command {
	var dur time.Duration
	c := &cobra.Command{
		Use:   "off <name>",
		Short: "Lift a gate for a while (human only: refused from the agent's Bash tool)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			g, err := lookupGate(args[0])
			if err != nil {
				return err
			}
			if !g.Liftable {
				return fmt.Errorf("gate %s cannot be lifted — it guards the gates themselves", g.Name)
			}
			if dur <= 0 || dur > gateLiftCap {
				return fmt.Errorf("--for %s is out of range: a lift lasts longer than 0 and at most %s", dur, gateLiftCap)
			}
			root, where, err := gateScope(g)
			if err != nil {
				return err
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			until := gateNow().Add(dur)
			if err := gate.Lift(home, g, root, until); err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "gate %s lifted %s until %s (%s)\n", g.Name, where, until.UTC().Format(time.RFC3339), dur)
			return nil
		},
	}
	c.Flags().DurationVar(&dur, "for", time.Hour, "how long the lift lasts (at most 24h)")
	return c
}

// gateOn ends a lift before it expires.
func gateOn() *cobra.Command {
	return &cobra.Command{
		Use:   "on <name>",
		Short: "Put a lifted gate back before its lift expires",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			g, err := lookupGate(args[0])
			if err != nil {
				return err
			}
			if !g.Liftable {
				fmt.Fprintf(c.OutOrStdout(), "gate %s is always on — it cannot be lifted\n", g.Name)
				return nil
			}
			root, where, err := gateScope(g)
			if err != nil {
				return err
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			if err := gate.Unlift(home, g, root); err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "gate %s on %s\n", g.Name, where)
			return nil
		},
	}
}

// gateStatus lists every registered gate: its scope, and whether it is on
// here or lifted until when. An override store that cannot be read lifts
// nothing, and says why rather than showing every gate quietly on.
func gateStatus() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show every gate: its scope, and whether it is on or lifted until when",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			out := c.OutOrStdout()
			now := gateNow()
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			list, err := gate.LoadOverrides(home)
			if err != nil {
				fmt.Fprintf(out, "overrides: %v — no gate is lifted while it cannot be read\n", err)
				list = nil
			}
			wd, err := os.Getwd()
			if err != nil {
				return err
			}
			root, err := gate.LocateRoot(wd)
			switch {
			case err != nil:
				fmt.Fprintf(out, "repo: cannot tell (%v)\n", err)
			case root == "":
				fmt.Fprintln(out, "repo: none here — workflow gates fire only inside a dross repo")
			default:
				fmt.Fprintf(out, "repo: %s\n", root)
			}
			for _, g := range gate.All() {
				state := "on"
				switch o := liftOf(list, g, root, now); {
				case !g.Liftable:
					state = "on (cannot be lifted)"
				case o != nil:
					where := "this repo"
					if g.Scope == gate.AlwaysOn {
						where = "machine-wide"
					}
					state = fmt.Sprintf("off %s until %s (%s left)", where, o.Until.UTC().Format(time.RFC3339), o.Until.Sub(now).Round(time.Minute))
				}
				fmt.Fprintf(out, "%-15s %-9s %s\n", g.Name, g.Scope, state)
			}
			return nil
		},
	}
}

// liftOf is g's unexpired lift for root: the entry gate.LiftedBy matches.
func liftOf(list []gate.Override, g gate.Gate, root string, now time.Time) *gate.Override {
	key := ""
	if g.Scope == gate.Workflow {
		if root == "" {
			return nil
		}
		key = gate.RepoKey(root)
	}
	for i, o := range list {
		if o.Name == g.Name && o.Repo == key && now.Before(o.Until) {
			return &list[i]
		}
	}
	return nil
}

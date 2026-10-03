package cmd

import (
	"errors"
	"fmt"
	"io"

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

// Gate is `dross gate`.
func Gate() *cobra.Command {
	c := &cobra.Command{
		Use:   "gate",
		Short: "Tool-call gates run by Claude Code's PreToolUse/PostToolUse hooks",
	}
	c.AddCommand(gateCheck(), gateRecord())
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
			res := GateEngine.Check(in, gate.Env{})
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

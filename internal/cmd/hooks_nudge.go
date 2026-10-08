package cmd

import (
	"io"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/ctxnudge"
	"github.com/Rivil/dross/internal/defaults"
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/state"
)

// NudgeHook is the PostToolUse command `dross hooks ensure` wires: the context
// checkpoint nudge.
const NudgeHook = "dross hooks nudge"

// nudgeRun is the seam tests swap to drive the verb's failure handling.
var nudgeRun = ctxnudge.Run

// hooksNudge is the PostToolUse hook that injects one "context checkpoint:"
// line once the session's context passes [context] threshold. It never fails:
// whatever happens it exits 0, and anything short of a nudge prints nothing —
// not even a warning, because it runs on every tool call.
func hooksNudge() *cobra.Command {
	return &cobra.Command{
		Use:           "nudge",
		Short:         "PostToolUse hook: inject a context-checkpoint line once the session passes [context] threshold",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(c *cobra.Command, _ []string) (err error) {
			defer func() {
				if recover() != nil {
					err = nil
				}
			}()
			in, rerr := io.ReadAll(io.LimitReader(c.InOrStdin(), maxGatePayload))
			if rerr != nil {
				return nil
			}
			out := nudgeRun(in, nudgeEnv{})
			if len(out) > 0 {
				_, _ = c.OutOrStdout().Write(out)
			}
			return nil
		},
	}
}

// nudgeEnv is the ctxnudge.Env the hook verb runs with: this machine's
// defaults.toml and this repo's re-entry line.
type nudgeEnv struct{}

// Threshold reads [context] threshold from ~/.claude/dross/defaults.toml.
// An unreadable or invalid file is an error, which silences the nudge; doctor
// is where a silently-off nudge is reported.
func (nudgeEnv) Threshold() (int64, error) {
	dir, err := GlobalDir()
	if err != nil {
		return 0, err
	}
	d, err := defaults.LoadFile(filepath.Join(dir, defaults.File))
	if err != nil {
		return 0, err
	}
	return d.Context.EffectiveThreshold()
}

// Reentry is the re-entry command a fresh session's SessionStart line will
// print for repoRoot: the "next: …" tail of reentryLine — suggestNext plus
// any open debug session — so the nudge and the line after /clear agree by
// construction. It only reads; a repo it cannot read falls back to
// /dross-status.
func (nudgeEnv) Reentry(repoRoot string) string {
	root := filepath.Join(repoRoot, RootDirName)
	proj, err := project.Load(filepath.Join(root, project.File))
	if err != nil {
		return "/dross-status"
	}
	st, err := state.Load(filepath.Join(root, state.File))
	if err != nil {
		return "/dross-status"
	}
	_, next, ok := strings.Cut(reentryLine(root, proj, st), " — next: ")
	if !ok {
		return "/dross-status"
	}
	return next
}

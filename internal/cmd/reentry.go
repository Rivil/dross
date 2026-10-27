package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Rivil/dross/internal/gatestate"

	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/render"
	"github.com/Rivil/dross/internal/state"
)

// sessionStartOutput is the Claude Code SessionStart hook JSON envelope.
// Plain hook stdout reaches only the model's context; systemMessage is the
// documented user-visible channel, so the same line rides both fields — the
// human and the model re-orient off identical text.
type sessionStartOutput struct {
	SystemMessage      string                 `json:"systemMessage"`
	HookSpecificOutput sessionStartHookOutput `json:"hookSpecificOutput"`
}

type sessionStartHookOutput struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext"`
}

// Reentry prints the one-line "you are here + next command" summary a fresh
// session needs after /clear, wrapped in the SessionStart hook JSON envelope.
// It is the SessionStart hook target, so outside a dross repo it exits 0 with
// no output — a non-dross cwd is the normal case, not an error.
func Reentry() *cobra.Command {
	return &cobra.Command{
		Use:   "reentry",
		Short: "One-line 'you are here + next command' for a fresh session (SessionStart hook target)",
		RunE: func(c *cobra.Command, _ []string) error {
			root, err := FindRoot()
			if errors.Is(err, ErrNoRoot) {
				return nil
			}
			if err != nil {
				return err
			}
			bumpFromHook(c.InOrStdin(), filepath.Dir(root))
			proj, err := project.Load(filepath.Join(root, project.File))
			if err != nil {
				return err
			}
			st, err := state.Load(filepath.Join(root, state.File))
			if err != nil {
				return err
			}
			line := reentryLine(root, proj, st)
			out, err := render.MarshalJSON(sessionStartOutput{
				SystemMessage: line,
				HookSpecificOutput: sessionStartHookOutput{
					HookEventName:     "SessionStart",
					AdditionalContext: line,
				},
			})
			if err != nil {
				return err
			}
			Print(string(out))
			return nil
		},
	}
}

// reentryLine is the shared generator behind `dross reentry` and the final
// line of `dross status` — status prints it verbatim, reentry embeds it in
// the hook envelope, so a fresh session reads the same "you are here"
// whichever it runs first.
//
// An open debug session rides the end of the line, not suggestNext: the
// phase's own next step stays what it was, and the hook — which fires after
// /clear and after compaction — still names the investigation to resume.
func reentryLine(root string, proj *project.Project, st *state.State) string {
	where := "(no phase)"
	if st.CurrentPhase != "" {
		where = st.CurrentPhase
		if st.CurrentPhaseStatus != "" {
			where += " (" + st.CurrentPhaseStatus + ")"
		}
	}
	line := fmt.Sprintf("you are here: %s · v%s — next: %s", where, st.Version, suggestNext(root, proj, st))
	if open := openDebugSessions(root); len(open) > 0 {
		e := open[0]
		line += fmt.Sprintf(" · debug: %s %s — /dross-debug %s", e.Slug, e.Session.State(), e.Slug)
		if len(open) > 1 {
			line += fmt.Sprintf(" (+%d more)", len(open)-1)
		}
	}
	return line
}

// reentryStdinIsTTY reports whether stdin is a terminal — `dross reentry` run
// by hand, with no hook payload to read. A test swaps it.
var reentryStdinIsTTY = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

const (
	// maxReentryPayload caps the SessionStart payload read; anything longer is
	// not a payload this hook understands.
	maxReentryPayload = 1 << 20
	// reentryReadDeadline bounds the read so a hook whose stdin never closes
	// cannot hold a session's start.
	reentryReadDeadline = 300 * time.Millisecond
)

// bumpFromHook starts a new show epoch for the session the SessionStart
// payload names. Every source bumps — startup, resume, clear and compact each
// leave a context that has not seen the last full `rule show` or `interaction
// show` — so a repeat after any of them prints in full. It is silent on every
// failure: the hook's envelope and exit status never depend on it.
func bumpFromHook(stdin io.Reader, repoDir string) {
	if reentryStdinIsTTY() {
		return
	}
	data, ok := readBounded(io.LimitReader(stdin, maxReentryPayload+1), reentryReadDeadline)
	if !ok || len(data) > maxReentryPayload {
		return
	}
	_ = gatestate.BumpForHook(repoDir, data)
}

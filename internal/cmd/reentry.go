package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/project"
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
		RunE: func(_ *cobra.Command, _ []string) error {
			root, err := FindRoot()
			if errors.Is(err, ErrNoRoot) {
				return nil
			}
			if err != nil {
				return err
			}
			proj, err := project.Load(filepath.Join(root, project.File))
			if err != nil {
				return err
			}
			st, err := state.Load(filepath.Join(root, state.File))
			if err != nil {
				return err
			}
			line := reentryLine(root, proj, st)
			out, err := json.Marshal(sessionStartOutput{
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
func reentryLine(root string, proj *project.Project, st *state.State) string {
	where := "(no phase)"
	if st.CurrentPhase != "" {
		where = st.CurrentPhase
		if st.CurrentPhaseStatus != "" {
			where += " (" + st.CurrentPhaseStatus + ")"
		}
	}
	return fmt.Sprintf("you are here: %s · v%s — next: %s", where, st.Version, suggestNext(root, proj, st))
}

package cmd

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/state"
)

// Reentry prints the one-line "you are here + next command" summary a fresh
// session needs after /clear. It is the SessionStart hook target, so outside a
// dross repo it exits 0 with no output — a non-dross cwd is the normal case,
// not an error.
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
			Print(reentryLine(root, proj, st))
			return nil
		},
	}
}

// reentryLine is the shared generator behind `dross reentry` and the final
// line of `dross status` — the two surfaces must stay byte-equal, so a fresh
// session reads the same "you are here" whichever it runs first.
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

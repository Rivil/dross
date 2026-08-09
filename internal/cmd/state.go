package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/state"
)

func State() *cobra.Command {
	c := &cobra.Command{
		Use:   "state",
		Short: "Read and edit .dross/state.json",
	}
	c.AddCommand(stateShow(), stateGet(), stateSet(), stateTouch(), stateBump())
	return c
}

func stateShow() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "show",
		Short: "Print state.json",
		RunE: func(_ *cobra.Command, _ []string) error {
			s, _, err := loadState()
			if err != nil {
				return err
			}
			b, _ := json.MarshalIndent(s, "", "  ")
			os.Stdout.Write(b)
			fmt.Println()
			return nil
		},
	}
	// state.json is already what this prints, so --json is accepted and
	// changes nothing. It exists because callers reach for it by analogy with
	// every other --json in the tree, and an unknown-flag error there is pure
	// friction.
	c.Flags().BoolVar(&asJSON, "json", false, "accepted for symmetry; state show always emits JSON")
	return c
}

// stateGet reads one or more fields out of state.json. One path prints its
// bare value; two or more emit a keyed JSON object (locked multi_get_shape).
func stateGet() *cobra.Command {
	return &cobra.Command{
		Use:   "get <field>...",
		Short: "Print one or more state fields (multiple emit a keyed JSON object)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			s, _, err := loadState()
			if err != nil {
				return err
			}
			return renderMultiGet(args, func(path string) (any, error) {
				return readStateDotted(s, path)
			})
		},
	}
}

// readStateDotted covers every readable field in state.json. The paths are
// bare, matching `state set <field>` — state.json has no nested tables.
func readStateDotted(s *state.State, path string) (any, error) {
	switch path {
	case "version":
		return s.Version, nil
	case "current_milestone":
		return s.CurrentMilestone, nil
	case "current_phase":
		return s.CurrentPhase, nil
	case "current_phase_status":
		return s.CurrentPhaseStatus, nil
	case "last_action":
		return s.LastAction, nil
	case "last_activity":
		return s.LastActivity.Format(time.RFC3339Nano), nil
	}
	return nil, fmt.Errorf("unknown field: %s", path)
}

func stateSet() *cobra.Command {
	return &cobra.Command{
		Use:   "set <field> <value>",
		Short: "Set version | current_milestone | current_phase | current_phase_status",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			s, path, err := loadState()
			if err != nil {
				return err
			}
			switch args[0] {
			case "version":
				// Both homes, one writer (c-4) — see writeVersion.
				if err := writeVersion(filepath.Dir(path), s, args[1]); err != nil {
					return err
				}
			case "current_milestone":
				s.CurrentMilestone = args[1]
			case "current_phase":
				s.CurrentPhase = args[1]
			case "current_phase_status":
				s.CurrentPhaseStatus = args[1]
			default:
				return fmt.Errorf("unknown field: %s", args[0])
			}
			s.Touch("set " + args[0] + "=" + args[1])
			return s.Save(path)
		},
	}
}

func stateTouch() *cobra.Command {
	return &cobra.Command{
		Use:   "touch <action>",
		Short: "Append an activity entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			s, path, err := loadState()
			if err != nil {
				// Hook target (c-2): slash commands stamp activity from
				// wherever they run, so "not a dross repo" is a silent exit 0
				// and writes nothing. Scoped to this handler on purpose —
				// putting it in loadState() would silence `state show` / `set`
				// / `bump` too, and a corrupt state.json (not ErrNoRoot) stays
				// loud here as well (locked completeness_check).
				if errors.Is(err, ErrNoRoot) {
					return nil
				}
				return err
			}
			s.Touch(args[0])
			if err := s.Save(path); err != nil {
				return err
			}
			Printf("touched: %s (history now %d entries)\n", args[0], len(s.History))
			return nil
		},
	}
}

// stateBump increments a segment of state.json's 4-part version.
//
// Today only `internal` is supported — the last segment, bumped per
// /dross-quick task per the global versioning rule. `patch` and the
// `major.minor` segments are driven by phase / milestone workflows that
// already write the version explicitly, so they don't need a CLI bump.
func stateBump() *cobra.Command {
	return &cobra.Command{
		Use:   "bump <segment>",
		Short: "Bump a version segment (currently only `internal`)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if args[0] != "internal" {
				return fmt.Errorf("unsupported segment %q (only `internal` is bumpable)", args[0])
			}
			// A loop-step boundary — see execGatedCommands.
			if err := requireExecConsent(); err != nil {
				return err
			}
			s, path, err := loadState()
			if err != nil {
				return err
			}
			next, err := bumpInternal(s.Version)
			if err != nil {
				return err
			}
			prev := s.Version
			// Both homes, one writer (c-4): a bump that only moved state.json
			// would leave the release tag pinned at the previous value.
			if err := writeVersion(filepath.Dir(path), s, next); err != nil {
				return err
			}
			s.Touch(fmt.Sprintf("bump internal %s → %s", prev, next))
			if err := s.Save(path); err != nil {
				return err
			}
			Printf("%s → %s\n", prev, next)
			return nil
		},
	}
}

// bumpInternal increments the 4th segment of a 4-part version string.
// Rejects anything that doesn't match major.minor.patch.internal with
// non-negative integer segments.
func bumpInternal(v string) (string, error) {
	if err := validateVersion(v); err != nil {
		return "", err
	}
	parts := strings.Split(v, ".")
	last, _ := strconv.Atoi(parts[3])
	parts[3] = strconv.Itoa(last + 1)
	return strings.Join(parts, "."), nil
}

// validateVersion checks the 4-part major.minor.patch.internal form with
// non-negative integer segments. Shared so the writer and the bumper cannot
// disagree about what a version is.
func validateVersion(v string) error {
	parts := strings.Split(v, ".")
	if len(parts) != 4 {
		return fmt.Errorf("version %q is not 4-part (major.minor.patch.internal)", v)
	}
	for _, p := range parts {
		if n, err := strconv.Atoi(p); err != nil || n < 0 {
			return fmt.Errorf("version %q has non-integer segment %q", v, p)
		}
	}
	return nil
}

// writeVersion is the single writer for the project version, which lives in two
// places: state.json, the machine-local position dross reads, and
// project.toml's [project].version, the tracked value release.yml tags from
// (locked version_home). One call writes both, so the release-facing number and
// the number dross bumps cannot drift apart (c-4).
//
// Order and sequencing are the contract:
//
//   - Validation runs before either write, so a rejected version leaves both
//     files byte-unchanged rather than truncating one and failing on the other.
//   - project.toml is written first and its failure returns immediately, naming
//     the path. A half-write that left state.json ahead of the tracked copy is
//     exactly the divergence this function exists to prevent.
//   - s is mutated but not saved: the caller usually has a Touch to append and
//     owns the save. By then the tracked copy is already on disk.
func writeVersion(root string, s *state.State, v string) error {
	if err := validateVersion(v); err != nil {
		return err
	}
	projPath := filepath.Join(root, project.File)
	p, err := project.Load(projPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", projPath, err)
	}
	p.Project.Version = v
	if err := p.Save(projPath); err != nil {
		return fmt.Errorf("write %s: %w", projPath, err)
	}
	s.Version = v
	return nil
}

// ensureState materializes .dross/state.json when it is absent, so a fresh
// clone — where the file is machine-local and gitignored rather than committed
// (locked decision: state_tracking) — resolves as a working root instead of a
// broken one. Version is seeded from project.toml's [project].version, the
// release-facing home of the number, and History starts empty: the durable
// record is the git log plus the tracked phase artefacts, not a mirrored
// activity log (locked decision: history_durability).
//
// It is strictly non-destructive. A file already on disk is left exactly as it
// is, malformed or not — parsing it belongs to whoever loads it, and replacing
// a corrupt state.json here would destroy the history it may still hold.
func ensureState(root string) error {
	path := filepath.Join(root, state.File)
	switch _, err := os.Stat(path); {
	case err == nil:
		return nil
	case !errors.Is(err, fs.ErrNotExist):
		return err
	}
	s := state.New()
	// An unparseable project.toml, or one carrying no version, is not this
	// function's to report — every command that loads it says so loudly. Seed
	// the default rather than turning it into a root-resolution failure.
	if p, err := project.Load(filepath.Join(root, project.File)); err == nil && p.Project.Version != "" {
		s.Version = p.Project.Version
	}
	return s.Save(path)
}

func loadState() (*state.State, string, error) {
	root, err := FindRoot()
	if err != nil {
		return nil, "", err
	}
	path := filepath.Join(root, state.File)
	s, err := state.Load(path)
	if err != nil {
		return nil, "", err
	}
	return s, path, nil
}

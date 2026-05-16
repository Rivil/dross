package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/state"
)

func State() *cobra.Command {
	c := &cobra.Command{
		Use:   "state",
		Short: "Read and edit .dross/state.json",
	}
	c.AddCommand(stateShow(), stateSet(), stateTouch(), stateBump())
	return c
}

func stateShow() *cobra.Command {
	return &cobra.Command{
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
				s.Version = args[1]
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
			s, path, err := loadState()
			if err != nil {
				return err
			}
			next, err := bumpInternal(s.Version)
			if err != nil {
				return err
			}
			prev := s.Version
			s.Version = next
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
	parts := strings.Split(v, ".")
	if len(parts) != 4 {
		return "", fmt.Errorf("version %q is not 4-part (major.minor.patch.internal)", v)
	}
	for _, p := range parts {
		if _, err := strconv.Atoi(p); err != nil {
			return "", fmt.Errorf("version %q has non-integer segment %q", v, p)
		}
	}
	last, _ := strconv.Atoi(parts[3])
	parts[3] = strconv.Itoa(last + 1)
	return strings.Join(parts, "."), nil
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

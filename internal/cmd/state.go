package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/state"
)

func State() *cobra.Command {
	c := &cobra.Command{
		Use:   "state",
		Short: "Read and edit .dross/state.json",
	}
	c.AddCommand(stateShow(), stateSet(), stateTouch())
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

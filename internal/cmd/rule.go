package cmd

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/rules"
)

// Rule registers `dross rule {add,list,remove,promote,disable,enable,show}`.
func Rule() *cobra.Command {
	c := &cobra.Command{
		Use:   "rule",
		Short: "Manage two-tier rules (global + project)",
	}
	c.AddCommand(ruleAdd(), ruleList(), ruleRemove(), rulePromote(), ruleDisable(), ruleEnable(), ruleShow())
	return c
}

func ruleAdd() *cobra.Command {
	var scope, severity, id string
	c := &cobra.Command{
		Use:   "add <text>",
		Short: "Add a rule",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			set, path, err := loadScope(scope)
			if err != nil {
				return err
			}
			if id == "" {
				id = nextID(set)
			}
			r := rules.Rule{
				ID:       id,
				Text:     args[0],
				Severity: rules.Severity(severity),
			}
			if err := set.Add(r); err != nil {
				return err
			}
			if err := set.SaveFile(path); err != nil {
				return err
			}
			Printf("added [%s/%s/%s] %s\n", scope, severity, id, args[0])
			return nil
		},
	}
	c.Flags().StringVar(&scope, "scope", "project", "global | project")
	c.Flags().StringVar(&severity, "severity", "hard", "hard | soft")
	c.Flags().StringVar(&id, "id", "", "rule id (auto-generated if empty)")
	return c
}

func ruleList() *cobra.Command {
	var scope string
	var merged bool
	c := &cobra.Command{
		Use:   "list",
		Short: "List rules in a scope (or merged)",
		RunE: func(_ *cobra.Command, _ []string) error {
			if merged || scope == "all" {
				m, err := loadMerged()
				if err != nil {
					return err
				}
				Print(rules.Render(m))
				return nil
			}
			set, _, err := loadScope(scope)
			if err != nil {
				return err
			}
			if len(set.Rules) == 0 {
				Print("(no rules)")
				return nil
			}
			for _, r := range set.Rules {
				flag := ""
				if r.Disabled {
					flag = " (disabled)"
				}
				sev := r.Severity
				if sev == "" {
					sev = rules.Hard
				}
				Printf("[%s/%s] %s%s\n  %s\n", scope, sev, r.ID, flag, r.Text)
			}
			return nil
		},
	}
	c.Flags().StringVar(&scope, "scope", "project", "global | project | all")
	c.Flags().BoolVar(&merged, "merged", false, "show merged + rendered prompt block")
	return c
}

func ruleRemove() *cobra.Command {
	var scope string
	c := &cobra.Command{
		Use:   "remove <id>",
		Short: "Remove a rule by id",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			set, path, err := loadScope(scope)
			if err != nil {
				return err
			}
			if !set.Remove(args[0]) {
				return fmt.Errorf("rule not found: %s", args[0])
			}
			return set.SaveFile(path)
		},
	}
	c.Flags().StringVar(&scope, "scope", "project", "global | project")
	return c
}

func rulePromote() *cobra.Command {
	return &cobra.Command{
		Use:   "promote <id>",
		Short: "Move a project rule to global scope",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			projSet, projPath, err := loadScope("project")
			if err != nil {
				return err
			}
			r, ok := projSet.Find(args[0])
			if !ok {
				return fmt.Errorf("project rule not found: %s", args[0])
			}
			globalSet, globalPath, err := loadScope("global")
			if err != nil {
				return err
			}
			if err := globalSet.Add(r); err != nil {
				return err
			}
			projSet.Remove(args[0])
			if err := projSet.SaveFile(projPath); err != nil {
				return err
			}
			return globalSet.SaveFile(globalPath)
		},
	}
}

func ruleDisable() *cobra.Command { return toggleCmd("disable", true) }
func ruleEnable() *cobra.Command  { return toggleCmd("enable", false) }

func toggleCmd(name string, disabled bool) *cobra.Command {
	var scope string
	c := &cobra.Command{
		Use:   name + " <id>",
		Short: name + " a rule (keeps it in file)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			set, path, err := loadScope(scope)
			if err != nil {
				return err
			}
			if !set.SetDisabled(args[0], disabled) {
				return fmt.Errorf("rule not found: %s", args[0])
			}
			return set.SaveFile(path)
		},
	}
	c.Flags().StringVar(&scope, "scope", "project", "global | project")
	return c
}

// ruleShow renders the merged rules block — what gets injected into prompt context.
func ruleShow() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Render merged rules as the <rules> prompt block",
		RunE: func(_ *cobra.Command, _ []string) error {
			m, err := loadMerged()
			if err != nil {
				return err
			}
			Print(rules.Render(m))
			return nil
		},
	}
}

// loadScope loads a single rules.toml. Project lookup walks up to find .dross/.
// Global lives at ~/.claude/dross/rules.toml.
func loadScope(scope string) (*rules.Set, string, error) {
	switch scope {
	case "global":
		dir, err := GlobalDir()
		if err != nil {
			return nil, "", err
		}
		path := filepath.Join(dir, rules.File)
		set, err := rules.LoadFile(path)
		return set, path, err
	case "project":
		root, err := FindRoot()
		if err != nil {
			return nil, "", err
		}
		path := filepath.Join(root, rules.File)
		set, err := rules.LoadFile(path)
		return set, path, err
	default:
		return nil, "", errors.New(`scope must be "global" or "project"`)
	}
}

func loadMerged() ([]rules.Resolved, error) {
	g, _, err := loadScope("global")
	if err != nil {
		return nil, err
	}
	p, _, err := loadScope("project")
	if err != nil {
		// project rules are optional outside a repo
		if errors.Is(err, ErrNoRoot) {
			p = &rules.Set{}
		} else {
			return nil, err
		}
	}
	return rules.Merge(g, p), nil
}

// nextID returns the next "r-NN" id not present in the set.
func nextID(set *rules.Set) string {
	n := len(set.Rules) + 1
	for {
		candidate := fmt.Sprintf("r-%02d", n)
		if _, exists := set.Find(candidate); !exists {
			return candidate
		}
		n++
	}
}

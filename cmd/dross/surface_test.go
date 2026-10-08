package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// surface_test.go pins the WHOLE assembled command tree — every command
// newRoot() wires, not just the four trees internal/cmd's cli_surface_test.go
// pins — as testdata/cli_tree.txt. The cmd-exec-baseline-drain phase moves
// spawn, codec and store logic out of internal/cmd; this golden, minted before
// anything moved, is what "command names and flags are unchanged" (c-7) means
// in enforceable form.
//
// It lives in package main because newRoot is here and internal/cmd cannot
// import it. Every rendered line carries its command path, so a drift diff
// names the command without needing surrounding context.

// goldenUpdateEnv is the opt-in that rewrites the golden instead of checking
// it — the same variable internal/cmd's goldens use.
const goldenUpdateEnv = "DROSS_UPDATE_GOLDEN"

// cliTreeGolden is the golden's path, relative to this package.
const cliTreeGolden = "testdata/cli_tree.txt"

// errTreeGoldenMissing signals "no reference on disk": a missing golden is a
// failure, never a vacuous pass.
var errTreeGoldenMissing = errors.New("golden missing")

// renderCLITree renders every command under root, depth-first in cobra's
// (sorted) child order: one line per command — path, use, short, aliases,
// hidden/deprecated — then one line per flag, sorted by name, with its
// shorthand, type, default, no-opt default, hidden bit and usage. Local flags
// exclude inherited ones, so a parent's persistent flag is rendered once, on
// the command that declares it. Long and Example are help prose and
// deliberately left out.
func renderCLITree(root *cobra.Command) string {
	var sb strings.Builder
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		path := c.CommandPath()
		fmt.Fprintf(&sb, "%s | use=%q short=%q", path, c.Use, c.Short)
		if len(c.Aliases) > 0 {
			fmt.Fprintf(&sb, " aliases=%q", strings.Join(c.Aliases, ","))
		}
		if c.Hidden {
			sb.WriteString(" hidden")
		}
		if c.Deprecated != "" {
			fmt.Fprintf(&sb, " deprecated=%q", c.Deprecated)
		}
		sb.WriteString("\n")
		renderTreeFlags(&sb, path, "flag", c.LocalNonPersistentFlags())
		renderTreeFlags(&sb, path, "pflag", c.PersistentFlags())
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(root)
	return sb.String()
}

// renderTreeFlags appends one self-describing line per flag, sorted by name.
func renderTreeFlags(sb *strings.Builder, path, label string, fs *pflag.FlagSet) {
	fs.VisitAll(func(f *pflag.Flag) {
		fmt.Fprintf(sb, "%s %s --%s", path, label, f.Name)
		if f.Shorthand != "" {
			fmt.Fprintf(sb, " -%s", f.Shorthand)
		}
		fmt.Fprintf(sb, " (%s) default=%q", f.Value.Type(), f.DefValue)
		if f.NoOptDefVal != "" {
			fmt.Fprintf(sb, " noopt=%q", f.NoOptDefVal)
		}
		if f.Hidden {
			sb.WriteString(" hidden")
		}
		if f.Deprecated != "" {
			fmt.Fprintf(sb, " deprecated=%q", f.Deprecated)
		}
		fmt.Fprintf(sb, " usage=%q\n", f.Usage)
	})
}

// treeGoldenCheck compares got against the golden at path. With update set it
// writes got. Without it a missing golden is an error, and a mismatch returns
// only the lines that differ — each one names its command.
func treeGoldenCheck(path, got string, update bool) error {
	if update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, []byte(got), 0o644)
	}
	want, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: %s (run with %s=1 to mint it)", errTreeGoldenMissing, path, goldenUpdateEnv)
		}
		return err
	}
	if string(want) == got {
		return nil
	}
	return fmt.Errorf("command tree differs from %s:\n%s", path, changedLines(string(want), got))
}

// changedLines is an LCS line diff that prints only the removed (-) and added
// (+) lines, in order. The tree is ~1000 lines; context would bury the change.
func changedLines(want, got string) string {
	a := strings.Split(strings.TrimSuffix(want, "\n"), "\n")
	b := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	n, m := len(a), len(b)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			switch {
			case a[i] == b[j]:
				lcs[i][j] = lcs[i+1][j+1] + 1
			case lcs[i+1][j] >= lcs[i][j+1]:
				lcs[i][j] = lcs[i+1][j]
			default:
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	var sb strings.Builder
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && a[i] == b[j]:
			i++
			j++
		case j < m && (i == n || lcs[i][j+1] >= lcs[i+1][j]):
			sb.WriteString("+" + b[j] + "\n")
			j++
		default:
			sb.WriteString("-" + a[i] + "\n")
			i++
		}
	}
	return sb.String()
}

// TestCLITreeGolden: the assembled tree renders exactly as its pre-phase
// golden. A dropped flag, a renamed command or a flipped default/hidden bit
// shows up as a diff line naming the command.
func TestCLITreeGolden(t *testing.T) {
	got := renderCLITree(newRoot())
	if err := treeGoldenCheck(cliTreeGolden, got, os.Getenv(goldenUpdateEnv) == "1"); err != nil {
		t.Fatal(err)
	}
}

// TestCLITreeGoldenBites proves the golden catches each drift the phase could
// introduce, on the live tree: a dropped `update --check`, a renamed
// `local get`, and a flipped default and hidden bit each produce a diff line
// naming the command — and a missing golden refuses rather than passing.
func TestCLITreeGoldenBites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cli_tree.txt")
	if err := treeGoldenCheck(path, "x\n", false); !errors.Is(err, errTreeGoldenMissing) {
		t.Fatalf("missing golden did not refuse with errTreeGoldenMissing; got %v", err)
	}
	if err := treeGoldenCheck(path, renderCLITree(newRoot()), true); err != nil {
		t.Fatal(err)
	}

	find := func(root *cobra.Command, args ...string) *cobra.Command {
		t.Helper()
		c, _, err := root.Find(args)
		if err != nil || c == root {
			t.Fatalf("no command %v in the live tree: %v", args, err)
		}
		return c
	}
	drifts := []struct {
		name   string
		mutate func(root *cobra.Command)
		want   string // a substring of some changed line
	}{
		{"drop update --check", func(root *cobra.Command) {
			u := find(root, "update")
			fs := pflag.NewFlagSet(u.Name(), pflag.ContinueOnError)
			u.LocalNonPersistentFlags().VisitAll(func(f *pflag.Flag) {
				if f.Name != "check" {
					fs.AddFlag(f)
				}
			})
			u.ResetFlags()
			u.Flags().AddFlagSet(fs)
		}, "-dross update flag --check"},
		{"rename local get", func(root *cobra.Command) {
			find(root, "local", "get").Use = "fetch <key>"
		}, "-dross local get |"},
		{"flip a default", func(root *cobra.Command) {
			find(root, "update").Flags().Lookup("force").DefValue = "true"
		}, "dross update flag --force (bool) default=\"true\""},
		{"flip a hidden bit", func(root *cobra.Command) {
			find(root, "update").Flags().Lookup("api-base").Hidden = false
		}, "-dross update flag --api-base"},
	}
	for _, d := range drifts {
		t.Run(d.name, func(t *testing.T) {
			root := newRoot()
			d.mutate(root)
			err := treeGoldenCheck(path, renderCLITree(root), false)
			if err == nil {
				t.Fatal("drift passed the golden")
			}
			if !strings.Contains(err.Error(), d.want) {
				t.Fatalf("diff does not name the drift %q:\n%v", d.want, err)
			}
		})
	}
}

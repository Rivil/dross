package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// cli_surface_test.go pins the user-visible surface of the four command trees
// the cmd-package-decomposition phase moves logic out of — trust, issue,
// doctor, verify — as goldens under testdata/cli_surface/. The goldens were
// minted BEFORE any code moved and are permanent: every later task in the
// phase must leave them byte-identical, which is what c-6 ("no user-visible
// CLI change") means in enforceable form.
//
// Goldens are written only under DROSS_UPDATE_GOLDEN=1. A run that finds no
// golden fails rather than passing vacuously — a missing reference is the one
// state that would let a regression through unnoticed.

// goldenUpdateEnv is the opt-in that rewrites goldens instead of checking them.
const goldenUpdateEnv = "DROSS_UPDATE_GOLDEN"

// cliSurfaceDir is where this file's goldens live, relative to the package.
const cliSurfaceDir = "testdata/cli_surface"

// errGoldenMissing is the pure-helper signal for "no reference on disk". It is
// its own error so TestGoldenMissingFails can assert the exact refusal rather
// than any failure.
var errGoldenMissing = errors.New("golden missing")

// goldenCheck compares got against the golden at path. With update set it
// writes got and reports success. Without it, a missing golden is an error —
// never a pass — and a mismatch returns an error carrying a unified diff.
//
// It is a pure function (no *testing.T) so the "missing golden fails"
// contract can be tested directly instead of through a sub-runner.
func goldenCheck(path, got string, update bool) error {
	if update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, []byte(got), 0o644)
	}
	want, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: %s (run with %s=1 to mint it)", errGoldenMissing, path, goldenUpdateEnv)
		}
		return err
	}
	if string(want) == got {
		return nil
	}
	return fmt.Errorf("output differs from golden %s:\n%s", path, unifiedDiff(string(want), got))
}

// checkGolden is the *testing.T wrapper around goldenCheck.
func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join(cliSurfaceDir, name)
	if err := goldenCheck(path, got, os.Getenv(goldenUpdateEnv) == "1"); err != nil {
		t.Fatal(err)
	}
}

// unifiedDiff renders a minimal unified diff of want → got, line-based, via an
// LCS table. Inputs here are a few hundred lines at most, so O(n·m) is fine.
func unifiedDiff(want, got string) string {
	a := strings.Split(strings.TrimSuffix(want, "\n"), "\n")
	b := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	n, m := len(a), len(b)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	var sb strings.Builder
	sb.WriteString("--- want\n+++ got\n")
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && a[i] == b[j]:
			sb.WriteString(" " + a[i] + "\n")
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

// renderCommandSurface walks a cobra tree into a stable text rendering of its
// user-visible surface: command path, Short, Aliases, hidden/deprecated
// markers, then every flag in DECLARED order — name, shorthand, type, default,
// no-opt default, hidden marker, usage. Long/Example are deliberately left
// out: they are help prose, and pinning them would make the golden churn on
// wording edits that change no behaviour.
//
// Flags are visited with SortFlags=false so the rendering follows declaration
// order; the trees are built fresh per test, so the mutation is harmless.
func renderCommandSurface(cmd *cobra.Command) string {
	var sb strings.Builder
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		fmt.Fprintf(&sb, "command: %s\n", c.CommandPath())
		fmt.Fprintf(&sb, "  use: %s\n", c.Use)
		fmt.Fprintf(&sb, "  short: %s\n", c.Short)
		if len(c.Aliases) > 0 {
			fmt.Fprintf(&sb, "  aliases: %s\n", strings.Join(c.Aliases, ", "))
		}
		if c.Hidden {
			sb.WriteString("  hidden: true\n")
		}
		if c.Deprecated != "" {
			fmt.Fprintf(&sb, "  deprecated: %s\n", c.Deprecated)
		}
		renderFlagSet(&sb, "flag", c.Flags())
		renderFlagSet(&sb, "pflag", c.PersistentFlags())
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(cmd)
	return sb.String()
}

// renderFlagSet appends one line per flag in declaration order.
func renderFlagSet(sb *strings.Builder, label string, fs *pflag.FlagSet) {
	prev := fs.SortFlags
	fs.SortFlags = false
	defer func() { fs.SortFlags = prev }()
	fs.VisitAll(func(f *pflag.Flag) {
		fmt.Fprintf(sb, "  %s: --%s", label, f.Name)
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

// TestCLISurfacePinned: the four trees the phase decomposes render exactly as
// their pre-move goldens. A renamed flag, a dropped subcommand or a changed
// default shows up as a diff line naming the command.
func TestCLISurfacePinned(t *testing.T) {
	trees := []struct {
		name  string
		build func() *cobra.Command
	}{
		{"trust", Trust},
		{"issue", Issue},
		{"doctor", Doctor},
		{"verify", Verify},
	}
	for _, tc := range trees {
		t.Run(tc.name, func(t *testing.T) {
			got := renderCommandSurface(tc.build())
			if !strings.HasPrefix(got, "command: "+tc.name+"\n") {
				t.Fatalf("rendering does not start with the %s command:\n%s", tc.name, got)
			}
			checkGolden(t, tc.name+".txt", got)
		})
	}
}

// TestDoctorRunGolden: the full `dross doctor` transcript over a greenfield
// repo — gitInit + Init(), HOME isolated so no user-level defaults leak a
// [remote] or [board], gitVersionOutput stubbed so the git-version line does
// not depend on the machine, every mutation tool absent via fakeLookPath so
// the toolchain section renders the same on a laptop and on the remote
// runner — plus the exit code, pinned byte-for-byte.
//
// The greenfield fixture rather than consentFixture: that one deliberately
// omits rules.toml, so doctor stops after the foundational-files block and
// the transcript would pin a single line. This one walks every section.
func TestDoctorRunGolden(t *testing.T) {
	pkgDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	gitInit(t, dir, "")
	chdir(t, dir)
	t.Setenv("HOME", t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	prev := gitVersionOutput
	t.Cleanup(func() { gitVersionOutput = prev })
	gitVersionOutput = func() (string, error) { return "git version 2.45.0", nil }
	fakeLookPath(t, nil)

	var out string
	runErr := runCmdCapturing(t, &out, Doctor())
	got := out + fmt.Sprintf("--- exit: %d\n", ExitCode(runErr))

	// chdir moved cwd into the fixture; the golden lives beside this file.
	path := filepath.Join(pkgDir, cliSurfaceDir, "doctor_run.txt")
	if err := goldenCheck(path, got, os.Getenv(goldenUpdateEnv) == "1"); err != nil {
		t.Fatal(err)
	}
}

// testFuncRE matches a top-level test declaration. A regexp rather than
// go/ast on purpose: the phase's boundary ratchet (c-5) forbids new go/ast
// imports in internal/cmd, and a line scan is all this inventory needs.
var testFuncRE = regexp.MustCompile(`^func (Test[A-Za-z0-9_]*)\(`)

// collectTestNames walks every *_test.go under internalDir and returns sorted
// unique `name<TAB>package` rows, where package is the directory relative to
// internal/. Homonyms across packages appear once per package.
func collectTestNames(internalDir string) ([]string, error) {
	seen := map[string]bool{}
	err := filepath.WalkDir(internalDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(internalDir, filepath.Dir(path))
		if err != nil {
			return err
		}
		pkg := filepath.ToSlash(rel)
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			if m := testFuncRE.FindStringSubmatch(sc.Text()); m != nil {
				seen[m[1]+"\t"+pkg] = true
			}
		}
		return sc.Err()
	})
	if err != nil {
		return nil, err
	}
	rows := make([]string, 0, len(seen))
	for r := range seen {
		rows = append(rows, r)
	}
	sort.Strings(rows)
	return rows, nil
}

// testsBeforeRowRE is the shape every tests_before.txt row must have.
var testsBeforeRowRE = regexp.MustCompile(`^Test[A-Za-z0-9_]*\t[a-z0-9_/]+$`)

// TestTestNamesRecorded: tests_before.txt is the pre-move inventory of every
// `func Test*` under internal/, one `name<TAB>package` row each. It is minted
// under DROSS_UPDATE_GOLDEN=1 and otherwise only checked for presence and
// shape — the no-test-lost comparison against the post-move tree is t-10's
// boundary test, which consumes this file. It is NOT compared to the current
// tree here: names legitimately change package as code moves.
func TestTestNamesRecorded(t *testing.T) {
	root := repoRootFromTest(t)
	path := filepath.Join(root, "internal", "cmd", cliSurfaceDir, "tests_before.txt")
	if os.Getenv(goldenUpdateEnv) == "1" {
		rows, err := collectTestNames(filepath.Join(root, "internal"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(strings.Join(rows, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("tests_before.txt missing — mint it with %s=1: %v", goldenUpdateEnv, err)
	}
	rows := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	if len(rows) == 0 || rows[0] == "" {
		t.Fatal("tests_before.txt is empty")
	}
	for i, r := range rows {
		if !testsBeforeRowRE.MatchString(r) {
			t.Errorf("row %d malformed (want name<TAB>package): %q", i+1, r)
		}
		if i > 0 && rows[i-1] >= r {
			t.Errorf("row %d out of order or duplicated: %q after %q", i+1, r, rows[i-1])
		}
	}
	// The inventory must at least see this file's own tests, or the walker
	// is scanning the wrong tree.
	if !strings.Contains(string(b), "TestTestNamesRecorded\tcmd\n") {
		t.Error("tests_before.txt does not list TestTestNamesRecorded in cmd — walker scanned the wrong tree")
	}
}

// TestGoldenMissingFails: with no golden on disk and the update env unset,
// the check refuses — it never passes vacuously. And with update set, it
// mints the file so a following check passes.
func TestGoldenMissingFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "never_minted.txt")
	err := goldenCheck(path, "anything\n", false)
	if !errors.Is(err, errGoldenMissing) {
		t.Fatalf("missing golden did not refuse with errGoldenMissing; got %v", err)
	}
	if err := goldenCheck(path, "anything\n", true); err != nil {
		t.Fatalf("update did not mint the golden: %v", err)
	}
	if err := goldenCheck(path, "anything\n", false); err != nil {
		t.Fatalf("freshly minted golden does not pass: %v", err)
	}
	err = goldenCheck(path, "something else\n", false)
	if err == nil || !strings.Contains(err.Error(), "-anything") || !strings.Contains(err.Error(), "+something else") {
		t.Fatalf("mismatch did not report a unified diff; got %v", err)
	}
}

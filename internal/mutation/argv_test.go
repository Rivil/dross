package mutation

import (
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/argfence"
)

// The Reject half of the locked analyzer_arg_policy decision, asserted at the
// three mutation call sites.
//
// None of gremlins, npx or dotnet has an end-of-options token, so a derived
// value beginning with a dash cannot be fenced — it is refused before exec.
// Two properties each rejection must have, and both are load-bearing:
//
//   - It happens BEFORE the process is built. A refusal that still spawned the
//     tool would be indistinguishable from one that didn't, so every row here
//     substitutes a build seam that fails the test if it is called.
//
//   - It yields a NIL report, not an empty one. An empty *Report reads as "no
//     surviving mutants" — a perfect score — so a rejection that returned one
//     alongside its error would sail through verify as a pass.

// refuseGremlinsExec installs a build seam that fails the test if reached.
func refuseGremlinsExec(t *testing.T) {
	t.Helper()
	prev := gremlinsBuildCmd
	gremlinsBuildCmd = func(_ *Gremlins, args []string) *exec.Cmd {
		t.Fatalf("gremlins was built despite a value that should have been refused: %v", args)
		return nil
	}
	t.Cleanup(func() { gremlinsBuildCmd = prev })
}

func refuseStrykerExec(t *testing.T) {
	t.Helper()
	prev := strykerBuildCmd
	strykerBuildCmd = func(_ *Stryker, args []string) *exec.Cmd {
		t.Fatalf("stryker was built despite a value that should have been refused: %v", args)
		return nil
	}
	t.Cleanup(func() { strykerBuildCmd = prev })
}

func refuseStrykerNetExec(t *testing.T) {
	t.Helper()
	prev := strykerNetBuildCmd
	strykerNetBuildCmd = func(_ *StrykerNet, args []string) *exec.Cmd {
		t.Fatalf("stryker.net was built despite a value that should have been refused: %v", args)
		return nil
	}
	t.Cleanup(func() { strykerNetBuildCmd = prev })
}

// TestGremlinsRejectsDashPackage drives Run with a package derivation that
// yields a leading-dash package.
//
// The derivation is substituted rather than fed a hostile filename because
// packagesFromFiles always emits "." or "./<dir>" today — prefix-constant, and
// therefore safe by accident of one function. The guard exists for the day that
// stops being true, so the test has to reach it directly.
func TestGremlinsRejectsDashPackage(t *testing.T) {
	refuseGremlinsExec(t)
	prev := packagesFromFilesFn
	packagesFromFilesFn = func([]string) []string { return []string{"-rf"} }
	t.Cleanup(func() { packagesFromFilesFn = prev })

	rep, err := (&Gremlins{ProjectRoot: t.TempDir()}).Run([]string{"internal/x/x.go"})
	if err == nil {
		t.Fatal("a leading-dash package was accepted")
	}
	if !errors.Is(err, argfence.ErrLeadingDash) {
		t.Errorf("error does not wrap argfence.ErrLeadingDash: %v", err)
	}
	if rep != nil {
		t.Errorf("rejection returned a report (%+v) — an empty report reads as a perfect score", rep)
	}
}

func TestUnleashArgsRejectsDashReport(t *testing.T) {
	g := &Gremlins{}
	args, err := g.buildUnleashArgs("-rf.json", []string{"./internal/cmd"})
	if err == nil {
		t.Fatal("a leading-dash report path was accepted")
	}
	if !errors.Is(err, argfence.ErrLeadingDash) {
		t.Errorf("error does not wrap argfence.ErrLeadingDash: %v", err)
	}
	if args != nil {
		t.Errorf("rejection returned an argv: %v", args)
	}
	// The same builder must still accept the shape sanitizePkg actually
	// produces — over-rejection would break every real run.
	if _, err := g.buildUnleashArgs("reports/gremlins/internal_cmd.json", []string{"./internal/cmd", "."}); err != nil {
		t.Errorf("a legitimate report path and package were rejected: %v", err)
	}
}

func TestStrykerRejectsDashEntries(t *testing.T) {
	t.Run("dash mutate entry", func(t *testing.T) {
		refuseStrykerExec(t)
		rep, err := (&Stryker{}).Run([]string{"-rf.ts"})
		if err == nil {
			t.Fatal("a leading-dash mutate entry was accepted")
		}
		if !errors.Is(err, argfence.ErrLeadingDash) {
			t.Errorf("error does not wrap argfence.ErrLeadingDash: %v", err)
		}
		if rep != nil {
			t.Errorf("rejection returned a report: %+v", rep)
		}
	})

	t.Run("dash entry only after the workdir trim", func(t *testing.T) {
		// It is the TRIMMED form that lands in the argv, so a path that looks
		// harmless before the prefix strip must still be caught.
		s := &Stryker{Workdir: "web"}
		if _, err := s.runArgs([]string{"web/-rf.ts"}); err == nil {
			t.Fatal("a path that becomes a dash entry after the trim was accepted")
		}
	})

	t.Run("dash workdir", func(t *testing.T) {
		refuseStrykerExec(t)
		if _, err := (&Stryker{Workdir: "--config=/tmp/x"}).Run([]string{"src/a.ts"}); err == nil {
			t.Fatal("a leading-dash workdir was accepted")
		}
	})

	t.Run("ordinary paths still pass", func(t *testing.T) {
		args, err := (&Stryker{}).runArgs([]string{"src/a.ts"})
		if err != nil {
			t.Fatalf("src/a.ts was rejected: %v", err)
		}
		if !strings.Contains(strings.Join(args, " "), "src/a.ts") {
			t.Errorf("argv lost the mutate path: %v", args)
		}
	})
}

func TestStrykerNetRejectsDashOutputDir(t *testing.T) {
	refuseStrykerNetExec(t)
	rep, err := (&StrykerNet{OutputDir: "-rf"}).Run([]string{"Foo.cs"})
	if err == nil {
		t.Fatal("a leading-dash output dir was accepted")
	}
	if !errors.Is(err, argfence.ErrLeadingDash) {
		t.Errorf("error does not wrap argfence.ErrLeadingDash: %v", err)
	}
	if rep != nil {
		t.Errorf("rejection returned a report: %+v", rep)
	}
}

// TestRejectionNamesToolAndValue: a bare "rejected" tells the user nothing
// about which config line to fix. Every rejection names both.
func TestRejectionNamesToolAndValue(t *testing.T) {
	cases := []struct {
		name       string
		run        func() error
		wantTool   string
		wantValue  string
		installSea func(*testing.T)
	}{
		{"gremlins", func() error {
			_, err := (&Gremlins{}).buildUnleashArgs("r.json", []string{"-rf"})
			return err
		}, "gremlins", "-rf", nil},
		{"npx", func() error {
			_, err := (&Stryker{}).runArgs([]string{"-mutate-me.ts"})
			return err
		}, "npx", "-mutate-me.ts", nil},
		{"dotnet", func() error {
			_, err := (&StrykerNet{OutputDir: "-out"}).Run([]string{"Foo.cs"})
			return err
		}, "dotnet", "-out", refuseStrykerNetExec},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.installSea != nil {
				tc.installSea(t)
			}
			err := tc.run()
			if err == nil {
				t.Fatal("expected a rejection")
			}
			if !strings.Contains(err.Error(), tc.wantTool) {
				t.Errorf("rejection does not name the tool %q: %v", tc.wantTool, err)
			}
			if !strings.Contains(err.Error(), tc.wantValue) {
				t.Errorf("rejection does not name the value %q: %v", tc.wantValue, err)
			}
		})
	}
}

// TestRejectionYieldsNilReport is the score-safety property, asserted across all
// three adapters at once: an empty *Report has Killed=0, Survived=0 and Score=0,
// which verify reads as "nothing survived". A rejection must never produce one.
func TestRejectionYieldsNilReport(t *testing.T) {
	refuseGremlinsExec(t)
	refuseStrykerExec(t)
	refuseStrykerNetExec(t)

	prev := packagesFromFilesFn
	packagesFromFilesFn = func([]string) []string { return []string{"-rf"} }
	t.Cleanup(func() { packagesFromFilesFn = prev })

	adapters := map[string]func() (*Report, error){
		"gremlins":    func() (*Report, error) { return (&Gremlins{ProjectRoot: t.TempDir()}).Run([]string{"x/x.go"}) },
		"stryker":     func() (*Report, error) { return (&Stryker{}).Run([]string{"-rf.ts"}) },
		"stryker-net": func() (*Report, error) { return (&StrykerNet{OutputDir: "-rf"}).Run([]string{"Foo.cs"}) },
	}
	for name, run := range adapters {
		t.Run(name, func(t *testing.T) {
			rep, err := run()
			if err == nil {
				t.Fatal("expected a rejection")
			}
			if rep != nil {
				t.Fatalf("rejection returned report %+v — a zero-count report reads as a perfect score and passes verify", rep)
			}
		})
	}
}

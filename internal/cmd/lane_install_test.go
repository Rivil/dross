package cmd

// The install DECISION layer: what dross would do about one lane tool, before
// any machine is touched.
//
// Every assertion here is about the four arms staying four — a resolver that
// collapsed refusal into unknown, or let a built-in recipe leak past a declared
// line, would still return "an install step" and satisfy a test that only
// checked one field.

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

// TestResolveInstallRefusesRuntimes pins the locked install_scope_for_lanes
// line. The runtime rows are not gaps waiting to be filled: deleting one turns
// its tool into an Unknown, which is silently reported rather than refused —
// so the row disappearing would quietly relitigate a locked decision.
func TestResolveInstallRefusesRuntimes(t *testing.T) {
	for _, tool := range []string{"go", "node", "npx", "dotnet"} {
		t.Run(tool, func(t *testing.T) {
			s := resolveInstall(tool, "")
			if s.Refusal == "" {
				t.Fatalf("%s is not refused: %+v", tool, s)
			}
			if !strings.Contains(s.Refusal, tool) {
				t.Errorf("the refusal does not name %s: %q", tool, s.Refusal)
			}
			if !strings.Contains(s.Refusal, "runtime") {
				t.Errorf("the refusal does not say WHY — that it is a runtime: %q", s.Refusal)
			}
			if s.Argv != nil {
				t.Errorf("a refused tool carries an argv: %v", s.Argv)
			}
			if s.Unknown {
				t.Errorf("a refusal also set Unknown — the two arms collapsed")
			}
			if laneInstallable(s) {
				t.Errorf("a refused tool reports as installable")
			}
		})
	}
}

// TestDeclaredLineReplacesTheTableEntry is c-4's override half. Appending to
// the built-in recipe rather than replacing it would keep running the very
// command the user overrode to say was wrong.
func TestDeclaredLineReplacesTheTableEntry(t *testing.T) {
	builtin := laneInstallRecipes["pnpm"]
	if builtin.argv == nil {
		t.Fatal("the fixture assumes a built-in pnpm recipe; the table lost it")
	}

	s := resolveInstall("pnpm", "corepack enable pnpm")

	if s.Line != "corepack enable pnpm" {
		t.Fatalf("the declared line did not win: %+v", s)
	}
	if s.Argv != nil {
		t.Errorf("a declared line still carried an argv: %v", s.Argv)
	}
	if s.Runtime != "" {
		t.Errorf("an overridden step carried the table's runtime %q — the lane's author owns that line's prerequisites", s.Runtime)
	}
	// Asserted over the WHOLE step, not just Argv: the built-in recipe must
	// appear nowhere, including in a note or a field a later renderer prints.
	// The tool's NAME is not the recipe — `corepack enable pnpm` names pnpm
	// too — so the fragment looked for is the built-in command itself.
	rendered := fmt.Sprintf("%+v", s)
	if recipe := strings.Join(builtin.argv, " "); strings.Contains(rendered, recipe) {
		t.Errorf("the built-in recipe %q leaked into an overridden step:\n%s", recipe, rendered)
	}

	// And the rendered argv, which is what actually reaches a machine.
	argv, err := installArgv(s)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(argv, " "), strings.Join(builtin.argv, " ")) {
		t.Errorf("the built-in recipe reached the argv: %q", argv)
	}
}

// TestTableInstallsWithoutADeclaredLine is the half that makes the feature work
// on lanes ALREADY declared. Without it every existing lane would sit
// uninstallable until someone rewrote it with an install line.
func TestTableInstallsWithoutADeclaredLine(t *testing.T) {
	s := resolveInstall("staticcheck", "")

	if len(s.Argv) == 0 {
		t.Fatalf("a tool with a table row returned no argv: %+v", s)
	}
	if s.Line != "" || s.Refusal != "" || s.Unknown {
		t.Errorf("a recipe step set another arm too: %+v", s)
	}
	if s.Runtime != "go" {
		t.Errorf("runtime = %q, want the binary that must already be there", s.Runtime)
	}
	if !laneInstallable(s) {
		t.Errorf("a table step reports as not installable")
	}

	// The returned argv must be a copy. Aliasing the table would let one
	// caller's edit change what every later call in the process installs.
	s.Argv[0] = "mutated"
	if laneInstallRecipes["staticcheck"].argv[0] == "mutated" {
		t.Error("the returned argv aliases the package-level table")
	}
}

// TestUnknownToolIsReportedNotRefused encodes locked undeclared_exit at the
// TYPE level. Refusal drives the non-zero exit; a tool nobody wrote a line for
// is a gap in this repo's configuration, and counting it as a refusal would
// make every repo with lanes and no install lines start failing a command that
// passed the day before.
func TestUnknownToolIsReportedNotRefused(t *testing.T) {
	s := resolveInstall("nosuchtool", "")

	if !s.Unknown {
		t.Fatalf("an unknown tool did not set Unknown: %+v", s)
	}
	if s.Refusal != "" {
		t.Errorf("an unknown tool was reported as a refusal: %q", s.Refusal)
	}
	if !strings.Contains(s.Note, "no install line") || !strings.Contains(s.Note, "nosuchtool") {
		t.Errorf("the note does not carry the remedy wording: %q", s.Note)
	}
	if laneInstallable(s) {
		t.Errorf("an unknown tool reports as installable")
	}

	// The sweep: no key in the table may ever produce both arms, in either
	// declared state. The two exit differently, so a step setting both would
	// have no defined exit at all.
	for tool := range laneInstallRecipes {
		for _, declared := range []string{"", "some install line"} {
			got := resolveInstall(tool, declared)
			if got.Unknown && got.Refusal != "" {
				t.Errorf("%q (declared=%q) set BOTH Unknown and Refusal: %+v", tool, declared, got)
			}
		}
	}
}

// TestDeclaredLineIsNotArgv0: a line quoted whole would exec a binary literally
// named `npm install -g pnpm`, which fails with a message about a missing file
// rather than about anything the user wrote.
func TestDeclaredLineIsNotArgv0(t *testing.T) {
	argv, err := installArgv(resolveInstall("pnpm", "npm install -g pnpm"))
	if err != nil {
		t.Fatal(err)
	}
	if len(argv) == 1 {
		t.Fatalf("the declared line was rendered as a single argv element: %q", argv)
	}
	if argv[0] == "npm install -g pnpm" {
		t.Fatalf("argv[0] is the whole line — nothing on any PATH is named that: %q", argv)
	}
}

// TestInstallArgvIsTheOneRenderer: a declared line becomes exactly `sh -c
// <line>` and a table step's argv passes through untouched. Two renderers would
// mean one surface quoting a line the other executes.
func TestInstallArgvIsTheOneRenderer(t *testing.T) {
	line, err := installArgv(resolveInstall("pnpm", "corepack enable pnpm"))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"sh", "-c", "corepack enable pnpm"}; !reflect.DeepEqual(line, want) {
		t.Errorf("declared line rendered %q, want %q", line, want)
	}

	step := resolveInstall("staticcheck", "")
	argv, err := installArgv(step)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(argv, step.Argv) {
		t.Errorf("a table step's argv was rewritten: %q, want %q", argv, step.Argv)
	}

	// The leading-dash fence, shared with runtime.test_command: a line starting
	// with `-` is read by sh as its own flag rather than as a script.
	if _, err := installArgv(resolveInstall("pnpm", "-c evil")); err == nil {
		t.Error("a leading-dash install line was rendered rather than refused")
	}
}

// TestLaneInstallReusesTheRemoteSeam: two remote seams means each surface's
// tests stub a different one, and a test that proved "nothing was installed"
// would be watching the variable the code does not use.
func TestLaneInstallReusesTheRemoteSeam(t *testing.T) {
	src, err := os.ReadFile("lane_install.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	if !strings.Contains(body, "remoteExecFn") {
		t.Error("lane_install.go does not reach the existing remote exec seam")
	}
	if strings.Contains(body, "= remote.Exec") {
		t.Error("lane_install.go declares a remote exec seam of its own — there must be exactly one")
	}
}

// TestNoTableRowInstallsARuntime is a STRUCTURAL sweep rather than a list of
// known-bad rows: it holds for rows nobody has written yet, which is the only
// way a table guard survives the table growing.
func TestNoTableRowInstallsARuntime(t *testing.T) {
	// A system package manager is how a runtime gets installed, and every one
	// of them is root on a machine dross was merely lent.
	managers := map[string]bool{"apt": true, "apt-get": true, "brew": true, "curl": true, "dnf": true, "yum": true, "pacman": true, "apk": true}

	for tool, r := range laneInstallRecipes {
		if r.argv == nil {
			if r.refusal == "" {
				t.Errorf("%q has neither an argv nor a refusal — it would fall through as a silent no-op", tool)
			}
			continue
		}
		if r.runtime == "" {
			t.Errorf("%q installs without naming a runtime it needs — a row that needs nothing already there is installing a runtime", tool)
		}
		if len(r.argv) == 0 {
			t.Errorf("%q has an empty argv", tool)
			continue
		}
		if managers[r.argv[0]] {
			t.Errorf("%q installs through %q — a system package manager is host administration, not a package install", tool, r.argv[0])
		}
	}
}

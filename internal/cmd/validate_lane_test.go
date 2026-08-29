package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/configenum"
	"github.com/Rivil/dross/internal/project"
)

// laneFixture is a repo that validates clean, ready for a lane block to be
// appended. Everything these tests assert is therefore attributable to the
// lane: a problem list of length zero before, and exactly the lane's own
// problems after.
func laneFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustRunSet(t, "project.name", "test-app")
	mustRunSet(t, "runtime.mode", "native")
	return dir
}

// appendLanes adds raw [[runtime.test_lane]] blocks to the fixture's
// project.toml. Appended as text, not written through a struct: an
// array-of-tables header names its full path from the document root, so it is
// valid wherever it lands, and the tests then exercise the same decode path a
// hand-edited project.toml takes — which is the path validate exists for.
func appendLanes(t *testing.T, dir, blocks string) {
	t.Helper()
	path := filepath.Join(dir, ".dross", "project.toml")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("\n" + blocks + "\n"); err != nil {
		t.Fatal(err)
	}
}

// validateOutput runs validate over the cwd fixture and returns its printed
// problem lines alongside the error, since the error only carries a count and
// every assertion here is about which lane was named.
func validateOutput(t *testing.T) (string, error) {
	t.Helper()
	var out string
	err := runCmdCapturing(t, &out, Validate())
	return out, err
}

// TestValidateNamesLaneMissingCommand: a lane with a name and globs but no
// command is unrunnable, and the message has to say WHICH lane so a
// project.toml carrying several does not send the user reading all of them.
func TestValidateNamesLaneMissingCommand(t *testing.T) {
	dir := laneFixture(t)
	appendLanes(t, dir, `[[runtime.test_lane]]
name = "go"
match = ["internal/**"]`)

	out, err := validateOutput(t)
	if err == nil {
		t.Fatalf("validate accepted a lane with no command:\n%s", out)
	}
	if !strings.Contains(out, `"go"`) || !strings.Contains(out, "command") {
		t.Errorf("problem must name the lane and the missing field, got:\n%s", out)
	}
}

// TestValidateNamesLaneWithEmptyMatch: a lane matching nothing is not a lane
// that runs rarely, it is a lane that can never be selected — reported by name
// rather than accepted as a deliberate no-op.
func TestValidateNamesLaneWithEmptyMatch(t *testing.T) {
	dir := laneFixture(t)
	appendLanes(t, dir, `[[runtime.test_lane]]
name = "docs"
match = []
command = "true"`)

	out, err := validateOutput(t)
	if err == nil {
		t.Fatalf("validate accepted a lane with an empty match list:\n%s", out)
	}
	if !strings.Contains(out, `"docs"`) || !strings.Contains(out, "match") {
		t.Errorf("problem must name the lane and the empty match list, got:\n%s", out)
	}
}

// TestValidateIdentifiesNamelessLaneByIndex: a nameless lane cannot be named,
// so it is addressed by its ordinal. Reporting it as `runtime.test_lane ""`
// would point at nothing when the document holds more than one block.
func TestValidateIdentifiesNamelessLaneByIndex(t *testing.T) {
	dir := laneFixture(t)
	appendLanes(t, dir, `[[runtime.test_lane]]
match = ["internal/**"]
command = "go test ./..."`)

	out, err := validateOutput(t)
	if err == nil {
		t.Fatalf("validate accepted a nameless lane:\n%s", out)
	}
	if !strings.Contains(out, "runtime.test_lane[0]") {
		t.Errorf("nameless lane must be addressed by ordinal, got:\n%s", out)
	}
	if strings.Contains(out, `runtime.test_lane ""`) {
		t.Errorf("nameless lane reported as the empty name, which points at nothing:\n%s", out)
	}
}

// TestValidateRejectsDuplicateLaneName: the machine-local grant store is keyed
// by lane name, so two lanes sharing one name means one grant with authority
// over two different command lines. Dropping this check is how a consented
// `go test` lane silently authorizes whatever the second `go` lane runs.
func TestValidateRejectsDuplicateLaneName(t *testing.T) {
	dir := laneFixture(t)
	appendLanes(t, dir, `[[runtime.test_lane]]
name = "go"
match = ["internal/**"]
command = "go test ./internal/..."

[[runtime.test_lane]]
name = "go"
match = ["cmd/**"]
command = "go test ./cmd/..."`)

	out, err := validateOutput(t)
	if err == nil {
		t.Fatalf("validate accepted two lanes named go:\n%s", out)
	}
	if !strings.Contains(out, `"go"`) || !strings.Contains(out, "more than once") {
		t.Errorf("problem must report the repeated lane name, got:\n%s", out)
	}
}

// TestValidateRejectsUncompilableGlob: a pattern that does not compile matches
// nothing, forever. Left unchecked it surfaces later as a file set that
// mysteriously selects no lane — a file-set-shaped symptom for a
// lane-configuration cause, which sends the user to the wrong file.
func TestValidateRejectsUncompilableGlob(t *testing.T) {
	dir := laneFixture(t)
	appendLanes(t, dir, `[[runtime.test_lane]]
name = "go"
match = ["internal/[cmd/**"]
command = "go test ./..."`)

	out, err := validateOutput(t)
	if err == nil {
		t.Fatalf("validate accepted an uncompilable glob:\n%s", out)
	}
	if !strings.Contains(out, `"go"`) {
		t.Errorf("problem must name the lane, got:\n%s", out)
	}
	if !strings.Contains(out, "internal/[cmd/**") {
		t.Errorf("problem must quote the offending pattern, got:\n%s", out)
	}
}

// TestValidateAcceptsAWellFormedLane is the other side of every check above:
// a complete lane must pass. Without it, a validator that rejected all lanes
// would satisfy the whole rest of this file.
func TestValidateAcceptsAWellFormedLane(t *testing.T) {
	dir := laneFixture(t)
	appendLanes(t, dir, `[[runtime.test_lane]]
name = "go"
match = ["internal/**/*.go", "main.go"]
command = "go test -count=1 ./..."

[[runtime.test_lane]]
name = "docs"
match = ["docs/", "README.md"]
command = "true"`)

	out, err := validateOutput(t)
	if err != nil {
		t.Fatalf("validate rejected well-formed lanes: %v\n%s", err, out)
	}
}

// TestValidateIgnoresARepoWithNoLanes: lanes are opt-in (the bare_test_run
// decision), so the validator must invent no work for the repos that never
// asked for them — which today is every existing repo.
func TestValidateIgnoresARepoWithNoLanes(t *testing.T) {
	laneFixture(t)

	out, err := validateOutput(t)
	if err != nil {
		t.Fatalf("a lane-less repo must still validate clean: %v\n%s", err, out)
	}
	if strings.Contains(out, "test_lane") {
		t.Errorf("a lane-less repo must produce no lane problems, got:\n%s", out)
	}
}

// TestValidateNamesLaneWithUnknownSelector: a selector style dross cannot
// translate is a lane that would either spawn unscoped or not at all, and the
// message has to name the lane so a project.toml carrying several does not send
// the user reading all of them.
func TestValidateNamesLaneWithUnknownSelector(t *testing.T) {
	dir := laneFixture(t)
	appendLanes(t, dir, `[[runtime.test_lane]]
name = "go"
match = ["internal/**"]
command = "go test"
selector = "packages"`)

	out, err := validateOutput(t)
	if err == nil {
		t.Fatalf("validate accepted selector = \"packages\":\n%s", out)
	}
	if !strings.Contains(out, `runtime.test_lane "go"`) {
		t.Errorf("the refusal must name the lane, got:\n%s", out)
	}
	// Rendered from the Set, never typed here as a literal: a style added to
	// SelectorStyles must reach this message without anyone editing it.
	if !strings.Contains(out, configenum.SelectorStyles.List()) {
		t.Errorf("the refusal must name the accepted styles (%s), got:\n%s", configenum.SelectorStyles.List(), out)
	}
}

// TestValidateAcceptsAbsentAndDeclaredSelectors: the omitted selector is the
// pre-selector behaviour every existing lane relies on, so it must be as valid
// as a declared one. Set.Has("") is false for SelectorStyles — it has no code
// default — so an implementation that forwards straight to Has fails here.
func TestValidateAcceptsAbsentAndDeclaredSelectors(t *testing.T) {
	dir := laneFixture(t)
	appendLanes(t, dir, `[[runtime.test_lane]]
name = "docs"
match = ["docs/"]
command = "true"

[[runtime.test_lane]]
name = "go"
match = ["internal/**"]
command = "go test"
selector = "go-package"`)

	out, err := validateOutput(t)
	if err != nil {
		t.Fatalf("validate rejected an absent and a declared selector: %v\n%s", err, out)
	}
	if strings.Contains(out, "selector") {
		t.Errorf("neither lane may produce a selector problem, got:\n%s", out)
	}
}

// TestValidateNormalizesSelectorCase: the value goes through
// configenum.Normalize like every other enumerated field, so a hand-edited
// project.toml carrying GO-PACKAGE validates. A hand-rolled == comparison
// against the literal fails here.
func TestValidateNormalizesSelectorCase(t *testing.T) {
	dir := laneFixture(t)
	appendLanes(t, dir, `[[runtime.test_lane]]
name = "go"
match = ["internal/**"]
command = "go test"
selector = "GO-PACKAGE"`)

	out, err := validateOutput(t)
	if err != nil {
		t.Fatalf("validate rejected selector = \"GO-PACKAGE\": %v\n%s", err, out)
	}
}

// TestValidateRejectsUnusableEmptyExitCodes: each of these codes already means
// something else, so a lane declaring one would relabel a different outcome as
// "no tests collected" — the exact confusion the miss verdict exists to
// prevent.
func TestValidateRejectsUnusableEmptyExitCodes(t *testing.T) {
	cases := []struct {
		name   string
		code   string
		needle string
	}{
		// 0 is the runner's success code: every green run would read as a miss.
		{"success", "0", "success code"},
		// remote.Classify spends 255 on ssh transport failure: an unreachable
		// host would read as a lane that found no tests.
		{"ssh-transport", "255", "transport-failure"},
		// Outside the byte a process can exit with, so nothing can return it.
		{"out-of-range", "300", "0-255"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := laneFixture(t)
			appendLanes(t, dir, `[[runtime.test_lane]]
name = "go"
match = ["internal/**"]
command = "go test"
selector = "go-package"
empty_exit = [`+tc.code+`]`)

			out, err := validateOutput(t)
			if err == nil {
				t.Fatalf("validate accepted empty_exit = [%s]:\n%s", tc.code, out)
			}
			if !strings.Contains(out, `runtime.test_lane "go"`) {
				t.Errorf("the refusal must name the lane, got:\n%s", out)
			}
			if !strings.Contains(out, tc.needle) {
				t.Errorf("the refusal must say why %s is unusable (%q), got:\n%s", tc.code, tc.needle, out)
			}
		})
	}
}

// TestValidateRejectsEmptyExitWithoutSelector: an unscoped lane runs its whole
// suite, so a code declared on it can never fire. Accepting it silently would
// leave the user believing they configured a miss they will never see.
func TestValidateRejectsEmptyExitWithoutSelector(t *testing.T) {
	dir := laneFixture(t)
	appendLanes(t, dir, `[[runtime.test_lane]]
name = "go"
match = ["internal/**"]
command = "go test"
empty_exit = [5]`)

	out, err := validateOutput(t)
	if err == nil {
		t.Fatalf("validate accepted empty_exit with no selector:\n%s", out)
	}
	if !strings.Contains(out, `runtime.test_lane "go"`) || !strings.Contains(out, "no selector") {
		t.Errorf("the refusal must name the lane and the missing selector, got:\n%s", out)
	}
}

// TestValidateAcceptsAUsableEmptyExitCode: 5 is pytest's "collected no tests",
// declared on a scoped lane — the combination the feature exists for.
func TestValidateAcceptsAUsableEmptyExitCode(t *testing.T) {
	dir := laneFixture(t)
	appendLanes(t, dir, `[[runtime.test_lane]]
name = "py"
match = ["**/*.py"]
command = "pytest"
selector = "path"
empty_exit = [5]`)

	out, err := validateOutput(t)
	if err != nil {
		t.Fatalf("validate rejected a scoped lane declaring empty_exit = [5]: %v\n%s", err, out)
	}
}

// TestValidateNamesLaneWithWhitespaceOnlyPrepare: a `prepare = "   "` is the
// one shape that disagrees with itself — it survives project.Load non-empty,
// so the lane's consent fingerprint covers it and `dross trust --lane` prints
// a blank line, while every reader that asks "does this lane declare a
// prepare" trims it and says no. `dross test lane add --prepare` normalizes it
// away, so only a hand-edited project.toml reaches here — which is the path
// validate exists for.
func TestValidateNamesLaneWithWhitespaceOnlyPrepare(t *testing.T) {
	dir := laneFixture(t)
	appendLanes(t, dir, `[[runtime.test_lane]]
name = "go"
match = ["internal/**"]
command = "go test"
prepare = "   "`)

	out, err := validateOutput(t)
	if err == nil {
		t.Fatalf("validate accepted a whitespace-only prepare:\n%s", out)
	}
	if !strings.Contains(out, `"go"`) || !strings.Contains(out, "prepare") {
		t.Errorf("problem must name the lane and the field, got:\n%s", out)
	}
}

// TestValidateAcceptsAbsentAndDeclaredPrepares is the opt-in half: neither a
// lane that omits prepare nor one that declares a real line is a problem, or
// validate would invent a fault for every repo written before this phase.
func TestValidateAcceptsAbsentAndDeclaredPrepares(t *testing.T) {
	dir := laneFixture(t)
	appendLanes(t, dir, `[[runtime.test_lane]]
name = "go"
match = ["internal/**"]
command = "go test"

[[runtime.test_lane]]
name = "docs"
match = ["docs/**"]
command = "markdownlint docs"
prepare = "make build"`)

	out, err := validateOutput(t)
	if err != nil {
		t.Errorf("validate rejected usable prepare fields:\n%s", out)
	}
}

// TestValidateNamesLaneWithUnusableToolchainEntries walks the four shapes an
// override entry must not take. Each is asked of a host as `command -v <entry>`
// and none of them can ever resolve, so an accepted entry pins its lane to a
// local run — or refuses to spawn it — with nothing in the transcript pointing
// at the override as the cause.
func TestValidateNamesLaneWithUnusableToolchainEntries(t *testing.T) {
	for _, tc := range []struct {
		name  string
		entry string
		why   string
	}{
		{"blank", `"", "go"`, "no host answers to the empty string"},
		{"command line", `"go test"`, "an entry is one binary, not a command line"},
		{"env assignment", `"FOO=1"`, "the override exists to replace this shape, not carry it"},
		{"path", `"./x"`, "a path resolves against a working directory the two hosts need not share"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := laneFixture(t)
			appendLanes(t, dir, `[[runtime.test_lane]]
name = "go"
match = ["internal/**"]
command = "go test"
toolchain = [`+tc.entry+`]`)

			out, err := validateOutput(t)
			if err == nil {
				t.Fatalf("validate accepted a %s toolchain entry — %s:\n%s", tc.name, tc.why, out)
			}
			if !strings.Contains(out, `"go"`) || !strings.Contains(out, "toolchain") {
				t.Errorf("problem must name the lane and the field, got:\n%s", out)
			}
		})
	}
}

// TestValidateAcceptsAbsentAndDeclaredToolchains is the opt-in half. A lane
// omitting the key derives its toolchain (the locked toolchain_source
// decision), so inventing a problem for it would make an opt-in field
// mandatory by nagging every repo written before this phase.
func TestValidateAcceptsAbsentAndDeclaredToolchains(t *testing.T) {
	dir := laneFixture(t)
	appendLanes(t, dir, `[[runtime.test_lane]]
name = "go"
match = ["internal/**"]
command = "go test"

[[runtime.test_lane]]
name = "web"
match = ["web/**"]
command = "mise exec -- pnpm test"
toolchain = ["mise"]`)

	out, err := validateOutput(t)
	if err != nil {
		t.Errorf("validate rejected a usable toolchain surface:\n%s", out)
	}
}

// TestLaneWithNoToolchainRoundTripsByteIdentically pins the omitempty tag. The
// field is new and every repo on disk predates it, so a Load/Save cycle must
// leave a lane block byte-for-byte as it was — a `toolchain = []` written into
// every existing project.toml would make an opt-in field visible everywhere it
// was never asked for.
func TestLaneWithNoToolchainRoundTripsByteIdentically(t *testing.T) {
	dir := laneFixture(t)
	appendLanes(t, dir, `[[runtime.test_lane]]
name = "go"
match = ["internal/**"]
command = "go test"`)

	path := filepath.Join(dir, ".dross", "project.toml")
	p, err := project.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Save(path); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(saved, []byte("toolchain")) {
		t.Errorf("saving a lane that declares no toolchain wrote the key:\n%s", saved)
	}

	before := saved
	if _, err := project.Load(path); err != nil {
		t.Fatal(err)
	}
	if err := p.Save(path); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("a second save changed the file:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestValidateNamesLaneWithWhitespaceOnlyInstall is the prepare rule one field
// over, and it exists for the same reason: `install = "   "` survives
// project.Load non-empty, so the install line's own consent fingerprint covers
// it and the trust verb prints a blank line, while every reader that asks "does
// this lane declare an install" trims it and says no. `dross test lane add
// --install` normalizes it away, so only a hand-edited project.toml reaches
// here — which is the path validate exists for.
func TestValidateNamesLaneWithWhitespaceOnlyInstall(t *testing.T) {
	dir := laneFixture(t)
	appendLanes(t, dir, `[[runtime.test_lane]]
name = "go"
match = ["internal/**"]
command = "go test"
install = "   "`)

	out, err := validateOutput(t)
	if err == nil {
		t.Fatalf("validate accepted a whitespace-only install:\n%s", out)
	}
	if !strings.Contains(out, `"go"`) || !strings.Contains(out, "install") {
		t.Errorf("problem must name the lane and the field, got:\n%s", out)
	}
}

// TestValidateAcceptsAbsentAndDeclaredInstalls is the opt-in half. A lane with
// NO install key must add ZERO problems, or the rule would invent a fault for
// every lane written before this phase — turning an opt-in field into a nag on
// every repo that already declares lanes.
func TestValidateAcceptsAbsentAndDeclaredInstalls(t *testing.T) {
	dir := laneFixture(t)
	appendLanes(t, dir, `[[runtime.test_lane]]
name = "go"
match = ["internal/**"]
command = "go test"

[[runtime.test_lane]]
name = "docs"
match = ["docs/**"]
command = "markdownlint docs"
install = "npm install -g markdownlint-cli"`)

	out, err := validateOutput(t)
	if err != nil {
		t.Errorf("validate rejected usable install fields:\n%s", out)
	}

	// Asserted at the rule's own layer too, not only through validate's exit
	// status: a lane with no install key must contribute nothing at all, which
	// an aggregate "no error" check would still pass if the problem were
	// reported and something else swallowed it.
	p, err := project.Load(filepath.Join(dir, ".dross", "project.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range laneProblems(p) {
		if strings.Contains(problem, "install") {
			t.Errorf("a lane declaring no install key was reported: %s", problem)
		}
	}
}

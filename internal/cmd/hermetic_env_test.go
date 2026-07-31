package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/defaults"
)

// ambientHome is the HOME this test binary inherited, captured before TestMain
// replaces it. Kept so TestHermeticHome_IsIsolated can assert the replacement
// actually happened rather than trust it.
var ambientHome = os.Getenv("HOME")

// TestMain pins HOME to an empty throwaway directory for the whole package.
//
// Without it, every test that runs `dross init` in a temp repo silently
// inherits the developer's real ~/.claude/dross/defaults.toml: seedRemote
// overlays those defaults onto the git-detected remote (init.go), so a machine
// whose defaults set remote.auth_env = BITBUCKET_TOKEN produced a scaffolded
// repo that doctor called unhealthy unless that token happened to be exported
// in the shell. Five doctor/validate tests were red on every other host. A test
// suite's verdict must not depend on the developer's home directory.
//
// This is the same isolation chdir already applies to DROSS_NO_TELEMETRY and
// CLAUDE_CONFIG_DIR, hoisted to the process because it has to be in place
// before any test body runs. Tests that need a populated home still override
// with t.Setenv("HOME", ...); that restores to this throwaway dir afterwards,
// never to the ambient one.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "dross-test-home-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "hermetic HOME: mkdtemp: %v\n", err)
		os.Exit(1)
	}
	if err := os.Setenv("HOME", home); err != nil {
		fmt.Fprintf(os.Stderr, "hermetic HOME: setenv: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(home)
	os.Exit(code)
}

// TestHermeticHome_IsIsolated pins the TestMain isolation itself, so removing
// it fails here by name instead of surfacing as an unrelated doctor complaint
// on whichever machine has the unlucky defaults.toml.
func TestHermeticHome_IsIsolated(t *testing.T) {
	home := os.Getenv("HOME")
	if home == "" {
		t.Fatal("HOME is empty — TestMain should have pinned it to a throwaway dir")
	}
	if ambientHome != "" && home == ambientHome {
		t.Fatalf("HOME is still the ambient %q, so this package reads the developer's real ~/.claude", ambientHome)
	}

	dir, err := GlobalDir()
	if err != nil {
		t.Fatalf("GlobalDir: %v", err)
	}
	path := filepath.Join(dir, defaults.File)
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("%s must not exist under the test HOME (stat err = %v) — init would overlay it", path, err)
	}
}

// TestHermeticHome_HostileGlobalDefaultsDoNotRedden reproduces the verify
// BLOCKING deterministically: a global defaults.toml naming a token that is not
// exported must not make a scaffolded repo fail its own health check. The
// scaffold configures remote.auth_env itself, so the ambient suggestion is
// never adopted; reverting that leaves init inheriting absentToken and doctor
// reporting it unset.
func TestHermeticHome_HostileGlobalDefaultsDoNotRedden(t *testing.T) {
	const absentToken = "DROSS_TEST_ABSENT_TOKEN"
	t.Setenv(absentToken, "") // empty reads as unset to doctor's os.Getenv check

	home := t.TempDir()
	t.Setenv("HOME", home)
	mustWrite(t, filepath.Join(home, ".claude", "dross", defaults.File),
		"[remote_defaults]\nauth_env = \""+absentToken+"\"\n")

	// scaffoldDoctorRepo fatals if the repo it builds is not doctor-clean.
	dir := scaffoldDoctorRepo(t)

	if body := mustRead(t, filepath.Join(dir, ".dross", "project.toml")); strings.Contains(body, absentToken) {
		t.Errorf("the scaffold adopted the ambient defaults' auth_env %q:\n%s", absentToken, body)
	}
}

// TestHermeticHome_DoctorScaffoldOwnsItsToken keeps the scaffold's two halves
// in step: the env var it exports and the one it writes to project.toml must be
// the same name, or doctor reports an unset token for a var nothing set.
func TestHermeticHome_DoctorScaffoldOwnsItsToken(t *testing.T) {
	dir := scaffoldDoctorRepo(t)

	body := mustRead(t, filepath.Join(dir, ".dross", "project.toml"))
	if want := `auth_env = "` + doctorTokenEnv + `"`; !strings.Contains(body, want) {
		t.Errorf("project.toml does not carry %s:\n%s", want, body)
	}
	if os.Getenv(doctorTokenEnv) == "" {
		t.Errorf("the scaffold must export $%s, or doctor flags it as unset", doctorTokenEnv)
	}
}

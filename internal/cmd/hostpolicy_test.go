package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/hostallow"
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/ship"
)

// The distinction this file exists to protect: a POLICY refusal and a
// TRANSIENT failure look the same at a call site that only checks `err != nil`,
// and dross deliberately degrades gracefully on the second. Collapsing them
// would make an active attack indistinguishable from a flaky network — and,
// worse, silently downgrade capability exactly when someone is attacking.

// TestBoardConfigDerivesFromRemoteURL: boardConfig synthesises a
// "https://board.local/<project>" URL to carry owner/repo to the forge
// backends. Deriving the allowlist from THAT would authorize a host nobody
// configured and make the policy self-satisfying, so the derivation source is
// the real [remote].url.
func TestBoardConfigDerivesFromRemoteURL(t *testing.T) {
	cfg := boardConfig(project.Board{
		Provider: "forgejo",
		BaseURL:  "https://git.corp.internal/api/v1",
		AuthEnv:  "TOKEN",
		Project:  "me/proj",
	}, "https://git.corp.internal/me/proj", nil)

	allowed := strings.Join(cfg.Hosts.Allowed(), " ")
	if !strings.Contains(allowed, "git.corp.internal") {
		t.Errorf("policy does not carry the [remote].url host: %v", cfg.Hosts.Allowed())
	}
	if strings.Contains(allowed, "board.local") {
		t.Errorf("policy was derived from the synthetic board URL: %v", cfg.Hosts.Allowed())
	}
	// And the synthetic URL is still doing its real job.
	if cfg.URL != "https://board.local/me/proj" {
		t.Errorf("synthetic owner/repo URL changed: %q", cfg.URL)
	}
}

// TestBuildOpenOptsCarriesPolicy: an off-allowlist api_base must abort before
// any network call. The seam records whether it was reached at all — an error
// returned after the request is an error the attacker already has a token for.
func TestBuildOpenOptsCarriesPolicy(t *testing.T) {
	p := &project.Project{}
	p.Remote.Provider = "forgejo"
	p.Remote.URL = "https://github.com/Rivil/dross"
	p.Remote.APIBase = "https://attacker.example"
	p.Remote.AuthEnv = "DROSS_POLICY_TEST_TOKEN"
	t.Setenv(p.Remote.AuthEnv, "sentinel-token")

	opts := buildOpenOpts(p, hostallow.Derive(p.Remote.URL, nil))
	opts.HeadBranch = "phase/x"
	opts.BaseBranch = "main"
	opts.Title = "t"

	_, err := ship.OpenPR(opts)
	if err == nil {
		t.Fatal("ship opened a PR against an off-allowlist api_base")
	}
	if !errors.Is(err, hostallow.ErrRefused) {
		t.Errorf("not a policy refusal: %v", err)
	}
	if strings.Contains(err.Error(), "sentinel-token") {
		t.Errorf("the refusal echoed the token: %v", err)
	}
}

// TestTrackedLocalTomlBlocksBoardConfig: a hostile repo's committed local.toml
// must not be able to authorize its own host. The assertion is that the client
// is still refused — not merely that the hosts list came back empty, which a
// "parse it but drop allow_hosts" softening would also satisfy while still
// letting a cloned quick_base ride history.
func TestTrackedLocalTomlBlocksBoardConfig(t *testing.T) {
	dir := hostileRepo(t, "main")
	root := filepath.Join(dir, ".dross")

	if err := os.WriteFile(filepath.Join(root, LocalFile),
		[]byte("allow_hosts = \"attacker.example\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", "-f", ".dross/"+LocalFile)

	extra, err := readAllowHosts(root, dir)
	if err == nil {
		t.Fatal("a tracked local.toml was read rather than refused")
	}
	if extra != nil {
		t.Errorf("extras must be nil on refusal, got %v", extra)
	}

	// And the board client built WITHOUT those extras still refuses the host
	// the repo was trying to authorize.
	cfg := boardConfig(project.Board{
		Provider: "forgejo",
		BaseURL:  "https://attacker.example",
		AuthEnv:  "TOKEN",
		Project:  "me/proj",
	}, "https://github.com/Rivil/dross", nil)
	if cerr := cfg.Hosts.Check("[board].base_url", cfg.APIBase); cerr == nil {
		t.Error("the host the tracked local.toml named was allowed anyway")
	}
}

// TestMergeGateDoesNotDegradeOnHostRefusal is the locked refusal_behaviour
// decision, made executable. Both arms matter: a policy refusal must SURFACE,
// and a genuine transient error must STILL degrade — a test that only checked
// the first would pass equally well if the fallback had been deleted outright,
// which would break every offline completion.
func TestMergeGateDoesNotDegradeOnHostRefusal(t *testing.T) {
	dir := hostileRepo(t, "main")
	mustGit(t, dir, "checkout", "-q", "-b", "phase/01-x")

	opts := ship.OpenOpts{Provider: "forgejo", URL: "https://github.com/Rivil/dross"}

	t.Run("host refusal surfaces", func(t *testing.T) {
		prev := ship.PRStatusFunc
		t.Cleanup(func() { ship.PRStatusFunc = prev })
		ship.PRStatusFunc = func(ship.OpenOpts) (ship.PRStatus, error) {
			return ship.PRStatus{}, fmt.Errorf("wrapped: %w", hostallow.ErrRefused)
		}

		var out string
		err := captureInto(t, &out, func() error {
			return mergeGate(dir, opts, "01-x", "phase/01-x", "main", 7)
		})
		if err == nil {
			t.Fatal("mergeGate proceeded past a host refusal")
		}
		if !errors.Is(err, hostallow.ErrRefused) {
			t.Errorf("the refusal did not surface: %v", err)
		}
		if strings.Contains(out, "falling back to git ancestry") {
			t.Errorf("mergeGate degraded on a host refusal:\n%s", out)
		}
	})

	t.Run("transient error still degrades", func(t *testing.T) {
		prev := ship.PRStatusFunc
		t.Cleanup(func() { ship.PRStatusFunc = prev })
		ship.PRStatusFunc = func(ship.OpenOpts) (ship.PRStatus, error) {
			return ship.PRStatus{}, errors.New("dial tcp: connection refused")
		}

		var out string
		_ = captureInto(t, &out, func() error {
			return mergeGate(dir, opts, "01-x", "phase/01-x", "main", 7)
		})
		if !strings.Contains(out, "falling back to git ancestry") {
			t.Errorf("a transient forge error must still degrade to ancestry:\n%s", out)
		}
	})
}

// TestDependentsCheckDoesNotDegradeOnHostRefusal: the open-PR check gates an
// irreversible branch delete, and its skip path is intentionally permissive
// about forge trouble. A host refusal must not ride that path — the delete
// would proceed on the strength of an answer nobody got.
func TestDependentsCheckDoesNotDegradeOnHostRefusal(t *testing.T) {
	dir := hostileRepo(t, "main")
	ppath := filepath.Join(dir, ".dross", project.File)
	p, err := project.Load(ppath)
	if err != nil {
		t.Fatal(err)
	}
	p.Remote.Provider = "forgejo"
	p.Remote.URL = "https://github.com/Rivil/dross"
	p.Remote.APIBase = "https://attacker.example"
	if err := p.Save(ppath); err != nil {
		t.Fatal(err)
	}

	prev := ship.OpenPRsTargetingFunc
	t.Cleanup(func() { ship.OpenPRsTargetingFunc = prev })
	ship.OpenPRsTargetingFunc = func(ship.OpenOpts, string) ([]ship.BasePR, error) {
		return nil, fmt.Errorf("wrapped: %w", hostallow.ErrRefused)
	}

	var out string
	gerr := captureInto(t, &out, func() error {
		return guardOpenPRsTargeting("milestone/v9.9")
	})
	if gerr == nil {
		t.Fatal("the open-PR check returned nil on a host refusal — the delete would proceed")
	}
	if !errors.Is(gerr, hostallow.ErrRefused) {
		t.Errorf("the refusal did not surface: %v", gerr)
	}
	if strings.Contains(out, "open-PR check skipped") {
		t.Errorf("a host refusal was announced as a skip:\n%s", out)
	}
}

// captureInto runs fn with stdout captured, so a test can assert on what was
// PRINTED as well as what was returned. The degrade paths announce themselves
// and return nil, so the printed text is the only evidence they ran.
func captureInto(t *testing.T, dst *string, fn func() error) error {
	t.Helper()
	var err error
	*dst = captureStdout(t, func() { err = fn() })
	return err
}

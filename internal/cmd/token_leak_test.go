package cmd

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/telemetry"
)

// c-3's proof, owned separately from the two tasks that implement it.
//
// t-3 scrubs the forge clients and t-7 scrubs ship's backends. Both can be
// correct while a fourth code path prints the token — the error goes to stderr,
// into the telemetry JSONL as err_detail, and into whatever the agent pastes
// back. So this test does not check the scrubbers; it checks the SURFACES, by
// driving real failures through the cobra commands and looking at everything
// that came out.
//
// Two canaries, not one. The forge/ship path reads [remote].auth_env and the
// board clients (Jira, YouTrack) read [board].auth_env. A single canary seeded
// into remote.auth_env never drives the board clients at all, so the proof would
// pass while they leaked.

const (
	remoteCanary    = "r3mote-c4n4ry-do-not-leak-11ac"
	boardCanary     = "b04rd-c4n4ry-do-not-leak-77de"
	remoteCanaryEnv = "DROSS_LEAK_REMOTE_TOKEN"
	boardCanaryEnv  = "DROSS_LEAK_BOARD_TOKEN"
)

// mirroringServer answers every request with 403 and echoes the credential
// headers back in the body — the upstream behaviour that turns dross's own
// error message into the exfiltration channel.
func mirroringServer(t *testing.T, hits *int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprintf(w, `{"message":"denied for Authorization=%s PRIVATE-TOKEN=%s"}`,
			r.Header.Get("Authorization"), r.Header.Get("PRIVATE-TOKEN"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// leakFixture wires a repo whose [remote] and [board] both point at
// header-mirroring servers, each authenticated with its own canary.
func leakFixture(t *testing.T, boardProvider string, remoteHits, boardHits *int) (dir, home string, remoteSrv, boardSrv *httptest.Server) {
	t.Helper()
	dir = t.TempDir()
	chdir(t, dir)

	// Telemetry must be ON: it is one of the surfaces under test. chdir pins the
	// opt-out for every other test in this package, so it is undone here, with
	// HOME redirected so the events land in a temp dir and never in the
	// developer's real ~/.claude.
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DROSS_NO_TELEMETRY", "")

	t.Setenv(remoteCanaryEnv, remoteCanary)
	t.Setenv(boardCanaryEnv, boardCanary)

	remoteSrv = mirroringServer(t, remoteHits)
	boardSrv = mirroringServer(t, boardHits)

	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustRunSet(t, "project.name", "leaky")
	mustRunSet(t, "remote.provider", "forgejo")
	mustRunSet(t, "remote.url", "https://forge.example/me/proj")
	mustRunSet(t, "remote.api_base", remoteSrv.URL)
	mustRunSet(t, "remote.auth_env", remoteCanaryEnv)

	mustRunSet(t, "board.provider", boardProvider)
	mustRunSet(t, "board.base_url", boardSrv.URL)
	mustRunSet(t, "board.auth_env", boardCanaryEnv)
	// The forge REST board resolves owner/repo from the project ref, so it needs
	// the "owner/repo" shape; YouTrack and Jira take a short project key.
	boardProject := "PROJ"
	if boardProvider == "forgejo" {
		boardProject = "me/proj"
	}
	mustRunSet(t, "board.project", boardProject)
	if boardProvider == "jira" {
		mustRunSet(t, "board.auth_user", "me@example.com")
	}
	mustRunSet(t, "board.enabled", "true")

	// The allowlist derives from [remote].url, so both loopback hosts are
	// authorized by hand — machine-local, never from committed config.
	if err := runCmd(t, Local(), "set", "allow_hosts",
		remoteSrv.Listener.Addr().String()+","+boardSrv.Listener.Addr().String()); err != nil {
		t.Fatal(err)
	}
	return dir, home, remoteSrv, boardSrv
}

// runLeaky drives one cobra command, capturing stdout, and records the telemetry
// event exactly as main.go does — err_detail carries the redacted message for
// the "other" bucket, so the JSONL is a real emitted surface, not a theoretical
// one.
func runLeaky(t *testing.T, name string, c *cobra.Command, args ...string) leakResult {
	t.Helper()
	var err error
	out := captureStdout(t, func() { err = runCmd(t, c, args...) })
	RecordCLIEvent(c, 5*time.Millisecond, err)
	return leakResult{name: name, out: out, err: err}
}

type leakResult struct {
	name string
	out  string
	err  error
}

// TestTokenReachesNoEmittedSurface is the whole of c-3's proof.
func TestTokenReachesNoEmittedSurface(t *testing.T) {
	for _, boardProvider := range []string{"youtrack", "jira", "forgejo"} {
		t.Run("board="+boardProvider, func(t *testing.T) {
			var remoteHits, boardHits int
			_, home, _, _ := leakFixture(t, boardProvider, &remoteHits, &boardHits)

			var results []leakResult

			// --- board leg: reads [board].auth_env ---
			results = append(results, runLeaky(t, "issue pull", Issue(), "pull"))
			writeSpec(t, mustCwd(t), "01-auth", "[phase]\nid=\"01-auth\"\ntitle=\"Auth\"\n")
			results = append(results, runLeaky(t, "issue phase-sync", Issue(), "phase", "sync", "01-auth"))

			// --- ship / forge leg: reads [remote].auth_env ---
			results = append(results, runLeaky(t, "ship comment",
				Ship(), "comment", "--pr", "3", "--body", "panel findings"))

			sawFailure := false
			for _, r := range results {
				if r.err != nil {
					sawFailure = true
				}
				assertNoCanaries(t, r.name+" error", errText(r.err))
				assertNoCanaries(t, r.name+" stdout", r.out)
			}
			if !sawFailure {
				t.Fatal("no command failed — the servers answer 403, so a clean run means the requests never happened and this test proves nothing")
			}
			// Reaching the server is what makes the leg real. A board client
			// that errored on config before it ever sent a request would leave
			// every canary assertion below trivially true.
			if boardHits == 0 {
				t.Errorf("the %s board client never reached the board server — this leg proves nothing", boardProvider)
			}
			if remoteHits == 0 {
				t.Error("the ship/forge client never reached the remote server — this leg proves nothing")
			}

			// --- telemetry JSONL ---
			jsonl := readTelemetry(t, home)
			if strings.TrimSpace(jsonl) == "" {
				t.Fatal("no telemetry was written — the JSONL surface was not actually exercised")
			}
			assertNoCanaries(t, "telemetry jsonl", jsonl)

			// The other half: the redaction must be VISIBLE somewhere, or a
			// change that stopped making the requests entirely would pass this
			// test by silence.
			combined := jsonl
			for _, r := range results {
				combined += errText(r.err) + r.out
			}
			if !strings.Contains(combined, "[redacted $") {
				t.Errorf("no redaction marker on any surface — reverting t-3/t-7 must fail this test, not pass it:\n%s", combined)
			}
		})
	}
}

func assertNoCanaries(t *testing.T, surface, text string) {
	t.Helper()
	for name, canary := range map[string]string{
		"[remote].auth_env": remoteCanary,
		"[board].auth_env":  boardCanary,
	} {
		if strings.Contains(text, canary) {
			t.Errorf("%s carries the %s token:\n%s", surface, name, text)
		}
		// The ENCODED forms too. Jira's credential reaches the wire as
		// base64(email:token) and never appears literally, so a raw-substring
		// check alone would report that client clean while it leaked a
		// perfectly usable credential.
		for _, user := range []string{"me@example.com", "", "x"} {
			enc := base64.StdEncoding.EncodeToString([]byte(user + ":" + canary))
			if strings.Contains(text, enc) {
				t.Errorf("%s carries base64(%s:%s token):\n%s", surface, user, name, text)
			}
		}
		if enc := base64.StdEncoding.EncodeToString([]byte(canary)); strings.Contains(text, enc) {
			t.Errorf("%s carries base64(%s token):\n%s", surface, name, text)
		}
	}
}

func readTelemetry(t *testing.T, home string) string {
	t.Helper()
	path := filepath.Join(home, ".claude", "dross", telemetry.File)
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

func mustCwd(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return wd
}

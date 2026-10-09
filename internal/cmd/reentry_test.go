package cmd

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/assets"
	"github.com/Rivil/dross/internal/gatestate"
)

func TestReentryNoopOutsideRepo(t *testing.T) {
	chdir(t, t.TempDir())
	out := captureStdout(t, func() {
		if err := runCmd(t, Reentry()); err != nil {
			t.Errorf("want silent exit 0 outside a dross repo, got: %v", err)
		}
	})
	if out != "" {
		t.Errorf("want empty stdout outside a dross repo, got:\n%s", out)
	}
}

// decodeReentry parses `dross reentry` stdout as the SessionStart hook JSON
// envelope, pins the envelope invariants (event name, systemMessage ==
// additionalContext), and returns the embedded re-entry line.
func decodeReentry(t *testing.T, out string) string {
	t.Helper()
	var env sessionStartOutput
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("reentry stdout is not the hook JSON envelope: %v\nstdout:\n%s", err, out)
	}
	if env.HookSpecificOutput.HookEventName != "SessionStart" {
		t.Errorf("want hookEventName %q, got %q", "SessionStart", env.HookSpecificOutput.HookEventName)
	}
	if env.SystemMessage != env.HookSpecificOutput.AdditionalContext {
		t.Errorf("user-visible and model-visible lines diverged:\nsystemMessage:     %q\nadditionalContext: %q",
			env.SystemMessage, env.HookSpecificOutput.AdditionalContext)
	}
	if env.SystemMessage == "" {
		t.Fatal("envelope carries an empty re-entry line")
	}
	return env.SystemMessage
}

// TestReentryMatchesSuggestNext pins the line to the same heuristic status
// uses: the named command must track suggestNext across lifecycle stages.
func TestReentryMatchesSuggestNext(t *testing.T) {
	t.Run("spec only suggests plan", func(t *testing.T) {
		chdir(t, t.TempDir())
		scaffoldPhaseWithSpecOnly(t, "01-auth")
		out := captureStdout(t, func() {
			if err := runCmd(t, Reentry()); err != nil {
				t.Fatal(err)
			}
		})
		line := decodeReentry(t, out)
		if !strings.HasPrefix(line, "you are here: 01-auth") {
			t.Errorf("want 'you are here' + phase, got:\n%s", line)
		}
		if !strings.Contains(line, "/dross-plan") {
			t.Errorf("spec-only phase should point at /dross-plan:\n%s", line)
		}
	})

	t.Run("runnable task suggests execute", func(t *testing.T) {
		chdir(t, t.TempDir())
		scaffoldPhaseWithSpecAndPlan(t, "01-auth", `[phase]
id = "01-auth"
[[task]]
id = "t-1"
wave = 1
title = "schema"
files = ["x.ts"]
covers = ["c-1"]
`)
		out := captureStdout(t, func() {
			if err := runCmd(t, Reentry()); err != nil {
				t.Fatal(err)
			}
		})
		if line := decodeReentry(t, out); !strings.Contains(line, "/dross-execute") {
			t.Errorf("runnable task should point at /dross-execute:\n%s", line)
		}
	})

	t.Run("partial verify suggests execute on the phase", func(t *testing.T) {
		chdir(t, t.TempDir())
		scaffoldPhaseWithSpecAndPlan(t, "01-auth", `[phase]
id = "01-auth"
[[task]]
id = "t-1"
wave = 1
title = "schema"
files = ["x.ts"]
covers = ["c-1"]
status = "done"
`)
		mustWrite(t, ".dross/phases/01-auth/verify.toml", `[verify]
phase = "01-auth"
verdict = "partial"
`)
		out := captureStdout(t, func() {
			if err := runCmd(t, Reentry()); err != nil {
				t.Fatal(err)
			}
		})
		line := decodeReentry(t, out)
		if !strings.Contains(line, "verify is partial") {
			t.Errorf("partial verdict should be named:\n%s", line)
		}
		if !strings.Contains(line, "/dross-execute 01-auth") {
			t.Errorf("partial verdict should name the exact re-entry command '/dross-execute 01-auth':\n%s", line)
		}
	})

	t.Run("pass verify with recorded changes suggests ship", func(t *testing.T) {
		chdir(t, t.TempDir())
		scaffoldPhaseWithSpecAndPlan(t, "01-auth", `[phase]
id = "01-auth"
[[task]]
id = "t-1"
wave = 1
title = "schema"
files = ["x.ts"]
covers = ["c-1"]
status = "done"
`)
		mustWrite(t, ".dross/phases/01-auth/verify.toml", `[verify]
phase = "01-auth"
verdict = "pass"
`)
		mustWrite(t, ".dross/phases/01-auth/changes.json", `{"phase":"01-auth","tasks":{"t-1":{"files":["x.ts"]}}}`)
		out := captureStdout(t, func() {
			if err := runCmd(t, Reentry()); err != nil {
				t.Fatal(err)
			}
		})
		if line := decodeReentry(t, out); !strings.Contains(line, "/dross-ship") {
			t.Errorf("pass verdict with unshipped changes should point at /dross-ship:\n%s", line)
		}
	})
}

// TestStatusEndsWithReentryLine pins c-4: the LAST printed line of
// `dross status` is byte-equal to the line embedded in `dross reentry`'s hook
// envelope. Asserting on the last line (not a substring) catches a regression
// that buries the re-entry line mid-output.
func TestStatusEndsWithReentryLine(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithSpecOnly(t, "01-auth")

	statusOut := captureStdout(t, func() {
		if err := runCmd(t, Status()); err != nil {
			t.Fatal(err)
		}
	})
	reentryOut := captureStdout(t, func() {
		if err := runCmd(t, Reentry()); err != nil {
			t.Fatal(err)
		}
	})

	statusLines := strings.Split(strings.TrimRight(statusOut, "\n"), "\n")
	last := statusLines[len(statusLines)-1]
	want := decodeReentry(t, reentryOut)
	if last != want {
		t.Errorf("status's last line != reentry envelope line:\nstatus last: %q\nreentry:     %q", last, want)
	}
}

// TestReentrySilentOnIncompleteRoot (c-2): reentry inherits the silence from
// FindRoot for free, which is exactly why nothing else would catch a
// regression that made it loud — it is the one c-2 command with no other
// silent-case assertion.
func TestReentrySilentOnIncompleteRoot(t *testing.T) {
	dir := realTempDir(t)
	mkRoot(t, dir) // incomplete: no project.toml
	chdir(t, dir)

	var err error
	out := captureStdout(t, func() { err = runCmd(t, Reentry()) })
	if err != nil {
		t.Errorf("want silent exit 0 on an incomplete root, got: %v", err)
	}
	if out != "" {
		t.Errorf("want empty stdout on an incomplete root, got:\n%s", out)
	}
}

// TestReentryLoudOnCorruptProject (locked completeness_check): the SessionStart
// hook stays loud on broken state, rather than folding it into the ErrNoRoot
// silence.
func TestReentryLoudOnCorruptProject(t *testing.T) {
	dir := realTempDir(t)
	root := mkRoot(t, dir, "state.json")
	if err := os.WriteFile(filepath.Join(root, "project.toml"), []byte("[[[not toml"), 0o644); err != nil {
		t.Fatal(err)
	}
	chdir(t, dir)

	err := runCmd(t, Reentry())
	if err == nil {
		t.Fatal("reentry should fail on an undecodable project.toml")
	}
	if !strings.Contains(err.Error(), "project.toml") {
		t.Errorf("error should name project.toml, got %v", err)
	}
}

// TestReentryLineTerminalRegionMatchesStatus extends the byte-equality contract
// to the region t-4 rewrote. TestStatusEndsWithReentryLine pins it on a
// spec-only phase, which exits suggestNext long before the merge oracle, the
// unfinalized-verdict arm or the removed terminal branch — so it would stay
// green through a divergence in any of them.
func TestReentryLineTerminalRegionMatchesStatus(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T) string
	}{
		{"merged PR, no completion record", func(t *testing.T) string {
			dir := terminalPhaseFixture(t, "auth", 42)
			mustGit(t, dir, "merge", "-q", "--no-ff", "-m", "merge auth", "phase/auth")
			mustGit(t, dir, "push", "-q", "origin", "main")
			return dir
		}},
		{"open PR", func(t *testing.T) string {
			return terminalPhaseFixture(t, "auth", 42)
		}},
		{"pass verdict, no PR", func(t *testing.T) string {
			return terminalPhaseFixture(t, "auth", 0)
		}},
		{"unfinalized verdict", func(t *testing.T) string {
			dir := terminalPhaseFixture(t, "auth", 0)
			mustWrite(t, filepath.Join(dir, ".dross", "phases", "auth", "verify.toml"),
				"[verify]\nphase = \"auth\"\nverdict = \"\"\n")
			return dir
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.setup(t)

			statusOut := captureStdout(t, func() {
				if err := runCmd(t, Status()); err != nil {
					t.Fatal(err)
				}
			})
			reentryOut := captureStdout(t, func() {
				if err := runCmd(t, Reentry()); err != nil {
					t.Fatal(err)
				}
			})

			statusLines := strings.Split(strings.TrimRight(statusOut, "\n"), "\n")
			last := statusLines[len(statusLines)-1]
			want := decodeReentry(t, reentryOut)
			if last != want {
				t.Errorf("status's last line != reentry envelope line:\nstatus last: %q\nreentry:     %q", last, want)
			}
		})
	}
}

// reentryWith runs `dross reentry` with payload on stdin, the hook's way,
// and returns its stdout.
func reentryWith(t *testing.T, stdin io.Reader) string {
	t.Helper()
	orig := reentryStdinIsTTY
	t.Cleanup(func() { reentryStdinIsTTY = orig })
	reentryStdinIsTTY = func() bool { return false }
	return captureStdout(t, func() {
		c := Reentry()
		c.SetIn(stdin)
		c.SetArgs(nil)
		if err := c.Execute(); err != nil {
			t.Fatalf("reentry: %v", err)
		}
	})
}

func payload(sid, source string) io.Reader {
	return strings.NewReader(`{"session_id":"` + sid + `","hook_event_name":"SessionStart","source":"` + source + `"}`)
}

// showEpoch is the repo's current show epoch, "" when none.
func showEpoch(t *testing.T, repo string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repo, ".dross", "gate", gatestate.ShowDir, "epoch"))
	if err != nil {
		return ""
	}
	return string(b)
}

func TestClearThenShowPrintsFull(t *testing.T) {
	showRepoFixture(t)
	ruleShowOut(t)
	if out := ruleShowOut(t); out != RulesUnchanged+"\n" {
		t.Fatalf("the repeat before /clear printed %q, want the marker", out)
	}
	// /clear hands the hook a NEW session id while the environment still
	// carries the old one.
	reentryWith(t, payload("after-clear", "clear"))
	if out := ruleShowOut(t); !strings.Contains(out, "<rules>") {
		t.Fatalf("after /clear the show printed %q, want the full block", out)
	}
}

func TestCompactThenShowPrintsFull(t *testing.T) {
	showRepoFixture(t)
	ruleShowOut(t)
	interactionShowOut(t)
	reentryWith(t, payload("s", "compact"))
	if out := ruleShowOut(t); !strings.Contains(out, "<rules>") {
		t.Fatalf("after compaction rule show printed %q, want the full block", out)
	}
	if out := interactionShowOut(t); out != assets.InteractionPlaybook {
		t.Fatalf("after compaction interaction show printed %q, want the playbook", out[:min(len(out), 80)])
	}
}

func TestReentryBumpsEverySource(t *testing.T) {
	dir := showRepoFixture(t)
	for _, source := range []string{"startup", "resume", "clear", "compact"} {
		before := showEpoch(t, dir)
		sid := "sid-" + source
		reentryWith(t, payload(sid, source))
		if after := showEpoch(t, dir); after == "" || after == before {
			t.Errorf("source %s left the epoch at %q", source, after)
		}
		if err := gatestate.RecordShown(dir, sid, gatestate.ShowRules, "h"); err != nil {
			t.Fatal(err)
		}
		if !gatestate.ShownCurrent(dir, sid, gatestate.ShowRules, "h") {
			t.Errorf("source %s did not mark %s seen", source, sid)
		}
	}
}

// readCounter records whether stdin was read at all.
type readCounter struct{ reads int }

func (r *readCounter) Read([]byte) (int, error) { r.reads++; return 0, io.EOF }

func TestReentryOpenPipeReturns(t *testing.T) {
	dir := showRepoFixture(t)
	want := captureStdout(t, func() {
		if err := runCmd(t, Reentry()); err != nil {
			t.Fatal(err)
		}
	})
	if err := os.RemoveAll(filepath.Join(dir, ".dross", "gate", gatestate.ShowDir)); err != nil {
		t.Fatal(err)
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close() // never written, closed only after the hook returned
	start := time.Now()
	got := reentryWith(t, r)
	if d := time.Since(start); d >= 500*time.Millisecond {
		t.Fatalf("reentry with a stdin that never closes took %s, want < 500ms", d)
	}
	if got != want {
		t.Fatalf("the envelope moved:\n got %s\nwant %s", got, want)
	}
	if showEpoch(t, dir) != "" {
		t.Fatal("a payload that never arrived bumped the epoch")
	}

	// A terminal on stdin is a human, not a hook: it is not read at all.
	orig := reentryStdinIsTTY
	t.Cleanup(func() { reentryStdinIsTTY = orig })
	reentryStdinIsTTY = func() bool { return true }
	rc := &readCounter{}
	captureStdout(t, func() {
		c := Reentry()
		c.SetIn(rc)
		c.SetArgs(nil)
		if err := c.Execute(); err != nil {
			t.Fatal(err)
		}
	})
	if rc.reads != 0 {
		t.Fatalf("a terminal stdin was read %d times", rc.reads)
	}
}

// TestReentryGoldenWithPayload: the envelope is byte-identical to the pinned
// golden when the hook hands it a payload.
func TestReentryGoldenWithPayload(t *testing.T) {
	pkgDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	norm := outputFixture(t)
	out := reentryWith(t, payload("golden", "startup"))
	checkOutputGolden(t, pkgDir, "reentry", norm(out))
}

func TestReentryBadPayload(t *testing.T) {
	for _, tc := range []struct {
		name  string
		stdin func() io.Reader
		setup func(t *testing.T, dir string)
	}{
		{"not json", func() io.Reader { return strings.NewReader("not json at all") }, nil},
		{"10 MiB", func() io.Reader {
			return strings.NewReader(`{"session_id":"big","pad":"` + strings.Repeat("x", 10<<20) + `"}`)
		}, nil},
		{"unwritable gate", func() io.Reader { return payload("s2", "startup") }, func(t *testing.T, dir string) {
			gate := filepath.Join(dir, ".dross", "gate")
			if err := os.RemoveAll(gate); err != nil {
				t.Fatal(err)
			}
			mustWrite(t, gate, "not a directory\n")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := showRepoFixture(t)
			if err := os.RemoveAll(filepath.Join(dir, ".dross", "gate", gatestate.ShowDir)); err != nil {
				t.Fatal(err)
			}
			want := captureStdout(t, func() {
				if err := runCmd(t, Reentry()); err != nil {
					t.Fatal(err)
				}
			})
			if tc.setup != nil {
				tc.setup(t, dir)
			}
			if got := reentryWith(t, tc.stdin()); got != want {
				t.Fatalf("the envelope moved:\n got %s\nwant %s", got, want)
			}
			if showEpoch(t, dir) != "" {
				t.Fatal("a bad payload bumped the epoch")
			}
		})
	}
}

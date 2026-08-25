package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

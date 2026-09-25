package localstore

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/consent"
	"github.com/Rivil/dross/internal/remote"
)

// store_test.go holds the store's own tests, moved here from internal/cmd with
// the store in cmd-exec-baseline-drain. Assertions are unchanged; only the
// fixture setup is local — a bare .dross dir, since nothing here resolves a
// project root.

// storeRoot returns a fresh .dross dir under a temp repo dir. The repo dir is
// not a git work tree, so the tracked-store refusal reads it as untracked.
func storeRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), ".dross")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

// firstRemoteGrant is candidate zero of ReadRemoteGrants, or nil when there is
// no grant — the scalar question the alias tests ask.
func firstRemoteGrant(root, repoDir string) (*remote.Target, error) {
	ts, err := ReadRemoteGrants(root, repoDir)
	if err != nil || len(ts) == 0 {
		return nil, err
	}
	return ts[0], nil
}

// gitInit makes dir a git work tree, with an origin when originURL is set.
func gitInit(t *testing.T, dir, originURL string) {
	t.Helper()
	cmds := [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"config", "commit.gpgsign", "false"},
		{"config", "gc.auto", "0"},
	}
	if originURL != "" {
		cmds = append(cmds, []string{"remote", "add", "origin", originURL})
	}
	for _, args := range cmds {
		mustGit(t, dir, args...)
	}
}

// mustGit runs git in dir and fails the test on a non-zero exit.
func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// sampleDetachedRun is the fully-populated record every round-trip assertion
// compares against. Every field is set to a DISTINCT non-zero value, so a
// helper that dropped one — or that wrote two fields from the same source —
// fails rather than passing on a coincidence of empty strings.
func sampleDetachedRun() DetachedRun {
	return DetachedRun{
		Phase:        "remote-run-detach",
		RunID:        "r-20260830-2201",
		Host:         "helicon",
		Workdir:      "/var/lib/buildcache/src/dross",
		RunDir:       "/var/lib/buildcache/src/dross/.dross-runs/r-20260830-2201",
		DispatchedAt: time.Date(2026, 8, 30, 22, 1, 5, 0, time.UTC),
		ScheduledFor: time.Date(2026, 8, 31, 2, 0, 0, 0, time.UTC),
		State:        "scheduled",
	}
}

// writeLocalStore drops a raw local.toml body at the fixture root. Raw TOML
// rather than the typed writer on purpose: these tests are about what an
// on-disk file from a PREVIOUS version of dross resolves to, and a file only
// the current writer can produce would prove nothing about the one already on
// the user's machine.
func writeLocalStore(t *testing.T, root, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, File), []byte(body), 0o600); err != nil {
		t.Fatalf("write local.toml: %v", err)
	}
}

// TestDetachedRunRoundTripsEveryField is the record's basic contract: what the
// dispatch writes, a later process reads back.
//
// It matters more here than for an ordinary config key because the reader is a
// DIFFERENT session — that is the whole point of the phase. A field silently
// dropped on the way in is not discovered until a fetch hours later cannot find
// the run, by which time the leg has already been paid for.
//
// Compared field-for-field rather than with a single struct equality, so a
// failure names which field was lost.
func TestDetachedRunRoundTripsEveryField(t *testing.T) {
	root := storeRoot(t)
	repoDir := filepath.Dir(root)
	want := sampleDetachedRun()

	if err := RecordDetachedRun(root, repoDir, want); err != nil {
		t.Fatalf("record: %v", err)
	}

	got, err := FindDetachedRun(root, repoDir, want.Phase)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got == nil {
		t.Fatal("the recorded run was not found")
	}
	for _, f := range []struct {
		name      string
		got, want any
	}{
		{"Phase", got.Phase, want.Phase},
		{"RunID", got.RunID, want.RunID},
		{"Host", got.Host, want.Host},
		{"Workdir", got.Workdir, want.Workdir},
		{"RunDir", got.RunDir, want.RunDir},
		{"State", got.State, want.State},
	} {
		if f.got != f.want {
			t.Errorf("%s did not round-trip: got %v want %v", f.name, f.got, f.want)
		}
	}
	// Times compared with Equal rather than ==: a TOML decode may return a
	// different *location carrying the same instant, and == on time.Time
	// compares the monotonic/location fields too.
	if !got.DispatchedAt.Equal(want.DispatchedAt) {
		t.Errorf("DispatchedAt did not round-trip: got %v want %v", got.DispatchedAt, want.DispatchedAt)
	}
	if !got.ScheduledFor.Equal(want.ScheduledFor) {
		t.Errorf("ScheduledFor did not round-trip: got %v want %v", got.ScheduledFor, want.ScheduledFor)
	}
}

// TestSecondRunForOnePhaseIsRefused is the one_run_per_phase decision made
// mechanical.
//
// Two live runs both write the phase's tests.json when collected, and the loser
// wins silently: whichever fetch lands second overwrites the first with numbers
// from a different dispatch, at a different time, possibly on a different host.
// The refusal must NAME the run already in flight — a bare "already exists"
// leaves the user with no way to find what to cancel.
func TestSecondRunForOnePhaseIsRefused(t *testing.T) {
	root := storeRoot(t)
	repoDir := filepath.Dir(root)
	first := sampleDetachedRun()
	if err := RecordDetachedRun(root, repoDir, first); err != nil {
		t.Fatalf("record: %v", err)
	}

	second := first
	second.RunID = "r-20260830-2359"
	second.Host = "anachryon"
	err := RecordDetachedRun(root, repoDir, second)
	if err == nil {
		t.Fatal("a second detached run for one phase was accepted")
	}
	for _, want := range []string{first.RunID, first.Host, first.Phase} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}

	// The refusal must not have half-written: the stored run is still the
	// first one, not the second and not both.
	runs, err := ReadDetachedRuns(root, repoDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].RunID != first.RunID {
		t.Errorf("the refused record mutated the store: %+v", runs)
	}
}

// TestNewKeyWinsOverAlias pins the direction of the fallback.
//
// A store carrying both generations is half-migrated — someone re-granted
// through the new verb while the old keys were still on disk — and the value
// they most recently authorized is the new one. Resolving the other way would
// run their code on a box they had already moved off.
func TestNewKeyWinsOverAlias(t *testing.T) {
	root := storeRoot(t)
	writeLocalStore(t, root, strings.Join([]string{
		`remote_host = "newbox"`,
		`remote_workdir = "/srv/new"`,
		`mutation_remote_host = "oldbox"`,
		`mutation_remote_workdir = "/srv/old"`,
		"",
	}, "\n"))

	target, err := firstRemoteGrant(root, filepath.Dir(root))
	if err != nil {
		t.Fatalf("firstRemoteGrant: %v", err)
	}
	if target == nil {
		t.Fatal("a store carrying both key generations resolved to no target")
	}
	if target.Host != "newbox" {
		t.Errorf("Host = %q, want newbox — the newer grant is the one the user authorized last", target.Host)
	}
	if target.Workdir != "/srv/new" {
		t.Errorf("Workdir = %q, want /srv/new — host and workdir resolve as a pair, or a path from one generation lands on a machine from the other", target.Workdir)
	}
}

// TestRemoteGrantPairResolvesTogether is the mixed-file case the pair rule
// exists for: new host, old workdir. Taking the workdir from the alias would
// point the run at a path on a machine that was never granted with it.
//
// Refusing is the correct outcome, not merely an acceptable one — the pair
// resolves from one generation, so a new host with no new workdir has no
// workdir at all, and `Target.Validate` says so by name. What must never happen
// is a target that silently splices the two halves together.
func TestRemoteGrantPairResolvesTogether(t *testing.T) {
	root := storeRoot(t)
	writeLocalStore(t, root, strings.Join([]string{
		`remote_host = "newbox"`,
		`mutation_remote_workdir = "/srv/old"`,
		"",
	}, "\n"))

	target, err := firstRemoteGrant(root, filepath.Dir(root))
	if target != nil && target.Workdir == "/srv/old" {
		t.Fatalf("a new host was paired with the deprecated workdir %q — the pair must resolve from one generation", target.Workdir)
	}
	if err == nil {
		t.Fatalf("a half-migrated store resolved cleanly to %+v — it must refuse rather than guess which half is current", target)
	}
	if !strings.Contains(err.Error(), "workdir") {
		t.Errorf("the refusal does not name the missing half: %v", err)
	}
}

// TestUnreadableStoreIsNotASilentLocalRun: every other reader of local.toml
// treats a decode failure as "no value". A trust-bearing key cannot — "I could
// not read your config" must never resolve to a local run the user thought was
// remote.
func TestUnreadableStoreIsNotASilentLocalRun(t *testing.T) {
	root := storeRoot(t)
	writeLocalStore(t, root, "this is not ][ valid toml\n")

	target, err := firstRemoteGrant(root, filepath.Dir(root))
	if err == nil {
		t.Fatalf("an unparseable local.toml resolved cleanly to target=%v — a broken grant must error, not degrade to a local run", target)
	}
	if target != nil {
		t.Errorf("a failed read returned a target: %+v", target)
	}
}

// TestReadRemoteGrantRefusesTrackedLocal is c-2's machine-local half.
//
// A committed local.toml naming a remote host is a repo shipping the machine it
// wants your working tree rsync'd to and your test suite run on. The refusal
// fires UNREAD — the same provenance check allow_hosts and the exec consent gate
// go through, not a second one that could drift.
func TestReadRemoteGrantRefusesTrackedLocal(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "")
	root := filepath.Join(dir, ".dross")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	// A VALID grant, so the test proves provenance is what refuses it rather
	// than the value happening to be malformed.
	body := "mutation_remote_host = \"attacker.example\"\nmutation_remote_workdir = \"/srv/dross\"\n"
	local := filepath.Join(root, File)
	if err := os.WriteFile(local, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", "-f", ".dross/"+File)

	got, err := firstRemoteGrant(root, dir)
	if err == nil {
		t.Fatal("a tracked local.toml was read rather than refused")
	}
	if got != nil {
		t.Errorf("grant must be nil on refusal, got %+v", got)
	}
	if strings.Contains(err.Error(), "attacker.example") {
		t.Errorf("the refusal echoed the file's contents — it must not be parsed: %v", err)
	}
	for _, want := range []string{"refusing to read", ".dross/" + File, "tracked"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not mention %q: %v", want, err)
		}
	}

	// Untracked, the same file grants: the refusal is about provenance, not
	// about the value.
	mustGit(t, dir, "rm", "--cached", "-q", ".dross/"+File)
	got, err = firstRemoteGrant(root, dir)
	if err != nil {
		t.Fatalf("an untracked local.toml must be readable: %v", err)
	}
	if got == nil || got.Host != "attacker.example" || got.Workdir != "/srv/dross" {
		t.Errorf("grant did not parse: %+v", got)
	}
}

// TestLocalStoreRoundTripsGrants pins the persistence seam behind
// internal/consent: Store embeds consent.Grants, and GrantStore is
// the ONE writer of local.toml. A local.toml seeded with foreign keys
// (quick_base, mutation_remote_host, a [[detached_run]]) and every trusted_*
// key must, after GrantConsent through GrantStore(root), still decode every
// foreign key byte-identical and read every grant back. A second writer, or
// a toml tag drifting on either side of the embed, drops a key here.
func TestLocalStoreRoundTripsGrants(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".dross")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	seeded := `quick_base = "main"
mutation_remote_host = "helicon"
mutation_remote_workdir = "/srv/dross"
mutation_workers = "6"
trusted_test_command = "aaaa"
trusted_replay_commands = "bbbb,cccc"
trusted_run_commands = "dddd"

[trusted_lane_commands]
  go = "eeee"
  web = "ffff"

[trusted_lane_installs]
  web = "gggg"

[[detached_run]]
  phase = "p"
  run_id = "r1"
  host = "helicon"
  workdir = "/srv/dross"
  run_dir = "/srv/dross/.dross-runs/r1"
  dispatched_at = 2026-09-20T10:00:00Z
  state = "running"
`
	mustWrite(t, Path(root), seeded)

	before, err := Load(Path(root))
	if err != nil {
		t.Fatal(err)
	}
	// Every grant reads back through the embed.
	if before.TrustedTestCommand != "aaaa" || before.TrustedReplayCommands != "bbbb,cccc" || before.TrustedRunCommands != "dddd" ||
		before.TrustedLaneCommands["go"] != "eeee" || before.TrustedLaneCommands["web"] != "ffff" || before.TrustedLaneInstalls["web"] != "gggg" {
		t.Fatalf("seeded grants did not decode through the embedded consent.Grants: %+v", before.Grants)
	}

	const cmd = "go test ./..."
	if err := consent.GrantConsent(GrantStore(root), cmd); err != nil {
		t.Fatalf("GrantConsent through GrantStore: %v", err)
	}
	// The lane grants ride the same writer (t-5): each one lands beside the
	// others without disturbing a foreign key either.
	if err := consent.GrantLaneConsent(GrantStore(root), "docs", "markdownlint docs"); err != nil {
		t.Fatalf("GrantLaneConsent through GrantStore: %v", err)
	}
	if err := consent.GrantLaneInstallConsent(GrantStore(root), "go", "go install x@latest"); err != nil {
		t.Fatalf("GrantLaneInstallConsent through GrantStore: %v", err)
	}

	after, err := Load(Path(root))
	if err != nil {
		t.Fatal(err)
	}
	if after.TrustedTestCommand != consent.Fingerprint(cmd) {
		t.Errorf("trusted_test_command = %q, want the new fingerprint", after.TrustedTestCommand)
	}
	// Every OTHER grant survives the writes, and the lane grants landed.
	if after.TrustedReplayCommands != "bbbb,cccc" || after.TrustedRunCommands != "dddd" ||
		after.TrustedLaneCommands["go"] != "eeee" || after.TrustedLaneCommands["web"] != "ffff" || after.TrustedLaneInstalls["web"] != "gggg" {
		t.Errorf("a sibling grant was dropped by a grant: %+v", after.Grants)
	}
	if after.TrustedLaneCommands["docs"] != consent.Fingerprint("markdownlint docs") || after.TrustedLaneInstalls["go"] != consent.Fingerprint("go install x@latest") {
		t.Errorf("the lane grants did not land through the single writer: %+v", after.Grants)
	}
	// And every foreign key is byte-identical to what was seeded.
	after.Grants = before.Grants
	if !reflect.DeepEqual(after, before) {
		t.Errorf("a non-consent key changed under a grant:\n before: %+v\n after:  %+v", before, after)
	}
	body := mustRead(t, Path(root))
	for _, want := range []string{`quick_base = "main"`, `mutation_remote_host = "helicon"`, `mutation_workers = "6"`, `run_id = "r1"`, `[trusted_lane_installs]`, `web = "gggg"`} {
		if !strings.Contains(body, want) {
			t.Errorf("local.toml lost %q after a grant:\n%s", want, body)
		}
	}
	// GrantStore is consent.Store: Load sees the file's grants, Save writes
	// only them.
	g, err := GrantStore(root).Load()
	if err != nil || g.TrustedTestCommand != consent.Fingerprint(cmd) {
		t.Errorf("GrantStore.Load = %+v, %v", g, err)
	}
}

// TestSaveBytesArePinned: the one writer of local.toml keeps the file's exact
// layout — key order, table order, two-space indent. The store moved out of
// internal/cmd with its encoder unchanged, and a later edit that reordered a
// field or dropped the indent would rewrite every machine's local.toml on its
// next save; these are the bytes the pre-move writer produced.
func TestSaveBytesArePinned(t *testing.T) {
	path := filepath.Join(storeRoot(t), File)
	s := &Store{
		QuickBase: "main", AllowHosts: "forge.example", RemoteHost: "helicon", RemoteWorkdir: "/srv/dross",
		RemotePool:      []RemoteCandidate{{Host: "anachryon", Workdir: "/srv/pool"}},
		MutationWorkers: "6", MutationRemoteEnv: "NODE_ENV",
		Grants: consent.Grants{TrustedTestCommand: "aaaa", TrustedLaneCommands: map[string]string{"go": "bbbb"}},
		DetachedRuns: []DetachedRun{{Phase: "p", RunID: "r1", Host: "helicon", Workdir: "/srv/dross", RunDir: "/srv/dross/.dross-runs/r1",
			DispatchedAt: time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC), State: "running"}},
	}
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	const want = `quick_base = "main"
allow_hosts = "forge.example"
trusted_test_command = "aaaa"
remote_host = "helicon"
remote_workdir = "/srv/dross"
mutation_workers = "6"
mutation_remote_env = "NODE_ENV"

[trusted_lane_commands]
  go = "bbbb"

[[remote_pool]]
  host = "anachryon"
  workdir = "/srv/pool"

[[detached_run]]
  phase = "p"
  run_id = "r1"
  host = "helicon"
  workdir = "/srv/dross"
  run_dir = "/srv/dross/.dross-runs/r1"
  dispatched_at = 2026-09-20T10:00:00Z
  state = "running"
`
	if got := mustRead(t, path); got != want {
		t.Errorf("local.toml bytes changed:\n--- got\n%s\n--- want\n%s", got, want)
	}
	back, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back.RemotePool, s.RemotePool) || back.QuickBase != s.QuickBase || back.Grants.TrustedLaneCommands["go"] != "bbbb" {
		t.Errorf("the pinned file does not load back: %+v", back)
	}
}

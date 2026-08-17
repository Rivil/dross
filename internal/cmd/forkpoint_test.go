package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/changes"
)

// readRecordedForkPoint reads the fork-point SHA out of a phase's changes.json
// — off disk, deliberately, so a test asserting the cache is asserting what was
// written rather than what an in-memory value happened to hold.
func readRecordedForkPoint(t *testing.T, dir, phaseID string) string {
	t.Helper()
	c, err := changes.Load(changes.FilePath(filepath.Join(dir, ".dross"), phaseID), phaseID)
	if err != nil {
		t.Fatalf("load changes for %s: %v", phaseID, err)
	}
	return c.BaseCommit
}

// TestPhaseCreateRecordsForkPoint is c-1: a phase carries a durable commit, not
// just the moving branch name, from the moment its branch exists.
func TestPhaseCreateRecordsForkPoint(t *testing.T) {
	dir := initWithGit(t)

	if err := runCmd(t, Phase(), "create", "auth"); err != nil {
		t.Fatalf("create: %v", err)
	}

	got := readRecordedForkPoint(t, dir, "auth")
	if got == "" {
		t.Fatal("changes.json recorded no base_commit — a base branch alone is a moving target, not a fork point")
	}
	if want := mustGit(t, dir, "rev-parse", "main"); got != want {
		t.Errorf("recorded fork point = %q, want main's tip at fork time %q", got, want)
	}
	if base := readRecordedBase(t, dir, "auth"); base != "main" {
		t.Errorf("recorded base = %q, want %q — the fork point must not displace the branch name", base, "main")
	}
}

// TestPhaseCreateRecordsMilestoneForkPoint pins that the recorded SHA follows
// the base actually forked from. Under a milestone that is milestone/<version>,
// whose tip is a different commit from main's — recording main's tip here would
// be a fork point the phase never forked from.
func TestPhaseCreateRecordsMilestoneForkPoint(t *testing.T) {
	dir := initWithGit(t)
	if err := runCmd(t, State(), "set", "current_milestone", "v0.9"); err != nil {
		t.Fatalf("state set: %v", err)
	}
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "--allow-empty", "-m", "scope v0.9")
	mustGit(t, dir, "branch", "milestone/v0.9")
	// Move main on so the two tips differ and the assertion can tell them apart.
	mustGit(t, dir, "commit", "-q", "--allow-empty", "-m", "chore: main moves on")

	if err := runCmd(t, Phase(), "create", "auth"); err != nil {
		t.Fatalf("create: %v", err)
	}

	got := readRecordedForkPoint(t, dir, "auth")
	if want := mustGit(t, dir, "rev-parse", "milestone/v0.9"); got != want {
		t.Errorf("recorded fork point = %q, want milestone/v0.9's tip %q (main is %q)",
			got, want, mustGit(t, dir, "rev-parse", "main"))
	}
}

// backfillFixture creates a phase, adds a commit on its branch, then strips the
// fork point back out of the record — the shape of the ~30 dirs that predate
// the field: a base branch, the phase's own commits, and no base_commit.
func backfillFixture(t *testing.T, phaseID string) (dir, wantSHA string) {
	t.Helper()
	dir = initWithGit(t)
	if err := runCmd(t, Phase(), "create", phaseID); err != nil {
		t.Fatalf("create: %v", err)
	}
	wantSHA = mustGit(t, dir, "rev-parse", "main")
	mustWrite(t, filepath.Join(dir, "work.txt"), "phase work\n")
	mustGit(t, dir, "add", "work.txt")
	mustGit(t, dir, "commit", "-q", "-m", "feat: phase work")
	// Main moves on afterwards, so a resolver that answers "main's tip today"
	// instead of the fork point gets a visibly wrong SHA.
	mustGit(t, dir, "checkout", "-q", "main")
	mustGit(t, dir, "commit", "-q", "--allow-empty", "-m", "chore: main moves on")
	mustGit(t, dir, "checkout", "-q", "phase/"+phaseID)

	stripForkPoint(t, dir, phaseID)
	return dir, wantSHA
}

// stripForkPoint rewrites a phase's changes.json without its base_commit.
func stripForkPoint(t *testing.T, dir, phaseID string) {
	t.Helper()
	path := changes.FilePath(filepath.Join(dir, ".dross"), phaseID)
	c, err := changes.Load(path, phaseID)
	if err != nil {
		t.Fatalf("load changes: %v", err)
	}
	c.BaseCommit = ""
	if err := c.Save(path); err != nil {
		t.Fatalf("save changes: %v", err)
	}
}

// TestForkPointBackfillResolves covers the pre-field record: the fork point is
// the merge-base of the recorded base and the phase's own commits, not the
// base's tip today.
func TestForkPointBackfillResolves(t *testing.T) {
	dir, want := backfillFixture(t, "auth")

	got, err := phaseForkPoint(dir, filepath.Join(dir, ".dross"), "auth")
	if err != nil {
		t.Fatalf("phaseForkPoint: %v", err)
	}
	if got != want {
		t.Errorf("backfilled fork point = %q, want %q (main's tip today is %q)",
			got, want, mustGit(t, dir, "rev-parse", "main"))
	}
}

// TestForkPointBackfillCaches is the caching half of fork_point_backfill: the
// resolved SHA is written back to the record, so the second call answers from
// disk and asks git nothing at all. The argv tap is the proof — a resolver that
// re-derives every time shows up as merge-base/rev-parse invocations on the
// second call.
func TestForkPointBackfillCaches(t *testing.T) {
	dir, want := backfillFixture(t, "auth")
	root := filepath.Join(dir, ".dross")

	if _, err := phaseForkPoint(dir, root, "auth"); err != nil {
		t.Fatalf("first phaseForkPoint: %v", err)
	}
	if got := readRecordedForkPoint(t, dir, "auth"); got != want {
		t.Fatalf("resolved fork point was not cached to changes.json: got %q, want %q", got, want)
	}

	seen := recordGitArgv(t)
	got, err := phaseForkPoint(dir, root, "auth")
	if err != nil {
		t.Fatalf("second phaseForkPoint: %v", err)
	}
	if got != want {
		t.Errorf("cached fork point = %q, want %q", got, want)
	}
	if argvs := seen(); len(argvs) != 0 {
		t.Errorf("second call ran %d git command(s) — the cache is not being read:\n%s",
			len(argvs), formatArgvs(argvs))
	}
}

// TestForkPointBackfillNoBase pins the loud failure. A phase with no recorded
// base has nothing to resolve against; returning "" would hand callers an empty
// ref to pass to git, and the confusing error would surface two layers down
// instead of here, naming the phase.
func TestForkPointBackfillNoBase(t *testing.T) {
	dir := initWithGit(t)
	root := filepath.Join(dir, ".dross")
	path := changes.FilePath(root, "orphan")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{"phase": "orphan", "tasks": map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, path, string(body))

	got, err := phaseForkPoint(dir, root, "orphan")
	if err == nil {
		t.Fatalf("expected an error for a base-less phase, got fork point %q", got)
	}
	if got != "" {
		t.Errorf("errored but still returned %q — a caller reading the value would pass an empty ref to git", got)
	}
	if !strings.Contains(err.Error(), "orphan") {
		t.Errorf("error does not name the phase: %v", err)
	}
}

// TestForkPointBackfillUsesRecordedCommits covers the completed-phase path: the
// phase branch is gone, so the tip has to come from the commits the phase's own
// changes.json recorded.
func TestForkPointBackfillUsesRecordedCommits(t *testing.T) {
	dir, want := backfillFixture(t, "auth")
	root := filepath.Join(dir, ".dross")
	path := changes.FilePath(root, "auth")

	tip := mustGit(t, dir, "rev-parse", "phase/auth")
	c, err := changes.Load(path, "auth")
	if err != nil {
		t.Fatal(err)
	}
	c.Record("t-1", []string{"work.txt"}, tip, "", nil)
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	// Delete the branch, keeping the commit alive only through the record.
	mustGit(t, dir, "checkout", "-q", "main")
	mustGit(t, dir, "branch", "-D", "phase/auth")

	got, err := phaseForkPoint(dir, root, "auth")
	if err != nil {
		t.Fatalf("phaseForkPoint after branch deletion: %v", err)
	}
	if got != want {
		t.Errorf("fork point from recorded commits = %q, want %q", got, want)
	}
}

// TestForkPointBackfillFallsBackToMain covers the population a rotted pin
// actually comes from: a completed phase whose recorded base — a milestone
// branch — was merged and deleted long ago. Giving up there leaves doctor's
// repoint hint unresolvable in exactly the case it exists for.
func TestForkPointBackfillFallsBackToMain(t *testing.T) {
	dir, want := backfillFixture(t, "auth")
	root := filepath.Join(dir, ".dross")

	// Rewrite the record the way a completed phase's looks: a base branch that
	// no longer resolves anywhere, plus its own commits.
	path := changes.FilePath(root, "auth")
	c, err := changes.Load(path, "auth")
	if err != nil {
		t.Fatal(err)
	}
	c.Base = "milestone/v1.2"
	c.Record("t-1", []string{"work.txt"}, mustGit(t, dir, "rev-parse", "phase/auth"), "", nil)
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "checkout", "-q", "main")
	mustGit(t, dir, "branch", "-qD", "phase/auth")

	got, err := phaseForkPoint(dir, root, "auth")
	if err != nil {
		t.Fatalf("a phase whose base branch is gone had no fork point: %v", err)
	}
	if got != want {
		t.Errorf("fork point via the main-branch fallback = %q, want %q", got, want)
	}
}

// TestForkPointBackfillNoSurvivingCommit: with the base gone AND no commit of
// the phase's own left, there is genuinely nothing to merge-base. That must be
// an error naming the phase, not a plausible-looking SHA from somewhere else.
func TestForkPointBackfillNoSurvivingCommit(t *testing.T) {
	dir := initWithGit(t)
	root := filepath.Join(dir, ".dross")
	path := changes.FilePath(root, "ghost")
	c := changes.New("ghost")
	c.Base = "milestone/v1.2"
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}

	got, err := phaseForkPoint(dir, root, "ghost")
	if err == nil {
		t.Fatalf("expected an error, got fork point %q", got)
	}
	if got != "" {
		t.Errorf("errored but still returned %q", got)
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Errorf("error does not name the phase: %v", err)
	}
}

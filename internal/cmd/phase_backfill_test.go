package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/changes"
)

// backfillRepo builds a work repo with a real bare origin on disk, so
// ls-remote, fetch and origin/<base> all answer for real. Returns the work dir.
func backfillRepo(t *testing.T) string {
	t.Helper()
	origin := t.TempDir()
	mustGitBare(t, origin)
	dir := t.TempDir()
	gitInit(t, dir, origin)
	mustWrite(t, filepath.Join(dir, "seed.txt"), "seed\n")
	mustGit(t, dir, "add", "seed.txt")
	mustGit(t, dir, "commit", "-qm", "seed")
	mustGit(t, dir, "push", "-q", "-u", "origin", "main")
	return dir
}

// shipCommit lands one squash-merge-shaped ship commit on main and pushes it.
func shipCommit(t *testing.T, dir, subject string) string {
	t.Helper()
	mustGit(t, dir, "commit", "-q", "--allow-empty", "-m", subject)
	mustGit(t, dir, "push", "-q", "origin", "main")
	return mustGit(t, dir, "rev-parse", "HEAD")
}

func mustShips(t *testing.T, dir string) map[string]string {
	t.Helper()
	ships, err := backfillShipCommits(dir, "main")
	if err != nil {
		t.Fatalf("backfillShipCommits: %v", err)
	}
	return ships
}

// TestBackfillSubjectMatchIsAnchoredOnTheWholeSlug is the anchoring contract.
// `phase multilang-stack-profiles: ...` is a real subject on this repo and it
// CONTAINS `stack-profiles`, so an unanchored or substring match closes a phase
// that never shipped — the exact wrong-record write a 67-record sweep must not
// be capable of.
func TestBackfillSubjectMatchIsAnchoredOnTheWholeSlug(t *testing.T) {
	dir := backfillRepo(t)
	sha := shipCommit(t, dir, "phase multilang-stack-profiles: profiles for other languages")

	ships := mustShips(t, dir)
	if got := resolveBackfill(dir, "stack-profiles", ships); got.OK {
		t.Errorf("stack-profiles was closed by multilang-stack-profiles' ship commit: %+v", got)
	}
	if got := resolveBackfill(dir, "multilang-stack-profiles", ships); !got.OK || got.EvidenceSHA != sha {
		t.Errorf("the phase the subject actually names must be backfillable off it: %+v (want sha %s)", got, sha)
	}
	// The prefix direction too: a subject must not close a LONGER slug either.
	if got := resolveBackfill(dir, "multilang-stack-profiles-extra", ships); got.OK {
		t.Errorf("a longer slug was closed by a shorter subject: %+v", got)
	}
}

// TestBackfillAcceptsOptionalOrdinalPrefix: `dross phase migrate` renamed phase
// directories from `07-stack-profiles` to `stack-profiles` long after those
// phases shipped, so their ship commits still carry the ordinal. Requiring the
// prefix, or refusing it, both lose the oldest milestones entirely — which is
// most of what this phase exists to close.
func TestBackfillAcceptsOptionalOrdinalPrefix(t *testing.T) {
	dir := backfillRepo(t)
	prefixed := shipCommit(t, dir, "phase 03-foo: foo (#7)")
	bare := shipCommit(t, dir, "phase bar: bar (#8)")

	ships := mustShips(t, dir)
	got := resolveBackfill(dir, "foo", ships)
	if !got.OK || got.EvidenceSHA != prefixed {
		t.Errorf("an ordinal-prefixed ship commit must close the migrated slug: %+v (want sha %s)", got, prefixed)
	}
	if got := resolveBackfill(dir, "bar", ships); !got.OK || got.EvidenceSHA != bare {
		t.Errorf("an unprefixed ship commit must still close its slug: %+v", got)
	}
	// The prefix survives in both directions. Most directories were migrated
	// (`07-stack-profiles` → `stack-profiles`) while their ship commits kept
	// the ordinal; 14-stable-slug-phase-ids — the phase that shipped the
	// migration and so could not migrate itself — kept the ordinal in the
	// DIRECTORY. Normalising only one side closes one group and misses the
	// other.
	if got := resolveBackfill(dir, "03-foo", ships); !got.OK || got.EvidenceSHA != prefixed {
		t.Errorf("an unmigrated directory name must match its own ship subject: %+v", got)
	}
	// The prefix is exactly two digits, not any leading token.
	if got := resolveBackfill(dir, "xx-foo", ships); got.OK {
		t.Errorf("a non-ordinal leading token must not be stripped: %+v", got)
	}
}

// TestBackfillLiveLocalBranchDisqualifies: a phase whose branch still exists is
// in flight, not shipped. Its ship commit may be on the base from an earlier
// delivery while the current round is still open.
func TestBackfillLiveLocalBranchDisqualifies(t *testing.T) {
	dir := backfillRepo(t)
	shipCommit(t, dir, "phase inflight: inflight (#9)")
	mustGit(t, dir, "branch", "phase/inflight")

	got := resolveBackfill(dir, "inflight", mustShips(t, dir))
	if got.OK {
		t.Fatalf("a live local phase branch must disqualify: %+v", got)
	}
	if !strings.Contains(got.Reason, "phase/inflight") {
		t.Errorf("the reason must name the branch that disqualified it: %q", got.Reason)
	}
}

// TestBackfillLiveOriginBranchDisqualifies is the same rule from the other side,
// with the branch on origin only — the case a local-refs-only check misses.
func TestBackfillLiveOriginBranchDisqualifies(t *testing.T) {
	dir := backfillRepo(t)
	shipCommit(t, dir, "phase remote-inflight: remote-inflight (#10)")
	mustGit(t, dir, "push", "-q", "origin", "HEAD:refs/heads/phase/remote-inflight")

	got := resolveBackfill(dir, "remote-inflight", mustShips(t, dir))
	if got.OK {
		t.Fatalf("a live phase branch on origin must disqualify: %+v", got)
	}
	if !strings.Contains(got.Reason, "origin") {
		t.Errorf("the reason must say the branch is on origin: %q", got.Reason)
	}
}

// TestBackfillIgnoresStaleRemoteTrackingRef pins the ls-remote choice.
// refs/remotes/origin/ is a local cache that is stale until a fetch --prune, so
// a check reading it reports a live branch for every legacy phase whose branch
// the forge deleted at merge — which is all of them, and the sweep would mark
// nothing. Here the tracking ref exists and the branch on origin does not.
func TestBackfillIgnoresStaleRemoteTrackingRef(t *testing.T) {
	dir := backfillRepo(t)
	sha := shipCommit(t, dir, "phase merged-and-deleted: merged-and-deleted (#11)")
	// The branch never reaches origin; only the local remote-tracking CACHE
	// claims it does. That is what a forge's merge-time branch deletion leaves
	// behind on a machine that has not fetched --prune since — and `git push
	// --delete` would prune it here, which is why the ref is planted directly.
	mustGit(t, dir, "update-ref", "refs/remotes/origin/phase/merged-and-deleted", sha)
	if out := mustGit(t, dir, "rev-parse", "--verify", "refs/remotes/origin/phase/merged-and-deleted"); out == "" {
		t.Fatal("fixture: the stale remote-tracking ref must be present")
	}
	if out := mustGit(t, dir, "ls-remote", "--heads", "origin", "phase/merged-and-deleted"); out != "" {
		t.Fatalf("fixture: origin must NOT carry the branch, got %q", out)
	}

	got := resolveBackfill(dir, "merged-and-deleted", mustShips(t, dir))
	if !got.OK || got.EvidenceSHA != sha {
		t.Errorf("a stale remote-tracking ref must not read as a live branch on origin: %+v", got)
	}
}

// TestBackfillUnreachableOriginIsNotAbsence: absence has to be PROVED. A failed
// ls-remote — offline, origin unset, auth failure — read as "no ref" would mark
// 67 phases off a dropped network connection. It is a per-slug reason rather
// than a run abort, so one unreachable query does not kill a sweep, but nothing
// is written for that slug.
func TestBackfillUnreachableOriginIsNotAbsence(t *testing.T) {
	dir := backfillRepo(t)
	shipCommit(t, dir, "phase orphan: orphan (#12)")
	ships := mustShips(t, dir)
	// Point origin at a path that does not exist, AFTER the scan — the query
	// now fails rather than answering "no such branch".
	mustGit(t, dir, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))

	got := resolveBackfill(dir, "orphan", ships)
	if got.OK {
		t.Fatalf("an unanswerable origin query must not read as absence: %+v", got)
	}
	if !strings.Contains(got.Reason, "origin") {
		t.Errorf("the reason must name the failed origin query: %q", got.Reason)
	}
	if got.EvidenceSHA != "" {
		t.Errorf("an unbackfillable verdict must carry no evidence: %+v", got)
	}
}

// TestBackfillScansOriginNotLocalBase: under squash-merge local main lags
// origin/main from the moment a PR merges until somebody fetches. A scan of the
// local ref reports every recently shipped phase as having no evidence, so the
// sweep silently under-marks exactly the newest milestones.
func TestBackfillScansOriginNotLocalBase(t *testing.T) {
	dir := backfillRepo(t)
	sha := shipCommit(t, dir, "phase only-on-origin: only-on-origin (#13)")
	// Rewind the local ref so the ship commit exists on origin/main alone.
	mustGit(t, dir, "reset", "-q", "--hard", "HEAD~1")
	if local := mustGit(t, dir, "log", "--format=%s", "-1", "main"); strings.Contains(local, "only-on-origin") {
		t.Fatal("fixture: the ship commit must not be on the local ref")
	}

	got := resolveBackfill(dir, "only-on-origin", mustShips(t, dir))
	if !got.OK || got.EvidenceSHA != sha {
		t.Errorf("a ship commit on origin/main must be found — the scan read the local ref: %+v (want sha %s)", got, sha)
	}
}

// TestBackfillNoShipCommitIsUnbackfillable: no evidence, no marker. This is the
// residue arm — a phase that was scaffolded and never shipped stays unwritten
// and gets named, rather than being closed on the strength of its branch being
// gone alone.
func TestBackfillNoShipCommitIsUnbackfillable(t *testing.T) {
	dir := backfillRepo(t)
	shipCommit(t, dir, "phase something-else: something-else (#14)")

	got := resolveBackfill(dir, "never-shipped", mustShips(t, dir))
	if got.OK {
		t.Fatalf("a phase with no ship commit must not be marked: %+v", got)
	}
	if !strings.Contains(got.Reason, "never-shipped") {
		t.Errorf("the reason must name the pattern it looked for: %q", got.Reason)
	}
}

// TestBackfillNewestShipWins: a follow-up PR against the same phase dir ships
// the slug twice. The recorded evidence should be the most recent delivery —
// re-running the sweep against better evidence is the point of recording it.
func TestBackfillNewestShipWins(t *testing.T) {
	dir := backfillRepo(t)
	shipCommit(t, dir, "phase twice: first delivery (#15)")
	newest := shipCommit(t, dir, "phase twice: follow-up (#16)")

	got := resolveBackfill(dir, "twice", mustShips(t, dir))
	if !got.OK || got.EvidenceSHA != newest {
		t.Errorf("evidence = %+v, want the newest ship commit %s", got, newest)
	}
}

// TestBackfillFetchFailureIsHard: a stale origin/<base> answers the scan with
// the wrong list and nothing looks wrong. The scan refuses rather than reporting
// under-evidenced verdicts from a base it could not refresh.
func TestBackfillFetchFailureIsHard(t *testing.T) {
	dir := backfillRepo(t)
	shipCommit(t, dir, "phase whatever: whatever (#17)")
	mustGit(t, dir, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))

	if _, err := backfillShipCommits(dir, "main"); err == nil {
		t.Fatal("a failed fetch must abort the scan — a stale base answers with the wrong list")
	}
}

// backfillCmdFixture is backfillRepo plus an initialized .dross, chdir'd so the
// command's FindRoot lands on it.
func backfillCmdFixture(t *testing.T) string {
	t.Helper()
	dir := backfillRepo(t)
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	return dir
}

// backfillPhaseDir creates .dross/phases/<slug>/. An empty status leaves the
// pre-field shape; "none" leaves no changes.json at all, which Load also reads
// as status-less.
func backfillPhaseDir(t *testing.T, dir, slug, status string) {
	t.Helper()
	root := filepath.Join(dir, ".dross")
	if err := os.MkdirAll(filepath.Join(root, "phases", slug), 0o755); err != nil {
		t.Fatal(err)
	}
	switch status {
	case "none":
		return
	case "":
		mustWrite(t, changes.FilePath(root, slug), `{"phase":"`+slug+`","tasks":{}}`+"\n")
	default:
		if err := changes.SetStatus(root, slug, status); err != nil {
			t.Fatal(err)
		}
	}
}

// snapshotRecords reads every changes.json under .dross/phases.
func snapshotRecords(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	base := filepath.Join(dir, ".dross", "phases")
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(base, e.Name(), changes.File))
		if err != nil {
			out[e.Name()] = "<absent>"
			continue
		}
		out[e.Name()] = string(b)
	}
	return out
}

func mustBackfill(t *testing.T, args ...string) string {
	t.Helper()
	var out string
	if err := runCmdCapturing(t, &out, Phase(), append([]string{"backfill"}, args...)...); err != nil {
		t.Fatalf("phase backfill %v: %v\n%s", args, err, out)
	}
	return out
}

// TestBackfillPreviewWritesNothing is the write gate. A 67-record sweep driven
// by a regex over commit subjects has to be readable before it lands, and "the
// bare form is a preview" is only true if not one byte moves.
func TestBackfillPreviewWritesNothing(t *testing.T) {
	dir := backfillCmdFixture(t)
	shipCommit(t, dir, "phase alpha: alpha (#1)")
	backfillPhaseDir(t, dir, "alpha", "")
	backfillPhaseDir(t, dir, "beta", "none")

	before := snapshotRecords(t, dir)
	out := mustBackfill(t)
	if !strings.Contains(out, "preview only") {
		t.Errorf("the bare form must say it wrote nothing:\n%s", out)
	}
	after := snapshotRecords(t, dir)
	if !reflect.DeepEqual(before, after) {
		t.Errorf("the preview mutated records:\nbefore %v\nafter  %v", before, after)
	}
}

// TestBackfillApplyWritesStatusAndEvidence: the marker and its provenance land
// together. A marker with no evidence SHA is an unauditable claim — the sweep
// could not be re-run against better evidence, which is the whole reason the
// provenance field exists.
func TestBackfillApplyWritesStatusAndEvidence(t *testing.T) {
	dir := backfillCmdFixture(t)
	sha := shipCommit(t, dir, "phase alpha: alpha (#1)")
	backfillPhaseDir(t, dir, "alpha", "")

	mustBackfill(t, "--apply")

	root := filepath.Join(dir, ".dross")
	c, err := changes.Load(changes.FilePath(root, "alpha"), "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != changes.StatusComplete {
		t.Errorf("status = %q, want %q", c.Status, changes.StatusComplete)
	}
	if c.BackfillEvidence == nil || c.BackfillEvidence.SHA != sha {
		t.Errorf("evidence = %+v, want the ship commit %s", c.BackfillEvidence, sha)
	}
	if !phaseDone(root, "alpha") {
		t.Error("a backfilled record must read done through the shared reader")
	}
}

// TestBackfillApplyLeavesFailedEvidenceUntouched: a phase failing EITHER test —
// live branch, or no ship commit — is left exactly as it was. This is the arm
// that makes an unattended sweep safe: over-reach writes a done marker onto
// unfinished work, and nothing downstream can tell it from a real one.
func TestBackfillApplyLeavesFailedEvidenceUntouched(t *testing.T) {
	dir := backfillCmdFixture(t)
	shipCommit(t, dir, "phase inflight: inflight (#1)")
	backfillPhaseDir(t, dir, "inflight", "")
	mustGit(t, dir, "branch", "phase/inflight")
	backfillPhaseDir(t, dir, "no-evidence", "")

	before := snapshotRecords(t, dir)
	mustBackfill(t, "--apply")
	after := snapshotRecords(t, dir)

	for _, slug := range []string{"inflight", "no-evidence"} {
		if before[slug] != after[slug] {
			t.Errorf("%s failed its evidence test but its record was rewritten:\n%s", slug, after[slug])
		}
		if phaseDone(filepath.Join(dir, ".dross"), slug) {
			t.Errorf("%s was marked done with no evidence", slug)
		}
	}
}

// TestBackfillPreviewNamesEveryCandidateWithItsVerdict: the preview doubles as
// the residue listing (the locked backfill_write_gate decision). Dropping the
// unbackfillable rows would make a run that closed 67 of 69 records look like
// one that closed everything.
func TestBackfillPreviewNamesEveryCandidateWithItsVerdict(t *testing.T) {
	dir := backfillCmdFixture(t)
	sha := shipCommit(t, dir, "phase alpha: alpha (#1)")
	shipCommit(t, dir, "phase inflight: inflight (#2)")
	backfillPhaseDir(t, dir, "alpha", "")
	backfillPhaseDir(t, dir, "inflight", "")
	mustGit(t, dir, "branch", "phase/inflight")
	backfillPhaseDir(t, dir, "no-evidence", "")

	out := mustBackfill(t)
	for _, want := range []string{
		"alpha  backfillable  " + sha,
		"inflight  unbackfillable",
		"no-evidence  unbackfillable",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the preview is missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "phase/inflight") {
		t.Errorf("an unbackfillable row must carry the reason it failed:\n%s", out)
	}
}

// TestBackfillCandidatesAreDirectoriesNotRoadmapArrays: the sweep's candidate
// set is every status-less phase DIRECTORY, while doctor's residue check is
// scoped to the milestones' phases arrays. The two coincide on this repo today,
// so nothing catches the divergence unless it is pinned — and a directory on no
// roadmap would otherwise fall out of both surfaces at once.
func TestBackfillCandidatesAreDirectoriesNotRoadmapArrays(t *testing.T) {
	dir := backfillCmdFixture(t)
	sha := shipCommit(t, dir, "phase orphan-dir: orphan-dir (#1)")
	backfillPhaseDir(t, dir, "orphan-dir", "")
	// No milestone toml at all, so no roadmap array names it.

	out := mustBackfill(t)
	if !strings.Contains(out, "orphan-dir  backfillable  "+sha) {
		t.Errorf("a status-less directory on no roadmap must still be a candidate:\n%s", out)
	}
}

// TestBackfillSkipsRecordsThatAlreadyHaveStatus: a record carrying an OBSERVED
// marker is not a candidate. Re-running the sweep must be a no-op over it, not
// a rewrite that replaces a `dross phase complete` verdict with an inferred one.
func TestBackfillSkipsRecordsThatAlreadyHaveStatus(t *testing.T) {
	dir := backfillCmdFixture(t)
	shipCommit(t, dir, "phase shipped-already: shipped-already (#1)")
	backfillPhaseDir(t, dir, "shipped-already", changes.StatusShipped)

	before := snapshotRecords(t, dir)
	out := mustBackfill(t, "--apply")
	if strings.Contains(out, "shipped-already") {
		t.Errorf("a record with a status is not a candidate:\n%s", out)
	}
	if after := snapshotRecords(t, dir); !reflect.DeepEqual(before, after) {
		t.Errorf("the sweep rewrote an already-marked record:\n%v", after)
	}
}

// TestBackfillRefusesAmbiguousOrdinalPair: `03-foo` and `foo` both match one
// ship commit and only one of them shipped. No evidence says which, so neither
// is marked — an ambiguous marker reads exactly like a verified one afterwards.
func TestBackfillRefusesAmbiguousOrdinalPair(t *testing.T) {
	dir := backfillCmdFixture(t)
	shipCommit(t, dir, "phase 03-foo: foo (#1)")
	backfillPhaseDir(t, dir, "03-foo", "")
	backfillPhaseDir(t, dir, "foo", "")

	out := mustBackfill(t, "--apply")
	for _, slug := range []string{"03-foo", "foo"} {
		if !strings.Contains(out, slug+"  unbackfillable") {
			t.Errorf("%s should be refused as ambiguous:\n%s", slug, out)
		}
		if phaseDone(filepath.Join(dir, ".dross"), slug) {
			t.Errorf("%s was marked off ambiguous evidence", slug)
		}
	}
	if !strings.Contains(out, "ambiguous") {
		t.Errorf("the reason must say the pair is ambiguous:\n%s", out)
	}
}

package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/secretscan"
)

// c-6: the detector run over dross's own tracked tree — source and .dross/
// alike — reports nothing, and the set of lines that need a silencing marker
// to get there is pinned, so a marker cannot be added or dropped unnoticed.
//
// A SILENCING marker is one whose line hits when the marker is stripped. The
// three lines that merely spell the literal — the AllowMarker const, the
// remedy line in Report(), ship's refusal message — silence nothing and are
// excluded by that definition, not by name.

// repoRoot is the dross checkout, relative to internal/cmd.
const repoRoot = "../.."

// selfScanFiles lists every path `git ls-files -z` reports at the repo root,
// repo-relative, or skips the test when this is not a git checkout.
func selfScanFiles(t *testing.T) []string {
	t.Helper()
	cmd := exec.Command("git", "-C", repoRoot, "ls-files", "-z")
	out, err := cmd.Output()
	if err != nil {
		t.Skipf("not a git checkout (git ls-files: %v) — the self-scan needs the tracked set", err)
	}
	var files []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			files = append(files, p)
		}
	}
	if len(files) == 0 {
		t.Skip("git ls-files listed nothing")
	}
	return files
}

// scanTracked runs the scanner over every listed file (repo-relative, or
// absolute for a planted extra) with the given path as each hit's location.
func scanTracked(t *testing.T, files []string) []secretscan.Hit {
	t.Helper()
	var hits []secretscan.Hit
	for _, rel := range files {
		p := filepath.Join(repoRoot, filepath.FromSlash(rel))
		if filepath.IsAbs(rel) {
			p = rel
		}
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		found, err := secretscan.Scan(rel, bytes.NewReader(b))
		if err != nil {
			t.Fatalf("scan %s: %v", rel, err)
		}
		hits = append(hits, found...)
	}
	return hits
}

// silencingMarkers returns, per repo-relative file, how many marker-bearing
// lines hit once the marker is stripped.
func silencingMarkers(t *testing.T, files []string) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, rel := range files {
		b, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		for _, line := range strings.Split(string(b), "\n") {
			if !strings.Contains(line, secretscan.AllowMarker) {
				continue
			}
			stripped := strings.ReplaceAll(line, secretscan.AllowMarker, "")
			if len(secretscan.ScanString(rel, stripped)) > 0 {
				out[rel]++
			}
		}
	}
	return out
}

// pinnedMarkerSites is the table: repo-relative file → number of silencing
// markers it carries. Adding a marker anywhere means editing this table in the
// same diff, which is the review moment the allowlist_route decision wants.
var pinnedMarkerSites = map[string]int{
	"internal/cmd/env_test.go":                                1,
	"internal/cmd/hermetic_env_test.go":                       1,
	"internal/forge/hostallow_test.go":                        1,
	"internal/forge/hostile_config_test.go":                   1,
	"internal/argfence/policy.go":                             1,
	"internal/security/gitleaks_test.go":                      2,
	".dross/phases/scanner-self-exclusion/panel/risk.md":      1,
	".dross/phases/scanner-self-exclusion/panel/synthesis.md": 1,
	// This phase's own panel notes quote the fixture shapes they reason about;
	// synthesis.md also carries t-8's task text, which names a fixture value
	// on the same line as the marker it prescribes.
	".dross/phases/secret-detection/panel/mvp.md":          2,
	".dross/phases/secret-detection/panel/synthesis.md":    2,
	".dross/phases/secret-detection/panel/verification.md": 3,
}

// TestDrossTreeHasNoUnmarkedHits: zero hits over every tracked file. The
// failure lists fingerprints only — never a line.
func TestDrossTreeHasNoUnmarkedHits(t *testing.T) {
	hits := scanTracked(t, selfScanFiles(t))
	for _, h := range hits {
		t.Errorf("%s:%d %s (len=%d, prefix=%s)", h.Location, h.Line, h.Rule, h.Length, h.Prefix)
	}
}

// TestSelfScanIsNotVacuous: the same walk plus one file holding a synthesized
// hit yields exactly that one finding, at that file.
func TestSelfScanIsNotVacuous(t *testing.T) {
	files := selfScanFiles(t)
	dir := t.TempDir()
	leak := filepath.Join(dir, "leak.md")
	mustWrite(t, leak, "note: "+synthGitlabToken()+"\n")
	hits := scanTracked(t, append(files, leak))
	if len(hits) != 1 {
		t.Fatalf("want exactly one hit (the planted one), got %d: %v", len(hits), hits)
	}
	if hits[0].Location != leak || hits[0].Rule != "gitlab-pat" {
		t.Fatalf("hit = %v, want gitlab-pat at %s", hits[0], leak)
	}
}

// TestAllowMarkerSitesArePinned compares the live multiset of silencing
// markers against the table, naming every file that differs.
func TestAllowMarkerSitesArePinned(t *testing.T) {
	got := silencingMarkers(t, selfScanFiles(t))
	names := map[string]bool{}
	for k := range got {
		names[k] = true
	}
	for k := range pinnedMarkerSites {
		names[k] = true
	}
	var keys []string
	for k := range names {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if got[k] != pinnedMarkerSites[k] {
			t.Errorf("%s: %d silencing marker(s) in the tree, table pins %d — a marker was added or removed without a table edit", k, got[k], pinnedMarkerSites[k])
		}
	}
	if len(got) == 0 {
		t.Fatal("no silencing marker found anywhere — the marker walk is not seeing them")
	}
}

// TestSilencingMarkerNeverAppearsInNonTestGoOutsideArgfence: the only
// non-test .go file whose marker silences a hit is argfence/policy.go. The
// two production files that spell the literal — secretscan.go and ship.go —
// silence nothing, asserted by stripping their markers and scanning.
func TestSilencingMarkerNeverAppearsInNonTestGoOutsideArgfence(t *testing.T) {
	files := selfScanFiles(t)
	var nonTestGo []string
	for _, f := range files {
		if strings.HasSuffix(f, ".go") && !strings.HasSuffix(f, "_test.go") {
			nonTestGo = append(nonTestGo, f)
		}
	}
	for file, n := range silencingMarkers(t, nonTestGo) {
		if file != "internal/argfence/policy.go" {
			t.Errorf("%s carries %d silencing marker(s) in production code; only argfence/policy.go may", file, n)
		}
	}

	inert := map[string]bool{"internal/secretscan/secretscan.go": false, "internal/cmd/ship.go": false}
	for _, f := range nonTestGo {
		if _, ok := inert[f]; !ok {
			continue
		}
		b, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(f)))
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(b), "\n") {
			if !strings.Contains(line, secretscan.AllowMarker) {
				continue
			}
			inert[f] = true
			if hits := secretscan.ScanString(f, strings.ReplaceAll(line, secretscan.AllowMarker, "")); len(hits) > 0 {
				t.Errorf("%s: a marker line hits once stripped — it is silencing, not inert: %v", f, hits)
			}
		}
	}
	for f, seen := range inert {
		if !seen {
			t.Errorf("%s no longer spells the marker literal — the inert-marker assertion is vacuous for it", f)
		}
	}
}

// TestSelfScanCoversTheWholeTreeNotJustDross pins the walk's breadth.
func TestSelfScanCoversTheWholeTreeNotJustDross(t *testing.T) {
	files := selfScanFiles(t)
	under, outside := 0, 0
	for _, f := range files {
		if strings.HasPrefix(f, ".dross/") {
			under++
		} else {
			outside++
		}
	}
	if len(files) < 1500 {
		t.Errorf("visited %d tracked files, want at least 1500", len(files))
	}
	if under < 900 {
		t.Errorf("visited %d files under .dross/, want at least 900", under)
	}
	if outside < 1 {
		t.Error("visited no file outside .dross/ — the self-scan has narrowed to artifacts only")
	}
}

// TestValidateOnDrossItself runs the live gate over a hermetic clone of HEAD
// with the working tree's TRACKED .dross files laid over it — so the run sees
// what the next commit will carry, while untracked panel notes or an
// uncommitted REVIEW.md cannot turn it red. A token committed into the clone
// flips it to a refusal (non-vacuity).
func TestValidateOnDrossItself(t *testing.T) {
	if _, err := exec.Command("git", "-C", repoRoot, "rev-parse", "--verify", "HEAD").Output(); err != nil {
		t.Skip("not a git checkout with a HEAD")
	}
	src, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	clone := filepath.Join(t.TempDir(), "dross")
	if out, err := exec.Command("git", "clone", "-q", "--no-hardlinks", src, clone).CombinedOutput(); err != nil {
		t.Fatalf("git clone: %v\n%s", err, out)
	}
	// Overlay the working tree's tracked .dross files so uncommitted edits to
	// tracked artifacts (a marker added this phase) are what validate sees.
	out, err := exec.Command("git", "-C", src, "ls-files", "-z", "--", ".dross").Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range strings.Split(string(out), "\x00") {
		if rel == "" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(src, filepath.FromSlash(rel)))
		if err != nil {
			continue // deleted in the working tree; the clone keeps HEAD's copy
		}
		mustWrite(t, filepath.Join(clone, filepath.FromSlash(rel)), string(b))
	}
	// state.json is gitignored, so the clone has none; validate needs one.
	mustWrite(t, filepath.Join(clone, ".dross", "state.json"), "{}\n")
	chdir(t, clone)

	var verr error
	vout := captureStdout(t, func() { verr = runCmd(t, Validate()) })
	if verr != nil {
		t.Fatalf("validate fails on dross's own tracked tree: %v\n%s", verr, vout)
	}
	if strings.Contains(vout, "secret:") {
		t.Fatalf("validate reported a secret on dross's own tree:\n%s", vout)
	}

	// Non-vacuity: commit a token into the clone and the gate must flip.
	mustWrite(t, filepath.Join(clone, ".dross", "notes.md"), "t = "+synthGitlabToken()+"\n")
	mustGit(t, clone, "add", ".dross/notes.md")
	mustGit(t, clone, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "leak")
	vout = captureStdout(t, func() { verr = runCmd(t, Validate()) })
	if verr == nil || !strings.Contains(vout, "secret: gitlab-pat at .dross/notes.md:1") {
		t.Fatalf("a committed token did not flip validate: err=%v\n%s", verr, vout)
	}
}

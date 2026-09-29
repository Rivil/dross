package pincheck

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// realPinFiles are the files in this repo that hold a pin cmd/pincheck checks,
// plus ci.yml, which names the Node version file. The bump dry run copies them
// into a temp tree and rewrites the copies — never the checkout.
var realPinFiles = []string{
	".github/workflows/ci.yml",
	".github/actions/govulncheck/action.yml",
	".github/actions/goreleaser/action.yml",
	"internal/mutation/testdata/ts-project/.node-version",
	"go.mod",
	"internal/cmd/remote_bootstrap.go",
	"internal/mutation/stryker.go",
}

// copyRealPins copies realPinFiles from the repo into a temp tree.
func copyRealPins(t *testing.T) (root string, originals map[string][]byte) {
	t.Helper()
	root = t.TempDir()
	originals = map[string][]byte{}
	for _, rel := range realPinFiles {
		b, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
		originals[rel] = b
	}
	return root, originals
}

// treeSites is what cmd/pincheck checks in a tree: the scan plus the two
// Go-source pins.
func treeSites(t *testing.T, root string) []Site {
	t.Helper()
	sites, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []struct {
		file, name string
		kind       Kind
	}{
		{"internal/cmd/remote_bootstrap.go", "gremlinsPin", KindGoInstall},
		{"internal/mutation/stryker.go", "strykerPin", KindNPM},
	} {
		v, line, err := GoConst(filepath.Join(root, filepath.FromSlash(p.file)), p.name)
		if err != nil {
			t.Fatal(err)
		}
		sites = append(sites, SpecSite(p.file, line, p.kind, v))
	}
	return sites
}

// patchPlus returns v with its patch number raised by n, prefix kept:
// v1.8.0 → v1.8.1, go1.27.1 → go1.27.2, 24.19.0 → 24.19.1.
func patchPlus(t *testing.T, v string, n int) string {
	t.Helper()
	i := strings.LastIndex(v, ".")
	p, err := strconv.Atoi(v[i+1:])
	if err != nil {
		t.Fatalf("patchPlus(%q): %v", v, err)
	}
	return v[:i+1] + strconv.Itoa(p+n)
}

// upstreamAhead builds a resolver where every site's upstream has one newer
// patch out 30 days. The Node site's upstream also has a second, newer patch
// only 2 days old — inside the cooldown, so never the target.
func upstreamAhead(t *testing.T, sites []Site, include func(Site) bool) fakeResolver {
	r := fakeResolver{}
	for _, s := range sites {
		rels := []Release{{Version: s.Version, Published: daysAgo(90)}}
		if include(s) {
			rels = append(rels, Release{Version: patchPlus(t, s.Version, 1), Published: daysAgo(30)})
			if s.Kind == KindNode {
				rels = append(rels, Release{Version: patchPlus(t, s.Version, 2), Published: daysAgo(2)})
			}
		}
		r[s.Name] = rels
	}
	return r
}

func TestBumpRewritesOnlyStaleSites(t *testing.T) {
	root, originals := copyRealPins(t)
	sites := treeSites(t, root)
	if len(sites) != 6 {
		t.Fatalf("scanned %d sites from the real pin files, want 6: %+v", len(sites), sites)
	}
	rep := Check(context.Background(), sites, upstreamAhead(t, sites, func(Site) bool { return true }), Strict, classifyNow)
	outcomes, err := Bump(root, rep.Results)
	if err != nil {
		t.Fatalf("Bump: %v", err)
	}

	bumped := map[string]BumpOutcome{}
	for _, o := range outcomes {
		switch o.Status {
		case Bumped:
			bumped[o.File] = o
			if want := patchPlus(t, o.Version, 1); o.Target != want {
				t.Errorf("%s: target %s, want %s — the newest release past the cooldown", o.File, o.Target, want)
			}
		case Skipped:
			if o.Kind != KindNPM {
				t.Errorf("skipped a non-npm pin: %s", o)
			}
		default:
			t.Errorf("unexpected outcome %s", o)
		}
	}
	for _, want := range []string{
		".github/actions/govulncheck/action.yml",
		".github/actions/goreleaser/action.yml",
		"internal/mutation/testdata/ts-project/.node-version",
		"go.mod",
		"internal/cmd/remote_bootstrap.go",
	} {
		if _, ok := bumped[want]; !ok {
			t.Errorf("%s was not bumped", want)
		}
	}

	// Byte diff: a file with no bumped site is identical; a bumped file
	// differs on the site's line alone, and only by the version token.
	for _, rel := range realPinFiles {
		got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		o, ok := bumped[rel]
		if !ok {
			if !bytes.Equal(got, originals[rel]) {
				t.Errorf("%s changed though nothing in it was bumped", rel)
			}
			continue
		}
		before := strings.SplitAfter(string(originals[rel]), "\n")
		after := strings.SplitAfter(string(got), "\n")
		if len(before) != len(after) {
			t.Fatalf("%s: line count %d → %d", rel, len(before), len(after))
		}
		for i := range before {
			want := before[i]
			if i == o.Line-1 {
				want = strings.Replace(want, o.Version, o.Target, 1)
			}
			if after[i] != want {
				t.Errorf("%s:%d = %q, want %q", rel, i+1, after[i], want)
			}
		}
	}
}

func TestBumpRefusesWorkflowFiles(t *testing.T) {
	const wf = "jobs:\n  a:\n    steps:\n      - uses: actions/setup-node@49933ea5288caeca8642d1e84afbd3f7d6820020  # v4.4.0\n        with:\n          node-version: '24.19.0'\n"
	root := writeTree(t, map[string]string{".github/workflows/ci.yml": wf})
	sites, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	rep := Check(context.Background(), sites, upstreamAhead(t, sites, func(Site) bool { return true }), Strict, classifyNow)
	outcomes, err := Bump(root, rep.Results)
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 1 || outcomes[0].Status != Refused || outcomes[0].Reason == "" {
		t.Fatalf("outcomes = %v, want the inline node-version refused with a reason", outcomes)
	}
	if got, _ := os.ReadFile(filepath.Join(root, ".github/workflows/ci.yml")); string(got) != wf {
		t.Errorf("a refused workflow file changed:\n%s", got)
	}
}

func TestBumpSkipsStrykerPin(t *testing.T) {
	root, originals := copyRealPins(t)
	sites := treeSites(t, root)
	rep := Check(context.Background(), sites, upstreamAhead(t, sites, func(s Site) bool { return s.Kind == KindNPM }), Strict, classifyNow)
	if !rep.Stale() {
		t.Fatal("setup: strykerPin is not stale under the stub")
	}
	outcomes, err := Bump(root, rep.Results)
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 1 || outcomes[0].Status != Skipped || outcomes[0].Name != "@stryker-mutator/core" {
		t.Fatalf("outcomes = %v, want strykerPin reported skipped", outcomes)
	}
	got, _ := os.ReadFile(filepath.Join(root, "internal/mutation/stryker.go"))
	if !bytes.Equal(got, originals["internal/mutation/stryker.go"]) {
		t.Error("stryker.go changed — the bump job must never touch strykerPin")
	}
}

func TestBumpIsIdempotent(t *testing.T) {
	root, _ := copyRealPins(t)
	sites := treeSites(t, root)
	stub := upstreamAhead(t, sites, func(s Site) bool { return s.Kind != KindNPM })
	if _, err := Bump(root, Check(context.Background(), sites, stub, Strict, classifyNow).Results); err != nil {
		t.Fatal(err)
	}
	snapshot := map[string][]byte{}
	for _, rel := range realPinFiles {
		snapshot[rel], _ = os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	}

	again := Check(context.Background(), treeSites(t, root), stub, Strict, classifyNow)
	outcomes, err := Bump(root, again.Results)
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 0 {
		t.Errorf("second bump reported %v, want nothing", outcomes)
	}
	for _, rel := range realPinFiles {
		if got, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel))); !bytes.Equal(got, snapshot[rel]) {
			t.Errorf("second bump changed %s", rel)
		}
	}
}

func TestBumpRefusesAMovedLine(t *testing.T) {
	root := writeTree(t, map[string]string{"go.mod": "module m\n\ntoolchain go1.27.1 // go1.27.1\n"})
	s := Site{File: "go.mod", Line: 3, Kind: KindGoToolchain, Name: "go", Version: "go1.27.1", Pinned: true}
	res := []Result{{Site: s, Classification: Classification{Verdict: Stale, Target: "go1.27.2"}}}
	if _, err := Bump(root, res); err == nil {
		t.Error("two occurrences of the version on the line: want an error, not a guess")
	}
	s.Line = 9
	res[0].Site = s
	if _, err := Bump(root, res); err == nil {
		t.Error("a line past the end of the file: want an error")
	}

	if got := versionOccurrences("x 11.8.0 1.8.01 v1.8.0", "1.8.0"); len(got) != 1 || got[0] != 17 {
		t.Errorf("versionOccurrences = %v, want only the bounded match at 17", got)
	}
}

// TestBumpOutcomeString pins the line cmd/pincheck prints per stale site: a
// skipped or refused site carries its reason after a dash, a bumped one ends
// at its target.
func TestBumpOutcomeString(t *testing.T) {
	for _, tc := range []struct {
		o    BumpOutcome
		want string
	}{
		{
			BumpOutcome{
				Site:   Site{File: "internal/mutation/stryker.go", Line: 21, Kind: KindNPM, Name: "@stryker-mutator/core", Version: "9.1.0", Pinned: true},
				Target: "9.2.0", Status: Skipped,
				Reason: "npm pins move with Dependabot's lockfile bump, never the pin-currency bot",
			},
			"skipped  internal/mutation/stryker.go:21 @stryker-mutator/core 9.1.0 → 9.2.0 — npm pins move with Dependabot's lockfile bump, never the pin-currency bot",
		},
		{
			BumpOutcome{
				Site:   Site{File: ".node-version", Line: 1, Kind: KindNode, Name: "node", Version: "24.19.0", Pinned: true},
				Target: "24.21.0", Status: Bumped,
			},
			"bumped   .node-version:1 node 24.19.0 → 24.21.0",
		},
	} {
		if got := tc.o.String(); got != tc.want {
			t.Errorf("String() = %q\nwant       %q", got, tc.want)
		}
	}
}

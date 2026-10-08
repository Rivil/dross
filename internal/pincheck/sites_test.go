package pincheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTree lays files out under a fresh temp dir and returns its path.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func scanTree(t *testing.T, files map[string]string) []Site {
	t.Helper()
	sites, err := Scan(writeTree(t, files))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return sites
}

func TestScanFindsEverySiteKind(t *testing.T) {
	sites := scanTree(t, map[string]string{
		".github/workflows/ci.yml": `name: ci
on: [push]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683  # v4.2.2
      - name: install govulncheck
        run: go install golang.org/x/vuln/cmd/govulncheck@v1.8.0
      - uses: actions/setup-node@49933ea5288caeca8642d1e84afbd3f7d6820020  # v4.4.0
        with:
          node-version: '24.19.0'
          cache: npm
`,
		".github/workflows/other.yaml": `name: other
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - name: tools
        run: |
          set -euo pipefail
          echo installing
          go install honnef.co/go/tools/cmd/staticcheck@v0.7.0
      - name: node from a file
        uses: actions/setup-node@49933ea5288caeca8642d1e84afbd3f7d6820020  # v4.4.0
        with:
          node-version-file: web/.nvmrc
      - uses: goreleaser/goreleaser-action@9ed2f89a662bf1735a48bc8557fd212fa902bebf  # v6.1.0
        with:
          distribution: goreleaser
          version: v2.18.2
          args: release --clean
`,
		".github/actions/tool/action.yaml": `name: tool
runs:
  using: composite
  steps:
    - shell: bash
      run: go install github.com/goreleaser/goreleaser/v2@v2.18.2
`,
		"web/.nvmrc": "v24.19.0\n",
		"go.mod":     "module example.com/m\n\ngo 1.27.0\n\ntoolchain go1.27.1\n\nrequire golang.org/x/mod v0.41.0\n",
	})

	want := []Site{
		{File: ".github/actions/tool/action.yaml", Line: 6, Kind: KindGoInstall, Name: "github.com/goreleaser/goreleaser/v2", Version: "v2.18.2", Pinned: true},
		{File: ".github/workflows/ci.yml", Line: 9, Kind: KindGoInstall, Name: "golang.org/x/vuln/cmd/govulncheck", Version: "v1.8.0", Pinned: true},
		{File: ".github/workflows/ci.yml", Line: 12, Kind: KindNode, Name: "node", Version: "24.19.0", Pinned: true},
		{File: ".github/workflows/other.yaml", Line: 11, Kind: KindGoInstall, Name: "honnef.co/go/tools/cmd/staticcheck", Version: "v0.7.0", Pinned: true},
		{File: ".github/workflows/other.yaml", Line: 19, Kind: KindGoreleaserAction, Name: "goreleaser", Version: "v2.18.2", Pinned: true},
		{File: "go.mod", Line: 5, Kind: KindGoToolchain, Name: "go", Version: "go1.27.1", Pinned: true},
		{File: "web/.nvmrc", Line: 1, Kind: KindNode, Name: "node", Version: "24.19.0", Pinned: true},
	}
	assertSites(t, sites, want)
}

func assertSites(t *testing.T, got, want []Site) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d sites, want %d:\n got  %+v\n want %+v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("site %d:\n got  %+v\n want %+v", i, got[i], want[i])
		}
	}
}

// nodeFileWorkflow is a workflow whose only pin is a setup-node step reading
// the named version file.
func nodeFileWorkflow(file string) string {
	return `jobs:
  a:
    steps:
      - uses: actions/setup-node@49933ea5288caeca8642d1e84afbd3f7d6820020  # v4.4.0
        with:
          node-version-file: ` + file + `
`
}

func TestScanNodeVersionFiles(t *testing.T) {
	pinned := []struct {
		name, file, body string
		line             int
	}{
		{"node-version", ".node-version", "24.19.0\n", 1},
		{"tool-versions", ".tool-versions", "golang 1.27.1\nnodejs 24.19.0\n", 2},
		{"nvmrc after a comment", ".nvmrc", "# pinned\nv24.19.0\n", 2},
	}
	for _, tc := range pinned {
		t.Run(tc.name, func(t *testing.T) {
			sites := scanTree(t, map[string]string{
				".github/workflows/ci.yml": nodeFileWorkflow(tc.file),
				tc.file:                    tc.body,
			})
			assertSites(t, sites, []Site{{File: tc.file, Line: tc.line, Kind: KindNode, Name: "node", Version: "24.19.0", Pinned: true}})
		})
	}

	unpinned := []struct {
		name, file, body, file2 string
	}{
		{"package.json", "package.json", `{"engines":{"node":">=24"}}`, ".github/workflows/ci.yml"},
		{"lts codename", ".nvmrc", "lts/iron\n", ".nvmrc"},
		{"bare node alias", ".nvmrc", "node\n", ".nvmrc"},
		{"missing file", ".node-version", "", ".github/workflows/ci.yml"},
		{"tool-versions without node", ".tool-versions", "golang 1.27.1\n", ".github/workflows/ci.yml"},
	}
	for _, tc := range unpinned {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{".github/workflows/ci.yml": nodeFileWorkflow(tc.file)}
			if tc.name != "missing file" {
				files[tc.file] = tc.body
			}
			sites := scanTree(t, files)
			if len(sites) != 1 {
				t.Fatalf("got %d sites, want 1: %+v", len(sites), sites)
			}
			s := sites[0]
			if s.Pinned || s.Version != "" || s.Reason == "" || s.Kind != KindNode {
				t.Errorf("want an unpinned node site carrying a reason and no version, got %+v", s)
			}
			if s.File != tc.file2 {
				t.Errorf("site file = %s, want %s", s.File, tc.file2)
			}
		})
	}
}

func TestScanIgnoresNonRunInstalls(t *testing.T) {
	sites := scanTree(t, map[string]string{
		".github/workflows/ci.yml": `jobs:
  a:
    env:
      INSTALL: go install example.com/env/cmd/x@v1.2.3
    steps:
      # - run: go install example.com/yamlcomment/cmd/x@v1.2.3
      - name: go install example.com/name/cmd/x@v1.2.3
        run: echo hi  # go install example.com/trailing/cmd/x@v1.2.3
      - uses: actions/github-script@60a0d83039c74a4aee543508d2ffcb1c3799cdea  # v7.0.1
        with:
          script: |
            // go install example.com/script/cmd/x@v1.2.3
      - run: |
          # go install example.com/shellcomment/cmd/x@v1.2.3
          go build ./...
`,
	})
	if len(sites) != 0 {
		t.Fatalf("want no sites outside run-block shell, got %+v", sites)
	}
}

func TestScanReportsUnpinned(t *testing.T) {
	sites := scanTree(t, map[string]string{
		".github/workflows/ci.yml": `jobs:
  a:
    steps:
      - uses: goreleaser/goreleaser-action@9ed2f89a662bf1735a48bc8557fd212fa902bebf  # v6.1.0
        with:
          distribution: goreleaser
          version: '~> v2'
      - uses: actions/setup-node@49933ea5288caeca8642d1e84afbd3f7d6820020  # v4.4.0
        with:
          node-version: ${{ matrix.node }}
      - uses: actions/setup-node@49933ea5288caeca8642d1e84afbd3f7d6820020  # v4.4.0
        with:
          node-version: '24.x'
      - run: go install example.com/tool/cmd/x@latest
`,
	})
	wantLines := []int{7, 10, 13, 14}
	if len(sites) != len(wantLines) {
		t.Fatalf("got %d sites, want %d: %+v", len(sites), len(wantLines), sites)
	}
	for i, s := range sites {
		if s.Line != wantLines[i] {
			t.Errorf("site %d on line %d, want %d", i, s.Line, wantLines[i])
		}
		if s.Pinned || s.Version != "" || s.Reason == "" {
			t.Errorf("site %d: want unpinned with a reason and no version, got %+v", i, s)
		}
	}
}

func TestScanUnpinnedShapes(t *testing.T) {
	// Each value names no single release, so each is unpinned.
	for _, v := range []string{"@v1", "@master", "@v1.2", "@latest"} {
		if s := goInstallSites("w.yml", 1, "go install example.com/x"+v); len(s) != 1 || s[0].Pinned {
			t.Errorf("go install x%s: want one unpinned site, got %+v", v, s)
		}
	}
	// A pre-release or pseudo-version is still one exact module version.
	for _, v := range []string{"@v1.2.3-rc.1", "@v0.0.0-20260101000000-abcdefabcdef", "@v2.0.0+incompatible"} {
		if s := goInstallSites("w.yml", 1, "go install example.com/x"+v); len(s) != 1 || !s[0].Pinned {
			t.Errorf("go install x%s: want one pinned site, got %+v", v, s)
		}
	}
	// A goreleaser step with no version: installs whatever is newest.
	sites := scanTree(t, map[string]string{".github/workflows/r.yml": `jobs:
  a:
    steps:
      - uses: goreleaser/goreleaser-action@9ed2f89a662bf1735a48bc8557fd212fa902bebf  # v6.1.0
        with:
          args: release
`})
	if len(sites) != 1 || sites[0].Pinned || !strings.Contains(sites[0].Reason, "no version") {
		t.Errorf("goreleaser with no version: got %+v", sites)
	}
	// A pro distribution is carried as the site's name.
	sites = scanTree(t, map[string]string{".github/workflows/r.yml": `jobs:
  a:
    steps:
      - with:
          version: v2.18.2
          distribution: goreleaser-pro
        uses: goreleaser/goreleaser-action@9ed2f89a662bf1735a48bc8557fd212fa902bebf  # v6.1.0
`})
	if len(sites) != 1 || sites[0].Name != "goreleaser-pro" || sites[0].Version != "v2.18.2" || sites[0].Line != 5 {
		t.Errorf("goreleaser-pro: got %+v", sites)
	}
	// An rc toolchain is not a release line.
	if s := toolchainSites("module m\ntoolchain go1.28rc1\n"); len(s) != 1 || s[0].Pinned {
		t.Errorf("rc toolchain: got %+v", s)
	}
	// A version file outside the repo is never read.
	sites = scanTree(t, map[string]string{".github/workflows/ci.yml": nodeFileWorkflow("../.nvmrc")})
	if len(sites) != 1 || sites[0].Pinned || !strings.Contains(sites[0].Reason, "outside") {
		t.Errorf("escaping node-version-file: got %+v", sites)
	}
}

func TestScanDedupesSharedVersionFile(t *testing.T) {
	sites := scanTree(t, map[string]string{
		".github/workflows/a.yml": nodeFileWorkflow(".node-version"),
		".github/workflows/b.yml": nodeFileWorkflow(".node-version"),
		".node-version":           "24.19.0\n",
	})
	if len(sites) != 1 {
		t.Fatalf("two workflows naming one version file: got %d sites, want 1: %+v", len(sites), sites)
	}
}

func TestGoConstSite(t *testing.T) {
	root := writeTree(t, map[string]string{"pins.go": `package x

const (
	other = 3
	toolPin = "example.com/tool/cmd/tool@v0.6.0"
)

const notString = 42

const concat = "a" + "b"
`})
	file := filepath.Join(root, "pins.go")

	v, line, err := GoConst(file, "toolPin")
	if err != nil {
		t.Fatalf("GoConst toolPin: %v", err)
	}
	if v != "example.com/tool/cmd/tool@v0.6.0" || line != 5 {
		t.Errorf("GoConst toolPin = %q line %d, want the pin on line 5", v, line)
	}
	for _, name := range []string{"missing", "notString", "concat", "other"} {
		if _, _, err := GoConst(file, name); err == nil {
			t.Errorf("GoConst %s: want an error", name)
		}
	}
}

// TestDedupeOrdersByKind: two sites on one line sort by kind, and a repeat of
// either is dropped.
func TestDedupeOrdersByKind(t *testing.T) {
	node := Site{File: "a.yml", Line: 3, Kind: KindNode, Name: "node", Version: "24.19.0", Pinned: true}
	install := Site{File: "a.yml", Line: 3, Kind: KindGoInstall, Name: "golang.org/x/vuln/cmd/govulncheck", Version: "v1.1.4", Pinned: true}
	got := dedupe([]Site{node, install, node})
	if len(got) != 2 || got[0] != install || got[1] != node {
		t.Errorf("dedupe = %+v, want [%s %s]: one line's sites ordered by kind, the repeat dropped", got, install.Kind, node.Kind)
	}
}

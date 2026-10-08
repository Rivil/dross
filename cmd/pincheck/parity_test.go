package main

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/cmd"
	"github.com/Rivil/dross/internal/pincheck"
)

// TestPinVerdictParity drives one fixture repo and one stubbed upstream through
// both callers of the shared checker — this command's run path (the weekly
// cron) and `dross doctor`'s exported section builder — and requires every pin
// doctor reports to carry the same verdict, and the same bump target, as the
// cron gave it. One checker, two callers (locked decision check_surface): if
// either path grew its own classification, a pin could be stale to one and
// current to the other. Criterion c-9.
func TestPinVerdictParity(t *testing.T) {
	root := runFixture(t, map[string]string{
		".github/workflows/release.yml": `jobs:
  release:
    steps:
      - uses: goreleaser/goreleaser-action@9ed2f89a662bf1735a48bc8557fd212fa902bebf  # v6.1.0
        with:
          distribution: goreleaser
          version: v2.18.2
`,
		".github/workflows/ci.yml": `jobs:
  test:
    steps:
      - uses: actions/setup-node@49933ea5288caeca8642d1e84afbd3f7d6820020  # v4.4.0
        with:
          node-version-file: .node-version
      - run: go install honnef.co/go/tools/cmd/staticcheck@latest
`,
		".node-version": "24.19.0\n",
		".github/actions/vuln/action.yml": `runs:
  using: composite
  steps:
    - shell: bash
      run: go install golang.org/x/vuln/cmd/govulncheck@v1.8.0
`,
	})
	stub := currentUpstream()
	stub["goreleaser"] = []pincheck.Release{old("v2.18.2"), old("v2.19.0")} // stale
	stub["node"] = []pincheck.Release{old("24.19.0"), old("26.0.0")}        // info
	// govulncheck is absent from the stub: unreachable, so unknown.
	// staticcheck@latest is unpinned; the toolchain is current.

	var out, errb bytes.Buffer
	run(root, nil, &out, &errb, deps{resolver: stub, now: runNow, getenv: func(string) string { return "" }})
	cron := map[string]string{} // "file:line" → the cron's result line
	for _, line := range strings.Split(out.String(), "\n") {
		if f := strings.Fields(line); len(f) >= 2 && strings.Contains(f[1], ":") {
			cron[f[1]] = line
		}
	}

	_, results, err := cmd.PinCurrencySection(context.Background(), root, stub, runNow)
	if err != nil {
		t.Fatalf("PinCurrencySection: %v", err)
	}
	seen := map[pincheck.Verdict]bool{}
	for _, r := range results {
		at := fmt.Sprintf("%s:%d", r.File, r.Line)
		line, ok := cron[at]
		if !ok {
			t.Errorf("doctor reports %s but the cron did not check it:\n%s", at, out.String())
			continue
		}
		if got := strings.Fields(line)[0]; got != string(r.Verdict) {
			t.Errorf("%s: cron says %s, doctor says %s", at, got, r.Verdict)
		}
		if r.Verdict == pincheck.Stale && !strings.Contains(line, "→ "+r.Target) {
			t.Errorf("%s: doctor's bump target %s is not the cron's: %s", at, r.Target, line)
		}
		seen[r.Verdict] = true
	}
	for _, v := range []pincheck.Verdict{pincheck.Stale, pincheck.Current, pincheck.Info, pincheck.Unknown, pincheck.Unpinned} {
		if !seen[v] {
			t.Errorf("the fixture never produced a %s pin on doctor's path — parity was not tested for it", v)
		}
	}
	// Doctor checks the generic subset only; the cron adds the source pins.
	if len(cron) != len(results)+len(sourcePins) {
		t.Errorf("cron checked %d pins, doctor %d — want doctor's plus the %d source pins", len(cron), len(results), len(sourcePins))
	}
}

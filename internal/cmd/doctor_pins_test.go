package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/pincheck"
	"github.com/Rivil/dross/internal/telemetry"
)

// Doctor's Pin currency section (criterion c-9) runs the cron's checker in
// lenient mode (locked decision doctor_net): a stale pin is a warning naming
// its bump target, an unpinned one warns, an unreachable upstream is a line —
// and none of it moves doctor's exit code, because /dross-ship and
// /dross-review gate on that exit code and upstream's release calendar must not
// block them. Every test here installs its own resolver; TestMain's counting
// default fails the binary if one did not.

// pinStub answers by site name; a name it does not know is an unreachable
// upstream.
type pinStub map[string][]pincheck.Release

func (s pinStub) Releases(_ context.Context, site pincheck.Site) ([]pincheck.Release, error) {
	rels, ok := s[site.Name]
	if !ok {
		return nil, errors.New("GET https://nodejs.org/dist/index.json: dial tcp: no route to host")
	}
	return rels, nil
}

func pinRelease(v string, age time.Duration) pincheck.Release {
	return pincheck.Release{Version: v, Published: time.Now().Add(-age)}
}

const day = 24 * time.Hour

// pinDoctorRepo is an initialised dross repo carrying a workflow whose
// setup-node step reads .nvmrc, plus any extra files, committed.
func pinDoctorRepo(t *testing.T, nodeWith string, extra map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	gitInit(t, dir, "https://github.com/Rivil/dross.git")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustWrite(t, filepath.Join(dir, ".github", "workflows", "ci.yml"), `jobs:
  a:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/setup-node@49933ea5288caeca8642d1e84afbd3f7d6820020  # v4.4.0
        with:
          `+nodeWith+`
`)
	for rel, body := range extra {
		mustWrite(t, filepath.Join(dir, filepath.FromSlash(rel)), body)
	}
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: baseline")
	return dir
}

// doctorWith runs doctor under stub and returns its output and error text.
func doctorWith(t *testing.T, stub pinStub) (out, errText string) {
	t.Helper()
	withPinResolver(t, stub)
	out = captureStdout(t, func() {
		if err := runCmd(t, Doctor()); err != nil {
			errText = err.Error()
		}
	})
	return out, errText
}

func currentNode() pinStub {
	return pinStub{"node": {pinRelease("24.19.0", 90*day)}}
}

func TestDoctorPinSectionStaleWarns(t *testing.T) {
	pinDoctorRepo(t, "node-version-file: .nvmrc", map[string]string{".nvmrc": "24.19.0\n"})

	_, baseline := doctorWith(t, currentNode())
	out, errText := doctorWith(t, pinStub{"node": {
		pinRelease("24.19.0", 90*day),
		pinRelease("24.20.0", 30*day),
		pinRelease("24.21.0", 2*day), // inside the cooldown: never the target
	}})

	if !strings.Contains(out, "Pin currency:") {
		t.Fatalf("no Pin currency section:\n%s", out)
	}
	if !strings.Contains(out, "⚠ .nvmrc:1 node 24.19.0 is stale — bump to 24.20.0") {
		t.Errorf("want a warning naming the pin and its bump target 24.20.0:\n%s", out)
	}
	if errText != baseline {
		t.Errorf("a stale pin moved doctor's verdict: %q with a stale pin, %q without — it must stay a warning", errText, baseline)
	}
}

func TestDoctorPinSectionUnknownIsNotIssue(t *testing.T) {
	t.Run("unreachable upstream", func(t *testing.T) {
		pinDoctorRepo(t, "node-version-file: .nvmrc", map[string]string{".nvmrc": "24.19.0\n"})
		_, baseline := doctorWith(t, currentNode())
		out, errText := doctorWith(t, pinStub{})
		if !strings.Contains(out, "· .nvmrc:1 node 24.19.0 — could not check upstream") {
			t.Errorf("want an unknown line for the unreachable upstream:\n%s", out)
		}
		if strings.Contains(out, "✗ .nvmrc") {
			t.Errorf("an unreachable upstream was reported as an issue:\n%s", out)
		}
		if errText != baseline {
			t.Errorf("offline moved doctor's verdict: %q offline, %q online", errText, baseline)
		}
	})

	t.Run("unpinned site", func(t *testing.T) {
		pinDoctorRepo(t, "node-version: '24.x'", nil)
		out, errText := doctorWith(t, pinStub{})
		if !strings.Contains(out, "⚠ .github/workflows/ci.yml:7 node is not pinned to an exact release") {
			t.Errorf("want a warning for the unpinned node-version:\n%s", out)
		}
		// The same repo with the pin made exact: the verdict must not differ.
		mustWrite(t, filepath.Join(".github", "workflows", "ci.yml"), strings.Replace(mustRead(t, filepath.Join(".github", "workflows", "ci.yml")), "'24.x'", "'24.19.0'", 1))
		_, baseline := doctorWith(t, currentNode())
		if errText != baseline {
			t.Errorf("an unpinned site moved doctor's verdict: %q unpinned, %q pinned", errText, baseline)
		}
	})
}

// TestDoctorPinSectionSilentWithoutPins: a repo holding no such pin gains no
// section — and makes no upstream lookup at all.
func TestDoctorPinSectionSilentWithoutPins(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "https://github.com/Rivil/dross.git")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	calls := 0
	withPinResolver(t, resolverFunc(func() { calls++ }))
	out := captureStdout(t, func() { _ = runCmd(t, Doctor()) })
	if strings.Contains(out, "Pin currency:") {
		t.Errorf("a repo with no pins printed a Pin currency section:\n%s", out)
	}
	if calls != 0 {
		t.Errorf("doctor made %d upstream lookup(s) in a repo with no pins", calls)
	}
}

// TestDoctorPinScanErrorWarns: a pin scan that fails outright — here a
// node-version-file naming a directory — prints what failed and counts as one
// warning in doctor's outcome, never an issue.
func TestDoctorPinScanErrorWarns(t *testing.T) {
	run := func(extra map[string]string, stub pinStub) (out, errText string, warnings int) {
		pinDoctorRepo(t, "node-version-file: .nvmrc", extra)
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("DROSS_NO_TELEMETRY", "") // re-enable (chdir pins it to "1")
		out, errText = doctorWith(t, stub)
		return out, errText, doctorOutcomeWarnings(t, filepath.Join(home, ".claude", "dross", telemetry.File))
	}
	_, baseline, baseWarnings := run(map[string]string{".nvmrc": "24.19.0\n"}, currentNode())
	out, errText, warnings := run(map[string]string{".nvmrc/keep": ""}, pinStub{})

	if !strings.Contains(out, "⚠ could not scan the repo's pins:") {
		t.Errorf("want a warning naming the failed scan:\n%s", out)
	}
	if warnings != baseWarnings+1 {
		t.Errorf("doctor recorded %d warning(s) with a failed pin scan, %d without — the scan error must count as exactly one", warnings, baseWarnings)
	}
	if errText != baseline {
		t.Errorf("a failed pin scan moved doctor's verdict: %q failed, %q scanned", errText, baseline)
	}
}

// doctorOutcomeWarnings is the warnings count on the last doctor outcome event
// in the telemetry file at path.
func doctorOutcomeWarnings(t *testing.T, path string) int {
	t.Helper()
	body := mustRead(t, path)
	warnings, found := 0, false
	for _, line := range strings.Split(body, "\n") {
		var ev telemetry.Event
		if json.Unmarshal([]byte(line), &ev) != nil || ev.Kind != "outcome" || ev.Command != "doctor" {
			continue
		}
		if w, ok := ev.Counts["warnings"]; ok {
			warnings, found = w, true
		}
	}
	if !found {
		t.Fatalf("no doctor outcome event carrying warnings in %s:\n%s", path, body)
	}
	return warnings
}

// resolverFunc counts lookups and fails them.
type resolverFunc func()

func (f resolverFunc) Releases(context.Context, pincheck.Site) ([]pincheck.Release, error) {
	f()
	return nil, fmt.Errorf("unexpected lookup")
}

func TestPinCurrencyLineLevels(t *testing.T) {
	site := pincheck.Site{File: "go.mod", Line: 5, Kind: pincheck.KindGoToolchain, Name: "go", Version: "go1.27.1", Pinned: true}
	for _, tc := range []struct {
		verdict pincheck.Verdict
		sev     pincheck.Severity
		level   string
		want    string
	}{
		{pincheck.Current, pincheck.SeverityOK, "ok", "go.mod:5 go go1.27.1 is current"},
		{pincheck.Stale, pincheck.SeverityWarn, "warn", "bump to go1.27.2"},
		{pincheck.Unpinned, pincheck.SeverityWarn, "warn", "not pinned"},
		{pincheck.Info, pincheck.SeverityLine, "note", "off its line (go1.28.0)"},
		{pincheck.Unknown, pincheck.SeverityLine, "note", "could not check upstream: offline"},
		{pincheck.Stale, pincheck.SeverityFail, "issue", "bump to go1.27.2"},
	} {
		res := pincheck.Result{Site: site, Severity: tc.sev, Classification: pincheck.Classification{Verdict: tc.verdict, Target: "go1.27.2", Latest: "go1.28.0", Reason: "offline"}}
		if tc.verdict == pincheck.Unpinned {
			res.Classification.Reason = "toolchain default is not a released go1.X.Y toolchain"
		}
		l := pinCurrencyLine(res)
		if l.Level.String() != tc.level || !strings.Contains(l.Text, tc.want) {
			t.Errorf("%s/%s: got %s %q, want %s containing %q", tc.verdict, tc.sev, l.Level, l.Text, tc.level, tc.want)
		}
	}
}

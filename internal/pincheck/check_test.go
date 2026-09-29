package pincheck

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeResolver answers from a table keyed by site name; a name mapped to nil
// releases fails the fetch.
type fakeResolver map[string][]Release

func (f fakeResolver) Releases(_ context.Context, s Site) ([]Release, error) {
	rels, ok := f[s.Name]
	if !ok || rels == nil {
		return nil, errors.New("upstream unreachable")
	}
	return rels, nil
}

func pinned(name, version string) Site {
	return Site{File: "x.yml", Line: 1, Kind: KindGoInstall, Name: name, Version: version, Pinned: true}
}

func TestCheckModes(t *testing.T) {
	resolver := fakeResolver{
		"stale":   {{Version: "v1.1.0", Published: daysAgo(30)}},
		"current": {{Version: "v1.0.0", Published: daysAgo(30)}},
		"info":    {{Version: "v2.0.0", Published: daysAgo(30)}},
	}
	unpinnedSite := Site{File: "x.yml", Line: 2, Kind: KindGoInstall, Name: "unpinned", Reason: "go install x@latest is not an exact module version"}

	for _, tc := range []struct {
		name            string
		site            Site
		verdict         Verdict
		strict, lenient Severity
	}{
		{"stale", pinned("stale", "v1.0.0"), Stale, SeverityFail, SeverityWarn},
		{"unknown", pinned("unknown", "v1.0.0"), Unknown, SeverityFail, SeverityLine},
		{"unpinned", unpinnedSite, Unpinned, SeverityFail, SeverityWarn},
		{"info", pinned("info", "v1.0.0"), Info, SeverityLine, SeverityLine},
		{"current", pinned("current", "v1.0.0"), Current, SeverityOK, SeverityOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, m := range []struct {
				mode Mode
				want Severity
			}{{Strict, tc.strict}, {Lenient, tc.lenient}} {
				rep := Check(context.Background(), []Site{tc.site}, resolver, m.mode, classifyNow)
				res := rep.Results[0]
				if res.Verdict != tc.verdict || res.Severity != m.want {
					t.Errorf("mode %d: got %s/%s, want %s/%s", m.mode, res.Verdict, res.Severity, tc.verdict, m.want)
				}
				if rep.Failed != (m.want == SeverityFail) {
					t.Errorf("mode %d: Failed = %v with severity %s", m.mode, rep.Failed, m.want)
				}
			}
		})
	}

	// Lenient fails on nothing, even all of them at once.
	all := []Site{pinned("stale", "v1.0.0"), pinned("unknown", "v1.0.0"), unpinnedSite, pinned("info", "v1.0.0")}
	if rep := Check(context.Background(), all, resolver, Lenient, classifyNow); rep.Failed {
		t.Error("lenient check failed — doctor must never fail on a pin")
	}
	if rep := Check(context.Background(), all, resolver, Strict, classifyNow); !rep.Failed || !rep.Stale() {
		t.Errorf("strict check over a stale set: Failed=%v Stale=%v, want both", rep.Failed, rep.Stale())
	}
	if rep := Check(context.Background(), []Site{pinned("unknown", "v1.0.0"), unpinnedSite}, resolver, Strict, classifyNow); !rep.Failed || rep.Stale() {
		t.Errorf("unknown+unpinned: Failed=%v Stale=%v, want failed and not stale", rep.Failed, rep.Stale())
	}
}

func TestResultString(t *testing.T) {
	for _, tc := range []struct {
		res  Result
		want []string
	}{
		{Result{Site: pinned("x", "v1.0.0"), Classification: Classification{Verdict: Stale, Target: "v1.1.0"}}, []string{"stale", "x.yml:1", "x v1.0.0 → v1.1.0"}},
		{Result{Site: pinned("x", "v1.0.0"), Classification: Classification{Verdict: Info, Latest: "v2.0.0"}}, []string{"info", "v2.0.0"}},
		{Result{Site: pinned("x", "v1.0.0"), Classification: Classification{Verdict: Unknown, Reason: "GET … 503"}}, []string{"unknown", "503"}},
	} {
		got := tc.res.String()
		for _, w := range tc.want {
			if !strings.Contains(got, w) {
				t.Errorf("%q lacks %q", got, w)
			}
		}
	}
}

func TestSpecSite(t *testing.T) {
	for _, tc := range []struct {
		kind       Kind
		spec       string
		name, ver  string
		wantPinned bool
	}{
		{KindGoInstall, "github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0", "github.com/go-gremlins/gremlins/cmd/gremlins", "v0.6.0", true},
		{KindNPM, "@stryker-mutator/core@9.6.1", "@stryker-mutator/core", "9.6.1", true},
		{KindGoInstall, "example.com/x@latest", "example.com/x", "", false},
		{KindNPM, "@stryker-mutator/core@^9.6.1", "@stryker-mutator/core", "", false},
		{KindNPM, "@stryker-mutator/core", "@stryker-mutator/core", "", false},
	} {
		s := SpecSite("f.go", 7, tc.kind, tc.spec)
		if s.Name != tc.name || s.Version != tc.ver || s.Pinned != tc.wantPinned || s.Line != 7 || s.File != "f.go" {
			t.Errorf("SpecSite(%q) = %+v", tc.spec, s)
		}
		if !s.Pinned && s.Reason == "" {
			t.Errorf("SpecSite(%q): unpinned without a reason", tc.spec)
		}
	}
}

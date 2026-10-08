package pincheck

import (
	"testing"
	"time"
)

var classifyNow = time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

func daysAgo(d float64) time.Time {
	return classifyNow.Add(-time.Duration(d * float64(24*time.Hour)))
}

func TestClassifyCooldownBoundary(t *testing.T) {
	for _, tc := range []struct {
		name string
		age  time.Time
		want Verdict
	}{
		{"8 days", daysAgo(8), Stale},
		{"6 days", daysAgo(6), Current},
		{"exactly 7 days", daysAgo(7), Current},
		{"7 days and a second", daysAgo(7).Add(-time.Second), Stale},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(KindGoInstall, "v1.8.0", []Release{
				{Version: "v1.8.0", Published: daysAgo(90)},
				{Version: "v1.8.1", Published: tc.age},
			}, classifyNow)
			if got.Verdict != tc.want {
				t.Fatalf("verdict = %s, want %s (%+v)", got.Verdict, tc.want, got)
			}
			if tc.want == Stale && got.Target != "v1.8.1" {
				t.Errorf("target = %q, want v1.8.1", got.Target)
			}
			if tc.want == Current && got.Target != "" {
				t.Errorf("current pin carries target %q", got.Target)
			}
		})
	}

	// A Node release dated by day only counts from that day's 00:00 UTC.
	published, err := ParseReleaseDate("2026-09-07")
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC); !published.Equal(want) {
		t.Fatalf("ParseReleaseDate(2026-09-07) = %v, want %v", published, want)
	}
	rels := []Release{{Version: "24.20.0", Published: published}}
	edge := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	if got := Classify(KindNode, "24.19.0", rels, edge); got.Verdict != Current {
		t.Errorf("day-dated release exactly 7 days old: verdict %s, want current", got.Verdict)
	}
	if got := Classify(KindNode, "24.19.0", rels, edge.Add(time.Second)); got.Verdict != Stale || got.Target != "24.20.0" {
		t.Errorf("day-dated release 7 days and a second old: got %+v, want stale → 24.20.0", got)
	}
}

func TestParseReleaseDate(t *testing.T) {
	rfc, err := ParseReleaseDate("2026-09-01T10:11:12.5Z")
	if err != nil || !rfc.Equal(time.Date(2026, 9, 1, 10, 11, 12, 5e8, time.UTC)) {
		t.Errorf("RFC 3339: got %v, %v", rfc, err)
	}
	if _, err := ParseReleaseDate("Sept 1"); err == nil {
		t.Error("an unparseable date must be an error")
	}
}

func TestBumpTargetClearsCooldown(t *testing.T) {
	got := Classify(KindNode, "24.19.0", []Release{
		{Version: "24.19.0", Published: daysAgo(60)},
		{Version: "24.20.0", Published: daysAgo(30)},
		{Version: "24.21.0", Published: daysAgo(2)},
	}, classifyNow)
	if got.Verdict != Stale || got.Target != "24.20.0" {
		t.Fatalf("got %+v, want stale with target 24.20.0 (24.21.0 is inside the cooldown)", got)
	}
	if got.Latest != "24.21.0" {
		t.Errorf("latest = %q, want 24.21.0", got.Latest)
	}
}

func TestClassifyOffLineIsInfo(t *testing.T) {
	for _, tc := range []struct {
		name, pin, next string
		kind            Kind
	}{
		{"go toolchain: next minor", "go1.27.1", "go1.28.0", KindGoToolchain},
		{"node: next major", "24.19.0", "26.0.0", KindNode},
		{"tool: next major", "v1.8.0", "v2.0.0", KindGoInstall},
		{"goreleaser: next major", "v2.18.2", "v3.0.0", KindGoreleaserAction},
		{"npm: next major", "9.6.1", "10.0.0", KindNPM},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(tc.kind, tc.pin, []Release{
				{Version: tc.pin, Published: daysAgo(90)},
				{Version: tc.next, Published: daysAgo(60)},
			}, classifyNow)
			if got.Verdict != Info || got.Target != "" {
				t.Fatalf("got %+v, want info with no target", got)
			}
			if got.Latest != tc.next {
				t.Errorf("latest = %q, want %q", got.Latest, tc.next)
			}
		})
	}

	// On the toolchain's own minor a patch is stale; a tool's minor on its
	// major is stale too.
	if got := Classify(KindGoToolchain, "go1.27.1", []Release{{Version: "go1.27.2", Published: daysAgo(30)}}, classifyNow); got.Verdict != Stale || got.Target != "go1.27.2" {
		t.Errorf("toolchain patch on the pinned minor: got %+v, want stale → go1.27.2", got)
	}
	if got := Classify(KindGoInstall, "v1.8.0", []Release{{Version: "v1.9.0", Published: daysAgo(30)}}, classifyNow); got.Verdict != Stale || got.Target != "v1.9.0" {
		t.Errorf("tool minor on the pinned major: got %+v, want stale → v1.9.0", got)
	}
	// A stale pin with a newer major beside it is stale, and bumps on-line.
	if got := Classify(KindNode, "24.19.0", []Release{
		{Version: "24.20.0", Published: daysAgo(30)},
		{Version: "26.1.0", Published: daysAgo(30)},
	}, classifyNow); got.Verdict != Stale || got.Target != "24.20.0" {
		t.Errorf("stale with a newer major present: got %+v, want stale → 24.20.0", got)
	}
}

func TestClassifyUsesListMaximum(t *testing.T) {
	// gremlins' shape: v0.5.1 was published after v0.6.0, so a proxy's
	// @latest (or a newest-by-date pick) calls v0.5.1 the latest.
	got := Classify(KindGoInstall, "v0.6.0", []Release{
		{Version: "v0.5.0", Published: daysAgo(400)},
		{Version: "v0.6.0", Published: daysAgo(200)},
		{Version: "v0.5.1", Published: daysAgo(100)},
	}, classifyNow)
	if got.Verdict != Current || got.Latest != "v0.6.0" {
		t.Fatalf("got %+v, want current with latest v0.6.0", got)
	}
}

func TestClassifyIgnoresPrereleases(t *testing.T) {
	for _, tc := range []struct {
		kind     Kind
		pin, pre string
	}{
		{KindGoInstall, "v1.8.0", "v1.9.0-rc.1"},
		{KindGoreleaserAction, "v2.18.2", "v2.19.0-nightly"},
		{KindNode, "24.19.0", "24.20.0-beta"},
		{KindGoInstall, "v1.8.0", "v2.0.0-rc.1"},
		{KindGoToolchain, "go1.27.1", "go1.27rc2"},
	} {
		got := Classify(tc.kind, tc.pin, []Release{
			{Version: tc.pin, Published: daysAgo(90)},
			{Version: tc.pre, Published: daysAgo(30)},
		}, classifyNow)
		if got.Verdict != Current || got.Target != "" || got.Latest != tc.pin {
			t.Errorf("%s over %s: got %+v, want current with latest %s", tc.pre, tc.pin, got, tc.pin)
		}
	}
}

func TestClassifyUncomparablePinIsUnknown(t *testing.T) {
	for _, pin := range []string{"latest", "1.27.1", "go1.27rc1"} {
		if got := Classify(KindGoToolchain, pin, nil, classifyNow); got.Verdict != Unknown || got.Reason == "" {
			t.Errorf("toolchain pin %q: got %+v, want unknown with a reason", pin, got)
		}
	}
}

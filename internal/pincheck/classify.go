package pincheck

import (
	"fmt"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

// Cooldown is how long a release must have been public before a pin behind it
// counts as stale — Dependabot's cooldown, so the two bots agree on when a
// release is old enough to adopt. A release exactly Cooldown old is still
// inside it.
const Cooldown = 7 * 24 * time.Hour

// Release is one upstream version and when it was published.
type Release struct {
	Version   string
	Published time.Time
}

// Verdict is what the check concluded about one site.
type Verdict string

const (
	// Current: no release on the pin's line is both newer and past the cooldown.
	Current Verdict = "current"
	// Stale: a newer release on the pin's line has been out longer than the
	// cooldown. Target names the version to move to.
	Stale Verdict = "stale"
	// Info: the pin is current on its line, but a newer major (or, for the Go
	// toolchain, minor) exists. A migration, not a chore — never a failure.
	Info Verdict = "info"
	// Unknown: upstream could not be read, so nothing was concluded.
	Unknown Verdict = "unknown"
	// Unpinned: the site names no exact release to judge.
	Unpinned Verdict = "unpinned"
)

// Classification is the verdict on one pinned version against its upstream.
type Classification struct {
	Verdict Verdict
	// Target is the version a bump moves the pin to, spelled the way the pin
	// is spelled. Set only when Verdict is Stale.
	Target string
	// Latest is the highest stable version in the whole list, on the pin's
	// line or not. It is what an Info verdict points at.
	Latest string
	// Reason explains an Unknown verdict.
	Reason string
}

// Classify judges a pinned version of the given kind against upstream's
// releases as of now.
//
// Only the pin's own line can make it stale: a Go toolchain is judged on its
// minor (go1.27.x), Node on its major (24.x), every other tool on its major. A
// newer release off that line is Info. "Latest" is the highest stable version
// in the list — never whatever an @latest endpoint returns, and never the most
// recently published: a registry can publish a v0.5.1 fix after v0.6.0, and the
// pin on v0.6.0 is still the newest. Pre-releases are dropped before anything
// is compared.
//
// A pin is stale when an on-line release newer than it is more than Cooldown
// old, and its Target is the newest such release. A newer release still inside
// the cooldown is never the target, so a bump never lands on something younger
// than Dependabot would take.
func Classify(kind Kind, pinned string, releases []Release, now time.Time) Classification {
	pin := comparable(kind, pinned)
	if pin == "" {
		return Classification{Verdict: Unknown, Reason: fmt.Sprintf("pinned version %q is not a comparable release", pinned)}
	}
	var target, latest, offLine string
	for _, r := range releases {
		v := comparable(kind, r.Version)
		if v == "" {
			continue
		}
		if latest == "" || semver.Compare(v, latest) > 0 {
			latest = v
		}
		if semver.Compare(v, pin) <= 0 {
			continue
		}
		if !onLine(kind, pin, v) {
			if offLine == "" || semver.Compare(v, offLine) > 0 {
				offLine = v
			}
			continue
		}
		if now.Sub(r.Published) <= Cooldown {
			continue
		}
		if target == "" || semver.Compare(v, target) > 0 {
			target = v
		}
	}
	c := Classification{Verdict: Current}
	if latest != "" {
		c.Latest = spell(kind, pinned, latest)
	}
	switch {
	case target != "":
		c.Verdict, c.Target = Stale, spell(kind, pinned, target)
	case offLine != "":
		c.Verdict = Info
	}
	return c
}

// comparable maps a version of the given kind onto a stable semver string
// ("v1.2.3"), or "" when it is not a stable release: go1.27.1 → v1.27.1,
// 24.19.0 → v24.19.0. Pre-releases, rc toolchains and anything unparseable map
// to "".
func comparable(kind Kind, v string) string {
	if kind == KindGoToolchain {
		rest, ok := strings.CutPrefix(v, "go")
		if !ok {
			return ""
		}
		v = rest
	}
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	if !exactRelease.MatchString(v) || !semver.IsValid(v) {
		return ""
	}
	return v
}

// onLine reports whether v sits on pin's line: the same minor for a Go
// toolchain, the same major for everything else.
func onLine(kind Kind, pin, v string) bool {
	if kind == KindGoToolchain {
		return semver.MajorMinor(v) == semver.MajorMinor(pin)
	}
	return semver.Major(v) == semver.Major(pin)
}

// spell writes a comparable version back in the pin's own spelling, so a bump
// replaces like with like: go1.27.2 for a toolchain, 24.20.0 for a Node pin
// written without a v, v1.9.0 for a module.
func spell(kind Kind, pinned, v string) string {
	if kind == KindGoToolchain {
		return "go" + strings.TrimPrefix(v, "v")
	}
	if strings.HasPrefix(pinned, "v") {
		return v
	}
	return strings.TrimPrefix(v, "v")
}

// ParseReleaseDate reads an upstream publish time: RFC 3339, or a bare
// 2006-01-02 date (nodejs.org's index carries only the day), which is taken as
// that day's 00:00 UTC.
func ParseReleaseDate(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("pincheck: release date %q is neither RFC 3339 nor YYYY-MM-DD", s)
	}
	return t.UTC(), nil
}

package pincheck

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Mode picks how hard a verdict lands.
type Mode int

const (
	// Strict is the scheduled cron's mode: a stale, unknown or unpinned site
	// fails the run. It is the fail-closed path — an upstream that could not be
	// read is a red run, never a green one.
	Strict Mode = iota
	// Lenient is `dross doctor`'s mode: nothing fails. Stale and unpinned are
	// warnings, unknown and info are plain lines. Doctor must stay usable
	// offline and must not block shipping on upstream's release calendar
	// (locked decision doctor_net).
	Lenient
)

// Severity is how one result reads under a mode.
type Severity string

const (
	SeverityOK   Severity = "ok"
	SeverityLine Severity = "line"
	SeverityWarn Severity = "warn"
	SeverityFail Severity = "fail"
)

// Result is one site and what the check concluded about it.
type Result struct {
	Site
	Classification
	Severity Severity
}

// Report is a check over a set of sites.
type Report struct {
	Results []Result
	// Failed is true when any result's severity is SeverityFail.
	Failed bool
}

// Stale reports whether any result is Stale — the signal that a bump has
// something to do, whatever else failed.
func (r Report) Stale() bool {
	for _, res := range r.Results {
		if res.Verdict == Stale {
			return true
		}
	}
	return false
}

// severity maps a verdict to how it lands under mode.
func severity(mode Mode, v Verdict) Severity {
	switch v {
	case Current:
		return SeverityOK
	case Info:
		return SeverityLine
	case Stale, Unpinned:
		if mode == Strict {
			return SeverityFail
		}
		return SeverityWarn
	default: // Unknown
		if mode == Strict {
			return SeverityFail
		}
		return SeverityLine
	}
}

// Check judges every site against its upstream as of now. One checker, two
// callers (locked decision check_surface): the dross repo's weekly cron runs it
// Strict over every pin, and `dross doctor` runs it Lenient over the generic
// subset, so the two can never classify a pin differently.
func Check(ctx context.Context, sites []Site, r Resolver, mode Mode, now time.Time) Report {
	var rep Report
	for _, s := range sites {
		c := Judge(ctx, r, s, now)
		res := Result{Site: s, Classification: c, Severity: severity(mode, c.Verdict)}
		if res.Severity == SeverityFail {
			rep.Failed = true
		}
		rep.Results = append(rep.Results, res)
	}
	return rep
}

// String is the one-line report for a result: the verdict, where the pin is
// written, what it pins, and what the verdict points at.
func (r Result) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-8s %s:%d %s", r.Verdict, r.File, r.Line, r.Name)
	if r.Version != "" {
		fmt.Fprintf(&b, " %s", r.Version)
	}
	switch r.Verdict {
	case Stale:
		fmt.Fprintf(&b, " → %s", r.Target)
	case Info:
		fmt.Fprintf(&b, " (newer off its line: %s)", r.Latest)
	case Unknown, Unpinned:
		fmt.Fprintf(&b, " — %s", r.Classification.Reason)
	}
	return b.String()
}

// SpecSite builds a site from a `<pkg>@<version>` pin held somewhere the
// scanner does not read — a Go-source const such as gremlinsPin or strykerPin.
// Name is everything before the last `@` (npm scopes start with one), and the
// version must be exact for the kind: a module version for a go install pin,
// a plain X.Y.Z for an npm one.
func SpecSite(file string, line int, kind Kind, spec string) Site {
	s := Site{File: file, Line: line, Kind: kind}
	i := strings.LastIndex(spec, "@")
	if i <= 0 {
		s.Name, s.Reason = spec, fmt.Sprintf("%q is not a <pkg>@<version> pin", spec)
		return s
	}
	s.Name = spec[:i]
	v := spec[i+1:]
	exact := exactRelease.MatchString(v)
	if kind == KindGoInstall {
		exact = exactModuleVersion(v)
	}
	if !exact {
		s.Reason = fmt.Sprintf("%s is not an exact version", spec)
		return s
	}
	s.Version, s.Pinned = v, true
	return s
}

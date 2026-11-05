package memadvise

import (
	"fmt"
	"strings"
	"time"
)

// Render prints a report as advice lines: one per suggestion (all of them
// when limit <= 0), a "+N more" pointer when truncated, an integrity line when
// the index has problems the list doesn't already carry, and the share line
// the $/month figures rest on. It names files, hooks and reasons — never a
// memory's body. Nothing to say renders nothing at all.
func Render(r Report, limit int) string {
	dupNames := len(r.Integrity.DuplicateNames)
	if !r.Found || len(r.Suggestions) == 0 && dupNames == 0 {
		return ""
	}
	var b strings.Builder
	shown := r.Suggestions
	if limit > 0 && len(shown) > limit {
		shown = shown[:limit]
	}
	for _, s := range shown {
		b.WriteString(renderItem(s))
		b.WriteByte('\n')
	}
	if more := len(r.Suggestions) - len(shown); more > 0 {
		fmt.Fprintf(&b, "  +%d more — dross memory advise\n", more)
	}
	if line := integrityLine(r.Integrity); line != "" {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "  this repo: %d of %d assistant turns (%s) in 30d · turns counted %s ago · $%.2f/MTok cache read\n",
		r.Turns.Repo, r.Turns.All, ShareText(r.Turns), fmtAge(r.Turns.Age), CacheReadUSDPerMTok)
	return b.String()
}

func renderItem(s Suggestion) string {
	line := fmt.Sprintf("  %s  %s  %s  $%.2f/mo  %s", s.ID, s.File, s.Action, s.MonthlyUSD, s.Reason)
	if s.Action == ActionReword {
		line += fmt.Sprintf(" → %q", s.NewHook)
	}
	if s.ReportOnly {
		line += "  [report only]"
	}
	return line
}

// integrityLine counts the index problems. Dead links and orphans are listed
// as items too; duplicated name: slugs have no item, so this is where they show.
func integrityLine(in Integrity) string {
	var parts []string
	if n := len(in.DeadLinks); n > 0 {
		parts = append(parts, plural(n, "dead index link"))
	}
	if n := len(in.Orphans); n > 0 {
		parts = append(parts, plural(n, "orphan"))
	}
	if n := len(in.DuplicateNames); n > 0 {
		parts = append(parts, plural(n, "duplicated name: slug"))
	}
	if len(parts) == 0 {
		return ""
	}
	return "  integrity: " + strings.Join(parts, ", ")
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// fmtAge rounds a cache age to what a reader needs: hours past one, else minutes.
func fmtAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d >= time.Hour {
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	return fmt.Sprintf("%dm", int(d/time.Minute))
}

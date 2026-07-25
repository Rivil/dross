package cmd

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/defaults"
	"github.com/Rivil/dross/internal/telemetry"
)

// Stats registers `dross stats` — read the local telemetry log and
// surface aggregates. Subcommands: `show` (default), `path`, `opt-in`,
// `opt-out`.
func Stats() *cobra.Command {
	c := &cobra.Command{
		Use:   "stats",
		Short: "Inspect local usage telemetry recorded at ~/.claude/dross/telemetry.jsonl",
	}
	c.AddCommand(statsShow(), statsPath(), statsOptIn(), statsOptOut())
	c.RunE = statsShow().RunE // bare `dross stats` runs `show`
	return c
}

func statsShow() *cobra.Command {
	var since string
	c := &cobra.Command{
		Use:   "show",
		Short: "Print aggregate views of recorded events",
		RunE: func(_ *cobra.Command, _ []string) error {
			path := telemetryPath()
			events, err := telemetry.Load(path)
			if err != nil {
				return err
			}
			cutoff := parseSince(since)
			if !cutoff.IsZero() {
				events = filterSince(events, cutoff)
			}
			if len(events) == 0 {
				Printf("(no telemetry events at %s)\n", path)
				return nil
			}
			renderHeader(events, path)
			renderTopCommands(events)
			renderErrorBuckets(events)
			renderForceFlags(events)
			renderOutcomes(events)
			return nil
		},
	}
	c.Flags().StringVar(&since, "since", "", "filter to events newer than (e.g. 7d, 24h, 2026-05-01)")
	return c
}

func statsPath() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the telemetry file path",
		RunE: func(_ *cobra.Command, _ []string) error {
			Print(telemetryPath())
			return nil
		},
	}
}

func statsOptIn() *cobra.Command {
	return &cobra.Command{
		Use:   "opt-in",
		Short: "Enable telemetry recording",
		RunE: func(_ *cobra.Command, _ []string) error {
			return setTelemetryEnabled(true)
		},
	}
}

func statsOptOut() *cobra.Command {
	return &cobra.Command{
		Use:   "opt-out",
		Short: "Disable telemetry recording",
		RunE: func(_ *cobra.Command, _ []string) error {
			return setTelemetryEnabled(false)
		},
	}
}

// setTelemetryEnabled persists the bit in defaults.toml and stamps
// asked_at so init/onboard knows the user has been prompted.
func setTelemetryEnabled(on bool) error {
	dir, err := GlobalDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, defaults.File)
	d, err := defaults.LoadFile(path)
	if err != nil {
		return err
	}
	d.Telemetry.Enabled = &on
	d.Telemetry.AskedAt = time.Now().UTC().Format("2006-01-02")
	if err := d.SaveFile(path); err != nil {
		return err
	}
	if on {
		Printf("Telemetry enabled. Events written to %s\n", telemetryPath())
	} else {
		Printf("Telemetry disabled. Existing events at %s are kept; new events suppressed.\n", telemetryPath())
	}
	return nil
}

// --- aggregations ---

func renderHeader(events []telemetry.Event, path string) {
	first := events[0].Timestamp
	last := events[len(events)-1].Timestamp
	Printf("# dross telemetry — %s\n", path)
	Printf("  events:  %d\n", len(events))
	Printf("  span:    %s → %s (%s)\n",
		first.Format("2006-01-02"),
		last.Format("2006-01-02"),
		humanDuration(last.Sub(first)))
	Print("")
}

func renderTopCommands(events []telemetry.Event) {
	type row struct {
		cmd    string
		count  int
		errors int
		totMS  int64
	}
	by := map[string]*row{}
	for _, e := range events {
		if e.Kind != "cli" || e.Command == "" {
			continue
		}
		r, ok := by[e.Command]
		if !ok {
			r = &row{cmd: e.Command}
			by[e.Command] = r
		}
		r.count++
		r.totMS += e.DurationMS
		if e.ExitCode != 0 {
			r.errors++
		}
	}
	rows := make([]*row, 0, len(by))
	for _, r := range by {
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].count > rows[j].count })

	Print("## commands (top 10 by count)")
	Printf("  %-30s %8s %8s %12s\n", "command", "calls", "errors", "median_ms")
	max := 10
	if len(rows) < max {
		max = len(rows)
	}
	for _, r := range rows[:max] {
		median := int64(0)
		if r.count > 0 {
			median = r.totMS / int64(r.count) // good enough approximation
		}
		Printf("  %-30s %8d %8d %12d\n", r.cmd, r.count, r.errors, median)
	}
	Print("")
}

// tailTopN caps the unclassified-shape list printed under the errors table.
// The tail is a work queue — the shapes worth graduating into named buckets —
// so it wants the head of the distribution, not the whole long tail.
const tailTopN = 5

// tailDetailLen caps a tail line. err_detail is already capped at 240 runes,
// but a wrapped error that long turns the block into a wall; 100 runes is
// enough to recognise a shape.
const tailDetailLen = 100

func renderErrorBuckets(events []telemetry.Event) {
	by := map[string]int{}
	tail := map[string]int{}
	total := 0
	for _, e := range events {
		if e.ErrorClass == "" {
			continue
		}
		// Count the effective class, not the stored one: a bucket added after
		// an event was written still applies to it. The log is not rewritten.
		class := telemetry.Reclassify(e)
		by[class]++
		total++
		// Whatever is still "other" after re-derivation is genuinely
		// unclassified — that's the graduation queue.
		if class == "other" && e.ErrorDetail != "" {
			tail[e.ErrorDetail]++
		}
	}
	if total == 0 {
		Print("## errors")
		Print("  (no failed invocations recorded)")
		Print("")
		return
	}
	type kv struct {
		k string
		v int
	}
	rows := make([]kv, 0, len(by))
	for k, v := range by {
		rows = append(rows, kv{k, v})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].v > rows[j].v })

	Print("## errors (by class)")
	for _, r := range rows {
		Printf("  %-16s %4d\n", r.k, r.v)
	}
	renderOtherTail(tail)
	Print("")
}

// renderOtherTail prints the unclassified `other` shapes as an indented block
// under the errors table — the queue of message shapes that have earned a named
// bucket but not got one yet. Omitted entirely when the tail is empty, so the
// section costs nothing once the bucket is drained.
func renderOtherTail(tail map[string]int) {
	if len(tail) == 0 {
		return
	}
	type kv struct {
		k string
		v int
	}
	rows := make([]kv, 0, len(tail))
	for k, v := range tail {
		rows = append(rows, kv{k, v})
	}
	// Count descending, then shape ascending — map iteration order must not
	// leak into the output, or equal-count shapes reorder between runs.
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].v != rows[j].v {
			return rows[i].v > rows[j].v
		}
		return rows[i].k < rows[j].k
	})
	if len(rows) > tailTopN {
		rows = rows[:tailTopN]
	}
	Printf("  unclassified `other` shapes (top %d):\n", len(rows))
	for _, r := range rows {
		Printf("    %4d  %s\n", r.v, oneLineDetail(r.k))
	}
}

// oneLineDetail flattens an err_detail for the tail block: embedded newlines
// and tabs collapse to single spaces (cobra's "Did you mean this?" suggestions
// are multi-line, and telemetry.Detail preserves them), then the result is
// truncated so one shape occupies exactly one line.
func oneLineDetail(detail string) string {
	s := strings.Join(strings.Fields(detail), " ")
	if r := []rune(s); len(r) > tailDetailLen {
		s = string(r[:tailDetailLen]) + "…"
	}
	return s
}

func renderForceFlags(events []telemetry.Event) {
	// Surfaces "user reached for an override" patterns. We don't log raw
	// flag values, but outcome events can tag force=true.
	count := 0
	for _, e := range events {
		if e.Tags["force"] == "true" {
			count++
		}
	}
	if count == 0 {
		return
	}
	Print("## overrides")
	Printf("  force-flag invocations: %d (signal of friction worth investigating)\n", count)
	Print("")
}

func renderOutcomes(events []telemetry.Event) {
	verifyVerdicts := map[string]int{}
	mutationScores := []float64{}
	shipResults := map[string]int{}
	doctorResults := map[string]int{}
	doctorIssues := 0
	for _, e := range events {
		if e.Kind != "outcome" {
			continue
		}
		switch e.Command {
		case "verify":
			if v := e.Tags["verdict"]; v != "" {
				verifyVerdicts[v]++
			}
			if s, ok := e.Numbers["mutation_score"]; ok {
				mutationScores = append(mutationScores, s)
			}
		case "ship":
			if r := e.Tags["result"]; r != "" {
				shipResults[r]++
			}
		case "doctor":
			if r := e.Tags["result"]; r != "" {
				doctorResults[r]++
			}
			doctorIssues += e.Counts["issues"]
		}
	}
	if len(verifyVerdicts) == 0 && len(shipResults) == 0 && len(doctorResults) == 0 {
		return
	}
	Print("## outcomes")
	if len(verifyVerdicts) > 0 {
		Printf("  verify verdicts: ")
		first := true
		for _, k := range []string{"pass", "partial", "fail"} {
			if v, ok := verifyVerdicts[k]; ok {
				if !first {
					Printf(", ")
				}
				Printf("%s=%d", k, v)
				first = false
			}
		}
		if pending := verifyVerdicts["pending"]; pending > 0 {
			if !first {
				Printf(", ")
			}
			Printf("pending=%d", pending)
		}
		Print("")
		if pending := verifyVerdicts["pending"]; pending > 0 {
			Printf("  (pending verifies have not been finalized — run `dross verify finalize <phase>` after /dross-verify)\n")
		}
		if len(mutationScores) > 0 {
			Printf("  mutation score: avg=%.2f n=%d\n", avg(mutationScores), len(mutationScores))
		}
	}
	if len(shipResults) > 0 {
		Printf("  ship results:    ")
		first := true
		for k, v := range shipResults {
			if !first {
				Printf(", ")
			}
			Printf("%s=%d", k, v)
			first = false
		}
		Print("")
	}
	if len(doctorResults) > 0 {
		Printf("  doctor runs:     ")
		first := true
		for _, k := range []string{"passed", "issues_found"} {
			if v, ok := doctorResults[k]; ok {
				if !first {
					Printf(", ")
				}
				Printf("%s=%d", k, v)
				first = false
			}
		}
		if doctorIssues > 0 {
			Printf("  (cumulative issues across runs: %d)", doctorIssues)
		}
		Print("")
	}
	Print("")
}

func avg(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

func filterSince(events []telemetry.Event, cutoff time.Time) []telemetry.Event {
	out := events[:0:0]
	for _, e := range events {
		if !e.Timestamp.Before(cutoff) {
			out = append(out, e)
		}
	}
	return out
}

func parseSince(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t
	}
	if d, err := time.ParseDuration(s); err == nil {
		return time.Now().Add(-d)
	}
	// Try days-style "7d"
	if len(s) > 1 && s[len(s)-1] == 'd' {
		if d, err := time.ParseDuration(s[:len(s)-1] + "h"); err == nil {
			return time.Now().Add(-d * 24)
		}
	}
	return time.Time{}
}

func humanDuration(d time.Duration) string {
	days := int(d.Hours() / 24)
	if days >= 1 {
		return fmt.Sprintf("%d days", days)
	}
	if d.Hours() >= 1 {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return d.Truncate(time.Minute).String()
}

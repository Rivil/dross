package cmd

import (
	"fmt"
	"path/filepath"
	"sort"
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

func renderErrorBuckets(events []telemetry.Event) {
	by := map[string]int{}
	total := 0
	for _, e := range events {
		if e.ErrorClass == "" {
			continue
		}
		by[e.ErrorClass]++
		total++
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
	Print("")
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
		}
	}
	if len(verifyVerdicts) == 0 && len(shipResults) == 0 {
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
		Print("")
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

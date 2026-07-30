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

// statsSummary is the aggregation both renderings read. The table and the
// --json payload are two views of this one struct, so the payload cannot
// silently drop a section the table still prints — c-5's "the same data its
// default rendering shows", made structural rather than promised.
//
// It carries one field per rendered section: header, commands, errors,
// overrides, outcomes. Where the table truncates (top 10 commands, top 5
// unclassified shapes) the summary keeps the full ordered list and the
// renderers cap — JSON is for machines, and a silent cap there would be a
// second, invisible truncation.
type statsSummary struct {
	Path       string         `json:"path"`
	Events     int            `json:"events"`
	Span       *statsSpan     `json:"span"` // nil when there are no events
	Commands   []statsCommand `json:"commands"`
	Errors     statsErrors    `json:"errors"`
	ForceFlags int            `json:"force_flag_invocations"`
	Outcomes   statsOutcomes  `json:"outcomes"`
}

type statsSpan struct {
	First string `json:"first"`
	Last  string `json:"last"`
	Human string `json:"human"`
}

type statsCommand struct {
	Command  string `json:"command"`
	Calls    int    `json:"calls"`
	Errors   int    `json:"errors"`
	MedianMS int64  `json:"median_ms"`
}

type statsErrorClass struct {
	Class string `json:"class"`
	Count int    `json:"count"`
}

// statsOtherShape is one entry of the graduation queue — an err_detail still
// landing in the `other` bucket. Carried in the payload as well as the table
// so a consumer can see what has earned a named bucket.
type statsOtherShape struct {
	Detail string `json:"detail"`
	Count  int    `json:"count"`
}

type statsErrors struct {
	Total     int               `json:"total"`
	Classes   []statsErrorClass `json:"classes"`
	OtherTail []statsOtherShape `json:"other_tail"`
}

type statsOutcomes struct {
	VerifyVerdicts   map[string]int `json:"verify_verdicts"`
	MutationScoreAvg float64        `json:"mutation_score_avg"`
	MutationScoreN   int            `json:"mutation_score_n"`
	ShipResults      map[string]int `json:"ship_results"`
	DoctorResults    map[string]int `json:"doctor_results"`
	DoctorIssues     int            `json:"doctor_issues"`
}

func statsShow() *cobra.Command {
	var since string
	var asJSON bool
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
			s := summarizeStats(events, path)
			if asJSON {
				// An empty log is an empty-but-valid document, not the
				// "(no telemetry events …)" sentence — a consumer asking for
				// JSON must always get JSON.
				return emitJSON(s)
			}
			if len(events) == 0 {
				Printf("(no telemetry events at %s)\n", path)
				return nil
			}
			renderHeader(s)
			renderTopCommands(s)
			renderErrorBuckets(s)
			renderForceFlags(s)
			renderOutcomes(s)
			return nil
		},
	}
	c.Flags().StringVar(&since, "since", "", "filter to events newer than (e.g. 7d, 24h, 2026-05-01)")
	c.Flags().BoolVar(&asJSON, "json", false, jsonFlagUsage)
	return c
}

// summarizeStats folds the event log into every number the renderings print.
// Slices and maps are always non-nil so the payload carries `[]` / `{}` rather
// than `null` for an empty section.
func summarizeStats(events []telemetry.Event, path string) *statsSummary {
	s := &statsSummary{
		Path:     path,
		Events:   len(events),
		Commands: []statsCommand{},
		Errors:   statsErrors{Classes: []statsErrorClass{}, OtherTail: []statsOtherShape{}},
		Outcomes: statsOutcomes{
			VerifyVerdicts: map[string]int{},
			ShipResults:    map[string]int{},
			DoctorResults:  map[string]int{},
		},
	}
	if len(events) == 0 {
		return s
	}

	first, last := events[0].Timestamp, events[len(events)-1].Timestamp
	s.Span = &statsSpan{
		First: first.Format("2006-01-02"),
		Last:  last.Format("2006-01-02"),
		Human: humanDuration(last.Sub(first)),
	}

	// --- commands ---
	type cmdRow struct {
		cmd    string
		count  int
		errors int
		totMS  int64
	}
	by := map[string]*cmdRow{}
	for _, e := range events {
		if e.Kind != "cli" || e.Command == "" {
			continue
		}
		r, ok := by[e.Command]
		if !ok {
			r = &cmdRow{cmd: e.Command}
			by[e.Command] = r
		}
		r.count++
		r.totMS += e.DurationMS
		if e.ExitCode != 0 {
			r.errors++
		}
	}
	rows := make([]*cmdRow, 0, len(by))
	for _, r := range by {
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].count > rows[j].count })
	for _, r := range rows {
		median := int64(0)
		if r.count > 0 {
			median = r.totMS / int64(r.count) // good enough approximation
		}
		s.Commands = append(s.Commands, statsCommand{Command: r.cmd, Calls: r.count, Errors: r.errors, MedianMS: median})
	}

	// --- errors ---
	classes := map[string]int{}
	tail := map[string]int{}
	for _, e := range events {
		if e.ErrorClass == "" {
			continue
		}
		// Count the effective class, not the stored one: a bucket added after
		// an event was written still applies to it. The log is not rewritten.
		class := telemetry.Reclassify(e)
		classes[class]++
		s.Errors.Total++
		// Whatever is still "other" after re-derivation is genuinely
		// unclassified — that's the graduation queue.
		if class == "other" && e.ErrorDetail != "" {
			tail[e.ErrorDetail]++
		}
	}
	for k, v := range classes {
		s.Errors.Classes = append(s.Errors.Classes, statsErrorClass{Class: k, Count: v})
	}
	sort.Slice(s.Errors.Classes, func(i, j int) bool { return s.Errors.Classes[i].Count > s.Errors.Classes[j].Count })
	for k, v := range tail {
		s.Errors.OtherTail = append(s.Errors.OtherTail, statsOtherShape{Detail: k, Count: v})
	}
	// Count descending, then shape ascending — map iteration order must not
	// leak into the output, or equal-count shapes reorder between runs.
	sort.Slice(s.Errors.OtherTail, func(i, j int) bool {
		if s.Errors.OtherTail[i].Count != s.Errors.OtherTail[j].Count {
			return s.Errors.OtherTail[i].Count > s.Errors.OtherTail[j].Count
		}
		return s.Errors.OtherTail[i].Detail < s.Errors.OtherTail[j].Detail
	})

	// --- overrides ---
	for _, e := range events {
		if e.Tags["force"] == "true" {
			s.ForceFlags++
		}
	}

	// --- outcomes ---
	// latestVerifyByPhase tracks the most recent verify verdict per phase
	// (events arrive timestamp-ordered from Load). "pending" is counted from
	// here — a phase whose pending event was later closed by a resolved event
	// (finalize, or a gate's auto-heal) is NOT an unfinalized verify, so raw
	// pending-event counting would nag forever. Legacy events without a phase
	// id can't be matched and are excluded from the pending count.
	latestVerifyByPhase := map[string]string{}
	mutationScores := []float64{}
	for _, e := range events {
		if e.Kind != "outcome" {
			continue
		}
		switch e.Command {
		case "verify":
			if v := e.Tags["verdict"]; v != "" {
				if v != "pending" {
					s.Outcomes.VerifyVerdicts[v]++
				}
				if e.Phase != "" {
					latestVerifyByPhase[e.RepoHash+"/"+e.Phase] = v
				}
			}
			if score, ok := e.Numbers["mutation_score"]; ok {
				mutationScores = append(mutationScores, score)
			}
		case "ship":
			if r := e.Tags["result"]; r != "" {
				s.Outcomes.ShipResults[r]++
			}
		case "doctor":
			if r := e.Tags["result"]; r != "" {
				s.Outcomes.DoctorResults[r]++
			}
			s.Outcomes.DoctorIssues += e.Counts["issues"]
		}
	}
	for _, v := range latestVerifyByPhase {
		if v == "pending" {
			s.Outcomes.VerifyVerdicts["pending"]++
		}
	}
	s.Outcomes.MutationScoreN = len(mutationScores)
	if len(mutationScores) > 0 {
		s.Outcomes.MutationScoreAvg = avg(mutationScores)
	}
	return s
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

func renderHeader(s *statsSummary) {
	Printf("# dross telemetry — %s\n", s.Path)
	Printf("  events:  %d\n", s.Events)
	Printf("  span:    %s → %s (%s)\n", s.Span.First, s.Span.Last, s.Span.Human)
	Print("")
}

func renderTopCommands(s *statsSummary) {
	rows := s.Commands
	Print("## commands (top 10 by count)")
	Printf("  %-30s %8s %8s %12s\n", "command", "calls", "errors", "median_ms")
	max := 10
	if len(rows) < max {
		max = len(rows)
	}
	for _, r := range rows[:max] {
		Printf("  %-30s %8d %8d %12d\n", r.Command, r.Calls, r.Errors, r.MedianMS)
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

func renderErrorBuckets(s *statsSummary) {
	if s.Errors.Total == 0 {
		Print("## errors")
		Print("  (no failed invocations recorded)")
		Print("")
		return
	}
	Print("## errors (by class)")
	for _, r := range s.Errors.Classes {
		Printf("  %-16s %4d\n", r.Class, r.Count)
	}
	renderOtherTail(s.Errors.OtherTail)
	Print("")
}

// renderOtherTail prints the unclassified `other` shapes as an indented block
// under the errors table — the queue of message shapes that have earned a named
// bucket but not got one yet. Omitted entirely when the tail is empty, so the
// section costs nothing once the bucket is drained.
func renderOtherTail(rows []statsOtherShape) {
	if len(rows) == 0 {
		return
	}
	// Already sorted count-descending then shape-ascending by summarizeStats;
	// the cap is the renderer's, so the payload keeps the whole queue.
	if len(rows) > tailTopN {
		rows = rows[:tailTopN]
	}
	Printf("  unclassified `other` shapes (top %d):\n", len(rows))
	for _, r := range rows {
		Printf("    %4d  %s\n", r.Count, oneLineDetail(r.Detail))
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

func renderForceFlags(s *statsSummary) {
	// Surfaces "user reached for an override" patterns. We don't log raw
	// flag values, but outcome events can tag force=true.
	if s.ForceFlags == 0 {
		return
	}
	Print("## overrides")
	Printf("  force-flag invocations: %d (signal of friction worth investigating)\n", s.ForceFlags)
	Print("")
}

func renderOutcomes(s *statsSummary) {
	verifyVerdicts := s.Outcomes.VerifyVerdicts
	shipResults := s.Outcomes.ShipResults
	doctorResults := s.Outcomes.DoctorResults
	doctorIssues := s.Outcomes.DoctorIssues
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
		if s.Outcomes.MutationScoreN > 0 {
			Printf("  mutation score: avg=%.2f n=%d\n", s.Outcomes.MutationScoreAvg, s.Outcomes.MutationScoreN)
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

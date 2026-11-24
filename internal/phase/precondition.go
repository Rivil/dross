package phase

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// This file is the single rule set for a task's preconditions: the schema
// (Precondition), the one shape check (PreconditionProblems, which ValidatePlan
// folds in), the strict single-precondition parser the CLI feeds, the record of
// keys the schema does not know, and the canonical line consent fingerprints.

// DefaultPreconditionTimeout is how long a precondition may run when its
// timeout is unset.
const DefaultPreconditionTimeout = 60 * time.Second

// Precondition is one [[task.preconditions]] entry: a command that must pass
// before the task may start. With no bound it passes on exit 0; with Max
// and/or Min its stdout must be a single number inside the bounds, each
// inclusive. The bounds are pointers so an absent bound and a bound of 0 stay
// distinct, and float64 so an integer `max = 5` and `max = 5.0` decode alike.
type Precondition struct {
	Run string   `toml:"run" json:"run"`
	Max *float64 `toml:"max,omitempty" json:"max,omitempty"`
	Min *float64 `toml:"min,omitempty" json:"min,omitempty"`
	// Timeout is a Go duration; empty means DefaultPreconditionTimeout.
	Timeout string `toml:"timeout,omitempty" json:"timeout,omitempty"`
	Why     string `toml:"why,omitempty" json:"why,omitempty"`
}

// preconditionKeys is every key the schema knows, in declaration order.
var preconditionKeys = []string{"run", "max", "min", "timeout", "why"}

// EffectiveTimeout is how long the precondition may run: its Timeout parsed,
// or DefaultPreconditionTimeout when unset. A timeout that does not parse, or
// is not positive, is an error — never quietly the default, since a malformed
// limit is a plan defect PreconditionProblems reports.
func (p Precondition) EffectiveTimeout() (time.Duration, error) {
	if p.Timeout == "" {
		return DefaultPreconditionTimeout, nil
	}
	d, err := time.ParseDuration(p.Timeout)
	if err != nil {
		return 0, fmt.Errorf("timeout %q is not a Go duration (e.g. \"90s\", \"5m\")", p.Timeout)
	}
	if d <= 0 {
		return 0, fmt.Errorf("timeout %q is not positive", p.Timeout)
	}
	return d, nil
}

// preconditionDefects returns what is wrong with one precondition's shape, or
// nil. It is the rule set behind both PreconditionProblems and
// ParsePrecondition, so a plan and a CLI flag are judged identically.
func preconditionDefects(p Precondition) []string {
	var defects []string
	run := strings.TrimSpace(p.Run)
	switch {
	case run == "":
		defects = append(defects, "run is empty")
	case strings.HasPrefix(run, "-"):
		// sh -c would read a leading dash as an option, not a command.
		defects = append(defects, fmt.Sprintf("run %q starts with \"-\"", p.Run))
	}
	finite := true
	for _, b := range []struct {
		name string
		v    *float64
	}{{"max", p.Max}, {"min", p.Min}} {
		if b.v != nil && (math.IsNaN(*b.v) || math.IsInf(*b.v, 0)) {
			defects = append(defects, fmt.Sprintf("%s = %s is not a finite number", b.name, formatBound(*b.v)))
			finite = false
		}
	}
	if finite && p.Max != nil && p.Min != nil && *p.Min > *p.Max {
		defects = append(defects, fmt.Sprintf("min %s is above max %s", formatBound(*p.Min), formatBound(*p.Max)))
	}
	if _, err := p.EffectiveTimeout(); err != nil {
		defects = append(defects, err.Error())
	}
	return defects
}

// PreconditionProblems returns one line per defect of every precondition in
// the plan, in plan order, each naming its task and 1-based position as
// `task <id> precondition <n>: <defect>`. A plan whose preconditions are all
// well-formed, or that has none, returns nil.
//
// Unknown keys are not shape defects: they are recorded at load time on
// Plan.UnknownPreconditionKeys and reported by its owners alongside these.
func PreconditionProblems(plan *Plan) []string {
	var out []string
	for _, t := range plan.Task {
		for i, p := range t.Preconditions {
			for _, d := range preconditionDefects(p) {
				out = append(out, fmt.Sprintf("task %s precondition %d: %s", t.ID, i+1, d))
			}
		}
	}
	return out
}

// ParsePrecondition parses one precondition written as a TOML inline table,
// e.g. `{run = "go test ./x", max = 5}`. It is strict: an unknown key, a value
// of the wrong type, anything after the table, or a shape PreconditionProblems
// would report is an error, so nothing malformed reaches a plan by this route.
func ParsePrecondition(s string) (Precondition, error) {
	doc := "p = " + s
	var raw map[string]any
	if _, err := toml.Decode(doc, &raw); err != nil {
		return Precondition{}, fmt.Errorf("precondition %q is not a TOML inline table: %w", s, err)
	}
	table, ok := raw["p"].(map[string]any)
	if !ok || len(raw) != 1 {
		return Precondition{}, fmt.Errorf("precondition %q is not a single TOML inline table", s)
	}
	if unknown := unknownPreconditionKeys(table); len(unknown) > 0 {
		return Precondition{}, fmt.Errorf("precondition %q: unknown key %s (want %s)",
			s, quoteAll(unknown), strings.Join(preconditionKeys, ", "))
	}
	var holder struct {
		P Precondition `toml:"p"`
	}
	if _, err := toml.Decode(doc, &holder); err != nil {
		return Precondition{}, fmt.Errorf("precondition %q: %w", s, err)
	}
	if defects := preconditionDefects(holder.P); len(defects) > 0 {
		return Precondition{}, fmt.Errorf("precondition %q: %s", s, strings.Join(defects, "; "))
	}
	return holder.P, nil
}

// UnknownPreconditionKey is a key inside a task's preconditions that the
// schema does not know — a typo like `mx = 3` that TOML decoding would
// otherwise drop without a word, leaving a bound that is silently not there.
type UnknownPreconditionKey struct {
	Task  string // task id
	Index int    // 0-based position in the task's preconditions
	Key   string
}

// String renders the key in PreconditionProblems' form.
func (u UnknownPreconditionKey) String() string {
	return fmt.Sprintf("task %s precondition %d: unknown key %q (want %s)",
		u.Task, u.Index+1, u.Key, strings.Join(preconditionKeys, ", "))
}

// recordUnknownPreconditionKeys decodes src a second time, keeping each
// task's preconditions as raw tables, and records every key not in the schema
// on plan. The header ([[task.preconditions]]) and inline
// (preconditions = [{…}]) forms decode alike. Keys outside task.preconditions
// are not its concern and are left alone, as LoadPlan always has.
func recordUnknownPreconditionKeys(plan *Plan, src string) error {
	var raw struct {
		Task []struct {
			ID            string           `toml:"id"`
			Preconditions []map[string]any `toml:"preconditions"`
		} `toml:"task"`
	}
	if _, err := toml.Decode(src, &raw); err != nil {
		return err
	}
	for _, t := range raw.Task {
		for i, table := range t.Preconditions {
			for _, k := range unknownPreconditionKeys(table) {
				plan.UnknownPreconditionKeys = append(plan.UnknownPreconditionKeys,
					UnknownPreconditionKey{Task: t.ID, Index: i, Key: k})
			}
		}
	}
	return nil
}

// unknownPreconditionKeys returns table's keys the schema does not know,
// sorted so the report is stable across map iteration.
func unknownPreconditionKeys(table map[string]any) []string {
	var out []string
	for k := range table {
		if !slices.Contains(preconditionKeys, k) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func quoteAll(ss []string) string {
	q := make([]string, len(ss))
	for i, s := range ss {
		q[i] = strconv.Quote(s)
	}
	return strings.Join(q, ", ")
}

// formatBound renders a bound the shortest way that round-trips, so 3 and 3.0
// both read "3".
func formatBound(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// canonicalBound is a bound as the consent line carries it: nil when absent,
// otherwise its shortest round-tripping text (JSON has no NaN or Inf), with
// -0 read as 0.
func canonicalBound(v *float64) any {
	if v == nil {
		return nil
	}
	f := *v
	if f == 0 {
		f = 0 // -0 and 0 are one bound
	}
	return formatBound(f)
}

// PreconditionSetLine is the canonical form of a plan's whole precondition
// set — the line `dross trust --preconditions` fingerprints. It is a JSON
// array of {task, preconditions} for each task carrying any, sorted by task
// id with each task's preconditions in plan order, so what a precondition
// runs, bounds, allows or explains — and which task it gates — is in it,
// while comments, layout, task order, titles and statuses are not. Every key
// is always present, and the timeout is the duration it means, so an unset
// timeout and "60s" are one grant. A plan with no preconditions gives "".
//
// Objects are maps, which encoding/json writes in sorted key order, so the
// line is canonical without a tagged struct for the field ledger to file.
// JSON string escaping keeps it unambiguous: a run of `a","b` cannot read as
// two preconditions `a` and `b`.
func PreconditionSetLine(plan *Plan) string {
	type entry struct {
		task string
		line map[string]any
	}
	var tasks []entry
	for _, t := range plan.Task {
		if len(t.Preconditions) == 0 {
			continue
		}
		var pcs []map[string]any
		for _, p := range t.Preconditions {
			// A malformed timeout is kept verbatim on purpose, not dropped
			// or defaulted: PreconditionProblems reports it and ValidatePlan
			// refuses it, so it never reaches a grant through a save, and
			// keeping the raw text means the line still changes when it
			// does. A valid one is canonicalised ("" and "60s" alike), and
			// no raw invalid text can equal a valid canonical duration.
			timeout := p.Timeout
			if d, err := p.EffectiveTimeout(); err == nil {
				timeout = d.String()
			}
			pcs = append(pcs, map[string]any{
				"run":     p.Run,
				"max":     canonicalBound(p.Max),
				"min":     canonicalBound(p.Min),
				"timeout": timeout,
				"why":     p.Why,
			})
		}
		tasks = append(tasks, entry{t.ID, map[string]any{"task": t.ID, "preconditions": pcs}})
	}
	if len(tasks) == 0 {
		return ""
	}
	sort.SliceStable(tasks, func(i, j int) bool { return tasks[i].task < tasks[j].task })
	lines := make([]map[string]any, len(tasks))
	for i, e := range tasks {
		lines[i] = e.line
	}
	// Every value is a string, nil or a slice or map of them: Marshal cannot
	// fail.
	b, _ := json.Marshal(lines)
	return string(b)
}

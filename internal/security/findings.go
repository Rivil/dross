package security

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/Rivil/dross/internal/pathfence"
)

// Severity is the contextual, exploitability-adjusted severity assigned to a
// finding by the refute/verify panel — not the nominal scariness of the attack
// class (the locked severity_model decision).
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
	SeverityInfo     Severity = "info"
)

// severityRank orders severities most-severe-first (lower rank = more severe),
// so criticals sort to the top of the report and drive wave order.
var severityRank = map[Severity]int{
	SeverityCritical: 0,
	SeverityHigh:     1,
	SeverityMedium:   2,
	SeverityLow:      3,
	SeverityInfo:     4,
}

// Valid reports whether s is one of the known severities.
func (s Severity) Valid() bool { _, ok := severityRank[s]; return ok }

// Rank returns the sort rank (0 = most severe). Unknown severities sort last.
func (s Severity) Rank() int {
	if r, ok := severityRank[s]; ok {
		return r
	}
	return len(severityRank)
}

// Finding is one security finding in the ledger. Every finding carries a stable
// id (so the scaffold writer can cite it per criterion), a contextual severity,
// and the refutation evidence proving it survived the refute panel.
type Finding struct {
	ID         string   `toml:"id"`
	Title      string   `toml:"title"`
	Severity   Severity `toml:"severity"`
	Class      string   `toml:"class"`
	File       string   `toml:"file"`
	Line       int      `toml:"line"`
	Evidence   string   `toml:"evidence"`
	Refutation string   `toml:"refutation"`
}

// Survived reports whether the finding survived adversarial verification: it must
// carry refutation evidence (the panel tried to refute it and failed). A finding
// with no refutation is an unverified candidate, not a survivor.
func (f Finding) Survived() bool { return strings.TrimSpace(f.Refutation) != "" }

// Ledger is the machine-readable findings.toml that the scaffold writer consumes.
type Ledger struct {
	Findings []Finding `toml:"finding"`
}

// Validate checks every finding has a non-empty, unique id and a known severity.
func (l Ledger) Validate() error {
	seen := map[string]bool{}
	for i, f := range l.Findings {
		if strings.TrimSpace(f.ID) == "" {
			return fmt.Errorf("finding %d: empty id", i)
		}
		if seen[f.ID] {
			return fmt.Errorf("finding %q: duplicate id", f.ID)
		}
		seen[f.ID] = true
		if !f.Severity.Valid() {
			return fmt.Errorf("finding %q: invalid severity %q", f.ID, f.Severity)
		}
	}
	return nil
}

// Survivors returns the findings that survived the refute panel, in severity
// order (criticals first), preserving input order within a severity.
func (l Ledger) Survivors() []Finding {
	var out []Finding
	for _, f := range l.Findings {
		if f.Survived() {
			out = append(out, f)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Severity.Rank() < out[j].Severity.Rank()
	})
	return out
}

// Save writes the ledger as TOML to the contained path.
//
// It takes a pathfence.Contained rather than a string because this is where the
// path meets the filesystem: the type has no constructor outside pathfence, so a
// caller that skipped the containment check has no value to pass and fails to
// build. The write goes through the pathfence seam for the same reason — the
// former os.Create here took a plain string, with nothing but convention tying
// it to a check that ran two frames up.
//
// Encoding into a buffer first is what lets the seam do the write: the seam
// takes bytes, and a mid-encode failure now leaves the target untouched rather
// than truncated.
func Save(c pathfence.Contained, l Ledger) error {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(l); err != nil {
		return err
	}
	return pathfence.WriteFile(c, buf.Bytes(), 0o644)
}

// Load reads and parses a findings.toml ledger. It returns an error (never
// panics) on missing or garbled input, so a malformed ledger fails the run
// cleanly rather than crashing the scaffold writer downstream.
func Load(c pathfence.Contained) (Ledger, error) {
	b, err := pathfence.ReadFile(c)
	if err != nil {
		return Ledger{}, fmt.Errorf("load findings ledger %s: %w", c, err)
	}
	var l Ledger
	if err := toml.Unmarshal(b, &l); err != nil {
		return Ledger{}, fmt.Errorf("load findings ledger %s: %w", c, err)
	}
	return l, nil
}

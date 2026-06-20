package quality

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Risk is the contextual maintainability-risk assigned to a finding by the
// refute/verify panel — panel-judged change-cost / bug-likelihood weighted by
// blast radius (how central or hot the affected code is), NOT the nominal
// category of the issue (the locked ranking_model decision). A high-complexity
// function on a cold path is Low; the same on a core path is High.
type Risk string

const (
	RiskCritical Risk = "critical"
	RiskHigh     Risk = "high"
	RiskMedium   Risk = "medium"
	RiskLow      Risk = "low"
	RiskInfo     Risk = "info"
)

// riskRank orders risks highest-first (lower rank = higher risk), so the most
// damaging debt sorts to the top of the report and drives wave order.
var riskRank = map[Risk]int{
	RiskCritical: 0,
	RiskHigh:     1,
	RiskMedium:   2,
	RiskLow:      3,
	RiskInfo:     4,
}

// Valid reports whether r is one of the known risk levels.
func (r Risk) Valid() bool { _, ok := riskRank[r]; return ok }

// Rank returns the sort rank (0 = highest risk). Unknown risks sort last.
func (r Risk) Rank() int {
	if n, ok := riskRank[r]; ok {
		return n
	}
	return len(riskRank)
}

// Finding is one quality finding in the ledger. Every finding carries a stable
// id (so the scaffold writer can cite it per criterion), a contextual Risk, the
// substantive Dimension it belongs to, and the refutation evidence proving it
// survived the refute panel.
type Finding struct {
	ID         string    `toml:"id"`
	Title      string    `toml:"title"`
	Risk       Risk      `toml:"risk"`
	Dimension  Dimension `toml:"dimension"`
	File       string    `toml:"file"`
	Line       int       `toml:"line"`
	Evidence   string    `toml:"evidence"`
	Refutation string    `toml:"refutation"`
}

// Survived reports whether the finding survived adversarial verification: it must
// carry refutation evidence (the panel tried to refute it and failed). A finding
// with no refutation is an unverified candidate, not a survivor.
func (f Finding) Survived() bool { return strings.TrimSpace(f.Refutation) != "" }

// Ledger is the machine-readable findings.toml that the scaffold writer consumes.
type Ledger struct {
	Findings []Finding `toml:"finding"`
}

// Validate checks every finding has a non-empty, unique id and a known risk.
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
		if !f.Risk.Valid() {
			return fmt.Errorf("finding %q: invalid risk %q", f.ID, f.Risk)
		}
	}
	return nil
}

// Survivors returns the findings that survived the refute panel, in risk order
// (highest risk first), preserving input order within a risk level. Unknown
// risks sort last.
func (l Ledger) Survivors() []Finding {
	var out []Finding
	for _, f := range l.Findings {
		if f.Survived() {
			out = append(out, f)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Risk.Rank() < out[j].Risk.Rank()
	})
	return out
}

// Save writes the ledger as TOML to path.
func Save(path string, l Ledger) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(l)
}

// Load reads and parses a findings.toml ledger. It returns an error (never
// panics) on missing or garbled input, so a malformed ledger fails the run
// cleanly rather than crashing the scaffold writer downstream.
func Load(path string) (Ledger, error) {
	var l Ledger
	if _, err := toml.DecodeFile(path, &l); err != nil {
		return Ledger{}, fmt.Errorf("load findings ledger %s: %w", path, err)
	}
	return l, nil
}

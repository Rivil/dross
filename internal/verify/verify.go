// Package verify aggregates per-language mutation reports into
// .dross/phases/<id>/tests.json and writes a verdict skeleton to
// verify.toml. The criterion-to-test mapping (the actual judgement
// of "does this test cover this acceptance criterion?") is filled
// in by the LLM via /dross-verify; the Go side handles the
// mechanical aggregation only.
package verify

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/Rivil/dross/internal/mutation"
)

const (
	TestsFile  = "tests.json"
	VerifyFile = "verify.toml"
)

// Tests is the machine-written aggregation of mutation/coverage results.
type Tests struct {
	Phase       string         `json:"phase"`
	GeneratedAt time.Time      `json:"generated_at"`
	Languages   []LanguageRun  `json:"languages"`
	Skipped     []SkippedFile  `json:"skipped,omitempty"`
}

type LanguageRun struct {
	Name     string           `json:"name"` // "typescript" | "go" | ...
	Tool     string           `json:"tool"` // "stryker" | "gremlins" | ...
	Files    []string         `json:"files"`
	Mutation *mutation.Report `json:"mutation,omitempty"`
}

type SkippedFile struct {
	File   string `json:"file"`
	Reason string `json:"reason"` // why no adapter ran on it
}

// Verify is the human-readable + LLM-mappable verdict.
type Verify struct {
	Verify   VerifyMeta        `toml:"verify"`
	Summary  VerifySummary     `toml:"summary"`
	Criteria []CriterionResult `toml:"criterion,omitempty"`
	Findings []Finding         `toml:"finding,omitempty"`
}

type VerifyMeta struct {
	Phase       string    `toml:"phase"`
	GeneratedAt time.Time `toml:"generated_at"`
	Verdict     string    `toml:"verdict"` // pass | fail | partial | pending
}

type VerifySummary struct {
	MutationScore     float64 `toml:"mutation_score"`
	MutantsKilled     int     `toml:"mutants_killed"`
	MutantsSurvived   int     `toml:"mutants_survived"`
	CriteriaTotal     int     `toml:"criteria_total"`
	CriteriaCovered   int     `toml:"criteria_covered"`
	CriteriaUncovered int     `toml:"criteria_uncovered"`
}

type CriterionResult struct {
	ID     string   `toml:"id"`
	Status string   `toml:"status"` // covered | weak | uncovered | unknown
	Tests  []string `toml:"tests,omitempty"`
	Notes  string   `toml:"notes,omitempty"`
}

type Finding struct {
	Severity string `toml:"severity"` // BLOCKING | FLAG | NOTE
	Text     string `toml:"text"`
}

// FilePaths returns canonical paths for tests.json and verify.toml.
func FilePaths(root, phaseID string) (tests, verify string) {
	dir := filepath.Join(root, "phases", phaseID)
	return filepath.Join(dir, TestsFile), filepath.Join(dir, VerifyFile)
}

// Run executes the configured adapters against the given files, grouped
// by language, and returns the aggregated Tests struct. It does NOT
// write to disk — caller decides when/where to persist.
//
// adapters is a list of mutation.Adapter implementations; the first
// matching adapter (by Supports()) is used for each file. Files with
// no matching adapter end up in Skipped.
func Run(phaseID string, files []string, adapters []mutation.Adapter) (*Tests, error) {
	t := &Tests{
		Phase:       phaseID,
		GeneratedAt: time.Now().UTC(),
	}

	// Group files by adapter.
	byAdapter := map[string][]string{}
	adapterByName := map[string]mutation.Adapter{}
	for _, f := range files {
		a := mutation.Dispatch(f, adapters)
		if a == nil {
			t.Skipped = append(t.Skipped, SkippedFile{
				File:   f,
				Reason: "no mutation adapter for " + filepath.Ext(f),
			})
			continue
		}
		byAdapter[a.Name()] = append(byAdapter[a.Name()], f)
		adapterByName[a.Name()] = a
	}

	// Stable order for output.
	names := make([]string, 0, len(byAdapter))
	for n := range byAdapter {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		a := adapterByName[name]
		report, err := a.Run(byAdapter[name])
		if err != nil {
			return nil, fmt.Errorf("%s adapter: %w", name, err)
		}
		t.Languages = append(t.Languages, LanguageRun{
			Name:     adapterLanguage(name),
			Tool:     name,
			Files:    byAdapter[name],
			Mutation: report,
		})
	}

	return t, nil
}

// adapterLanguage maps adapter names to the language label users see.
func adapterLanguage(name string) string {
	switch name {
	case "stryker":
		return "typescript"
	case "stryker.net":
		return "csharp"
	case "gremlins":
		return "go"
	}
	return name
}

// Save writes Tests as JSON, creating parent dir if needed.
func (t *Tests) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// LoadTests reads tests.json. Missing file = nil, no error (verify
// hasn't run yet).
func LoadTests(path string) (*Tests, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var t Tests
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// Save writes verify.toml.
func (v *Verify) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := toml.NewEncoder(f)
	enc.Indent = "  "
	return enc.Encode(v)
}

func LoadVerify(path string) (*Verify, error) {
	var v Verify
	_, err := toml.DecodeFile(path, &v)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// Skeleton builds a verify.toml seeded from machine results, leaving
// the criterion-to-test mapping for the LLM to fill in. Verdict is
// "pending" until the LLM marks it.
func Skeleton(t *Tests, criteriaIDs []string) *Verify {
	v := &Verify{
		Verify: VerifyMeta{
			Phase:       t.Phase,
			GeneratedAt: t.GeneratedAt,
			Verdict:     "pending",
		},
		Summary: VerifySummary{
			CriteriaTotal: len(criteriaIDs),
		},
	}
	for _, lr := range t.Languages {
		if lr.Mutation == nil {
			continue
		}
		v.Summary.MutationScore = combineScore(v.Summary.MutationScore, lr.Mutation.Score)
		v.Summary.MutantsKilled += lr.Mutation.Killed
		v.Summary.MutantsSurvived += lr.Mutation.Survived
	}
	for _, id := range criteriaIDs {
		v.Criteria = append(v.Criteria, CriterionResult{
			ID:     id,
			Status: "unknown", // LLM fills this in
		})
	}
	for _, lr := range t.Languages {
		if lr.Mutation == nil {
			continue
		}
		for _, m := range lr.Mutation.Surviving {
			v.Findings = append(v.Findings, Finding{
				Severity: "FLAG",
				Text: fmt.Sprintf("%s mutant survived: %s:%d (%s)",
					lr.Tool, m.File, m.Line, m.Op),
			})
		}
	}
	for _, s := range t.Skipped {
		v.Findings = append(v.Findings, Finding{
			Severity: "NOTE",
			Text:     fmt.Sprintf("skipped %s: %s", s.File, s.Reason),
		})
	}
	return v
}

// combineScore averages mutation scores across multiple language
// runs in the simplest way: the mean. If we ever have weighting
// requirements (e.g. weight by mutant count) this is the place.
func combineScore(existing, next float64) float64 {
	if existing == 0 {
		return next
	}
	if next == 0 {
		return existing
	}
	return (existing + next) / 2
}

// FilesFromChanges flattens changes.json's per-task file lists into
// a deduped, sorted slice. Used by `dross verify` to know what to
// mutation-test.
func FilesFromChanges(filesByTask map[string][]string) []string {
	seen := map[string]bool{}
	for _, fs := range filesByTask {
		for _, f := range fs {
			seen[f] = true
		}
	}
	out := make([]string, 0, len(seen))
	for f := range seen {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// SplitFiles separates files Stryker handles from "other" files
// (HTML/CSS/etc). Useful for the prompt to surface what was
// mutation-tested vs what was just snapshot-checked.
func SplitFiles(files []string) (mutable, snapshot []string) {
	for _, f := range files {
		ext := strings.ToLower(filepath.Ext(f))
		switch ext {
		case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".svelte",
			".go", ".cs":
			mutable = append(mutable, f)
		default:
			snapshot = append(snapshot, f)
		}
	}
	return
}

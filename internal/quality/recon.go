package quality

import "github.com/Rivil/dross/internal/stack"

// DetectLanguages returns the sorted, de-duplicated set of languages under root.
// It delegates to internal/stack so the ext->lang mapping lives in exactly one
// place — security and quality recon share the same canonical detector and can
// never drift.
func DetectLanguages(root string) ([]string, error) {
	return stack.DetectLanguages(root)
}

// Manifest is the tool-coverage record for a run: which analyzers ran (installed)
// and which were skipped (missing). Recording both is the whole point — a thin
// toolbelt must never read as a clean "all clear".
type Manifest struct {
	Languages []string
	Tools     []ToolStatus
}

// Ran returns the analyzers that are installed and will run.
func (m Manifest) Ran() []ToolStatus {
	var out []ToolStatus
	for _, t := range m.Tools {
		if t.Installed {
			out = append(out, t)
		}
	}
	return out
}

// Skipped returns the analyzers that are missing (each keeping its install hint).
func (m Manifest) Skipped() []ToolStatus {
	var out []ToolStatus
	for _, t := range m.Tools {
		if !t.Installed {
			out = append(out, t)
		}
	}
	return out
}

// BuildManifest detects languages under root, resolves the applicable analyzers
// (the agnostic set always, plus any dedicated to a detected language), and
// records each one's availability via lookPath. Both ran and skipped tools are
// recorded — the manifest is the coverage signal.
func BuildManifest(root string, lookPath func(string) (string, error)) (Manifest, error) {
	langs, err := DetectLanguages(root)
	if err != nil {
		return Manifest{}, err
	}
	seen := map[string]bool{}
	var analyzers []Analyzer
	add := func(list []Analyzer) {
		for _, a := range list {
			if !seen[a.Name] {
				seen[a.Name] = true
				analyzers = append(analyzers, a)
			}
		}
	}
	add(AnalyzersFor("")) // agnostic tools run regardless of detected language
	for _, lang := range langs {
		add(AnalyzersFor(lang))
	}
	// Marker-file stacks (detected by filename pattern, not source extension) are
	// unioned in additively: their dedicated analyzers run on top of the language
	// set, deduped by name, with Languages left unchanged. Mirrors the security
	// recon so the manifest path honours marker stacks consistently.
	profiles, _ := stack.LoadAll()
	for _, id := range stack.MarkerProfiles(root, profiles) {
		add(profileAnalyzers(id))
	}
	return Manifest{Languages: langs, Tools: Detect(analyzers, lookPath)}, nil
}

package security

import "github.com/Rivil/dross/internal/stack"

// DetectLanguages returns the sorted, de-duplicated set of languages under root.
// It delegates to internal/stack so the ext->lang mapping lives in exactly one
// place — security and quality recon share the same canonical detector and can
// never drift.
func DetectLanguages(root string) ([]string, error) {
	return stack.DetectLanguages(root)
}

// Manifest is the tool-coverage record for a run: which scanners ran (installed)
// and which were skipped (missing). Recording both is the whole point — a thin
// toolbelt must never read as a clean "all clear".
type Manifest struct {
	Languages []string
	Tools     []ToolStatus
}

// Ran returns the scanners that are installed and will run.
func (m Manifest) Ran() []ToolStatus {
	var out []ToolStatus
	for _, t := range m.Tools {
		if t.Installed {
			out = append(out, t)
		}
	}
	return out
}

// Skipped returns the scanners that are missing (each keeping its install hint).
func (m Manifest) Skipped() []ToolStatus {
	var out []ToolStatus
	for _, t := range m.Tools {
		if !t.Installed {
			out = append(out, t)
		}
	}
	return out
}

// BuildManifest detects languages under root, resolves the applicable scanners
// (the agnostic set always, plus any dedicated to a detected language), and
// records each one's availability via lookPath. Both ran and skipped tools are
// recorded — the manifest is the coverage signal.
func BuildManifest(root string, lookPath func(string) (string, error)) (Manifest, error) {
	langs, err := DetectLanguages(root)
	if err != nil {
		return Manifest{}, err
	}
	seen := map[string]bool{}
	var scanners []Scanner
	add := func(list []Scanner) {
		for _, s := range list {
			if !seen[s.Name] {
				seen[s.Name] = true
				scanners = append(scanners, s)
			}
		}
	}
	add(ScannersFor("")) // agnostic tools run regardless of detected language
	for _, lang := range langs {
		add(ScannersFor(lang))
	}
	return Manifest{Languages: langs, Tools: Detect(scanners, lookPath)}, nil
}

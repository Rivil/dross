package quality

import (
	"io/fs"
	"path/filepath"
	"sort"
)

// extLang maps source-file extensions to languages. Unknown extensions are
// simply ignored — recon never crashes on an extension it doesn't recognise.
var extLang = map[string]string{
	".go":    "go",
	".py":    "python",
	".js":    "javascript",
	".jsx":   "javascript",
	".ts":    "typescript",
	".tsx":   "typescript",
	".rb":    "ruby",
	".rs":    "rust",
	".java":  "java",
	".kt":    "kotlin",
	".c":     "c",
	".h":     "c",
	".cc":    "cpp",
	".cpp":   "cpp",
	".cs":    "csharp",
	".php":   "php",
	".swift": "swift",
}

// skipDirs are never descended into during recon. The tool sweep stays code-only
// — .dross is excluded so the sweep reads no planning artifacts (the locked
// context_model keeps the sweep code-only; calibrate-only context lives in the
// prompt half). The rest are build/vendor noise that would only pollute language
// detection.
var skipDirs = map[string]bool{
	".dross":       true,
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	"dist":         true,
	".idea":        true,
	".vscode":      true,
}

// DetectLanguages walks the tree at root and returns the sorted, de-duplicated set
// of languages found, mapped from file extensions. It never descends into .dross/
// (keeping the tool sweep code-only) or other noise dirs, and silently ignores
// files with unknown extensions.
func DetectLanguages(root string) ([]string, error) {
	set := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if lang, ok := extLang[filepath.Ext(d.Name())]; ok {
			set[lang] = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(set))
	for l := range set {
		out = append(out, l)
	}
	sort.Strings(out)
	return out, nil
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
	return Manifest{Languages: langs, Tools: Detect(analyzers, lookPath)}, nil
}

package security

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

// skipDirs are never descended into during recon. .dross is excluded so the audit
// stays context-free — it reads no planning artifacts; the rest are build/vendor
// noise that would only pollute language detection.
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
// (keeping the audit context-free) or other noise dirs, and silently ignores
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

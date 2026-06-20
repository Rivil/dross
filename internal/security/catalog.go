package security

import (
	"os/exec"
	"runtime"

	"github.com/Rivil/dross/internal/stack"
)

// Scanner is one security tool dross knows how to run. The catalog is data-driven
// so adding a tool (or a whole language) is an edit to this table, not a code
// change elsewhere — honoring the locked catalog_scope decision (Go-complete now,
// other languages extend the same table later).
type Scanner struct {
	// Name is the human-facing scanner name (e.g. "govulncheck").
	Name string
	// Bin is the executable looked up on PATH to detect availability (usually == Name).
	Bin string
	// Languages lists the languages this scanner is dedicated to. Empty means
	// agnostic — it applies to every codebase regardless of language.
	Languages []string
	// Install is the instruction shown when the scanner is missing.
	Install string
	// Core marks scanners whose absence warrants a prominent warning, so a thin
	// toolbelt never reads as a clean "all clear".
	Core bool
}

// Agnostic reports whether the scanner applies to every codebase (no dedicated
// language).
func (s Scanner) Agnostic() bool { return len(s.Languages) == 0 }

// AppliesTo reports whether the scanner runs for the given language — true for
// agnostic scanners and for any scanner that lists lang.
func (s Scanner) AppliesTo(lang string) bool {
	if s.Agnostic() {
		return true
	}
	for _, l := range s.Languages {
		if l == lang {
			return true
		}
	}
	return false
}

// agnosticCatalog holds the scanners that apply to every codebase regardless of
// language (secret/SAST/dependency tools). Language-dedicated scanners are NOT
// here — they live in the stack profile for that language (internal/stack), so
// `dross stack`'s go.toml is the single source for which Go scanners run and
// adding a language's scanners is a profile drop-in, not a code edit (c-3).
var agnosticCatalog = []Scanner{
	{Name: "gitleaks", Bin: "gitleaks", Core: true,
		Install: "brew install gitleaks  (or see github.com/gitleaks/gitleaks)"},
	{Name: "semgrep", Bin: "semgrep", Core: true,
		Install: "pipx install semgrep  (or brew install semgrep)"},
	{Name: "trivy", Bin: "trivy", Core: true,
		Install: "brew install trivy  (or see github.com/aquasecurity/trivy)"},
}

// toScanner converts a profile tool into a language-dedicated Scanner.
func toScanner(t stack.Tool, lang string) Scanner {
	return Scanner{
		Name:      t.Name,
		Bin:       t.EffectiveBin(runtime.GOOS),
		Languages: []string{lang},
		Install:   t.Install,
		Core:      t.Core,
	}
}

// profileScanners returns the dedicated scanners declared in the stack profile
// whose id == lang. A missing profile (unknown/stub language) yields none, so the
// caller falls back to the agnostic set.
func profileScanners(lang string) []Scanner {
	if lang == "" {
		return nil
	}
	profiles, _ := stack.LoadAll() // merged set still includes embedded on user error
	p := stack.ByID(profiles, lang)
	if p == nil {
		return nil
	}
	var out []Scanner
	for _, tool := range p.Tools {
		if tool.Kind == "scanner" {
			out = append(out, toScanner(tool, lang))
		}
	}
	return out
}

// Catalog returns the full scanner table: the agnostic set plus every
// language-dedicated scanner contributed by a shipped stack profile.
func Catalog() []Scanner {
	out := append([]Scanner{}, agnosticCatalog...)
	profiles, _ := stack.LoadAll()
	for _, p := range profiles {
		for _, tool := range p.Tools {
			if tool.Kind == "scanner" {
				out = append(out, toScanner(tool, p.ID))
			}
		}
	}
	return out
}

// ScannersFor returns the scanners that apply to lang: every agnostic scanner
// plus any dedicated to that language by its profile. An unknown / stub language
// still gets the agnostic set, so a run is never left with zero applicable
// scanners.
func ScannersFor(lang string) []Scanner {
	out := append([]Scanner{}, agnosticCatalog...)
	return append(out, profileScanners(lang)...)
}

// ToolStatus pairs a scanner with whether it was found on PATH.
type ToolStatus struct {
	Scanner
	Installed bool
}

// Detect partitions scanners into installed vs missing using lookPath (pass
// LookPath in production; inject a fake in tests). Missing entries keep their
// Install hint so callers can tell the user how to get them.
func Detect(scanners []Scanner, lookPath func(string) (string, error)) []ToolStatus {
	out := make([]ToolStatus, 0, len(scanners))
	for _, s := range scanners {
		_, err := lookPath(s.Bin)
		out = append(out, ToolStatus{Scanner: s, Installed: err == nil})
	}
	return out
}

// LookPath is the default PATH lookup used by the CLI layer.
func LookPath(bin string) (string, error) { return exec.LookPath(bin) }

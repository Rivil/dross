package security

import "os/exec"

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

// catalog is the master scanner table. Go is complete; the agnostic tools
// (gitleaks, semgrep, trivy) apply everywhere. Other languages get the agnostic
// set until a dedicated catalog is added for them.
var catalog = []Scanner{
	{Name: "govulncheck", Bin: "govulncheck", Languages: []string{"go"}, Core: true,
		Install: "go install golang.org/x/vuln/cmd/govulncheck@latest"},
	{Name: "gosec", Bin: "gosec", Languages: []string{"go"}, Core: true,
		Install: "go install github.com/securego/gosec/v2/cmd/gosec@latest"},
	{Name: "staticcheck", Bin: "staticcheck", Languages: []string{"go"},
		Install: "go install honnef.co/go/tools/cmd/staticcheck@latest"},
	{Name: "osv-scanner", Bin: "osv-scanner", Languages: []string{"go"},
		Install: "go install github.com/google/osv-scanner/cmd/osv-scanner@latest"},
	{Name: "gitleaks", Bin: "gitleaks", Core: true,
		Install: "brew install gitleaks  (or see github.com/gitleaks/gitleaks)"},
	{Name: "semgrep", Bin: "semgrep", Core: true,
		Install: "pipx install semgrep  (or brew install semgrep)"},
	{Name: "trivy", Bin: "trivy", Core: true,
		Install: "brew install trivy  (or see github.com/aquasecurity/trivy)"},
}

// Catalog returns a copy of the full scanner table.
func Catalog() []Scanner {
	out := make([]Scanner, len(catalog))
	copy(out, catalog)
	return out
}

// ScannersFor returns the scanners that apply to lang: every agnostic scanner
// plus any dedicated to that language. An unknown / stub language still gets the
// agnostic set, so a run is never left with zero applicable scanners.
func ScannersFor(lang string) []Scanner {
	var out []Scanner
	for _, s := range catalog {
		if s.AppliesTo(lang) {
			out = append(out, s)
		}
	}
	return out
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

package quality

import "os/exec"

// Dimension is a substantive maintainability axis a quality finding can belong
// to. The catalog maps each analyzer to the dimension it measures; the set is
// deliberately substantive-only — cosmetic/format/naming axes are excluded per
// the locked quality_scope decision (the language's own formatter + basic vet
// already own those, and a nit-flood drowns real debt in the remediation scaffold).
type Dimension string

const (
	// Complexity — cyclomatic / cognitive complexity hot spots.
	Complexity Dimension = "complexity"
	// Duplication — copy-pasted / structurally cloned code.
	Duplication Dimension = "duplication"
	// DeadCode — unreachable / unused symbols.
	DeadCode Dimension = "dead-code"
	// Coupling — excessive coupling / poor cohesion between units.
	Coupling Dimension = "coupling"
	// TestGap — untested or weakly-tested behaviour.
	TestGap Dimension = "test-gap"
	// ErrorHandling — unhandled errors, ineffectual assignments, resource leaks
	// (the risky-lint categories that signal real defects, per quality_scope).
	ErrorHandling Dimension = "error-handling"
)

// substantiveDimensions is the allowlist of dimensions the catalog may carry. A
// cosmetic/naming dimension is intentionally absent, so an analyzer tagged with
// one fails TestCatalogExcludesCosmetic.
var substantiveDimensions = map[Dimension]bool{
	Complexity: true, Duplication: true, DeadCode: true,
	Coupling: true, TestGap: true, ErrorHandling: true,
}

// IsSubstantive reports whether d is an allowed substantive dimension.
func IsSubstantive(d Dimension) bool { return substantiveDimensions[d] }

// cosmeticBins are tools that only enforce format/naming/style — the layer
// quality_scope excludes because the language formatter + basic vet already own
// it. Adding any of these to the catalog fails TestCatalogExcludesCosmetic even
// if mislabelled with a substantive dimension.
var cosmeticBins = map[string]bool{
	"gofmt": true, "goimports": true, "golint": true, "gofumpt": true, "stylecheck": true,
}

// Analyzer is one quality tool dross knows how to run. The catalog is data-driven
// so adding a tool (or a whole language) is an edit to this table, not a code
// change elsewhere — honoring the locked catalog_scope decision (Go-complete now,
// other languages extend the same table later).
type Analyzer struct {
	// Name is the human-facing analyzer name (e.g. "gocyclo").
	Name string
	// Bin is the executable looked up on PATH to detect availability (usually == Name).
	Bin string
	// Dimension is the substantive maintainability axis this analyzer measures.
	Dimension Dimension
	// Languages lists the languages this analyzer is dedicated to. Empty means
	// agnostic — it applies to every codebase regardless of language.
	Languages []string
	// Install is the instruction shown when the analyzer is missing.
	Install string
	// Core marks analyzers whose absence warrants a prominent warning, so a thin
	// toolbelt never reads as a clean "all clear".
	Core bool
}

// Agnostic reports whether the analyzer applies to every codebase (no dedicated
// language).
func (a Analyzer) Agnostic() bool { return len(a.Languages) == 0 }

// AppliesTo reports whether the analyzer runs for the given language — true for
// agnostic analyzers and for any analyzer that lists lang.
func (a Analyzer) AppliesTo(lang string) bool {
	if a.Agnostic() {
		return true
	}
	for _, l := range a.Languages {
		if l == lang {
			return true
		}
	}
	return false
}

// catalog is the master analyzer table. Go is complete; the agnostic tools (scc,
// jscpd) apply everywhere. Other languages get the agnostic set until a dedicated
// catalog is added for them.
var catalog = []Analyzer{
	{Name: "gocyclo", Bin: "gocyclo", Dimension: Complexity, Languages: []string{"go"}, Core: true,
		Install: "go install github.com/fzipp/gocyclo/cmd/gocyclo@latest"},
	{Name: "dupl", Bin: "dupl", Dimension: Duplication, Languages: []string{"go"}, Core: true,
		Install: "go install github.com/mibk/dupl@latest"},
	{Name: "deadcode", Bin: "deadcode", Dimension: DeadCode, Languages: []string{"go"}, Core: true,
		Install: "go install golang.org/x/tools/cmd/deadcode@latest"},
	{Name: "errcheck", Bin: "errcheck", Dimension: ErrorHandling, Languages: []string{"go"}, Core: true,
		Install: "go install github.com/kisielk/errcheck@latest"},
	{Name: "ineffassign", Bin: "ineffassign", Dimension: ErrorHandling, Languages: []string{"go"}, Core: true,
		Install: "go install github.com/gordonklaus/ineffassign@latest"},
	{Name: "scc", Bin: "scc", Dimension: Complexity, Core: true,
		Install: "brew install scc  (or go install github.com/boyter/scc/v3@latest)"},
	{Name: "jscpd", Bin: "jscpd", Dimension: Duplication, Core: true,
		Install: "npm install -g jscpd  (or see github.com/kucherenko/jscpd)"},
}

// Catalog returns a copy of the full analyzer table.
func Catalog() []Analyzer {
	out := make([]Analyzer, len(catalog))
	copy(out, catalog)
	return out
}

// AnalyzersFor returns the analyzers that apply to lang: every agnostic analyzer
// plus any dedicated to that language. An unknown / stub language still gets the
// agnostic set, so a run is never left with zero applicable analyzers.
func AnalyzersFor(lang string) []Analyzer {
	var out []Analyzer
	for _, a := range catalog {
		if a.AppliesTo(lang) {
			out = append(out, a)
		}
	}
	return out
}

// ToolStatus pairs an analyzer with whether it was found on PATH.
type ToolStatus struct {
	Analyzer
	Installed bool
}

// Detect partitions analyzers into installed vs missing using lookPath (pass
// LookPath in production; inject a fake in tests). Missing entries keep their
// Install hint so callers can tell the user how to get them.
func Detect(analyzers []Analyzer, lookPath func(string) (string, error)) []ToolStatus {
	out := make([]ToolStatus, 0, len(analyzers))
	for _, a := range analyzers {
		_, err := lookPath(a.Bin)
		out = append(out, ToolStatus{Analyzer: a, Installed: err == nil})
	}
	return out
}

// LookPath is the default PATH lookup used by the CLI layer.
func LookPath(bin string) (string, error) { return exec.LookPath(bin) }

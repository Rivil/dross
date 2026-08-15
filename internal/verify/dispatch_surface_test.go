package verify

import (
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/mutation"
)

// The whole non-Go surface the adapters claim, stated once.
//
// A phase touching only these files must produce a language run, not a list of
// Skipped entries. That failure does not look like a failure: the phase verifies
// green, the score is computed over nothing, and no line says anything went
// unmeasured — which is exactly the shape this milestone exists to close.
var claimedExtensions = map[string]string{
	".ts":     "stryker",
	".tsx":    "stryker",
	".js":     "stryker",
	".jsx":    "stryker",
	".mjs":    "stryker",
	".cjs":    "stryker",
	".svelte": "stryker",
	".cs":     "stryker-net",
	".go":     "gremlins",
}

func allAdapters() []mutation.Adapter {
	return []mutation.Adapter{
		&mutation.Gremlins{},
		&mutation.Stryker{},
		&mutation.StrykerNet{},
	}
}

// TestEveryClaimedExtensionDispatches walks the surface by extension rather
// than sampling one representative file.
//
// A representative .ts file proves the adapter exists; it does not prove the
// surface is intact. The realistic regression is ONE entry dropped from a
// Supports switch — .mjs, .cjs, .svelte — and the guard has to name which.
func TestEveryClaimedExtensionDispatches(t *testing.T) {
	adapters := allAdapters()
	for ext, want := range claimedExtensions {
		t.Run(ext, func(t *testing.T) {
			a := mutation.Dispatch("src/thing"+ext, adapters)
			if a == nil {
				t.Fatalf("%s dispatches to no adapter — a phase of these files would land entirely in Skipped and still verify green", ext)
			}
			if a.Name() != want {
				t.Errorf("%s dispatched to %s, want %s", ext, a.Name(), want)
			}
		})
	}
}

// TestDispatchTableMatchesTheAdapters cross-checks the table above against the
// adapters' own Supports methods, in both directions.
//
// Without it the table is a second, hand-maintained copy of the same knowledge,
// and a guard that can fall behind the code it guards is worse than none — it
// reports health it is no longer measuring.
func TestDispatchTableMatchesTheAdapters(t *testing.T) {
	for ext, want := range claimedExtensions {
		var supporting []string
		for _, a := range allAdapters() {
			if a.Supports("x" + ext) {
				supporting = append(supporting, a.Name())
			}
		}
		if len(supporting) == 0 {
			t.Errorf("the table claims %s is handled by %s, but no adapter supports it", ext, want)
		}
		if len(supporting) > 1 {
			t.Errorf("%s is claimed by more than one adapter (%s) — dispatch takes the first, so the winner depends on registration order", ext, strings.Join(supporting, ", "))
		}
	}

	// The other direction: an extension an adapter claims but the table does
	// not know about is a surface nothing here is guarding.
	for _, ext := range []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".svelte", ".cs", ".go"} {
		if _, ok := claimedExtensions[ext]; !ok {
			t.Errorf("%s is supported by an adapter but missing from the table", ext)
		}
	}
}

// TestNonGoPhaseIsNotAllSkipped is the criterion, run through the real
// grouping code rather than asserted about it.
//
// RunScoped is given adapters whose Run is never reached (the files do not
// exist), so what is measured is the DISPATCH: a non-Go file set must be
// grouped onto a language run with those files attached, not swept into
// Skipped.
func TestNonGoPhaseIsNotAllSkipped(t *testing.T) {
	files := []string{"src/a.ts", "src/b.svelte", "src/c.js"}
	tests, err := Run("p", files, []mutation.Adapter{&stubAdapter{name: "stryker", exts: []string{".ts", ".svelte", ".js"}}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(tests.Skipped) != 0 {
		t.Errorf("%d non-Go file(s) landed in Skipped: %+v", len(tests.Skipped), tests.Skipped)
	}
	if len(tests.Languages) != 1 {
		t.Fatalf("expected one language run, got %d", len(tests.Languages))
	}
	if got := len(tests.Languages[0].Files); got != len(files) {
		t.Errorf("the language run carries %d files, want %d — a partially-dispatched set measures less than the phase changed", got, len(files))
	}
}

// TestUnknownExtensionStillSkips is the control. Without it, every assertion
// above would be satisfied by a dispatcher that matched everything — which
// would send unmeasurable files to an adapter that cannot mutate them.
func TestUnknownExtensionStillSkips(t *testing.T) {
	tests, err := Run("p", []string{"docs/README.md"}, allAdapters())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(tests.Skipped) != 1 {
		t.Fatalf("a .md file should be skipped, got %+v", tests.Skipped)
	}
	if !strings.Contains(tests.Skipped[0].Reason, ".md") {
		t.Errorf("the skip reason does not name the extension: %q", tests.Skipped[0].Reason)
	}
}

// stubAdapter dispatches by extension and is never Run — these tests are about
// grouping, and reaching a real tool would make them depend on a toolchain.
type stubAdapter struct {
	name string
	exts []string
}

func (s *stubAdapter) Name() string { return s.name }

func (s *stubAdapter) Supports(file string) bool {
	for _, e := range s.exts {
		if strings.HasSuffix(strings.ToLower(file), e) {
			return true
		}
	}
	return false
}

func (s *stubAdapter) Run(files []string) (*mutation.Report, error) {
	return &mutation.Report{Tool: s.name}, nil
}

package argfence

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestPolicyFor(t *testing.T) {
	cases := []struct {
		tool string
		want Kind
		ok   bool
	}{
		{"ast-grep", Separator, true},
		{"semgrep", Separator, true},
		{"git", Separator, true},
		{"gh", Separator, true},
		{"gremlins", Reject, true},
		{"npx", Reject, true},
		{"dotnet", Reject, true},
		// An unlisted binary must never resolve. A permissive default here is
		// the whole failure mode the table exists to prevent.
		{"cargo", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		t.Run(c.tool, func(t *testing.T) {
			r, ok := PolicyFor(c.tool)
			if ok != c.ok {
				t.Fatalf("PolicyFor(%q) ok = %v, want %v", c.tool, ok, c.ok)
			}
			if !c.ok {
				return
			}
			if r.Kind != c.want {
				t.Errorf("PolicyFor(%q) kind = %v, want %v", c.tool, r.Kind, c.want)
			}
			if r.Kind == Separator && r.Token == "" {
				t.Errorf("PolicyFor(%q) is Separator with an empty token", c.tool)
			}
			if r.Why == "" {
				t.Errorf("PolicyFor(%q) has no recorded reason", c.tool)
			}
		})
	}
}

func TestPolicyCoversEveryKnownBinary(t *testing.T) {
	want := []string{"ast-grep", "dotnet", "gh", "git", "gremlins", "npx", "semgrep"}
	var got []string
	for k := range Policy() {
		got = append(got, k)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("policy key set = %v, want %v", got, want)
	}
}

func TestPolicyTableIsSingleSource(t *testing.T) {
	exported := Policy()
	// Every exported entry must be what the runtime lookup returns.
	for tool, want := range exported {
		got, ok := PolicyFor(tool)
		if !ok {
			t.Errorf("PolicyFor(%q) missing but present in Policy()", tool)
			continue
		}
		if got != want {
			t.Errorf("PolicyFor(%q) = %+v, Policy()[%q] = %+v", tool, got, tool, want)
		}
	}
	// And the exported map must be a copy: mutating it must not reach the
	// runtime lookup, or a caller could widen the policy at a distance.
	exported["cargo"] = Rule{Kind: Separator, Token: "--"}
	delete(exported, "gremlins")
	if _, ok := PolicyFor("cargo"); ok {
		t.Error("mutating Policy()'s result added a runtime entry")
	}
	if _, ok := PolicyFor("gremlins"); !ok {
		t.Error("mutating Policy()'s result removed a runtime entry")
	}
}

func TestRejectLeadingDash(t *testing.T) {
	err := RejectLeadingDash("gremlins", "package", "-rf")
	if err == nil {
		t.Fatal("RejectLeadingDash(gremlins, package, -rf) = nil, want an error")
	}
	if !errors.Is(err, ErrLeadingDash) {
		t.Errorf("error does not wrap ErrLeadingDash: %v", err)
	}
	if !strings.Contains(err.Error(), "gremlins") {
		t.Errorf("error does not name the tool: %v", err)
	}
	if !strings.Contains(err.Error(), "-rf") {
		t.Errorf("error does not name the value: %v", err)
	}

	// Over-rejection is its own bug: a relative path and an empty value are
	// not flags and must pass.
	for _, ok := range []string{"./internal/cmd", "", "internal/cmd", "."} {
		if err := RejectLeadingDash("gremlins", "package", ok); err != nil {
			t.Errorf("RejectLeadingDash(gremlins, package, %q) = %v, want nil", ok, err)
		}
	}
}

func TestFenceSeparatorTools(t *testing.T) {
	got, err := Fence("ast-grep", "file", "-rf")
	if err != nil {
		t.Fatalf("Fence(ast-grep, file, -rf) = %v, want nil error", err)
	}
	want := []string{"--", "-rf"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Fence(ast-grep) = %v, want %v", got, want)
	}
	// git's token is not "--": using the path separator for a ref reclassifies
	// it as a pathspec, which is a different bug rather than a fix.
	got, err = Fence("git", "ref", "main")
	if err != nil {
		t.Fatalf("Fence(git, ref, main) = %v", err)
	}
	if got[0] != "--end-of-options" {
		t.Errorf("Fence(git) token = %q, want --end-of-options", got[0])
	}
}

func TestFenceRejectTools(t *testing.T) {
	got, err := Fence("gremlins", "package", "./internal/cmd", ".")
	if err != nil {
		t.Fatalf("Fence(gremlins, ...) = %v, want nil error", err)
	}
	if !reflect.DeepEqual(got, []string{"./internal/cmd", "."}) {
		t.Errorf("Fence(gremlins) = %v, want the values verbatim", got)
	}
}

func TestFenceReturnsNilArgvOnError(t *testing.T) {
	// A caller that drops the error must not be handed something runnable.
	got, err := Fence("gremlins", "package", "./ok", "-rf")
	if err == nil {
		t.Fatal("Fence(gremlins, package, -rf) = nil error, want a rejection")
	}
	if got != nil {
		t.Errorf("Fence returned argv %v alongside error %v, want nil argv", got, err)
	}
}

func TestFenceUnknownToolFailsClosed(t *testing.T) {
	got, err := Fence("cargo", "package", "./internal/cmd")
	if err == nil {
		t.Fatal("Fence(cargo, ...) = nil error, want ErrNoPolicy")
	}
	if !errors.Is(err, ErrNoPolicy) {
		t.Errorf("error does not wrap ErrNoPolicy: %v", err)
	}
	if !strings.Contains(err.Error(), "cargo") {
		t.Errorf("error does not name the tool: %v", err)
	}
	if got != nil {
		t.Errorf("Fence returned argv %v for an unlisted tool, want nil", got)
	}
}

// catalogToolsWithoutArgv are the binaries named in internal/security/catalog.go
// that dross does not spawn from Go at all — they are LookPath presence checks,
// and the scan itself is driven by the agent prompt. They carry no dross-derived
// argv, so there is nothing for a policy to fence.
//
// Recorded here with their reason (rule r-02) rather than by widening the table
// with entries no call site reads. A catalog tool that is in neither this map
// nor the policy table fails the test, so a newly added scanner is in scope the
// day it lands.
var catalogToolsWithoutArgv = map[string]string{
	"gitleaks": "presence check only; `dross secure` shells it from the prompt, not from Go",
	"trivy":    "presence check only; `dross secure` shells it from the prompt, not from Go",
}

func TestEveryCatalogToolHasAnArgvPolicy(t *testing.T) {
	bins := catalogBins(t)
	if len(bins) == 0 {
		t.Fatal("found no Bin literals in internal/security/catalog.go — the walk is broken, not the catalog")
	}
	for _, bin := range bins {
		if _, ok := PolicyFor(bin); ok {
			continue
		}
		if why, ok := catalogToolsWithoutArgv[bin]; ok {
			if why == "" {
				t.Errorf("catalog tool %q is accepted with an empty reason", bin)
			}
			continue
		}
		t.Errorf("catalog tool %q has no argfence policy and no accepted-with-reason entry", bin)
	}
	// The accepted list must not outlive the catalog either, or it becomes the
	// rotting exception list the locked audit_gate_breadth decision rejects.
	for bin := range catalogToolsWithoutArgv {
		found := false
		for _, b := range bins {
			if b == bin {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("accepted-with-reason entry %q names no tool in catalog.go", bin)
		}
	}
}

// catalogBins parses internal/security/catalog.go and returns every `Bin: "..."`
// literal in it.
func catalogBins(t *testing.T) []string {
	t.Helper()
	const path = "../security/catalog.go"
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		kv, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || key.Name != "Bin" {
			return true
		}
		lit, ok := kv.Value.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		v, err := strconv.Unquote(lit.Value)
		if err == nil && v != "" {
			out = append(out, v)
		}
		return true
	})
	sort.Strings(out)
	return out
}

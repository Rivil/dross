package boardsync

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/forge"
)

// TestResolvePhaseIssueFirstSyncIsSilent pins c-3 where it was reported: the
// first sync of a new phase looks its card up by dross/phase:<id>, which the
// board has never seen. That is "no card yet", not a stale filter, so it says
// nothing. The marker is known, so the legacy title lookup runs warning-free
// too.
func TestResolvePhaseIssueFirstSyncIsSilent(t *testing.T) {
	var phaseQueries int
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/issueTags") {
			_, _ = io.WriteString(w, `[{"name":"dross"}]`)
			return
		}
		if strings.Contains(r.URL.Query().Get("query"), "dross/phase:") {
			phaseQueries++
		}
		_, _ = io.WriteString(w, `[]`)
	})
	_, ctx := ytRepoMode(t, h, emptyBoard, "")

	var key string
	warn := captureStderr(t, func() {
		var err error
		if key, err = ResolvePhaseIssue(ctx, "new", "phase new"); err != nil {
			t.Fatalf("ResolvePhaseIssue: %v", err)
		}
	})
	if key != "" {
		t.Errorf("key = %q, want none", key)
	}
	if phaseQueries != 0 {
		t.Errorf("issued %d queries for the unknown phase label, want 0", phaseQueries)
	}
	if warn != "" {
		t.Errorf("stderr = %q, want nothing on a first sync", warn)
	}
}

// TestIdentityLabelsAgreeWithForge is the drift guard for the two places the
// identity vocabulary lives: the constructors boardsync stamps cards with, and
// forge's list that silences an unknown one. A prefix registered on one side
// only fails here.
func TestIdentityLabelsAgreeWithForge(t *testing.T) {
	for _, spec := range identityLabels {
		if !forge.IsIdentityLabel(spec.prefix + "x") {
			t.Errorf("reap identity prefix %q is not a forge identity label", spec.prefix)
		}
	}
	for _, l := range []string{PhaseLabel("p"), TaskLabel("p", "t-1"), DeferredLabel("abc"), TargetLabel("slug")} {
		if !forge.IsIdentityLabel(l) {
			t.Errorf("%q is stamped as an identity label but forge does not treat it as one", l)
		}
	}
	for _, l := range []string{StatusLabel("done"), StatusLabel(StatusPlanned), LabelQuick, LabelMarker} {
		if forge.IsIdentityLabel(l) {
			t.Errorf("%q is shared by many cards, yet forge treats it as identity", l)
		}
	}

	reap := map[string]bool{}
	for _, spec := range identityLabels {
		reap[spec.prefix] = true
	}
	stamped := stampedLabelPrefixes(t)
	for _, p := range forge.IdentityLabelPrefixes() {
		if !reap[p] {
			t.Errorf("forge identity prefix %q is not classified by reap", p)
		}
		if !stamped[p] {
			t.Errorf("forge identity prefix %q is never stamped by boardsync", p)
		}
		delete(stamped, p)
	}
	var extra []string
	for p := range stamped {
		extra = append(extra, p)
	}
	sort.Strings(extra)
	if len(extra) > 0 {
		t.Errorf("boardsync stamps %v, which forge does not list as identity — register them in forge.IdentityLabelPrefixes", extra)
	}
}

// prefixLiteral matches a label-prefix literal: "dross/<kind>:".
var prefixLiteral = regexp.MustCompile(`^dross/[a-z]+:$`)

// stampedLabelPrefixes reads every dross/<kind>: string literal out of this
// package's non-test source. dross/status: is excluded by name: a status label
// is shared lifecycle state, never an identity.
func stampedLabelPrefixes(t *testing.T) map[string]bool {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	out := map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(fset, f, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			v, err := strconv.Unquote(lit.Value)
			if err == nil && prefixLiteral.MatchString(v) && v != "dross/status:" {
				out[v] = true
			}
			return true
		})
	}
	if len(out) == 0 {
		t.Fatal("found no dross/<kind>: literals — the scan is reading the wrong directory")
	}
	return out
}

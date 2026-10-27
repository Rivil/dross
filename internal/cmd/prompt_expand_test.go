package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Rivil/dross/assets"
)

// expandedPrompt is a prompt as the agent loads it: the base file with every
// partial it fetches inlined at its fetch line (assets.Expand over promptFS).
// Prompt-content tests read through it so an instruction moved into a partial
// is still seen, by positive and negative assertions alike. A test that needs
// the bare base prompt reads assets.FS directly.
func expandedPrompt(t *testing.T, name string) string {
	t.Helper()
	b, err := assets.Expand(promptFS, name)
	if err != nil {
		t.Fatalf("expand %s: %v", name, err)
	}
	return string(b)
}

// normalisePrompt lowercases s and drops markdown emphasis and underscores,
// so a phrase matches however it is formatted.
func normalisePrompt(s string) string {
	return strings.NewReplacer("`", "", "*", "", "_", "").Replace(strings.ToLower(s))
}

// rawPromptReadAllowlist names, as <file>#<func>, the internal/ tests that may
// still read a prompt file off disk rather than through expandedPrompt. It was
// seeded from every such reader when prompts gained partials and may only
// shrink: rawPromptReadAllowlistSize pins its size.
var rawPromptReadAllowlist = map[string]bool{
	"internal/boardsync/board_lifecycle_divergence_test.go#rawPrompt":               true,
	"internal/cmd/architecture_prompt_test.go#architecturePromptContent":            true,
	"internal/cmd/execconsent_docs_test.go#TestPromptsDescribeTheGateAsEnumerated":  true,
	"internal/cmd/inbox_prompt_test.go#inboxPromptContent":                          true,
	"internal/cmd/install_symlink_test.go#TestMakeInstallLinksInteractionSnippet":   true,
	"internal/cmd/interaction_coreloop_test.go#TestPlanPromptNoArtifactDump":        true,
	"internal/cmd/interaction_coreloop_test.go#TestShipPromptDecisionTurnsSeparate": true,
	"internal/cmd/interaction_othercmds_test.go#otherCmdPromptBody":                 true,
	"internal/cmd/interaction_pilot_test.go#TestSpecPilotUsesEmitter":               true,
	"internal/cmd/interaction_setupcmds_test.go#setupPromptBody":                    true,
	"internal/cmd/interaction_snippet_test.go#TestFooterPromptsLoadPlaybook":        true,
	"internal/cmd/interaction_snippet_test.go#contextRuleSection":                   true,
	"internal/cmd/interaction_snippet_test.go#interactionSnippetContent":            true,
	"internal/cmd/interaction_test.go#TestInteractionPlaybookSingleSource":          true,
	"internal/cmd/milestone_dispatch_prompt_test.go#milestonePromptText":            true,
	"internal/cmd/options_docs_test.go#TestVerifyPromptTeachesTheDetachedPath":      true,
	"internal/cmd/options_docs_test.go#optionsPrompt":                               true,
	"internal/cmd/pause_prompt_test.go#pausePromptContent":                          true,
	"internal/cmd/phase_test.go#TestPhaseListPromptConsumersAreCurrent":             true,
	"internal/cmd/plan_prompt_test.go#planPromptContent":                            true,
	"internal/cmd/plan_review_tiers_test.go#TestPlanPromptDescribesEveryReviewTier": true,
	"internal/cmd/pull_envelope_test.go#TestPromptsDescribeTheEnvelopeContract":     true,
	"internal/cmd/quality_prompt_test.go#qualityPromptContent":                      true,
	"internal/cmd/quick_prompt_test.go#quickPromptContent":                          true,
	"internal/cmd/readme_doc_test.go#optionsSection":                                true,
	"internal/cmd/respond_prompt_test.go#respondPrompt":                             true,
	"internal/cmd/resume_prompt_test.go#resumePromptContent":                        true,
	"internal/cmd/secure_prompt_test.go#securePromptContent":                        true,
	"internal/cmd/ship_prompt_test.go#rawShipSection":                               true,
	"internal/cmd/spec_prompt_test.go#specPromptContent":                            true,
	"internal/cmd/squash_claim_guard_test.go#TestQuickPromptHandlesShippedWindow":   true,
	"internal/cmd/verify_prompt_test.go#TestVerifyPromptOffloadGuidance":            true,
	"internal/cmd/verify_prompt_test.go#TestVerifyPromptReadsRangeProvenance":       true,
	"internal/cmd/verify_prompt_test.go#verifyPromptBody":                           true,
	"internal/cmd/watch_prompt_test.go#watchPromptContent":                          true,
	"internal/cmd/watch_test.go#TestWatchPromptNamesEveryDigestField":               true,
	"internal/prtriage/split_test.go#reviewTemplate":                                true,
	"internal/rules/rules_test.go#TestInteractionContractNoDrift":                   true,
	"internal/rules/rules_test.go#TestInteractionRuleUsesEmitter":                   true,
	"internal/rules/rules_test.go#TestSpecPromptDefersToSnippet":                    true,
}

const rawPromptReadAllowlistSize = 40

// rawPromptReads finds every os.ReadFile or readRepoFile call in internal/
// test files whose path argument names assets and prompts — as one literal,
// or as separate filepath.Join literals — keyed <file>#<func> with the
// file:line of each call.
func rawPromptReads(t *testing.T) map[string][]string {
	t.Helper()
	root := repoRootFromTest(t)
	out := map[string][]string{}
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, "_test.go") {
			return err
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if !isRawPromptRead(call) {
					return true
				}
				key := rel + "#" + fn.Name.Name
				out[key] = append(out[key], rel+":"+strconv.Itoa(fset.Position(call.Pos()).Line))
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// isRawPromptRead reports whether call is os.ReadFile or readRepoFile of a
// path under assets/prompts. assets.FS.ReadFile — the sanctioned raw path —
// is neither.
func isRawPromptRead(call *ast.CallExpr) bool {
	arg := -1
	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		if x, ok := fun.X.(*ast.Ident); ok && x.Name == "os" && fun.Sel.Name == "ReadFile" {
			arg = 0
		}
	case *ast.Ident:
		if fun.Name == "readRepoFile" {
			arg = 1
		}
	}
	return arg >= 0 && len(call.Args) > arg && namesPromptsDir(call.Args[arg])
}

// namesPromptsDir reports whether expr's string literals spell a path under
// assets/prompts.
func namesPromptsDir(expr ast.Expr) bool {
	var assetsLit, promptsLit bool
	ast.Inspect(expr, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		v, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		if strings.Contains(v, "assets/prompts") {
			assetsLit, promptsLit = true, true
		}
		assetsLit = assetsLit || v == "assets"
		promptsLit = promptsLit || v == "prompts"
		return true
	})
	return assetsLit && promptsLit
}

// TestPromptReadsGoThroughExpansion keeps prompt-content tests on the expanded
// prompt: a raw read of a prompt file would miss every instruction moved into
// a partial. assets.FS.ReadFile stays the sanctioned raw path.
func TestPromptReadsGoThroughExpansion(t *testing.T) {
	if len(rawPromptReadAllowlist) != rawPromptReadAllowlistSize {
		t.Errorf("rawPromptReadAllowlist has %d entries, pinned at %d — it may only shrink: lower the pin with the entry you removed, never add one",
			len(rawPromptReadAllowlist), rawPromptReadAllowlistSize)
	}
	reads := rawPromptReads(t)
	for key, at := range reads {
		if !rawPromptReadAllowlist[key] {
			t.Errorf("%s (%s) reads a prompt off disk; read it through expandedPrompt (or assets.FS for the bare base prompt)", strings.Join(at, ", "), key)
		}
	}
	for key := range rawPromptReadAllowlist {
		if _, ok := reads[key]; !ok {
			t.Errorf("rawPromptReadAllowlist entry %s matches no raw read — remove it and lower the pin", key)
		}
	}
}

func TestRawPromptReadDetection(t *testing.T) {
	for src, want := range map[string]bool{
		`os.ReadFile(filepath.Join(root, "assets", "prompts", "verify.md"))`: true,
		`os.ReadFile("assets/prompts/verify.md")`:                            true,
		`readRepoFile(t, filepath.Join("assets", "prompts", "verify.md"))`:   true,
		`assets.FS.ReadFile("prompts/verify.md")`:                            false,
		`os.ReadFile(filepath.Join(root, "README.md"))`:                      false,
		`os.ReadFile(filepath.Join(root, "assets", "commands", "x.md"))`:     false,
	} {
		expr, err := parser.ParseExpr(src)
		if err != nil {
			t.Fatal(err)
		}
		if got := isRawPromptRead(expr.(*ast.CallExpr)); got != want {
			t.Errorf("isRawPromptRead(%s) = %v, want %v", src, got, want)
		}
	}
}

// TestPromptLoadersCompose: every prompt loader reads the expanded prompt,
// each keeping its own normalisation.
func TestPromptLoadersCompose(t *testing.T) {
	files, err := fs.Glob(assets.FS, "prompts/[a-z]*.md")
	if err != nil || len(files) == 0 {
		t.Fatalf("no prompts found (%v)", err)
	}
	for _, f := range files {
		name := filepath.Base(f)
		want := expandedPrompt(t, name)
		for loader, got := range map[string]string{
			"promptBody":    promptBody(t, name),
			"rawPrompt":     rawPrompt(t, name),
			"readPrompt":    readPrompt(t, name),
			"promptContent": promptContent(t, name),
		} {
			w := want
			if loader == "promptContent" {
				w = normalisePrompt(want)
			}
			if got != w {
				t.Errorf("%s(%s) does not compose over the expanded prompt", loader, name)
			}
		}
	}
	if executePromptContent(t) != normalisePrompt(expandedPrompt(t, "execute.md")) {
		t.Error("executePromptContent does not compose over the expanded execute.md")
	}
}

// TestPromptLoadersSeePartials: text that lives only in a partial reaches the
// loaders, so a negative assertion over them cannot miss it.
func TestPromptLoadersSeePartials(t *testing.T) {
	seedPromptFS(t, fstest.MapFS{
		"prompts/execute.md":       {Data: []byte("# execute\n**When** `mode=solo` — solo: `dross prompt partial execute solo`\nend\n")},
		"prompts/_execute.solo.md": {Data: []byte("a BANNED_PHRASE only here\n")},
	})
	for loader, got := range map[string]string{
		"promptBody": promptBody(t, "execute.md"),
		"rawPrompt":  rawPrompt(t, "execute.md"),
		"readPrompt": readPrompt(t, "execute.md"),
	} {
		if !strings.Contains(got, "BANNED_PHRASE") {
			t.Errorf("%s missed text that lives only in a partial", loader)
		}
	}
	if !strings.Contains(promptContent(t, "execute.md"), "bannedphrase") || !strings.Contains(executePromptContent(t), "bannedphrase") {
		t.Error("the normalised loaders missed text that lives only in a partial")
	}
}

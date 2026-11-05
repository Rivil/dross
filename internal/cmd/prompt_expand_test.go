package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
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

// rawPromptReadAllowlist names, as <file>#<func>, the internal/ and cmd/ tests that may
// still read a prompt file off disk rather than through expandedPrompt. It was
// seeded from every such reader when prompts gained partials and may only
// shrink: rawPromptReadAllowlistSize pins its size.
var rawPromptReadAllowlist = map[string]bool{
	"internal/cmd/architecture_prompt_test.go#architecturePromptContent":          true,
	"internal/cmd/inbox_prompt_test.go#inboxPromptContent":                        true,
	"internal/cmd/install_symlink_test.go#TestMakeInstallLinksInteractionSnippet": true,
	"internal/cmd/interaction_othercmds_test.go#otherCmdPromptBody":               true,
	"internal/cmd/interaction_snippet_test.go#TestFooterPromptsLoadPlaybook":      true,
	"internal/cmd/interaction_snippet_test.go#contextRuleSection":                 true,
	"internal/cmd/interaction_snippet_test.go#interactionSnippetContent":          true,
	"internal/cmd/interaction_test.go#TestInteractionPlaybookSingleSource":        true,
	"internal/cmd/milestone_dispatch_prompt_test.go#milestonePromptText":          true,
	"internal/cmd/pause_prompt_test.go#pausePromptContent":                        true,
	"internal/cmd/pull_envelope_test.go#TestPromptsDescribeTheEnvelopeContract":   true,
	"internal/cmd/quality_prompt_test.go#qualityPromptContent":                    true,
	"internal/cmd/respond_prompt_test.go#respondPrompt":                           true,
	"internal/cmd/resume_prompt_test.go#resumePromptContent":                      true,
	"internal/cmd/secure_prompt_test.go#securePromptContent":                      true,
	"internal/cmd/watch_prompt_test.go#watchPromptContent":                        true,
	"internal/cmd/watch_test.go#TestWatchPromptNamesEveryDigestField":             true,
	"internal/prtriage/split_test.go#reviewTemplate":                              true,
	"internal/rules/rules_test.go#TestInteractionContractNoDrift":                 true,
	"internal/rules/rules_test.go#TestInteractionRuleUsesEmitter":                 true,
}

const rawPromptReadAllowlistSize = 20

// promptGuardRoots are the trees the raw-read guards walk, relative to the
// repo root: internal/ holds the prompt-content tests, cmd/ the binaries'.
var promptGuardRoots = []string{"internal", "cmd"}

// eachTestFunc parses every _test.go file under root's promptGuardRoots and
// calls visit with the file's root-relative slash path, its file set and each
// function declaration that has a body.
func eachTestFunc(t *testing.T, root string, visit func(rel string, fset *token.FileSet, fn *ast.FuncDecl)) {
	t.Helper()
	for _, dir := range promptGuardRoots {
		err := filepath.WalkDir(filepath.Join(root, dir), func(p string, d fs.DirEntry, err error) error {
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
				if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
					visit(rel, fset, fn)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// rawPromptReads finds every raw prompt read (see isRawPromptRead) in the test
// files under root's promptGuardRoots, keyed <file>#<func> with the file:line
// of each call.
func rawPromptReads(t *testing.T, root string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	eachTestFunc(t, root, func(rel string, fset *token.FileSet, fn *ast.FuncDecl) {
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !isRawPromptRead(call) {
				return true
			}
			key := rel + "#" + fn.Name.Name
			out[key] = append(out[key], rel+":"+strconv.Itoa(fset.Position(call.Pos()).Line))
			return true
		})
	})
	return out
}

// pathCalls build, list or stat a path without reading a file, so prompt path
// literals passed to one are not a read.
var pathCalls = map[string]bool{"filepath.Join": true, "path.Join": true, "filepath.Glob": true, "os.ReadDir": true, "os.Stat": true}

// isRawPromptRead reports whether call reads a file under assets/prompts off
// disk: os.ReadFile or readRepoFile of such a path, or any other call — a read
// helper like docsBody(t, "assets", "prompts", "options.md") — whose own
// arguments are the literals "assets", "prompts" and a <file>.md. pathCalls
// are never reads, and assets.FS.ReadFile — the sanctioned raw path — names no
// assets literal.
func isRawPromptRead(call *ast.CallExpr) bool {
	switch name := calleeName(call.Fun); {
	case name == "os.ReadFile":
		return len(call.Args) > 0 && namesPromptsDir(call.Args[0])
	case name == "readRepoFile":
		return len(call.Args) > 1 && namesPromptsDir(call.Args[1])
	case pathCalls[name]:
		return false
	}
	return argsSpellPromptFile(call.Args)
}

// calleeName is fun as written — "name" or "pkg.Name" — or "" for any other
// callee expression.
func calleeName(fun ast.Expr) string {
	switch fun := fun.(type) {
	case *ast.Ident:
		return fun.Name
	case *ast.SelectorExpr:
		if x, ok := fun.X.(*ast.Ident); ok {
			return x.Name + "." + fun.Sel.Name
		}
	}
	return ""
}

// argsSpellPromptFile reports whether args themselves — not literals nested in
// them — include "assets", "prompts" and a <file>.md.
func argsSpellPromptFile(args []ast.Expr) bool {
	var assetsLit, promptsLit, mdLit bool
	for _, arg := range args {
		lit, ok := arg.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			continue
		}
		v, err := strconv.Unquote(lit.Value)
		if err != nil {
			continue
		}
		assetsLit = assetsLit || v == "assets"
		promptsLit = promptsLit || v == "prompts"
		mdLit = mdLit || strings.HasSuffix(v, ".md")
	}
	return assetsLit && promptsLit && mdLit
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
	reads := rawPromptReads(t, repoRootFromTest(t))
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
		// A read helper taking the path as variadic parts is a read.
		`docsBody(t, "assets", "prompts", "options.md")`:  true,
		`readDoc(t, "assets", "prompts", "ship.md")`:      true,
		`docText(t, "assets", "prompts", "status.md")`:    true,
		`h.read("assets", "prompts", "ship.md")`:          true,
		`docsBody(t, "README.md")`:                        false,
		`docsBody(t, "assets", "commands", "options.md")`: false,
		`expandedPrompt(t, "ship.md")`:                    false,
		// Building, listing or statting a prompt path reads nothing.
		`filepath.Join(root, "assets", "prompts", "ship.md")`:                   false,
		`path.Join("assets", "prompts", "ship.md")`:                             false,
		`os.ReadDir(filepath.Join(root, "assets", "prompts"))`:                  false,
		`filepath.Glob(filepath.Join(root, "assets", "prompts", "*.md"))`:       false,
		`os.Stat(filepath.Join(root, "assets", "prompts", "ship.md"))`:          false,
		`fixture(t, filepath.Join(root, "assets", "prompts", "ship.md"), body)`: false,
	} {
		expr, err := parser.ParseExpr(src)
		if err != nil {
			t.Fatal(err)
		}
		// Every call in the expression, as the walk sees it: the Join nested
		// in os.ReadDir must not count on its own either.
		got := false
		ast.Inspect(expr, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok && isRawPromptRead(call) {
				got = true
			}
			return true
		})
		if got != want {
			t.Errorf("isRawPromptRead over %s = %v, want %v", src, got, want)
		}
	}
}

// TestRawPromptReadsWalkCmd: the guard walks cmd/ as well as internal/, so a
// binary's test reading a prompt off disk is reported under its own key.
func TestRawPromptReadsWalkCmd(t *testing.T) {
	root := t.TempDir()
	for rel, body := range map[string]string{
		"cmd/dross/main_test.go":   "package main\n\nfunc TestShip(t *testing.T) {\n\tos.ReadFile(filepath.Join(\"..\", \"..\", \"assets\", \"prompts\", \"ship.md\"))\n}\n",
		"internal/x/x_test.go":     "package x\n\nfunc TestOptions(t *testing.T) {\n\tdocsBody(t, \"assets\", \"prompts\", \"options.md\")\n}\n",
		"internal/x/clean_test.go": "package x\n\nfunc TestClean(t *testing.T) {\n\texpandedPrompt(t, \"ship.md\")\n}\n",
	} {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := rawPromptReads(t, root)
	want := map[string][]string{
		"cmd/dross/main_test.go#TestShip":  {"cmd/dross/main_test.go:4"},
		"internal/x/x_test.go#TestOptions": {"internal/x/x_test.go:4"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rawPromptReads = %v, want %v", got, want)
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
			"promptDocText": promptDocText(t, name),
		} {
			w := want
			switch loader {
			case "promptContent":
				w = normalisePrompt(want)
			case "promptDocText":
				w = normaliseDoc(want)
			}
			if got != w {
				t.Errorf("%s(%s) does not compose over the expanded prompt", loader, name)
			}
		}
	}
	if executePromptContent(t) != normalisePrompt(expandedPrompt(t, "execute.md")) {
		t.Error("executePromptContent does not compose over the expanded execute.md")
	}
	if specPromptContent(t) != normalisePrompt(expandedPrompt(t, "spec.md")) {
		t.Error("specPromptContent does not compose over the expanded spec.md")
	}
	if planPromptContent(t) != normalisePrompt(expandedPrompt(t, "plan.md")) {
		t.Error("planPromptContent does not compose over the expanded plan.md")
	}
	if quickPromptContent(t) != normaliseKeepUnderscores(expandedPrompt(t, "quick.md")) {
		t.Error("quickPromptContent does not compose over the expanded quick.md")
	}
	if optionsPrompt(t) != expandedPrompt(t, "options.md") {
		t.Error("optionsPrompt does not compose over the expanded options.md")
	}
	for _, name := range setupPrompts {
		if setupPromptBody(t, name) != expandedPrompt(t, name+".md") {
			t.Errorf("setupPromptBody(%s) does not compose over the expanded %s.md", name, name)
		}
	}
}

// TestFooterPlaybookReadStaysRaw: the footer check asks whether the base prompt
// itself runs `dross interaction show` — a pre-flight a partial cannot carry —
// so its raw read is by design and keeps its allowlist entry.
func TestFooterPlaybookReadStaysRaw(t *testing.T) {
	if !rawPromptReadAllowlist["internal/cmd/interaction_snippet_test.go#TestFooterPromptsLoadPlaybook"] {
		t.Error("TestFooterPromptsLoadPlaybook left the raw-read allowlist; its base-prompt read is raw by design")
	}
}

// TestOptionsSectionFindsEveryHeading: every numbered heading of the bare
// options.md stays in the base prompt, so optionsSection, reading the expanded
// prompt, finds a non-empty body for each one.
func TestOptionsSectionFindsEveryHeading(t *testing.T) {
	b, err := assets.FS.ReadFile("prompts/options.md")
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, line := range strings.Split(string(b), "\n") {
		rest, ok := strings.CutPrefix(line, "## ")
		if !ok {
			continue
		}
		num, _, ok := strings.Cut(rest, ".")
		n, err := strconv.Atoi(num)
		if !ok || err != nil {
			continue
		}
		found++
		if strings.TrimSpace(optionsSection(t, n)) == "" {
			t.Errorf("optionsSection(%d) is empty for options.md heading %q", n, line)
		}
	}
	if found == 0 {
		t.Fatal("options.md has no numbered headings — the check would pass vacuously")
	}
}

// TestPromptLoadersSeePartials: text that lives only in a partial reaches the
// loaders, so a negative assertion over them cannot miss it.
func TestPromptLoadersSeePartials(t *testing.T) {
	seedPromptFS(t, fstest.MapFS{
		"prompts/execute.md":        {Data: []byte("# execute\n**When** `mode=solo` — solo: `dross prompt partial execute solo`\nend\n")},
		"prompts/_execute.solo.md":  {Data: []byte("a BANNED_PHRASE only here\n")},
		"prompts/spec.md":           {Data: []byte("# spec\n**When** `deferral=chosen` — a deferral is chosen: `dross prompt partial spec route`\nend\n")},
		"prompts/_spec.route.md":    {Data: []byte("a BANNED_PHRASE only here\n")},
		"prompts/plan.md":           {Data: []byte("# plan\n**When** `panel=on` — the panel is asked for: `dross prompt partial plan panel`\nend\n")},
		"prompts/_plan.panel.md":    {Data: []byte("a BANNED_PHRASE only here\n")},
		"prompts/quick.md":          {Data: []byte("# quick\n**When** `mode=solo` — solo: `dross prompt partial quick solo`\nend\n")},
		"prompts/_quick.solo.md":    {Data: []byte("a BANNED_PHRASE only here\n")},
		"prompts/options.md":        {Data: []byte("# options\n## 1. Lanes\n**When** `lanes=editing` — a lane is being edited: `dross prompt partial options lanes`\n## 2. Next\nend\n")},
		"prompts/_options.lanes.md": {Data: []byte("a BANNED_PHRASE only here\n")},
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
	if !strings.Contains(promptContent(t, "execute.md"), "bannedphrase") || !strings.Contains(executePromptContent(t), "bannedphrase") || !strings.Contains(specPromptContent(t), "bannedphrase") || !strings.Contains(planPromptContent(t), "bannedphrase") {
		t.Error("the normalised loaders missed text that lives only in a partial")
	}
	// quickPromptContent keeps underscores.
	if !strings.Contains(quickPromptContent(t), "banned_phrase") {
		t.Error("quickPromptContent missed text that lives only in a partial")
	}
	for loader, got := range map[string]string{
		"optionsPrompt":            optionsPrompt(t),
		"optionsSection(1)":        optionsSection(t, 1),
		"setupPromptBody(quick)":   setupPromptBody(t, "quick"),
		"setupPromptBody(options)": setupPromptBody(t, "options"),
	} {
		if !strings.Contains(got, "BANNED_PHRASE") {
			t.Errorf("%s missed text that lives only in a partial", loader)
		}
	}
}

// splitPrompts lists, as <name>.md, every base prompt in fsys with at least
// one partial — the registry, not a hand-kept list, so a prompt that gains a
// partial is covered the moment its file lands. No allowlisted raw reader may
// read one, directly or through a helper called with its name.
func splitPrompts(t *testing.T, fsys fs.FS) []string {
	t.Helper()
	files, err := fs.Glob(fsys, "prompts/[a-z]*.md")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, f := range files {
		name := strings.TrimSuffix(path.Base(f), ".md")
		names, err := assets.PartialNames(fsys, name)
		if err != nil {
			t.Fatal(err)
		}
		if len(names) > 0 {
			out = append(out, name+".md")
		}
	}
	return out
}

// TestSplitPromptsFollowTheRegistry: a partial file landing makes its prompt a
// split prompt, with no list edited; _interaction.md is no partial.
func TestSplitPromptsFollowTheRegistry(t *testing.T) {
	fsys := fstest.MapFS{
		"prompts/execute.md":       {Data: []byte("# execute\n")},
		"prompts/_execute.solo.md": {Data: []byte("solo\n")},
		"prompts/plan.md":          {Data: []byte("# plan\n")},
		"prompts/ship.md":          {Data: []byte("# ship\n")},
		"prompts/_interaction.md":  {Data: []byte("playbook\n")},
	}
	if got, want := splitPrompts(t, fsys), []string{"execute.md"}; !reflect.DeepEqual(got, want) {
		t.Errorf("splitPrompts = %v, want %v", got, want)
	}
	fsys["prompts/_plan.panel.md"] = &fstest.MapFile{Data: []byte("panel\n")}
	if got, want := splitPrompts(t, fsys), []string{"execute.md", "plan.md"}; !reflect.DeepEqual(got, want) {
		t.Errorf("after _plan.panel.md lands, splitPrompts = %v, want %v", got, want)
	}
	if !namesSplitPrompt(&ast.BasicLit{Kind: token.STRING, Value: `"plan.md"`}, splitPrompts(t, fsys)) {
		t.Error("namesSplitPrompt does not see plan.md once it has a partial")
	}
}

// expandingLoaders read the expanded prompt; a split-prompt name passed to one
// is a sanctioned read, not a raw one.
var expandingLoaders = map[string]bool{"expandedPrompt": true, "promptBody": true, "rawPrompt": true, "readPrompt": true, "promptContent": true, "promptDocText": true}

// namesSplitPrompt reports whether expr carries a string literal naming one of
// split, outside any call to an expanding loader.
func namesSplitPrompt(expr ast.Node, split []string) bool {
	found := false
	ast.Inspect(expr, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if id, ok := call.Fun.(*ast.Ident); ok && expandingLoaders[id.Name] {
				return false
			}
		}
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		v, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		for _, p := range split {
			if strings.HasSuffix(v, p) {
				found = true
			}
		}
		return true
	})
	return found
}

// allowlistedSplitReads lists each allowlist entry that reads a split prompt:
// a function holding a raw read names one anywhere in its body, or a call to it
// (it being a helper) passes one as a literal.
func allowlistedSplitReads(t *testing.T) []string {
	t.Helper()
	split := splitPrompts(t, assets.FS)
	var out []string
	eachTestFunc(t, repoRootFromTest(t), func(rel string, fset *token.FileSet, fn *ast.FuncDecl) {
		dir := path.Dir(rel)
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			pos := rel + ":" + strconv.Itoa(fset.Position(call.Pos()).Line)
			// The whole body, not just the call: a loop over prompt names
			// passes them through a variable.
			if isRawPromptRead(call) && rawPromptReadAllowlist[rel+"#"+fn.Name.Name] && namesSplitPrompt(fn.Body, split) {
				out = append(out, pos+": allowlisted "+rel+"#"+fn.Name.Name+" reads a split prompt raw")
			}
			// A call to an allowlisted helper in the same package.
			if id, ok := call.Fun.(*ast.Ident); ok && namesSplitPrompt(call, split) {
				for key := range rawPromptReadAllowlist {
					file, name, _ := strings.Cut(key, "#")
					if name == id.Name && path.Dir(file) == dir {
						out = append(out, pos+": calls allowlisted helper "+key+" with a split prompt")
					}
				}
			}
			return true
		})
	})
	sort.Strings(out)
	return out
}

func TestNoAllowlistedReaderReadsASplitPrompt(t *testing.T) {
	for _, p := range allowlistedSplitReads(t) {
		t.Error(p + " — read it through expandedPrompt, or assets.FS for the bare base prompt")
	}
}

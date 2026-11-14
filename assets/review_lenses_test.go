package assets

import (
	"regexp"
	"strings"
	"testing"
)

// The six /dross-review lenses: the four originals plus edge-case and
// silent-failure. Each token must appear on every surface naming the panel.
var sixLensTokens = []string{"security", "quality", "tests", "spec-fidelity", "edge-case", "silent-failure"}

func readAsset(t *testing.T, name string) string {
	t.Helper()
	b, err := FS.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

// lensTemplate returns the blockquote prompt under a review.md lens heading
// (**<name>** followed by its general-purpose subagent_type): the contiguous `>` lines
// that follow it. It fails rather than returning an empty template.
func lensTemplate(t *testing.T, doc, name string) string {
	t.Helper()
	head := "**" + name + "** — `subagent_type: general-purpose`"
	_, after, ok := strings.Cut(doc, head+"\n")
	if !ok {
		t.Fatalf("review.md has no %q lens template", name)
	}
	var lines []string
	for _, l := range strings.Split(after, "\n") {
		if !strings.HasPrefix(l, ">") {
			break
		}
		lines = append(lines, l)
	}
	if len(lines) == 0 {
		t.Fatalf("the %q lens template has no `>` prompt lines", name)
	}
	return strings.Join(lines, "\n")
}

// prefixedLines returns the template lines starting with prefix (e.g. "> Rule:").
func prefixedLines(tmpl, prefix string) []string {
	var out []string
	for _, l := range strings.Split(tmpl, "\n") {
		if strings.HasPrefix(l, prefix) {
			out = append(out, l)
		}
	}
	return out
}

// section returns review.md's text from the heading starting with `from` up
// to the next `## ` heading.
func section(t *testing.T, doc, from string) string {
	t.Helper()
	i := strings.Index(doc, "\n"+from)
	if i < 0 {
		t.Fatalf("review.md has no %q section", from)
	}
	rest := doc[i+1:]
	if j := strings.Index(rest[len(from):], "\n## "); j >= 0 {
		return rest[:len(from)+j]
	}
	return rest
}

func TestReviewSpawnsSixLenses(t *testing.T) {
	doc := readAsset(t, "prompts/review.md")

	numbered := regexp.MustCompile(`(?m)^(\d+)\. \*\*[a-z ]+\*\* — `).FindAllStringSubmatch(doc, -1)
	if len(numbered) != 6 {
		t.Fatalf("review.md lists %d numbered lenses, want 6", len(numbered))
	}
	for i, m := range numbered {
		if want := string(rune('1' + i)); m[1] != want {
			t.Errorf("lens %d is numbered %s", i+1, m[1])
		}
	}
	if n := strings.Count(doc, "`subagent_type: general-purpose`"); n != 6 {
		t.Errorf("review.md holds %d lens templates, want 6", n)
	}
	if !strings.Contains(doc, "all six subagents in a single tool block") {
		t.Error("review.md no longer says to spawn all six subagents in a single tool block")
	}
	if loc := regexp.MustCompile(`(?i)\bfour\b|\b4 lenses`).FindStringIndex(doc); loc != nil {
		line := strings.Count(doc[:loc[0]], "\n") + 1
		t.Errorf("review.md:%d still counts the panel as four: %q", line, doc[loc[0]:loc[1]])
	}
	for _, sec := range []string{"## 2.", "## 3.", "## Hard rules"} {
		if !strings.Contains(section(t, doc, sec), "six") {
			t.Errorf("review.md %q section no longer says six", sec)
		}
	}
	intro, _, _ := strings.Cut(doc, "\n## 0.")
	if strings.Count(intro, "six") < 2 {
		t.Error("review.md intro no longer names the six lenses")
	}
	wrap := section(t, doc, "## 6.")
	for _, s := range []string{"security", "code quality", "test efficacy", "spec fidelity", "edge cases", "silent failures"} {
		if !strings.Contains(wrap, s) {
			t.Errorf("the §6 wrap print omits subagent %q", s)
		}
	}
}

const (
	valueSetRule   = "> Rule: Grep/Read the working tree to find the enum/status/flag's full value set; a finding says where the set is defined."
	defaultArmRule = "> Rule: A catch-all arm that errors, panics or logs counts as handling; a silent catch-all (no-op or fallback value) is flagged, naming the values that fall into it."
	discardRule    = "> Rule: A discarded error with an adjacent comment stating the reason is skipped; every uncommented discard is flagged."
	lensSeverity   = "> Severity: FLAG by default; BLOCKING only when the gap sits on a path a spec.toml criterion covers or verify.toml shows a surviving mutant there."
)

func hasLine(lines []string, want string) bool {
	for _, l := range lines {
		if l == want {
			return true
		}
	}
	return false
}

func TestEdgeCaseLensRules(t *testing.T) {
	tmpl := lensTemplate(t, readAsset(t, "prompts/review.md"), "edge cases")
	rules := prefixedLines(tmpl, "> Rule:")
	if !hasLine(rules, valueSetRule) {
		t.Errorf("edge-case template lost the value_set_source rule line %q; rules: %q", valueSetRule, rules)
	}
	if !hasLine(rules, defaultArmRule) {
		t.Errorf("edge-case template lost the default_arm rule line %q; rules: %q", defaultArmRule, rules)
	}
	for _, needle := range []string{"enum, status or flag", "the diff adds or changes", "names every unhandled value", "file:line"} {
		if !strings.Contains(tmpl, needle) {
			t.Errorf("edge-case template no longer says %q", needle)
		}
	}
}

func TestSilentFailureLensRules(t *testing.T) {
	tmpl := lensTemplate(t, readAsset(t, "prompts/review.md"), "silent failures")
	for _, tc := range []struct{ name, needle string }{
		{"discarded error returns", "discarded error returns"},
		{"empty catch", "empty catch blocks"},
		{"empty recover", "empty recover handlers"},
		{"quiet fallback", "a default returned on failure with nothing logged or surfaced"},
		{"error site scope", "every error site the diff adds or changes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(tmpl, tc.needle) {
				t.Errorf("silent-failure template no longer names %q", tc.needle)
			}
		})
	}
	t.Run("reason-comment exemption", func(t *testing.T) {
		if !hasLine(prefixedLines(tmpl, "> Rule:"), discardRule) {
			t.Errorf("silent-failure template lost the intentional_discards rule line %q", discardRule)
		}
	})
}

func TestNewLensSeverity(t *testing.T) {
	doc := readAsset(t, "prompts/review.md")
	for _, lens := range []string{"edge cases", "silent failures"} {
		t.Run(lens, func(t *testing.T) {
			tmpl := lensTemplate(t, doc, lens)
			if sev := prefixedLines(tmpl, "> Severity:"); len(sev) != 1 || sev[0] != lensSeverity {
				t.Errorf("%s template severity lines = %q, want exactly %q", lens, sev, lensSeverity)
			}
			if !strings.Contains(tmpl, "Read `.dross/phases/<phase-id>/spec.toml` and `.dross/phases/<phase-id>/verify.toml`") {
				t.Errorf("%s template no longer tells the lens to read spec.toml and verify.toml", lens)
			}
		})
	}
}

func TestCodeQualityCedesSilentFailures(t *testing.T) {
	doc := readAsset(t, "prompts/review.md")
	tmpl := strings.ToLower(lensTemplate(t, doc, "code quality"))
	for _, banned := range []string{"error handling", "surprising defaults"} {
		if strings.Contains(tmpl, banned) {
			t.Errorf("code-quality template still names %q (silent-failure owns it)", banned)
		}
	}
	if !strings.Contains(tmpl, "swallowed errors and quiet fallbacks belong to the silent-failure lens") {
		t.Error("code-quality template no longer cedes swallowed errors and quiet fallbacks to the silent-failure lens")
	}
	if strings.Contains(strings.ToLower(doc), "naming, error handling") || regexp.MustCompile(`(?m)^2\. \*\*code quality\*\*.*error handling`).MatchString(doc) {
		t.Error("the lens list still gives error handling to code quality")
	}
	agg := section(t, doc, "## 3.")
	if !strings.Contains(agg, "Drop any code-quality finding about a swallowed error or quiet fallback, regardless of file or line") {
		t.Error("§3 lost the rule dropping code-quality findings about swallowed errors or quiet fallbacks")
	}
}

func TestNewLensesLanguageAgnostic(t *testing.T) {
	doc := readAsset(t, "prompts/review.md")
	silent := lensTemplate(t, doc, "silent failures")
	for _, w := range []string{"catch", "recover"} {
		if !strings.Contains(silent, w) {
			t.Errorf("silent-failure template omits %q — it must name the non-Go shapes too", w)
		}
	}
	for _, lens := range []string{"edge cases", "silent failures"} {
		tmpl := lensTemplate(t, doc, lens)
		for _, goism := range []string{"err != nil", ".go"} {
			if strings.Contains(tmpl, goism) {
				t.Errorf("%s template contains Go-specific %q; lens prompts are language-agnostic", lens, goism)
			}
		}
	}
}

func TestReviewSummaryNamesSixLenses(t *testing.T) {
	doc := readAsset(t, "prompts/review.md")
	var summary string
	for _, l := range strings.Split(doc, "\n") {
		if strings.HasPrefix(l, "<one-line summary:") {
			summary = l
			break
		}
	}
	if !strings.Contains(summary, "across 6 lenses") {
		t.Fatalf("comment summary line %q lacks \"across 6 lenses\"", summary)
	}
	for _, tok := range sixLensTokens {
		if !strings.Contains(summary, tok) {
			t.Errorf("comment summary line omits lens %q: %q", tok, summary)
		}
	}
}

func TestReviewCommandNamesSixLenses(t *testing.T) {
	doc := readAsset(t, "commands/dross-review.md")
	var desc string
	for _, l := range strings.Split(doc, "\n") {
		if strings.HasPrefix(l, "description:") {
			desc = l
			break
		}
	}
	if desc == "" {
		t.Fatal("commands/dross-review.md has no description line")
	}
	for _, tok := range sixLensTokens {
		if !strings.Contains(desc, tok) {
			t.Errorf("dross-review description omits lens %q: %q", tok, desc)
		}
	}
}

// localMode is review.md's `--diff` Local mode block, up to the next `## `.
func localMode(t *testing.T, doc string) string {
	t.Helper()
	return section(t, doc, "### Local mode — `--diff <patch> --out <dir>`")
}

// guardBefore fails unless sec holds guard, and holds it before the first
// occurrence of action.
func guardBefore(t *testing.T, name, sec, guard, action string) {
	t.Helper()
	g, a := strings.Index(sec, guard), strings.Index(sec, action)
	switch {
	case g < 0:
		t.Errorf("%s lost its --diff guard %q", name, guard)
	case a >= 0 && a < g:
		t.Errorf("%s reaches %q before its --diff guard", name, action)
	}
}

func TestReviewDiffModeNeverPosts(t *testing.T) {
	doc := readAsset(t, "prompts/review.md")
	cmd := readAsset(t, "commands/dross-review.md")
	for _, hint := range []string{"--diff <patch>", "--out <dir>"} {
		if !regexp.MustCompile(`(?m)^argument-hint: .*` + regexp.QuoteMeta(hint)).MatchString(cmd) {
			t.Errorf("dross-review argument-hint lacks %q", hint)
		}
	}
	local := localMode(t, doc)
	if !strings.Contains(local, "Skip §5 and §6's commit") {
		t.Error("Local mode no longer skips §5 and §6's commit")
	}
	for _, banned := range []string{"dross ship comment", "git commit", "git add"} {
		if strings.Contains(local, banned) {
			t.Errorf("Local mode names %q; --diff never posts or commits", banned)
		}
	}
	guardBefore(t, "§5", section(t, doc, "## 5."), "**`--diff` mode:** skip this section", "dross ship comment")
	guardBefore(t, "§6", section(t, doc, "## 6."), "**`--diff` mode:** no commit", "git commit")
}

func TestReviewDiffModeSkipsFetch(t *testing.T) {
	doc := readAsset(t, "prompts/review.md")
	if !strings.Contains(section(t, doc, "## 0."), "Required unless `--diff` is passed") {
		t.Error("§0 still requires a PR number in --diff mode")
	}
	local := localMode(t, doc)
	if !strings.Contains(local, "No PR number, no fetch:** skip §1") {
		t.Error("Local mode no longer replaces §1's fetch")
	}
	for _, banned := range []string{"<pr-number>", "gh pr diff", "curl"} {
		if strings.Contains(local, banned) {
			t.Errorf("Local mode names %q; --diff fetches no PR", banned)
		}
	}
	guardBefore(t, "§1", section(t, doc, "## 1."), "**`--diff` mode:** skip this section", "gh pr diff")
}

func TestReviewDiffModeWritesOutDir(t *testing.T) {
	doc := readAsset(t, "prompts/review.md")
	local := localMode(t, doc)
	for _, want := range []string{"`<diff-path>` is `<dir>/review-diff.patch`", "copy the patch to `<dir>/review-diff.patch`", "§4 saves to `<dir>/review-comment.md`"} {
		if !strings.Contains(local, want) {
			t.Errorf("Local mode no longer says %q", want)
		}
	}
	if strings.Contains(local, ".dross/phases") {
		t.Error("Local mode names a .dross/phases path; --diff writes only under --out")
	}
	// §4's fenced template holds its own `## /dross-review` heading, so match
	// the save sentence whole rather than slicing §4 at the next `## `.
	if !strings.Contains(doc, "Save the composed comment to `.dross/phases/<phase-id>/review-comment.md` for posterity — in `--diff` mode to `<dir>/review-comment.md` instead.") {
		t.Error("§4 no longer redirects the saved comment to the --out dir in --diff mode")
	}
	for _, lens := range []string{"security", "code quality", "test efficacy", "spec fidelity", "edge cases", "silent failures"} {
		tmpl := lensTemplate(t, doc, lens)
		if !strings.Contains(tmpl, "`<diff-path>`") || strings.Contains(tmpl, "review-diff.patch") {
			t.Errorf("%s template does not read the diff from `<diff-path>`", lens)
		}
	}
}

func TestReviewDiffModeRequiresOut(t *testing.T) {
	local := localMode(t, readAsset(t, "prompts/review.md"))
	if !strings.Contains(local, "**Refuse without `--out <dir>`:** print `--diff needs --out <dir>` and stop.") {
		t.Error("Local mode no longer refuses --diff without --out")
	}
}

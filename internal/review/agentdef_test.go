package review

import (
	"strings"
	"testing"

	"github.com/Rivil/dross/assets"
)

// agentDef returns the shipped reviewer definition's frontmatter (key → raw
// value) and body, read from the embedded assets the binary installs.
func agentDef(t *testing.T) (map[string]string, string) {
	t.Helper()
	b, err := assets.FS.ReadFile("agents/" + ReviewerAgent + ".md")
	if err != nil {
		t.Fatalf("the reviewer definition is not embedded: %v", err)
	}
	s := string(b)
	if !strings.HasPrefix(s, "---\n") {
		t.Fatal("the reviewer definition has no frontmatter")
	}
	end := strings.Index(s[4:], "\n---\n")
	if end < 0 {
		t.Fatal("the reviewer definition's frontmatter is never closed")
	}
	front := map[string]string{}
	for _, line := range strings.Split(s[4:4+end], "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok || strings.HasPrefix(line, " ") {
			continue
		}
		front[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return front, s[4+end+5:]
}

// TestReviewerDefinitionIsolation pins reviewer_isolation and c-5: CLAUDE.md
// omitted, no memory, the session's model, and read-only tools — a reviewer
// that could edit, run commands or spawn could change the tree it judges.
func TestReviewerDefinitionIsolation(t *testing.T) {
	front, _ := agentDef(t)
	if front["omitClaudeMd"] != "true" {
		t.Errorf("omitClaudeMd = %q, want true: the user's and project's CLAUDE.md would reach the reviewer", front["omitClaudeMd"])
	}
	if _, ok := front["memory"]; ok {
		t.Error("the definition sets memory: the reviewer must not see auto-memory")
	}
	if m, ok := front["model"]; ok && m != "inherit" {
		t.Errorf("the definition pins model %q: reviewer_model inherits the session's", m)
	}
	tools, ok := front["tools"]
	if !ok || strings.TrimSpace(tools) == "" {
		t.Fatal("the definition sets no tools, so the reviewer inherits every tool")
	}
	allowed := map[string]bool{"Read": true, "Grep": true, "Glob": true}
	for _, tool := range strings.Split(tools, ",") {
		tool = strings.TrimSpace(tool)
		if !allowed[tool] {
			t.Errorf("the reviewer has tool %q; only Read, Grep and Glob are allowed", tool)
		}
	}
	for _, banned := range []string{"Bash", "Edit", "Write", "MultiEdit", "NotebookEdit", "Agent", "Task", "WebFetch", "WebSearch"} {
		if strings.Contains(tools, banned) {
			t.Errorf("the reviewer's tools include %s", banned)
		}
	}
}

func TestReviewerNameMatchesRecorder(t *testing.T) {
	front, _ := agentDef(t)
	if front["name"] != ReviewerAgent {
		t.Fatalf("definition name %q != review.ReviewerAgent %q: the recorder would claim no spawn of it", front["name"], ReviewerAgent)
	}
}

// TestReviewerBodyMatchesParser: the worked example the reviewer copies is a
// verdict ParseVerdict accepts, carrying both kinds of finding.
func TestReviewerBodyMatchesParser(t *testing.T) {
	_, body := agentDef(t)
	v, err := ParseVerdict(body, KindTask)
	if err != nil {
		t.Fatalf("the body's worked example does not parse: %v", err)
	}
	if len(v.Spec) == 0 || len(v.Quality) == 0 {
		t.Fatalf("the worked example carries %d spec and %d quality findings, want at least one of each", len(v.Spec), len(v.Quality))
	}
	for _, want := range []string{"Spec findings always block", "**BLOCKING**", "**FLAG**", "**NOTE**", "exactly one fenced block whose info string is `dross-verdict`"} {
		if !strings.Contains(body, want) {
			t.Errorf("the body no longer says %q", want)
		}
	}
}

// TestReviewerBodyScopesSpecFindings pins c-2 as amended: a criterion split
// across tasks is judged only on this task's slice.
func TestReviewerBodyScopesSpecFindings(t *testing.T) {
	_, body := agentDef(t)
	for _, want := range []string{
		"Judge spec compliance only on the sub-surfaces this task's record claims for each covered criterion",
		"A sub-surface the record does not claim (owned by a sibling task) is not a finding",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the body no longer says %q", want)
		}
	}
}

// TestReviewerBodyLimitsReads pins c-5's "sees only": Read has no path limit,
// so the definition says what is off limits.
func TestReviewerBodyLimitsReads(t *testing.T) {
	_, body := agentDef(t)
	want := "Never read anything else: nothing under `.dross/` except the named context file, no `CLAUDE.md` or `AGENTS.md` anywhere, nothing under `~/.claude`, no memory files."
	if !strings.Contains(body, want) {
		t.Errorf("the body no longer limits the reviewer's reads:\nwant %q", want)
	}
}

// TestReviewerBodyDefinesGrades pins quality_blocking's list and the quick's
// spec source.
func TestReviewerBodyDefinesGrades(t *testing.T) {
	_, body := agentDef(t)
	for _, want := range []string{
		"**BLOCKING** — a correctness bug, an unmet test_contract, a locked-decision or rule violation, partial enum/status/flag handling, or an uncommented swallowed error or quiet fallback. Only these block.",
		"FLAG and NOTE are recorded only; they never block.",
		`For a quick, the stated description is the only spec source: cite criterion "description".`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the body no longer says %q", want)
		}
	}
}

// TestReviewerBodyWarnsOffNearMissFences: on a diff saturated with "dross
// verify", the reviewer twice fenced its verdict dross-verify — read as no
// verdict, so the task failed with a pass in hand. The body names the exact
// label and the near-misses it is not.
func TestReviewerBodyWarnsOffNearMissFences(t *testing.T) {
	_, body := agentDef(t)
	for _, want := range []string{"The info string is exactly `" + VerdictFence + "`", "not `dross-verify`", "recorded as unavailable"} {
		if !strings.Contains(body, want) {
			t.Errorf("the body does not say %q", want)
		}
	}
}

// TestParseVerdictRefusesNearMissFences: the label is a contract, not a hint —
// a near-miss is refused rather than read as a verdict.
func TestParseVerdictRefusesNearMissFences(t *testing.T) {
	const verdict = `{"verdict": "pass", "spec": [], "quality": []}`
	if _, err := ParseVerdict("ok\n\n```"+VerdictFence+"\n"+verdict+"\n```\n", KindTask); err != nil {
		t.Fatalf("the real fence was refused: %v", err)
	}
	for _, label := range []string{"dross-verify", "dross-review", "json"} {
		if _, err := ParseVerdict("ok\n\n```"+label+"\n"+verdict+"\n```\n", KindTask); err == nil {
			t.Errorf("a %s fence was read as a verdict", label)
		}
	}
}

// lensLines returns the lines starting with prefix (e.g. "> Rule: ") in
// review.md's edge-case and silent-failure lens templates, prefix stripped.
func lensLines(t *testing.T, prefix string) []string {
	t.Helper()
	b, err := assets.FS.ReadFile("prompts/review.md")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, lens := range []string{"edge cases", "silent failures"} {
		_, after, ok := strings.Cut(string(b), "**"+lens+"** — `subagent_type: general-purpose`\n")
		if !ok {
			t.Fatalf("review.md has no %q lens template", lens)
		}
		for _, l := range strings.Split(after, "\n") {
			if !strings.HasPrefix(l, ">") {
				break
			}
			if s, ok := strings.CutPrefix(l, prefix); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// reviewerLensSection is the body's partial-value-set / swallowed-error
// section, up to the next heading.
func reviewerLensSection(t *testing.T) string {
	t.Helper()
	_, body := agentDef(t)
	_, after, ok := strings.Cut(body, "## Partial value sets and swallowed errors\n")
	if !ok {
		t.Fatal("the reviewer body has no \"Partial value sets and swallowed errors\" section")
	}
	sec, _, _ := strings.Cut(after, "\n## ")
	return sec
}

// TestReviewerSharesLensRules pins c-7: the reviewer judges partial value
// sets and swallowed errors under the same rules as the two /dross-review
// lenses, sentence for sentence.
func TestReviewerSharesLensRules(t *testing.T) {
	rules := lensLines(t, "> Rule: ")
	if len(rules) < 3 {
		t.Fatalf("review.md's new lenses hold %d `> Rule:` lines, want at least 3: %q", len(rules), rules)
	}
	sec := reviewerLensSection(t)
	for _, r := range rules {
		if !strings.Contains(sec, r) {
			t.Errorf("the reviewer's lens section lacks the review.md rule %q", r)
		}
	}
}

// TestReviewerEdgeHitsBlock: a hit must fail the verdict. The lenses' severity
// rule (FLAG by default) would let a reviewer pass one, so it is never copied.
func TestReviewerEdgeHitsBlock(t *testing.T) {
	_, body := agentDef(t)
	sev := lensLines(t, "> Severity: ")
	if len(sev) == 0 {
		t.Fatal("review.md's new lenses hold no `> Severity:` line")
	}
	for _, s := range sev {
		if strings.Contains(body, s) {
			t.Errorf("the reviewer body copies the lens severity rule %q; a hit would grade FLAG and pass", s)
		}
	}
	if strings.Contains(body, "FLAG by default") {
		t.Error("the reviewer body grades a lens hit FLAG by default")
	}
	if !strings.Contains(reviewerLensSection(t), "a hit makes the verdict `\"block\"`") {
		t.Error("the reviewer's lens section no longer says a hit blocks the verdict")
	}
}

// TestReviewerSectionNoDrossReads: the lens section may send the reviewer to
// the working tree for a value set, never into .dross/ (verify.toml included).
func TestReviewerSectionNoDrossReads(t *testing.T) {
	sec := reviewerLensSection(t)
	for _, banned := range []string{"verify.toml", ".dross/", "spec.toml"} {
		if strings.Contains(sec, banned) {
			t.Errorf("the reviewer's lens section names %q; the read fence forbids .dross/", banned)
		}
	}
}

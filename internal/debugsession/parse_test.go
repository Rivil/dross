package debugsession

import (
	"reflect"
	"strings"
	"testing"
)

// sessionDoc renders a well-formed session whose header says status and whose
// fixed sections hold the given bodies; a section absent from bodies is empty.
func sessionDoc(status string, bodies map[string]string) string {
	var b strings.Builder
	b.WriteString("# Debug: flaky-login\n\nstatus: " + status + "\nopened: 2026-10-05T10:00:00Z\n")
	for _, h := range Headings() {
		b.WriteString("\n## " + h + "\n")
		b.WriteString(bodies[h])
	}
	return b.String()
}

func fixes(items ...string) map[string]string {
	return map[string]string{HeadingFixAttempts: "- " + strings.Join(items, "\n- ") + "\n"}
}

func repeat(item string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = item
	}
	return out
}

func noProblems(t *testing.T, s Session) {
	t.Helper()
	if len(s.Problems) != 0 {
		t.Fatalf("Problems = %q, want none", s.Problems)
	}
}

func TestReplanThreshold(t *testing.T) {
	cat := func(parts ...[]string) []string {
		var out []string
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	failed := func(n int) []string { return repeat("[failed] tried it, still flaky", n) }
	replan := []string{"[replan] the model was wrong; it is the clock"}

	cases := []struct {
		name   string
		items  []string
		state  State
		failed int
	}{
		{"two failures", failed(2), StateOpen, 2},
		{"three failures", failed(3), StateNeedsReplan, 3},
		{"four failures", failed(4), StateNeedsReplan, 4},
		{"three then replan", cat(failed(3), replan), StateOpen, 0},
		{"three, replan, two", cat(failed(3), replan, failed(2)), StateOpen, 2},
		{"three, replan, three", cat(failed(3), replan, failed(3)), StateNeedsReplan, 3},
		{"failures above the last replan never count", cat(failed(2), replan, failed(1), replan, failed(2)), StateOpen, 2},
		{"unmarked attempts do not count", cat(failed(2), []string{"tried a retry loop", "failed: no bracket"}), StateOpen, 2},
		{"an unclosed bracket is no marker", cat(failed(2), []string{"[failed no close"}), StateOpen, 2},
		{"spelling variants all count", []string{"[failed] a", "[FAILED] b", "[ Failed ] c"}, StateNeedsReplan, 3},
		{"every bullet kind counts", nil, StateNeedsReplan, 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bodies := fixes(c.items...)
			if c.items == nil {
				bodies = map[string]string{HeadingFixAttempts: "* [failed] a\n+ [failed] b\n1. [failed] c\n"}
			}
			s := Parse([]byte(sessionDoc("open", bodies)))
			noProblems(t, s)
			if s.FailedSinceReplan != c.failed || s.State() != c.state {
				t.Fatalf("FailedSinceReplan=%d State=%s, want %d %s", s.FailedSinceReplan, s.State(), c.failed, c.state)
			}
		})
	}
	for _, v := range []string{"[failed]", "[FAILED]", "[ Failed ]", "[\tfailed\t]"} {
		s := Parse([]byte(sessionDoc("open", fixes(v+" x"))))
		if s.FailedSinceReplan != 1 {
			t.Errorf("%q counted %d, want 1", v, s.FailedSinceReplan)
		}
	}
	for _, v := range []string{"[replan]", "[REPLAN]", "[ Replan ]"} {
		s := Parse([]byte(sessionDoc("open", fixes(append(failed(3), v+" x")...))))
		if s.FailedSinceReplan != 0 {
			t.Errorf("%q left %d failures, want 0", v, s.FailedSinceReplan)
		}
	}
}

func TestSectionScope(t *testing.T) {
	three := "- [failed] a\n- [failed] b\n- [failed] c\n"
	for _, h := range []string{HeadingSymptom, HeadingHypotheses, HeadingCurrentTheory, HeadingNextProbe, HeadingResolution, HeadingPrevention} {
		s := Parse([]byte(sessionDoc("open", map[string]string{h: three})))
		if s.FailedSinceReplan != 0 || s.State() != StateOpen {
			t.Errorf("three [failed] under ## %s: FailedSinceReplan=%d State=%s, want 0 open", h, s.FailedSinceReplan, s.State())
		}
	}
	s := Parse([]byte(sessionDoc("open", map[string]string{HeadingHypotheses: three})))
	if s.Hypotheses != 3 {
		t.Errorf("Hypotheses = %d, want 3", s.Hypotheses)
	}

	t.Run("a deeper heading keeps the section", func(t *testing.T) {
		s := Parse([]byte(sessionDoc("open", map[string]string{HeadingFixAttempts: "### Round one\n" + three})))
		noProblems(t, s)
		if s.State() != StateNeedsReplan {
			t.Fatalf("State = %s, want needs-replan", s.State())
		}
	})
	t.Run("a foreign ## heading ends the section", func(t *testing.T) {
		doc := sessionDoc("open", map[string]string{HeadingFixAttempts: "- [failed] a\n"}) + "\n## Notes\n" + three
		s := Parse([]byte(doc))
		noProblems(t, s)
		if s.FailedSinceReplan != 1 {
			t.Fatalf("FailedSinceReplan = %d, want 1", s.FailedSinceReplan)
		}
	})
	t.Run("the header counts nothing", func(t *testing.T) {
		doc := strings.Replace(sessionDoc("open", nil), "status: open\n", "status: open\n"+three, 1)
		s := Parse([]byte(doc))
		noProblems(t, s)
		if s.FailedSinceReplan != 0 || s.Hypotheses != 0 || s.Signals != 0 {
			t.Fatalf("header items counted: %+v", s)
		}
	})
}

func TestClosedHeaderWins(t *testing.T) {
	for _, st := range []State{StateResolved, StateAbandoned} {
		s := Parse([]byte(sessionDoc(string(st), fixes(repeat("[failed] x", 5)...))))
		noProblems(t, s)
		if s.State() != st || s.Status != st {
			t.Errorf("%s header with five failures: Status=%s State=%s", st, s.Status, s.State())
		}
	}
	if s := Parse([]byte(sessionDoc("Resolved", nil))); s.State() != StateResolved {
		t.Errorf("header value case: State = %s, want resolved", s.State())
	}
}

func TestParseImmunity(t *testing.T) {
	cases := []struct {
		name   string
		bodies map[string]string
	}{
		{"backtick fence", map[string]string{HeadingFixAttempts: "```\n- [failed] a\n- [failed] b\n- [failed] c\n```\n"}},
		{"tilde fence", map[string]string{HeadingFixAttempts: "~~~text\n- [failed] a\n- [failed] b\n- [failed] c\n~~~\n"}},
		{"a longer fence is not closed by a shorter run", map[string]string{HeadingFixAttempts: "````\n```\n- [failed] a\n- [failed] b\n- [failed] c\n````\n"}},
		{"a tilde fence is not closed by backticks", map[string]string{HeadingFixAttempts: "~~~\n```\n- [failed] a\n- [failed] b\n- [failed] c\n~~~\n"}},
		{"an indented fence inside an item", map[string]string{HeadingFixAttempts: "- tried a probe:\n  ```\n- [failed] a\n- [failed] b\n- [failed] c\n  ```\n"}},
		{"multi-line comment", map[string]string{HeadingFixAttempts: "<!--\n- [failed] a\n- [failed] b\n- [failed] c\n-->\n"}},
		{"comment opened mid-line", map[string]string{HeadingFixAttempts: "note <!-- start\n- [failed] a\n- [failed] b\n- [failed] c\nend --> done\n"}},
		{"indented sub-bullets", map[string]string{HeadingFixAttempts: "  - [failed] a\n\t- [failed] b\n    - [failed] c\n"}},
		{"a fenced ## Fix attempts is no heading", map[string]string{HeadingSymptom: "```\n## Fix attempts\n- [failed] a\n- [failed] b\n- [failed] c\n```\n"}},
		{"a commented ## Fix attempts is no heading", map[string]string{HeadingSymptom: "<!--\n## Fix attempts\n-->\n- [failed] a\n- [failed] b\n- [failed] c\n"}},
		{"a fence opened straight after a list marker", map[string]string{HeadingFixAttempts: "- ```\n- [failed] a\n- [failed] b\n- [failed] c\n  ```\n"}},
		{"a tilde fence may carry backticks in its info string", map[string]string{HeadingFixAttempts: "~~~ a`b\n- [failed] a\n- [failed] b\n- [failed] c\n~~~\n"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := Parse([]byte(sessionDoc("open", c.bodies)))
			noProblems(t, s)
			if s.FailedSinceReplan != 0 || s.State() != StateOpen {
				t.Fatalf("FailedSinceReplan=%d State=%s, want 0 open", s.FailedSinceReplan, s.State())
			}
		})
	}

	// The other direction: a line that only looks like a fence must not hide
	// the real items after it.
	notFences := []struct {
		name string
		body string
	}{
		{"a backtick info string holding a backtick is inline code", "```not `a` fence\n- [failed] a\n- [failed] b\n- [failed] c\n"},
		{"a comment opener in a fence's info string ends with the fence", "``` <!-- x\nlog\n```\n- [failed] a\n- [failed] b\n- [failed] c\n"},
		{"two backticks are no fence", "``\n- [failed] a\n- [failed] b\n- [failed] c\n``\n"},
		{"a fence marker inside a comment is commented out", "<!--\n```\n-->\n- [failed] a\n- [failed] b\n- [failed] c\n"},
		{"a backticked comment opener is code", "- [failed] stripped `<!--` from the log\n- [failed] b\n- [failed] c\n<!-- a later placeholder -->\n"},
		{"a double-backtick span may hold a single backtick", "- [failed] ``a ` <!-- b``\n- [failed] b\n- [failed] c\n<!-- a later placeholder -->\n"},
		{"a comment after a closed span still strips", "- [failed] `x` <!-- note -->\n- [failed] b\n- [failed] c\n"},
	}
	for _, c := range notFences {
		t.Run(c.name, func(t *testing.T) {
			s := Parse([]byte(sessionDoc("open", map[string]string{HeadingFixAttempts: c.body})))
			noProblems(t, s)
			if s.FailedSinceReplan != 3 {
				t.Fatalf("FailedSinceReplan = %d, want 3", s.FailedSinceReplan)
			}
		})
	}

	t.Run("an unmatched backtick does not shield a comment", func(t *testing.T) {
		body := "- [failed] a ` <!--\n- [failed] hidden\n- [failed] hidden too\n-->\n- [failed] b\n"
		s := Parse([]byte(sessionDoc("open", map[string]string{HeadingFixAttempts: body})))
		noProblems(t, s)
		if s.FailedSinceReplan != 2 {
			t.Fatalf("FailedSinceReplan = %d, want 2", s.FailedSinceReplan)
		}
	})

	t.Run("one hypothesis with two sub-bullets is one", func(t *testing.T) {
		s := Parse([]byte(sessionDoc("open", map[string]string{HeadingHypotheses: "- the clock skews\n  - seen at 02:00\n  - seen at 14:00\n"})))
		noProblems(t, s)
		if s.Hypotheses != 1 {
			t.Fatalf("Hypotheses = %d, want 1", s.Hypotheses)
		}
	})
	t.Run("fenced and commented signals do not count", func(t *testing.T) {
		s := Parse([]byte(sessionDoc("open", map[string]string{HeadingResolution: "```\n- repro gone\n- suite green\n```\n<!-- - ci green -->\n"})))
		noProblems(t, s)
		if s.Signals != 0 {
			t.Fatalf("Signals = %d, want 0", s.Signals)
		}
	})
	t.Run("a body status line leaves the header open", func(t *testing.T) {
		s := Parse([]byte(sessionDoc("open", map[string]string{HeadingSymptom: "status: resolved\n", HeadingResolution: "status: abandoned\n"})))
		noProblems(t, s)
		if s.State() != StateOpen {
			t.Fatalf("State = %s, want open", s.State())
		}
	})
	t.Run("a fenced header status line leaves the header open", func(t *testing.T) {
		doc := strings.Replace(sessionDoc("open", nil), "status: open\n", "status: open\n```\nstatus: resolved\n```\n<!-- status: abandoned -->\n", 1)
		s := Parse([]byte(doc))
		noProblems(t, s)
		if s.State() != StateOpen {
			t.Fatalf("State = %s, want open", s.State())
		}
	})
	t.Run("an indented status line is not the header's", func(t *testing.T) {
		doc := strings.Replace(sessionDoc("open", nil), "status: open\n", "status: open\n  status: resolved\n", 1)
		s := Parse([]byte(doc))
		noProblems(t, s)
		if s.State() != StateOpen {
			t.Fatalf("State = %s, want open", s.State())
		}
	})
}

func TestParseProblems(t *testing.T) {
	drop := func(doc, heading string) string { return strings.Replace(doc, "\n## "+heading+"\n", "\n", 1) }
	cases := []struct {
		name string
		doc  string
		want string // a substring of the one Problem expected
	}{
		{"unclosed backtick fence", sessionDoc("open", map[string]string{HeadingPrevention: "```\nlog\n"}), "``` fence is never closed"},
		{"unclosed tilde fence", sessionDoc("open", map[string]string{HeadingPrevention: "~~~~\n"}), "~~~~ fence is never closed"},
		{"unclosed comment", sessionDoc("open", map[string]string{HeadingPrevention: "<!-- todo\n"}), "comment is never closed"},
		{"missing heading", drop(sessionDoc("open", nil), HeadingResolution), `missing "## Resolution" heading`},
		{"duplicated heading", sessionDoc("open", map[string]string{HeadingPrevention: "## Resolution\n"}), `duplicate "## Resolution" heading`},
		{"duplicated heading, other spelling", sessionDoc("open", map[string]string{HeadingPrevention: "##   fix  ATTEMPTS\n"}), `duplicate "## Fix attempts" heading`},
		{"wrong-level heading", sessionDoc("open", map[string]string{HeadingNextProbe: "### Fix attempts\n"}), `"Fix attempts" is a level-3 heading; want "## Fix attempts"`},
		{"unrecognised status", sessionDoc("fixed", nil), `header status "fixed" is not open, resolved or abandoned`},
		{"derived state in the header", sessionDoc("needs-replan", nil), `header status "needs-replan"`},
		{"empty status", sessionDoc("", nil), `header status ""`},
		{"no status line", strings.Replace(sessionDoc("open", nil), "status: open\n", "", 1), `header has no "status:" line`},
		{"two status lines, open first", strings.Replace(sessionDoc("open", nil), "status: open\n", "status: open\nstatus: resolved\n", 1), `header carries 2 "status:" lines (lines 3, 4)`},
		{"two status lines, resolved first", strings.Replace(sessionDoc("resolved", nil), "status: resolved\n", "status: resolved\nStatus: abandoned\n", 1), `header carries 2 "status:" lines`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := Parse([]byte(c.doc))
			if len(s.Problems) != 1 || !strings.Contains(s.Problems[0], c.want) {
				t.Fatalf("Problems = %q, want exactly one containing %q", s.Problems, c.want)
			}
			if s.Status != StateOpen || s.State() != StateOpen {
				t.Fatalf("Status=%s State=%s, want open", s.Status, s.State())
			}
		})
	}

	t.Run("an unclosed fence hides the rest", func(t *testing.T) {
		s := Parse([]byte(sessionDoc("open", map[string]string{HeadingSymptom: "```\n"})))
		if len(s.Problems) != 7 {
			t.Fatalf("Problems = %q, want the fence plus six missing headings", s.Problems)
		}
	})
	t.Run("a duplicated heading's items do not count", func(t *testing.T) {
		doc := sessionDoc("open", map[string]string{HeadingResolution: "- repro gone\n"}) + "\n## Resolution\n- suite green\n"
		s := Parse([]byte(doc))
		if s.Signals != 1 || len(s.Problems) != 1 {
			t.Fatalf("Signals=%d Problems=%q, want 1 and the duplicate", s.Signals, s.Problems)
		}
	})
	t.Run("an empty file", func(t *testing.T) {
		s := Parse(nil)
		if s.State() != StateOpen || len(s.Problems) != 8 {
			t.Fatalf("State=%s Problems=%q, want open with seven missing headings and no status", s.State(), s.Problems)
		}
	})
}

func TestResolutionAndPrevention(t *testing.T) {
	signals := []struct {
		name string
		body string
		want int
	}{
		{"two distinct", "- repro gone\n- suite green\n", 2},
		{"whitespace-identical count once", "- repro gone\n-   repro\tgone  \n", 1},
		{"case still distinguishes", "- repro gone\n- Repro gone\n", 2},
		{"empty items count zero", "- \n-\n*\n1.\n", 0},
		{"no space after the bullet is prose", "-repro gone\n--suite green\n", 0},
		{"ordered items count", "1. repro gone\n2) suite green\n", 2},
		{"prose does not count", "the repro is gone and the suite is green\n", 0},
	}
	for _, c := range signals {
		t.Run("signals/"+c.name, func(t *testing.T) {
			s := Parse([]byte(sessionDoc("open", map[string]string{HeadingResolution: c.body})))
			noProblems(t, s)
			if s.Signals != c.want {
				t.Fatalf("Signals = %d, want %d", s.Signals, c.want)
			}
		})
	}

	prevention := []struct {
		name string
		body string
		want []string
	}{
		{"absent", "", nil},
		{"only a comment", "<!-- the rule that would have caught this -->\n", nil},
		{"only blanks", "\n   \n\t\n", nil},
		{"only an empty bullet", "- \n", nil},
		{"only an indented empty bullet", "  -\n", nil},
		{"only a fenced block", "```\nAlways pin the clock in tests\n```\n", nil},
		{"one prose line", "Always pin the clock in tests\n", []string{"Always pin the clock in tests"}},
		{"comment then prose", "<!-- placeholder -->\nAlways pin the clock\n", []string{"Always pin the clock"}},
		{"items kept with their markers", "- Always X\n- \n  - Never Y\n", []string{"- Always X", "- Never Y"}},
	}
	for _, c := range prevention {
		t.Run("prevention/"+c.name, func(t *testing.T) {
			s := Parse([]byte(sessionDoc("open", map[string]string{HeadingPrevention: c.body})))
			noProblems(t, s)
			if !reflect.DeepEqual(s.Prevention, c.want) {
				t.Fatalf("Prevention = %q, want %q", s.Prevention, c.want)
			}
		})
	}

	t.Run("a following heading ends Prevention", func(t *testing.T) {
		doc := sessionDoc("open", map[string]string{HeadingPrevention: "Always X\n"}) + "\n## Notes\nscratch\n"
		if s := Parse([]byte(doc)); !reflect.DeepEqual(s.Prevention, []string{"Always X"}) {
			t.Fatalf("Prevention = %q", s.Prevention)
		}
	})
}

func TestCRLFParsesLikeLF(t *testing.T) {
	docs := []string{
		sessionDoc("open", map[string]string{
			HeadingHypotheses:  "- the clock\n- the cache\n",
			HeadingFixAttempts: "- [failed] a\n```\n- [failed] fenced\n```\n- [failed] b\n- [failed] c\n",
			HeadingResolution:  "- repro gone\n- repro  gone\n- suite green\n",
			HeadingPrevention:  "<!-- placeholder\n-->\nAlways pin the clock\n",
		}),
		sessionDoc("resolved", map[string]string{HeadingSymptom: "```\nunclosed\n"}),
		strings.Replace(sessionDoc("open", nil), "status: open\n", "status: open\nstatus: open\n", 1),
		// A bare `##` ends Fix attempts only once its \r is gone: without the
		// normalisation the third failure would count.
		sessionDoc("open", map[string]string{HeadingFixAttempts: "- [failed] a\n- [failed] b\n##\n- [failed] c\n"}),
	}
	for i, lf := range docs {
		crlf := strings.ReplaceAll(lf, "\n", "\r\n")
		a, b := Parse([]byte(lf)), Parse([]byte(crlf))
		if !reflect.DeepEqual(a, b) {
			t.Errorf("doc %d: LF %+v\nCRLF %+v", i, a, b)
		}
	}
	if s := Parse([]byte(strings.ReplaceAll(docs[0], "\n", "\r\n"))); s.State() != StateNeedsReplan || s.Signals != 2 || s.Hypotheses != 2 {
		t.Fatalf("CRLF doc parsed as %+v", s)
	}
	if s := Parse([]byte(strings.ReplaceAll(docs[3], "\n", "\r\n"))); s.FailedSinceReplan != 2 {
		t.Fatalf("CRLF bare-## doc: FailedSinceReplan = %d, want 2", s.FailedSinceReplan)
	}
}

func TestHeadingsAreTheSpecOrder(t *testing.T) {
	want := []string{"Symptom", "Hypotheses", "Current theory", "Next probe", "Fix attempts", "Resolution", "Prevention"}
	got := Headings()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Headings() = %q, want %q", got, want)
	}
	got[0] = "Mutated"
	if Headings()[0] != "Symptom" {
		t.Fatal("Headings() shares its backing array")
	}
}

package debugsession

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

var closedAt = time.Date(2026, 10, 6, 9, 30, 0, 0, time.FixedZone("BST", 3600))

func TestFixedCloseGaps(t *testing.T) {
	const prose = "Always pin the clock in tests\n"
	resolution := func(n int) string {
		return []string{"", "- repro gone\n", "- repro gone\n- suite green\n"}[n]
	}
	cases := []struct {
		name string
		doc  string
		want []string
	}{
		{"one signal, empty Prevention", sessionDoc("open", map[string]string{HeadingResolution: resolution(1)}),
			[]string{"Resolution lists 1 of 2 signals", "Prevention is empty"}},
		{"one signal, prose", sessionDoc("open", map[string]string{HeadingResolution: resolution(1), HeadingPrevention: prose}),
			[]string{"Resolution lists 1 of 2 signals"}},
		{"two signals, empty Prevention", sessionDoc("open", map[string]string{HeadingResolution: resolution(2)}),
			[]string{"Prevention is empty"}},
		{"two signals, placeholder comment", sessionDoc("open", map[string]string{HeadingResolution: resolution(2), HeadingPrevention: "<!-- placeholder -->\n"}),
			[]string{"Prevention is empty"}},
		{"two signals, whitespace", sessionDoc("open", map[string]string{HeadingResolution: resolution(2), HeadingPrevention: "  \n\t\n"}),
			[]string{"Prevention is empty"}},
		{"two signals, prose", sessionDoc("open", map[string]string{HeadingResolution: resolution(2), HeadingPrevention: prose}),
			nil},
		{"fenced signals do not count", sessionDoc("open", map[string]string{HeadingResolution: "```\n- repro gone\n- suite green\n```\n", HeadingPrevention: prose}),
			[]string{"Resolution lists 0 of 2 signals"}},
		{"one signal pasted twice", sessionDoc("open", map[string]string{HeadingResolution: "- repro gone\n-  repro  gone\n", HeadingPrevention: prose}),
			[]string{"Resolution lists 1 of 2 signals"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := FixedCloseGaps(Parse([]byte(c.doc))); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("gaps = %q, want %q", got, c.want)
			}
		})
	}

	t.Run("a missing Resolution heading is named", func(t *testing.T) {
		doc := strings.Replace(sessionDoc("open", map[string]string{HeadingPrevention: prose}), "\n## Resolution\n", "\n", 1)
		gaps := FixedCloseGaps(Parse([]byte(doc)))
		if len(gaps) != 2 || !strings.Contains(gaps[0], `missing "## Resolution" heading`) || gaps[1] != "Resolution lists 0 of 2 signals" {
			t.Fatalf("gaps = %q", gaps)
		}
	})

	t.Run("a duplicated Resolution never makes two signals", func(t *testing.T) {
		doc := sessionDoc("open", map[string]string{HeadingResolution: "- repro gone\n", HeadingPrevention: prose}) + "\n## Resolution\n- suite green\n"
		gaps := FixedCloseGaps(Parse([]byte(doc)))
		if len(gaps) != 2 || !strings.Contains(gaps[0], `the session is malformed: `) || !strings.Contains(gaps[0], `duplicate "## Resolution" heading`) ||
			gaps[1] != "Resolution lists 1 of 2 signals" {
			t.Fatalf("gaps = %q", gaps)
		}
	})

	t.Run("every Problem is a gap", func(t *testing.T) {
		doc := sessionDoc("fixed", map[string]string{HeadingResolution: resolution(2), HeadingPrevention: prose + "<!-- unclosed\n"})
		s := Parse([]byte(doc))
		gaps := FixedCloseGaps(s)
		if len(s.Problems) != 2 || len(gaps) != len(s.Problems) {
			t.Fatalf("Problems %q, gaps %q: want one gap per Problem", s.Problems, gaps)
		}
		for i, p := range s.Problems {
			if gaps[i] != "the session is malformed: "+p {
				t.Errorf("gap %d = %q, want it to name %q", i, gaps[i], p)
			}
		}
	})
}

// body is everything from the first `## ` heading on.
func body(t *testing.T, b []byte) []byte {
	t.Helper()
	i := bytes.Index(b, []byte("\n## "))
	if i < 0 {
		t.Fatalf("no ## heading in %q", b)
	}
	return b[i:]
}

func TestCloseTextHeaderOnly(t *testing.T) {
	doc := sessionDoc("open", map[string]string{
		HeadingSymptom:     "status: open\n```\nstatus: open\n```\n",
		HeadingFixAttempts: "- [failed] a\n- [failed] b\n- [failed] c\n",
	})
	for _, eol := range []string{"\n", "\r\n"} {
		in := []byte(strings.ReplaceAll(doc, "\n", eol))
		for _, to := range []State{StateResolved, StateAbandoned} {
			out, err := CloseText(in, to, "gave up", closedAt)
			if err != nil {
				t.Fatalf("%q close to %s: %v", eol, to, err)
			}
			if !bytes.Equal(body(t, out), body(t, in)) {
				t.Fatalf("%q close to %s touched the body", eol, to)
			}
			s := Parse(out)
			if s.State() != to || len(s.Problems) != 0 {
				t.Fatalf("%q close to %s: State=%s Problems=%q", eol, to, s.State(), s.Problems)
			}
			head := string(out[:len(out)-len(body(t, out))])
			want := "status: " + string(to) + eol + "closed: 2026-10-06T08:30:00Z" + eol
			if to == StateAbandoned {
				want += "reason: gave up" + eol
			}
			if !strings.Contains(head, want) {
				t.Fatalf("%q header %q lacks %q", eol, head, want)
			}
			if to == StateResolved && strings.Contains(head, "reason:") {
				t.Fatalf("a resolved close wrote a reason: %q", head)
			}
			if strings.Count(head, "status:") != 1 {
				t.Fatalf("header %q carries more than one status line", head)
			}
		}
	}

	t.Run("an unrecognised status can still be closed", func(t *testing.T) {
		out, err := CloseText([]byte(sessionDoc("fixed", nil)), StateAbandoned, "x", closedAt)
		if err != nil || Parse(out).State() != StateAbandoned {
			t.Fatalf("CloseText = %v, State %s", err, Parse(out).State())
		}
	})

	refusals := []struct {
		name string
		doc  string
		to   State
		want string
	}{
		{"already resolved", sessionDoc("resolved", nil), StateResolved, "invalid close: the session is already resolved"},
		{"already abandoned", sessionDoc("abandoned", nil), StateResolved, "invalid close: the session is already abandoned"},
		{"no status line", strings.Replace(sessionDoc("open", nil), "status: open\n", "", 1), StateResolved, `has no "status:" line`},
		{"only a fenced status line", strings.Replace(sessionDoc("open", nil), "status: open\n", "```\nstatus: open\n```\n", 1), StateResolved, `has no "status:" line`},
		{"two status lines", strings.Replace(sessionDoc("open", nil), "status: open\n", "status: open\nstatus: open\n", 1), StateResolved, `carries 2 "status:" lines`},
		{"a comment closing on the status line", strings.Replace(sessionDoc("open", nil), "status: open\n", "<!-- note\n-->status: open\n", 1), StateResolved, "shares a line with a comment marker"},
		{"open is no close", sessionDoc("open", nil), StateOpen, "invalid close state"},
		{"needs-replan is no close", sessionDoc("open", nil), StateNeedsReplan, "invalid close state"},
	}
	for _, c := range refusals {
		t.Run(c.name, func(t *testing.T) {
			in := []byte(c.doc)
			keep := append([]byte(nil), in...)
			out, err := CloseText(in, c.to, "x", closedAt)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
			if !bytes.Equal(out, keep) || !bytes.Equal(in, keep) {
				t.Fatal("a refused close changed the text")
			}
		})
	}
}

func TestCloseTextReason(t *testing.T) {
	in := []byte(sessionDoc("open", nil))
	keep := append([]byte(nil), in...)
	for _, reason := range []string{"\n## Resolution\n- x", "gave up\nstatus: open", "gave up\rstatus: open", "a\r", "gave up <!-- see notes", "", "  \t "} {
		out, err := CloseText(in, StateAbandoned, reason, closedAt)
		if err == nil {
			t.Errorf("reason %q accepted", reason)
		}
		if !bytes.Equal(out, keep) || !bytes.Equal(in, keep) {
			t.Errorf("reason %q: the input was not returned unchanged", reason)
		}
	}
	out, err := CloseText(in, StateAbandoned, "  gave up  ", closedAt)
	if err != nil || !bytes.Contains(out, []byte("\nreason: gave up\n")) {
		t.Fatalf("a padded one-line reason: %v, %q", err, out)
	}
	if _, err := CloseText(in, StateResolved, "", closedAt); err != nil {
		t.Fatalf("a resolved close needs no reason: %v", err)
	}
}

func prevention(body string) Session {
	return Parse([]byte(sessionDoc("open", map[string]string{HeadingPrevention: body})))
}

func TestRuleAddFlatten(t *testing.T) {
	cases := []struct {
		name, body, want string
	}{
		{"a placeholder comment is dropped", "<!-- the rule that would have caught this -->\nAlways X\n", "Always X"},
		{"a fenced block is dropped", "```\nrm -rf build\n```\nAlways X\n", "Always X"},
		{"list markers are stripped and joined", "- Always X\n- Never Y\n", "Always X; Never Y"},
		{"ordered and nested items too", "1. Always X\n  - Never Y\n2) Then Z\n", "Always X; Never Y; Then Z"},
		{"whitespace runs collapse", "Always   X\twhen Y \n", "Always X when Y"},
		{"prose lines are joined", "Always X\nwhen Y\n", "Always X; when Y"},
		{"nothing", "<!-- placeholder -->\n", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := RuleText(prevention(c.body)); got != c.want {
				t.Fatalf("RuleText = %q, want %q", got, c.want)
			}
		})
	}
	if got := RuleAddCommand(prevention("<!-- placeholder -->\n")); got != "" {
		t.Fatalf("RuleAddCommand on an empty Prevention = %q, want none", got)
	}
}

func TestRuleAddCommandShellOracle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX /bin/sh")
	}
	s := prevention(strings.Join([]string{
		`Always quote ' and " before $(touch pwned) or ` + "`touch pwned`" + ` runs!`,
		`Never trust a back\slash path or ${HOME}`,
		`--force is never the fix`,
		`- one`,
		`- two`,
		`- three`,
	}, "\n") + "\n")
	want := `Always quote ' and " before $(touch pwned) or ` + "`touch pwned`" + ` runs!; Never trust a back\slash path or ${HOME}; --force is never the fix; one; two; three`
	if got := RuleText(s); got != want {
		t.Fatalf("RuleText = %q, want %q", got, want)
	}
	line := RuleAddCommand(s)
	if !strings.HasPrefix(line, "dross rule add -- '") || strings.ContainsAny(line, "\r\n") {
		t.Fatalf("RuleAddCommand = %q", line)
	}

	dir := t.TempDir()
	script := `dross() { for a in "$@"; do printf '%s\n' "$a"; done; }; ` + line
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("sh -c %q: %v", script, err)
	}
	got := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if wantArgv := []string{"rule", "add", "--", want}; !reflect.DeepEqual(got, wantArgv) {
		t.Fatalf("the shell read argv %q, want %q", got, wantArgv)
	}
	if _, err := os.Stat(filepath.Join(dir, "pwned")); !os.IsNotExist(err) {
		t.Fatal("the printed line ran a command substitution")
	}
}

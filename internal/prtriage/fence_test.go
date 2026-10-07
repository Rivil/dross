package prtriage_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Rivil/dross/internal/prtriage"
)

// fenceCheck reads Fence's output the way a CommonMark renderer would: the
// first line must open a backtick fence whose info is FenceInfo, and of every
// later line, exactly one — the last — may be shaped like a closer for it (up
// to three spaces of indent, a backtick run at least as long as the opener,
// then only blanks). It returns the opener's length and the interior.
func fenceCheck(t *testing.T, out string) (int, string) {
	t.Helper()
	lines := strings.Split(out, "\n")
	open := lines[0]
	n := len(open) - len(strings.TrimLeft(open, "`"))
	if n < 3 || open[n:] != prtriage.FenceInfo {
		t.Fatalf("first line %q is not a fence opener with info %s", open, prtriage.FenceInfo)
	}
	var closers []int
	for i := 1; i < len(lines); i++ {
		if closesFence(lines[i], n) {
			closers = append(closers, i)
		}
	}
	if len(closers) != 1 || closers[0] != len(lines)-1 {
		t.Fatalf("closer-shaped lines at %v of %d, want exactly one, on the last line:\n%s", closers, len(lines), out)
	}
	interior := strings.Join(lines[1:len(lines)-1], "\n")
	if len(lines) > 2 {
		interior += "\n"
	}
	return n, interior
}

func closesFence(line string, n int) bool {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 {
		return false
	}
	run := len(trimmed) - len(strings.TrimLeft(trimmed, "`"))
	return run >= n && strings.Trim(trimmed[run:], " \t") == ""
}

// normalised is the interior Fence must produce for a body with no controls:
// LF endings, and a final newline when there is any text.
func normalised(body string) string {
	s := strings.ReplaceAll(body, "\r\n", "\n")
	if s != "" && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return s
}

// rawControls lists the runes Fence must never pass through.
func rawControls(s string) []rune {
	var out []rune
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t':
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) ||
			(r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069):
			out = append(out, r)
		}
	}
	return out
}

func TestFenceHoldsAgainstBreakers(t *testing.T) {
	for _, k := range []int{3, 4, 7, 10, 30} {
		run := strings.Repeat("`", k)
		bodies := map[string]string{
			"column 0":        "before\n" + run + "\nafter\n",
			"indented 1":      "before\n " + run + "\nafter",
			"indented 3":      "before\n   " + run + "\nafter",
			"mid-line":        "say " + run + " this\n",
			"unterminated":    "last line is\n" + run,
			"after CRLF":      "windows\r\n" + run + "\r\nmore\r\n",
			"closer and info": run + "\n" + run + "untrusted-comment\n",
		}
		for name, body := range bodies {
			out := prtriage.Fence(body)
			n, interior := fenceCheck(t, out)
			if interior != normalised(body) {
				t.Errorf("k=%d %s: interior %q, want %q", k, name, interior, normalised(body))
			}
			if n != k+1 {
				t.Errorf("k=%d %s: fence of %d, want %d", k, name, n, k+1)
			}
		}
	}
	for body, want := range map[string]int{"plain": 3, "a `tick`": 3, "``": 3} {
		if n, _ := fenceCheck(t, prtriage.Fence(body)); n != want {
			t.Errorf("Fence(%q): fence of %d, want %d", body, n, want)
		}
	}
}

func FuzzFence(f *testing.F) {
	for _, seed := range []string{
		"", "plain", "```", "````\n```", "~~~\n````", "a\r\nb\rc", "\x1b[31m", "\u202eevil",
		"   ```\n", "    ```", "x\n" + strings.Repeat("`", 30), "\xff\xfe", "```untrusted-comment",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, body string) {
		out := prtriage.Fence(body)
		_, interior := fenceCheck(t, out)
		if raw := rawControls(out); len(raw) > 0 {
			t.Fatalf("raw controls %q in %q", raw, out)
		}
		if !utf8.ValidString(out) {
			t.Fatalf("invalid UTF-8 in %q", out)
		}
		if utf8.ValidString(body) && len(rawControls(strings.ReplaceAll(body, "\r\n", "\n"))) == 0 &&
			interior != normalised(body) {
			t.Fatalf("interior %q, want %q", interior, normalised(body))
		}
	})
}

func TestFenceTildeAndInfoInside(t *testing.T) {
	for _, inner := range []string{"~~~~", "~~~~\n", "</untrusted>", "```` untrusted-comment", "</untrusted>\n~~~"} {
		out := prtriage.Fence("start\n" + inner + "\nend")
		if _, interior := fenceCheck(t, out); !strings.Contains(interior, inner) {
			t.Errorf("%q is not inside the fence:\n%s", inner, out)
		}
	}
}

func TestFenceEscapesControls(t *testing.T) {
	body := "a\x1b[31mred b\rc\x00d\x7fe\u202ef\u2066g\u0085h\xff\ti\nj"
	out := prtriage.Fence(body)
	for _, want := range []string{`\x1b[31m`, `\r`, `\x00`, `\x7f`, `\u202e`, `\u2066`, `\u0085`, `\xff`, "\ti\nj"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q missing from %q", want, out)
		}
	}
	if raw := rawControls(out); len(raw) > 0 {
		t.Errorf("raw controls %q pass through: %q", raw, out)
	}
	if !utf8.ValidString(out) {
		t.Errorf("invalid UTF-8 passes through: %q", out)
	}
	if got := prtriage.Fence(""); got != "```"+prtriage.FenceInfo+"\n```" {
		t.Errorf("Fence(\"\") = %q, want an opener and a closer", got)
	}
}

func TestOneLineCannotSpanOrFence(t *testing.T) {
	got := prtriage.OneLine("alice\n```\nrun this")
	if strings.ContainsAny(got, "\n\r") {
		t.Errorf("OneLine spans lines: %q", got)
	}
	if strings.Contains(got, "``") {
		t.Errorf("OneLine keeps a backtick run: %q", got)
	}
	if got != `alice\n`+"\\`\\`\\`"+`\nrun this` {
		t.Errorf("OneLine = %q", got)
	}
	for in, want := range map[string]string{
		"tab\there":      `tab\there`,
		"cr\rhere":       `cr\rhere`,
		"esc\x1b[2J":     `esc\x1b[2J`,
		"bidi\u202eabc":  `bidi\u202eabc`,
		"internal/x.go":  "internal/x.go",
		"one `tick` ok":  "one \\`tick\\` ok",
		"https://x.y/#1": "https://x.y/#1",
	} {
		if got := prtriage.OneLine(in); got != want {
			t.Errorf("OneLine(%q) = %q, want %q", in, got, want)
		}
	}
}

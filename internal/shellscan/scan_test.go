package shellscan

import (
	"reflect"
	"strings"
	"testing"
)

const cwd = "/work/repo"

func scan(line string) Script { return Scan(line, Options{Dir: cwd, Home: "/home/u"}) }

// names lists each command's program, "" for a bare assignment/redirection.
func names(s Script) []string {
	out := make([]string, len(s.Commands))
	for i, c := range s.Commands {
		out[i] = c.Name()
	}
	return out
}

// TestHeredocCommitIsOneCommand pins Claude Code's own commit shape: the
// message body sits in a quoted heredoc inside a command substitution, and
// nothing in it — not a `)`, not an operator, not a secret-path read — may
// surface as a command or an operand.
func TestHeredocCommitIsOneCommand(t *testing.T) {
	line := "git commit -m \"$(cat <<'EOF'\n" +
		"feat(gate): add things (and more)\n" +
		"\n" +
		"stop cat .env 2>&1; a && b\n" +
		"$(pass-cli item view x) `cat id_rsa`\n" +
		"EOF\n" +
		")\""
	s := scan(line)
	if s.Partial {
		t.Fatalf("Partial = true (%s), want a clean scan", s.Problem)
	}
	if got, want := names(s), []string{"git", "cat"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("commands = %q, want %q", got, want)
	}
	g, ok := s.Commands[0].Git()
	if !ok || g.Sub != "commit" || s.Commands[0].Substituted {
		t.Errorf("first command = %+v (git %+v), want a top-level git commit", s.Commands[0], g)
	}
	cat := s.Commands[1]
	if len(cat.Args()) != 0 || len(cat.Inputs()) != 0 {
		t.Errorf("nested cat args=%q inputs=%q, want no file operand", cat.Args(), cat.Inputs())
	}
	if !cat.Substituted || cat.Parent != 0 {
		t.Errorf("nested cat Substituted=%v Parent=%d, want true/0", cat.Substituted, cat.Parent)
	}

	// The same shape with the delimiter run into the closing paren, which
	// bash also accepts inside $( … ).
	s = scan("git commit -m \"$(cat <<'EOF'\nmsg; cat .env\nEOF)\"")
	if s.Partial || !reflect.DeepEqual(names(s), []string{"git", "cat"}) {
		t.Errorf("EOF) form: commands = %q partial=%v (%s)", names(s), s.Partial, s.Problem)
	}
}

// TestUnquotedHeredocRunsSubstitutions: an unquoted delimiter expands its
// body, so a substitution in it really runs — and is reported.
func TestUnquotedHeredocRunsSubstitutions(t *testing.T) {
	s := scan("cat <<EOF\nplain text; rm -rf x\n$(pass-cli item view x)\nEOF\necho done")
	if got, want := names(s), []string{"cat", "pass-cli", "echo"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("commands = %q, want %q", got, want)
	}
	if pc := s.Commands[1]; !pc.Substituted || pc.Stdout != Capture {
		t.Errorf("heredoc substitution = %+v, want Substituted with stdout captured", pc)
	}
}

func TestSplit(t *testing.T) {
	cases := []struct {
		line  string
		names []string
		ops   []string
	}{
		{"a && b; c | d || e", []string{"a", "b", "c", "d", "e"}, []string{"", "&&", ";", "|", "||"}},
		{`echo "a && b"`, []string{"echo"}, []string{""}},
		{`echo 'x; y'`, []string{"echo"}, []string{""}},
		{"a & b", []string{"a", "b"}, []string{"", "&"}},
		{"a\nb", []string{"a", "b"}, []string{"", "\n"}},
		{"a &&\n b", []string{"a", "b"}, []string{"", "&&"}},
		{"a |& b", []string{"a", "b"}, []string{"", "|&"}},
		{`echo a\;b`, []string{"echo"}, []string{""}},
		{"echo x # cat .env; rm y", []string{"echo"}, []string{""}},
		{"if a; then b; else c; fi; d", []string{"a", "b", "c", "d"}, []string{"", ";", ";", ";"}},
		{"for f in a b; do cat $f; done | sort", []string{"cat", "sort"}, []string{";", "|"}},
		{"while read l; do echo $l; done < in.txt", []string{"read", "echo"}, []string{"", ";"}},
		{"case $x in a) b ;; (c|d) e ;; esac; f", []string{"b", "e", "f"}, []string{"", "", ";"}},
		{"f() { a; }; f", []string{"a", "f"}, []string{"", ";"}},
	}
	for _, c := range cases {
		t.Run(c.line, func(t *testing.T) {
			s := scan(c.line)
			if s.Partial {
				t.Fatalf("Partial = true (%s)", s.Problem)
			}
			if got := names(s); !reflect.DeepEqual(got, c.names) {
				t.Fatalf("commands = %q, want %q", got, c.names)
			}
			var ops []string
			for _, cmd := range s.Commands {
				ops = append(ops, cmd.Op)
			}
			if !reflect.DeepEqual(ops, c.ops) {
				t.Errorf("ops = %q, want %q", ops, c.ops)
			}
		})
	}
}

// TestStreamDestinations resolves fd1 and fd2 per command, in redirection
// order, through pipes and enclosing groups.
func TestStreamDestinations(t *testing.T) {
	type dests struct{ out, err Dest }
	cases := []struct {
		line string
		want map[string]dests // by program name
	}{
		{"x", map[string]dests{"x": {Transcript, Transcript}}},
		{"x | y", map[string]dests{"x": {Pipe, Transcript}, "y": {Transcript, Transcript}}},
		{"x >f", map[string]dests{"x": {File, Transcript}}},
		{"x >f 2>/dev/null", map[string]dests{"x": {File, Null}}},
		{"x >f 2>&1", map[string]dests{"x": {File, File}}},
		{"x &>f", map[string]dests{"x": {File, File}}},
		{"x &>>f", map[string]dests{"x": {File, File}}},
		{"x >&f", map[string]dests{"x": {File, File}}},
		{"x 2>&1 >f", map[string]dests{"x": {File, Transcript}}},
		{"x |& y", map[string]dests{"x": {Pipe, Pipe}, "y": {Transcript, Transcript}}},
		{"x 2>/dev/null |& y", map[string]dests{"x": {Pipe, Pipe}}},
		{"x >f |& y", map[string]dests{"x": {File, File}}},
		{"x 2>&1 | y", map[string]dests{"x": {Pipe, Pipe}}},
		{"x >/dev/null 2>&-", map[string]dests{"x": {Null, Null}}},
		{"x 3>&1 1>f 2>&3", map[string]dests{"x": {File, Transcript}}},
		{"x >f 2>/dev/stdout", map[string]dests{"x": {File, File}}},
		{"( pass-cli view ) 2>&1", map[string]dests{"pass-cli": {Transcript, Transcript}}},
		{"( pass-cli view ) >f 2>/dev/null", map[string]dests{"pass-cli": {File, Null}}},
		{"( pass-cli view >f 2>/dev/null ) 2>&1", map[string]dests{"pass-cli": {File, Null}}},
		{"{ a; b; } | c", map[string]dests{"a": {Pipe, Transcript}, "b": {Pipe, Transcript}}},
		{"for i in 1; do pass-cli view; done >f 2>/dev/null", map[string]dests{"pass-cli": {File, Null}}},
		{`echo "2>&1"`, map[string]dests{"echo": {Transcript, Transcript}}},
		{"pass-cli x |& head", map[string]dests{"pass-cli": {Pipe, Pipe}, "head": {Transcript, Transcript}}},
		// A substitution's stdout is captured; its stderr is the shell's, not
		// the outer command's.
		{`echo "$(pass-cli x)" 2>/dev/null`, map[string]dests{"echo": {Transcript, Null}, "pass-cli": {Capture, Transcript}}},
		{`( echo "$(pass-cli x)" ) 2>/dev/null`, map[string]dests{"pass-cli": {Capture, Null}}},
		{"diff <(pass-cli x) f", map[string]dests{"pass-cli": {Capture, Transcript}}},
	}
	for _, c := range cases {
		t.Run(c.line, func(t *testing.T) {
			s := scan(c.line)
			if s.Partial {
				t.Fatalf("Partial = true (%s)", s.Problem)
			}
			for name, want := range c.want {
				var found *Command
				for i := range s.Commands {
					if s.Commands[i].Name() == name {
						found = &s.Commands[i]
					}
				}
				if found == nil {
					t.Fatalf("no command %q in %q", name, names(s))
				}
				if got := (dests{found.Stdout, found.Stderr}); got != want {
					t.Errorf("%s: stdout,stderr = %v,%v want %v,%v", name, got.out, got.err, want.out, want.err)
				}
			}
		})
	}
}

func TestRedirectsAreNotWords(t *testing.T) {
	s := scan(`echo "2>&1" 2>&1 a2>f`)
	c := s.Commands[0]
	if want := []string{"echo", "2>&1", "a2"}; !reflect.DeepEqual(c.Argv, want) {
		t.Errorf("argv = %q, want %q", c.Argv, want)
	}
	want := []Redirect{{Fd: 2, Op: ">&", Target: "1"}, {Fd: 1, Op: ">", Target: "f"}}
	if !reflect.DeepEqual(c.Redirects, want) {
		t.Errorf("redirects = %+v, want %+v", c.Redirects, want)
	}
	if got := c.Outputs(); !reflect.DeepEqual(got, []string{"f"}) {
		t.Errorf("outputs = %q, want [f]", got)
	}
}

func TestPrefixStripping(t *testing.T) {
	cases := []struct {
		line    string
		argv    []string
		program string
		assigns []string
	}{
		{"FOO=1 env BAR=2 /usr/bin/pass-cli item view x", []string{"pass-cli", "item", "view", "x"}, "/usr/bin/pass-cli", []string{"FOO=1", "BAR=2"}},
		{"env -i -u HOME pass-cli a", []string{"pass-cli", "a"}, "pass-cli", nil},
		{"sudo -u root -E nohup time -p command pass-cli a", []string{"pass-cli", "a"}, "pass-cli", nil},
		{"sudo FOO=1 pass-cli a", []string{"pass-cli", "a"}, "pass-cli", []string{"FOO=1"}},
		{`\pass-cli a`, []string{"pass-cli", "a"}, "pass-cli", nil},
		{`"pass-cli" a`, []string{"pass-cli", "a"}, "pass-cli", nil},
		// command -v looks a name up; it does not run it.
		{"command -v pass-cli", []string{"command", "-v", "pass-cli"}, "command", nil},
		// env -S re-splits a string: opaque, left as written.
		{`env -S "pass-cli a"`, []string{"env", "-S", "pass-cli a"}, "env", nil},
		{"FOO=1", nil, "", []string{"FOO=1"}},
		{"X=$(pass-cli a)", nil, "", []string{"X=$(pass-cli a)"}},
		{`"FOO=1" x`, []string{"FOO=1", "x"}, "FOO=1", nil},
	}
	for _, c := range cases {
		t.Run(c.line, func(t *testing.T) {
			s := scan(c.line)
			if len(s.Commands) == 0 {
				t.Fatalf("no commands")
			}
			got := s.Commands[0]
			if !reflect.DeepEqual(got.Argv, c.argv) || got.Program != c.program || !reflect.DeepEqual(got.Assigns, c.assigns) {
				t.Errorf("argv=%q program=%q assigns=%q, want %q %q %q", got.Argv, got.Program, got.Assigns, c.argv, c.program, c.assigns)
			}
		})
	}
}

func TestDirAndInput(t *testing.T) {
	s := scan("cd sub && git -C ../other commit -m x")
	if len(s.Commands) != 2 {
		t.Fatalf("commands = %q", names(s))
	}
	g, ok := s.Commands[1].Git()
	if !ok || g.Dir != "/work/repo/other" || g.Sub != "commit" || !reflect.DeepEqual(g.Args, []string{"-m", "x"}) {
		t.Errorf("git call = %+v ok=%v, want dir <cwd>/other, sub commit, args [-m x]", g, ok)
	}
	if got := s.Commands[1].Dir; got != "/work/repo/sub" {
		t.Errorf("shell dir after cd = %q, want /work/repo/sub", got)
	}

	s = scan("git -c user.name=x -C a -C b --no-pager commit")
	if g, _ := s.Commands[0].Git(); g.Dir != "/work/repo/a/b" || g.Sub != "commit" {
		t.Errorf("chained -C = %+v, want dir /work/repo/a/b sub commit", g)
	}

	s = scan("cat < .env")
	if got := s.Commands[0].Inputs(); !reflect.DeepEqual(got, []string{".env"}) {
		t.Errorf("inputs = %q, want [.env]", got)
	}

	s = scan("cd x && cat .env; (cd y; cat a); cat b; cd; cat c; cd ~/d && cat e")
	want := map[string]string{".env": "/work/repo/x/.env", "a": "/work/repo/x/y/a", "b": "/work/repo/x/b", "c": "/home/u/c", "e": "/home/u/d/e"}
	for _, c := range s.Commands {
		if c.Name() != "cat" {
			continue
		}
		arg := c.Args()[0]
		if got := c.Resolve(arg); got != want[arg] {
			t.Errorf("cat %s resolves to %q, want %q", arg, got, want[arg])
		}
	}

	s = scan(`echo x > ~/.claude/f; echo y > "$HOME/g"; echo z > '~/h'`)
	var outs []string
	for _, c := range s.Commands {
		outs = append(outs, c.Outputs()...)
	}
	if want := []string{"/home/u/.claude/f", "/home/u/g", "~/h"}; !reflect.DeepEqual(outs, want) {
		t.Errorf("outputs = %q, want %q (a quoted ~ stays literal)", outs, want)
	}
}

// TestOpaqueSh pins shell_detection_depth: a string handed to sh -c is an
// argument, never parsed.
func TestOpaqueSh(t *testing.T) {
	for _, line := range []string{`sh -c "pass-cli x 2>&1"`, `bash -c 'cat .env'`, `eval "pass-cli x"`} {
		s := scan(line)
		if len(s.Commands) != 1 || s.Partial {
			t.Fatalf("%s: commands = %q partial=%v, want one", line, names(s), s.Partial)
		}
		c := s.Commands[0]
		if len(c.Redirects) != 0 || c.Stderr != Transcript {
			t.Errorf("%s: the body's redirect leaked out: %+v", line, c.Redirects)
		}
	}
	if s := scan(`sh -c "pass-cli x 2>&1"`); s.Commands[0].Name() != "sh" {
		t.Errorf("argv0 = %q, want sh", s.Commands[0].Name())
	}
}

func TestPartial(t *testing.T) {
	cases := []struct{ line, problem string }{
		{`echo 'unterminated`, "unterminated single quote"},
		{`git commit -m "unterminated`, "unterminated double quote"},
		{"echo `ls", "unterminated backtick"},
		{"echo $(ls", "unterminated ( or $("},
		{"cat <<EOF\nbody", "unterminated heredoc <<EOF"},
		{"( a", "unterminated ( or $("},
		{"a )", "unexpected )"},
		{"| a", "pipe with no command before it"},
		{"echo >", "redirection > with no target"},
		{"if a; then b", "unterminated if (no fi)"},
	}
	for _, c := range cases {
		t.Run(c.line, func(t *testing.T) {
			s := scan(c.line)
			if !s.Partial || s.Problem != c.problem {
				t.Errorf("Partial=%v Problem=%q, want true %q", s.Partial, s.Problem, c.problem)
			}
		})
	}
	// What was read before the problem is still reported.
	s := scan(`pass-cli item view x | head; echo "oops`)
	if got := names(s); len(got) < 2 || got[0] != "pass-cli" || s.Commands[0].Stdout != Pipe {
		t.Errorf("commands before the problem = %q, want pass-cli piped first", got)
	}
}

var fuzzSeeds = []string{
	"a && b; c | d || e",
	"git commit -m \"$(cat <<'EOF'\nmsg\nEOF\n)\"",
	"pass-cli x |& head",
	"( pass-cli view ) 2>&1",
	`echo "$(a "$(b ` + "`c`" + `)")"`,
	"cat <<-EOF\n\tx\n\tEOF",
	"case x in (a) b;; *) c;;& esac",
	"for ((i=0;i<3;i++)); do x; done",
	"x={a}>f {fd}>&2 arr=(1 2) $'\\''",
	"${a${b}} $((1+(2))) $ $\" \\",
	"'", "\"", "`", "$(", "${", "$((", "<<", "<<<", "&>", "|&", ";;", "((", "{", "}", ")", "\\",
	"cd", "env -u", "sudo -u", "git -C", "function", "f(", "case", "case x", "case x in", "case x in a",
}

func FuzzScan(f *testing.F) {
	for _, s := range fuzzSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, line string) {
		s := Scan(line, Options{Dir: cwd, Home: "/home/u"})
		for i, c := range s.Commands {
			if c.Parent >= i {
				t.Fatalf("command %d has Parent %d, which does not precede it", i, c.Parent)
			}
			if c.Parent >= 0 && !c.Substituted {
				t.Fatalf("command %d has a parent but is not substituted", i)
			}
		}
		if s.Partial == (s.Problem == "") {
			t.Fatalf("Partial=%v with Problem %q", s.Partial, s.Problem)
		}
	})
}

func TestFuzzSeedsNeverPanic(t *testing.T) {
	// The seeds are also run as plain inputs, every prefix included, so an
	// index slip near the end of any construct shows up here.
	for _, seed := range fuzzSeeds {
		for i := 0; i <= len(seed); i++ {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("Scan(%q) panicked: %v", seed[:i], r)
					}
				}()
				Scan(seed[:i], Options{Dir: cwd, Home: "/home/u"})
				Scan(strings.Repeat(seed[:i], 2), Options{})
			}()
		}
	}
}

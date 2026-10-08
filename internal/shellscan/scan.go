// Package shellscan is a lenient, read-only scanner for the command lines
// Claude Code's Bash tool runs. It answers the questions the tool gates ask —
// which programs a line invokes, with which arguments, in which directory, and
// where each one's stdout and stderr finally land — without executing anything.
//
// It is deliberately not a shell. It recognises the honest-mistake shapes the
// gates target (shell_detection_depth): chained commands (&& || ; | |& & and
// newlines), env-var and env/command/sudo/nohup/time prefixes, `cd` and
// `git -C` directory changes, redirections in source order, ( … ) and { … }
// groups, and command substitutions ($( … ), backticks, <( … ), >( … )). Heredoc
// bodies are data, never commands. `sh -c "…"`, `eval` and encoded payloads are
// opaque on purpose: the string handed to them is an argument, not parsed.
//
// Scanning never fails and never panics. Input it cannot read with confidence —
// an unbalanced quote, an unterminated substitution or heredoc, a stray closer —
// sets Script.Partial and names the first problem in Script.Problem; everything
// read up to that point is still reported, so a gate can refuse a partial line
// that holds a token it claims instead of guessing about the rest.
package shellscan

import (
	"path/filepath"
	"strconv"
	"strings"
)

// Dest is where a command's output stream finally lands once every
// redirection — its own, its pipeline's and its enclosing groups' — is applied.
type Dest int

const (
	// Transcript is an open stream: the Bash tool returns it to Claude.
	Transcript Dest = iota
	// Pipe feeds the next pipeline element, which can print it.
	Pipe
	// Capture is read by the shell into a word: $( … ), backticks or <( … ).
	Capture
	// File is a regular file the stream was redirected to.
	File
	// Null is /dev/null or a closed descriptor.
	Null
)

func (d Dest) String() string {
	switch d {
	case Transcript:
		return "transcript"
	case Pipe:
		return "pipe"
	case Capture:
		return "capture"
	case File:
		return "file"
	case Null:
		return "null"
	}
	return "dest(" + strconv.Itoa(int(d)) + ")"
}

// OffTranscript reports whether the stream cannot reach Claude: it went to a
// file or nowhere. A pipe or a capture can still be printed by whatever reads
// it, so neither counts.
func (d Dest) OffTranscript() bool { return d == File || d == Null }

// Options places the line: Dir is the directory the Bash tool runs it in and
// Home expands an unquoted leading `~` and $HOME. Either may be empty, in which
// case relative paths stay relative and `~` stays literal.
type Options struct {
	Dir  string
	Home string
}

// Redirect is one redirection as written. Fd is the descriptor it applies to,
// or -1 for the both-streams forms (`&>f`, `&>>f`, `>&f`). Target is the file
// word after quote removal, the descriptor digits of a duplication (`2>&1` has
// Target "1"), "-" for a close, or the delimiter of a heredoc.
type Redirect struct {
	Fd     int
	Op     string
	Target string
}

// isDup reports whether r duplicates or closes a descriptor instead of naming
// a file.
func (r Redirect) isDup() bool {
	if r.Op != ">&" && r.Op != "<&" {
		return false
	}
	t := strings.TrimSuffix(r.Target, "-")
	if t == "" {
		return true // a bare `-` closes the descriptor
	}
	_, err := strconv.Atoi(t)
	return err == nil
}

// Command is one simple command.
type Command struct {
	// Argv is the invocation after leading assignments and the env, command,
	// sudo, nohup and time prefixes are stripped; Argv[0] is the program's
	// basename. Empty for a bare assignment or redirection.
	Argv []string
	// Program is Argv[0] as written, e.g. /usr/bin/pass-cli.
	Program string
	// Assigns holds the NAME=value words stripped from the front, including
	// any that followed env or sudo.
	Assigns []string
	// Redirects are the command's own redirections, in source order.
	Redirects []Redirect
	// Dir is the shell's working directory when the command runs, after every
	// earlier `cd` on the line.
	Dir string
	// Stdout and Stderr are where descriptors 1 and 2 finally land.
	Stdout, Stderr Dest
	// Substituted marks a command that runs inside $( … ), backticks or a
	// process substitution rather than as part of the line's own list.
	Substituted bool
	// Parent is the index in Script.Commands of the command whose word holds
	// this substitution, or -1.
	Parent int
	// Op is the operator that joined this command to the one before it in its
	// list: "", "&&", "||", ";", "|", "|&", "&" or "\n".
	Op string

	frame  *frame
	pipe   pipeKind
	parent *Command
}

// Name is the program's basename, or "" for a command with no argv.
func (c Command) Name() string {
	if len(c.Argv) == 0 {
		return ""
	}
	return c.Argv[0]
}

// Args is the argv after the program.
func (c Command) Args() []string {
	if len(c.Argv) == 0 {
		return nil
	}
	return c.Argv[1:]
}

// Inputs are the files read on standard input through `<` or `<>`.
func (c Command) Inputs() []string {
	var out []string
	for _, r := range c.Redirects {
		if r.Fd == 0 && (r.Op == "<" || r.Op == "<>") {
			out = append(out, r.Target)
		}
	}
	return out
}

// Outputs are the files any output redirection opens for writing.
func (c Command) Outputs() []string {
	var out []string
	for _, r := range c.Redirects {
		switch r.Op {
		case ">", ">>", ">|", "&>", "&>>":
			out = append(out, r.Target)
		case "<>":
			if r.Fd != 0 {
				out = append(out, r.Target)
			}
		case ">&":
			if !r.isDup() {
				out = append(out, r.Target)
			}
		}
	}
	return out
}

// Resolve joins a path operand to the command's directory. Absolute paths
// come back cleaned; with no directory known a relative path stays relative.
func (c Command) Resolve(p string) string { return resolve(c.Dir, p) }

// GitCall is a git invocation with its global options read: Dir applies every
// `-C`, Sub is the subcommand ("" when there is none) and Args follow it.
type GitCall struct {
	Dir  string
	Sub  string
	Args []string
}

// Git reads a git command's global options. ok is false for any other program.
func (c Command) Git() (GitCall, bool) {
	if c.Name() != "git" {
		return GitCall{}, false
	}
	g := GitCall{Dir: c.Dir}
	args := c.Args()
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-C":
			if i+1 < len(args) {
				g.Dir = resolve(g.Dir, args[i+1])
				i++
			}
		case gitArgOpts[a]:
			i++
		case strings.HasPrefix(a, "-"):
		default:
			g.Sub = a
			g.Args = args[i+1:]
			return g, true
		}
	}
	return g, true
}

// gitArgOpts are git's global options that take their value as the next word.
var gitArgOpts = map[string]bool{
	"-c": true, "--git-dir": true, "--work-tree": true, "--namespace": true,
	"--config-env": true, "--super-prefix": true, "--list-cmds": true,
}

// Script is a scanned line.
type Script struct {
	// Commands holds every simple command in the order it starts in the source,
	// substituted ones included.
	Commands []Command
	// Partial is set when part of the line could not be read with confidence.
	Partial bool
	// Problem names the first thing that could not be read.
	Problem string
}

// Scan reads line as the Bash tool would hand it to the shell.
func Scan(line string, opts Options) Script {
	st := &state{home: opts.Home}
	p := &parser{src: line, st: st}
	p.parseList(ctx{grp: nil, dir: &dirState{path: opts.Dir}}, termEOF)
	p.flushHeredocs(termEOF)
	return st.result()
}

type pipeKind int

const (
	pipeNone pipeKind = iota
	pipeOut           // `|`: stdout feeds the pipe
	pipeAll           // `|&`: stdout and stderr feed the pipe
)

// frame is a group: a ( … ) subshell, a { … } brace group, or a substitution
// whose stdout the shell captures. Its pipe and redirections apply to every
// command inside it, before the command's own.
type frame struct {
	parent  *frame
	capture bool
	pipe    pipeKind
	redirs  []Redirect
}

type dirState struct{ path string }

// ctx is the parsing context a command inherits: its enclosing frame, the
// working directory (shared by a brace group, copied by a subshell), and for a
// substitution the command whose word holds it.
type ctx struct {
	grp    *frame
	dir    *dirState
	subst  bool
	parent *Command
}

// term is what ends a list: end of input, a closing paren, or the reserved
// word that closes a compound command.
type term int

const (
	termEOF term = iota
	termParen
	termBrace
	termDone
	termFi
	termCase
)

// closer is the reserved word each compound terminator waits for.
var closer = map[term]string{termBrace: "}", termDone: "done", termFi: "fi", termCase: "esac"}

var unterminated = map[term]string{
	termParen: "unterminated ( or $(",
	termBrace: "unterminated {",
	termDone:  "unterminated loop (no done)",
	termFi:    "unterminated if (no fi)",
	termCase:  "unterminated case (no esac)",
}

type state struct {
	home    string
	cmds    []*Command
	partial bool
	problem string
}

func (st *state) fail(problem string) {
	if !st.partial {
		st.partial = true
		st.problem = problem
	}
}

func (st *state) result() Script {
	index := map[*Command]int{}
	var kept []*Command
	for _, c := range st.cmds {
		if len(c.Argv) == 0 && len(c.Assigns) == 0 && len(c.Redirects) == 0 {
			continue
		}
		index[c] = len(kept)
		kept = append(kept, c)
	}
	out := Script{Partial: st.partial, Problem: st.problem}
	for _, c := range kept {
		cc := *c
		cc.Parent = -1
		if i, ok := index[c.parent]; ok {
			cc.Parent = i
		}
		cc.Stdout, cc.Stderr = streams(c)
		cc.frame, cc.parent = nil, nil
		out.Commands = append(out.Commands, cc)
	}
	return out
}

// streams applies the enclosing frames outermost first, then the command's
// own pipe and redirections, and reads off descriptors 1 and 2.
func streams(c *Command) (Dest, Dest) {
	var chain []*frame
	for f := c.frame; f != nil; f = f.parent {
		chain = append(chain, f)
	}
	var fds [10]Dest
	for i := range fds {
		fds[i] = Null // an unopened descriptor: writing to it fails
	}
	fds[0], fds[1], fds[2] = Transcript, Transcript, Transcript
	for i := len(chain) - 1; i >= 0; i-- {
		f := chain[i]
		apply(&fds, f.capture, f.pipe, f.redirs)
	}
	apply(&fds, false, c.pipe, c.Redirects)
	return fds[1], fds[2]
}

// apply follows bash: a pipe connects stdout before the command's own
// redirections run, and `|&`'s implicit 2>&1 runs after them.
func apply(fds *[10]Dest, capture bool, pipe pipeKind, redirs []Redirect) {
	if capture {
		fds[1] = Capture
	}
	if pipe != pipeNone {
		fds[1] = Pipe
	}
	for _, r := range redirs {
		switch r.Op {
		case "<<", "<<-", "<<<":
			continue // stdin only
		}
		if r.isDup() {
			if r.Fd < 0 || r.Fd >= len(fds) {
				continue
			}
			src := strings.TrimSuffix(r.Target, "-")
			if src == "" {
				fds[r.Fd] = Null
				continue
			}
			n, _ := strconv.Atoi(src)
			if n >= 0 && n < len(fds) {
				fds[r.Fd] = fds[n]
			} else {
				fds[r.Fd] = Null
			}
			continue
		}
		d := fileDest(fds, r.Target)
		if r.Fd == -1 {
			fds[1], fds[2] = d, d
		} else if r.Fd >= 0 && r.Fd < len(fds) {
			fds[r.Fd] = d
		}
	}
	if pipe == pipeAll {
		fds[2] = fds[1]
	}
}

func fileDest(fds *[10]Dest, target string) Dest {
	switch target {
	case "/dev/null":
		return Null
	case "/dev/stdout":
		return fds[1]
	case "/dev/stderr":
		return fds[2]
	case "/dev/tty":
		return Transcript
	}
	if rest, ok := strings.CutPrefix(target, "/dev/fd/"); ok {
		if n, err := strconv.Atoi(rest); err == nil && n >= 0 && n < len(fds) {
			return fds[n]
		}
	}
	return File
}

func resolve(dir, p string) string {
	if p == "" {
		return dir
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(dir, p)
}

// heredoc is a body waiting for the next newline.
type heredoc struct {
	delim  string
	strip  bool // <<- strips leading tabs
	quoted bool // a quoted delimiter leaves the body unexpanded
	owner  *Command
	c      ctx
}

// parser walks one source string. Backtick and heredoc bodies get their own
// parser over the body text, sharing the state.
type parser struct {
	src     string
	pos     int
	st      *state
	pending []heredoc
}

func (p *parser) eof() bool { return p.pos >= len(p.src) }

func (p *parser) peekAt(off int) byte {
	if p.pos+off < len(p.src) {
		return p.src[p.pos+off]
	}
	return 0
}

func (p *parser) has(s string) bool { return strings.HasPrefix(p.src[p.pos:], s) }

func (p *parser) skipBlanks() {
	for !p.eof() {
		switch c := p.src[p.pos]; {
		case c == ' ' || c == '\t' || c == '\r':
			p.pos++
		case c == '\\' && p.peekAt(1) == '\n':
			p.pos += 2
		default:
			return
		}
	}
}

func (p *parser) skipComment() {
	for !p.eof() && p.src[p.pos] != '\n' {
		p.pos++
	}
}

func isMeta(c byte) bool {
	switch c {
	case ' ', '\t', '\r', '\n', ';', '&', '|', '<', '>', '(', ')':
		return true
	}
	return false
}

// peekPlainWord returns the next word when it is made only of plain
// characters, without consuming it or running any substitution. Reserved
// words are always plain, so this is enough to spot them.
func (p *parser) peekPlainWord() string {
	i := p.pos
	for i < len(p.src) && !isMeta(p.src[i]) {
		switch p.src[i] {
		case '\'', '"', '\\', '$', '`':
			return ""
		}
		i++
	}
	return p.src[p.pos:i]
}

// reservedSkip are the reserved words inside a compound command that separate
// its parts; the commands between them are scanned as if they were not there.
var reservedSkip = map[string]bool{"then": true, "elif": true, "else": true, "do": true, "!": true}

// unit is a pipeline element: a command or a group.
type unit interface{ setPipe(pipeKind) }

func (c *Command) setPipe(k pipeKind) { c.pipe = k }
func (f *frame) setPipe(k pipeKind)   { f.pipe = k }

// parseList reads commands joined by operators until t's terminator.
func (p *parser) parseList(c ctx, t term) {
	op := ""
	var last unit
	for {
		p.skipBlanks()
		if p.eof() {
			if msg, ok := unterminated[t]; ok {
				p.st.fail(msg)
			}
			return
		}
		start := p.pos
		switch ch := p.src[p.pos]; ch {
		case '#':
			p.skipComment()
		case '\n':
			p.pos++
			p.flushHeredocs(t)
			if last != nil && op == "" {
				op = "\n"
			}
			last = nil
		case ';':
			p.pos++
			if next := p.peekAt(0); next == ';' || next == '&' {
				p.pos++
				if next == ';' && p.peekAt(0) == '&' {
					p.pos++
				}
				if t == termCase {
					return
				}
				p.st.fail("case terminator outside a case")
			}
			op, last = ";", nil
		case '&':
			switch p.peekAt(1) {
			case '&':
				p.pos += 2
				op, last = "&&", nil
			case '>':
				last = p.parseCommand(c, op)
				op = ""
			default:
				p.pos++
				op, last = "&", nil
			}
		case '|':
			kind, tok := pipeOut, "|"
			switch p.peekAt(1) {
			case '|':
				p.pos += 2
				op, last = "||", nil
				continue
			case '&':
				kind, tok = pipeAll, "|&"
			}
			p.pos += len(tok)
			if last == nil {
				p.st.fail("pipe with no command before it")
			} else {
				last.setPipe(kind)
			}
			op, last = tok, nil
		case ')':
			p.pos++
			if t == termParen {
				return
			}
			p.st.fail("unexpected )")
		case '(':
			if p.peekAt(1) == '(' {
				p.skipArith(p.pos + 2)
				last = &frame{}
				break
			}
			p.pos++
			last = p.parseGroup(c, termParen)
			op = ""
		default:
			switch w := p.peekPlainWord(); {
			case w == "{":
				p.pos++
				last, op = p.parseGroup(c, termBrace), ""
			case w == "}" || w == "done" || w == "fi" || w == "esac":
				if closer[t] == w {
					if t != termCase {
						p.pos += len(w) // parseCase consumes its own esac
					}
					return
				}
				p.pos += len(w)
				p.st.fail("unexpected " + w)
			case w == "if":
				p.pos += len(w)
				last, op = p.parseGroup(c, termFi), ""
			case w == "while" || w == "until":
				p.pos += len(w)
				last, op = p.parseGroup(c, termDone), ""
			case w == "for" || w == "select":
				p.pos += len(w)
				p.loopHeader(c)
				last, op = p.parseGroup(c, termDone), ""
			case w == "case":
				p.pos += len(w)
				last, op = p.parseCase(c), ""
			case w == "function":
				p.pos += len(w)
				p.skipBlanks()
				p.pos += len(p.peekPlainWord())
				p.skipBlanks()
				if p.peekAt(0) == '(' {
					p.emptyParens()
				}
			case reservedSkip[w]:
				p.pos += len(w)
			default:
				if last != nil {
					p.st.fail("missing operator between commands")
				}
				last, op = p.parseCommand(c, op), ""
			}
		}
		if p.pos == start {
			p.st.fail("unreadable character " + strconv.QuoteRune(rune(p.src[p.pos])))
			p.pos++
		}
	}
}

// parseGroup reads a group body (any opener already consumed) and the
// redirections that follow its closer. A ( … ) subshell gets its own copy of
// the working directory; every other group shares it.
func (p *parser) parseGroup(c ctx, t term) *frame {
	f := &frame{parent: c.grp}
	inner := ctx{grp: f, dir: c.dir, subst: c.subst, parent: c.parent}
	if t == termParen {
		inner.dir = &dirState{path: c.dir.path}
	}
	p.parseList(inner, t)
	p.trailingRedirects(c, f)
	return f
}

func (p *parser) trailingRedirects(c ctx, f *frame) {
	for {
		p.skipBlanks()
		if p.eof() || !p.atRedirect() {
			return
		}
		before := p.pos
		if r, ok := p.parseRedirect(c, nil); ok {
			f.redirs = append(f.redirs, r)
		}
		if p.pos == before {
			return
		}
	}
}

// loopHeader reads a for/select header (the keyword consumed). Its words are
// data, not a command, but any substitution in them still runs.
func (p *parser) loopHeader(c ctx) {
	p.skipBlanks()
	if p.has("((") {
		p.skipArith(p.pos + 2)
		return
	}
	hdr := p.parseCommand(c, "")
	hdr.Argv, hdr.Program, hdr.Assigns, hdr.Redirects = nil, "", nil, nil
}

// parseCase reads `case WORD in [(]PATTERN) LIST ;; … esac` (the keyword
// consumed). Patterns are skipped; each clause's list is scanned.
func (p *parser) parseCase(c ctx) *frame {
	f := &frame{parent: c.grp}
	inner := ctx{grp: f, dir: c.dir, subst: c.subst, parent: c.parent}
	p.skipBlanks()
	if p.eof() {
		p.st.fail(unterminated[termCase])
		return f
	}
	p.readWord(c, nil)
	p.skipLines(termCase)
	if p.peekPlainWord() != "in" {
		p.st.fail("case without in")
		return f
	}
	p.pos += len("in")
	for {
		p.skipLines(termCase)
		if p.eof() {
			p.st.fail(unterminated[termCase])
			return f
		}
		if p.peekPlainWord() == "esac" {
			p.pos += len("esac")
			p.trailingRedirects(c, f)
			return f
		}
		if !p.skipPattern() {
			p.st.fail("unterminated case pattern")
			return f
		}
		p.parseList(inner, termCase)
	}
}

// skipLines skips blanks, comments and newlines, reading any heredoc bodies
// the newlines release.
func (p *parser) skipLines(t term) {
	for {
		p.skipBlanks()
		switch {
		case p.peekAt(0) == '\n':
			p.pos++
			p.flushHeredocs(t)
		case p.peekAt(0) == '#':
			p.skipComment()
		default:
			return
		}
	}
}

// skipPattern consumes a case pattern through its unquoted `)`.
func (p *parser) skipPattern() bool {
	for !p.eof() {
		switch p.src[p.pos] {
		case '\\':
			p.pos++
		case '\'':
			end := strings.IndexByte(p.src[p.pos+1:], '\'')
			if end < 0 {
				return false
			}
			p.pos += end + 1
		case '"':
			end := strings.IndexByte(p.src[p.pos+1:], '"')
			if end < 0 {
				return false
			}
			p.pos += end + 1
		case ')':
			p.pos++
			return true
		case '\n':
			return false
		}
		p.pos++
	}
	return false
}

// skipArith skips an arithmetic body from i (just past its opening "((").
func (p *parser) skipArith(i int) {
	depth := 2
	for i < len(p.src) {
		switch p.src[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				p.pos = i + 1
				return
			}
		}
		i++
	}
	p.pos = len(p.src)
	p.st.fail("unterminated ((")
}

// atRedirect reports whether a redirection operator starts here, optionally
// prefixed by a descriptor number or a {name}.
func (p *parser) atRedirect() bool {
	i := p.pos
	if i < len(p.src) && p.src[i] == '{' {
		j := strings.IndexByte(p.src[i:], '}')
		if j > 1 && isName(p.src[i+1:i+j]) {
			i += j + 1
		}
	} else {
		for i < len(p.src) && p.src[i] >= '0' && p.src[i] <= '9' {
			i++
		}
	}
	if i >= len(p.src) {
		return false
	}
	switch p.src[i] {
	case '<', '>':
		return i+1 >= len(p.src) || p.src[i+1] != '('
	case '&':
		return i == p.pos && i+1 < len(p.src) && p.src[i+1] == '>'
	}
	return false
}

// redirOps is longest-first so a prefix never shadows a longer operator.
var redirOps = []string{"&>>", "<<<", "<<-", "&>", ">>", ">|", ">&", "<<", "<&", "<>", ">", "<"}

// parseRedirect reads one redirection. owner is the command it belongs to, or
// nil for a group's.
func (p *parser) parseRedirect(c ctx, owner *Command) (Redirect, bool) {
	fd, explicit := -2, false
	if p.peekAt(0) == '{' {
		j := strings.IndexByte(p.src[p.pos:], '}')
		p.pos += j + 1
		fd, explicit = 9, true // a {name} descriptor: one we do not track
	} else {
		start := p.pos
		for !p.eof() && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
			p.pos++
		}
		if p.pos > start {
			n, err := strconv.Atoi(p.src[start:p.pos])
			if err != nil || n > 9 {
				n = 9
			}
			fd, explicit = n, true
		}
	}
	op := ""
	for _, o := range redirOps {
		if p.has(o) {
			op = o
			break
		}
	}
	if op == "" {
		p.st.fail("unreadable redirection")
		return Redirect{}, false
	}
	p.pos += len(op)
	if !explicit {
		switch {
		case op == "&>" || op == "&>>":
			fd = -1
		case op[0] == '<':
			fd = 0
		default:
			fd = 1
		}
	}
	p.skipBlanks()
	if p.eof() || isMeta(p.src[p.pos]) {
		p.st.fail("redirection " + op + " with no target")
		return Redirect{}, false
	}
	if op == "<<" || op == "<<-" {
		w := p.readWord(c, owner)
		p.pending = append(p.pending, heredoc{delim: w.val, strip: op == "<<-", quoted: w.quoted, owner: owner, c: c})
		return Redirect{Fd: fd, Op: op, Target: w.val}, true
	}
	w := p.readWord(c, owner)
	r := Redirect{Fd: fd, Op: op, Target: w.val}
	if op == ">&" && !explicit && !r.isDup() {
		r.Fd = -1 // `>&file` sends both streams to file
	}
	return r, true
}

// parseCommand reads one simple command: words and redirections up to the
// next operator.
func (p *parser) parseCommand(c ctx, op string) *Command {
	cmd := &Command{Op: op, frame: c.grp, Substituted: c.subst, parent: c.parent}
	p.st.cmds = append(p.st.cmds, cmd)
	var words []word
	funcDef := false
loop:
	for {
		p.skipBlanks()
		if p.eof() {
			break
		}
		switch ch := p.src[p.pos]; {
		case ch == '\n' || ch == ';' || ch == '|' || ch == ')':
			break loop
		case ch == '&' && p.peekAt(1) != '>':
			break loop
		case ch == '#':
			p.skipComment()
			break loop
		case (ch == '<' || ch == '>') && p.peekAt(1) == '(':
			words = append(words, p.readProcSubst(c, cmd))
			continue
		case p.atRedirect():
			before := p.pos
			if r, ok := p.parseRedirect(c, cmd); ok {
				cmd.Redirects = append(cmd.Redirects, r)
			}
			if p.pos == before {
				p.pos++
			}
			continue
		case ch == '(':
			if len(words) == 1 && p.emptyParens() {
				funcDef = true
				break loop
			}
			p.st.fail("unexpected ( inside a command")
			break loop
		}
		before := p.pos
		w := p.readWord(c, cmd)
		if p.pos == before {
			p.st.fail("unreadable character " + strconv.QuoteRune(rune(p.src[p.pos])))
			p.pos++
			continue
		}
		words = append(words, w)
	}
	if funcDef {
		words = nil
	}
	p.finish(cmd, words, c)
	return cmd
}

// emptyParens consumes the `()` of a `name()` function definition.
func (p *parser) emptyParens() bool {
	i := p.pos + 1
	for i < len(p.src) && (p.src[i] == ' ' || p.src[i] == '\t') {
		i++
	}
	if i < len(p.src) && p.src[i] == ')' {
		p.pos = i + 1
		return true
	}
	return false
}

// readProcSubst reads <( … ) or >( … ). The reader's output is captured by
// the command that opens it; the writer's is not.
func (p *parser) readProcSubst(c ctx, owner *Command) word {
	start := p.pos
	capture := p.src[p.pos] == '<'
	p.pos += 2
	f := &frame{parent: c.grp, capture: capture}
	p.parseList(ctx{grp: f, dir: &dirState{path: c.dir.path}, subst: true, parent: owner}, termParen)
	return word{val: p.src[start:p.pos], quoted: true}
}

type word struct {
	val    string
	quoted bool // any part was quoted or escaped
	assign bool // an unquoted NAME= or NAME+= prefix
}

// readWord reads one word, running every substitution it holds.
func (p *parser) readWord(c ctx, owner *Command) word {
	var b strings.Builder
	w := word{}
	plain := true // every byte so far is unquoted and literal
	atStart := true
	for !p.eof() {
		ch := p.src[p.pos]
		if isMeta(ch) {
			if ch == '(' && w.assign && strings.HasSuffix(b.String(), "=") {
				b.WriteString(p.readBalanced())
				continue
			}
			break
		}
		switch ch {
		case '\\':
			if p.peekAt(1) == '\n' {
				p.pos += 2
				continue
			}
			w.quoted, plain = true, false
			if p.pos+1 < len(p.src) {
				b.WriteByte(p.src[p.pos+1])
				p.pos += 2
			} else {
				p.pos++
			}
		case '\'':
			w.quoted, plain = true, false
			end := strings.IndexByte(p.src[p.pos+1:], '\'')
			if end < 0 {
				p.st.fail("unterminated single quote")
				b.WriteString(p.src[p.pos+1:])
				p.pos = len(p.src)
				break
			}
			b.WriteString(p.src[p.pos+1 : p.pos+1+end])
			p.pos += end + 2
		case '"':
			w.quoted, plain = true, false
			p.pos++
			p.readDouble(&b, c, owner, false)
		case '$':
			if p.peekAt(1) == '\'' {
				w.quoted, plain = true, false
				p.pos += 2
				p.readANSI(&b)
				break
			}
			if p.peekAt(1) == '"' {
				w.quoted, plain = true, false
				p.pos += 2
				p.readDouble(&b, c, owner, false)
				break
			}
			plain = false
			p.readDollar(&b, c, owner)
		case '`':
			plain = false
			p.readBacktick(&b, c, owner)
		case '~':
			if atStart && p.st.home != "" && (p.peekAt(1) == '/' || p.peekAt(1) == 0 || isMeta(p.peekAt(1))) {
				b.WriteString(p.st.home)
			} else {
				b.WriteByte('~')
			}
			plain = false
			p.pos++
		case '=':
			if plain && !w.assign && isAssignName(b.String()) {
				w.assign = true
			}
			plain = false
			b.WriteByte('=')
			p.pos++
		default:
			b.WriteByte(ch)
			p.pos++
		}
		atStart = false
	}
	w.val = b.String()
	return w
}

// readBalanced reads an array assignment's ( … ) verbatim.
func (p *parser) readBalanced() string {
	start, depth := p.pos, 0
	for !p.eof() {
		switch p.src[p.pos] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				p.pos++
				return p.src[start:p.pos]
			}
		}
		p.pos++
	}
	p.st.fail("unterminated array assignment")
	return p.src[start:]
}

func isName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (i > 0 && c >= '0' && c <= '9') {
			continue
		}
		return false
	}
	return true
}

func isAssignName(s string) bool { return isName(strings.TrimSuffix(s, "+")) }

// readDouble reads a double-quoted body (the opening quote consumed). In
// heredoc mode there is no closing quote: the body runs to the end.
func (p *parser) readDouble(b *strings.Builder, c ctx, owner *Command, heredocMode bool) {
	for !p.eof() {
		ch := p.src[p.pos]
		switch {
		case ch == '"' && !heredocMode:
			p.pos++
			return
		case ch == '\\':
			next := p.peekAt(1)
			switch {
			case next == '\n':
				p.pos += 2
			case next == '$' || next == '`' || next == '\\' || (next == '"' && !heredocMode):
				b.WriteByte(next)
				p.pos += 2
			default:
				b.WriteByte('\\')
				p.pos++
			}
		case ch == '$':
			p.readDollar(b, c, owner)
		case ch == '`':
			p.readBacktick(b, c, owner)
		default:
			b.WriteByte(ch)
			p.pos++
		}
	}
	if !heredocMode {
		p.st.fail("unterminated double quote")
	}
}

// readANSI reads a $'…' body (the opener consumed).
func (p *parser) readANSI(b *strings.Builder) {
	for !p.eof() {
		ch := p.src[p.pos]
		switch ch {
		case '\'':
			p.pos++
			return
		case '\\':
			if p.pos+1 < len(p.src) {
				switch n := p.src[p.pos+1]; n {
				case 'n':
					b.WriteByte('\n')
				case 't':
					b.WriteByte('\t')
				default:
					b.WriteByte(n)
				}
				p.pos += 2
				continue
			}
			p.pos++
		default:
			b.WriteByte(ch)
			p.pos++
		}
	}
	p.st.fail("unterminated $' quote")
}

// readDollar reads an expansion starting at `$`: a command substitution is
// parsed as nested commands, arithmetic and ${…} are skipped, and HOME
// expands when Options.Home is set. Anything else stays literal.
func (p *parser) readDollar(b *strings.Builder, c ctx, owner *Command) {
	start := p.pos
	switch next := p.peekAt(1); {
	case next == '(' && p.peekAt(2) == '(':
		p.skipArith(p.pos + 3)
		b.WriteString(p.src[start:p.pos])
	case next == '(':
		p.pos += 2
		f := &frame{parent: c.grp, capture: true}
		p.parseList(ctx{grp: f, dir: &dirState{path: c.dir.path}, subst: true, parent: owner}, termParen)
		b.WriteString(p.src[start:p.pos])
	case next == '{':
		p.pos += 2
		depth := 1
		for !p.eof() && depth > 0 {
			switch p.src[p.pos] {
			case '\\':
				p.pos++
			case '{':
				depth++
			case '}':
				depth--
			}
			p.pos++
		}
		if depth > 0 {
			p.st.fail("unterminated ${")
			p.pos = len(p.src)
		}
		if body := p.src[start:p.pos]; body == "${HOME}" && p.st.home != "" {
			b.WriteString(p.st.home)
		} else {
			b.WriteString(body)
		}
	default:
		p.pos++
		i := p.pos
		for i < len(p.src) && isName(p.src[p.pos:i+1]) {
			i++
		}
		name := p.src[p.pos:i]
		switch {
		case name == "HOME" && p.st.home != "":
			b.WriteString(p.st.home)
			p.pos = i
		case name != "":
			b.WriteString("$" + name)
			p.pos = i
		case !p.eof() && !isMeta(p.src[p.pos]) && p.src[p.pos] != '"':
			b.WriteByte('$')
			b.WriteByte(p.src[p.pos])
			p.pos++
		default:
			b.WriteByte('$')
		}
	}
}

// readBacktick reads a `…` substitution and scans its body (unescaped) as
// nested commands with their stdout captured.
func (p *parser) readBacktick(b *strings.Builder, c ctx, owner *Command) {
	start := p.pos
	p.pos++
	var body strings.Builder
	for !p.eof() {
		ch := p.src[p.pos]
		if ch == '`' {
			p.pos++
			b.WriteString(p.src[start:p.pos])
			sub := &parser{src: body.String(), st: p.st}
			f := &frame{parent: c.grp, capture: true}
			sub.parseList(ctx{grp: f, dir: &dirState{path: c.dir.path}, subst: true, parent: owner}, termEOF)
			sub.flushHeredocs(termEOF)
			return
		}
		if ch == '\\' && p.pos+1 < len(p.src) {
			if n := p.src[p.pos+1]; n == '`' || n == '\\' || n == '$' {
				body.WriteByte(n)
				p.pos += 2
				continue
			}
		}
		body.WriteByte(ch)
		p.pos++
	}
	p.st.fail("unterminated backtick")
	b.WriteString(p.src[start:])
}

// flushHeredocs consumes the bodies queued on the line just ended. A body is
// data: only an unquoted delimiter's body is expanded, and then only its
// substitutions run.
func (p *parser) flushHeredocs(t term) {
	pending := p.pending
	p.pending = nil
	for _, h := range pending {
		if p.eof() {
			p.st.fail("unterminated heredoc <<" + h.delim)
			return
		}
		bodyStart, found := p.pos, false
		bodyEnd := p.pos
		for !p.eof() {
			eol := strings.IndexByte(p.src[p.pos:], '\n')
			line := p.src[p.pos:]
			if eol >= 0 {
				line = line[:eol]
			}
			check := line
			if h.strip {
				check = strings.TrimLeft(check, "\t")
			}
			if check == h.delim {
				bodyEnd, found = p.pos, true
				p.pos += len(line)
				if eol >= 0 {
					p.pos++
				}
				break
			}
			// Inside $( … ) bash also accepts the delimiter run straight into
			// the closing paren.
			if t == termParen && strings.HasPrefix(check, h.delim+")") {
				bodyEnd, found = p.pos, true
				p.pos += len(line) - len(check) + len(h.delim)
				break
			}
			if eol < 0 {
				p.pos = len(p.src)
				break
			}
			p.pos += eol + 1
		}
		if !found {
			p.st.fail("unterminated heredoc <<" + h.delim)
			bodyEnd = len(p.src)
		}
		if !h.quoted {
			sub := &parser{src: p.src[bodyStart:bodyEnd], st: p.st}
			var discard strings.Builder
			sub.readDouble(&discard, h.c, h.owner, true)
		}
	}
}

// finish turns a command's words into argv: leading assignments and the
// transparent prefixes come off, cd moves the directory for what follows.
func (p *parser) finish(cmd *Command, words []word, c ctx) {
	i := 0
	for i < len(words) && words[i].assign {
		cmd.Assigns = append(cmd.Assigns, words[i].val)
		i++
	}
	rest := stripPrefixes(words[i:], cmd)
	cmd.Dir = c.dir.path
	if len(rest) == 0 {
		return
	}
	cmd.Program = rest[0].val
	cmd.Argv = make([]string, len(rest))
	for j, w := range rest {
		cmd.Argv[j] = w.val
	}
	cmd.Argv[0] = base(cmd.Program)
	// A cd on the right of a pipe runs in a subshell and moves nothing. One on
	// the left does too, but the pipe is not read yet; lenient, it counts.
	if n := cmd.Argv[0]; (n == "cd" || n == "pushd") && cmd.Op != "|" && cmd.Op != "|&" {
		c.dir.path = cdTarget(cmd.Argv[1:], c.dir.path, p.st.home)
	}
}

func base(prog string) string {
	if !strings.Contains(prog, "/") {
		return prog
	}
	return filepath.Base(prog)
}

func cdTarget(args []string, dir, home string) string {
	for len(args) > 0 {
		a := args[0]
		if a == "--" {
			args = args[1:]
			break
		}
		if len(a) > 1 && a[0] == '-' {
			args = args[1:]
			continue
		}
		break
	}
	if len(args) == 0 {
		if home != "" {
			return home
		}
		return dir
	}
	if args[0] == "-" {
		return dir // the previous directory is not tracked
	}
	return resolve(dir, args[0])
}

// sudoArgOpts are sudo's options that take their value as the next word.
var sudoArgOpts = map[string]bool{
	"-u": true, "-g": true, "-h": true, "-p": true, "-C": true, "-D": true,
	"-r": true, "-t": true, "-U": true, "-T": true, "-R": true,
	"--user": true, "--group": true, "--host": true, "--prompt": true,
	"--close-from": true, "--chdir": true, "--role": true, "--type": true,
	"--other-user": true, "--command-timeout": true,
}

// stripPrefixes removes the programs that only run the rest of the argv:
// env (and its NAME=value words), command, sudo, nohup and time. `command -v`
// looks a name up instead of running it and `env -S` re-splits a string, so
// both stay as written.
func stripPrefixes(rest []word, cmd *Command) []word {
	for len(rest) > 0 {
		switch base(rest[0].val) {
		case "env":
			orig := rest
			rest = rest[1:]
			opaque := false
		envOpts:
			for len(rest) > 0 {
				a := rest[0].val
				switch {
				case a == "--":
					rest = rest[1:]
					break envOpts
				case a == "-S" || strings.HasPrefix(a, "-S") || strings.HasPrefix(a, "--split-string"):
					opaque = true
					break envOpts
				case a == "-u" || a == "--unset" || a == "-C" || a == "--chdir":
					rest = rest[min(2, len(rest)):]
				case len(a) > 0 && a[0] == '-':
					rest = rest[1:]
				default:
					break envOpts
				}
			}
			if opaque {
				return orig
			}
			for len(rest) > 0 && strings.Contains(rest[0].val, "=") && isAssignName(rest[0].val[:strings.IndexByte(rest[0].val, '=')]) {
				cmd.Assigns = append(cmd.Assigns, rest[0].val)
				rest = rest[1:]
			}
		case "command":
			if len(rest) > 1 && (rest[1].val == "-v" || rest[1].val == "-V") {
				return rest
			}
			rest = rest[1:]
			for len(rest) > 0 && (rest[0].val == "-p" || rest[0].val == "--") {
				rest = rest[1:]
			}
		case "sudo":
			rest = rest[1:]
		sudoOpts:
			for len(rest) > 0 {
				a := rest[0].val
				switch {
				case a == "--":
					rest = rest[1:]
					break sudoOpts
				case sudoArgOpts[a]:
					rest = rest[min(2, len(rest)):]
				case len(a) > 1 && a[0] == '-':
					rest = rest[1:]
				default:
					break sudoOpts
				}
			}
			for len(rest) > 0 && rest[0].assign {
				cmd.Assigns = append(cmd.Assigns, rest[0].val)
				rest = rest[1:]
			}
		case "nohup":
			rest = rest[1:]
			if len(rest) > 0 && rest[0].val == "--" {
				rest = rest[1:]
			}
		case "time":
			rest = rest[1:]
			for len(rest) > 0 && (rest[0].val == "-p" || rest[0].val == "--") {
				rest = rest[1:]
			}
		default:
			return rest
		}
	}
	return rest
}

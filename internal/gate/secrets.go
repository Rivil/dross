package gate

import (
	"fmt"
	"path"
	"strings"
	"unicode"

	"github.com/Rivil/dross/internal/secretpath"
	"github.com/Rivil/dross/internal/shellscan"
)

// The secret guards (c-6, c-7). Both are always-on (guard_scope): the leaks
// behind them — recovery codes printed by a masking regex, an age key printed
// by a merged stderr — happened outside dross repos, so a guard that only
// fired inside one would have stopped neither. Neither opens a file or spawns
// anything: they judge the command line and the path, never the contents.

func init() {
	Register(Gate{
		Name: "secret-stream", Scope: AlwaysOn, Liftable: true, Extensible: true,
		Claims: claimsSecretStream, Judge: judgeSecretStream,
	})
	Register(Gate{
		Name: "secret-read", Scope: AlwaysOn, Liftable: true, Extensible: true,
		Claims: claimsSecretRead, Judge: judgeSecretRead,
	})
}

// secretReaders are the Bash programs that print a file's contents.
var secretReaders = map[string]bool{"cat": true, "head": true, "tail": true, "less": true, "sed": true}

// rawTokens splits a command line into bare words for the partial-scan
// fallback: when the scanner could not read the whole line, a word naming a
// claimed program or path anywhere in it is enough to claim the call.
func rawTokens(line string) []string {
	return strings.FieldsFunc(line, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("'\"`;&|<>(){}$=", r)
	})
}

func inList(name string, list []string) bool {
	for _, s := range list {
		if s == name {
			return true
		}
	}
	return false
}

// ---- secret-stream ------------------------------------------------------

func claimsSecretStream(c *Call) bool {
	if c.ToolName != "Bash" {
		return false
	}
	s := c.Script()
	for _, cmd := range s.Commands {
		if inList(cmd.Name(), c.Lists.SecretTools) {
			return true
		}
	}
	if s.Partial {
		for _, tok := range rawTokens(c.Command()) {
			if inList(path.Base(tok), c.Lists.SecretTools) {
				return true
			}
		}
	}
	return false
}

func judgeSecretStream(c *Call) (*Refusal, error) {
	s := c.Script()
	if s.Partial {
		return NewRefusal(
			fmt.Sprintf("this line runs a secret-emitting tool, but its shell could not be read (%s), so where its output goes is unknown", s.Problem),
			"fix the quoting so the line parses, and redirect both streams: `<tool> … >file 2>/dev/null`")
	}
	for _, cmd := range s.Commands {
		if !inList(cmd.Name(), c.Lists.SecretTools) {
			continue
		}
		if cmd.Stdout.OffTranscript() && cmd.Stderr.OffTranscript() {
			continue
		}
		return NewRefusal(
			fmt.Sprintf("`%s` emits secret material, and this line leaves its output reachable by the transcript (stdout: %s, stderr: %s) — piping, an open stream and a merge into an open stdout all print it",
				cmd.Name(), cmd.Stdout, cmd.Stderr),
			fmt.Sprintf("send both streams away from the transcript — `%s … >file 2>/dev/null` — then check the file structurally (`wc -c`, `shasum -a 256`), never by printing it", cmd.Name()))
	}
	return nil, nil
}

// ---- secret-read --------------------------------------------------------

// SecretPattern returns the first pattern p matches, or "" — an env template
// (.env.example and kin) never matches. A pattern with a slash matches p's
// trailing path segments; one without matches its base name. The one
// implementation lives in secretpath, shared with the solo review context.
func SecretPattern(p string, patterns []string) string {
	return secretpath.Pattern(p, patterns)
}

// readerOperands are the paths a reader command opens: its non-option
// arguments and any file on its standard input.
func readerOperands(cmd shellscan.Command) []string {
	var out []string
	for _, a := range cmd.Args() {
		if !strings.HasPrefix(a, "-") {
			out = append(out, a)
		}
	}
	return append(out, cmd.Inputs()...)
}

func claimsSecretRead(c *Call) bool {
	switch c.ToolName {
	case "Read":
		return SecretPattern(c.FilePath(), c.Lists.SecretPaths) != ""
	case "Bash":
	default:
		return false
	}
	s := c.Script()
	for _, cmd := range s.Commands {
		if !secretReaders[cmd.Name()] {
			continue
		}
		for _, p := range readerOperands(cmd) {
			if SecretPattern(p, c.Lists.SecretPaths) != "" {
				return true
			}
		}
	}
	if s.Partial {
		reader, secret := false, false
		for _, tok := range rawTokens(c.Command()) {
			reader = reader || secretReaders[path.Base(tok)]
			secret = secret || SecretPattern(tok, c.Lists.SecretPaths) != ""
		}
		return reader && secret
	}
	return false
}

func judgeSecretRead(c *Call) (*Refusal, error) {
	const remedy = "check it structurally instead — `wc -c`, `shasum -a 256`, or `grep -q KEY <file>` for a yes/no — and `source` it to use its values; never print it"
	if c.ToolName == "Read" {
		p := c.FilePath()
		return NewRefusal(
			fmt.Sprintf("%s matches the secret path pattern `%s`; reading it puts its contents in the transcript", p, SecretPattern(p, c.Lists.SecretPaths)),
			remedy)
	}
	s := c.Script()
	if s.Partial {
		return NewRefusal(
			fmt.Sprintf("this line reads what looks like a secret file, but its shell could not be read (%s)", s.Problem),
			"fix the quoting so the line parses; then "+remedy)
	}
	for _, cmd := range s.Commands {
		if !secretReaders[cmd.Name()] {
			continue
		}
		for _, p := range readerOperands(cmd) {
			if pat := SecretPattern(p, c.Lists.SecretPaths); pat != "" {
				return NewRefusal(
					fmt.Sprintf("`%s %s` prints a file matching the secret path pattern `%s` into the transcript", cmd.Name(), p, pat),
					remedy)
			}
		}
	}
	return nil, nil
}

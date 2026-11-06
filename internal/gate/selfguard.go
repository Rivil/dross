package gate

import (
	"fmt"
	"path"
	"strconv"
	"strings"
)

// gate-off-guard keeps the off switch human (gate_override). An override the
// agent could run would be the first thing it reached for after a refusal,
// which would make every gate advisory. So `dross gate off`, arriving through
// the agent's Bash tool, is refused — and so are the hook-only verbs
// `dross gate check` and `dross gate record`: run by hand with a piped,
// forged payload, record would write an approval no human gave. A granting
// `dross trust` is refused for the same reason as `gate off`: a grant is the
// consent every repo-authored spawn and vouching lane rests on, so an agent
// that could write one would answer its own refusal. Its `--check` form only
// reads, and passes. The guard cannot be lifted; a human lifts gates from their
// own terminal, where no hook runs. It blocks these direct routes, and does not
// claim to stop a deliberate forgery.
func init() {
	Register(Gate{
		Name: "gate-off-guard", Scope: AlwaysOn, Liftable: false,
		Claims: func(c *Call) bool { return guardedGateVerb(c) != "" || grantingTrust(c) },
		Judge:  judgeGateOffGuard,
	})
}

// guardedVerbs are the `dross gate` subcommands the agent may not run.
var guardedVerbs = map[string]bool{"off": true, "check": true, "record": true}

// guardedGateVerb names the guarded `dross gate` verb the call runs, or "".
func guardedGateVerb(c *Call) string {
	if c.ToolName != "Bash" {
		return ""
	}
	s := c.Script()
	for _, cmd := range s.Commands {
		if cmd.Name() != "dross" {
			continue
		}
		var words []string
		for _, a := range cmd.Args() {
			if !strings.HasPrefix(a, "-") {
				words = append(words, a)
			}
		}
		if len(words) >= 2 && words[0] == "gate" && guardedVerbs[words[1]] {
			return words[1]
		}
	}
	if s.Partial {
		toks := rawTokens(c.Command())
		for i := 0; i+2 < len(toks); i++ {
			if path.Base(toks[i]) == "dross" && toks[i+1] == "gate" && guardedVerbs[toks[i+2]] {
				return toks[i+2]
			}
		}
	}
	return ""
}

// trustValueFlags are the `dross trust` flags that take the next word as their
// value, so `--lane --check` names a lane called "--check" and still grants. A
// new value-taking flag must join this list: until it does, `--new --check`
// reads as a check.
var trustValueFlags = map[string]bool{"--lane": true, "--lane-install": true, "--run": true, "--replay": true}

// trustGrants reports whether `dross trust` given args would write a grant:
// every form does, bar a true `--check` (the last one wins, as in pflag) or a
// help request. So a new boolean granting flag refuses with no edit here.
func trustGrants(args []string) bool {
	check := false
	for i := 0; i < len(args); i++ {
		name, val, hasVal := strings.Cut(args[i], "=")
		switch {
		case args[i] == "--":
			return !check
		case args[i] == "--help" || args[i] == "-h":
			return false
		case name == "--check":
			on, err := strconv.ParseBool(val)
			check = !hasVal || (err == nil && on)
		case trustValueFlags[name] && !hasVal:
			i++
		}
	}
	return !check
}

// trustArgs returns the flags of a `dross trust` line, verb removed, and false
// for any other dross line. Cobra finds the verb past flags, so `dross --lane x
// trust` is the same line as `dross trust --lane x`.
func trustArgs(args []string) ([]string, bool) {
	for i := 0; i < len(args); i++ {
		switch {
		case trustValueFlags[args[i]]:
			i++
		case strings.HasPrefix(args[i], "-"):
		case args[i] == "trust":
			return append(args[:i:i], args[i+1:]...), true
		default:
			return nil, false
		}
	}
	return nil, false
}

// grantingTrust reports whether the call runs a `dross trust` that writes a
// grant. On a line the scanner could not finish, any `dross trust` counts:
// rawTokens splits on `=`, so `--check=false` cannot be told from `--check`.
func grantingTrust(c *Call) bool {
	if c.ToolName != "Bash" {
		return false
	}
	s := c.Script()
	for _, cmd := range s.Commands {
		if cmd.Name() != "dross" {
			continue
		}
		if args, ok := trustArgs(cmd.Args()); ok && trustGrants(args) {
			return true
		}
	}
	if s.Partial {
		toks := rawTokens(c.Command())
		for i := 0; i+1 < len(toks); i++ {
			if path.Base(toks[i]) == "dross" && toks[i+1] == "trust" {
				return true
			}
		}
	}
	return false
}

func judgeGateOffGuard(c *Call) (*Refusal, error) {
	verb := guardedGateVerb(c)
	if verb == "" {
		return NewRefusal(
			"`dross trust` writes a consent grant, and only a human may grant one — from their own terminal, never through the agent's Bash tool",
			"stop and show the user the exact line the grant covers, then let them run `dross trust` in their own terminal; `dross trust --check` only reads, and passes")
	}
	if verb == "off" {
		return NewRefusal(
			"`dross gate off` lifts a gate, and only a human may lift one — from their own terminal, never through the agent's Bash tool",
			"stop and tell the user which gate refused and why; if the gate is wrong here, they can lift it with `dross gate off <name>` in their own terminal")
	}
	return NewRefusal(
		fmt.Sprintf("`dross gate %s` is a hook-only verb: Claude Code runs it with the real tool-call payload, and a hand-run one could forge a payload — a green or an approval nobody gave", verb),
		"do not run the gate's hook verbs yourself; make the tool call itself and let the hooks judge and record it")
}

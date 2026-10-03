package gate

import (
	"fmt"
	"path"
	"strings"
)

// gate-off-guard keeps the off switch human (gate_override). An override the
// agent could run would be the first thing it reached for after a refusal,
// which would make every gate advisory. So `dross gate off`, arriving through
// the agent's Bash tool, is refused — and so are the hook-only verbs
// `dross gate check` and `dross gate record`: run by hand with a piped,
// forged payload, record would write an approval no human gave. The guard
// cannot be lifted; a human lifts gates from their own terminal, where no hook
// runs.
func init() {
	Register(Gate{
		Name: "gate-off-guard", Scope: AlwaysOn, Liftable: false,
		Claims: func(c *Call) bool { return guardedGateVerb(c) != "" },
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

func judgeGateOffGuard(c *Call) (*Refusal, error) {
	verb := guardedGateVerb(c)
	if verb == "off" {
		return NewRefusal(
			"`dross gate off` lifts a gate, and only a human may lift one — from their own terminal, never through the agent's Bash tool",
			"stop and tell the user which gate refused and why; if the gate is wrong here, they can lift it with `dross gate off <name>` in their own terminal")
	}
	return NewRefusal(
		fmt.Sprintf("`dross gate %s` is a hook-only verb: Claude Code runs it with the real tool-call payload, and a hand-run one could forge a payload — a green or an approval nobody gave", verb),
		"do not run the gate's hook verbs yourself; make the tool call itself and let the hooks judge and record it")
}

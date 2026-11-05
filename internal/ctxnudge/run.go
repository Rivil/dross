package ctxnudge

import (
	"encoding/json"

	"github.com/Rivil/dross/internal/gate"
	"github.com/Rivil/dross/internal/gatestate"
)

// eventPostToolUse is the one hook event the nudge answers.
const eventPostToolUse = "PostToolUse"

// Env is what Run needs beyond the payload; the hook verb supplies it. An
// interface rather than a struct of funcs, so the call graph resolves each call
// to the one concrete env that flows here.
type Env interface {
	// Threshold returns the effective threshold in tokens; 0 is off. An error
	// (an undecodable or negative setting) silences the nudge.
	Threshold() (int64, error)
	// Reentry returns the re-entry command a fresh session's SessionStart line
	// will print for root. It runs git, so Run calls it only once a nudge is
	// due, never on the below-threshold or already-nudged path.
	Reentry(root string) string
}

// Run answers one PostToolUse hook payload: the envelope that injects the
// nudge line, or nil. Every reason not to nudge — another event, a subagent's
// call, no dross repo, the nudge off, no readable transcript, below the
// threshold, a band already nudged — is nil, and so is a panic: the hook must
// never block or break a tool call.
//
// The line is composed before the band is claimed, so a failed compose leaves
// the band unclaimed and the next fire tries again.
func Run(payload []byte, env Env) (out []byte) {
	defer func() {
		if recover() != nil {
			out = nil
		}
	}()

	// A narrow decode by key, as gate.Decode reads its payload: only these
	// fields are read, and the tool's input and response are never decoded.
	var top map[string]json.RawMessage
	if json.Unmarshal(payload, &top) != nil || top == nil {
		return nil
	}
	if field(top, "hook_event_name") != eventPostToolUse {
		return nil
	}
	// A call made inside a subagent carries its agent_id, but its
	// transcript_path names the main session's transcript — so the transcript
	// cannot tell the two apart, and only this field can.
	if field(top, "agent_id") != "" {
		return nil
	}
	session := field(top, "session_id")
	if session == "" {
		return nil
	}
	root, err := gate.LocateRoot(field(top, "cwd"))
	if err != nil || root == "" {
		return nil
	}
	threshold, err := env.Threshold()
	if err != nil || threshold <= 0 {
		return nil
	}
	tokens, ok := LatestMainContext(field(top, "transcript_path"))
	if !ok {
		return nil
	}
	band := Band(tokens, threshold)
	if band == 0 || gatestate.NudgeClaimed(root, session, band) {
		return nil
	}
	line := Line(tokens, threshold, env.Reentry(root))
	if won, err := gatestate.ClaimNudge(root, session, band); err != nil || !won {
		return nil
	}
	b, err := json.Marshal(envelope{
		SystemMessage: line,
		HookSpecificOutput: hookOutput{
			HookEventName:     eventPostToolUse,
			AdditionalContext: line,
		},
	})
	if err != nil {
		return nil
	}
	return b
}

// envelope is the hook's stdout: systemMessage shows the line to the user,
// additionalContext hands the same line to the model. It carries no decision,
// continue or permission key, so it can never block anything.
type envelope struct {
	SystemMessage      string     `json:"systemMessage"`
	HookSpecificOutput hookOutput `json:"hookSpecificOutput"`
}

type hookOutput struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext"`
}

// field returns top[key] as a string, "" when absent or not a string.
func field(top map[string]json.RawMessage, key string) string {
	var s string
	if raw, ok := top[key]; ok {
		_ = json.Unmarshal(raw, &s)
	}
	return s
}

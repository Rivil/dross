package gate

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Rivil/dross/internal/gatestate"
	"github.com/Rivil/dross/internal/review"
)

// The solo-review recorder (review_pass_signal). A pass is recorded only here,
// from Claude Code's own hook events for the reviewer agent's spawn: the
// verdict is the agent's final reply, which the executing agent did not
// write. No CLI verb records a review, and tamper-guard keeps the ledger out
// of every tool call's reach — so the commit gate's pass cannot be forged by
// the agent it gates.
//
// A foreground spawn completes inside the Agent call, so its PostToolUse
// carries the reply and yields a round at once. A background spawn — the only
// kind an interactive session makes — reports just a launch: its PostToolUse
// records a pending launch (the agent's id, the digest its prompt named, the
// tree that context was built from), and the same agent's SubagentStop
// delivers the reply that turns it into a round. Either way the round is bound
// to the tree its context was built from:
//
//   - pass / block — the reviewer's parsed verdict over the context whose
//     digest the prompt names, regenerated here and found equal;
//   - stale — the prompt names no context, or one the tree has moved past;
//     it counts for nothing and warns, naming `dross review context`;
//   - unavailable — the reviewer ran but its verdict cannot be trusted: it
//     does not parse or contradicts itself, or its prompt carried text beyond
//     the context line (c-5 — the reviewer sees only the context). Sticky.
func init() {
	RegisterRecorder(Recorder{
		Name: "solo-review", Scope: Workflow,
		Claims: claimsReviewer,
		Record: recordReview,
	})
}

// claimsReviewer claims the reviewer's spawns, under either name Claude Code
// has given the subagent tool, and the reviewer agent's SubagentStop.
func claimsReviewer(c *Call) bool {
	if c.Event == EventSubagentStop {
		return c.AgentType == review.ReviewerAgent
	}
	return (c.ToolName == "Agent" || c.ToolName == "Task") && c.Field("subagent_type") == review.ReviewerAgent
}

func recordReview(c *Call) error {
	root, err := c.Root()
	if err != nil || root == "" {
		return err
	}
	scope, err := ArmedScope(root)
	if err != nil || scope == nil {
		// Pair mode, no task in progress, off the phase branch: the human is
		// the gate and nothing is recorded.
		return err
	}
	rec, err := gatestate.LoadReview(root)
	if err != nil {
		return err
	}
	if !scope.Matches(rec) {
		rec = &gatestate.Review{Kind: scope.Kind, Phase: scope.Phase, Task: scope.Task, Attempt: scope.Attempt}
	}

	var warn error
	if c.Event == EventSubagentStop {
		i := pendingIndex(rec.Pending, c.AgentID)
		if i < 0 {
			// Not a launch this scope is waiting on — an earlier attempt's
			// reviewer, or one spawned while nothing was armed.
			return nil
		}
		l := rec.Pending[i]
		rec.Pending = append(rec.Pending[:i:i], rec.Pending[i+1:]...)
		rec.Rounds = append(rec.Rounds, verdictRound(c.LastMessage, scope, l.Digest, l.Tree))
	} else {
		var resp map[string]json.RawMessage
		_ = json.Unmarshal(c.Response, &resp)
		var status, agentID string
		_ = json.Unmarshal(resp["status"], &status)
		_ = json.Unmarshal(resp["agentId"], &agentID)
		if status != "completed" && status != "async_launched" {
			return fmt.Errorf("the %s spawn reported status %q — neither a completed review nor a launch, so nothing was recorded", review.ReviewerAgent, status)
		}
		digest, tree, bad, w := judgeLaunch(c, root, scope)
		warn = w
		switch {
		case bad != nil:
			rec.Rounds = append(rec.Rounds, *bad)
		case status == "completed":
			rec.Rounds = append(rec.Rounds, verdictRound(reviewerReply(resp), scope, digest, tree))
		case agentID == "":
			rec.Rounds = append(rec.Rounds, review.Round{Outcome: review.OutcomeUnavailable, Digest: digest,
				Cause: "the background reviewer's launch reported no agent id, so its verdict cannot be joined to it"})
		default:
			rec.Pending = append(rec.Pending, gatestate.Launch{AgentID: agentID, Digest: digest, Tree: tree})
		}
	}
	if err := gatestate.SaveReview(root, *rec); err != nil {
		return err
	}
	return warn
}

func pendingIndex(pending []gatestate.Launch, agentID string) int {
	for i, l := range pending {
		if agentID != "" && l.AgentID == agentID {
			return i
		}
	}
	return -1
}

// judgeLaunch checks a spawn's prompt against the context it must name. It
// returns the digest and tree a verdict will be bound to, or the terminal
// round the spawn already earned — unavailable for a widened prompt or a
// context that cannot be rebuilt, stale (with a warning) for a prompt naming
// no context or one the tree has moved past.
func judgeLaunch(c *Call, root string, scope *ReviewScope) (string, string, *review.Round, error) {
	prompt := c.Field("prompt")
	digest, ok := review.ParsePromptLine(prompt)
	if !ok {
		if strings.Contains(prompt, review.ContextPath) {
			return "", "", &review.Round{Outcome: review.OutcomeUnavailable,
				Cause: "the reviewer's prompt carried text beyond the context line — it must see only the context (c-5), so its verdict cannot be trusted"}, nil
		}
		return "", "", &review.Round{Outcome: review.OutcomeStale, Cause: "the reviewer's prompt names no review context"},
			errors.New("the reviewer was spawned without a review context: run `dross review context` and spawn it with the printed prompt — nothing counted")
	}
	cs, err := ContextScope(root, c.Home, scope)
	if err != nil {
		return "", "", &review.Round{Outcome: review.OutcomeUnavailable, Cause: "the review context could not be rebuilt: " + err.Error()}, nil
	}
	ctx, err := review.BuildContext(root, cs, ReviewSecrets(c.Home))
	if errors.Is(err, review.ErrNoCode) || (err == nil && ctx.Digest != digest) {
		return "", "", &review.Round{Outcome: review.OutcomeStale, Digest: digest, Cause: "the tree changed after `dross review context`"},
			errors.New("the tree changed after `dross review context`, so the reviewer judged a context that no longer matches it — re-run `dross review context` and the reviewer; nothing counted")
	}
	if err != nil {
		return "", "", &review.Round{Outcome: review.OutcomeUnavailable, Cause: "the review context could not be rebuilt: " + err.Error()}, nil
	}
	return digest, ctx.Tree, nil, nil
}

// verdictRound turns the reviewer's reply into a round bound to tree.
func verdictRound(reply string, scope *ReviewScope, digest, tree string) review.Round {
	v, err := review.ParseVerdict(reply, scope.Kind)
	if err != nil {
		return review.Round{Outcome: review.OutcomeUnavailable, Tree: tree, Digest: digest, Cause: "the reviewer's verdict did not parse: " + err.Error()}
	}
	outcome := review.OutcomeBlock
	if v.Pass {
		outcome = review.OutcomePass
	}
	return review.Round{Outcome: outcome, Tree: tree, Digest: digest, Spec: v.Spec, Quality: v.Quality}
}

// reviewerReply joins the text blocks of a completed Agent call's tool_response.
func reviewerReply(resp map[string]json.RawMessage) string {
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	_ = json.Unmarshal(resp["content"], &blocks)
	var b strings.Builder
	for _, blk := range blocks {
		if blk.Type == "text" {
			b.WriteString(blk.Text)
		}
	}
	return b.String()
}

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
// from the PostToolUse payload of the reviewer agent's own spawn: the verdict
// is the reply in that call's tool_response, which the executing agent did
// not write. No CLI verb records a review, and tamper-guard keeps the ledger
// out of every tool call's reach — so the commit gate's pass cannot be forged
// by the agent it gates.
//
// Each claimed spawn appends one round to the armed scope's ledger, bound to
// the tree its context was built from:
//
//   - pass / block — the reviewer's parsed verdict over the context whose
//     digest the prompt names, regenerated here and found equal;
//   - stale — the prompt names no context, or one the tree has moved past;
//     it counts for nothing and warns, naming `dross review context`;
//   - unavailable — the reviewer ran but its verdict cannot be trusted: it
//     does not parse or contradicts itself, or its prompt carried text beyond
//     the context line (c-5 — the reviewer sees only the context). Sticky.
//
// A background spawn's PostToolUse is only a launch acknowledgement (the
// captured fixtures pin this), so it records nothing and warns.
func init() {
	RegisterRecorder(Recorder{
		Name: "solo-review", Scope: Workflow,
		Claims: claimsReviewer,
		Record: recordReview,
	})
}

// claimsReviewer claims the reviewer's spawns, under either name Claude Code
// has given the subagent tool.
func claimsReviewer(c *Call) bool {
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
	var resp map[string]json.RawMessage
	_ = json.Unmarshal(c.Response, &resp)
	var status string
	_ = json.Unmarshal(resp["status"], &status)
	if status != "completed" {
		return fmt.Errorf("the %s spawn did not complete inside the tool call (status %q) — a background launch never carries the verdict, so nothing was recorded; spawn it in the foreground with run_in_background: false",
			review.ReviewerAgent, status)
	}

	rec, err := gatestate.LoadReview(root)
	if err != nil {
		return err
	}
	if !scope.Matches(rec) {
		rec = &gatestate.Review{Kind: scope.Kind, Phase: scope.Phase, Task: scope.Task, Attempt: scope.Attempt}
	}
	round, warn := judgeRound(c, root, scope, resp)
	rec.Rounds = append(rec.Rounds, round)
	if err := gatestate.SaveReview(root, *rec); err != nil {
		return err
	}
	return warn
}

// judgeRound turns one completed spawn into a round, and a warning when the
// round is stale.
func judgeRound(c *Call, root string, scope *ReviewScope, resp map[string]json.RawMessage) (review.Round, error) {
	prompt := c.Field("prompt")
	digest, ok := review.ParsePromptLine(prompt)
	if !ok {
		if strings.Contains(prompt, review.ContextPath) {
			return review.Round{Outcome: review.OutcomeUnavailable,
				Cause: "the reviewer's prompt carried text beyond the context line — it must see only the context (c-5), so its verdict cannot be trusted"}, nil
		}
		return review.Round{Outcome: review.OutcomeStale, Cause: "the reviewer's prompt names no review context"},
			errors.New("the reviewer was spawned without a review context: run `dross review context` and spawn it with the printed prompt — nothing counted")
	}
	cs, err := ContextScope(root, c.Home, scope)
	if err != nil {
		return review.Round{Outcome: review.OutcomeUnavailable, Cause: "the review context could not be rebuilt: " + err.Error()}, nil
	}
	ctx, err := review.BuildContext(root, cs, ReviewSecrets(c.Home))
	if errors.Is(err, review.ErrNoCode) || (err == nil && ctx.Digest != digest) {
		return review.Round{Outcome: review.OutcomeStale, Digest: digest, Cause: "the tree changed after `dross review context`"},
			errors.New("the tree changed after `dross review context`, so the reviewer judged a context that no longer matches it — re-run `dross review context` and the reviewer; nothing counted")
	}
	if err != nil {
		return review.Round{Outcome: review.OutcomeUnavailable, Cause: "the review context could not be rebuilt: " + err.Error()}, nil
	}
	v, err := review.ParseVerdict(reviewerReply(resp), scope.Kind)
	if err != nil {
		return review.Round{Outcome: review.OutcomeUnavailable, Tree: ctx.Tree, Digest: digest, Cause: "the reviewer's verdict did not parse: " + err.Error()}, nil
	}
	outcome := review.OutcomeBlock
	if v.Pass {
		outcome = review.OutcomePass
	}
	return review.Round{Outcome: outcome, Tree: ctx.Tree, Digest: digest, Spec: v.Spec, Quality: v.Quality}, nil
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

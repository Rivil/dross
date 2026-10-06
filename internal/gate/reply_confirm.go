package gate

import (
	"errors"
	"regexp"
	"strconv"

	"github.com/Rivil/dross/internal/gatestate"
)

// reply-approval (c-5): `dross pr reply <pr> --post` posts the drafted reply
// only when a human approved exactly that draft. The approval is an
// AskUserQuestion answer that is exactly ReplyApproveLabel(pr, digest), where
// digest is the token the draft run printed for the body it showed; the
// recorder writes it to .dross/gate/reply.json, which tamper-guard keeps every
// tool call from writing. So the confirm lives in Go: the agent can print the
// label, but only the human choosing it records anything.
//
// Every other answer records nothing — a different case, trailing text, a
// freeform reply, a reject — and so does the label appearing only in
// tool_input, which is what Claude sent rather than what the human chose.
func init() {
	RegisterRecorder(Recorder{
		Name: "reply-approval", Scope: Workflow,
		Claims: func(c *Call) bool { return c.ToolName == "AskUserQuestion" },
		Record: recordReplyApproval,
	})
}

// ReplyApproveLabel is the AskUserQuestion option label that approves posting
// the reply drafted for pr, whose body digests to digest.
func ReplyApproveLabel(pr int, digest string) string {
	return "post reply #" + strconv.Itoa(pr) + " " + digest
}

// replyLabel is a well-formed ReplyApproveLabel: a positive PR number and a
// lower-case sha256 hex digest, nothing before or after.
var replyLabel = regexp.MustCompile(`^post reply #([1-9][0-9]*) ([0-9a-f]{64})$`)

// recordReplyApproval records the human's approval when exactly one chosen
// answer is a well-formed reply label. A response with no readable answers is
// a warning, not silence: /dross-respond runs outside any execute task, where
// the pair-approval recorder never reads the response, so a hook-schema change
// would otherwise stop approvals recording with nobody told. Two different
// reply labels in one response record nothing — which was meant is ambiguous,
// and the answers come back in no fixed order.
func recordReplyApproval(c *Call) error {
	root, err := c.Root()
	if err != nil || root == "" {
		return err
	}
	answers, err := chosenLabels(c.Response)
	if err != nil {
		return err
	}
	var approval *gatestate.ReplyApproval
	for _, a := range answers {
		m := replyLabel.FindStringSubmatch(a)
		if m == nil {
			continue
		}
		pr, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		if approval != nil && (approval.PR != pr || approval.Digest != m[2]) {
			return errors.New("one AskUserQuestion answer approves two different PR replies, so which one is meant is ambiguous — no reply approval was recorded")
		}
		approval = &gatestate.ReplyApproval{PR: pr, Digest: m[2], At: c.Now.UTC()}
	}
	if approval == nil {
		return nil
	}
	return gatestate.SaveReplyApproval(root, *approval)
}

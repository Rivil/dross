package boardsync

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/Rivil/dross/internal/forge"
)

// collectInbound returns the board's inbound triage feed: issues matching the
// filter, minus every issue dross authored — one linked in any board.json
// namespace, one carrying the dross marker label — and minus those dismissed. It is
// deliberately MARK-FREE — it never stamps last_pull — so read-only callers
// (dross watch, dross status) share one filter path with `issue pull` and can
// never re-introduce a board mutation. The filter's State scopes the feed;
// callers wanting reopen-resurfaces semantics pass State:"open".
func CollectInbound(ctx *Ctx, filter forge.IssueFilter) ([]forge.Issue, error) {
	issues, err := ctx.Client.ListIssues(filter)
	if err != nil {
		return nil, Wrap(err)
	}
	var inbound []forge.Issue
	for _, iss := range issues {
		// HasMarker is the second exclusion basis (exclusion_basis lock):
		// board.json is branch-local, so a mirror created on a phase branch
		// that never merged is invisible to IsLinked, while the marker label
		// travels with the issue itself. Hiding a human-filed issue somebody
		// tagged `dross` is the cheaper error.
		if ctx.Board.IsLinked(iss.Key) || ctx.Board.IsDismissed(iss.Key) || HasMarker(iss) {
			continue
		}
		inbound = append(inbound, iss)
	}
	return inbound, nil
}

// PullEnvelope is the `issue pull --json` shape. It exists so a board that
// could not be reached is distinguishable from a board with nothing on it: a
// bare array collapses both onto `[]`, and every prompt then reports zero
// inbound issues for a tracker that is simply down.
//
// Issues is never null — an empty feed is `[]` — and Error is null on success.
type PullEnvelope struct {
	Issues []forge.Issue `json:"issues"`
	Error  *string       `json:"error"`
}

// emitPullEnvelope marshals and prints the envelope. Issues is normalised to a
// non-nil slice so consumers can index it without a null check.
func EmitPullEnvelope(w io.Writer, issues []forge.Issue, boardErr error) error {
	env := PullEnvelope{Issues: issues}
	if env.Issues == nil {
		env.Issues = []forge.Issue{}
	}
	if boardErr != nil {
		msg := boardErr.Error()
		env.Error = &msg
	}
	out, err := json.Marshal(env)
	if err != nil {
		return err
	}
	fmt.Fprintln(w, string(out))
	return nil
}

// reportBoardFailure delivers a pull failure the way the caller asked to
// receive it.
//
// Under --json the answer is always the envelope with a non-null .error and a
// zero exit, because that is the contract status.md and inbox.md publish and
// the only shape a `jq .issues` consumer survives. A consumer that dies on a
// parse error reports nothing at all, which is strictly worse than a named
// failure in a field the prompt already prints — including for the tracked
// local.toml refusal, whose own wording travels intact so it still reads as
// something to fix rather than an outage to wait out.
//
// Human mode is deliberately NOT changed by this phase. A fetch failure stays a
// printed no-op, because the workflow prompts call `dross issue …`
// unconditionally on the promise that it is safe. A setup failure stays fatal
// (humanFatal), because a person who typed the command wants to know their
// token is unset, and the seven other openBoard callers exit non-zero on
// exactly these conditions — only the machine-facing --json contract was broken.
func ReportBoardFailure(w io.Writer, asJSON, humanFatal bool, boardErr error) error {
	if asJSON {
		return EmitPullEnvelope(w, nil, boardErr)
	}
	if humanFatal {
		return boardErr
	}
	fmt.Fprintf(w, "board unreachable: %v\n", boardErr)
	return nil
}

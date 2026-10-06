package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/gate"
	"github.com/Rivil/dross/internal/gatestate"
	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/prtriage"
	"github.com/Rivil/dross/internal/ship"
)

func prReply() *cobra.Command {
	var post bool
	c := &cobra.Command{
		Use:   "reply <pr>",
		Short: "Draft the one reply listing a phase PR's rejected comments; --post sends it once a human approved that draft",
		Long: "Prints the reply for PR <pr> — every rejected comment not yet posted, with its reason — " +
			"and the exact AskUserQuestion option label that approves this draft. --post sends it, " +
			"once, only when a human's answer choosing that label is recorded: the agent cannot " +
			"approve its own reply.",
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			t, err := loadPRThread(args[0])
			if err != nil {
				return err
			}
			out := c.OutOrStdout()
			body, ok := prtriage.ReplyBody(t.rec, t.pr)
			if !ok {
				fmt.Fprintf(out, "nothing to reply: no rejected comment on PR #%d is waiting to be posted\n", t.pr)
				return nil
			}
			label := gate.ReplyApproveLabel(t.pr, prtriage.ReplyDigest(body))
			if !post {
				fmt.Fprintf(out, "%s\n---\nTo post this reply, ask with AskUserQuestion offering exactly this option label:\n  %s\nthen run `dross pr reply %d --post`.\n", body, label, t.pr)
				return nil
			}
			return postReply(c, t, body, label)
		},
	}
	c.Flags().BoolVar(&post, "post", false, "post the drafted reply (needs a recorded human approval of exactly this draft)")
	return c
}

// postReply sends body once, and only on a recorded approval of exactly this
// PR's current draft. A failed post keeps the approval for a retry; a
// successful one spends it before the record marks the rejects posted.
func postReply(c *cobra.Command, t *prThread, body, label string) error {
	appr, err := gatestate.LoadReplyApproval(t.repoDir)
	if err != nil {
		return err
	}
	digest := prtriage.ReplyDigest(body)
	if appr == nil || appr.PR != t.pr || appr.Digest != digest {
		return fmt.Errorf("a recorded human approval of this exact draft is required, and none matches — ask with AskUserQuestion offering exactly %q, then re-run with --post (recording the answer needs the dross hooks: `dross hooks ensure`)", label)
	}
	// Everything the record save after the post needs is checked before the
	// post: a reply that went out but could not be marked posted would go
	// out again on the next --post.
	var ids []string
	for _, r := range t.rec.Resolution {
		if r.PR == t.pr && r.Verdict == prtriage.VerdictReject && !r.Posted {
			ids = append(ids, r.ID)
		}
	}
	sort.Strings(ids)
	refs, err := phaseRefs(t)
	if err != nil {
		return err
	}
	marked := prtriage.Record{Resolution: append([]prtriage.Resolution(nil), t.rec.Resolution...)}
	prtriage.MarkPosted(&marked, ids)
	if errs := prtriage.Validate(marked, refs); len(errs) > 0 {
		return fmt.Errorf("%s is invalid, so the reply could not be marked posted — fix it (`dross validate` names each problem) before posting: %w", prtriage.File, errors.Join(errs...))
	}

	p, _, err := loadProject()
	if err != nil {
		return err
	}
	hosts, err := remotePolicy(t.root, t.repoDir, p)
	if err != nil {
		return err
	}
	opts := buildCommentOpts(p, hosts)
	opts.PRNumber, opts.Body = t.pr, body
	if err := ship.PostComment(opts); err != nil {
		return fmt.Errorf("post the reply to PR #%d: %w — nothing was marked posted; the approval stands for a retry", t.pr, err)
	}
	// Posted: from here every step still runs, so a re-run finds nothing
	// left to send even when one of them fails.
	spendErr := gatestate.ClearReplyApproval(t.repoDir)
	beforeTriageSave(t.recPath)
	saveErr := prtriage.Save(t.recPath, t.recBytes, marked, refs)
	switch {
	case saveErr != nil:
		return fmt.Errorf("the reply was posted to PR #%d, but marking %s posted in %s failed: %w — set posted = true on those entries by hand, or the next reply repeats them",
			t.pr, strings.Join(ids, ", "), prtriage.File, saveErr)
	case spendErr != nil:
		return fmt.Errorf("the reply was posted to PR #%d and %s marked posted, but spending its approval failed: %w", t.pr, strings.Join(ids, ", "), spendErr)
	}
	fmt.Fprintf(c.OutOrStdout(), "posted the reply to PR #%d (%s) and marked them posted\n", t.pr, strings.Join(ids, ", "))
	return nil
}

// phaseRefs are the task and deferred ids the PR phase's record may point at.
func phaseRefs(t *prThread) (prtriage.Refs, error) {
	dir, err := phase.ContainID(t.root, t.phase)
	if err != nil {
		return prtriage.Refs{}, err
	}
	plan, err := phase.LoadPlan(filepath.Join(dir.String(), "plan.toml"))
	if err != nil {
		return prtriage.Refs{}, err
	}
	spec := &phase.Spec{}
	specPath := filepath.Join(dir.String(), "spec.toml")
	if _, err := os.Stat(specPath); err == nil {
		if spec, err = phase.LoadSpec(specPath); err != nil {
			return prtriage.Refs{}, err
		}
	}
	return refsOf(plan, spec), nil
}

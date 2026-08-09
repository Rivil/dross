package ship

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Rivil/dross/internal/redact"
)

// --- Forgejo / Gitea via REST ---

// forgejoTarget resolves the credential+addressing preamble every
// Forgejo/Gitea REST call needs: the token and the owner/repo split out of
// the canonical repo URL.
func forgejoTarget(opts OpenOpts) (owner, repo, token string, err error) {
	if opts.APIBase == "" {
		return "", "", "", errors.New("forgejo backend needs APIBase (set [remote].api_base)")
	}
	if opts.AuthEnv == "" {
		return "", "", "", errors.New("forgejo backend needs AuthEnv (set [remote].auth_env)")
	}
	token, err = resolveToken(opts.APIBase, opts.AuthEnv, opts.Hosts)
	if err != nil {
		return "", "", "", err
	}
	owner, repo, err = splitOwnerRepo(opts.URL)
	if err != nil {
		return "", "", "", err
	}
	return owner, repo, token, nil
}

func openForgejoPR(opts OpenOpts) (*OpenResult, error) {
	owner, repo, token, err := forgejoTarget(opts)
	if err != nil {
		return nil, err
	}

	body := map[string]any{
		"title": opts.Title,
		"body":  opts.Body,
		"head":  opts.HeadBranch,
		"base":  opts.BaseBranch,
	}
	if opts.Draft {
		// Forgejo / Gitea support draft via the title prefix convention.
		// REST API doesn't accept a "draft" boolean; titles starting with
		// "WIP:" or "Draft:" are treated as drafts in the UI.
		body["title"] = "Draft: " + opts.Title
	}

	endpoint := strings.TrimRight(opts.APIBase, "/") + fmt.Sprintf("/repos/%s/%s/pulls", owner, repo)
	resp, err := jsonPost(endpoint, opts.AuthEnv, token, body)
	if err != nil {
		return nil, fmt.Errorf("create PR: %w", err)
	}
	num, _ := resp["number"].(float64)
	htmlURL, _ := resp["html_url"].(string)
	if num == 0 {
		return nil, fmt.Errorf("forgejo response missing number: %v", resp)
	}

	if len(opts.Reviewers) > 0 {
		revEndpoint := strings.TrimRight(opts.APIBase, "/") +
			fmt.Sprintf("/repos/%s/%s/pulls/%d/requested_reviewers", owner, repo, int(num))
		if _, err := jsonPost(revEndpoint, opts.AuthEnv, token, map[string]any{
			"reviewers": opts.Reviewers,
		}); err != nil {
			// Don't fail the whole ship for reviewer-assignment trouble — the PR is open.
			return &OpenResult{Number: int(num), URL: htmlURL}, fmt.Errorf("PR opened (#%d) but reviewer request failed: %w", int(num), err)
		}
	}

	return &OpenResult{Number: int(num), URL: htmlURL}, nil
}

// forgejoPRStatus asks GET /repos/{owner}/{repo}/pulls/{index} and reports
// the "merged" boolean plus base.ref. Gitea reports a merged PR as
// state "closed" — reading state alone would call a landed PR unmerged — so
// merged comes from the "merged" field, never derived from state.
func forgejoPRStatus(opts OpenOpts) (PRStatus, error) {
	if opts.PRNumber <= 0 {
		return PRStatus{}, errors.New("forgejo merged-status lookup needs a PR number")
	}
	owner, repo, token, err := forgejoTarget(opts)
	if err != nil {
		return PRStatus{}, err
	}
	endpoint := strings.TrimRight(opts.APIBase, "/") + fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, opts.PRNumber)
	respBody, status, err := jsonGet(endpoint, opts.AuthEnv, token)
	if err != nil {
		return PRStatus{}, fmt.Errorf("get PR #%d: %w", opts.PRNumber, err)
	}
	if status >= 300 {
		return PRStatus{}, fmt.Errorf("get PR #%d: HTTP %d: %s", opts.PRNumber, status, string(respBody))
	}
	var pr struct {
		Merged bool `json:"merged"`
		Base   struct {
			Ref string `json:"ref"`
		} `json:"base"`
	}
	if err := json.Unmarshal(respBody, &pr); err != nil {
		return PRStatus{}, fmt.Errorf("parse PR #%d: %w", opts.PRNumber, err)
	}
	return PRStatus{Merged: pr.Merged, BaseRef: pr.Base.Ref}, nil
}

// forgejoOpenPRsTargeting lists the open PRs whose base.ref is base,
// paginating through Gitea/Forgejo's list-pulls endpoint (default 30/page)
// and filtering client-side: the endpoint has no base= query parameter, so a
// naive query filter would silently include every base's open PRs.
//
// A lookup failure is always an error, never an empty or partial slice: an
// empty slice reads as "no dependents" and authorizes an irreversible branch
// delete.
func forgejoOpenPRsTargeting(opts OpenOpts, base string) ([]BasePR, error) {
	owner, repo, token, err := forgejoTarget(opts)
	if err != nil {
		return nil, err
	}

	const limit = 50
	var out []BasePR
	for page := 1; ; page++ {
		endpoint := strings.TrimRight(opts.APIBase, "/") + fmt.Sprintf(
			"/repos/%s/%s/pulls?state=open&page=%d&limit=%d", owner, repo, page, limit)
		respBody, status, err := jsonGet(endpoint, opts.AuthEnv, token)
		if err != nil {
			return nil, fmt.Errorf("list open PRs targeting %s: %w", base, err)
		}
		if status >= 300 {
			return nil, fmt.Errorf("list open PRs targeting %s: HTTP %d: %s", base, status, string(respBody))
		}
		var prs []struct {
			Number  int    `json:"number"`
			Title   string `json:"title"`
			HTMLURL string `json:"html_url"`
			Head    struct {
				Ref string `json:"ref"`
			} `json:"head"`
			Base struct {
				Ref string `json:"ref"`
			} `json:"base"`
		}
		if err := json.Unmarshal(respBody, &prs); err != nil {
			return nil, fmt.Errorf("parse open PRs targeting %s: %w", base, err)
		}
		for _, pr := range prs {
			if pr.Base.Ref != base {
				continue
			}
			out = append(out, BasePR{Number: pr.Number, Title: pr.Title, URL: pr.HTMLURL, HeadRefName: pr.Head.Ref})
		}
		if len(prs) < limit {
			return out, nil
		}
	}
}

// jsonGet performs an authenticated GET against a Forgejo/Gitea REST endpoint,
// returning the raw body and status — unlike jsonPost, the caller may need to
// unmarshal into a slice (a list-pulls response) rather than a single object.
func jsonGet(endpoint, authEnv, token string) ([]byte, int, error) {
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "token "+token)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	// Scrubbed HERE, at the one place the body enters the package, rather than at
	// each Errorf that interpolates it. Every caller's `string(respBody)` is then
	// safe by construction — including the ones that are not about HTTP status at
	// all ("response missing iid"), which a per-error-site scrub would miss.
	respBody = []byte(redact.Scrub(string(respBody), authEnv, token))
	return respBody, resp.StatusCode, nil
}

package ship

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Rivil/dross/internal/configenum"
)

// --- GitLab via REST ---

// gitlabTarget resolves the GitLab API request target: the authenticated
// token and the project ref (numeric override or URL-encoded owner/repo) —
// the credential+addressing preamble every GitLab REST call needs.
func gitlabTarget(opts OpenOpts) (ref, token string, err error) {
	if opts.APIBase == "" {
		return "", "", errors.New("gitlab backend needs APIBase (set [remote].api_base)")
	}
	if opts.AuthEnv == "" {
		return "", "", errors.New("gitlab backend needs AuthEnv (set [remote].auth_env)")
	}
	token, err = resolveToken(opts.APIBase, opts.AuthEnv, opts.Hosts)
	if err != nil {
		return "", "", err
	}
	owner, repo, err := splitOwnerRepo(opts.URL)
	if err != nil {
		return "", "", err
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(opts.ProjectID))
	return gitlabProjectRef(owner, repo, pid), token, nil
}

func openGitLabPR(opts OpenOpts) (*OpenResult, error) {
	ref, token, err := gitlabTarget(opts)
	if err != nil {
		return nil, err
	}

	title := opts.Title
	if opts.Draft {
		// GitLab marks a Merge Request as a draft via a "Draft:" title prefix.
		title = "Draft: " + opts.Title
	}
	body := map[string]any{
		"source_branch": opts.HeadBranch,
		"target_branch": opts.BaseBranch,
		"title":         title,
		"description":   opts.Body,
	}
	endpoint := strings.TrimRight(opts.APIBase, "/") + fmt.Sprintf("/projects/%s/merge_requests", ref)
	respBody, status, err := gitlabReq("POST", endpoint, opts.AuthScheme, token, body)
	if err != nil {
		return nil, fmt.Errorf("create MR: %w", err)
	}
	if status >= 300 {
		return nil, fmt.Errorf("create MR: HTTP %d: %s", status, string(respBody))
	}
	var mr struct {
		IID    int    `json:"iid"`
		WebURL string `json:"web_url"`
	}
	_ = json.Unmarshal(respBody, &mr)
	if mr.IID == 0 {
		return nil, fmt.Errorf("gitlab response missing iid: %s", string(respBody))
	}
	result := &OpenResult{Number: mr.IID, URL: mr.WebURL}

	if len(opts.Reviewers) > 0 {
		ids, err := gitlabReviewerIDs(opts.APIBase, opts.AuthScheme, token, opts.Reviewers)
		if err != nil {
			// Non-fatal: the MR is open. Surface the reviewer trouble as a warning.
			return result, fmt.Errorf("MR opened (!%d) but reviewer lookup failed: %w", mr.IID, err)
		}
		if len(ids) > 0 {
			updEndpoint := strings.TrimRight(opts.APIBase, "/") +
				fmt.Sprintf("/projects/%s/merge_requests/%d", ref, mr.IID)
			rb, st, err := gitlabReq("PUT", updEndpoint, opts.AuthScheme, token, map[string]any{"reviewer_ids": ids})
			if err != nil || st >= 300 {
				return result, fmt.Errorf("MR opened (!%d) but reviewer assignment failed (HTTP %d): %v %s", mr.IID, st, err, string(rb))
			}
		}
	}
	return result, nil
}

// gitlabPRStatus asks GET /projects/{ref}/merge_requests/{iid} and reports
// merged == (state == "merged") plus the MR's current target_branch. GitLab's
// other closed states — "closed" (declined without landing) and "locked" —
// both report Merged false, never true, so a discarded MR never
// false-completes the phase whose work it carried.
func gitlabPRStatus(opts OpenOpts) (PRStatus, error) {
	if opts.PRNumber <= 0 {
		return PRStatus{}, errors.New("gitlab merged-status lookup needs a PR number")
	}
	ref, token, err := gitlabTarget(opts)
	if err != nil {
		return PRStatus{}, err
	}
	endpoint := strings.TrimRight(opts.APIBase, "/") + fmt.Sprintf("/projects/%s/merge_requests/%d", ref, opts.PRNumber)
	respBody, status, err := gitlabReq("GET", endpoint, opts.AuthScheme, token, nil)
	if err != nil {
		return PRStatus{}, fmt.Errorf("get MR !%d: %w", opts.PRNumber, err)
	}
	if status >= 300 {
		return PRStatus{}, fmt.Errorf("get MR !%d: HTTP %d: %s", opts.PRNumber, status, string(respBody))
	}
	var mr struct {
		State        string `json:"state"`
		TargetBranch string `json:"target_branch"`
	}
	if err := json.Unmarshal(respBody, &mr); err != nil {
		return PRStatus{}, fmt.Errorf("parse MR !%d: %w", opts.PRNumber, err)
	}
	return PRStatus{Merged: strings.EqualFold(mr.State, "merged"), BaseRef: mr.TargetBranch}, nil
}

// gitlabOpenMRsTargeting lists the open Merge Requests whose target_branch is
// base, paginating through every page — GitLab defaults to 20 results per
// page, so a first-page-only read would let `milestone prune` delete a branch
// with live dependents still sitting on page 2.
//
// A lookup failure is always an error, never an empty or partial slice: an
// empty slice reads as "no dependents" and authorizes an irreversible branch
// delete.
func gitlabOpenMRsTargeting(opts OpenOpts, base string) ([]BasePR, error) {
	ref, token, err := gitlabTarget(opts)
	if err != nil {
		return nil, err
	}

	const perPage = 100
	var out []BasePR
	for page := 1; ; page++ {
		endpoint := strings.TrimRight(opts.APIBase, "/") + fmt.Sprintf(
			"/projects/%s/merge_requests?state=opened&target_branch=%s&per_page=%d&page=%d",
			ref, url.QueryEscape(base), perPage, page)
		respBody, status, err := gitlabReq("GET", endpoint, opts.AuthScheme, token, nil)
		if err != nil {
			return nil, fmt.Errorf("list open MRs targeting %s: %w", base, err)
		}
		if status >= 300 {
			return nil, fmt.Errorf("list open MRs targeting %s: HTTP %d: %s", base, status, string(respBody))
		}
		var mrs []struct {
			IID          int    `json:"iid"`
			Title        string `json:"title"`
			WebURL       string `json:"web_url"`
			SourceBranch string `json:"source_branch"`
		}
		if err := json.Unmarshal(respBody, &mrs); err != nil {
			return nil, fmt.Errorf("parse open MRs targeting %s: %w", base, err)
		}
		for _, mr := range mrs {
			out = append(out, BasePR{Number: mr.IID, Title: mr.Title, URL: mr.WebURL, HeadRefName: mr.SourceBranch})
		}
		if len(mrs) < perPage {
			return out, nil
		}
	}
}

// gitlabProjectRef returns the GitLab project identifier for the API path.
// A positive numeric projectID wins (the config override); otherwise the
// URL-encoded "owner/repo" path (owner%2Frepo) is used.
func gitlabProjectRef(owner, repo string, projectID int) string {
	if projectID > 0 {
		return strconv.Itoa(projectID)
	}
	return url.PathEscape(owner + "/" + repo)
}

// gitlabAuthHeader sets the GitLab auth header on req per the scheme: "bearer"
// uses Authorization: Bearer; anything else (incl. "" and "private-token") uses
// the PRIVATE-TOKEN header. Exactly one scheme's header is set.
func gitlabAuthHeader(req *http.Request, scheme, token string) {
	if configenum.Normalize(scheme) == "bearer" {
		req.Header.Set("Authorization", "Bearer "+token)
		return
	}
	req.Header.Set("PRIVATE-TOKEN", token)
}

// gitlabReq performs a GitLab REST request with the scheme-appropriate auth
// header, returning the raw body and status. body is JSON-encoded when non-nil.
func gitlabReq(method, endpoint, scheme, token string, body any) ([]byte, int, error) {
	var buf io.Reader
	if body != nil {
		b := new(bytes.Buffer)
		if err := json.NewEncoder(b).Encode(body); err != nil {
			return nil, 0, err
		}
		buf = b
	}
	req, err := http.NewRequest(method, endpoint, buf)
	if err != nil {
		return nil, 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	gitlabAuthHeader(req, scheme, token)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return respBody, resp.StatusCode, nil
}

// gitlabReviewerIDs resolves usernames to numeric GitLab user ids via
// GET /users?username=. An unresolved username is skipped; a transport or
// HTTP error is returned so the caller can warn (reviewer failure is non-fatal).
func gitlabReviewerIDs(apiBase, scheme, token string, usernames []string) ([]int, error) {
	var ids []int
	for _, name := range usernames {
		endpoint := strings.TrimRight(apiBase, "/") + "/users?username=" + url.QueryEscape(name)
		respBody, status, err := gitlabReq("GET", endpoint, scheme, token, nil)
		if err != nil {
			return ids, err
		}
		if status >= 300 {
			return ids, fmt.Errorf("user lookup %q: HTTP %d", name, status)
		}
		var users []struct {
			ID int `json:"id"`
		}
		if err := json.Unmarshal(respBody, &users); err != nil {
			return ids, fmt.Errorf("user lookup %q: %w", name, err)
		}
		if len(users) > 0 && users[0].ID > 0 {
			ids = append(ids, users[0].ID)
		}
	}
	return ids, nil
}

// splitOwnerRepo parses a canonical https://host/owner/repo URL.
func splitOwnerRepo(repoURL string) (owner, repo string, err error) {
	u, perr := url.Parse(repoURL)
	if perr != nil || u.Host == "" {
		return "", "", fmt.Errorf("bad repo URL %q", repoURL)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("URL %q does not look like /owner/repo", repoURL)
	}
	owner = parts[0]
	repo = strings.TrimSuffix(parts[1], ".git")
	return owner, repo, nil
}

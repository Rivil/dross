package ship

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// --- PR comments, head and caller on Forgejo / Gitea ---
//
// Forgejo and Gitea expose no bot flag on a user, so every author reads
// AuthorUnknown — never a guess from a `-bot` login.

// forgejoCommentPage is the page size the comment reads ask for.
const forgejoCommentPage = 50

// forgejoUser is a Forgejo/Gitea account as its REST API reports it.
type forgejoUser struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
}

// forgejoComment is an issue comment, a review, or a review's inline comment;
// each fills the fields it has.
type forgejoComment struct {
	ID               int64        `json:"id"`
	User             *forgejoUser `json:"user"`
	Body             string       `json:"body"`
	HTMLURL          string       `json:"html_url"`
	State            string       `json:"state"`
	Path             string       `json:"path"`
	Position         int          `json:"position"`
	OriginalPosition int          `json:"original_position"`
}

// forgejoPull is the part of pulls/{index} the head binding reads.
type forgejoPull struct {
	State   string         `json:"state"`
	HTMLURL string         `json:"html_url"`
	Head    forgejoPullEnd `json:"head"`
	Base    forgejoPullEnd `json:"base"`
}

type forgejoPullEnd struct {
	Ref  string           `json:"ref"`
	Repo *forgejoRepoName `json:"repo"`
}

type forgejoRepoName struct {
	FullName string `json:"full_name"`
}

// forgejoGetJSON GETs endpoint through jsonGet and decodes it into a T. A
// non-2xx answer is an error carrying the status and the body jsonGet has
// already scrubbed of the token.
func forgejoGetJSON[T any](endpoint, authEnv, token string) (T, error) {
	var out T
	body, status, err := jsonGet(endpoint, authEnv, token)
	if err != nil {
		return out, err
	}
	if status >= 300 {
		return out, fmt.Errorf("HTTP %d: %s", status, string(body))
	}
	if json.Unmarshal(body, &out) != nil {
		return out, errors.New("the forge's answer is not the JSON it promises")
	}
	return out, nil
}

// forgejoListAll pages a comment list at page=&limit=. The read ends on the
// first page that adds no id it has not seen — never on a short page, because
// an instance whose [api] MAX_RESPONSE_ITEMS is under the limit asked for caps
// every page, and a short full page would end the read early. Gitea's
// issue-comment list takes no page parameters and answers everything every
// time; its second answer adds nothing, which ends the read rather than looping
// on it. A run of prThreadMaxPages pages that keep adding is refused as
// possibly truncated.
func forgejoListAll(endpoint, authEnv, token string) ([]forgejoComment, error) {
	seen := map[int64]bool{}
	var all []forgejoComment
	sep := "?"
	if strings.Contains(endpoint, "?") {
		sep = "&"
	}
	for page := 1; page <= prThreadMaxPages; page++ {
		batch, err := forgejoGetJSON[[]forgejoComment](fmt.Sprintf("%s%spage=%d&limit=%d", endpoint, sep, page, forgejoCommentPage), authEnv, token)
		if err != nil {
			return nil, err
		}
		added := 0
		for _, c := range batch {
			if c.ID <= 0 {
				return nil, errors.New("a record carries no id")
			}
			if !seen[c.ID] {
				seen[c.ID] = true
				all = append(all, c)
				added++
			}
		}
		if added == 0 {
			return all, nil
		}
	}
	return nil, fmt.Errorf("still full after %d pages of %d, so the list may be truncated", prThreadMaxPages, forgejoCommentPage)
}

func forgejoPRComments(opts OpenOpts, n int) ([]PRComment, error) {
	owner, repo, token, err := forgejoTarget(opts)
	if err != nil {
		return nil, err
	}
	base := strings.TrimRight(opts.APIBase, "/") + "/repos/" + owner + "/" + repo
	issue := base + "/issues/" + strconv.Itoa(n)
	pull := base + "/pulls/" + strconv.Itoa(n)

	conv, err := forgejoListAll(issue+"/comments", opts.AuthEnv, token)
	if err != nil {
		return nil, fmt.Errorf("read PR #%d conversation comments: %w", n, err)
	}
	out := make([]PRComment, 0, len(conv))
	for _, c := range conv {
		out = append(out, forgejoPRComment(CommentConversation, c))
	}
	reviews, err := forgejoListAll(pull+"/reviews", opts.AuthEnv, token)
	if err != nil {
		return nil, fmt.Errorf("read PR #%d reviews: %w", n, err)
	}
	for _, r := range reviews {
		if r.State == "PENDING" {
			continue // the reviewer's unsent draft
		}
		if strings.TrimSpace(r.Body) != "" {
			out = append(out, forgejoPRComment(CommentReview, r))
		}
		inline, err := forgejoGetJSON[[]forgejoComment](pull+"/reviews/"+strconv.FormatInt(r.ID, 10)+"/comments", opts.AuthEnv, token)
		if err != nil {
			return nil, fmt.Errorf("read PR #%d inline comments: %w", n, err)
		}
		for _, c := range inline {
			if c.ID <= 0 {
				return nil, fmt.Errorf("read PR #%d inline comments: a record carries no id", n)
			}
			out = append(out, forgejoPRComment(CommentInline, c))
		}
	}
	return out, nil
}

// forgejoPRComment maps one record. An inline comment's line is its new-side
// position, else the old-side one a comment on a removed line keeps.
func forgejoPRComment(kind CommentKind, c forgejoComment) PRComment {
	item := PRComment{
		ID:     strconv.FormatInt(c.ID, 10),
		Kind:   kind,
		Author: CommentAuthor{Bot: AuthorUnknown},
		URL:    c.HTMLURL,
		Body:   c.Body,
	}
	if c.User != nil {
		item.Author.Login = c.User.Login
		if c.User.ID > 0 {
			item.Author.ID = strconv.FormatInt(c.User.ID, 10)
		}
	}
	if kind == CommentInline {
		item.Path = c.Path
		item.Line = c.Position
		if item.Line <= 0 {
			item.Line = c.OriginalPosition
		}
	}
	return item
}

func forgejoPRHead(opts OpenOpts, n int) (PRHead, error) {
	owner, repo, token, err := forgejoTarget(opts)
	if err != nil {
		return PRHead{}, err
	}
	endpoint := strings.TrimRight(opts.APIBase, "/") + "/repos/" + owner + "/" + repo + "/pulls/" + strconv.Itoa(n)
	pull, err := forgejoGetJSON[forgejoPull](endpoint, opts.AuthEnv, token)
	if err != nil {
		return PRHead{}, fmt.Errorf("read PR #%d: %w", n, err)
	}
	if pull.Head.Ref == "" {
		return PRHead{}, fmt.Errorf("read PR #%d: the forge's answer names no head branch", n)
	}
	same := pull.Head.Repo != nil && pull.Base.Repo != nil && pull.Head.Repo.FullName != "" &&
		strings.EqualFold(pull.Head.Repo.FullName, pull.Base.Repo.FullName)
	return PRHead{Ref: pull.Head.Ref, CrossRepo: !same, Open: pull.State == "open", URL: pull.HTMLURL}, nil
}

func forgejoAuthenticatedUser(opts OpenOpts) (Account, error) {
	_, _, token, err := forgejoTarget(opts)
	if err != nil {
		return Account{}, err
	}
	u, err := forgejoGetJSON[forgejoUser](strings.TrimRight(opts.APIBase, "/")+"/user", opts.AuthEnv, token)
	if err != nil {
		return Account{}, fmt.Errorf("read the authenticated account: %w", err)
	}
	if u.Login == "" || u.ID <= 0 {
		return Account{}, errors.New("read the authenticated account: the forge's answer names no account")
	}
	return Account{ID: strconv.FormatInt(u.ID, 10), Login: u.Login}, nil
}

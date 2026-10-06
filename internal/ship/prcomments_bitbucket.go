package ship

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// --- PR comments, head and caller on Bitbucket Cloud ---
//
// Bitbucket has no review-summary kind: a comment anchored to a file is
// inline, every other is conversation. An account's identity is its uuid —
// display names are not unique, so nothing is matched by one.

// bbCommentPage is the pagelen every comment read asks for.
const bbCommentPage = 100

// bbAccount is a Bitbucket account. Type is "user" or "app_user".
type bbAccount struct {
	UUID        string `json:"uuid"`
	Nickname    string `json:"nickname"`
	DisplayName string `json:"display_name"`
	Type        string `json:"type"`
}

// bbComment is one PR comment.
type bbComment struct {
	ID      int64      `json:"id"`
	Deleted bool       `json:"deleted"`
	User    *bbAccount `json:"user"`
	Content struct {
		Raw string `json:"raw"`
	} `json:"content"`
	Inline *bbInline `json:"inline"`
	Links  bbLinks   `json:"links"`
}

type bbInline struct {
	Path string `json:"path"`
	To   *int   `json:"to"`
	From *int   `json:"from"`
}

type bbLinks struct {
	HTML struct {
		Href string `json:"href"`
	} `json:"html"`
}

// bbCommentList is one page of the comment list. Next is only read as "there
// is more": its URL is never requested, because bbRequest sends Basic auth to
// whatever URL it is handed.
type bbCommentList struct {
	Values []bbComment `json:"values"`
	Next   string      `json:"next"`
}

// bbPull is the part of a PR the head binding reads.
type bbPull struct {
	State  string  `json:"state"`
	Links  bbLinks `json:"links"`
	Source struct {
		Branch struct {
			Name string `json:"name"`
		} `json:"branch"`
		Repository *bbRepoName `json:"repository"`
	} `json:"source"`
	Destination struct {
		Repository *bbRepoName `json:"repository"`
	} `json:"destination"`
}

type bbRepoName struct {
	FullName string `json:"full_name"`
}

// bbGetJSON GETs endpoint through bbRequest and decodes it into a T. A non-2xx
// answer is an error carrying the status and the body bbRequest has already
// scrubbed of the token.
func bbGetJSON[T any](opts OpenOpts, user, token, endpoint string) (T, error) {
	var out T
	body, status, err := bbRequest("GET", endpoint, opts.AuthEnv, user, token, nil)
	if err != nil {
		return out, err
	}
	if status >= 300 {
		return out, fmt.Errorf("HTTP %d: %s", status, string(body))
	}
	if json.Unmarshal(body, &out) != nil {
		return out, errors.New("the answer from Bitbucket is not the JSON it promises")
	}
	return out, nil
}

// bbPullBase is the PR's endpoint under api_base.
func bbPullBase(opts OpenOpts, n int) (string, error) {
	workspace, slug, err := bbRepoRef(opts.URL)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(opts.APIBase, "/") + "/repositories/" + workspace + "/" + slug + "/pullrequests/" + strconv.Itoa(n), nil
}

func bitbucketPRComments(opts OpenOpts, n int) ([]PRComment, error) {
	user, token, err := bbCredentials(opts.APIBase, opts.AuthEnv, opts.AuthUser, opts.Hosts)
	if err != nil {
		return nil, err
	}
	pull, err := bbPullBase(opts, n)
	if err != nil {
		return nil, err
	}
	var out []PRComment
	for page := 1; ; page++ {
		if page > prThreadMaxPages {
			return nil, fmt.Errorf("read PR #%d comments: still more after %d pages of %d, so the list may be truncated", n, prThreadMaxPages, bbCommentPage)
		}
		list, err := bbGetJSON[bbCommentList](opts, user, token, fmt.Sprintf("%s/comments?pagelen=%d&page=%d", pull, bbCommentPage, page))
		if err != nil {
			return nil, fmt.Errorf("read PR #%d comments: %w", n, err)
		}
		for _, c := range list.Values {
			if c.ID <= 0 {
				return nil, fmt.Errorf("read PR #%d comments: a record carries no id", n)
			}
			if c.Deleted {
				continue
			}
			out = append(out, bbItem(c))
		}
		if list.Next == "" {
			return out, nil
		}
	}
}

// bbItem maps one comment. An inline comment's line is its new-side `to`, or
// `from` for a comment on a removed line.
func bbItem(c bbComment) PRComment {
	item := PRComment{
		ID:     strconv.FormatInt(c.ID, 10),
		Kind:   CommentConversation,
		Author: bbAuthor(c.User),
		URL:    c.Links.HTML.Href,
		Body:   c.Content.Raw,
	}
	if c.Inline != nil && c.Inline.Path != "" {
		item.Kind = CommentInline
		item.Path = c.Inline.Path
		switch {
		case c.Inline.To != nil:
			item.Line = *c.Inline.To
		case c.Inline.From != nil:
			item.Line = *c.Inline.From
		}
	}
	return item
}

// bbAuthor reads the bot flag from Bitbucket's account type.
func bbAuthor(a *bbAccount) CommentAuthor {
	if a == nil {
		return CommentAuthor{Bot: AuthorUnknown}
	}
	out := CommentAuthor{ID: a.UUID, Login: a.Nickname, Bot: AuthorUnknown}
	if out.Login == "" {
		out.Login = a.DisplayName
	}
	switch a.Type {
	case "app_user":
		out.Bot = AuthorBot
	case "user":
		out.Bot = AuthorHuman
	}
	return out
}

func bitbucketPRHead(opts OpenOpts, n int) (PRHead, error) {
	user, token, err := bbCredentials(opts.APIBase, opts.AuthEnv, opts.AuthUser, opts.Hosts)
	if err != nil {
		return PRHead{}, err
	}
	endpoint, err := bbPullBase(opts, n)
	if err != nil {
		return PRHead{}, err
	}
	pr, err := bbGetJSON[bbPull](opts, user, token, endpoint)
	if err != nil {
		return PRHead{}, fmt.Errorf("read PR #%d: %w", n, err)
	}
	if pr.Source.Branch.Name == "" {
		return PRHead{}, fmt.Errorf("read PR #%d: Bitbucket's answer names no source branch", n)
	}
	src, dst := pr.Source.Repository, pr.Destination.Repository
	same := src != nil && dst != nil && src.FullName != "" && strings.EqualFold(src.FullName, dst.FullName)
	return PRHead{Ref: pr.Source.Branch.Name, CrossRepo: !same, Open: pr.State == "OPEN", URL: pr.Links.HTML.Href}, nil
}

func bitbucketAuthenticatedUser(opts OpenOpts) (Account, error) {
	user, token, err := bbCredentials(opts.APIBase, opts.AuthEnv, opts.AuthUser, opts.Hosts)
	if err != nil {
		return Account{}, err
	}
	a, err := bbGetJSON[bbAccount](opts, user, token, strings.TrimRight(opts.APIBase, "/")+"/user")
	if err != nil {
		return Account{}, fmt.Errorf("read the authenticated account: %w", err)
	}
	if a.UUID == "" {
		return Account{}, errors.New("read the authenticated account: Bitbucket's answer names no account")
	}
	login := a.Nickname
	if login == "" {
		login = a.DisplayName
	}
	return Account{ID: a.UUID, Login: login}, nil
}

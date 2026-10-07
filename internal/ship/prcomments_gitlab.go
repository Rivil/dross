package ship

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// --- MR notes, head and caller on GitLab ---
//
// GitLab has no review-summary kind: a note on a diff line is inline, every
// other note is conversation. A note carries no web link of its own, so each
// one links to the MR's web_url with #note_<id>.

// gitlabNotePage is the per_page every note read asks for.
const gitlabNotePage = 100

// gitlabUser is a GitLab account; Bot is GitLab's own flag, read per author.
type gitlabUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Bot      *bool  `json:"bot"`
}

// gitlabNote is one MR note. A DiffNote carries its position.
type gitlabNote struct {
	ID       int64         `json:"id"`
	Body     string        `json:"body"`
	System   bool          `json:"system"`
	Type     string        `json:"type"`
	Author   *gitlabUser   `json:"author"`
	Position *gitlabNoteAt `json:"position"`
}

type gitlabNoteAt struct {
	NewPath string `json:"new_path"`
	NewLine *int   `json:"new_line"`
	OldPath string `json:"old_path"`
	OldLine *int   `json:"old_line"`
}

// gitlabMR is the part of an MR the head binding and the note links read.
type gitlabMR struct {
	State           string `json:"state"`
	WebURL          string `json:"web_url"`
	SourceBranch    string `json:"source_branch"`
	SourceProjectID int64  `json:"source_project_id"`
	TargetProjectID int64  `json:"target_project_id"`
}

// gitlabGetJSON GETs endpoint through gitlabReq and decodes it into a T. A
// non-2xx answer is an error carrying the status and the body gitlabReq has
// already scrubbed of the token.
func gitlabGetJSON[T any](opts OpenOpts, token, endpoint string) (T, error) {
	var out T
	body, status, err := gitlabReq("GET", endpoint, opts.AuthEnv, opts.AuthScheme, token, nil)
	if err != nil {
		return out, err
	}
	if status >= 300 {
		return out, fmt.Errorf("HTTP %d: %s", status, string(body))
	}
	if json.Unmarshal(body, &out) != nil {
		return out, errors.New("GitLab's answer is not the JSON it promises")
	}
	return out, nil
}

func gitlabMRAt(opts OpenOpts, ref, token string, n int) (gitlabMR, error) {
	endpoint := strings.TrimRight(opts.APIBase, "/") + "/projects/" + ref + "/merge_requests/" + strconv.Itoa(n)
	mr, err := gitlabGetJSON[gitlabMR](opts, token, endpoint)
	if err != nil {
		return gitlabMR{}, fmt.Errorf("read MR !%d: %w", n, err)
	}
	return mr, nil
}

func gitlabPRComments(opts OpenOpts, n int) ([]PRComment, error) {
	ref, token, err := gitlabTarget(opts)
	if err != nil {
		return nil, err
	}
	mr, err := gitlabMRAt(opts, ref, token, n)
	if err != nil {
		return nil, err
	}
	if mr.WebURL == "" {
		return nil, fmt.Errorf("read MR !%d: GitLab's answer has no web_url to link each note to", n)
	}
	notes, err := gitlabNotes(opts, ref, token, n)
	if err != nil {
		return nil, fmt.Errorf("read MR !%d notes: %w", n, err)
	}
	bots := map[int64]AuthorClass{}
	var out []PRComment
	for _, note := range notes {
		if note.System {
			continue // "added 1 commit" and the like: GitLab's own activity log
		}
		c := PRComment{
			ID:     strconv.FormatInt(note.ID, 10),
			Kind:   CommentConversation,
			Author: CommentAuthor{Bot: AuthorUnknown},
			URL:    mr.WebURL + "#note_" + strconv.FormatInt(note.ID, 10),
			Body:   note.Body,
		}
		if note.Author != nil {
			c.Author.Login = note.Author.Username
			if note.Author.ID > 0 {
				c.Author.ID = strconv.FormatInt(note.Author.ID, 10)
				if _, ok := bots[note.Author.ID]; !ok {
					bots[note.Author.ID] = gitlabBotFlag(opts, token, note.Author.ID)
				}
				c.Author.Bot = bots[note.Author.ID]
			}
		}
		if note.Type == "DiffNote" && note.Position != nil {
			c.Kind = CommentInline
			c.Path, c.Line = gitlabNoteSide(note.Position)
		}
		out = append(out, c)
	}
	return out, nil
}

// gitlabNoteSide is where a diff note sits: the new side when it has a new
// line; the old side only when it has an old line — a note on a removed line;
// otherwise, for a file-level or image note with neither, the file by its
// current name (the old one only when the new side names none) and line 0.
func gitlabNoteSide(at *gitlabNoteAt) (string, int) {
	switch {
	case at.NewLine != nil && *at.NewLine > 0:
		return at.NewPath, *at.NewLine
	case at.OldLine != nil && *at.OldLine > 0:
		return at.OldPath, *at.OldLine
	case at.NewPath != "":
		return at.NewPath, 0
	}
	return at.OldPath, 0
}

// gitlabNotes reads every note at per_page=100, oldest first, by page=.
func gitlabNotes(opts OpenOpts, ref, token string, n int) ([]gitlabNote, error) {
	base := strings.TrimRight(opts.APIBase, "/") + "/projects/" + ref + "/merge_requests/" + strconv.Itoa(n) + "/notes"
	var all []gitlabNote
	for page := 1; page <= prThreadMaxPages; page++ {
		batch, err := gitlabGetJSON[[]gitlabNote](opts, token, fmt.Sprintf("%s?per_page=%d&sort=asc&page=%d", base, gitlabNotePage, page))
		if err != nil {
			return nil, err
		}
		for _, note := range batch {
			if note.ID <= 0 {
				return nil, errors.New("a note carries no id")
			}
		}
		all = append(all, batch...)
		if len(batch) < gitlabNotePage {
			return all, nil
		}
	}
	return nil, fmt.Errorf("still full after %d pages of %d, so the list may be truncated", prThreadMaxPages, gitlabNotePage)
}

// gitlabBotFlag asks GitLab whether user id is a bot. A failed lookup is
// unknown, never a guess, and never fails the read it is part of.
func gitlabBotFlag(opts OpenOpts, token string, id int64) AuthorClass {
	u, err := gitlabGetJSON[gitlabUser](opts, token, strings.TrimRight(opts.APIBase, "/")+"/users/"+strconv.FormatInt(id, 10))
	switch {
	case err != nil || u.Bot == nil:
		return AuthorUnknown
	case *u.Bot:
		return AuthorBot
	}
	return AuthorHuman
}

func gitlabPRHead(opts OpenOpts, n int) (PRHead, error) {
	ref, token, err := gitlabTarget(opts)
	if err != nil {
		return PRHead{}, err
	}
	mr, err := gitlabMRAt(opts, ref, token, n)
	if err != nil {
		return PRHead{}, err
	}
	if mr.SourceBranch == "" {
		return PRHead{}, fmt.Errorf("read MR !%d: GitLab's answer names no source branch", n)
	}
	same := mr.SourceProjectID > 0 && mr.SourceProjectID == mr.TargetProjectID
	return PRHead{Ref: mr.SourceBranch, CrossRepo: !same, Open: mr.State == "opened", URL: mr.WebURL}, nil
}

func gitlabAuthenticatedUser(opts OpenOpts) (Account, error) {
	_, token, err := gitlabTarget(opts)
	if err != nil {
		return Account{}, err
	}
	u, err := gitlabGetJSON[gitlabUser](opts, token, strings.TrimRight(opts.APIBase, "/")+"/user")
	if err != nil {
		return Account{}, fmt.Errorf("read the authenticated account: %w", err)
	}
	if u.Username == "" || u.ID <= 0 {
		return Account{}, errors.New("read the authenticated account: GitLab's answer names no account")
	}
	return Account{ID: strconv.FormatInt(u.ID, 10), Login: u.Username}, nil
}

package ship

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Rivil/dross/internal/configenum"
	"github.com/Rivil/dross/internal/protect"
	"github.com/Rivil/dross/internal/secretscan"
)

// BranchRulesResult is a branch's live ruleset rules, or why they couldn't be
// read. The zero value is unknown, never "no rules": a caller that forgets to
// fill it in reads as unknown, which is the false-green this phase closes.
type BranchRulesResult struct {
	Known    bool
	Reason   string // why the rules couldn't be read, when !Known
	Rules    []protect.LiveRule
	Rulesets []protect.LiveRuleset // details of the rulesets Rules came from, where readable
}

// MergeSettings is the part of a repo's settings `dross protect` reads: whether
// auto-merge and merge commits are allowed. Known is false when either can't be
// read; an unread setting is never reported as on.
type MergeSettings struct {
	Known            bool
	Reason           string
	AllowAutoMerge   bool
	AllowMergeCommit bool
}

// RulesetSummary is one entry of a repo's ruleset list.
type RulesetSummary struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// ghAPITimeout bounds every `gh api` spawn here: generous for one REST call,
// still bounding a stalled network. Tests shorten it.
var ghAPITimeout = 20 * time.Second

// ghAPIWaitDelay bounds how long Wait waits for gh's pipes to close once gh
// has exited or been killed.
const ghAPIWaitDelay = time.Second

// The overridable seams cmd-package callers use, and cmd-package tests stub,
// to read and write branch protection without gh or a network — the
// unexported ghCommand seam is unreachable from package cmd.
var (
	BranchRulesFunc       = BranchRules
	RepoMergeSettingsFunc = RepoMergeSettings
	ListRulesetsFunc      = ListRulesets
	CreateRulesetFunc     = CreateRuleset
	UpdateRulesetFunc     = UpdateRuleset
	SetAllowAutoMergeFunc = SetAllowAutoMerge
)

// BranchRules reads the rules in force on branch (GET rules/branches/{branch})
// and the details of each ruleset they come from (GET rulesets/{id}). Any
// failure to read the rules — gh missing, not logged in, HTTP 401/403/404, no
// answer in time, output that isn't the promised JSON — is unknown with the
// reason, never a known empty list. A ruleset whose details can't be read is
// left out of Rulesets, which protect.Assess reports as an unknown bypass list.
func BranchRules(opts OpenOpts, branch string) BranchRulesResult {
	repo, err := githubRepo(opts)
	if err != nil {
		return BranchRulesResult{Reason: err.Error()}
	}
	if branch == "" {
		return BranchRulesResult{Reason: "no branch name"}
	}
	out, err := ghAPI(repo, "GET", repo.path+"/rules/branches/"+url.PathEscape(branch)+"?per_page=100", nil)
	if err != nil {
		return BranchRulesResult{Reason: err.Error()}
	}
	var raw []protect.LiveRule
	if json.Unmarshal(out, &raw) != nil || raw == nil {
		return BranchRulesResult{Reason: "GitHub's rules answer is not the JSON list it promises"}
	}
	//dross:taint-cleared LiveRule is gh's decoded --json rule record: a rule type, a ruleset id and typed parameters (check contexts, counts, flags) — forge metadata, not gh's prose
	rules := raw
	res := BranchRulesResult{Known: true, Rules: rules}
	seen := map[int64]bool{}
	for _, r := range rules {
		if r.RulesetID <= 0 || seen[r.RulesetID] {
			continue
		}
		seen[r.RulesetID] = true
		body, err := ghAPI(repo, "GET", repo.path+"/rulesets/"+strconv.FormatInt(r.RulesetID, 10), nil)
		if err != nil {
			continue
		}
		var raw protect.LiveRuleset
		if json.Unmarshal(body, &raw) != nil || raw.ID != r.RulesetID {
			continue
		}
		//dross:taint-cleared LiveRuleset is gh's decoded --json ruleset record: an id, a name, an enforcement state and bypass entries — forge metadata, not gh's prose
		rs := raw
		res.Rulesets = append(res.Rulesets, rs)
	}
	return res
}

// RepoMergeSettings reads allow_auto_merge and allow_merge_commit (GET
// repos/{owner}/{repo}). GitHub returns them only to a caller who may change
// them; a missing field, like a failed read, is unknown.
func RepoMergeSettings(opts OpenOpts) MergeSettings {
	repo, err := githubRepo(opts)
	if err != nil {
		return MergeSettings{Reason: err.Error()}
	}
	out, err := ghAPI(repo, "GET", repo.path, nil)
	if err != nil {
		return MergeSettings{Reason: err.Error()}
	}
	var raw struct {
		AllowAutoMerge   *bool `json:"allow_auto_merge"`
		AllowMergeCommit *bool `json:"allow_merge_commit"`
	}
	if json.Unmarshal(out, &raw) != nil {
		return MergeSettings{Reason: "GitHub's repo answer is not the JSON it promises"}
	}
	if raw.AllowAutoMerge == nil || raw.AllowMergeCommit == nil {
		return MergeSettings{Reason: "GitHub returned no merge settings — they are shown only to a caller with admin permission on the repo"}
	}
	return MergeSettings{Known: true, AllowAutoMerge: *raw.AllowAutoMerge, AllowMergeCommit: *raw.AllowMergeCommit}
}

// ListRulesets lists the repo's own rulesets (not ones inherited from an
// organisation).
func ListRulesets(opts OpenOpts) ([]RulesetSummary, error) {
	repo, err := githubRepo(opts)
	if err != nil {
		return nil, err
	}
	out, err := ghAPI(repo, "GET", repo.path+"/rulesets?includes_parents=false&per_page=100", nil)
	if err != nil {
		var api *ghAPIError
		if errors.As(err, &api) && api.status == 403 {
			return nil, errors.New("listing the repo's rulesets was refused (HTTP 403): writing branch protection needs admin permission on the repo")
		}
		return nil, fmt.Errorf("list rulesets: %w", err)
	}
	var list []RulesetSummary
	if json.Unmarshal(out, &list) != nil {
		return nil, errors.New("list rulesets: GitHub's answer is not the JSON list it promises")
	}
	return list, nil
}

// CreateRuleset POSTs rs as a new repo ruleset and returns its id.
func CreateRuleset(opts OpenOpts, rs protect.Ruleset) (int64, error) {
	repo, err := githubRepo(opts)
	if err != nil {
		return 0, err
	}
	out, err := ghAPI(repo, "POST", repo.path+"/rulesets", rs)
	if err != nil {
		return 0, fmt.Errorf("create ruleset %q: %w", rs.Name, err)
	}
	var created RulesetSummary
	if json.Unmarshal(out, &created) != nil || created.ID <= 0 {
		return 0, fmt.Errorf("create ruleset %q: GitHub's answer carries no ruleset id", rs.Name)
	}
	return created.ID, nil
}

// UpdateRuleset PUTs rs over ruleset id. The body carries every field, the
// empty bypass list included, so nothing of the old ruleset survives.
func UpdateRuleset(opts OpenOpts, id int64, rs protect.Ruleset) error {
	repo, err := githubRepo(opts)
	if err != nil {
		return err
	}
	if id <= 0 {
		return errors.New("update ruleset: no ruleset id")
	}
	if _, err := ghAPI(repo, "PUT", repo.path+"/rulesets/"+strconv.FormatInt(id, 10), rs); err != nil {
		return fmt.Errorf("update ruleset %q: %w", rs.Name, err)
	}
	return nil
}

// UpsertRuleset writes rs over the repo ruleset of the same name, or creates
// it when there is none. It goes through the seams, so a cmd-package test that
// stubs List/Create/Update sees exactly which write happened. created reports
// which one it was.
func UpsertRuleset(opts OpenOpts, rs protect.Ruleset) (id int64, created bool, err error) {
	list, err := ListRulesetsFunc(opts)
	if err != nil {
		return 0, false, err
	}
	var match []int64
	for _, s := range list {
		if s.Name == rs.Name {
			match = append(match, s.ID)
		}
	}
	switch len(match) {
	case 0:
		id, err := CreateRulesetFunc(opts, rs)
		return id, true, err
	case 1:
		return match[0], false, UpdateRulesetFunc(opts, match[0], rs)
	}
	return 0, false, fmt.Errorf("the repo has %d rulesets named %q; delete all but one, then re-run", len(match), rs.Name)
}

// SetAllowAutoMerge PATCHes the repo's allow_auto_merge setting and nothing
// else: a setting the user chose, like allow_merge_commit, is never touched.
func SetAllowAutoMerge(opts OpenOpts, enabled bool) error {
	repo, err := githubRepo(opts)
	if err != nil {
		return err
	}
	if _, err := ghAPI(repo, "PATCH", repo.path, map[string]bool{"allow_auto_merge": enabled}); err != nil {
		return fmt.Errorf("set allow_auto_merge: %w", err)
	}
	return nil
}

// ghRepo is a validated GitHub repo: the host gh is pointed at and the
// `repos/{owner}/{repo}` API path.
type ghRepo struct {
	host, path string
}

// repoSegment is what an owner or repo name may be. A leading `-` or a `..`
// never reaches gh, whatever the URL says.
var repoSegment = regexp.MustCompile(`^[A-Za-z0-9_.][A-Za-z0-9_.-]*$`)

func githubRepo(opts OpenOpts) (ghRepo, error) {
	if p := configenum.Normalize(opts.Provider); p != "github" {
		return ghRepo{}, fmt.Errorf("branch protection is read from GitHub only; provider is %q", opts.Provider)
	}
	u, err := url.Parse(opts.URL)
	if err != nil || u.Host == "" || strings.HasPrefix(u.Host, "-") {
		return ghRepo{}, errors.New("[remote].url is not an https://host/owner/repo URL")
	}
	owner, repo, err := splitOwnerRepo(opts.URL)
	if err != nil {
		return ghRepo{}, errors.New("[remote].url is not an https://host/owner/repo URL")
	}
	for _, seg := range []string{owner, repo} {
		if !repoSegment.MatchString(seg) || strings.Contains(seg, "..") {
			return ghRepo{}, errors.New("[remote].url names an owner or repo GitHub would not accept")
		}
	}
	return ghRepo{host: u.Host, path: "repos/" + owner + "/" + repo}, nil
}

// ghAPIError is a `gh api` call that didn't answer. Its text is fixed prose
// chosen from what gh reported — never gh's output itself, which can carry an
// API response body.
type ghAPIError struct {
	status int // HTTP status gh reported; 0 when there was none
	reason string
}

func (e *ghAPIError) Error() string { return e.reason }

// httpStatus finds gh's "(HTTP 404)"-style status in its output.
var httpStatus = regexp.MustCompile(`\(HTTP (\d{3})\)`)

// ghAPI runs `gh api --method <method> [--input -] -- <endpoint>` against
// repo's host. A body is secret-screened, JSON-encoded and sent on stdin, so
// it never appears in argv. stdout is returned; stderr is captured to classify
// a failure and is never printed — a caller like doctor shows the reason, not
// gh's prose.
func ghAPI(repo ghRepo, method, endpoint string, body any) ([]byte, error) {
	var stdin []byte
	args := []string{"api", "--hostname", repo.host, "--method", method,
		"-H", "Accept: application/vnd.github+json", "-H", "X-GitHub-Api-Version: 2022-11-28"}
	if body != nil {
		if err := secretscan.ScanPayload(method+" "+endpoint, body); err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		stdin = encoded
		args = append(args, "--input", "-")
	}
	args = append(args, "--", endpoint)

	cmd, err := screenedGH(args...)
	if err != nil {
		return nil, err
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.WaitDelay = ghAPIWaitDelay
	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, &ghAPIError{reason: "gh is not installed or not on PATH (https://cli.github.com)"}
		}
		return nil, &ghAPIError{reason: "gh could not be started"}
	}
	var timedOut atomic.Bool
	timer := time.AfterFunc(ghAPITimeout, func() {
		timedOut.Store(true)
		_ = cmd.Process.Kill()
	})
	err = cmd.Wait()
	timer.Stop()
	if timedOut.Load() {
		return nil, &ghAPIError{reason: fmt.Sprintf("no answer from GitHub within %s", ghAPITimeout)}
	}
	if err != nil {
		return nil, classifyGHAPIFailure(err, stdout.Bytes(), stderr.Bytes())
	}
	return stdout.Bytes(), nil
}

// classifyGHAPIFailure turns a failed `gh api` into fixed prose.
func classifyGHAPIFailure(err error, stdout, stderr []byte) error {
	status := 0
	if m := httpStatus.FindSubmatch(append(append([]byte{}, stderr...), stdout...)); m != nil {
		status, _ = strconv.Atoi(string(m[1]))
	}
	switch status {
	case 401:
		return &ghAPIError{status: 401, reason: "GitHub refused gh's credentials (HTTP 401) — run `gh auth login`"}
	case 403:
		return &ghAPIError{status: 403, reason: "GitHub refused access (HTTP 403) — the token lacks permission on the repo"}
	case 404:
		return &ghAPIError{status: 404, reason: "GitHub answered not found (HTTP 404) — check [remote].url, and that the token can see the repo"}
	case 0:
	default:
		return &ghAPIError{status: status, reason: fmt.Sprintf("GitHub answered HTTP %d", status)}
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 4 {
		return &ghAPIError{reason: "gh is not logged in — run `gh auth login`"}
	}
	return &ghAPIError{reason: "gh api failed without an HTTP status"}
}

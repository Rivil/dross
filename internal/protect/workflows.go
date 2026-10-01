// Package protect builds and checks the branch protection dross applies to a
// repo's main and milestone branches: which checks a PR must pass, the
// rulesets that require them, and the gaps between those and a branch's live
// rules.
//
// Workflow reading is line-based on purpose, like internal/pincheck and the
// workflow sweeps in internal/cmd: the stack decision is a single static binary
// with no YAML dependency. The scanner reads only the slice of YAML a GitHub
// workflow uses for its triggers and job keys, and it refuses — never guesses —
// a job whose check name can't be known from the file alone. A required check
// that never reports under the name the ruleset expects blocks every PR.
package protect

import (
	"sort"
	"strconv"
	"strings"
)

// Job is one check a pull_request workflow reports on a PR's head commit.
type Job struct {
	Workflow string // the path the workflow was read from
	ID       string // the job's key under jobs:
	Context  string // the status-check context it reports: name: when set, else ID
}

// Refusal is a pull_request job whose check context can't be known from the
// workflow file alone. Job names every affected job, comma-separated, when the
// cause is the workflow's trigger rather than one job.
type Refusal struct {
	Workflow string
	Job      string
	Reason   string
}

func (r *Refusal) Error() string {
	return r.Workflow + "/" + r.Job + ": " + r.Reason
}

// filterKeys are the pull_request trigger keys that make a workflow skip some
// PRs. A required check from a skipped workflow never reports, so those PRs
// can't merge.
var filterKeys = map[string]bool{
	"branches": true, "branches-ignore": true,
	"paths": true, "paths-ignore": true,
}

// PullRequestJobs returns the check of every job in every workflow triggered
// on pull_request, ordered by workflow path and then by position in the file.
// workflows maps a path to the file's content, so a caller can feed it any
// tree — origin/<main>'s, not just the working tree.
//
// pull_request_target never counts: it runs the base branch's copy of the
// workflow, so its jobs aren't a gate on the PR's own commit. The first
// refusal, in that same order, comes back as a *Refusal with no jobs.
func PullRequestJobs(workflows map[string]string) ([]Job, error) {
	paths := make([]string, 0, len(workflows))
	for p := range workflows {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var jobs []Job
	for _, p := range paths {
		found, err := scanWorkflow(p, workflows[p])
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, found...)
	}
	return jobs, nil
}

// Contexts returns the distinct check contexts of jobs, sorted.
func Contexts(jobs []Job) []string {
	seen := map[string]bool{}
	var out []string
	for _, j := range jobs {
		if !seen[j.Context] {
			seen[j.Context] = true
			out = append(out, j.Context)
		}
	}
	sort.Strings(out)
	return out
}

// jobEntry is one job under jobs: before its workflow's trigger is known to
// include pull_request. reason is set when its check name can't be read.
type jobEntry struct {
	id, name, reason string
}

func scanWorkflow(path, src string) ([]Job, error) {
	lines := yamlLines(src)
	on, jobsAt := -1, -1
	for i, l := range lines {
		if l.indent != 0 {
			continue
		}
		switch k, _, _ := keyValue(l.text); k {
		case "on":
			on = i
		case "jobs":
			jobsAt = i
		}
	}
	if on < 0 {
		return nil, nil
	}
	pr, triggerReason := readTrigger(lines, on)
	if !pr && triggerReason == "" {
		return nil, nil
	}
	entries, jobsReason := readJobs(lines, jobsAt)
	if jobsReason != "" {
		return nil, &Refusal{Workflow: path, Job: "jobs", Reason: jobsReason}
	}
	if len(entries) == 0 {
		return nil, nil
	}
	if triggerReason != "" {
		ids := make([]string, len(entries))
		for i, e := range entries {
			ids[i] = e.id
		}
		return nil, &Refusal{Workflow: path, Job: strings.Join(ids, ","), Reason: triggerReason}
	}

	jobs := make([]Job, 0, len(entries))
	for _, e := range entries {
		if e.reason != "" {
			return nil, &Refusal{Workflow: path, Job: e.id, Reason: e.reason}
		}
		ctx := e.name
		if ctx == "" {
			ctx = e.id
		}
		jobs = append(jobs, Job{Workflow: path, ID: e.id, Context: ctx})
	}
	return jobs, nil
}

// readTrigger reports whether the `on:` at lines[at] includes pull_request.
// reason is set when it does but filters on branches or paths, or when the
// value can't be read at all (then pr is false and the caller refuses anyway:
// a trigger it can't read might be pull_request).
func readTrigger(lines []yline, at int) (pr bool, reason string) {
	_, v, _ := keyValue(lines[at].text)
	switch {
	case v == "":
		body := valueBlock(lines, at)
		if len(body) == 0 {
			return false, ""
		}
		ci := body[0].indent
		for j, l := range body {
			if l.indent != ci {
				continue
			}
			if item, ok := strings.CutPrefix(l.text, "- "); ok {
				if unquote(strings.TrimSpace(item)) == "pull_request" {
					pr = true
				}
				continue
			}
			k, kv, ok := keyValue(l.text)
			if !ok || k != "pull_request" {
				continue
			}
			pr = true
			if f := blockFilter(kv, body[j+1:], ci); f != "" {
				return true, filterReason(f)
			}
		}
		return pr, ""
	case !closed(v):
		return false, "its `on:` flow value spans lines; write it on one line or as a block"
	case strings.HasPrefix(v, "["):
		for _, item := range flowItems(v) {
			if unquote(item) == "pull_request" {
				pr = true
			}
		}
		return pr, ""
	case strings.HasPrefix(v, "{"):
		for _, item := range flowItems(v) {
			k, kv, _ := strings.Cut(item, ":")
			if unquote(strings.TrimSpace(k)) != "pull_request" {
				continue
			}
			if f := flowFilter(strings.TrimSpace(kv)); f != "" {
				return true, filterReason(f)
			}
			pr = true
		}
		return pr, ""
	default:
		return unquote(v) == "pull_request", ""
	}
}

// blockFilter returns the first filter key on a block-map pull_request
// trigger whose inline value is kv and whose children follow in rest.
func blockFilter(kv string, rest []yline, keyIndent int) string {
	if kv != "" {
		return flowFilter(kv)
	}
	end := 0
	for end < len(rest) && rest[end].indent > keyIndent {
		end++
	}
	children := rest[:end]
	if len(children) == 0 {
		return ""
	}
	for _, c := range children {
		if c.indent != children[0].indent {
			continue
		}
		if k, _, ok := keyValue(c.text); ok && filterKeys[k] {
			return k
		}
	}
	return ""
}

// flowFilter returns the first filter key in a flow-map trigger value such as
// `{branches: [main]}`.
func flowFilter(v string) string {
	if !strings.HasPrefix(v, "{") {
		return ""
	}
	for _, item := range flowItems(v) {
		k, _, _ := strings.Cut(item, ":")
		if k = unquote(strings.TrimSpace(k)); filterKeys[k] {
			return k
		}
	}
	return ""
}

func filterReason(key string) string {
	return "its pull_request trigger filters on `" + key + "`, so its checks never report on the PRs the filter skips and those PRs can't merge"
}

// readJobs returns the jobs under the top-level `jobs:` at lines[at], in file
// order. reason is set when the jobs map itself can't be read.
func readJobs(lines []yline, at int) (entries []jobEntry, reason string) {
	if at < 0 {
		return nil, ""
	}
	if _, v, _ := keyValue(lines[at].text); v != "" {
		return nil, "its `jobs:` isn't a block mapping, so its job names can't be read"
	}
	body := valueBlock(lines, at)
	if len(body) == 0 {
		return nil, ""
	}
	ji := body[0].indent
	for i, l := range body {
		if l.indent != ji {
			continue
		}
		k, kv, ok := keyValue(l.text)
		if !ok {
			continue
		}
		e := jobEntry{id: k}
		if kv != "" {
			e.reason = "its body isn't a block mapping (`" + kv + "`), so its check name can't be read"
		} else {
			end := i + 1
			for end < len(body) && body[end].indent > ji {
				end++
			}
			readJobProps(body[i+1:end], &e)
		}
		entries = append(entries, e)
	}
	return entries, ""
}

// readJobProps reads the job-level keys that decide a job's check context.
// Only lines at the job's own property indent count, so a step's `name:` or a
// run: body never does.
func readJobProps(body []yline, e *jobEntry) {
	if len(body) == 0 {
		return
	}
	pi := body[0].indent
	for i, l := range body {
		if l.indent != pi || e.reason != "" {
			continue
		}
		k, v, ok := keyValue(l.text)
		if !ok {
			continue
		}
		switch k {
		case "name":
			switch {
			case isBlockScalar(v):
				e.reason = "its name: is a block scalar, so the check it reports can't be read"
			case strings.Contains(v, "${{"):
				e.reason = "its name: is an expression (`" + v + "`), so the check it reports is only known at run time"
			default:
				e.name = unquote(v)
			}
		case "uses":
			e.reason = "it calls a reusable workflow (`uses: " + v + "`), which reports its checks as \"<job> / <called job>\""
		case "strategy":
			if hasMatrix(v, body[i+1:], pi) {
				e.reason = "it runs a matrix, which reports one check per combination as \"<job> (<values>)\""
			}
		case "<<":
			e.reason = "it merges a YAML anchor (`<<:`), which can carry name:, uses: or strategy: from elsewhere in the file"
		}
	}
}

// hasMatrix reports whether a job-level `strategy:` (inline value v, children
// in rest) declares a matrix.
func hasMatrix(v string, rest []yline, keyIndent int) bool {
	if v != "" {
		return strings.Contains(v, "matrix")
	}
	end := 0
	for end < len(rest) && rest[end].indent > keyIndent {
		end++
	}
	children := rest[:end]
	for _, c := range children {
		if c.indent != children[0].indent {
			continue
		}
		if k, _, ok := keyValue(c.text); ok && k == "matrix" {
			return true
		}
	}
	return false
}

// yline is one meaningful workflow line: its indent and its text with any
// comment stripped. Blank and comment-only lines never become ylines.
//
// Block-scalar bodies (a run: script) do, but nothing reads them: every key
// the scanner wants is matched at one exact indent — `on:` and `jobs:` at 0, a
// trigger or job at its map's first indent, a job property at the job's — and
// YAML puts a block scalar's content deeper than the key that opens it. So a
// script that writes out a whole workflow can't contribute a key.
type yline struct {
	indent int
	text   string
}

func yamlLines(src string) []yline {
	var out []yline
	for _, raw := range strings.Split(src, "\n") {
		raw = strings.TrimRight(raw, "\r")
		trimmed := strings.TrimLeft(raw, " ")
		if text := stripComment(trimmed); text != "" {
			out = append(out, yline{indent: len(raw) - len(trimmed), text: text})
		}
	}
	return out
}

// isBlockScalar reports whether a value is a block scalar indicator: `|` or
// `>` with optional chomping and indentation indicators.
func isBlockScalar(v string) bool {
	if v == "" || (v[0] != '|' && v[0] != '>') {
		return false
	}
	for _, c := range v[1:] {
		if c != '+' && c != '-' && (c < '1' || c > '9') {
			return false
		}
	}
	return true
}

// valueBlock returns the lines that make up the block value of the key at
// lines[i]: every following line indented deeper, or — YAML's compact form
// for a sequence under a mapping key — the `- ` items at the key's own indent.
func valueBlock(lines []yline, i int) []yline {
	end := i + 1
	if end < len(lines) && lines[end].indent == lines[i].indent && strings.HasPrefix(lines[end].text, "- ") {
		for end < len(lines) && lines[end].indent == lines[i].indent && strings.HasPrefix(lines[end].text, "- ") {
			end++
		}
		return lines[i+1 : end]
	}
	for end < len(lines) && lines[end].indent > lines[i].indent {
		end++
	}
	return lines[i+1 : end]
}

// keyValue splits a `key: value` line. The key may be quoted; a line that
// starts a flow collection or holds no `: ` separator is not a key.
func keyValue(text string) (key, value string, ok bool) {
	if text == "" || text[0] == '[' || text[0] == '{' {
		return "", "", false
	}
	rest := text
	if q := text[0]; q == '"' || q == '\'' {
		end := closingQuote(text)
		if end < 0 {
			return "", "", false
		}
		key, rest = unquote(text[:end+1]), text[end+1:]
		if !strings.HasPrefix(rest, ":") {
			return "", "", false
		}
		return key, strings.TrimSpace(rest[1:]), true
	}
	for i := 0; i < len(rest); i++ {
		if rest[i] == ':' && (i == len(rest)-1 || rest[i+1] == ' ' || rest[i+1] == '\t') {
			return strings.TrimSpace(rest[:i]), strings.TrimSpace(rest[i+1:]), true
		}
	}
	return "", "", false
}

// closingQuote returns the index of the quote that closes the quoted scalar
// opening text, or -1.
func closingQuote(text string) int {
	q := text[0]
	for i := 1; i < len(text); i++ {
		switch {
		case q == '"' && text[i] == '\\':
			i++
		case text[i] == q && q == '\'' && i+1 < len(text) && text[i+1] == '\'':
			i++
		case text[i] == q:
			return i
		}
	}
	return -1
}

// unquote returns a scalar's value: the inside of a single- or double-quoted
// scalar, a plain one unchanged.
func unquote(s string) string {
	if len(s) < 2 {
		return s
	}
	switch {
	case s[0] == '"' && s[len(s)-1] == '"':
		if u, err := strconv.Unquote(s); err == nil {
			return u
		}
		return s[1 : len(s)-1]
	case s[0] == '\'' && s[len(s)-1] == '\'':
		return strings.ReplaceAll(s[1:len(s)-1], "''", "'")
	}
	return s
}

// stripComment drops a trailing `# comment`. A `#` is a comment only at the
// start or after whitespace, and never inside a quoted scalar; a quote opens a
// scalar only where a value can start, so the apostrophe in `Bob's tests`
// doesn't hide the comment after it.
func stripComment(s string) string {
	var quote byte
	prev := byte(' ') // last non-space byte; ' ' at the start of the line
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			switch {
			case quote == '"' && c == '\\':
				i++
			case c == quote:
				quote = 0
			}
			continue
		}
		switch {
		case c == '#' && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t'):
			return strings.TrimRight(s[:i], " \t")
		case (c == '"' || c == '\'') && strings.IndexByte(" :-[{,", prev) >= 0:
			quote = c
		}
		if c != ' ' && c != '\t' {
			prev = c
		}
	}
	return strings.TrimRight(s, " \t")
}

// closed reports whether a flow value's brackets and braces all close on this
// line.
func closed(v string) bool {
	if !strings.HasPrefix(v, "[") && !strings.HasPrefix(v, "{") {
		return true
	}
	depth := 0
	var quote byte
	for i := 0; i < len(v); i++ {
		c := v[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
		case '[', '{':
			depth++
		case ']', '}':
			depth--
		}
	}
	return depth == 0
}

// flowItems splits a one-line flow collection (`[a, b]`, `{a: x, b: {c: d}}`)
// into its top-level items, trimmed.
func flowItems(v string) []string {
	inner := strings.TrimSpace(v)
	if len(inner) < 2 {
		return nil
	}
	inner = inner[1 : len(inner)-1]
	var (
		items []string
		depth int
		quote byte
		start int
	)
	for i := 0; i < len(inner); i++ {
		c := inner[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
		case '[', '{':
			depth++
		case ']', '}':
			depth--
		case ',':
			if depth == 0 {
				items = append(items, strings.TrimSpace(inner[start:i]))
				start = i + 1
			}
		}
	}
	items = append(items, strings.TrimSpace(inner[start:]))
	out := items[:0]
	for _, it := range items {
		if it != "" {
			out = append(out, it)
		}
	}
	return out
}

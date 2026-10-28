package assets

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
)

// Prompt partials are the branches moved out of a base prompt so the common
// path loads fewer bytes. The registry is the filename rule, not a Go map:
// prompts/_<prompt>.<name>.md is partial <name> of prompts/<prompt>.md. A base
// prompt reaches a partial only through a fetch line naming the condition that
// triggers it:
//
//	**When** `<key>=<value>` — <prose>: `dross prompt partial <prompt> <name>`
//
// and may restate one after a compaction with a re-fetch reference:
//
//	**Re-fetch** if context was compacted: `dross prompt partial <prompt> <name>`
//
// _interaction.md has no `.<name>` part, so it is never a partial.

var (
	partialNameRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	partialFileRe = regexp.MustCompile(`^_([a-z][a-z0-9-]*)\.([a-z][a-z0-9-]*)\.md$`)
	// underscoreFileRe admits a partial or a playbook (_interaction.md).
	underscoreFileRe = regexp.MustCompile(`^_[a-z][a-z0-9-]*(\.[a-z][a-z0-9-]*)?\.md$`)
	invocationRe     = regexp.MustCompile("dross prompt partial ([^\\s`]+) ([^\\s`]+)")
	fetchLineRe      = regexp.MustCompile("^\\*\\*When\\*\\* `([^`=]+)=([^`]+)` — (.+): `dross prompt partial ([^\\s`]+) ([^\\s`]+)`$")
	refetchLineRe    = regexp.MustCompile("^\\*\\*Re-fetch\\*\\* if context was compacted: `dross prompt partial ([^\\s`]+) ([^\\s`]+)`$")
	// readPathRe catches a prompt pointing the agent at a partial's file
	// instead of fetching it: the file name itself, or the installed path.
	readPathRe = regexp.MustCompile(`_[a-z][a-z0-9-]*\.[a-z][a-z0-9-]*\.md|~/\.claude/dross/prompts/_`)
)

// TriggerVocabulary is the closed set of facts a fetch line may name. Each
// key's first value is the common path, the one that must never fetch.
var TriggerVocabulary = map[string][]string{
	"mode":         {"pair", "solo"},
	"preflight":    {"ok", "recovery"},
	"board_pull":   {"none", "reported"},
	"test_exit":    {"0", "1", "not-run"},
	"leg":          {"attached", "detached"},
	"mutation":     {"ran", "failed"},
	"scope":        {"ranged", "degraded"},
	"unclassified": {"zero", "nonzero"},
	"reviews":      {"none", "present"},
	"verdict":      {"pass", "partial", "fail"},
}

// alwaysWholeFile names gremlins' always-whole-file path. A trigger naming it
// would fire on every gremlins run, so no fetch line may.
var alwaysWholeFile = []string{"whole_file", "adapter-lacks-range-runner", "gremlins"}

// CommonPath returns the facts of the common path: every key at its first value.
func CommonPath() map[string]string {
	out := make(map[string]string, len(TriggerVocabulary))
	for k, vs := range TriggerVocabulary {
		out[k] = vs[0]
	}
	return out
}

// FetchPoint is one line of a base prompt that names a partial.
type FetchPoint struct {
	File    string // base prompt file, e.g. "execute.md"
	Line    int    // 1-based
	Prompt  string
	Partial string
	Key     string // trigger key; empty on a re-fetch reference
	Value   string // trigger value; empty on a re-fetch reference
	Refetch bool   // a re-fetch reference: never inlined, never a trigger
}

// Partial returns partial name of prompt, verbatim. Both names must match
// [a-z][a-z0-9-]*, so a lookup never leaves prompts/.
func Partial(fsys fs.FS, prompt, name string) ([]byte, error) {
	if err := checkPromptName(fsys, prompt); err != nil {
		return nil, err
	}
	if !partialNameRe.MatchString(name) {
		return nil, fmt.Errorf("partial name %q: must match [a-z][a-z0-9-]*", name)
	}
	b, err := fs.ReadFile(fsys, partialPath(prompt, name))
	if err == nil {
		return b, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	names, _ := PartialNames(fsys, prompt)
	if len(names) == 0 {
		return nil, fmt.Errorf("prompt %s has no partial %q (it has no partials)", prompt, name)
	}
	return nil, fmt.Errorf("prompt %s has no partial %q (valid: %s)", prompt, name, strings.Join(names, ", "))
}

// PartialNames lists prompt's partial names, sorted.
func PartialNames(fsys fs.FS, prompt string) ([]string, error) {
	if err := checkPromptName(fsys, prompt); err != nil {
		return nil, err
	}
	all, err := allPartials(fsys)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range all {
		if p[0] == prompt {
			out = append(out, p[1])
		}
	}
	return out, nil
}

// FetchPoints parses every partial reference in prompts/<file>. A line that
// invokes `dross prompt partial` outside the two grammars is an error naming
// file:line.
func FetchPoints(fsys fs.FS, file string) ([]FetchPoint, error) {
	b, err := fs.ReadFile(fsys, path.Join("prompts", file))
	if err != nil {
		return nil, err
	}
	points, problems := scanFetchLines(file, b)
	return points, joinProblems(problems)
}

// Fetched returns the partials prompt fetches when facts hold, in fetch order.
func Fetched(fsys fs.FS, prompt string, facts map[string]string) ([]string, error) {
	points, err := FetchPoints(fsys, prompt+".md")
	if err != nil {
		return nil, err
	}
	var out []string
	seen := map[string]bool{}
	for _, p := range points {
		if p.Refetch || facts[p.Key] != p.Value || seen[p.Partial] {
			continue
		}
		seen[p.Partial] = true
		out = append(out, p.Partial)
	}
	return out, nil
}

// Expand returns a prompt with each partial inlined right after the first
// fetch line naming it. A ".md" suffix is ignored; a name starting with "_"
// (a partial, or _interaction) comes back verbatim, as does a prompt that
// fetches nothing. Re-fetch references are never inlined.
func Expand(fsys fs.FS, name string) ([]byte, error) {
	name = strings.TrimSuffix(name, ".md")
	if strings.HasPrefix(name, "_") {
		if !underscoreFileRe.MatchString(name + ".md") {
			return nil, fmt.Errorf("prompt name %q: not a partial or playbook name", name)
		}
		return fs.ReadFile(fsys, path.Join("prompts", name+".md"))
	}
	if err := checkPromptName(fsys, name); err != nil {
		return nil, err
	}
	b, err := fs.ReadFile(fsys, path.Join("prompts", name+".md"))
	if err != nil {
		return nil, err
	}
	points, problems := scanFetchLines(name+".md", b)
	if err := joinProblems(problems); err != nil {
		return nil, err
	}
	at := map[int]string{} // line → partial inlined after it
	seen := map[string]bool{}
	for _, p := range points {
		if p.Refetch || seen[p.Partial] {
			continue
		}
		seen[p.Partial] = true
		at[p.Line] = p.Partial
	}
	if len(at) == 0 {
		return b, nil
	}
	var out bytes.Buffer
	for i, line := range bytes.SplitAfter(b, []byte("\n")) {
		out.Write(line)
		partial, ok := at[i+1]
		if !ok {
			continue
		}
		body, err := Partial(fsys, name, partial)
		if err != nil {
			return nil, fmt.Errorf("%s.md:%d: %w", name, i+1, err)
		}
		if !bytes.HasSuffix(line, []byte("\n")) {
			out.WriteByte('\n')
		}
		out.Write(body)
		if len(body) > 0 && !bytes.HasSuffix(body, []byte("\n")) {
			out.WriteByte('\n')
		}
	}
	return out.Bytes(), nil
}

// CheckPartials checks the whole registry over fsys: every reference parses,
// names an existing partial of its own prompt with a vocabulary trigger off the
// common path; every partial is fetched by at least one When line; and no
// prompt points the agent at a partial's file.
func CheckPartials(fsys fs.FS) error {
	bases, err := fs.Glob(fsys, "prompts/*.md")
	if err != nil {
		return err
	}
	all, err := allPartials(fsys)
	if err != nil {
		return err
	}
	exists := map[[2]string]bool{}
	for _, p := range all {
		exists[p] = true
	}
	fetched := map[[2]string]bool{}
	var problems []string
	for _, f := range bases {
		file := path.Base(f)
		b, err := fs.ReadFile(fsys, f)
		if err != nil {
			return err
		}
		if strings.HasPrefix(file, "_") {
			// A partial (or _interaction) never fetches: partials do not nest.
			for i, line := range strings.Split(string(b), "\n") {
				if invocationRe.MatchString(line) {
					problems = append(problems, fmt.Sprintf("%s:%d: partials do not nest; only a base prompt fetches", file, i+1))
				}
			}
			continue
		}
		prompt := strings.TrimSuffix(file, ".md")
		for i, line := range strings.Split(string(b), "\n") {
			if m := readPathRe.FindString(line); m != "" {
				problems = append(problems, fmt.Sprintf("%s:%d: names partial file %q; fetch it with `dross prompt partial` instead", file, i+1, m))
			}
		}
		points, perr := scanFetchLines(file, b)
		problems = append(problems, perr...)
		var refetches []FetchPoint
		for _, p := range points {
			at := fmt.Sprintf("%s:%d", file, p.Line)
			if p.Prompt != prompt {
				problems = append(problems, fmt.Sprintf("%s: fetches a partial of %s; a prompt fetches only its own", at, p.Prompt))
				continue
			}
			key := [2]string{p.Prompt, p.Partial}
			if !exists[key] {
				problems = append(problems, fmt.Sprintf("%s: names missing partial %s", at, partialPath(p.Prompt, p.Partial)))
				continue
			}
			if p.Refetch {
				refetches = append(refetches, p)
				continue
			}
			fetched[key] = true
		}
		for _, p := range refetches {
			if !fetched[[2]string{p.Prompt, p.Partial}] {
				problems = append(problems, fmt.Sprintf("%s:%d: re-fetches %s, which no When line fetches", file, p.Line, p.Partial))
			}
		}
	}
	for _, p := range all {
		if !fetched[p] {
			problems = append(problems, fmt.Sprintf("%s: orphan partial; no When line in %s.md fetches it", partialPath(p[0], p[1]), p[0]))
		}
	}
	return joinProblems(problems)
}

// scanFetchLines parses one base prompt's references, reporting every line
// that invokes the fetch command outside the grammar.
func scanFetchLines(file string, b []byte) ([]FetchPoint, []string) {
	var points []FetchPoint
	var problems []string
	for i, raw := range strings.Split(string(b), "\n") {
		if !invocationRe.MatchString(raw) {
			continue
		}
		at := fmt.Sprintf("%s:%d", file, i+1)
		line := strings.TrimSpace(raw)
		if m := refetchLineRe.FindStringSubmatch(line); m != nil {
			if !partialNameRe.MatchString(m[1]) || !partialNameRe.MatchString(m[2]) {
				problems = append(problems, fmt.Sprintf("%s: bad partial reference %s %s", at, m[1], m[2]))
				continue
			}
			points = append(points, FetchPoint{File: file, Line: i + 1, Prompt: m[1], Partial: m[2], Refetch: true})
			continue
		}
		m := fetchLineRe.FindStringSubmatch(line)
		if m == nil {
			problems = append(problems, fmt.Sprintf("%s: a fetch line must read **When** `<key>=<value>` — <prose>: `dross prompt partial <prompt> <name>`", at))
			continue
		}
		key, value, prose, prompt, name := m[1], m[2], m[3], m[4], m[5]
		values, ok := TriggerVocabulary[key]
		switch {
		case !ok:
			problems = append(problems, fmt.Sprintf("%s: unknown trigger key %q (vocabulary: %s)", at, key, strings.Join(vocabularyKeys(), ", ")))
			continue
		case !contains(values, value):
			problems = append(problems, fmt.Sprintf("%s: %s has no value %q (one of: %s)", at, key, value, strings.Join(values, ", ")))
			continue
		case value == values[0]:
			problems = append(problems, fmt.Sprintf("%s: %s=%s is the common path, which never fetches", at, key, value))
			continue
		}
		if w := namesAlwaysWholeFile(prose); w != "" {
			problems = append(problems, fmt.Sprintf("%s: trigger prose names %q, which holds on every gremlins run", at, w))
			continue
		}
		if !partialNameRe.MatchString(prompt) || !partialNameRe.MatchString(name) {
			problems = append(problems, fmt.Sprintf("%s: bad partial reference %s %s", at, prompt, name))
			continue
		}
		points = append(points, FetchPoint{File: file, Line: i + 1, Prompt: prompt, Partial: name, Key: key, Value: value})
	}
	return points, problems
}

// allPartials lists every partial file in fsys as {prompt, name}, sorted.
// Underscore files outside the filename rule (_interaction.md) are skipped.
func allPartials(fsys fs.FS) ([][2]string, error) {
	files, err := fs.Glob(fsys, "prompts/_*.md")
	if err != nil {
		return nil, err
	}
	var out [][2]string
	for _, f := range files {
		if m := partialFileRe.FindStringSubmatch(path.Base(f)); m != nil {
			out = append(out, [2]string{m[1], m[2]})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i][0] != out[j][0] {
			return out[i][0] < out[j][0]
		}
		return out[i][1] < out[j][1]
	})
	return out, nil
}

func checkPromptName(fsys fs.FS, prompt string) error {
	if !partialNameRe.MatchString(prompt) {
		return fmt.Errorf("prompt name %q: must match [a-z][a-z0-9-]*", prompt)
	}
	if _, err := fs.Stat(fsys, path.Join("prompts", prompt+".md")); err != nil {
		return fmt.Errorf("no prompt %q: %w", prompt, err)
	}
	return nil
}

func partialPath(prompt, name string) string {
	return path.Join("prompts", "_"+prompt+"."+name+".md")
}

func namesAlwaysWholeFile(prose string) string {
	lower := strings.ToLower(prose)
	for _, w := range alwaysWholeFile {
		if strings.Contains(lower, w) {
			return w
		}
	}
	return ""
}

func vocabularyKeys() []string {
	keys := make([]string, 0, len(TriggerVocabulary))
	for k := range TriggerVocabulary {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func joinProblems(problems []string) error {
	if len(problems) == 0 {
		return nil
	}
	return errors.New(strings.Join(problems, "\n"))
}

package cmd

import (
	"sort"
	"strings"
)

// Hint is one curated mis-reach and the invocation that actually works.
//
// It is structured rather than a bare sentence so the table can be checked
// against the real command tree: Command must resolve, and every entry in
// Flags must be declared on it. A hint that rots into pointing at nothing
// fails a test instead of misleading a user.
type Hint struct {
	Command string   // working command path, e.g. "dross task status"
	Flags   []string // flags the fix names, e.g. ["--files"]
	Fix     string   // the invocation to show the user
	Why     string   // one clause on why the reached-for form doesn't exist
}

// curatedHints maps a (command path, mis-reached token) pair to its working
// invocation. It is consulted before any string-distance fallback because
// these are semantic remaps — `task done` -> `task status <id> done` is a
// different verb with different arity, which edit distance can never find
// (locked hint_mechanism).
//
// Keys are normalised through hintKey, so `dross Task Done` hits the same
// entry as `dross task done`.
var curatedHints = map[string]Hint{
	hintKey("dross task", "done"): {
		Command: "dross task status",
		Fix:     "dross task status <phase-id> <task-id> done",
		Why:     "status is the verb; `done` is its value",
	},
	hintKey("dross phase create", "--title"): {
		Command: "dross phase create",
		Fix:     `dross phase create "<title>"`,
		Why:     "the title is a positional argument, not a flag",
	},
	hintKey("dross task edit", "--files"): {
		Command: "dross task add",
		Flags:   []string{"--files"},
		Fix:     "dross task add <phase-id> --files a.go,b.go",
		Why:     "`task edit` cannot change a task's files — they are set when the task is added",
	},
	hintKey("dross security run", "--new"): {
		Command: "dross security run",
		Fix:     "dross security run [path]",
		Why:     "every `security run` creates a new run dir; there is no --new",
	},
}

// hintKey normalises a lookup so case and surrounding whitespace can't miss an
// entry. The NUL separator keeps a path/token pair unambiguous.
func hintKey(cmdPath, token string) string {
	norm := func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
	return norm(cmdPath) + "\x00" + norm(token)
}

// CuratedHint looks up a known mis-reach. found=false means the table has no
// entry and the caller should fall back to Nearest.
func CuratedHint(cmdPath, token string) (Hint, bool) {
	h, ok := curatedHints[hintKey(cmdPath, token)]
	return h, ok
}

// nearestMaxDistance is deliberately one wider than cobra's
// SuggestionsMinimumDistance (2, set in EnforceSubcommandKnown). At the same
// threshold this fallback would only ever repeat what cobra already found; the
// extra step is what makes it reach typos cobra silently gives up on.
const nearestMaxDistance = 3

// Nearest returns the candidates closest to typed, or nothing when none is
// within nearestMaxDistance. A far-off typo gets no suggestion rather than a
// fabricated one.
func Nearest(typed string, candidates []string) []string {
	typed = strings.ToLower(strings.TrimSpace(typed))
	if typed == "" {
		return nil
	}
	best := nearestMaxDistance + 1
	var out []string
	for _, c := range candidates {
		d := levenshtein(typed, strings.ToLower(c))
		switch {
		case d > nearestMaxDistance || d > best:
			continue
		case d < best:
			best, out = d, []string{c}
		default:
			out = append(out, c)
		}
	}
	sort.Strings(out)
	return out
}

// levenshtein is the standard edit distance over runes, two rows at a time.
func levenshtein(a, b string) int {
	ar, br := []rune(a), []rune(b)
	prev := make([]int, len(br)+1)
	cur := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		cur[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, min(cur[j-1]+1, prev[j-1]+cost))
		}
		prev, cur = cur, prev
	}
	return prev[len(br)]
}

// CuratedMisreaches returns every (command path, token) pair the table knows to
// be broken, so callers — the prompt-corpus guard, and the tree check in
// cmd/dross — can iterate the table rather than re-listing it.
func CuratedMisreaches() [][2]string {
	var out [][2]string
	for k := range curatedHints {
		path, token, _ := strings.Cut(k, "\x00")
		out = append(out, [2]string{path, token})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i][0] != out[j][0] {
			return out[i][0] < out[j][0]
		}
		return out[i][1] < out[j][1]
	})
	return out
}

// LineTeachesMisreach reports whether one line of text advertises an
// invocation the curated table classifies as broken.
//
// A subcommand mis-reach must appear adjacent (`dross task done`), which is
// exactly what keeps the *correct* `dross task status <id> <id> done` from
// matching. A flag mis-reach matches when the line names both the command and
// the flag, since a flag legitimately trails positional arguments.
func LineTeachesMisreach(line string) (cmdPath, token string, found bool) {
	lower := strings.ToLower(line)
	for _, mr := range CuratedMisreaches() {
		path, tok := mr[0], mr[1]
		if strings.HasPrefix(tok, "-") {
			if strings.Contains(lower, path) && containsWord(lower, tok) {
				return path, tok, true
			}
			continue
		}
		if containsWord(lower, path+" "+tok) {
			return path, tok, true
		}
	}
	return "", "", false
}

// containsWord reports whether s contains sub bounded by a non-word character
// (or a string edge), so `--new` doesn't match inside `--newest`.
func containsWord(s, sub string) bool {
	for i := 0; ; {
		j := strings.Index(s[i:], sub)
		if j < 0 {
			return false
		}
		j += i
		end := j + len(sub)
		if end == len(s) || !isWordByte(s[end]) {
			return true
		}
		i = j + 1
	}
}

func isWordByte(b byte) bool {
	return b == '-' || b == '_' || b == '.' || (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

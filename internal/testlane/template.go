package testlane

import (
	"fmt"
	"strings"
)

// The two placeholders a lane's selector_template may carry.
//
// PathToken is not a prefix hazard for PathsToken: "{path}" ends in its own
// brace, so a strings.Contains for it never fires on "{paths}".
const (
	PathToken  = "{path}"
	PathsToken = "{paths}"
)

// Expand renders one lane's selector_template against the paths that matched
// it, returning the shell-ready fragments appended to that lane's command.
//
// It is the placement half of the translation Derive shapes: Derive decides
// whether the substituted values are files, dirs or Go packages, Expand decides
// where they land on the line. That split is what lets a cargo lane substitute
// directories without the template having to reshape paths itself.
//
// Two placeholders, both real runner shapes and neither expressible as the
// other. PathToken repeats the WHOLE template once per path, which is what
// `--package {path}` over two paths must become. PathsToken substitutes them
// into a single instance — separate tokens by default, one token joined by
// `join` when a join is declared, which is the only way `-R {paths}` reaches
// ctest as one regex alternation.
//
// A template carrying BOTH composes rather than conflicting: PathToken drives
// the repetition and PathsToken names the whole set inside each copy. Every
// copy is therefore identical in its PathsToken part, which is what a runner
// wanting "this path, in the context of all of them" asks for.
//
// A template containing NEITHER is an error and returns no fragments. Both
// halves matter: a caller that ignored the error and spawned anyway would run
// the lane's whole suite under a scoped lane's name — the silent unscoped run
// this feature exists to avoid.
//
// Substituted paths go through the same option-safe and shell-quoting
// treatment the plain append path gives them, mid-expansion, because nothing
// downstream can tell template text from a substituted path once the two are
// one string. The template's OWN text is emitted verbatim: it is the user's
// consented line, fenced by consent exactly as command and prepare are, and
// quoting it would break every legitimate flag and regex in it.
//
// With no paths, a well-formed template yields no fragments and no error — the
// call is then a pure fence on the template's shape, which is how the run site
// refuses a malformed template before any lane spawns.
func Expand(template, join string, paths []string) ([]string, error) {
	hasPath := strings.Contains(template, PathToken)
	hasPaths := strings.Contains(template, PathsToken)
	if !hasPath && !hasPaths {
		return nil, fmt.Errorf("selector_template %q contains neither %s nor %s — a template with no placeholder substitutes nothing, so the lane would run unscoped", template, PathToken, PathsToken)
	}
	if len(paths) == 0 {
		return nil, nil
	}

	// Quoted once, up front, and reused by both branches: a path is quoted
	// exactly as the plain append path quotes it, and the joined form quotes
	// the JOINED string rather than its members, since `-R 'a|b'` is one
	// argument whose separator must not be swallowed by a quote boundary.
	quoted := make([]string, 0, len(paths))
	safe := make([]string, 0, len(paths))
	for _, p := range paths {
		s := optionSafe(p)
		safe = append(safe, s)
		quoted = append(quoted, ShellQuote(s))
	}

	all := strings.Join(quoted, " ")
	if join != "" {
		all = ShellQuote(strings.Join(safe, join))
	}

	if !hasPath {
		return []string{strings.ReplaceAll(template, PathsToken, all)}, nil
	}
	out := make([]string, 0, len(paths))
	for _, q := range quoted {
		frag := strings.ReplaceAll(template, PathToken, q)
		out = append(out, strings.ReplaceAll(frag, PathsToken, all))
	}
	return out, nil
}

// ShellQuote single-quotes an argument for `sh -c`, the same allowlist-free
// approach internal/remote uses: wrap in single quotes and escape any embedded
// single quote, which is total rather than a metacharacter blocklist.
//
// It lives here rather than in internal/cmd because template expansion has to
// quote mid-flight — by the time a fragment reaches the run site, template text
// and substituted path are one string and nothing can tell them apart. One
// implementation serves the plain append path, the template path, and `dross
// run`; a second copy anywhere would be a second answer to "what is safe".
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

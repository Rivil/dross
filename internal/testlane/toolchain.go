package testlane

import "strings"

// Toolchain reports the binaries one lane needs on the host that runs it.
//
// It is the whole of the locked toolchain_source decision: a lane's toolchain
// is DERIVED from the first whitespace token of its command and prepare lines,
// with an optional override replacing that derived list wholesale. Derivation
// is what makes the feature work on every lane already declared — a
// declared-only field would leave locality detection dead until every existing
// lane was rewritten by hand.
//
// The command's token comes first and the prepare's second. The order is not
// cosmetic: it is the order the probe reports missing tools in, and a lane is
// far more often understood by what it runs than by what it bootstraps.
//
// Pure, like the rest of this package: nothing here asks the filesystem whether
// a token exists. Probing is the caller's, on whichever host it is deciding
// about — the same derived list is asked of the remote and of this machine.
func Toolchain(command, prepare string, override []string) []string {
	// A non-empty override REPLACES the derived list rather than extending it.
	// The field exists for the lines derivation gets wrong — an env prefix, a
	// wrapper script, a `mise exec` — and appending would keep probing the
	// very token the user overrode to say was not the binary.
	if tools := clean(override); len(tools) > 0 {
		return tools
	}
	return clean([]string{firstToken(command), firstToken(prepare)})
}

// firstToken returns the leading whitespace-delimited word of a command line,
// or "" when there is none.
//
// Verbatim, deliberately: `FOO=1 go test ./...` derives `FOO=1` and not `go`.
// The locked rule is first-token, and a quiet widening — skipping assignments,
// unwrapping `env`, following `sh -c` — would be a shell parser growing inside
// a locality check, each rule right until the line it is wrong about. A lane
// whose first token is not a binary is surfaced by `dross doctor` with the
// `--toolchain` override as its fix, which is a message the user can act on
// rather than a guess they cannot see.
func firstToken(line string) string {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// clean drops blanks and repeats, keeping first-seen order.
//
// Both halves are load-bearing. A blank token would reach the probe as
// `command -v ""`, which no host satisfies, so every lane with an empty prepare
// would report its toolchain missing and fall back forever. And a repeat — the
// common `go build` prepare beside a `go test` command — would double the
// `command -v` calls the run pays for on a host it has to reach over ssh.
func clean(tools []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(tools))
	for _, tool := range tools {
		tool = strings.TrimSpace(tool)
		if tool == "" || seen[tool] {
			continue
		}
		seen[tool] = true
		out = append(out, tool)
	}
	return out
}

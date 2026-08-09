// Package argfence holds the per-tool argv policy dross applies to every value
// it hands a subprocess, and the two runtime primitives that enforce it:
// Fence for the tools with an end-of-options token, RejectLeadingDash for the
// tools without one.
//
// See policy.go for the table and the reasoning behind the split.
package argfence

import (
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrLeadingDash is returned when a derived value begins with "-" and the
	// receiving tool has no end-of-options token to fence it behind. Callers
	// wrap it; tests match it with errors.Is.
	ErrLeadingDash = errors.New("value begins with a dash and the tool has no end-of-options separator")
	// ErrNoPolicy is returned for a binary with no table entry. It is an error
	// rather than a pass-through: the boundary fails closed, so an unlisted
	// tool can never be treated as fenced.
	ErrNoPolicy = errors.New("no argv policy for this tool")
)

// RejectLeadingDash refuses a derived value that would be read as an option by
// a tool that cannot be told to stop reading options.
//
// field names which config or state value it is (e.g. "package", "output dir")
// so the error points at the line the user has to fix; tool names the binary
// that would have received it. Both appear in the message because a bare
// "leading dash rejected" tells the user nothing about where to look.
//
// An empty value is not a flag and is not rejected — over-rejection would push
// callers toward bypassing this helper, which is worse than the case it guards.
func RejectLeadingDash(tool, field, value string) error {
	if !strings.HasPrefix(value, "-") {
		return nil
	}
	return fmt.Errorf("%s: %s %q begins with a dash: %w", tool, field, value, ErrLeadingDash)
}

// Fence returns the argv fragment carrying values for tool, applying whichever
// defence the table names:
//
//   - Separator tools get the fragment prefixed with their end-of-options
//     token, emitted unconditionally. Callers append the result after their own
//     literal flags — the separator fences the derived values, and demoting the
//     flags dross chose past it would break the command (in cobra it would make
//     them positionals).
//   - Reject tools get the values back verbatim, after every one has been
//     checked for a leading dash.
//
// On any error the returned argv is nil, never a best-effort fragment: a caller
// that drops the error must not be handed something runnable.
func Fence(tool, field string, values ...string) ([]string, error) {
	r, ok := PolicyFor(tool)
	if !ok {
		return nil, fmt.Errorf("%s: %w", tool, ErrNoPolicy)
	}
	switch r.Kind {
	case Separator:
		out := make([]string, 0, len(values)+1)
		out = append(out, r.Token)
		return append(out, values...), nil
	case Reject:
		for _, v := range values {
			if err := RejectLeadingDash(tool, field, v); err != nil {
				return nil, err
			}
		}
		return append([]string(nil), values...), nil
	default:
		return nil, fmt.Errorf("%s: kind %d: %w", tool, r.Kind, ErrNoPolicy)
	}
}

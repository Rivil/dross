// Package redact strips credential values out of text that is about to be
// emitted — an error message, a printed line, a telemetry payload.
//
// It exists because every forge and board client in dross does the same two
// things: it sets an Authorization header from a token, and on a non-2xx it
// echoes a snippet of the upstream response body into its error. Those two
// facts compose badly. An API that mirrors the request headers back in its
// error body — and several do, for debugging — turns dross's own error message
// into the exfiltration channel, and that message goes to stderr, into the
// telemetry JSONL, and into whatever the agent pastes back.
//
// The defence is deliberately NOT "don't echo the body". The snippet is what
// makes a 403 diagnosable. The defence is to remove the credential from the
// text on the way out, wherever the text came from.
//
// Two properties it must have, both learned the hard way elsewhere in this
// milestone:
//
//   - It removes the ENCODED forms too, not just the literal token. HTTP Basic
//     sends base64(user:token); a scrubber that only knows the raw token leaves
//     a perfectly usable credential in the message. Rather than asking each
//     caller which user string it paired with the token, Scrub decodes the
//     base64-looking runs it finds and redacts any that contain the token —
//     so it catches base64(email:token), base64(token) and forms nobody
//     anticipated, with no plumbing.
//
//   - The marker names the env var. A message reduced to "[redacted]" is safe
//     and useless; "[redacted $GITHUB_TOKEN]" still tells the user which
//     credential the failing request was carrying.
package redact

import (
	"encoding/base64"
	"regexp"
	"strings"
)

// minScrubLen is the shortest token value Scrub will act on.
//
// A one- or two-character env value is not a credential — it is a placeholder,
// a typo, or a test fixture — and replacing every occurrence of it would shred
// the message into unreadability, which is its own kind of failure: the user
// loses the diagnostic AND gains no security. Three is the floor because it is
// the shortest value that could plausibly be meant as a secret.
const minScrubLen = 3

// base64Run matches a run of base64 alphabet characters long enough to be worth
// decoding. Both the standard and URL-safe alphabets are covered, with optional
// padding, because which one an upstream chose is not knowable here.
var base64Run = regexp.MustCompile(`[A-Za-z0-9+/_-]{8,}={0,2}`)

// Marker returns the replacement text for a redacted credential, naming the env
// var it came from when one is known.
func Marker(envName string) string {
	if envName == "" {
		return "[redacted]"
	}
	return "[redacted $" + envName + "]"
}

// Scrub removes every form of token from s, replacing it with a marker naming
// envName.
//
// An empty token returns s unchanged — an unset credential must not turn every
// message into a redaction marker. So does a token shorter than minScrubLen.
func Scrub(s, envName, token string) string {
	if len(token) < minScrubLen {
		return s
	}
	marker := Marker(envName)
	// Encoded forms first, while the raw token is still intact to match
	// decoded output against.
	s = base64Run.ReplaceAllStringFunc(s, func(run string) string {
		if strings.Contains(run, token) {
			// A run that literally contains the token is handled by the raw
			// replacement below; leaving it lets that pass do the work and
			// keeps the surrounding characters.
			return run
		}
		if decodedContains(run, token) {
			return marker
		}
		return run
	})
	return strings.ReplaceAll(s, token, marker)
}

// Err returns err with every form of token removed from its message.
//
// A nil error stays nil, and an error whose message did not contain the token
// is returned untouched so the error chain is preserved exactly. Only when
// something was actually redacted is the error wrapped — and the wrapper keeps
// Unwrap, so errors.Is/As still work through it.
func Err(err error, envName, token string) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	clean := Scrub(msg, envName, token)
	if clean == msg {
		return err
	}
	return &scrubbedError{msg: clean, err: err}
}

type scrubbedError struct {
	msg string
	err error
}

func (e *scrubbedError) Error() string { return e.msg }
func (e *scrubbedError) Unwrap() error { return e.err }

// decodedContains reports whether run decodes, under any of the four base64
// variants, to text containing token. Encodings are tried rather than guessed:
// the caller has no way to know which one an upstream service used.
func decodedContains(run, token string) bool {
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	} {
		if b, err := enc.DecodeString(run); err == nil && strings.Contains(string(b), token) {
			return true
		}
	}
	return false
}

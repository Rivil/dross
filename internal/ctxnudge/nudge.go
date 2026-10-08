package ctxnudge

import (
	"fmt"
	"strings"
)

// Marker opens every nudge line. The interaction playbook keys its checkpoint
// rule on this exact text, so the two must change together.
const Marker = "context checkpoint:"

// step is the growth between repeat nudges in one session: the first fires at
// the threshold, the next at threshold+step, and so on. Fixed, not configured
// (locked nudge_cadence) — the threshold is the one knob.
const step int64 = 50_000

// Band places tokens against threshold: 0 below it (or when the nudge is off),
// 1 from the threshold up, and one more per further step. A session nudges once
// per band it reaches, however far one fire jumps.
func Band(tokens, threshold int64) int64 {
	if threshold <= 0 || tokens < threshold {
		return 0
	}
	return 1 + (tokens-threshold)/step
}

// Line renders the nudge: usage against the threshold, the checkpoint advice,
// the re-entry command a fresh session's SessionStart line will print, and the
// pause fallback for a thread that isn't on disk yet (locked nudge_content).
// It is always one line, whatever reentry carries.
func Line(tokens, threshold int64, reentry string) string {
	reentry = strings.Join(strings.Fields(reentry), " ")
	return fmt.Sprintf("%s %dk / %dk tokens — checkpoint at your next durable boundary → /clear, then %s · mid-thought? /dross-pause first",
		Marker, tokens/1000, threshold/1000, reentry)
}

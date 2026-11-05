package memadvise

import "testing"

func TestGuardPhrases(t *testing.T) {
	for _, body := range []string{
		"NEVER push to main.",
		"ASK before merging.",
		"This flag is a DECOY.",
		"REOPENED 2026-10-01.",
		"don't re-raise the locking question",
		"don't revert the squash setting",
	} {
		if !Guarded([]byte("---\nname: x\n---\n" + body + "\n")) {
			t.Errorf("%q does not guard", body)
		}
	}
	// The whole file counts, frontmatter included.
	if !Guarded([]byte("---\ndescription: NEVER drop this\n---\nbody\n")) {
		t.Error("a guard phrase in the frontmatter does not guard")
	}
}

func TestGuardWholeWordOnly(t *testing.T) {
	for _, body := range []string{
		"the TASK is done",
		"the user asked for it",
		"I'll ask later",
		"NEVERTHELESS it shipped",
		"Nevermind, never mind",
		"ask, never, decoy, reopened in lowercase",
		"DEAD done RESOLVED DECLINED SHIPPED",
	} {
		if Guarded([]byte(body)) {
			t.Errorf("%q guards", body)
		}
	}
}

func TestGuardApostropheVariants(t *testing.T) {
	for _, body := range []string{
		"don’t revert this",
		"Don't re-raise it",
		"DON'T REVERT",
	} {
		if !Guarded([]byte(body)) {
			t.Errorf("%q does not guard", body)
		}
	}
	if Guarded([]byte("dont revert")) {
		t.Error("a phrase without an apostrophe guards")
	}
}

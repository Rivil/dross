package consent

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// untouchedStore fails the test on any access, for the case where reaching the
// store at all is the bug.
type untouchedStore struct{ t *testing.T }

func (s untouchedStore) Load() (*Grants, error) {
	s.t.Fatal("the store was read for a repo with no exempt globs")
	return nil, nil
}

func (s untouchedStore) Save(*Grants) error {
	s.t.Fatal("the store was written for a repo with no exempt globs")
	return nil
}

func mustGrantExempt(t *testing.T, store Store, globs ...string) {
	t.Helper()
	if err := GrantExemptConsent(store, globs); err != nil {
		t.Fatalf("grant %v: %v", globs, err)
	}
}

// TestAppendingAGlobStalesTheExemptGrant: this is how an agent would widen what
// commits unmeasured. It reports Stale rather than Absent because something WAS
// trusted here and the list has since changed.
func TestAppendingAGlobStalesTheExemptGrant(t *testing.T) {
	store, _, repoDir := consentFixture(t)
	mustGrantExempt(t, store, "docs/**")

	state, err := ExemptConsented(store, repoDir, []string{"docs/**", "src/**"})
	if state != Stale {
		t.Errorf("state after appending a glob = %v, want stale", state)
	}
	if !errors.Is(err, ErrStaleConsent) {
		t.Errorf("err = %v, want ErrStaleConsent", err)
	}
}

// TestReorderingGlobsKeepsTheExemptGrant: a reorder exempts nothing new, and a
// grant that went stale on one would teach the user to re-consent unread.
func TestReorderingGlobsKeepsTheExemptGrant(t *testing.T) {
	store, _, repoDir := consentFixture(t)
	mustGrantExempt(t, store, "docs/**", "*.md", "assets/**")

	reordered := []string{"*.md", "assets/**", "docs/**"}
	if state, err := ExemptConsented(store, repoDir, reordered); state != Granted || err != nil {
		t.Errorf("state after a reorder = %v, %v; want granted", state, err)
	}
	if state, err := ExemptConsented(store, repoDir, []string{"assets/**", "docs/**", "*.md"}); state != Granted || err != nil {
		t.Errorf("state after a second reorder = %v, %v; want granted", state, err)
	}
}

// TestExemptLineLeavesTheCallersSliceAlone: the slice is project.toml's document
// order, and sorting it in place would rewrite the list on the next Save.
func TestExemptLineLeavesTheCallersSliceAlone(t *testing.T) {
	globs := []string{"docs/**", "*.md"}
	ExemptLine(globs)
	if got := SortedExempt(globs); !reflect.DeepEqual(got, []string{"*.md", "docs/**"}) {
		t.Errorf("SortedExempt = %v, want sorted", got)
	}
	if !reflect.DeepEqual(globs, []string{"docs/**", "*.md"}) {
		t.Errorf("the caller's slice was reordered: %v", globs)
	}
}

// TestExemptLineKeepsGlobsApart: each pair below is a different list that a join
// would collapse into one grant.
func TestExemptLineKeepsGlobsApart(t *testing.T) {
	for _, tc := range []struct {
		name string
		a, b []string
	}{
		{"a separator inside a glob", []string{"a,b"}, []string{"a", "b"}},
		{"a re-split across two globs", []string{"ab", "c"}, []string{"a", "bc"}},
		{"a NUL inside a glob", []string{"a\x00b"}, []string{"a", "b"}},
		{"a duplicate", []string{"a", "a"}, []string{"a"}},
		{"a respaced glob", []string{"a "}, []string{"a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if ExemptLine(tc.a) == ExemptLine(tc.b) {
				t.Errorf("%q and %q produce the same line", tc.a, tc.b)
			}
		})
	}
}

// TestExemptLineIsFramed: a bare command fingerprint must not satisfy the
// exempt grant, whatever the command is spelled like.
func TestExemptLineIsFramed(t *testing.T) {
	line := ExemptLine([]string{"docs/**"})
	if !strings.HasPrefix(line, ExemptFrame) {
		t.Errorf("line %q does not start with the frame", line)
	}
	if !strings.Contains(ExemptFrame, "\x00") {
		t.Error("the frame carries no NUL, so a command line could spell it")
	}
	if line == "docs/**" {
		t.Error("a one-glob list hashed as the bare glob")
	}
}

// TestNoExemptGlobsNeedsNoGrant: empty is not untrusted. The store is never
// read, and a tracked local.toml does not change the answer, because a repo
// with nothing exempt has nothing for that store to say.
func TestNoExemptGlobsNeedsNoGrant(t *testing.T) {
	_, _, repoDir := consentFixture(t)

	for _, globs := range [][]string{nil, {}} {
		state, err := ExemptConsented(untouchedStore{t}, repoDir, globs)
		if state != NotApplicable {
			t.Errorf("state for %#v = %v, want not-applicable", globs, state)
		}
		if !errors.Is(err, ErrNoTestExempt) {
			t.Errorf("err for %#v = %v, want ErrNoTestExempt", globs, err)
		}
	}
	if line := ExemptLine(nil); line != "" {
		t.Errorf("ExemptLine(nil) = %q, want empty", line)
	}

	store, _, tracked := consentFixture(t)
	if err := store.Save(&Grants{TrustedTestCommand: Fingerprint("x")}); err != nil {
		t.Fatal(err)
	}
	git(t, tracked, "add", "-f", RelPath)
	if state, _ := ExemptConsented(store, tracked, nil); state != NotApplicable {
		t.Errorf("state with a tracked store and no globs = %v, want not-applicable", state)
	}
}

// TestExemptConsentLadder walks the states a list with globs can be in, so a
// refusal can keep telling the reader which one they are in.
func TestExemptConsentLadder(t *testing.T) {
	globs := []string{"docs/**"}

	t.Run("absent", func(t *testing.T) {
		store, _, repoDir := consentFixture(t)
		state, err := ExemptConsented(store, repoDir, globs)
		if state != Absent || !errors.Is(err, ErrNoConsent) {
			t.Errorf("got %v, %v; want absent, ErrNoConsent", state, err)
		}
	})

	t.Run("another grant is not this one", func(t *testing.T) {
		store, _, repoDir := consentFixture(t)
		if err := GrantConsent(store, "go test ./..."); err != nil {
			t.Fatal(err)
		}
		if state, _ := ExemptConsented(store, repoDir, globs); state != Absent {
			t.Errorf("state = %v, want absent", state)
		}
	})

	t.Run("unparseable store fails closed", func(t *testing.T) {
		store, _, repoDir := consentFixture(t)
		if err := os.WriteFile(store.path, []byte("not = = toml"), 0o644); err != nil {
			t.Fatal(err)
		}
		state, err := ExemptConsented(store, repoDir, globs)
		if state != Absent || !errors.Is(err, ErrNoConsent) {
			t.Errorf("got %v, %v; want absent, ErrNoConsent", state, err)
		}
	})

	t.Run("tracked store is refused unread", func(t *testing.T) {
		store, _, repoDir := consentFixture(t)
		mustGrantExempt(t, store, globs...)
		git(t, repoDir, "add", "-f", RelPath)
		state, err := ExemptConsented(store, repoDir, globs)
		if state != Refused || err == nil || !strings.Contains(err.Error(), "tracked") {
			t.Errorf("got %v, %v; want refused naming the tracked store", state, err)
		}
	})

	t.Run("granted", func(t *testing.T) {
		store, _, repoDir := consentFixture(t)
		mustGrantExempt(t, store, globs...)
		if state, err := ExemptConsented(store, repoDir, globs); state != Granted || err != nil {
			t.Errorf("got %v, %v; want granted", state, err)
		}
	})
}

// TestGrantExemptLeavesTheOtherGrantsAlone: re-consenting to the list must not
// cost the user their test command or lane grants.
func TestGrantExemptLeavesTheOtherGrantsAlone(t *testing.T) {
	store, root, _ := consentFixture(t)
	seed := Grants{
		TrustedTestCommand:  "aaaa",
		TrustedLaneCommands: map[string]string{"go": "bbbb"},
		TrustedLaneInstalls: map[string]string{"go": "cccc"},
	}
	if err := store.Save(&seed); err != nil {
		t.Fatal(err)
	}
	mustGrantExempt(t, store, "docs/**")

	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.TrustedTestExempt != Fingerprint(ExemptLine([]string{"docs/**"})) {
		t.Errorf("TrustedTestExempt = %q, want the list's fingerprint", got.TrustedTestExempt)
	}
	got.TrustedTestExempt = ""
	if !reflect.DeepEqual(*got, seed) {
		t.Errorf("a sibling grant changed:\n got  %+v\n want %+v", *got, seed)
	}
	body, err := os.ReadFile(filepath.Join(root, File))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "docs/**") {
		t.Errorf("the store holds the globs themselves, not their fingerprint:\n%s", body)
	}
}

// TestExemptRefusalNamesEveryGlobAndTheHumanStep: the refusal is what the commit
// gate shows, so it has to carry the list and route to a human rather than to a
// re-run or to an agent's own `dross trust`.
func TestExemptRefusalNamesEveryGlobAndTheHumanStep(t *testing.T) {
	globs := []string{"docs/**", "*.md"}

	for _, tc := range []struct {
		name  string
		state State
		cerr  error
		want  string
	}{
		{"stale", Stale, ErrStaleConsent, "CHANGED"},
		{"absent", Absent, ErrNoConsent, "has not trusted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ExemptRefusal(globs, tc.state, tc.cerr)
			if !errors.Is(err, tc.cerr) {
				t.Errorf("the refusal lost its sentinel: %v", err)
			}
			for _, want := range []string{tc.want, "    *.md\n    docs/**\n", "dross trust --test-exempt", "a human"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal does not contain %q:\n%v", want, err)
				}
			}
		})
	}

	t.Run("stale and absent read differently", func(t *testing.T) {
		if err := ExemptRefusal(globs, Absent, ErrNoConsent); strings.Contains(err.Error(), "CHANGED") {
			t.Errorf("a first run was reported as stale: %v", err)
		}
	})

	for _, state := range []State{Refused, NotApplicable} {
		t.Run(state.String()+" passes the cause through", func(t *testing.T) {
			cause := errors.New("the cause")
			if err := ExemptRefusal(globs, state, cause); err != cause {
				t.Errorf("err = %v, want the cause unchanged", err)
			}
		})
	}
}

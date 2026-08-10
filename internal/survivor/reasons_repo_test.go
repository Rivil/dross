package survivor

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// This file audits THIS repo's own .dross/survivors.toml. An acceptance is the
// only lifecycle state that silences a survivor, so its reason is the whole of
// the review record — and prose is not reviewable unless it points at something
// a reader can check. The rule enforced here is the cheapest checkable version
// of that: every effective reason must name a Go test that actually exists.
//
// "Needs a tty", "unreachable in practice" and "coverage tool limitation" all
// read like reasons and none of them can be falsified. A cited TestXxx can:
// delete the test and this audit goes red.

// testNamePattern matches a Go test identifier as it appears inside prose.
var testNamePattern = regexp.MustCompile(`\bTest[A-Za-z0-9_]+`)

// reasonProblem is one audit finding, always naming the acceptance key so the
// offending entry is addressable without re-reading the store.
type reasonProblem struct {
	Key    string
	Detail string
}

func (p reasonProblem) String() string { return p.Key + ": " + p.Detail }

// auditReasons checks every acceptance's EFFECTIVE reason — its own prose, or
// the prose of the category it names — so a shared category cannot launder an
// uncheckable reason across a dozen entries.
func auditReasons(s *Store, exists func(name string) bool) []reasonProblem {
	var problems []reasonProblem
	for _, a := range s.Accepted {
		reason, err := s.ReasonFor(a)
		if err != nil {
			problems = append(problems, reasonProblem{a.Key, err.Error()})
			continue
		}
		names := testNamePattern.FindAllString(reason, -1)
		if len(names) == 0 {
			problems = append(problems, reasonProblem{a.Key,
				"reason names no Go test — an acceptance whose justification cannot be checked is an assertion"})
			continue
		}
		for _, n := range names {
			if !exists(n) {
				problems = append(problems, reasonProblem{a.Key,
					"reason cites " + n + ", which does not exist in this repo"})
			}
		}
	}
	return problems
}

// repoRootFromHere walks up to the directory holding .dross.
func repoRootFromHere(t *testing.T) string {
	t.Helper()
	path, err := LocatePath(".")
	if err != nil {
		t.Fatalf("locate store: %v", err)
	}
	return filepath.Dir(filepath.Dir(path))
}

// repoTestNames collects every TestXxx defined anywhere in the repo.
func repoTestNames(t *testing.T, root string) map[string]bool {
	t.Helper()
	decl := regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]*)\(`)
	names := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range decl.FindAllStringSubmatch(string(b), -1) {
			names[m[1]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo for test names: %v", err)
	}
	if len(names) == 0 {
		t.Fatal("found no test declarations in the repo — the audit would pass vacuously")
	}
	return names
}

// TestReasonAuditCatchesUncheckableProse checks the checker, against both ways
// a reason can fail to be evidence. A validator that passed either of these
// would make the real-store audit below vacuous — and a vacuous audit over
// acceptances reads exactly like a rigorous one.
func TestReasonAuditCatchesUncheckableProse(t *testing.T) {
	exists := func(name string) bool { return name == "TestThatDoesExist" }

	cases := []struct {
		name       string
		store      *Store
		wantKey    string
		wantDetail string
	}{
		{
			name: "reason cites no test at all",
			store: &Store{Accepted: []Acceptance{{
				Key: "k-prose", File: "a.go", Op: "OP", Text: "x",
				Reason: "this branch is unreachable in practice and needs a tty",
			}}},
			wantKey:    "k-prose",
			wantDetail: "names no Go test",
		},
		{
			name: "reason cites a test that does not exist",
			store: &Store{Accepted: []Acceptance{{
				Key: "k-ghost", File: "a.go", Op: "OP", Text: "x",
				Reason: "unreachable — pinned by TestThatDoesNotExist",
			}}},
			wantKey:    "k-ghost",
			wantDetail: "does not exist in this repo",
		},
		{
			name: "a category's prose is audited for its members",
			store: &Store{
				Categories: []Category{{Name: "shared", Reason: "they are simply fine"}},
				Accepted: []Acceptance{{
					Key: "k-cat", File: "a.go", Op: "OP", Text: "x", Category: "shared",
				}},
			},
			wantKey:    "k-cat",
			wantDetail: "names no Go test",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			problems := auditReasons(tc.store, exists)
			if len(problems) != 1 {
				t.Fatalf("got %d problems, want exactly 1: %v", len(problems), problems)
			}
			if problems[0].Key != tc.wantKey {
				t.Errorf("problem names key %q, want %q — a finding that does not name the entry is not actionable", problems[0].Key, tc.wantKey)
			}
			if !strings.Contains(problems[0].Detail, tc.wantDetail) {
				t.Errorf("detail = %q, want it to mention %q", problems[0].Detail, tc.wantDetail)
			}
		})
	}

	// And the passing shape: a reason citing a test that exists is clean.
	ok := &Store{Accepted: []Acceptance{{
		Key: "k-good", File: "a.go", Op: "OP", Text: "x",
		Reason: "unreachable through the CLI — pinned by TestThatDoesExist",
	}}}
	if problems := auditReasons(ok, exists); len(problems) != 0 {
		t.Errorf("a reason citing an existing test was flagged: %v", problems)
	}
}

// TestRepoAcceptanceReasonsCiteRealTests runs the audit against this repo's
// actual store. It is the gate that keeps the drain honest as it scales: this
// phase multiplies the acceptance count several-fold, and at that volume the
// only thing standing between "drained" and "silenced" is whether each reason
// points at something checkable.
func TestRepoAcceptanceReasonsCiteRealTests(t *testing.T) {
	root := repoRootFromHere(t)
	store, err := Load(Path(filepath.Join(root, RootDirName)))
	if err != nil {
		t.Fatalf("load repo store: %v", err)
	}
	if len(store.Accepted) == 0 {
		t.Skip("no acceptances recorded yet — nothing to audit")
	}

	names := repoTestNames(t, root)
	problems := auditReasons(store, func(n string) bool { return names[n] })
	for _, p := range problems {
		t.Errorf("%s", p)
	}
}

package cmd

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// c-5: the gate's documentation must describe the ENUMERATED surface, not a
// closed set of command names.
//
// The distinction is not pedantry. The old comment opened "the CLOSED set",
// doctor printed nothing about scope at all, and three prompts told the agent
// that "the loop commands" refuse — all of which were true right up until
// `survivor drain` joined, and none of which had any way to say so. A count in
// prose is a fact with no owner: it is correct on the day it is written and
// silently wrong afterwards, and the reader cannot tell which day they are on.
//
// So the wording is pinned by a test that reads the source files, the way
// redproof_doc_test.go reads the shipped red-proof doc. What it pins is a
// SHAPE — no closed-set framing, no command count, a pointer to the test that
// derives the surface — rather than an exact sentence, which would make every
// clarifying edit a red build.

// execGatedCommentBlock is the doc comment attached to execGatedCommands.
func execGatedCommentBlock(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(repoRootForDocs(t), "internal", "cmd", "trust.go"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(body), "\n")
	decl := -1
	for i, line := range lines {
		if strings.HasPrefix(line, "var execGatedCommands = ") {
			decl = i
			break
		}
	}
	if decl < 0 {
		t.Fatal("trust.go no longer declares execGatedCommands — this test has nothing to pin")
	}
	start := decl
	for start > 0 && strings.HasPrefix(lines[start-1], "//") {
		start--
	}
	if start == decl {
		t.Fatal("execGatedCommands carries no doc comment at all")
	}
	return strings.Join(lines[start:decl], "\n")
}

// execCommandCount matches a spelled-out or numeric count of commands — the
// shape that rots. "Two boundaries" is not one; "the other four commands" is.
var execCommandCount = regexp.MustCompile(`(?i)\b(one|two|three|four|five|six|seven|eight|nine|ten|\d+)\s+(\w+\s+){0,2}commands\b`)

// TestGateCommentDescribesTheEnumeratedSurface is contract one: the comment
// that opens the gate must not frame it as a closed list, and must send the
// reader to the thing that actually knows.
func TestGateCommentDescribesTheEnumeratedSurface(t *testing.T) {
	comment := execGatedCommentBlock(t)

	for _, banned := range []string{"CLOSED set", "closed set", "the other four"} {
		if strings.Contains(comment, banned) {
			t.Errorf("execGatedCommands' comment still frames the gate as %q:\n%s", banned, comment)
		}
	}
	if m := execCommandCount.FindString(comment); m != "" {
		t.Errorf("the comment states a command count (%q), which is a fact with no owner:\n%s", m, comment)
	}
	if !strings.Contains(comment, "TestEverySpawnSiteGatedOrExempt") {
		t.Error("the comment does not point at the test that derives the surface")
	}
	if !strings.Contains(comment, execExemptMarker) {
		t.Errorf("the comment does not name the exemption marker %s", execExemptMarker)
	}
}

// TestGateCommentPointerResolves: a pointer to a function that no longer exists
// is worse than none — it reads as authority and leads nowhere. The comment
// names TestEverySpawnSiteGatedOrExempt, so this asserts the name is real.
func TestGateCommentPointerResolves(t *testing.T) {
	root := repoRootForDocs(t)
	found := false
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return err
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		if strings.Contains(string(b), "func TestEverySpawnSiteGatedOrExempt(") {
			found = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Error("trust.go's comment names TestEverySpawnSiteGatedOrExempt, which no longer exists")
	}
}

// TestNoCommandCountSurvivesAnywhere is contract two, repo-wide. A count
// deleted from one file and left standing in another is the same rot with a
// longer fuse, so the grep is over every .go file and every shipped prompt —
// as a word and as a numeral.
func TestNoCommandCountSurvivesAnywhere(t *testing.T) {
	root := repoRootForDocs(t)
	// The leading class keeps `c-6 names both commands` — a criterion id in a
	// comment — out of it. A hyphen before the numeral is never a count.
	stale := regexp.MustCompile(`(?i)(^|[^\w-])(six|6)\s+(\w+\s+){0,2}commands\b`)

	check := func(path string) {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if m := stale.FindString(line); m != "" {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s:%d states %q — the gated set is enumerated, not counted:\n  %s",
					rel, i+1, m, strings.TrimSpace(line))
			}
		}
	}
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			check(path)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	prompts, err := filepath.Glob(filepath.Join(root, "assets", "prompts", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) == 0 {
		t.Fatal("found no prompts to check — the glob is wrong and this test proves nothing")
	}
	for _, p := range prompts {
		check(p)
	}
}

// TestPromptsDescribeTheGateAsEnumerated is contract three. The three prompts
// that tell an agent about consent all described a fixed surface: two said "the
// loop commands below refuse", verify.md called `dross test` "the one
// consent-gated execution site". Both stopped being true the moment the drain
// joined, and an agent reading either would conclude a drain needs no grant.
func TestPromptsDescribeTheGateAsEnumerated(t *testing.T) {
	root := repoRootForDocs(t)
	for _, name := range []string{"verify.md", "execute.md", "quick.md"} {
		t.Run(name, func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join(root, "assets", "prompts", name))
			if err != nil {
				t.Fatal(err)
			}
			body := string(b)
			for _, banned := range []string{
				"the loop commands below refuse",
				"the one consent-gated execution site",
				"closed set",
			} {
				if strings.Contains(body, banned) {
					t.Errorf("%s still describes the gate as a fixed surface: %q", name, banned)
				}
			}
			if !strings.Contains(body, "spawns a process") {
				t.Errorf("%s never says the gate covers every command that spawns a process", name)
			}
		})
	}
}

// TestDoctorNamesTheGatedSurface is contracts four and five, read off the
// command a user actually runs. Doctor is where someone learns what state they
// are in, so it is where the scope of the grant and the escape hatch belong —
// an exemption marker documented only in a test file is one that gets
// rediscovered by working around it.
func TestDoctorNamesTheGatedSurface(t *testing.T) {
	gatedFixture(t)
	var out string
	_ = runCmdCapturing(t, &out, Doctor())

	section := execConsentSection(t, out)
	if !strings.Contains(section, "survivor drain") {
		t.Errorf("doctor's Exec consent section does not name `survivor drain` among what the grant authorizes:\n%s", section)
	}
	if !strings.Contains(section, execExemptMarker) {
		t.Errorf("doctor's Exec consent section never names %s, so the escape hatch is undiscoverable:\n%s",
			execExemptMarker, section)
	}
	if m := execCommandCount.FindString(section); m != "" {
		t.Errorf("doctor prints a command count (%q), which is the rot this phase removed:\n%s", m, section)
	}
}

// execConsentSection slices doctor's output from `Exec consent:` to the blank
// line that closes it.
func execConsentSection(t *testing.T, out string) string {
	t.Helper()
	lines := strings.Split(out, "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "Exec consent:" {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("doctor printed no `Exec consent:` section:\n%s", out)
	}
	for i := start + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			return strings.Join(lines[start:i], "\n")
		}
	}
	return strings.Join(lines[start:], "\n")
}

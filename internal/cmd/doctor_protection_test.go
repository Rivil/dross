package cmd

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/protect"
	"github.com/Rivil/dross/internal/ship"
)

// protectionLines runs doctor and returns its "Branch protection:" section,
// one entry per line (heading excluded), plus doctor's error. present is
// false when the section is absent.
func protectionLines(t *testing.T) (lines []string, present bool, err error) {
	t.Helper()
	var out string
	err = runCmdCapturing(t, &out, Doctor())
	in := false
	for _, l := range strings.Split(out, "\n") {
		switch {
		case l == "Branch protection:":
			in, present = true, true
		case in && l == "":
			return lines, present, err
		case in && strings.HasPrefix(l, "    Advisory only"):
		case in:
			lines = append(lines, l)
		}
	}
	return lines, present, err
}

// doctorProtection is protectRepo with BranchRules answering res.
func doctorProtection(t *testing.T, res ship.BranchRulesResult) []string {
	t.Helper()
	protectRepo(t, "github")
	stubBranchRules(t, res)
	lines, present, _ := protectionLines(t)
	if !present {
		t.Fatal("doctor printed no Branch protection section for a GitHub remote")
	}
	return lines
}

func assertNoCheckmark(t *testing.T, lines []string) {
	t.Helper()
	for _, l := range lines {
		if strings.Contains(l, "✓") || strings.Contains(l, " is protected") {
			t.Errorf("an unread or gapped branch renders as protected: %q", l)
		}
	}
}

// gapLine returns the one section line containing phrase, failing unless it
// is a ⚠ line ending in the fix hint.
func gapLine(t *testing.T, lines []string, phrase string) {
	t.Helper()
	var hits []string
	for _, l := range lines {
		if strings.Contains(l, phrase) {
			hits = append(hits, l)
		}
	}
	if len(hits) != 1 {
		t.Errorf("want one line naming %q, got %d in:\n%s", phrase, len(hits), strings.Join(lines, "\n"))
		return
	}
	if !strings.HasPrefix(hits[0], "  ⚠ ") || !strings.HasSuffix(hits[0], ProtectFixHint) {
		t.Errorf("line %q is not a ⚠ ending in the fix hint", hits[0])
	}
}

func rulesFrom(t *testing.T, contexts []string, ruleset string) ship.BranchRulesResult {
	t.Helper()
	rs, err := protect.MainRuleset("main", contexts)
	if err != nil {
		t.Fatal(err)
	}
	res := liveFrom(t, rs, 1)
	if ruleset != "" {
		var rsd protect.LiveRuleset
		if err := json.Unmarshal([]byte(ruleset), &rsd); err != nil {
			t.Fatal(err)
		}
		res.Rulesets = []protect.LiveRuleset{rsd}
	}
	return res
}

// c-3: each gap doctor names prints its own ⚠ line ending in the fix.
func TestDoctorProtectionGapRender(t *testing.T) {
	t.Run("zero rules", func(t *testing.T) {
		lines := doctorProtection(t, ship.BranchRulesResult{Known: true})
		gapLine(t, lines, "unprotected")
		if len(lines) != 1 {
			t.Errorf("zero rules should be one unprotected line:\n%s", strings.Join(lines, "\n"))
		}
		assertNoCheckmark(t, lines)
	})
	t.Run("missing check", func(t *testing.T) {
		gapLine(t, doctorProtection(t, rulesFrom(t, []string{"test"}, "")), "required check missing: Lint")
	})
	t.Run("admin bypass", func(t *testing.T) {
		lines := doctorProtection(t, rulesFrom(t, []string{"Lint", "test"},
			`{"id": 1, "name": "dross: main", "enforcement": "active", "bypass_actors": [], "current_user_can_bypass": "always"}`))
		gapLine(t, lines, "admin bypass allowed")
	})
	t.Run("all three at once", func(t *testing.T) {
		res := liveTypes("non_fast_forward")
		res.Rulesets = []protect.LiveRuleset{{ID: 1, Name: "legacy", Enforcement: "active", CurrentUserCanBypass: "always"}}
		lines := doctorProtection(t, res)
		for _, phrase := range []string{"unprotected", "required check missing: Lint", "admin bypass allowed"} {
			gapLine(t, lines, phrase)
		}
		assertNoCheckmark(t, lines)
	})
}

func TestDoctorProtectionRemainingGaps(t *testing.T) {
	t.Run("force push and deletion allowed", func(t *testing.T) {
		res := liveTypes("pull_request", "required_status_checks")
		res.Rules[1] = rulesFrom(t, []string{"Lint", "test"}, "").Rules[3]
		res.Rulesets = []protect.LiveRuleset{{ID: 1, Name: "x", Enforcement: "active", BypassActors: &[]protect.BypassActor{}, CurrentUserCanBypass: "never"}}
		lines := doctorProtection(t, res)
		gapLine(t, lines, "force push allowed")
		gapLine(t, lines, "deletion allowed")
	})
	t.Run("stale check", func(t *testing.T) {
		gapLine(t, doctorProtection(t, rulesFrom(t, []string{"Lint", "test", "gone"}, "")), "required check gone has no pull_request job")
	})
}

// Unknown is never protected: an unread answer, and an unread bypass list.
func TestProtectionUnknownNeverReadsProtected(t *testing.T) {
	lines := doctorProtection(t, ship.BranchRulesResult{Reason: "GitHub refused gh's credentials (HTTP 401) — run `gh auth login`"})
	if len(lines) != 1 || !strings.Contains(lines[0], "unknown (GitHub refused gh's credentials (HTTP 401)") {
		t.Errorf("unread rules render as:\n%s", strings.Join(lines, "\n"))
	}
	assertNoCheckmark(t, lines)

	lines = doctorProtection(t, rulesFrom(t, []string{"Lint", "test"},
		`{"id": 1, "name": "dross: main", "enforcement": "active", "current_user_can_bypass": "never"}`))
	gapLine(t, lines, "bypass list unknown")
	for _, l := range lines {
		if strings.Contains(l, "none") {
			t.Errorf("an unread bypass list renders as %q", l)
		}
	}
	assertNoCheckmark(t, lines)
}

// A workflow protect can't read makes the required set unknown — never an
// empty want-list that reads as nothing to require.
func TestScannerRefusalReadsUnknown(t *testing.T) {
	dir, _ := protectRepo(t, "github")
	mustWrite(t, filepath.Join(dir, ".github", "workflows", "ci.yml"), protectCI+"  build:\n    strategy:\n      matrix:\n        os: [a, b]\n")
	mustGit(t, dir, "commit", "-q", "-am", "matrix")
	mustGit(t, dir, "push", "-q", "origin", "main")
	calls := stubBranchRules(t, rulesFrom(t, []string{"Lint", "test"}, ""))

	lines, _, _ := protectionLines(t)
	if len(lines) != 1 || !strings.Contains(lines[0], "unknown (.github/workflows/ci.yml/build: it runs a matrix") {
		t.Errorf("a matrix job renders as:\n%s", strings.Join(lines, "\n"))
	}
	assertNoCheckmark(t, lines)
	if *calls != 0 {
		t.Errorf("BranchRules asked %d times for an unreadable want-list", *calls)
	}
}

func TestNoOriginMainReadsUnknown(t *testing.T) {
	dir, _ := setupMilestoneRepo(t)
	setProtectRemote(t, dir, "github", "")
	stubBranchRules(t, ship.BranchRulesResult{Known: true})
	lines, present, _ := protectionLines(t)
	if !present || len(lines) != 1 || lines[0] != "  ⚠ unknown (no origin/main to read workflows from — run git fetch)" {
		t.Errorf("no origin/main renders as:\n%s", strings.Join(lines, "\n"))
	}
}

// doctor_reach: GitHub only. Other forges say so and ask nothing; no remote
// prints no section at all.
func TestDoctorProtectionProviderTable(t *testing.T) {
	for _, provider := range []string{"gitlab", "forgejo", "gitea", "bitbucket", ""} {
		t.Run("provider="+provider, func(t *testing.T) {
			protectRepo(t, provider)
			calls := stubBranchRules(t, ship.BranchRulesResult{Known: true})
			lines, present, _ := protectionLines(t)
			if *calls != 0 {
				t.Errorf("%d BranchRules calls", *calls)
			}
			if provider == "" {
				if present {
					t.Errorf("no [remote] printed a protection section:\n%s", strings.Join(lines, "\n"))
				}
				return
			}
			if len(lines) != 1 || !strings.Contains(lines[0], "not checked ("+provider+")") {
				t.Errorf("section = %q", lines)
			}
		})
	}
}

// cleanProtectRepo is a doctor-clean repo on a GitHub [remote] whose git
// origin is that same URL, with pushes redirected to a local bare repo —
// so doctor's remote check agrees, and origin/main carries the workflows.
func cleanProtectRepo(t *testing.T) {
	t.Helper()
	const url = "https://github.com/Rivil/dross.git"
	dir, origin := t.TempDir(), t.TempDir()
	mustGit(t, origin, "init", "--bare", "-q", "-b", "main")
	gitInit(t, dir, url)
	mustGit(t, dir, "config", "url."+origin+".pushInsteadOf", url)
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	t.Setenv(doctorTokenEnv, "x")
	mustRunSet(t, "remote.auth_env", doctorTokenEnv)
	mustRunSet(t, "project.name", "x")
	mustRunSet(t, "runtime.mode", "native")
	mustWrite(t, filepath.Join(dir, ".github", "workflows", "ci.yml"), protectCI)
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "-q", "-m", "init")
	mustGit(t, dir, "push", "-q", "-u", "origin", "main")
}

// Gaps are warnings: /dross-ship gates on doctor's exit code.
func TestDoctorProtectionNeverMovesExitCode(t *testing.T) {
	cleanProtectRepo(t)
	stubBranchRules(t, rulesFrom(t, []string{"Lint", "test"}, `{"id": 1, "name": "dross: main", "enforcement": "active", "bypass_actors": [], "current_user_can_bypass": "never"}`))
	clean, _, cleanErr := protectionLines(t)
	if cleanErr != nil || len(clean) != 1 || !strings.Contains(clean[0], "✓ main is protected") {
		t.Fatalf("baseline must be doctor-clean and protected (err %v):\n%s", cleanErr, strings.Join(clean, "\n"))
	}
	stubBranchRules(t, ship.BranchRulesResult{Known: true})
	gapped, _, gapErr := protectionLines(t)
	if gapErr != nil {
		t.Errorf("a protection gap moved doctor's exit: %v\n%s", gapErr, strings.Join(gapped, "\n"))
	}
	stubBranchRules(t, ship.BranchRulesResult{Reason: "no answer from GitHub within 20s"})
	if _, _, err := protectionLines(t); err != nil {
		t.Errorf("an unknown protection read moved doctor's exit: %v", err)
	}
}

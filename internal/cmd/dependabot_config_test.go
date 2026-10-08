package cmd

import (
	"strconv"
	"strings"
	"testing"
)

// .github/dependabot.yml is the repo's only automated currency mechanism: it is
// what raises a PR when a Go module, a SHA-pinned action, or the Stryker npm
// fixture falls behind. Nothing else in the tree references it, so a dropped
// ecosystem, a schedule slid off weekly, or a lost cooldown would go unnoticed
// until the next hand audit. These tests pin the config's shape (phase
// dependency-update-automation, criteria c-1 and c-2).
//
// Line-based on purpose, matching toolchain_source_test.go and
// workflow_run_expressions_test.go: the repo carries no YAML dependency, and a
// single static binary is a locked stack decision.
//
// cooldownDays is asserted against `cooldown.default-days` rather than the
// `semver-*-days` keys because GitHub supports `default-days` for all three of
// gomod, github-actions and npm, while the semver-bump variants are not offered
// for github-actions — `default-days` is the one key that gives every ecosystem
// here the same 7-day release-age window.
const dependabotCooldownDays = 7

// dependabotGroup is one entry under an ecosystem's `groups:` mapping.
type dependabotGroup struct {
	name        string
	updateTypes []string
}

// dependabotEcosystem is one `- package-ecosystem:` block and the keys found
// inside it.
type dependabotEcosystem struct {
	line         int
	name         string
	directory    string
	interval     string
	cooldownDays int // -1 when the block carries no `cooldown.default-days`
	targetBranch string
	groups       []dependabotGroup
}

// dependabotEcosystems walks .github/dependabot.yml by line, collecting each
// `- package-ecosystem:` block. A block ends at the next line indented at or
// above its own dash. Nested keys are scoped by the indent of their parent key
// (`cooldown:`, `groups:`, `update-types:`) so a `default-days:` outside a
// cooldown, or an `update-types:` outside a group, is not misread as one.
// Comment lines and trailing `# ...` comments are dropped first, so the header
// comment naming `target-branch` is not read as the key.
func dependabotEcosystems(cfg string) []dependabotEcosystem {
	var (
		out          []dependabotEcosystem
		cur          *dependabotEcosystem
		dashIndent   int
		cooldownAt   = -1
		groupsAt     = -1
		groupNameAt  = -1
		updateTypeAt = -1
	)
	flush := func() {
		if cur != nil {
			out = append(out, *cur)
			cur = nil
		}
		cooldownAt, groupsAt, groupNameAt, updateTypeAt = -1, -1, -1, -1
	}
	for i, raw := range strings.Split(cfg, "\n") {
		line := stripYAMLComment(raw)
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))

		if v, ok := strings.CutPrefix(trimmed, "- package-ecosystem:"); ok {
			flush()
			cur = &dependabotEcosystem{line: i + 1, name: dependabotScalar(v), cooldownDays: -1}
			dashIndent = indent
			continue
		}
		if cur == nil {
			continue
		}
		if indent <= dashIndent {
			flush()
			continue
		}
		// Close any nested context the current line has dedented out of,
		// before reading the line as a key.
		if cooldownAt >= 0 && indent <= cooldownAt {
			cooldownAt = -1
		}
		if updateTypeAt >= 0 && indent <= updateTypeAt {
			updateTypeAt = -1
		}
		if groupsAt >= 0 && indent <= groupsAt {
			groupsAt, groupNameAt = -1, -1
		}

		if item, ok := strings.CutPrefix(trimmed, "- "); ok {
			if updateTypeAt >= 0 && indent > updateTypeAt && len(cur.groups) > 0 {
				g := &cur.groups[len(cur.groups)-1]
				g.updateTypes = append(g.updateTypes, dependabotScalar(item))
			}
			continue
		}

		switch {
		case strings.HasPrefix(trimmed, "directory:"):
			cur.directory = dependabotScalar(strings.TrimPrefix(trimmed, "directory:"))
		case strings.HasPrefix(trimmed, "interval:"):
			cur.interval = dependabotScalar(strings.TrimPrefix(trimmed, "interval:"))
		case strings.HasPrefix(trimmed, "target-branch:"):
			cur.targetBranch = dependabotScalar(strings.TrimPrefix(trimmed, "target-branch:"))
		case trimmed == "cooldown:":
			cooldownAt = indent
		case trimmed == "groups:":
			groupsAt = indent
		case trimmed == "update-types:":
			updateTypeAt = indent
		case strings.HasPrefix(trimmed, "default-days:"):
			if cooldownAt >= 0 && indent > cooldownAt {
				if n, err := strconv.Atoi(dependabotScalar(strings.TrimPrefix(trimmed, "default-days:"))); err == nil {
					cur.cooldownDays = n
				}
			}
		case groupsAt >= 0 && indent > groupsAt && strings.HasSuffix(trimmed, ":") &&
			(groupNameAt == -1 || indent == groupNameAt):
			groupNameAt = indent
			cur.groups = append(cur.groups, dependabotGroup{name: strings.TrimSuffix(trimmed, ":")})
		}
	}
	flush()
	return out
}

func dependabotScalar(v string) string {
	return strings.Trim(strings.TrimSpace(v), `'"`)
}

// coversMinorAndPatch reports whether the ecosystem has at least one group
// batching both minor and patch updates into a single PR.
func (e dependabotEcosystem) coversMinorAndPatch() bool {
	for _, g := range e.groups {
		var minor, patch bool
		for _, ut := range g.updateTypes {
			switch ut {
			case "minor":
				minor = true
			case "patch":
				patch = true
			}
		}
		if minor && patch {
			return true
		}
	}
	return false
}

// TestDependabotConfigEcosystems pins criterion c-1: the three ecosystems dross
// actually depends on are each configured, weekly, with their minor and patch
// bumps grouped into one PR.
func TestDependabotConfigEcosystems(t *testing.T) {
	blocks := dependabotEcosystems(readRepoFile(t, ".github/dependabot.yml"))
	if len(blocks) == 0 {
		t.Fatal(".github/dependabot.yml: no `- package-ecosystem:` blocks found — the sweep below cannot go green by parsing nothing")
	}

	want := []struct{ ecosystem, directory string }{
		{"gomod", "/"},
		{"github-actions", "/"},
		{"npm", "/internal/mutation/testdata/ts-project"},
	}
	byName := map[string]dependabotEcosystem{}
	for _, b := range blocks {
		byName[b.name] = b
	}
	for _, w := range want {
		b, ok := byName[w.ecosystem]
		if !ok {
			t.Errorf(".github/dependabot.yml has no `%s` ecosystem — nothing keeps those dependencies current", w.ecosystem)
			continue
		}
		if b.directory != w.directory {
			t.Errorf("%s:%d ecosystem %s has directory %q; want %q", ".github/dependabot.yml", b.line, b.name, b.directory, w.directory)
		}
		if b.interval != "weekly" {
			t.Errorf("%s:%d ecosystem %s has schedule interval %q; want weekly", ".github/dependabot.yml", b.line, b.name, b.interval)
		}
		if !b.coversMinorAndPatch() {
			t.Errorf("%s:%d ecosystem %s has no group batching both minor and patch updates (groups: %+v) — every bump would open its own PR", ".github/dependabot.yml", b.line, b.name, b.groups)
		}
	}
}

// TestDependabotConfigCooldown pins criterion c-2: every ecosystem holds a
// candidate release for 7 days, the window in which a compromised release gets
// yanked. Applies to every block, not just the three above — a fourth ecosystem
// added without a cooldown is the same exposure.
func TestDependabotConfigCooldown(t *testing.T) {
	blocks := dependabotEcosystems(readRepoFile(t, ".github/dependabot.yml"))
	if len(blocks) == 0 {
		t.Fatal(".github/dependabot.yml: no `- package-ecosystem:` blocks found — nothing to check for a cooldown")
	}
	for _, b := range blocks {
		if b.cooldownDays != dependabotCooldownDays {
			t.Errorf("%s:%d ecosystem %s has `cooldown.default-days: %d`; want %d — without it a release published minutes ago is a bump candidate", ".github/dependabot.yml", b.line, b.name, b.cooldownDays, dependabotCooldownDays)
		}
	}
}

// TestDependabotConfigNoTargetBranch pins the locked target_branch decision:
// PRs target the repo default (main). A `target-branch` pointing at a merged
// milestone branch would silently no-op every update while the ecosystem,
// schedule, grouping and cooldown assertions all stayed green.
func TestDependabotConfigNoTargetBranch(t *testing.T) {
	for _, b := range dependabotEcosystems(readRepoFile(t, ".github/dependabot.yml")) {
		if b.targetBranch != "" {
			t.Errorf("%s:%d ecosystem %s sets `target-branch: %s` — locked decision target_branch says PRs target the default branch", ".github/dependabot.yml", b.line, b.name, b.targetBranch)
		}
	}
}

// TestDependabotEcosystemScanner pins the line scanner against an inline
// fixture so the sweeps above cannot pass over an empty or half-parsed set.
func TestDependabotEcosystemScanner(t *testing.T) {
	const cfg = `# target-branch: not a key, a comment
version: 2
updates:
  - package-ecosystem: "gomod"
    directory: "/"
    schedule:
      interval: "weekly"
    cooldown:
      default-days: 7
    groups:
      gomod-minor-patch:
        patterns:
          - "*"
        update-types:
          - "minor"
          - "patch"

  - package-ecosystem: npm
    directory: /sub
    target-branch: "milestone/v1.7"
    schedule:
      interval: monthly
    groups:
      a:
        update-types:
          - "patch"
      b:
        update-types:
          - "major"
  # a trailing comment block
`
	got := dependabotEcosystems(cfg)
	want := []dependabotEcosystem{
		{
			line: 4, name: "gomod", directory: "/", interval: "weekly", cooldownDays: 7,
			groups: []dependabotGroup{{name: "gomod-minor-patch", updateTypes: []string{"minor", "patch"}}},
		},
		{
			line: 18, name: "npm", directory: "/sub", interval: "monthly", cooldownDays: -1,
			targetBranch: "milestone/v1.7",
			groups: []dependabotGroup{
				{name: "a", updateTypes: []string{"patch"}},
				{name: "b", updateTypes: []string{"major"}},
			},
		},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d ecosystems %+v, want %d", len(got), got, len(want))
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.line != w.line || g.name != w.name || g.directory != w.directory ||
			g.interval != w.interval || g.cooldownDays != w.cooldownDays || g.targetBranch != w.targetBranch {
			t.Errorf("ecosystem %d scalars: got %+v, want %+v", i, g, w)
		}
		if len(g.groups) != len(w.groups) {
			t.Fatalf("ecosystem %d: got %d groups %+v, want %d", i, len(g.groups), g.groups, len(w.groups))
		}
		for j := range w.groups {
			if g.groups[j].name != w.groups[j].name ||
				strings.Join(g.groups[j].updateTypes, ",") != strings.Join(w.groups[j].updateTypes, ",") {
				t.Errorf("ecosystem %d group %d: got %+v, want %+v", i, j, g.groups[j], w.groups[j])
			}
		}
	}
	if want[0].coversMinorAndPatch() != true || want[1].coversMinorAndPatch() != false {
		t.Error("coversMinorAndPatch: a group must carry both minor and patch, not one each across two groups")
	}
}

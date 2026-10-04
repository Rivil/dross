package gate

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func put(t *testing.T, root, rel, body string) string {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func planWith(statuses ...string) string {
	var b strings.Builder
	b.WriteString("[phase]\n  id = \"p\"\n")
	for i, s := range statuses {
		b.WriteString("\n[[task]]\n  id = \"t-" + string(rune('1'+i)) + "\"\n  wave = 1\n  title = \"x\"\n  files = [\"a.go\"]\n")
		if s != "" {
			b.WriteString("  status = \"" + s + "\"\n")
		}
	}
	return b.String()
}

func write(t *testing.T, p, content string) []byte {
	return payload(t, "Write", map[string]any{"file_path": p, "content": content}, filepath.Dir(p))
}

func edit(t *testing.T, p string) []byte {
	return payload(t, "Edit", map[string]any{"file_path": p, "old_string": "a", "new_string": "b"}, filepath.Dir(p))
}

func TestPlanEdit(t *testing.T) {
	root := droot(t)
	e := gatesEnv(t, "plan-edit")
	plan := put(t, root, ".dross/phases/p/plan.toml", planWith("pending"))

	for _, in := range [][]byte{
		edit(t, plan),
		payload(t, "MultiEdit", map[string]any{"file_path": plan, "edits": []any{map[string]any{"old_string": "a", "new_string": "b"}}}, root),
	} {
		res := Check(in, e)
		if res.Allowed() || !strings.Contains(res.Text(), "dross task edit") || !strings.Contains(res.Text(), "plan-edit") {
			t.Errorf("an Edit of an existing plan.toml: %q, want a plan-edit refusal naming `dross task edit`", res.Text())
		}
	}
	fresh := filepath.Join(root, ".dross", "phases", "q", "plan.toml")
	if res := Check(write(t, fresh, planWith("")), e); !res.Allowed() {
		t.Errorf("a Write creating plan.toml was refused: %q", res.Text())
	}
}

func TestPlanOverwrite(t *testing.T) {
	e := gatesEnv(t, "plan-edit")
	for _, c := range []struct {
		statuses []string
		allowed  bool
	}{
		{[]string{"pending", "pending"}, true},
		{[]string{"", "pending"}, true},
		{[]string{"pending", "in_progress"}, false},
		{[]string{"done", "pending"}, false},
		{[]string{"pending", "failed"}, false},
	} {
		root := droot(t)
		plan := put(t, root, ".dross/phases/p/plan.toml", planWith(c.statuses...))
		res := Check(write(t, plan, planWith("pending")), e)
		if res.Allowed() != c.allowed {
			t.Errorf("Write over a plan with %q: allowed=%v (%q), want %v", c.statuses, res.Allowed(), res.Text(), c.allowed)
		}
	}
}

func TestDrossFilesClosedPosture(t *testing.T) {
	root := droot(t)
	plan := put(t, root, ".dross/phases/p/plan.toml", "[[task]\nbroken")
	res := Check(write(t, plan, planWith("pending")), gatesEnv(t, "plan-edit"))
	if res.Allowed() || !strings.Contains(res.Text(), "decode plan") {
		t.Errorf("a Write over an unparseable plan.toml: %q, want a refusal naming the decode error", res.Text())
	}

	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		return
	}
	spec := put(t, root, ".dross/phases/p/spec.toml", strings.Repeat("x", 1000))
	if err := os.Chmod(spec, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(spec, 0o644) })
	res = Check(write(t, spec, "tiny"), gatesEnv(t, "curated-shrink"))
	if res.Allowed() || !strings.Contains(res.Text(), "permission denied") {
		t.Errorf("a Write over an unreadable curated file: %q, want a refusal naming the read error", res.Text())
	}
}

func TestCuratedShrinkBoundary(t *testing.T) {
	e := gatesEnv(t, "curated-shrink")
	for _, rel := range []string{"project.toml", "milestones/v1.toml", "phases/p/spec.toml", "handoff.md", "rules.toml"} {
		for _, c := range []struct {
			size    int
			allowed bool
		}{{499, false}, {500, true}, {2000, true}} {
			root := droot(t)
			p := put(t, root, ".dross/"+rel, strings.Repeat("x", 1000))
			res := Check(write(t, p, strings.Repeat("y", c.size)), e)
			if res.Allowed() != c.allowed {
				t.Errorf("%s: a %d-byte Write over 1000 bytes allowed=%v (%q), want %v", rel, c.size, res.Allowed(), res.Text(), c.allowed)
			}
			if !c.allowed && !strings.Contains(res.Text(), "Edit") {
				t.Errorf("%s: the refusal %q does not point at Edit", rel, res.Text())
			}
			if res := Check(edit(t, p), e); !res.Allowed() {
				t.Errorf("%s: an Edit was judged: %q", rel, res.Text())
			}
		}
	}
}

func TestDrossFilesScope(t *testing.T) {
	root := droot(t)
	shrink, planEdit := gatesEnv(t, "curated-shrink"), gatesEnv(t, "plan-edit")

	if res := Check(write(t, filepath.Join(root, ".dross", "milestones", "v2.toml"), "x"), shrink); !res.Allowed() {
		t.Errorf("a new milestone file was refused: %q", res.Text())
	}
	for _, rel := range []string{"phases/p/changes.json", "phases/p/notes.md"} {
		p := put(t, root, ".dross/"+rel, strings.Repeat("x", 1000))
		if res := Check(write(t, p, "x"), shrink); !res.Allowed() {
			t.Errorf("shrinking the non-curated %s was refused: %q", rel, res.Text())
		}
	}
	docs := put(t, root, "docs/plan.toml", planWith("done"))
	if res := Check(edit(t, docs), planEdit); !res.Allowed() {
		t.Errorf("docs/plan.toml was gated: %q", res.Text())
	}

	extra := gatesEnv(t, "curated-shrink")
	writeLists(t, extra.Home, "curated_files = [\"phases/*/notes.md\"]\n")
	notes := put(t, root, ".dross/phases/p/notes.md", strings.Repeat("x", 1000))
	if res := Check(write(t, notes, "x"), extra); res.Allowed() {
		t.Error("a curated path added in gates.toml was not enforced")
	}
}

func TestPlanPathNormalisation(t *testing.T) {
	e := gatesEnv(t, "plan-edit")
	root := droot(t)
	put(t, root, ".dross/phases/x/plan.toml", planWith("done"))
	twisty := filepath.Join(root, ".dross", "phases", "x") + "/../x/plan.toml"
	if res := Check(edit(t, twisty), e); res.Allowed() {
		t.Errorf("%s escaped plan-edit", twisty)
	}

	bare := t.TempDir() // the same layout, no project.toml anywhere above it
	p := put(t, bare, ".dross/phases/x/plan.toml", planWith("done"))
	if res := Check(edit(t, p), e); !res.Allowed() {
		t.Errorf("plan-edit fired outside a dross repo: %q", res.Text())
	}
}

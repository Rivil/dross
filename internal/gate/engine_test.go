package gate

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/gitrun"
)

// payload builds a PreToolUse payload.
func payload(t *testing.T, tool string, input map[string]any, cwd string) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse", "session_id": "s", "tool_name": tool, "tool_input": input, "cwd": cwd,
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func bash(t *testing.T, cmd, cwd string) []byte {
	return payload(t, "Bash", map[string]any{"command": cmd}, cwd)
}

// droot is a dross repo root: a directory whose .dross/ holds project.toml.
func droot(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".dross"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".dross", "project.toml"), []byte("[project]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// fake is a gate that counts its calls and refuses (or errors, or panics) on
// demand.
type fake struct {
	claims, judged int
	refuse         bool
	err            error
	panicJudge     bool
	panicClaims    bool
	claimTool      string // "" claims every tool
}

func (f *fake) gate(name string, scope Scope, liftable bool) Gate {
	return Gate{
		Name: name, Scope: scope, Liftable: liftable,
		Claims: func(c *Call) bool {
			f.claims++
			if f.panicClaims {
				panic("claims blew up")
			}
			return f.claimTool == "" || c.ToolName == f.claimTool
		},
		Judge: func(c *Call) (*Refusal, error) {
			f.judged++
			switch {
			case f.panicJudge:
				panic("judge blew up")
			case f.err != nil:
				return nil, f.err
			case f.refuse:
				return NewRefusal("rule of "+name, "remedy of "+name)
			}
			return nil, nil
		},
	}
}

func env(t *testing.T, gates ...Gate) Env {
	return Env{Home: t.TempDir(), Gates: gates, Recorders: []Recorder{}}
}

func TestUnparseablePayloadAllows(t *testing.T) {
	for _, in := range []string{"{", "", "null", "[]", `"x"`, `{"tool_input":{"command":"pass-cli x"}}`, `{"tool_name":""}`} {
		f := &fake{refuse: true}
		res := Check([]byte(in), env(t, f.gate("g", AlwaysOn, true)))
		if !res.Allowed() || len(res.Warnings) != 1 || f.claims+f.judged != 0 {
			t.Errorf("payload %q: allowed=%v warnings=%q claims=%d judged=%d; want allowed, one warning, no gate run",
				in, res.Allowed(), res.Warnings, f.claims, f.judged)
		}
	}
}

func TestClosedPosture(t *testing.T) {
	cwd := t.TempDir()
	in := bash(t, "ls", cwd)

	f := &fake{err: errors.New("state.json: permission denied")}
	res := Check(in, env(t, f.gate("erring", AlwaysOn, true)))
	if res.Allowed() || !strings.Contains(res.Text(), "erring") || !strings.Contains(res.Text(), "permission denied") {
		t.Errorf("a judge error: %q, want a refusal naming the gate and the error", res.Text())
	}

	f = &fake{panicJudge: true}
	res = Check(in, env(t, f.gate("panicky", AlwaysOn, false)))
	if res.Allowed() || !strings.Contains(res.Text(), "panicky") || !strings.Contains(res.Text(), "internal error") {
		t.Errorf("a judge panic: %q, want a refusal naming the gate and \"internal error\"", res.Text())
	}

	f = &fake{panicClaims: true}
	quiet := &fake{}
	res = Check(in, env(t, f.gate("claims-panic", AlwaysOn, true), quiet.gate("after", AlwaysOn, true)))
	if res.Allowed() || !strings.Contains(res.Text(), "internal error") || quiet.judged != 1 {
		t.Errorf("a claims panic: %q, later gate judged %d; want a refusal and the next gate still run", res.Text(), quiet.judged)
	}
}

func TestRefusalShape(t *testing.T) {
	for _, c := range [][2]string{{"", "x"}, {"x", ""}, {"  ", "x"}, {"x", "\t"}} {
		if r, err := NewRefusal(c[0], c[1]); err == nil || r != nil {
			t.Errorf("NewRefusal(%q, %q) = %v, %v; want an error", c[0], c[1], r, err)
		}
	}
	f := &fake{refuse: true}
	res := Check(bash(t, "ls", t.TempDir()), env(t, f.gate("shape", AlwaysOn, false)))
	text := res.Text()
	for _, want := range []string{"dross gate shape refused this call", "rule of shape", "Use: remedy of shape"} {
		if !strings.Contains(text, want) {
			t.Errorf("refusal %q lacks %q", text, want)
		}
	}
	// A judge that builds a malformed refusal is refused as an error, never
	// allowed.
	bad := Gate{Name: "bad", Claims: func(*Call) bool { return true }, Judge: func(*Call) (*Refusal, error) { return NewRefusal("", "") }}
	if res := Check(bash(t, "ls", t.TempDir()), env(t, bad)); res.Allowed() {
		t.Error("a judge returning a malformed refusal was allowed")
	}
}

func TestEscapeHint(t *testing.T) {
	in := bash(t, "ls", t.TempDir())
	lift := &fake{refuse: true}
	text := Check(in, env(t, lift.gate("liftable-one", AlwaysOn, true))).Text()
	if want := "a human can run `dross gate off liftable-one` in their own terminal"; !strings.Contains(text, want) {
		t.Errorf("liftable refusal %q lacks %q", text, want)
	}
	fixed := &fake{refuse: true}
	text = Check(in, env(t, fixed.gate("fixed-one", AlwaysOn, false))).Text()
	if strings.Contains(text, "gate off") {
		t.Errorf("non-liftable refusal %q points at `dross gate off`, which refuses it", text)
	}
	// Errors and panics carry the hint too: the off switch is the escape from
	// a buggy gate.
	buggy := &fake{panicJudge: true}
	if text := Check(in, env(t, buggy.gate("buggy", AlwaysOn, true))).Text(); !strings.Contains(text, "dross gate off buggy") {
		t.Errorf("a liftable gate's internal error %q lacks the escape hint", text)
	}
}

func TestScope(t *testing.T) {
	root := droot(t)
	outside := t.TempDir()
	half := t.TempDir() // .dross without project.toml, inside a real root
	if err := os.MkdirAll(filepath.Join(root, "nested", ".dross"), 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "nested")
	if err := os.MkdirAll(filepath.Join(half, ".dross"), 0o755); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name     string
		in       []byte
		workflow bool // whether the workflow gate should run
	}{
		{"bash in a root", bash(t, "ls", root), true},
		{"bash in a subdir of a root", bash(t, "ls", filepath.Join(root, "sub")), true},
		{"bash outside", bash(t, "ls", outside), false},
		{"half-built .dross", bash(t, "ls", half), false},
		{"half-built .dross never borrows its parent's root", bash(t, "ls", nested), false},
		{"bash cd into a root", bash(t, "cd "+root+" && ls", outside), true},
		{"bash git -C a root", bash(t, "git -C "+root+" status", outside), true},
		{"edit inside a root from outside", payload(t, "Edit", map[string]any{"file_path": filepath.Join(root, "a.go")}, outside), true},
		{"edit outside from a root", payload(t, "Edit", map[string]any{"file_path": filepath.Join(outside, "a.go")}, root), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wf, on := &fake{refuse: true}, &fake{refuse: true}
			res := Check(c.in, env(t, wf.gate("wf", Workflow, true), on.gate("on", AlwaysOn, true)))
			if ran := wf.judged == 1; ran != c.workflow {
				t.Errorf("workflow gate judged=%d, want run=%v", wf.judged, c.workflow)
			}
			if on.judged != 1 {
				t.Errorf("always-on gate judged=%d, want 1", on.judged)
			}
			if want := 1; c.workflow {
				if len(res.Refusals) != 2 {
					t.Errorf("refusals = %d, want 2", len(res.Refusals))
				}
			} else if len(res.Refusals) != want {
				t.Errorf("refusals = %d, want %d", len(res.Refusals), want)
			}
		})
	}
	for _, d := range []string{root, half} {
		if _, err := os.Stat(filepath.Join(d, ".dross", "state.json")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("locating %s left a state.json behind (err=%v)", d, err)
		}
	}
}

func TestUnreadableRootRefusesInDomain(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory the test cannot search")
	}
	locked := t.TempDir()
	inner := filepath.Join(locked, "inner")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	wf, on := &fake{}, &fake{}
	res := Check(bash(t, "ls", inner), env(t, wf.gate("wf", Workflow, true), on.gate("on", AlwaysOn, true)))
	if res.Allowed() || !strings.Contains(res.Text(), "dross gate wf") || wf.judged != 0 {
		t.Errorf("an unreadable root probe: %q (wf judged %d), want the workflow gate to refuse naming the error unjudged", res.Text(), wf.judged)
	}
	if on.judged != 1 || strings.Contains(res.Text(), "dross gate on") {
		t.Errorf("the always-on gate was disturbed by a root it does not need: judged %d, %q", on.judged, res.Text())
	}
}

func TestUnclaimedTool(t *testing.T) {
	var argv [][]string
	prev := gitrun.ArgvRecorder
	gitrun.ArgvRecorder = func(a []string) { argv = append(argv, a) }
	t.Cleanup(func() { gitrun.ArgvRecorder = prev })

	root := droot(t)
	glob := payload(t, "Glob", map[string]any{"pattern": "**/*.env", "path": root}, root)
	// The registry as it stands, and a fake that claims Bash only.
	if res := Check(glob, Env{Home: t.TempDir()}); !res.Allowed() || len(res.Warnings) != 0 {
		t.Errorf("registry on Glob: %q / %q, want silence", res.Text(), res.Warnings)
	}
	f := &fake{refuse: true, claimTool: "Bash"}
	res := Check(glob, env(t, f.gate("bash-only", Workflow, true)))
	if !res.Allowed() || len(res.Warnings) != 0 || f.judged != 0 {
		t.Errorf("fake on Glob: %q / %q judged %d, want silence and no judge", res.Text(), res.Warnings, f.judged)
	}
	if len(argv) != 0 {
		t.Errorf("an unclaimed call spawned git: %q", argv)
	}
}

func TestDispatchLiftsAndJoins(t *testing.T) {
	in := bash(t, "ls", droot(t))
	lifted := func(names ...string) func(Gate, string) bool {
		return func(g Gate, _ string) bool {
			for _, n := range names {
				if g.Name == n {
					return true
				}
			}
			return false
		}
	}

	f := &fake{refuse: true}
	e := env(t, f.gate("liftme", Workflow, true))
	e.Lifted = lifted("liftme")
	if res := Check(in, e); !res.Allowed() || f.judged != 0 {
		t.Errorf("a lifted gate ran: judged %d, %q", f.judged, res.Text())
	}

	f = &fake{refuse: true}
	e = env(t, f.gate("pinned", AlwaysOn, false))
	e.Lifted = lifted("pinned")
	if res := Check(in, e); res.Allowed() || f.judged != 1 {
		t.Errorf("a Liftable=false gate honoured an override: judged %d", f.judged)
	}

	a, b, quiet := &fake{refuse: true}, &fake{refuse: true}, &fake{}
	res := Check(in, env(t, a.gate("first", AlwaysOn, true), quiet.gate("quiet", AlwaysOn, true), b.gate("second", Workflow, true)))
	if len(res.Refusals) != 2 || !strings.Contains(res.Text(), "rule of first") || !strings.Contains(res.Text(), "rule of second") {
		t.Errorf("two refusing gates: %q, want both rules", res.Text())
	}
}

func writeLists(t *testing.T, home, body string) {
	t.Helper()
	p := ListsPath(home)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func TestListsAreAdditive(t *testing.T) {
	home := t.TempDir()
	writeLists(t, home, "secret_tools = []\nsecret_paths = []\ncurated_files = []\n")
	l := LoadLists(home)
	if l.Err != nil || !contains(l.SecretTools, "pass-cli") || !contains(l.SecretPaths, "*.env") || !contains(l.CuratedFiles, "handoff.md") {
		t.Errorf("empty extension lists removed a default: %+v", l)
	}
	writeLists(t, home, "secret_tools = [\"op\", \"pass-cli\"]\ncurated_files = [\"notes/*.md\"]\n")
	l = LoadLists(home)
	if l.Err != nil || !contains(l.SecretTools, "op") || !contains(l.CuratedFiles, "notes/*.md") || len(l.SecretTools) != 2 {
		t.Errorf("extension = %+v, want op and notes/*.md added once", l)
	}
	if l := LoadLists(t.TempDir()); l.Err != nil || len(l.SecretTools) != 1 {
		t.Errorf("no gates.toml = %+v, want the defaults", l)
	}
}

func TestMalformedListsRefuseExtensibleGates(t *testing.T) {
	for _, body := range []string{"secret_tools = [\n", "secret_tool = [\"op\"]\n", "secret_tools = \"op\"\n"} {
		home := t.TempDir()
		writeLists(t, home, body)
		if l := LoadLists(home); l.Err == nil || !strings.Contains(l.Err.Error(), ListsPath(home)) {
			t.Errorf("gates.toml %q: Err = %v, want one naming the file", body, l.Err)
		}
		ext, plain := &fake{}, &fake{}
		g := ext.gate("lists", AlwaysOn, true)
		g.Extensible = true
		e := Env{Home: home, Gates: []Gate{g, plain.gate("plain", AlwaysOn, true)}}
		res := Check(bash(t, "pass-cli item view x", t.TempDir()), e)
		if res.Allowed() || !strings.Contains(res.Text(), ListsPath(home)) || ext.judged != 0 {
			t.Errorf("gates.toml %q: %q (judged %d), want the extensible gate to refuse naming the file", body, res.Text(), ext.judged)
		}
		if plain.judged != 1 || len(res.Refusals) != 1 {
			t.Errorf("gates.toml %q disturbed a non-extensible gate: judged %d, %d refusals", body, plain.judged, len(res.Refusals))
		}
	}
}

func TestPayloadDecode(t *testing.T) {
	p, err := Decode(payload(t, "Edit", map[string]any{"file_path": "rel/a.go", "old_string": "x"}, "/w"))
	if err != nil || p.FilePath() != filepath.Join("/w", "rel", "a.go") || p.Command() != "" || p.Field("old_string") != "x" {
		t.Errorf("Edit payload = %+v (file %q), %v", p, p.FilePath(), err)
	}
	p, _ = Decode(payload(t, "NotebookEdit", map[string]any{"notebook_path": "/n/x.ipynb"}, "/w"))
	if p.FilePath() != "/n/x.ipynb" {
		t.Errorf("notebook path = %q", p.FilePath())
	}
	p, _ = Decode(bash(t, "ls -la", "/w"))
	if p.Command() != "ls -la" || p.FilePath() != "" || p.Cwd != "/w" || p.Event != "PreToolUse" {
		t.Errorf("Bash payload = %+v", p)
	}
	p, err = Decode([]byte(`{"tool_name":"Bash","tool_input":"not an object","future_key":{"x":1}}`))
	if err != nil || p.Input != nil || p.Command() != "" {
		t.Errorf("odd payload = %+v, %v; want decoded with no input", p, err)
	}
}

func TestContainIn(t *testing.T) {
	root := droot(t)
	if c, ok, err := ContainIn(root, filepath.Join(root, ".dross", "plan.toml")); !ok || err != nil || c.Rel() != ".dross/plan.toml" {
		t.Errorf("inside = %q %v %v", c.Rel(), ok, err)
	}
	for _, p := range []string{filepath.Join(filepath.Dir(root), "elsewhere"), filepath.Join(root, "..", "x"), "relative/path"} {
		if _, ok, err := ContainIn(root, p); ok || err != nil {
			t.Errorf("ContainIn(%q) = %v, %v; want outside", p, ok, err)
		}
	}
}

func TestRecordNeverBlocks(t *testing.T) {
	root := droot(t)
	boom := Recorder{Name: "boom", Claims: func(*Call) bool { return true }, Record: func(*Call) error { panic("x") }}
	erring := Recorder{Name: "erring", Claims: func(*Call) bool { return true }, Record: func(*Call) error { return errors.New("disk full") }}
	ran := 0
	wf := Recorder{Name: "wf", Scope: Workflow, Claims: func(*Call) bool { return true }, Record: func(*Call) error { ran++; return nil }}
	warns := Record(bash(t, "ls", root), Env{Home: t.TempDir(), Recorders: []Recorder{boom, erring, wf}})
	if len(warns) != 2 || !strings.Contains(strings.Join(warns, "\n"), "disk full") || ran != 1 {
		t.Errorf("warnings = %q, workflow recorder ran %d; want two warnings and the recorder run", warns, ran)
	}
	ran = 0
	Record(bash(t, "ls", t.TempDir()), Env{Home: t.TempDir(), Recorders: []Recorder{wf}})
	if ran != 0 {
		t.Error("a workflow recorder ran outside a dross repo")
	}
	if warns := Record([]byte("{"), Env{}); len(warns) != 1 {
		t.Errorf("unparseable record payload warnings = %q, want one", warns)
	}
}

func TestValidate(t *testing.T) {
	ok := Gate{Name: "a", Claims: func(*Call) bool { return false }, Judge: func(*Call) (*Refusal, error) { return nil, nil }}
	if errs := Validate([]Gate{ok}); len(errs) != 0 {
		t.Errorf("a well-formed gate: %v", errs)
	}
	if errs := Validate([]Gate{ok, ok, {Name: "b"}, {}}); len(errs) != 4 {
		t.Errorf("duplicate, missing funcs, no name: %v, want 4 errors", errs)
	}
	if errs := Validate(All()); len(errs) != 0 {
		t.Errorf("the registry is malformed: %v", errs)
	}
}

// TestScopeString pins the labels `dross gate status` prints for each scope.
func TestScopeString(t *testing.T) {
	for s, want := range map[Scope]string{AlwaysOn: "always-on", Workflow: "workflow"} {
		if got := s.String(); got != want {
			t.Errorf("Scope(%d).String() = %q, want %q", s, got, want)
		}
	}
}

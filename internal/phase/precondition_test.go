package phase

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func fp(v float64) *float64 { return &v }

// loadPlanText writes src to a temp plan.toml and loads it.
func loadPlanText(t *testing.T, src string) *Plan {
	t.Helper()
	path := filepath.Join(t.TempDir(), "plan.toml")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := LoadPlan(path)
	if err != nil {
		t.Fatalf("LoadPlan: %v", err)
	}
	return p
}

// TestPreconditionProblemsRefusesMalformedShapes pins every shape rule: each
// malformed precondition, placed second on task t-7 behind a valid one, yields
// a problem naming the task and its 1-based position — through
// PreconditionProblems, through ValidatePlan which folds it in, and through
// ParsePrecondition which shares the rules.
func TestPreconditionProblemsRefusesMalformedShapes(t *testing.T) {
	cases := []struct {
		name string
		pc   Precondition
		want string
	}{
		{"empty run", Precondition{Run: ""}, "run is empty"},
		{"blank run", Precondition{Run: "   "}, "run is empty"},
		{"dash run", Precondition{Run: "-rf"}, `starts with "-"`},
		{"dash run after space", Precondition{Run: "  -x"}, `starts with "-"`},
		{"max nan", Precondition{Run: "x", Max: fp(math.NaN())}, "max = NaN is not a finite number"},
		{"max inf", Precondition{Run: "x", Max: fp(math.Inf(1))}, "max = +Inf is not a finite number"},
		{"min -inf", Precondition{Run: "x", Min: fp(math.Inf(-1))}, "min = -Inf is not a finite number"},
		{"min above max", Precondition{Run: "x", Min: fp(10), Max: fp(5)}, "min 10 is above max 5"},
		{"timeout not a duration", Precondition{Run: "x", Timeout: "soon"}, `timeout "soon" is not a Go duration`},
		{"timeout zero", Precondition{Run: "x", Timeout: "0s"}, `timeout "0s" is not positive`},
		{"timeout negative", Precondition{Run: "x", Timeout: "-1s"}, `timeout "-1s" is not positive`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := &Plan{Task: []Task{
				{ID: "t-1", Preconditions: []Precondition{{Run: "true"}}},
				{ID: "t-7", Preconditions: []Precondition{{Run: "true"}, tc.pc}},
			}}
			got := PreconditionProblems(plan)
			if len(got) != 1 {
				t.Fatalf("PreconditionProblems = %q, want one problem", got)
			}
			if !strings.HasPrefix(got[0], "task t-7 precondition 2: ") || !strings.Contains(got[0], tc.want) {
				t.Errorf("problem = %q, want prefix %q holding %q", got[0], "task t-7 precondition 2: ", tc.want)
			}
			err := ValidatePlan(plan, nil, t.TempDir())
			if err == nil || !strings.Contains(err.Error(), got[0]) {
				t.Errorf("ValidatePlan = %v, want it to carry %q", err, got[0])
			}
			if _, err := ParsePrecondition(inlineOf(tc.pc)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("ParsePrecondition(%s) = %v, want an error holding %q", inlineOf(tc.pc), err, tc.want)
			}
		})
	}
}

// inlineOf renders pc as the TOML inline table ParsePrecondition reads.
func inlineOf(pc Precondition) string {
	parts := []string{`run = "` + pc.Run + `"`}
	for _, b := range []struct {
		k string
		v *float64
	}{{"max", pc.Max}, {"min", pc.Min}} {
		if b.v == nil {
			continue
		}
		switch {
		case math.IsNaN(*b.v):
			parts = append(parts, b.k+" = nan")
		case math.IsInf(*b.v, 1):
			parts = append(parts, b.k+" = inf")
		case math.IsInf(*b.v, -1):
			parts = append(parts, b.k+" = -inf")
		default:
			parts = append(parts, b.k+" = "+formatBound(*b.v))
		}
	}
	if pc.Timeout != "" {
		parts = append(parts, `timeout = "`+pc.Timeout+`"`)
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// TestPreconditionProblemsAcceptsValidShapes is the over-rejection guard.
func TestPreconditionProblemsAcceptsValidShapes(t *testing.T) {
	plan := &Plan{Task: []Task{{ID: "t-1", Preconditions: []Precondition{
		{Run: "true"},
		{Run: "echo 5", Min: fp(5), Max: fp(5)},
		{Run: "echo 0", Max: fp(0), Min: fp(-1)},
		{Run: "echo 3", Max: fp(3), Timeout: "90s", Why: "keep it small"},
		{Run: "a-b --flag", Timeout: "1m30s"},
	}}}}
	if got := PreconditionProblems(plan); got != nil {
		t.Errorf("PreconditionProblems = %q, want nil", got)
	}
	if err := ValidatePlan(plan, nil, t.TempDir()); err != nil {
		t.Errorf("ValidatePlan = %v, want nil", err)
	}
	if got := PreconditionProblems(&Plan{Task: []Task{{ID: "t-1"}}}); got != nil {
		t.Errorf("no preconditions: PreconditionProblems = %q, want nil", got)
	}
}

// TestPreconditionIntegerBoundDecodesAsFloat pins that `max = 5` loads as 5.0,
// through LoadPlan and ParsePrecondition alike.
func TestPreconditionIntegerBoundDecodesAsFloat(t *testing.T) {
	p := loadPlanText(t, `[phase]
id = "x"

[[task]]
id = "t-1"
wave = 1
title = "T"
files = []

[[task.preconditions]]
run = "echo 5"
max = 5
min = -2
`)
	got := p.Task[0].Preconditions
	if len(got) != 1 || got[0].Max == nil || *got[0].Max != 5.0 || got[0].Min == nil || *got[0].Min != -2.0 {
		t.Fatalf("Preconditions = %+v, want max 5.0 and min -2.0", got)
	}
	pc, err := ParsePrecondition(`{run = "echo 5", max = 5}`)
	if err != nil {
		t.Fatal(err)
	}
	if pc.Run != "echo 5" || pc.Max == nil || *pc.Max != 5.0 || pc.Min != nil {
		t.Errorf("ParsePrecondition = %+v, want run \"echo 5\", max 5.0, no min", pc)
	}
}

func TestEffectiveTimeout(t *testing.T) {
	for _, tc := range []struct {
		timeout string
		want    time.Duration
	}{{"", 60 * time.Second}, {"90s", 90 * time.Second}, {"2m", 2 * time.Minute}} {
		got, err := Precondition{Run: "x", Timeout: tc.timeout}.EffectiveTimeout()
		if err != nil || got != tc.want {
			t.Errorf("EffectiveTimeout(%q) = %v, %v; want %v", tc.timeout, got, err, tc.want)
		}
	}
	for _, bad := range []string{"soon", "0s", "-1s"} {
		if got, err := (Precondition{Run: "x", Timeout: bad}).EffectiveTimeout(); err == nil {
			t.Errorf("EffectiveTimeout(%q) = %v, want an error, not a default", bad, got)
		}
	}
}

// TestLoadPlanRecordsUnknownPreconditionKeys: a key the schema does not know
// inside task.preconditions — header or inline form — is recorded with its
// task and position, the load does not fail, and unknown keys anywhere else
// are not recorded.
func TestLoadPlanRecordsUnknownPreconditionKeys(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []UnknownPreconditionKey
	}{
		{"header form", `[phase]
id = "x"

[[task]]
id = "t-1"
wave = 1
title = "One"
files = []

[[task]]
id = "t-2"
wave = 1
title = "Two"
files = []

[[task.preconditions]]
run = "echo 1"

[[task.preconditions]]
run = "echo 2"
mx = 3
`, []UnknownPreconditionKey{{Task: "t-2", Index: 1, Key: "mx"}}},
		{"inline form", `[phase]
id = "x"

[[task]]
id = "t-3"
wave = 1
title = "Three"
files = []
preconditions = [{run = "echo 2", mx = 3}]
`, []UnknownPreconditionKey{{Task: "t-3", Index: 0, Key: "mx"}}},
		{"several keys sort", `[phase]
id = "x"

[[task]]
id = "t-1"
wave = 1
title = "One"
files = []
preconditions = [{run = "x", zz = 1, aa = "y"}]
`, []UnknownPreconditionKey{{Task: "t-1", Index: 0, Key: "aa"}, {Task: "t-1", Index: 0, Key: "zz"}}},
		{"unknown keys elsewhere", `stray = 1

[phase]
id = "x"
bogus = 2

[[task]]
id = "t-1"
wave = 1
title = "One"
files = []
mx = 3

[[task.preconditions]]
run = "echo 1"
max = 3
`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := loadPlanText(t, tc.src)
			if !reflect.DeepEqual(p.UnknownPreconditionKeys, tc.want) {
				t.Errorf("UnknownPreconditionKeys = %+v, want %+v", p.UnknownPreconditionKeys, tc.want)
			}
		})
	}
}

func TestUnknownPreconditionKeyString(t *testing.T) {
	got := UnknownPreconditionKey{Task: "t-2", Index: 1, Key: "mx"}.String()
	for _, want := range []string{"task t-2 precondition 2: ", `unknown key "mx"`, "run, max, min, timeout, why"} {
		if !strings.Contains(got, want) {
			t.Errorf("String() = %q, want it to hold %q", got, want)
		}
	}
}

// TestParsePreconditionIsStrict: an unknown key, a wrong type, trailing keys
// or a non-table is an error; a full valid table parses field for field.
func TestParsePreconditionIsStrict(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`{run = "x", bogus = 1}`, `"bogus"`},
		{`{run = "x", max = "five"}`, "max"},
		{`{run = "x"}` + "\nother = 1", "single"},
		{`"x"`, "single"},
		{`{run = "x"`, "not a TOML inline table"},
		{``, "not a TOML inline table"},
	} {
		if pc, err := ParsePrecondition(tc.in); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("ParsePrecondition(%q) = %+v, %v; want an error holding %q", tc.in, pc, err, tc.want)
		}
	}
	got, err := ParsePrecondition(`{ run = "a, b && c", max = 3.5, min = 1, timeout = "90s", why = "small" }`)
	if err != nil {
		t.Fatal(err)
	}
	want := Precondition{Run: "a, b && c", Max: fp(3.5), Min: fp(1), Timeout: "90s", Why: "small"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParsePrecondition = %+v, want %+v", got, want)
	}
}

// setPlan is a two-task base plan for the fingerprint tests.
func setPlan() *Plan {
	return &Plan{Task: []Task{
		{ID: "t-1", Title: "One", Status: StatusPending, Preconditions: []Precondition{
			{Run: "echo 1", Max: fp(3), Why: "w1"},
			{Run: "true", Min: fp(1), Timeout: "90s"},
		}},
		{ID: "t-2", Title: "Two", Status: StatusPending, Preconditions: []Precondition{
			{Run: "echo 2", Max: fp(5), Min: fp(0), Why: "w2"},
		}},
		{ID: "t-3", Title: "Three", Status: StatusPending},
	}}
}

// TestPreconditionSetLineChangesWithTheSet: every change to what runs, its
// bounds, timeout or reason, or to which task it gates, changes the line.
func TestPreconditionSetLineChangesWithTheSet(t *testing.T) {
	base := PreconditionSetLine(setPlan())
	cases := []struct {
		name string
		mut  func(p *Plan)
	}{
		{"run", func(p *Plan) { p.Task[1].Preconditions[0].Run = "echo 3" }},
		{"max", func(p *Plan) { p.Task[0].Preconditions[0].Max = fp(4) }},
		{"max cleared", func(p *Plan) { p.Task[0].Preconditions[0].Max = nil }},
		{"max set", func(p *Plan) { p.Task[0].Preconditions[1].Max = fp(9) }},
		{"min", func(p *Plan) { p.Task[1].Preconditions[0].Min = fp(1) }},
		{"min becomes max", func(p *Plan) {
			p.Task[0].Preconditions[1].Max, p.Task[0].Preconditions[1].Min = p.Task[0].Preconditions[1].Min, nil
		}},
		{"timeout", func(p *Plan) { p.Task[0].Preconditions[1].Timeout = "91s" }},
		{"timeout from default", func(p *Plan) { p.Task[1].Preconditions[0].Timeout = "61s" }},
		{"timeout malformed", func(p *Plan) { p.Task[0].Preconditions[1].Timeout = "soon" }},
		{"why", func(p *Plan) { p.Task[1].Preconditions[0].Why = "other" }},
		{"add", func(p *Plan) {
			p.Task[1].Preconditions = append(p.Task[1].Preconditions, Precondition{Run: "true"})
		}},
		{"add to a task with none", func(p *Plan) { p.Task[2].Preconditions = []Precondition{{Run: "true"}} }},
		{"remove", func(p *Plan) { p.Task[0].Preconditions = p.Task[0].Preconditions[:1] }},
		{"remove last of a task", func(p *Plan) { p.Task[1].Preconditions = nil }},
		{"move to another task", func(p *Plan) {
			p.Task[2].Preconditions = p.Task[1].Preconditions
			p.Task[1].Preconditions = nil
		}},
		{"reorder within a task", func(p *Plan) {
			pcs := p.Task[0].Preconditions
			pcs[0], pcs[1] = pcs[1], pcs[0]
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := setPlan()
			tc.mut(p)
			if got := PreconditionSetLine(p); got == base {
				t.Errorf("PreconditionSetLine unchanged after %s: %s", tc.name, got)
			}
		})
	}
}

// TestPreconditionSetLineIgnoresEverythingElse: equivalent values and
// changes outside the set leave the line byte-identical.
func TestPreconditionSetLineIgnoresEverythingElse(t *testing.T) {
	base := PreconditionSetLine(setPlan())
	cases := []struct {
		name string
		mut  func(p *Plan)
	}{
		{"max 3 vs 3.0", func(p *Plan) { p.Task[0].Preconditions[0].Max = fp(3.0) }},
		{"min 0 vs -0", func(p *Plan) { p.Task[1].Preconditions[0].Min = fp(math.Copysign(0, -1)) }},
		{`timeout "" vs "60s"`, func(p *Plan) { p.Task[1].Preconditions[0].Timeout = "60s" }},
		{`timeout "90s" vs "1m30s"`, func(p *Plan) { p.Task[0].Preconditions[1].Timeout = "1m30s" }},
		{"title", func(p *Plan) { p.Task[0].Title = "Renamed" }},
		{"status", func(p *Plan) { p.Task[1].Status = StatusInProgress }},
		{"description and files", func(p *Plan) {
			p.Task[0].Description = "d"
			p.Task[0].Files = []string{"a.go"}
		}},
		{"task reorder", func(p *Plan) { p.Task[0], p.Task[1], p.Task[2] = p.Task[2], p.Task[0], p.Task[1] }},
		{"add a task with no preconditions", func(p *Plan) { p.Task = append(p.Task, Task{ID: "t-4", Title: "Four"}) }},
		{"empty preconditions slice", func(p *Plan) { p.Task[2].Preconditions = []Precondition{} }},
		{"phase and task_seq", func(p *Plan) { p.Phase.ID = "y"; p.TaskSeq = 9 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := setPlan()
			tc.mut(p)
			if got := PreconditionSetLine(p); got != base {
				t.Errorf("PreconditionSetLine changed after %s:\n got %s\nwant %s", tc.name, got, base)
			}
		})
	}
}

// TestPreconditionSetLineIgnoresLayout: the same set written with comments,
// header or inline form, integer or float bounds, and an explicit default
// timeout loads to the same line.
func TestPreconditionSetLineIgnoresLayout(t *testing.T) {
	a := loadPlanText(t, `[phase]
id = "x"

[[task]]
id = "t-1"
wave = 1
title = "One"
files = []

[[task.preconditions]]
run = "echo 1"
max = 3
`)
	b := loadPlanText(t, `# a comment
[phase]
  id = "x"

[[task]]
  id = "t-1"   # trailing
  title = "One, retitled"
  wave = 2
  files = ["z.go"]
  status = "done"
  preconditions = [
    # inside
    { run = "echo 1", max = 3.0, timeout = "60s" },
  ]
`)
	la, lb := PreconditionSetLine(a), PreconditionSetLine(b)
	if la == "" || la != lb {
		t.Errorf("layout changed the line:\n a %s\n b %s", la, lb)
	}
}

// TestPreconditionSetLineKeepsMalformedTimeoutVerbatim: a timeout that does
// not parse is carried as written, so editing one malformed value to another
// still changes the line, and it never reads as the default.
func TestPreconditionSetLineKeepsMalformedTimeoutVerbatim(t *testing.T) {
	line := func(timeout string) string {
		return PreconditionSetLine(&Plan{Task: []Task{{ID: "t-1", Preconditions: []Precondition{{Run: "x", Timeout: timeout}}}}})
	}
	soon, later := line("soon"), line("later")
	if !strings.Contains(soon, `"timeout":"soon"`) {
		t.Errorf("malformed timeout not kept verbatim: %s", soon)
	}
	if soon == later || soon == line("") {
		t.Errorf("malformed timeouts collide: soon %s, later %s, default %s", soon, later, line(""))
	}
}

func TestPreconditionSetLineEmpty(t *testing.T) {
	for name, p := range map[string]*Plan{
		"no tasks":             {},
		"tasks without any":    {Task: []Task{{ID: "t-1"}, {ID: "t-2", Title: "x"}}},
		"empty slices on each": {Task: []Task{{ID: "t-1", Preconditions: []Precondition{}}}},
	} {
		if got := PreconditionSetLine(p); got != "" {
			t.Errorf("%s: PreconditionSetLine = %q, want \"\"", name, got)
		}
	}
}

// TestPreconditionSetLineCannotCollide: one precondition whose run carries a
// JSON-looking separator does not encode like two preconditions.
func TestPreconditionSetLineCannotCollide(t *testing.T) {
	one := &Plan{Task: []Task{{ID: "t-1", Preconditions: []Precondition{{Run: `a","b`}}}}}
	two := &Plan{Task: []Task{{ID: "t-1", Preconditions: []Precondition{{Run: "a"}, {Run: "b"}}}}}
	if PreconditionSetLine(one) == PreconditionSetLine(two) {
		t.Errorf("collision: %s", PreconditionSetLine(one))
	}
	// Nor a task id that smuggles a second task.
	idA := &Plan{Task: []Task{{ID: `t-1"},{"task":"t-2`, Preconditions: []Precondition{{Run: "a"}}}}}
	idB := &Plan{Task: []Task{
		{ID: "t-1", Preconditions: []Precondition{{Run: "a"}}},
		{ID: "t-2", Preconditions: []Precondition{{Run: "a"}}},
	}}
	if PreconditionSetLine(idA) == PreconditionSetLine(idB) {
		t.Errorf("collision on task id: %s", PreconditionSetLine(idA))
	}
}

// TestPreconditionSetLineShape pins the exact canonical encoding: sorted
// tasks, in-plan precondition order, every key present, the effective timeout.
func TestPreconditionSetLineShape(t *testing.T) {
	p := &Plan{Task: []Task{
		{ID: "t-2", Preconditions: []Precondition{{Run: "b", Max: fp(5)}}},
		{ID: "t-1", Preconditions: []Precondition{{Run: "z", Min: fp(0.5), Timeout: "90s", Why: "w"}, {Run: "a"}}},
	}}
	want := `[{"preconditions":[{"max":null,"min":"0.5","run":"z","timeout":"1m30s","why":"w"},` +
		`{"max":null,"min":null,"run":"a","timeout":"1m0s","why":""}],"task":"t-1"},` +
		`{"preconditions":[{"max":"5","min":null,"run":"b","timeout":"1m0s","why":""}],"task":"t-2"}]`
	if got := PreconditionSetLine(p); got != want {
		t.Errorf("PreconditionSetLine =\n %s\nwant\n %s", got, want)
	}
}

// TestPlanSaveRoundTripsPreconditions: a plan carrying preconditions saves
// and reloads to the same struct, and a plan without writes no key for them.
func TestPlanSaveRoundTripsPreconditions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plan.toml")
	p := &Plan{Phase: PlanPhase{ID: "x"}, Task: []Task{
		{ID: "t-1", Wave: 1, Title: "One", Files: []string{"a.go"}},
		{ID: "t-2", Wave: 1, Title: "Two", Files: []string{"b.go"}, Preconditions: []Precondition{
			{Run: "echo 3", Max: fp(3), Min: fp(1), Timeout: "90s", Why: "w"},
			{Run: "true"},
		}},
	}}
	if err := p.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := LoadPlan(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, p) {
		t.Errorf("reload = %+v, want %+v", got, p)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(body), "preconditions"); n != 2 {
		t.Errorf("saved plan names preconditions %d times, want 2 (t-2's blocks only):\n%s", n, body)
	}
}

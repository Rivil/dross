package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/pflag"

	"github.com/Rivil/dross/internal/consent"
	"github.com/Rivil/dross/internal/localstore"
	"github.com/Rivil/dross/internal/phase"
)

// precondEditPlan is two tasks, t-1 carrying preconditions in the header form
// with comments around them, so a save that disturbs another task's bytes has
// something to disturb.
const precondEditPlan = `[phase]
id = "01-test"

[[task]]
id = "t-1"
wave = 1
title = "first"
files = ["a.go"]
covers = ["c-1"]

# the build must stay green before t-1 starts
[[task.preconditions]]
run = "go build ./..."   # exit 0 is enough

[[task.preconditions]]
run = "go test ./x | wc -l"
max = 3

[[task]]
id = "t-2"
wave = 1
title = "second"
files = ["b.go"]
covers = ["c-1"]
`

// precondEditFixture scaffolds a fresh repo whose phase 01-test carries plan.
func precondEditFixture(t *testing.T, plan string) {
	t.Helper()
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", plan)
}

// taskOut runs `dross task <args>`, returning its stdout and error.
func taskOut(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out string
	err := runCmdCapturing(t, &out, Task(), args...)
	return out, err
}

func loadEditPlan(t *testing.T) *phase.Plan {
	t.Helper()
	p, err := phase.LoadPlan(reasonPlanPath())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func runsOf(pcs []phase.Precondition) []string {
	var out []string
	for _, p := range pcs {
		out = append(out, p.Run)
	}
	return out
}

func TestTaskAddPreconditionRoundTripsAndTouchesNoOtherTask(t *testing.T) {
	precondEditFixture(t, precondEditPlan)
	before := planBytes(t)
	if _, err := taskOut(t, "add", "01-test", "--title", "third",
		"--precondition", `{run = "go test ./x", max = 5}`); err != nil {
		t.Fatalf("add: %v", err)
	}
	after := planBytes(t)
	// A tail append: the high-water counter is the one line added above the
	// plan, and every byte of the plan as it was follows it, in place.
	if !bytes.HasPrefix(after, append([]byte("task_seq = 3\n"), before...)) {
		t.Errorf("add changed bytes outside the new task:\n--- before\n%s\n--- after\n%s", before, after)
	}
	added := loadEditPlan(t).FindTask("t-3")
	if added == nil {
		t.Fatal("t-3 not added")
	}
	got := added.Preconditions
	if len(got) != 1 || got[0].Run != "go test ./x" || got[0].Max == nil || *got[0].Max != 5 ||
		got[0].Min != nil || got[0].Timeout != "" || got[0].Why != "" {
		t.Errorf("t-3 reloaded preconditions = %+v, want run \"go test ./x\", max 5 and nothing else", got)
	}
}

func TestPreconditionFlagIsNotSplitOnCommas(t *testing.T) {
	const tricky = `{ run = "a, b && c", max = 3 }`
	for _, args := range [][]string{
		{"add", "01-test", "--title", "x", "--precondition", tricky, "--precondition", `{run = "true"}`},
		{"edit", "01-test", "t-2", "--precondition", tricky, "--precondition", `{run = "true"}`},
		{"edit", "01-test", "t-2", "--add-precondition", tricky, "--add-precondition", `{run = "true"}`},
	} {
		t.Run(args[0]+" "+args[len(args)-4], func(t *testing.T) {
			precondEditFixture(t, precondEditPlan)
			if _, err := taskOut(t, args...); err != nil {
				t.Fatalf("%v: %v", args, err)
			}
			id := "t-2"
			if args[0] == "add" {
				id = "t-3"
			}
			got := loadEditPlan(t).FindTask(id).Preconditions
			if !slices.Equal(runsOf(got), []string{"a, b && c", "true"}) {
				t.Errorf("runs = %q, want exactly [\"a, b && c\" \"true\"]", runsOf(got))
			}
			if got[0].Max == nil || *got[0].Max != 3 {
				t.Errorf("max = %v, want 3", got[0].Max)
			}
		})
	}
}

// malformedPreconditions are flag values no route may write into a plan.
var malformedPreconditions = []struct{ name, val string }{
	{"empty run", `{run = ""}`},
	{"unknown key", `{run = "x", bogus = 1}`},
	{"bad timeout", `{run = "x", timeout = "soon"}`},
	{"malformed TOML", `{run = "x"`},
}

func TestMalformedPreconditionFlagWritesNothing(t *testing.T) {
	for _, m := range malformedPreconditions {
		for _, args := range [][]string{
			{"add", "01-test", "--title", "x", "--precondition", m.val},
			{"edit", "01-test", "t-2", "--precondition", m.val},
			{"edit", "01-test", "t-2", "--add-precondition", m.val},
		} {
			t.Run(m.name+" "+args[len(args)-2], func(t *testing.T) {
				precondEditFixture(t, precondEditPlan)
				before := planBytes(t)
				if _, err := taskOut(t, args...); err == nil {
					t.Errorf("%v accepted %s", args[:len(args)-1], m.val)
				}
				if !bytes.Equal(planBytes(t), before) {
					t.Errorf("a refused %s changed plan.toml", m.val)
				}
			})
		}
	}
}

// TestValidateAndEditAgreeOnPreconditionShape: the same value is refused by
// `task edit` exactly when `dross validate` reports it in a plan.
func TestValidateAndEditAgreeOnPreconditionShape(t *testing.T) {
	cases := append(slices.Clone(malformedPreconditions), struct{ name, val string }{"well-formed", `{run = "x", min = 1, max = 2, timeout = "90s", why = "w"}`})
	for _, m := range cases {
		t.Run(m.name, func(t *testing.T) {
			// The value as t-2's inline precondition list.
			plan := precondEditPlan + "preconditions = [" + m.val + "]\n"
			_, verr := validatePrecondPlan(t, plan)

			precondEditFixture(t, precondEditPlan)
			_, eerr := taskOut(t, "edit", "01-test", "t-2", "--precondition", m.val)
			if (verr == nil) != (eerr == nil) {
				t.Errorf("validate err = %v, edit err = %v: they disagree on %s", verr, eerr, m.val)
			}
			if m.name == "well-formed" && eerr != nil {
				t.Errorf("a well-formed precondition was refused: %v", eerr)
			}
		})
	}
}

func TestTaskEditPreconditionSemantics(t *testing.T) {
	t.Run("--precondition replaces", func(t *testing.T) {
		precondEditFixture(t, precondEditPlan)
		if _, err := taskOut(t, "edit", "01-test", "t-1", "--precondition", `{run = "c"}`); err != nil {
			t.Fatal(err)
		}
		if got := runsOf(loadEditPlan(t).FindTask("t-1").Preconditions); !slices.Equal(got, []string{"c"}) {
			t.Errorf("runs = %q, want [c]", got)
		}
	})
	t.Run("--add-precondition appends", func(t *testing.T) {
		precondEditFixture(t, precondEditPlan)
		if _, err := taskOut(t, "edit", "01-test", "t-1", "--add-precondition", `{run = "c"}`); err != nil {
			t.Fatal(err)
		}
		want := []string{"go build ./...", "go test ./x | wc -l", "c"}
		if got := runsOf(loadEditPlan(t).FindTask("t-1").Preconditions); !slices.Equal(got, want) {
			t.Errorf("runs = %q, want %q", got, want)
		}
	})
	// Only the cleared task carries preconditions, so after the clear the
	// file holds none at all — not a header block, not the stray key the
	// schema never decoded.
	t.Run("--clear-preconditions empties, header form with a stray key", func(t *testing.T) {
		precondEditFixture(t, strings.Replace(precondEditPlan, "max = 3\n", "max = 3\nbogus = 1\n", 1))
		if _, err := taskOut(t, "edit", "01-test", "t-1", "--clear-preconditions"); err != nil {
			t.Fatalf("clear: %v", err)
		}
		raw := string(planBytes(t))
		for _, gone := range []string{"preconditions", "bogus", "run ="} {
			if strings.Contains(raw, gone) {
				t.Errorf("clear left %q behind:\n%s", gone, raw)
			}
		}
		p := loadEditPlan(t)
		if n := len(p.FindTask("t-1").Preconditions); n != 0 {
			t.Errorf("t-1 still has %d preconditions", n)
		}
		if len(p.UnknownPreconditionKeys) != 0 {
			t.Errorf("unknown keys survived the clear: %v", p.UnknownPreconditionKeys)
		}
	})
	// The lossless patcher does not rewrite an inline table in place: the
	// clear is refused, naming the rewrite, and the plan stays whole rather
	// than half-cleared.
	t.Run("--clear-preconditions on the inline form refuses and writes nothing", func(t *testing.T) {
		precondEditFixture(t, precondPlan(precondTask{id: "t-1"}, precondTask{id: "t-2"})+
			`preconditions = [{ run = "true", bogus = 1 }]`+"\n")
		before := planBytes(t)
		_, err := taskOut(t, "edit", "01-test", "t-2", "--clear-preconditions")
		if err == nil || !strings.Contains(err.Error(), "inline table") {
			t.Fatalf("clear of an inline list = %v, want the patcher's inline-table refusal", err)
		}
		if !bytes.Equal(planBytes(t), before) {
			t.Error("a refused clear changed plan.toml")
		}
	})
	for _, args := range [][]string{
		{"--clear-preconditions", "--precondition", `{run = "c"}`},
		{"--clear-preconditions", "--add-precondition", `{run = "c"}`},
		{"--precondition", `{run = "c"}`, "--add-precondition", `{run = "d"}`},
	} {
		t.Run("refuses "+strings.Join(args, " "), func(t *testing.T) {
			precondEditFixture(t, precondEditPlan)
			before := planBytes(t)
			if _, err := taskOut(t, append([]string{"edit", "01-test", "t-1"}, args...)...); err == nil {
				t.Errorf("edit %v was accepted", args)
			}
			if !bytes.Equal(planBytes(t), before) {
				t.Error("a refused combination changed plan.toml")
			}
		})
	}
}

// grantState is what this machine says about phase 01-test's current set.
func grantState(t *testing.T) consent.State {
	t.Helper()
	root, err := FindRoot()
	if err != nil {
		t.Fatal(err)
	}
	state, err := consent.PreconditionsConsented(localstore.GrantStore(root), filepath.Dir(root), "01-test", phase.PreconditionSetLine(loadEditPlan(t)))
	// Only the three states these fixtures can reach, each with its own
	// error: anything else (a refused or unreadable store) would let an
	// assertion on the state pass for the wrong reason.
	switch {
	case state == consent.Granted && err == nil,
		state == consent.Stale && errors.Is(err, consent.ErrStaleConsent),
		state == consent.Absent && err == consent.ErrNoConsent:
		return state
	}
	t.Fatalf("grant state %v: %v", state, err)
	return state
}

const trustLine = "`dross trust --preconditions 01-test`"

func TestPreconditionSetChangeReportsRevokedGrant(t *testing.T) {
	for _, args := range [][]string{
		{"edit", "01-test", "t-1", "--precondition", `{run = "go build ./..."}`},
		{"edit", "01-test", "t-1", "--add-precondition", `{run = "true"}`},
		{"edit", "01-test", "t-2", "--precondition", `{run = "true"}`},
		{"edit", "01-test", "t-1", "--precondition", `{run = "go build ./...", why = "w"}`, "--title", "y"},
		{"add", "01-test", "--title", "x", "--precondition", `{run = "true"}`},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			precondEditFixture(t, precondEditPlan)
			grantPreconditions(t)
			out, err := taskOut(t, args...)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out, "grant is revoked") || !strings.Contains(out, trustLine) {
				t.Errorf("output does not report the revoked grant and %s:\n%s", trustLine, out)
			}
			if s := grantState(t); s != consent.Stale {
				t.Errorf("grant = %v after the set changed, want Stale", s)
			}
		})
	}
}

func TestPreconditionSetUnchangedPrintsNoRevocation(t *testing.T) {
	for _, args := range [][]string{
		// An unset timeout and "60s" are one set, as are 3 and 3.0.
		{"edit", "01-test", "t-1",
			"--precondition", `{run = "go build ./...", timeout = "60s"}`,
			"--precondition", `{run = "go test ./x | wc -l", max = 3.0, timeout = "1m"}`},
		// The identical set, rewritten.
		{"edit", "01-test", "t-1",
			"--precondition", `{run = "go build ./..."}`,
			"--precondition", `{run = "go test ./x | wc -l", max = 3}`},
		{"edit", "01-test", "t-1", "--title", "renamed"},
		{"add", "01-test", "--title", "no preconditions"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			precondEditFixture(t, precondEditPlan)
			grantPreconditions(t)
			out, err := taskOut(t, args...)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(out, "grant") || strings.Contains(out, "dross trust") {
				t.Errorf("an edit that leaves the set alone printed a grant line:\n%s", out)
			}
			if s := grantState(t); s != consent.Granted {
				t.Errorf("grant = %v after an edit that leaves the set alone, want Granted", s)
			}
		})
	}
}

func TestPreconditionGrantNoteOtherArms(t *testing.T) {
	t.Run("never trusted", func(t *testing.T) {
		precondEditFixture(t, precondEditPlan)
		out, err := taskOut(t, "edit", "01-test", "t-2", "--precondition", `{run = "true"}`)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "has not trusted them") || !strings.Contains(out, trustLine) {
			t.Errorf("untrusted set change does not name %s:\n%s", trustLine, out)
		}
	})
	t.Run("set emptied", func(t *testing.T) {
		precondEditFixture(t, precondEditPlan)
		grantPreconditions(t)
		out, err := taskOut(t, "edit", "01-test", "t-1", "--clear-preconditions")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "declares no preconditions now") || strings.Contains(out, "dross trust") {
			t.Errorf("emptied set: want the no-preconditions line and no trust instruction:\n%s", out)
		}
	})
	t.Run("edited back to the trusted set", func(t *testing.T) {
		precondEditFixture(t, precondEditPlan)
		grantPreconditions(t)
		if _, err := taskOut(t, "edit", "01-test", "t-1", "--precondition", `{run = "true"}`); err != nil {
			t.Fatal(err)
		}
		out, err := taskOut(t, "edit", "01-test", "t-1",
			"--precondition", `{run = "go build ./..."}`, "--precondition", `{run = "go test ./x | wc -l", max = 3}`)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "grant stands") || strings.Contains(out, "revoked") {
			t.Errorf("back-to-trusted edit misreports the grant:\n%s", out)
		}
		if s := grantState(t); s != consent.Granted {
			t.Errorf("grant = %v, want Granted for the trusted set", s)
		}
	})
	t.Run("tracked store", func(t *testing.T) {
		_, dir := precondTrustFixture(t, precondEditPlan)
		grantPreconditions(t)
		mustGit(t, dir, "add", "-f", RootDirName+"/"+LocalFile)
		out, err := taskOut(t, "edit", "01-test", "t-1", "--precondition", `{run = "true"}`)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "git reports it tracked") || !strings.Contains(out, trustLine) {
			t.Errorf("a tracked store's refusal is not surfaced with %s:\n%s", trustLine, out)
		}
		if strings.Contains(out, "has not trusted them") {
			t.Errorf("a tracked store reads as never trusted:\n%s", out)
		}
	})
	t.Run("unparseable store", func(t *testing.T) {
		precondEditFixture(t, precondEditPlan)
		root, err := FindRoot()
		if err != nil {
			t.Fatal(err)
		}
		mustWrite(t, localPath(root), "this is = = not toml\n")
		out, err := taskOut(t, "edit", "01-test", "t-1", "--precondition", `{run = "true"}`)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "no readable precondition grant ("+consent.ErrNoConsent.Error()+": ") ||
			!strings.Contains(out, trustLine) {
			t.Errorf("an unreadable store's reason is not surfaced with %s:\n%s", trustLine, out)
		}
	})
	t.Run("no dross root", func(t *testing.T) {
		chdir(t, t.TempDir())
		p := &phase.Plan{Task: []phase.Task{{ID: "t-1", Preconditions: []phase.Precondition{{Run: "true"}}}}}
		out := captureStdout(t, func() { notePreconditionGrant("01-test", "", p) })
		if !strings.Contains(out, "could not be read") || !strings.Contains(out, trustLine) {
			t.Errorf("an unreadable root is not surfaced with %s:\n%s", trustLine, out)
		}
	})
	t.Run("task remove", func(t *testing.T) {
		precondEditFixture(t, precondEditPlan+`preconditions = [{ run = "true" }]`+"\n")
		grantPreconditions(t)
		out, err := taskOut(t, "remove", "01-test", "t-2")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "grant is revoked") || !strings.Contains(out, trustLine) {
			t.Errorf("removing a task with preconditions does not report the revoked grant:\n%s", out)
		}
	})
}

const showPrecondPlan = `[phase]
id = "01-test"

[[task]]
id = "t-1"
wave = 1
title = "first"
files = ["a.go"]
covers = ["c-1"]

[[task.preconditions]]
run = "go test ./x | wc -l"
max = 3
min = 1.5
timeout = "90s"
why = "no more than three failing lines"

[[task.preconditions]]
run = "go build ./..."
mx = 4
`

func TestTaskShowPrintsPreconditions(t *testing.T) {
	precondEditFixture(t, showPrecondPlan)
	out, err := taskOut(t, "show", "01-test", "t-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"preconditions:",
		"1. run:     go test ./x | wc -l",
		"max:     3",
		"min:     1.5",
		"timeout: 90s",
		"why:     no more than three failing lines",
		"2. run:     go build ./...",
		"timeout: 1m0s (default)",
		`✗ task t-1 precondition 2: unknown key "mx"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("show lacks %q:\n%s", want, out)
		}
	}

	jsonOut, err := taskOut(t, "show", "01-test", "t-1", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var rec struct {
		Preconditions []map[string]any `json:"preconditions"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &rec); err != nil {
		t.Fatalf("show --json: %v\n%s", err, jsonOut)
	}
	if len(rec.Preconditions) != 2 {
		t.Fatalf("--json preconditions = %v, want 2 entries", rec.Preconditions)
	}
	first := rec.Preconditions[0]
	for k, v := range map[string]any{"run": "go test ./x | wc -l", "max": 3.0, "min": 1.5, "timeout": "90s", "why": "no more than three failing lines"} {
		if first[k] != v {
			t.Errorf("--json preconditions[0][%q] = %v, want %v", k, first[k], v)
		}
	}
}

func TestTaskShowWithoutPreconditionsPrintsNone(t *testing.T) {
	precondEditFixture(t, precondEditPlan)
	out, err := taskOut(t, "show", "01-test", "t-2")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "preconditions") {
		t.Errorf("a task with no preconditions printed a section:\n%s", out)
	}
}

// TestNoPreconditionBypassFlag pins the whole flag set of the verbs that could
// carry a bypass (no_bypass): any new flag — a --skip-precondition, a --force —
// fails here and has to be argued for.
func TestNoPreconditionBypassFlag(t *testing.T) {
	want := map[string][]string{
		"edit": {"add-precondition", "add-test-contract", "clear-preconditions", "covers", "depends-on",
			"description", "files", "precondition", "test-contract", "title", "wave"},
		"status": {"reason"},
	}
	for _, c := range Task().Commands() {
		names, ok := want[c.Name()]
		if !ok {
			continue
		}
		var got []string
		c.Flags().VisitAll(func(f *pflag.Flag) { got = append(got, f.Name) })
		if !slices.Equal(got, names) {
			t.Errorf("task %s flags = %q, want exactly %q", c.Name(), got, names)
		}
		delete(want, c.Name())
	}
	if len(want) != 0 {
		t.Errorf("verbs not found: %v", want)
	}
}

func TestEditTaskClearsToNoKey(t *testing.T) {
	p := &phase.Plan{Task: []phase.Task{{ID: "t-1", Preconditions: []phase.Precondition{{Run: "true"}}}}}
	none := []phase.Precondition{}
	if err := p.EditTask("t-1", phase.TaskEdit{Preconditions: &none}); err != nil {
		t.Fatal(err)
	}
	if p.Task[0].Preconditions != nil {
		t.Errorf("cleared preconditions = %#v, want nil", p.Task[0].Preconditions)
	}
}

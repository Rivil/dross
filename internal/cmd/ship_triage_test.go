package cmd

import (
	"path/filepath"
	"strings"
	"testing"
)

// writeTriagePlan gives phase x a plan whose tasks hold the given statuses,
// keyed by id, and commits it as .dross bookkeeping.
func writeTriagePlan(t *testing.T, dir string, statuses map[string]string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("[phase]\nid = \"x\"\n")
	for _, id := range []string{"t-1", "t-17", "t-18"} {
		st, ok := statuses[id]
		if !ok {
			continue
		}
		b.WriteString("\n[[task]]\nid = \"" + id + "\"\nwave = 1\ntitle = \"a task\"\nfiles = [\"src/tag.ts\"]\ncovers = [\"C1\"]\ntest_contract = [\"it works\"]\nstatus = \"" + st + "\"\n")
		if st == "failed" {
			b.WriteString("reason = \"superseded by the fix in t-1\"\n")
		}
	}
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "x", "plan.toml"), b.String())
}

// writeShipTriage writes phase x's pr-triage.toml from resolution bodies.
func writeShipTriage(t *testing.T, dir string, entries ...string) {
	t.Helper()
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "x", "pr-triage.toml"), strings.Join(entries, "\n"))
}

func shipTriageEntry(id, verdict, extra string) string {
	kind := map[byte]string{'c': "conversation", 'i': "inline", 'r': "review"}[id[0]]
	return `[[resolution]]
id = "` + id + `"
kind = "` + kind + `"
pr = 7
url = "https://forge.example/me/p/pulls/7#c"
author = "bob"
verdict = "` + verdict + `"
at = "src/tag.ts:1"
digest = "` + strings.Repeat("cd", 32) + `"
` + extra + "\n"
}

// TestShipRefusesUndoneAccept: an accept whose task is pending or in
// progress stops ship before anything reaches origin or the forge.
func TestShipRefusesUndoneAccept(t *testing.T) {
	for _, st := range []string{"pending", "in_progress"} {
		t.Run(st, func(t *testing.T) {
			dir := stampedShipFixture(t)
			cap, remoteDir := shipMockFlowRemote(t, dir)
			stampVerdict(t, dir)
			writeTriagePlan(t, dir, map[string]string{"t-17": st})
			writeShipTriage(t, dir, shipTriageEntry("c12", "accept", `task = "t-17"`))
			gitCommit(t, dir, "chore(dross): triage")

			err := shipErr(t, "x")
			if err == nil {
				t.Fatal("ship went ahead with an accepted comment's task " + st)
			}
			for _, want := range []string{"c12", "t-17", st} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal does not name %q:\n%v", want, err)
				}
			}
			if cap.posts != 0 {
				t.Errorf("POST /pulls ran %d time(s)", cap.posts)
			}
			if out, _ := gitOut(remoteDir, "show-ref", "--verify", "refs/heads/phase/x"); strings.TrimSpace(out) != "" {
				t.Errorf("phase/x reached origin: %s", out)
			}
		})
	}

	t.Run("a task the plan lacks", func(t *testing.T) {
		dir := stampedShipFixture(t)
		writeTriagePlan(t, dir, map[string]string{"t-1": "done"})
		writeShipTriage(t, dir, shipTriageEntry("c12", "accept", `task = "t-17"`))
		gitCommit(t, dir, "chore(dross): triage")
		if err := shipErr(t, "--no-push", "x"); err == nil || !strings.Contains(err.Error(), "not in the plan") {
			t.Errorf("err = %v, want a refusal naming the missing task", err)
		}
	})

	t.Run("every open accept is named", func(t *testing.T) {
		dir := stampedShipFixture(t)
		writeTriagePlan(t, dir, map[string]string{"t-17": "pending", "t-18": "in_progress", "t-1": "done"})
		writeShipTriage(t, dir,
			shipTriageEntry("c12", "accept", `task = "t-17"`),
			shipTriageEntry("i13", "accept", `task = "t-18"`),
			shipTriageEntry("r14", "accept", `task = "t-1"`))
		gitCommit(t, dir, "chore(dross): triage")
		err := shipErr(t, "--no-push", "x")
		if err == nil || !strings.Contains(err.Error(), "c12 -> t-17") || !strings.Contains(err.Error(), "i13 -> t-18") || strings.Contains(err.Error(), "r14") {
			t.Errorf("err = %v, want c12 and i13 named and the settled r14 left out", err)
		}
	})
}

// TestShipAllowsSettledAccepts: a done or failed task, or a record of only
// rejects and routes, lets ship through; with no record at all ship opens the
// PR exactly as it always has.
func TestShipAllowsSettledAccepts(t *testing.T) {
	cases := map[string]func(t *testing.T, dir string){
		"accepted task done": func(t *testing.T, dir string) {
			writeTriagePlan(t, dir, map[string]string{"t-17": "done"})
			writeShipTriage(t, dir, shipTriageEntry("c12", "accept", `task = "t-17"`))
		},
		"accepted task failed with a reason": func(t *testing.T, dir string) {
			writeTriagePlan(t, dir, map[string]string{"t-17": "failed"})
			writeShipTriage(t, dir, shipTriageEntry("c12", "accept", `task = "t-17"`))
		},
		"rejects and routes only": func(t *testing.T, dir string) {
			writeTriagePlan(t, dir, map[string]string{"t-17": "pending"})
			writeShipTriage(t, dir,
				shipTriageEntry("c12", "reject", `reason = "bounded already"`),
				shipTriageEntry("c13", "route", `deferred = "abc"
target = "later"`))
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			dir := stampedShipFixture(t)
			setup(t, dir)
			gitCommit(t, dir, "chore(dross): triage")
			if err := shipErr(t, "--no-push", "x"); err != nil {
				t.Errorf("ship refused: %v", err)
			}
		})
	}

	t.Run("no record", func(t *testing.T) {
		dir := stampedShipFixture(t)
		cap := shipMockFlow(t, dir)
		stampVerdict(t, dir)
		if err := shipErr(t, "x"); err != nil {
			t.Fatalf("ship with no pr-triage.toml: %v", err)
		}
		if cap.posts != 1 {
			t.Errorf("POST /pulls ran %d time(s), want 1", cap.posts)
		}
	})
}

// TestShipRefusesMalformedTriage: a record that does not load stops ship
// naming the file, before any provider call.
func TestShipRefusesMalformedTriage(t *testing.T) {
	dir := stampedShipFixture(t)
	cap := shipMockFlow(t, dir)
	stampVerdict(t, dir)
	writeShipTriage(t, dir, shipTriageEntry("c12", "reject", `reason = "r"
body = "pasted comment text"`))
	gitCommit(t, dir, "chore(dross): triage")
	err := shipErr(t, "x")
	if err == nil || !strings.Contains(err.Error(), "pr-triage.toml") {
		t.Fatalf("err = %v, want a refusal naming pr-triage.toml", err)
	}
	if cap.posts != 0 {
		t.Errorf("POST /pulls ran %d time(s)", cap.posts)
	}
}

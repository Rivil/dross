package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/prtriage"
	"github.com/Rivil/dross/internal/ship"
	"github.com/Rivil/dross/internal/telemetry"
)

// resolveRepo is prRepo with a stubbed PR #7 on phase/x carrying comment 1
// by alice, an open phase "later" to route to, and the mirror counted.
func resolveRepo(t *testing.T) (dir string, s *prStubs, mirrors *int) {
	t.Helper()
	dir = prRepo(t)
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "x", "spec.toml"), "[phase]\nid = \"x\"\ntitle = \"X\"\n\n[[criteria]]\nid = \"c-1\"\ntext = \"does a thing\"\n")
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "later", "spec.toml"), "[phase]\nid = \"later\"\ntitle = \"Later\"\n")
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "done", "spec.toml"), "[phase]\nid = \"done\"\ntitle = \"Done\"\n")
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "done", "changes.json"), `{"phase":"done","status":"complete","tasks":{}}`)
	gitCommit(t, dir, "chore(dross): phases")
	s = &prStubs{head: phaseXHead(), self: prSelf, comments: []ship.PRComment{
		prComment("1", ship.CommentConversation, "11", "alice", ship.AuthorHuman, "why loop forever?"),
	}}
	stubPRThread(t, s)
	n := 0
	prev := mirrorRoute
	mirrorRoute = func(string, deferredEntry) { n++ }
	t.Cleanup(func() { mirrorRoute = prev })
	return dir, s, &n
}

// seen1 is the digest `dross pr comments` prints for comment 1.
func seen1() string { return prtriage.Digest("why loop forever?") }

// resolve runs `dross pr resolve 7 <args>` with stdin.
func resolve(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	c := prRoot()
	var out bytes.Buffer
	c.SetArgs(append([]string{"resolve", "7"}, args...))
	c.SetIn(strings.NewReader(stdin))
	c.SetOut(&out)
	c.SetErr(new(bytes.Buffer))
	err := c.Execute()
	return out.String(), err
}

func phaseFile(dir, name string) string {
	return filepath.Join(dir, ".dross", "phases", "x", name)
}

func readOrEmpty(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func loadTriage(t *testing.T, dir string) prtriage.Record {
	t.Helper()
	rec, _, err := prtriage.Load(phaseFile(dir, prtriage.File))
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func TestPRResolveAccept(t *testing.T) {
	dir, _, _ := resolveRepo(t)
	if err := runCmd(t, State(), "set", "current_phase", "later"); err != nil {
		t.Fatal(err)
	}
	if _, err := resolve(t, "", "c1", "--seen", seen1(), "--accept", "--title", "Bound the loop", "--at", "x.go:1"); err != nil {
		t.Fatal(err)
	}
	plan, err := phase.LoadPlan(phaseFile(dir, "plan.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Task) != 2 {
		t.Fatalf("plan has %d tasks, want the original plus one", len(plan.Task))
	}
	added := plan.Task[1]
	if added.ID != "t-2" || added.Status != phase.StatusPending || added.Title != "Bound the loop" ||
		!strings.Contains(added.Description, "https://github.com/acme/widgets/pull/7#c1") {
		t.Errorf("added task = %+v", added)
	}
	rec := loadTriage(t, dir)
	if len(rec.Resolution) != 1 {
		t.Fatalf("record = %+v", rec)
	}
	r := rec.Resolution[0]
	if r.ID != "c1" || r.Verdict != prtriage.VerdictAccept || r.Task != "t-2" || r.At != "x.go:1" || r.Digest != seen1() || r.PR != 7 {
		t.Errorf("resolution = %+v", r)
	}
	if err := runCmd(t, Validate()); err != nil {
		t.Errorf("dross validate after an accept: %v", err)
	}
}

func TestPRResolveReject(t *testing.T) {
	dir, _, _ := resolveRepo(t)
	plan, spec := readOrEmpty(t, phaseFile(dir, "plan.toml")), readOrEmpty(t, phaseFile(dir, "spec.toml"))
	if _, err := resolve(t, "", "c1", "--seen", seen1(), "--reject", "--reason", "it is bounded by maxPages", "--at", "x.go:1"); err != nil {
		t.Fatal(err)
	}
	if r := loadTriage(t, dir).Resolution; len(r) != 1 || r[0].Verdict != prtriage.VerdictReject || r[0].Reason != "it is bounded by maxPages" {
		t.Errorf("record = %+v", r)
	}
	if readOrEmpty(t, phaseFile(dir, "plan.toml")) != plan || readOrEmpty(t, phaseFile(dir, "spec.toml")) != spec {
		t.Error("a reject changed plan.toml or spec.toml")
	}
}

func TestPRResolveRoute(t *testing.T) {
	dir, _, mirrors := resolveRepo(t)
	plan := readOrEmpty(t, phaseFile(dir, "plan.toml"))
	if _, err := resolve(t, "", "c1", "--seen", seen1(), "--route", "--title", "Rework paging", "--target", "later", "--at", "x.go:1"); err != nil {
		t.Fatal(err)
	}
	spec, err := phase.LoadSpec(phaseFile(dir, "spec.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Deferred) != 1 || spec.Deferred[0].Target != "later" || spec.Deferred[0].ID == "" || spec.Deferred[0].Text != "Rework paging" {
		t.Fatalf("spec deferred = %+v", spec.Deferred)
	}
	r := loadTriage(t, dir).Resolution
	if len(r) != 1 || r[0].Verdict != prtriage.VerdictRoute || r[0].Deferred != spec.Deferred[0].ID || r[0].Target != "later" {
		t.Errorf("record = %+v", r)
	}
	if readOrEmpty(t, phaseFile(dir, "plan.toml")) != plan {
		t.Error("a route changed plan.toml")
	}
	if *mirrors != 1 {
		t.Errorf("board mirror ran %d times, want 1", *mirrors)
	}

	for _, target := range []string{"nope", "done"} {
		t.Run(target, func(t *testing.T) {
			dir, _, mirrors := resolveRepo(t)
			spec := readOrEmpty(t, phaseFile(dir, "spec.toml"))
			if _, err := resolve(t, "", "c1", "--seen", seen1(), "--route", "--title", "x", "--target", target, "--at", "x.go:1"); err == nil {
				t.Fatalf("--target %s accepted", target)
			}
			if readOrEmpty(t, phaseFile(dir, "spec.toml")) != spec || readOrEmpty(t, phaseFile(dir, prtriage.File)) != "" || *mirrors != 0 {
				t.Errorf("--target %s wrote something", target)
			}
		})
	}
}

// TestPRResolveRouteWithReason: a route's --reason leads the deferred item's
// why, ahead of the comment link; a blank one adds nothing.
func TestPRResolveRouteWithReason(t *testing.T) {
	const link = "review comment https://github.com/acme/widgets/pull/7#c1"
	for _, tc := range []struct {
		name    string
		reason  []string
		wantWhy string
	}{
		{"reason", []string{"--reason", "wrong layer"}, "wrong layer — " + link},
		{"blank reason", []string{"--reason", "  "}, link},
		{"no reason", nil, link},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, _, _ := resolveRepo(t)
			args := append([]string{"c1", "--seen", seen1(), "--route", "--title", "Rework paging", "--target", "later", "--at", "x.go:1"}, tc.reason...)
			if _, err := resolve(t, "", args...); err != nil {
				t.Fatal(err)
			}
			spec, err := phase.LoadSpec(phaseFile(dir, "spec.toml"))
			if err != nil {
				t.Fatal(err)
			}
			if len(spec.Deferred) != 1 || spec.Deferred[0].Why != tc.wantWhy {
				t.Fatalf("spec deferred = %+v, want one item whose why is %q", spec.Deferred, tc.wantWhy)
			}
			if tc.name == "reason" {
				if r := loadTriage(t, dir).Resolution; len(r) != 1 || r[0].Reason != "wrong layer" {
					t.Errorf("record = %+v, want the route's reason recorded", r)
				}
			}
		})
	}
}

// refusals are invocations refused before anything is written, with the text
// each refusal must name.
var verdictRefusals = map[string][]string{
	"no verdict":              {"--at", "x.go:1"},
	"accept and reject":       {"--accept", "--title", "t", "--reject", "--reason", "r", "--at", "x.go:1"},
	"accept and route":        {"--accept", "--route", "--title", "t", "--target", "later", "--at", "x.go:1"},
	"reject and route":        {"--reject", "--route", "--reason", "r", "--title", "t", "--target", "later", "--at", "x.go:1"},
	"all three":               {"--accept", "--reject", "--route", "--title", "t", "--reason", "r", "--target", "later", "--at", "x.go:1"},
	"accept without title":    {"--accept", "--at", "x.go:1"},
	"reject without reason":   {"--reject", "--at", "x.go:1"},
	"route without target":    {"--route", "--title", "t", "--at", "x.go:1"},
	"no evidence (accept)":    {"--accept", "--title", "t"},
	"no evidence (reject)":    {"--reject", "--reason", "r"},
	"no evidence (route)":     {"--route", "--title", "t", "--target", "later"},
	"at without a line":       {"--reject", "--reason", "r", "--at", "x.go"},
	"at line zero":            {"--reject", "--reason", "r", "--at", "x.go:0"},
	"at escaping":             {"--reject", "--reason", "r", "--at", "../x:1"},
	"cmd without output-file": {"--reject", "--reason", "r", "--cmd", "go test"},
	"at and cmd":              {"--reject", "--reason", "r", "--at", "x.go:1", "--cmd", "go test", "--output-file", "-"},
}

func TestPRResolveVerdictMatrix(t *testing.T) {
	dir, s, _ := resolveRepo(t)
	before := drossDigest(t, dir)
	for name, flags := range verdictRefusals {
		args := append([]string{"c1", "--seen", seen1()}, flags...)
		_, err := resolve(t, "ok\n", args...)
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		switch name {
		case "no verdict", "accept and reject", "accept and route", "reject and route", "all three", "accept without title", "reject without reason", "route without target":
			if !strings.Contains(err.Error(), "--accept --title") {
				t.Errorf("%s: %v does not name the working form", name, err)
			}
		case "cmd without output-file":
			if !strings.Contains(err.Error(), "--output-file") {
				t.Errorf("%s: %v does not name --output-file", name, err)
			}
		}
	}
	if drossDigest(t, dir) != before {
		t.Error("a refused resolve changed .dross")
	}
	if s.headCalls != 0 || s.commentCalls != 0 {
		t.Errorf("a malformed invocation reached the forge: %d head, %d comment calls", s.headCalls, s.commentCalls)
	}
}

func TestPRResolveRefusesNoEvidence(t *testing.T) {
	dir, _, _ := resolveRepo(t)
	before := drossDigest(t, dir)
	for _, verdict := range [][]string{{"--accept", "--title", "t"}, {"--reject", "--reason", "r"}, {"--route", "--title", "t", "--target", "later"}} {
		_, err := resolve(t, "", append([]string{"c1", "--seen", seen1()}, verdict...)...)
		if err == nil || !strings.Contains(err.Error(), "is required") {
			t.Errorf("%v with no evidence: %v", verdict, err)
		}
	}
	for _, at := range []string{"x.go", "x.go:0", "../x:1"} {
		_, err := resolve(t, "", "c1", "--seen", seen1(), "--reject", "--reason", "r", "--at", at)
		if err == nil || !strings.Contains(err.Error(), "--at") || !strings.Contains(err.Error(), at) {
			t.Errorf("--at %s: %v, want an evidence refusal naming it", at, err)
		}
	}
	if _, err := resolve(t, "ok\n", "c1", "--seen", seen1(), "--reject", "--reason", "r", "--cmd", "go test"); err == nil || !strings.Contains(err.Error(), "--output-file") {
		t.Errorf("--cmd without --output-file: %v", err)
	}
	if drossDigest(t, dir) != before {
		t.Error("a refused resolve changed .dross")
	}
}

func TestPRResolveNeverRunsCmd(t *testing.T) {
	dir, _, _ := resolveRepo(t)
	if _, err := resolve(t, "ok\n", "c1", "--seen", seen1(), "--reject", "--reason", "covered", "--cmd", "touch pwned", "--output-file", "-"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(dir, "pwned"), "pwned"} {
		if _, err := os.Stat(p); err == nil {
			t.Fatalf("--cmd ran: %s exists", p)
		}
	}
	r := loadTriage(t, dir).Resolution
	if len(r) != 1 || r[0].Cmd != "touch pwned" || r[0].OutputBytes != 3 {
		t.Errorf("record = %+v", r)
	}
}

// TestPRResolveSeenToken: the seen=<hex> token `dross pr comments` prints is
// taken whole, with surrounding space, as well as bare.
func TestPRResolveSeenToken(t *testing.T) {
	dir, _, _ := resolveRepo(t)
	if _, err := resolve(t, "", "c1", "--seen", " seen="+seen1()+" ", "--reject", "--reason", "bounded", "--at", "x.go:1"); err != nil {
		t.Fatalf("the printed seen= token: %v", err)
	}
	if r := loadTriage(t, dir).Resolution; len(r) != 1 || r[0].Digest != seen1() {
		t.Errorf("record = %+v", r)
	}
}

func TestPRResolveSeenDigest(t *testing.T) {
	dir, _, _ := resolveRepo(t)
	before := drossDigest(t, dir)
	_, err := resolve(t, "", "c1", "--seen", prtriage.Digest("an older body"), "--reject", "--reason", "r", "--at", "x.go:1")
	if err == nil || !strings.Contains(err.Error(), "changed since listed") {
		t.Errorf("a stale --seen: %v", err)
	}
	if drossDigest(t, dir) != before {
		t.Error("a stale --seen wrote something")
	}
}

func TestPRResolveOncePerComment(t *testing.T) {
	dir, s, _ := resolveRepo(t)
	if _, err := resolve(t, "", "c1", "--seen", seen1(), "--accept", "--title", "first", "--at", "x.go:1"); err != nil {
		t.Fatal(err)
	}
	_, err := resolve(t, "", "c1", "--seen", seen1(), "--accept", "--title", "again", "--at", "x.go:1")
	if err == nil || !strings.Contains(err.Error(), "accept") {
		t.Errorf("re-resolving an unchanged comment: %v, want a refusal naming its verdict", err)
	}
	plan, _ := phase.LoadPlan(phaseFile(dir, "plan.toml"))
	if len(plan.Task) != 2 {
		t.Errorf("the refused re-resolve added a task: %d tasks", len(plan.Task))
	}

	s.comments[0].Body = "why loop forever? (edited)"
	if _, err := resolve(t, "", "c1", "--seen", prtriage.Digest(s.comments[0].Body), "--reject", "--reason", "bounded", "--at", "x.go:1"); err != nil {
		t.Fatalf("resolving the edited comment: %v", err)
	}
	r := loadTriage(t, dir).Resolution
	if len(r) != 1 || r[0].Verdict != prtriage.VerdictReject {
		t.Errorf("record = %+v, want one entry, replaced", r)
	}
	plan, _ = phase.LoadPlan(phaseFile(dir, "plan.toml"))
	if len(plan.Task) != 2 || plan.Task[1].ID != "t-2" {
		t.Error("the earlier accept's task did not stay")
	}
}

func TestPRResolveLookup(t *testing.T) {
	dir, s, _ := resolveRepo(t)
	review := "## /dross-review — phase x\n\n### Security\n- **FLAG** — `x.go:1` — one\n- **FLAG** — `x.go:1` — two\n\n---\n*Posted*\n"
	s.comments = append(s.comments, prComment("123", ship.CommentConversation, "7", "rivil", ship.AuthorHuman, review))
	before := drossDigest(t, dir)
	for id, want := range map[string]string{"c9": "not found", "c1;x": "invalid", "c123": "c123#1"} {
		_, err := resolve(t, "", id, "--seen", seen1(), "--reject", "--reason", "r", "--at", "x.go:1")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("id %s: %v, want a refusal naming %q", id, err, want)
		}
	}
	s.head.Open = false
	if _, err := resolve(t, "", "c1", "--seen", seen1(), "--reject", "--reason", "r", "--at", "x.go:1"); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Errorf("a closed PR: %v", err)
	}
	if drossDigest(t, dir) != before {
		t.Error("a refused lookup wrote something")
	}
	s.head.Open = true
	items := prtriage.Items(s.comments, prSelf)
	var seen string
	for _, it := range items {
		if it.ID == "c123#2" {
			seen = it.Digest
		}
	}
	if _, err := resolve(t, "", "c123#2", "--seen", seen, "--reject", "--reason", "r", "--at", "x.go:1"); err != nil {
		t.Errorf("a finding id: %v", err)
	}
}

func TestPRResolveWrongBranch(t *testing.T) {
	dir, _, _ := resolveRepo(t)
	mustGit(t, dir, "checkout", "-q", "main")
	before := drossDigest(t, dir)
	_, err := resolve(t, "", "c1", "--seen", seen1(), "--reject", "--reason", "r", "--at", "x.go:1")
	if err == nil || !strings.Contains(err.Error(), "dross phase checkout x") {
		t.Errorf("on main: %v", err)
	}
	if drossDigest(t, dir) != before {
		t.Error("an off-branch resolve wrote something")
	}
}

func TestPRResolveRollsBackOnRecordFailure(t *testing.T) {
	stale := func(t *testing.T) {
		prev := beforeTriageSave
		beforeTriageSave = func(recPath string) { mustWrite(t, recPath, "# changed under us\n") }
		t.Cleanup(func() { beforeTriageSave = prev })
	}
	t.Run("accept", func(t *testing.T) {
		dir, _, _ := resolveRepo(t)
		stale(t)
		plan := readOrEmpty(t, phaseFile(dir, "plan.toml"))
		if _, err := resolve(t, "", "c1", "--seen", seen1(), "--accept", "--title", "t", "--at", "x.go:1"); err == nil {
			t.Fatal("the record save did not fail")
		}
		if readOrEmpty(t, phaseFile(dir, "plan.toml")) != plan {
			t.Error("plan.toml was not restored byte for byte")
		}
	})
	t.Run("route", func(t *testing.T) {
		dir, _, mirrors := resolveRepo(t)
		stale(t)
		spec := readOrEmpty(t, phaseFile(dir, "spec.toml"))
		if _, err := resolve(t, "", "c1", "--seen", seen1(), "--route", "--title", "t", "--target", "later", "--at", "x.go:1"); err == nil {
			t.Fatal("the record save did not fail")
		}
		if readOrEmpty(t, phaseFile(dir, "spec.toml")) != spec {
			t.Error("spec.toml was not restored byte for byte")
		}
		if *mirrors != 0 {
			t.Errorf("the board mirror ran %d times for a route whose record did not save", *mirrors)
		}
	})
}

func TestPRResolveNeverPersistsBody(t *testing.T) {
	dir, s, _ := resolveRepo(t)
	const sentinel = "BODY-SENTINEL-4d2b"
	s.comments[0].Body = "why " + sentinel
	if _, err := resolve(t, "", "c1", "--seen", prtriage.Digest(s.comments[0].Body), "--accept", "--title", "t", "--at", "x.go:1"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{prtriage.File, "plan.toml", "spec.toml"} {
		if strings.Contains(readOrEmpty(t, phaseFile(dir, name)), sentinel) {
			t.Errorf("the comment body reached %s", name)
		}
	}
}

func TestPRResolveRedactsPersisted(t *testing.T) {
	tok := "ghp_" + strings.Repeat("Rs0v", 9)
	for name, flags := range map[string][]string{
		"reason":        {"--reject", "--reason", "leaked " + tok, "--at", "x.go:1"},
		"title":         {"--accept", "--title", "fix " + tok, "--at", "x.go:1"},
		"description":   {"--accept", "--title", "t", "--description", "see " + tok, "--at", "x.go:1"},
		"test-contract": {"--accept", "--title", "t", "--test-contract", "fails when " + tok + " leaks", "--at", "x.go:1"},
		"cmd":           {"--reject", "--reason", "r", "--cmd", "curl -H 'x: " + tok + "'", "--output-file", "-"},
	} {
		t.Run(name, func(t *testing.T) {
			dir, _, _ := resolveRepo(t)
			_, err := resolve(t, "ok\n", append([]string{"c1", "--seen", seen1()}, flags...)...)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range []string{prtriage.File, "plan.toml", "spec.toml"} {
				if strings.Contains(readOrEmpty(t, phaseFile(dir, f)), tok) {
					t.Errorf("the token reached %s", f)
				}
			}
			if err := runCmd(t, Validate()); err != nil {
				t.Errorf("dross validate: %v", err)
			}
		})
	}
}

func TestPRResolveErrorsClassified(t *testing.T) {
	dir, s, _ := resolveRepo(t)
	var errs []error
	for _, flags := range verdictRefusals {
		_, err := resolve(t, "ok\n", append([]string{"c1", "--seen", seen1()}, flags...)...)
		errs = append(errs, err)
	}
	for _, args := range [][]string{
		{"c1", "--seen", prtriage.Digest("older"), "--reject", "--reason", "r", "--at", "x.go:1"},
		{"c9", "--seen", seen1(), "--reject", "--reason", "r", "--at", "x.go:1"},
		{"c1;x", "--seen", seen1(), "--reject", "--reason", "r", "--at", "x.go:1"},
		{"c1", "--reject", "--reason", "r", "--at", "x.go:1"},
		{"c1", "--seen", seen1(), "--route", "--title", "t", "--target", "nope", "--at", "x.go:1"},
		{"c1", "--seen", seen1(), "--route", "--title", "t", "--target", "done", "--at", "x.go:1"},
	} {
		_, err := resolve(t, "", args...)
		errs = append(errs, err)
	}
	if _, err := resolve(t, "", "c1", "--seen", seen1(), "--reject", "--reason", "r", "--at", "x.go:1"); err != nil {
		t.Fatal(err)
	}
	_, err := resolve(t, "", "c1", "--seen", seen1(), "--reject", "--reason", "r", "--at", "x.go:1")
	errs = append(errs, err)
	s.head.Open = false
	_, err = resolve(t, "", "c1", "--seen", seen1(), "--reject", "--reason", "r", "--at", "x.go:1")
	errs = append(errs, err)
	s.head.Open = true
	mustGit(t, dir, "checkout", "-q", "main")
	_, err = resolve(t, "", "c1", "--seen", seen1(), "--reject", "--reason", "r", "--at", "x.go:1")
	errs = append(errs, err)
	for _, e := range errs {
		if e == nil {
			t.Error("a refusal case was accepted")
			continue
		}
		if b := telemetry.ClassifyError(e); b == "other" {
			t.Errorf("%q lands in the telemetry \"other\" bucket", e)
		}
	}
}

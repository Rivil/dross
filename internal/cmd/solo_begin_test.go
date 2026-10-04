package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/gatestate"
)

const soloBeginPlan = `[phase]
id = "p"
[[task]]
id = "t-1"
wave = 1
title = "one"
files = ["a.go"]
covers = ["c-1"]
`

// soloBeginFixture is a committed dross repo with phase p planned.
func soloBeginFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitInit(t, dir, "https://forge.example/me/p.git")
	chdir(t, dir)
	scaffoldPhaseWithPlan(t, "p", soloBeginPlan)
	mustWrite(t, filepath.Join(dir, "a.go"), "package a\n")
	gitCommit(t, dir, "init")
	return dir
}

func loadQuick(t *testing.T, dir string) *gatestate.Quick {
	t.Helper()
	q, err := gatestate.LoadQuick(dir)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func TestQuickBegin(t *testing.T) {
	dir := soloBeginFixture(t)
	head := mustGit(t, dir, "rev-parse", "HEAD")

	if err := runCmd(t, Quick(), "begin", "--solo", "fix x"); err != nil {
		t.Fatal(err)
	}
	q := loadQuick(t, dir)
	if q == nil || q.Mode != "solo" || q.Description != "fix x" || q.Head != head || q.At.IsZero() {
		t.Fatalf("quick begin --solo recorded %+v, want solo / %q / %s", q, "fix x", head)
	}
	desc := "  keep   the spacing\nand lines  "
	if err := runCmd(t, Quick(), "begin", desc); err != nil {
		t.Fatal(err)
	}
	if q := loadQuick(t, dir); q.Mode != "pair" || q.Description != desc {
		t.Fatalf("quick begin recorded %+v, want pair with the description verbatim", q)
	}
	for _, empty := range []string{"", "   ", "\n\t"} {
		if err := runCmd(t, Quick(), "begin", "--solo", empty); err == nil {
			t.Errorf("an empty description %q was accepted", empty)
		}
	}
}

func TestSoloBeginNeedsReviewer(t *testing.T) {
	cases := []struct {
		name   string
		break_ func(t *testing.T, dir string)
		want   string
	}{
		{"missing", func(t *testing.T, dir string) {
			if err := os.Remove(filepath.Join(userAgentsDir(""), reviewerFile)); err != nil {
				t.Fatal(err)
			}
		}, "dross install"},
		{"stale", func(t *testing.T, dir string) {
			p := filepath.Join(userAgentsDir(""), reviewerFile)
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, append(b, '\n'), 0o644); err != nil {
				t.Fatal(err)
			}
		}, "dross install"},
		{"shadowed", func(t *testing.T, dir string) {
			mustWrite(t, filepath.Join(dir, ".claude", "agents", reviewerFile), "---\nname: dross-task-reviewer\n---\n")
		}, filepath.Join(".claude", "agents", reviewerFile)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := soloBeginFixture(t)
			c.break_(t, dir)
			for _, args := range [][]string{{"begin", "p", "--solo"}} {
				err := runCmd(t, Execute(), args...)
				if err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), c.name) {
					t.Errorf("execute %v with the reviewer %s = %v, want a refusal naming %q", args, c.name, err, c.want)
				}
			}
			if err := runCmd(t, Quick(), "begin", "--solo", "x"); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("quick begin --solo with the reviewer %s = %v, want a refusal naming %q", c.name, err, c.want)
			}
			if ex, _ := gatestate.LoadExecute(dir); ex != nil {
				t.Errorf("a refused solo begin recorded %+v", ex)
			}
			if err := runCmd(t, Execute(), "begin", "p"); err != nil {
				t.Errorf("a pair begin refused with the reviewer %s: %v", c.name, err)
			}
			if err := runCmd(t, Quick(), "begin", "x"); err != nil {
				t.Errorf("a pair quick refused with the reviewer %s: %v", c.name, err)
			}
		})
	}
}

func TestQuickMarkerLifetime(t *testing.T) {
	dir := soloBeginFixture(t)
	if err := runCmd(t, Quick(), "begin", "--solo", "x"); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Execute(), "begin", "p"); err != nil {
		t.Fatal(err)
	}
	if q := loadQuick(t, dir); q != nil {
		t.Fatalf("execute begin left an earlier quick marker: %+v", q)
	}
	if err := runCmd(t, Quick(), "begin", "x"); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Execute(), "begin", "p", "--solo"); err != nil {
		t.Fatal(err)
	}
	if q := loadQuick(t, dir); q != nil {
		t.Fatalf("execute begin --solo left an earlier quick marker: %+v", q)
	}
	if err := runCmd(t, Quick(), "end"); err != nil {
		t.Fatalf("quick end with no marker: %v", err)
	}
	if err := runCmd(t, Quick(), "begin", "x"); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Quick(), "end"); err != nil {
		t.Fatal(err)
	}
	if q := loadQuick(t, dir); q != nil {
		t.Fatalf("quick end left the marker: %+v", q)
	}
}

func fileSum(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestQuickNoClobber(t *testing.T) {
	dir := soloBeginFixture(t)
	if err := runCmd(t, State(), "set", "current_phase", "p"); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(dir, ".dross", "state.json")
	plan := filepath.Join(dir, ".dross", "phases", "p", "plan.toml")
	before := fileSum(t, state) + fileSum(t, plan)
	if err := runCmd(t, Quick(), "begin", "--solo", "x"); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Quick(), "end"); err != nil {
		t.Fatal(err)
	}
	if after := fileSum(t, state) + fileSum(t, plan); after != before {
		t.Fatal("quick begin/end changed state.json or plan.toml")
	}
}

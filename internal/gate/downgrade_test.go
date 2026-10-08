package gate

import (
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/gatestate"
	"github.com/Rivil/dross/internal/gitrun"
)

// dirtySolo is a solo repo with t-1 in progress and a.go changed.
func dirtySolo(t *testing.T) string {
	t.Helper()
	dir := soloRepo(t, "in_progress")
	put(t, dir, "a.go", "package a // half done\n")
	return dir
}

// dirtyQuick is a solo quick armed at HEAD with a.go changed.
func dirtyQuick(t *testing.T) string {
	t.Helper()
	dir := soloRepo(t, "")
	if err := gatestate.SaveQuick(dir, gatestate.Quick{Mode: "solo", Description: "x", Head: headSHA(t, dir), At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	put(t, dir, "a.go", "package a // quick, half done\n")
	return dir
}

func TestSoloDowngradeGuard(t *testing.T) {
	for _, line := range []string{"dross execute begin p", `dross quick begin "x"`, "dross quick end"} {
		if res := soloCheck(t, line, dirtySolo(t)); res.Allowed() || !strings.Contains(res.Text(), "without its solo review") {
			t.Errorf("armed + a.go dirty: %q was allowed (%q)", line, res.Text())
		}
	}

	clean := soloRepo(t, "in_progress")
	put(t, clean, ".dross/notes.md", "bookkeeping only\n")
	for _, line := range []string{"dross execute begin p", `dross quick begin "x"`, "dross quick end"} {
		if res := soloCheck(t, line, clean); !res.Allowed() {
			t.Errorf("armed + only .dross/ dirty: %q refused: %q", line, res.Text())
		}
	}

	if res := soloCheck(t, "dross execute begin p --solo", dirtySolo(t)); !res.Allowed() {
		t.Errorf("a solo execute re-begin over its own armed task was refused: %q", res.Text())
	}
	res := soloCheck(t, "dross execute begin p --solo", dirtyQuick(t))
	if res.Allowed() || !strings.Contains(res.Text(), "the solo quick") {
		t.Errorf("a solo execute begin over a dirty solo quick: %q, want a refusal naming the quick", res.Text())
	}
}

func TestSoloDisarmGuard(t *testing.T) {
	for _, line := range []string{
		"dross task status p t-1 pending",
		"dross task status p t-1 done",
		`dross quick begin --solo "x"`,
	} {
		if res := soloCheck(t, line, dirtySolo(t)); res.Allowed() {
			t.Errorf("armed + dirty: %q disarmed the solo task", line)
		}
	}
	for _, line := range []string{
		"dross task status p t-1 failed --reason \"red suite\"",
		"dross task status p t-2 done",
		"dross task status other t-1 pending",
	} {
		if res := soloCheck(t, line, dirtySolo(t)); !res.Allowed() {
			t.Errorf("%q does not disarm t-1 but was refused: %q", line, res.Text())
		}
	}
	if res := soloCheck(t, `dross quick begin --solo "again"`, dirtyQuick(t)); res.Allowed() {
		t.Error("re-beginning a dirty solo quick (a fresh attempt sheds its review) was allowed")
	}
}

func TestDowngradeGuardSilent(t *testing.T) {
	var argv [][]string
	prev := gitrun.ArgvRecorder
	gitrun.ArgvRecorder = func(a []string) { argv = append(argv, a) }
	t.Cleanup(func() { gitrun.ArgvRecorder = prev })

	pair := pairRepo(t, "in_progress")
	setMode(t, pair, "pair")
	put(t, pair, "a.go", "package a // dirty\n")
	argv = nil
	if res := soloCheck(t, "dross execute begin p", pair); !res.Allowed() {
		t.Errorf("nothing armed: refused %q", res.Text())
	}
	if got := treefpArgv(argv); len(got) != 0 {
		t.Errorf("an unarmed scope call spawned treefp git: %q", got)
	}

	dir := dirtySolo(t)
	argv = nil
	if res := soloCheck(t, "ls -la && echo dross quick end", dir); !res.Allowed() {
		t.Errorf("a non-dross line was refused: %q", res.Text())
	}
	if len(argv) != 0 {
		t.Errorf("a non-dross line spawned git: %q", argv)
	}
}

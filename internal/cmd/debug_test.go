package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/debugsession"
	"github.com/Rivil/dross/internal/rules"
	"github.com/Rivil/dross/internal/telemetry"
)

// debugRun runs `dross debug <args>` on a fresh command tree — close's flag
// variables live in its closure, so a reused tree would carry them over.
func debugRun(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var err error
	out := captureStdout(t, func() { err = runCmd(t, Debug(), args...) })
	return out, err
}

// debugSessionFile writes a session for slug whose header says status and
// whose sections hold bodies, and returns its path.
func debugSessionFile(t *testing.T, dir, slug, status string, bodies map[string]string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("# Debug: " + slug + "\n\nstatus: " + status + "\nopened: 2026-10-05T10:00:00Z\n")
	for _, h := range debugsession.Headings() {
		b.WriteString("\n## " + h + "\n" + bodies[h])
	}
	p := debugsession.Path(dir, slug)
	mustWrite(t, p, b.String())
	return p
}

func debugBytes(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDebugNew(t *testing.T) {
	dir := initWithGit(t)
	out, err := debugRun(t, "new", "flaky-login")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if !strings.Contains(out, ".dross/debug/flaky-login.md") || !strings.Contains(out, "/dross-debug flaky-login") {
		t.Errorf("new printed %q, want the path and the /dross-debug command", out)
	}
	body := string(debugBytes(t, debugsession.Path(dir, "flaky-login")))
	if !strings.Contains(body, "\nstatus: open\n") {
		t.Errorf("the session is not marked open:\n%s", body)
	}
	for _, h := range debugsession.Headings() {
		if !strings.Contains(body, "\n## "+h+"\n") {
			t.Errorf("the session lacks ## %s", h)
		}
	}

	if _, err := debugRun(t, "new", "flaky-login"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("a second new: %v, want already exists", err)
	}
	if _, err := debugRun(t, "new", "../x"); err == nil || !strings.Contains(err.Error(), "invalid session name") {
		t.Errorf("new ../x: %v, want an invalid-name refusal", err)
	}
	for _, p := range []string{filepath.Join(dir, ".dross", "x.md"), filepath.Join(dir, "x.md")} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("new ../x wrote %s", p)
		}
	}
	des, err := os.ReadDir(filepath.Join(dir, ".dross", "debug"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, d := range des {
		names = append(names, d.Name())
	}
	if want := []string{".gitignore", "flaky-login.md"}; !reflect.DeepEqual(names, want) {
		t.Errorf(".dross/debug holds %q, want %q", names, want)
	}
}

func TestDebugSessionNoChoreRide(t *testing.T) {
	dir := initWithGit(t)
	head := mustGit(t, dir, "rev-parse", "HEAD")
	if _, err := debugRun(t, "new", "x"); err != nil {
		t.Fatal(err)
	}
	committed, err := autoCommitDrossDirt(dir, "ship")
	if err != nil || committed {
		t.Fatalf("autoCommitDrossDirt = %v, %v; want nothing to commit", committed, err)
	}
	if got := mustGit(t, dir, "rev-parse", "HEAD"); got != head {
		t.Fatalf("HEAD moved %s -> %s", head, got)
	}
}

func TestDebugList(t *testing.T) {
	t.Run("no sessions", func(t *testing.T) {
		initWithGit(t)
		out, err := debugRun(t, "list")
		if err != nil || !strings.Contains(out, "no debug sessions") {
			t.Fatalf("list = %q, %v", out, err)
		}
	})

	// ModTime comes back in time.Local; pin it off UTC so a dropped .UTC()
	// shows on a UTC host too.
	local := time.Local
	time.Local = time.FixedZone("UTC-3", -3*3600)
	t.Cleanup(func() { time.Local = local })

	dir := initWithGit(t)
	failed := func(n int) string { return strings.Repeat("- [failed] tried it\n", n) }
	fixtures := []struct {
		slug, status string
		bodies       map[string]string
		state        string
		hyp, fail    int
	}{
		{"a-open", "open", map[string]string{debugsession.HeadingHypotheses: "- the clock\n- the cache\n", debugsession.HeadingFixAttempts: failed(1)}, "open", 2, 1},
		{"b-replan", "open", map[string]string{debugsession.HeadingHypotheses: "- the clock\n", debugsession.HeadingFixAttempts: failed(3)}, "needs-replan", 1, 3},
		{"c-resolved", "resolved", map[string]string{debugsession.HeadingHypotheses: "- a\n- b\n- c\n", debugsession.HeadingFixAttempts: failed(4)}, "resolved", 3, 4},
		{"d-abandoned", "abandoned", nil, "abandoned", 0, 0},
	}
	zone := time.FixedZone("UTC+5", 5*3600)
	for i, f := range fixtures {
		p := debugSessionFile(t, dir, f.slug, f.status, f.bodies)
		mtime := time.Date(2026, 10, 1, 12+i, 0, 0, 0, zone)
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	broken := debugSessionFile(t, dir, "e-broken", "fixed", nil)
	old := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(broken, old, old); err != nil {
		t.Fatal(err)
	}

	out, err := debugRun(t, "list")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	rows := map[string][]string{}
	for _, l := range lines {
		if f := strings.Fields(l); len(f) > 0 && !strings.HasPrefix(l, " ") {
			rows[f[0]] = f
		}
	}
	for i, f := range fixtures {
		row := rows[f.slug]
		want := []string{f.slug, f.state, "hypotheses:", strconv.Itoa(f.hyp), "failed", "since", "re-plan:", strconv.Itoa(f.fail), "updated:",
			time.Date(2026, 10, 1, 7+i, 0, 0, 0, time.UTC).Format(time.RFC3339)}
		if !reflect.DeepEqual(row, want) {
			t.Errorf("%s row = %q, want %q", f.slug, row, want)
		}
	}
	// Newest first: d-abandoned was touched last, e-broken first.
	if !strings.HasPrefix(lines[0], "d-abandoned ") {
		t.Errorf("first row %q, want the newest session", lines[0])
	}
	last := lines[len(lines)-1]
	if !strings.HasPrefix(last, "    problem: ") || !strings.Contains(last, `header status "fixed"`) {
		t.Errorf("the malformed session's Problem is not listed under it: %q", last)
	}
	if rows["e-broken"][1] != "open" {
		t.Errorf("a malformed session reads %q, want open", rows["e-broken"][1])
	}
}

func TestDebugCloseFixedRefusal(t *testing.T) {
	dir := initWithGit(t)
	p := debugSessionFile(t, dir, "x", "open", map[string]string{debugsession.HeadingResolution: "- SENTINEL-7f3a repro gone\n"})
	before := debugBytes(t, p)
	_, err := debugRun(t, "close", "x", "--fixed")
	if err == nil {
		t.Fatal("close --fixed with one signal and no Prevention succeeded")
	}
	for _, want := range []string{"Resolution lists 1 of 2 signals", "Prevention is empty"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not name %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "SENTINEL-7f3a") {
		t.Errorf("the refusal quotes the session: %q", err)
	}
	if !bytes.Equal(debugBytes(t, p), before) {
		t.Error("a refused close changed the session")
	}
}

func TestDebugCloseFlagMatrix(t *testing.T) {
	dir := initWithGit(t)
	p := debugSessionFile(t, dir, "x", "open", nil)
	before := debugBytes(t, p)
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"--fixed", "--abandoned", "--reason", "r"}, "dross debug close x --fixed"},
		{nil, "dross debug close x --abandoned --reason"},
		{[]string{"--fixed", "--reason", "r"}, "dross debug close x --fixed"},
		{[]string{"--abandoned"}, `dross debug close x --abandoned --reason "<why>"`},
		{[]string{"--abandoned", "--reason", "  "}, `dross debug close x --abandoned --reason "<why>"`},
	}
	for _, c := range cases {
		_, err := debugRun(t, append([]string{"close", "x"}, c.args...)...)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("close x %q: %v, want a refusal naming %q", c.args, err, c.want)
		}
		if !bytes.Equal(debugBytes(t, p), before) {
			t.Errorf("close x %q changed the session", c.args)
		}
	}
}

func TestDebugCloseAbandoned(t *testing.T) {
	dir := initWithGit(t)
	p := debugSessionFile(t, dir, "x", "open", map[string]string{debugsession.HeadingPrevention: "Always X\n"})
	out, err := debugRun(t, "close", "x", "--abandoned", "--reason", "gave up")
	if err != nil {
		t.Fatalf("close --abandoned: %v", err)
	}
	s := debugsession.Parse(debugBytes(t, p))
	if s.State() != debugsession.StateAbandoned {
		t.Fatalf("the session reads %s, want abandoned", s.State())
	}
	if !bytes.Contains(debugBytes(t, p), []byte("\nreason: gave up\n")) {
		t.Error("the abandon reason is not in the header")
	}
	if strings.Contains(out, "dross rule add") {
		t.Errorf("an abandoned close printed a rule line: %q", out)
	}
}

func TestDebugCloseFixedPrintsNeverAdds(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX /bin/sh")
	}
	globalDir := ruleCovFakeHome(t)
	dir := initWithGit(t)
	p := debugSessionFile(t, dir, "x", "open", map[string]string{
		debugsession.HeadingResolution: "- repro gone\n- suite green\n",
		debugsession.HeadingPrevention: "<!-- the rule -->\n--force is never the fix\n- Always pin the clock in tests\n",
	})
	projectRules := filepath.Join(dir, ".dross", rules.File)
	globalRules := filepath.Join(globalDir, rules.File)
	snapshot := func(path string) []byte {
		b, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	projBefore, globBefore := snapshot(projectRules), snapshot(globalRules)

	out, err := debugRun(t, "close", "x", "--fixed")
	if err != nil {
		t.Fatalf("close --fixed: %v", err)
	}
	if debugsession.Parse(debugBytes(t, p)).State() != debugsession.StateResolved {
		t.Fatal("the session is not resolved")
	}
	var line string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "dross rule add ") {
			if line != "" {
				t.Fatalf("more than one rule line in %q", out)
			}
			line = strings.TrimSpace(l)
		}
	}
	if line == "" {
		t.Fatalf("no rule line in %q", out)
	}
	if !bytes.Equal(snapshot(projectRules), projBefore) || !bytes.Equal(snapshot(globalRules), globBefore) {
		t.Fatal("close --fixed changed a rules.toml")
	}

	// The printed line, split by a real shell, is a working `dross rule add`.
	sh := exec.Command("/bin/sh", "-c", `dross() { for a in "$@"; do printf '%s\n' "$a"; done; }; `+line)
	argvOut, err := sh.Output()
	if err != nil {
		t.Fatalf("sh: %v", err)
	}
	argv := strings.Split(strings.TrimSuffix(string(argvOut), "\n"), "\n")
	want := "--force is never the fix; Always pin the clock in tests"
	if !reflect.DeepEqual(argv, []string{"rule", "add", "--", want}) {
		t.Fatalf("the shell read %q", argv)
	}
	initWithGit(t)
	if err := runCmd(t, Rule(), argv[1:]...); err != nil {
		t.Fatalf("rule %q: %v", argv[1:], err)
	}
	root, err := FindRoot()
	if err != nil {
		t.Fatal(err)
	}
	set, err := rules.LoadFile(filepath.Join(root, rules.File))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(set.Rules); n == 0 || set.Rules[n-1].Text != want {
		t.Fatalf("stored rules %+v, want the last to read %q", set.Rules, want)
	}
}

func TestDebugCloseUnknownAndClosed(t *testing.T) {
	dir := initWithGit(t)
	if _, err := debugRun(t, "close", "nope", "--fixed"); err == nil || !strings.Contains(err.Error(), "dross debug list") {
		t.Errorf("unknown slug: %v, want a pointer at dross debug list", err)
	}
	if _, err := debugRun(t, "close", "../x", "--fixed"); err == nil || !strings.Contains(err.Error(), "invalid session name") {
		t.Errorf("bad slug: %v", err)
	}
	p := debugSessionFile(t, dir, "done", "resolved", map[string]string{
		debugsession.HeadingResolution: "- a\n- b\n", debugsession.HeadingPrevention: "Always X\n",
	})
	before := debugBytes(t, p)
	for _, args := range [][]string{{"--fixed"}, {"--abandoned", "--reason", "r"}} {
		_, err := debugRun(t, append([]string{"close", "done"}, args...)...)
		if err == nil || !strings.Contains(err.Error(), "already resolved") {
			t.Errorf("closing a resolved session %q: %v", args, err)
		}
	}
	if !bytes.Equal(debugBytes(t, p), before) {
		t.Error("a refused close changed a resolved session")
	}
}

func TestDebugCloseMalformed(t *testing.T) {
	dir := initWithGit(t)
	p := debugSessionFile(t, dir, "x", "open", map[string]string{
		debugsession.HeadingResolution: "- repro gone\n",
		debugsession.HeadingPrevention: "Always X\n\n## Resolution\n- suite green\n",
	})
	before := debugBytes(t, p)
	_, err := debugRun(t, "close", "x", "--fixed")
	if err == nil || !strings.Contains(err.Error(), `duplicate "## Resolution" heading`) {
		t.Fatalf("close --fixed on a duplicated Resolution: %v", err)
	}
	if !bytes.Equal(debugBytes(t, p), before) {
		t.Fatal("a refused close changed the session")
	}
	if _, err := debugRun(t, "close", "x", "--abandoned", "--reason", "gave up"); err != nil {
		t.Fatalf("abandoning a malformed session: %v", err)
	}
	if debugsession.Parse(debugBytes(t, p)).State() != debugsession.StateAbandoned {
		t.Fatal("the malformed session was not abandoned")
	}
}

func TestDebugCloseFreshSessionRefused(t *testing.T) {
	dir := initWithGit(t)
	if _, err := debugRun(t, "new", "fresh"); err != nil {
		t.Fatal(err)
	}
	p := debugsession.Path(dir, "fresh")
	before := debugBytes(t, p)
	_, err := debugRun(t, "close", "fresh", "--fixed")
	if err == nil || !strings.Contains(err.Error(), "Resolution lists 0 of 2 signals") || !strings.Contains(err.Error(), "Prevention is empty") {
		t.Fatalf("close --fixed on a fresh session: %v", err)
	}
	if !bytes.Equal(debugBytes(t, p), before) {
		t.Fatal("a refused close changed the fresh session")
	}
}

func TestDebugErrorsClassified(t *testing.T) {
	dir := initWithGit(t)
	debugSessionFile(t, dir, "open-one", "open", nil)
	debugSessionFile(t, dir, "closed-one", "resolved", nil)
	cases := []struct {
		name string
		args []string
	}{
		{"invalid slug", []string{"new", "Bad Name"}},
		{"already exists", []string{"new", "open-one"}},
		{"unknown slug", []string{"close", "nope", "--fixed"}},
		{"gate gaps", []string{"close", "open-one", "--fixed"}},
		{"already closed", []string{"close", "closed-one", "--fixed"}},
		{"both flags", []string{"close", "open-one", "--fixed", "--abandoned"}},
		{"neither flag", []string{"close", "open-one"}},
		{"reason on fixed", []string{"close", "open-one", "--fixed", "--reason", "r"}},
		{"abandon without reason", []string{"close", "open-one", "--abandoned"}},
	}
	for _, c := range cases {
		_, err := debugRun(t, c.args...)
		if err == nil {
			t.Errorf("%s: no error", c.name)
			continue
		}
		if got := telemetry.ClassifyError(err); got == "other" {
			t.Errorf("%s: %q classifies as other", c.name, err)
		}
	}
}

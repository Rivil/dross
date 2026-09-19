package mutation

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/remote"
)

// fakeNode puts a shell script named `node` first on PATH. The script records
// its argv, cwd and stdin under dir and then runs body, so a test can assert
// what the resolver said to node without a real parser anywhere. Returns the
// recording directory.
func fakeNode(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > \"" + dir + "/argv\"\n" +
		"pwd > \"" + dir + "/cwd\"\n" +
		"/bin/cat > \"" + dir + "/stdin\"\n" +
		"echo 1 >> \"" + dir + "/calls\"\n" +
		body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "node"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	return dir
}

func readRecord(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return ""
	}
	return string(b)
}

// strykerWithSource is a Stryker whose ProjectRoot holds file with src.
func strykerWithSource(t *testing.T, file, src string) *Stryker {
	t.Helper()
	root := t.TempDir()
	p := filepath.Join(root, filepath.FromSlash(file))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return &Stryker{ProjectRoot: root, Workdir: "web"}
}

func TestEmbeddedScriptIsNonEmpty(t *testing.T) {
	for _, want := range []string{"@babel/parser", "constructs", "@stryker-mutator/instrumenter", "process.exit(2)"} {
		if !strings.Contains(astspanScript, want) {
			t.Errorf("astspanScript lacks %q", want)
		}
	}
}

// The request rides on stdin and argv is the two literals `node -`: nothing
// derived from the project reaches the command line.
func TestConstructsPassesTheRequestOnStdinNotArgv(t *testing.T) {
	rec := fakeNode(t, `echo '{"constructs":[{"start":10,"end":60,"kind":"FunctionDeclaration","name":"run"}]}'`)
	s := strykerWithSource(t, "web/src/a.ts", "export function run() {\n  return 1;\n}\n")
	if err := os.MkdirAll(s.workDir(), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := s.Constructs("web/src/a.ts")
	if err != nil {
		t.Fatalf("Constructs: %v", err)
	}
	want := []Construct{{Start: 10, End: 60, Kind: "FunctionDeclaration", Name: "run"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Constructs = %v, want %v", got, want)
	}
	if argv := readRecord(t, rec, "argv"); argv != "-\n" {
		t.Errorf("argv after node = %q, want exactly the stdin operand", argv)
	}
	if cwd := strings.TrimSpace(readRecord(t, rec, "cwd")); cwd != s.workDir() {
		// macOS reports /private/tmp for /tmp; compare resolved paths.
		want, _ := filepath.EvalSymlinks(s.workDir())
		if cwd != want {
			t.Errorf("cwd = %q, want the adapter's workDir %q", cwd, s.workDir())
		}
	}
	stdin := readRecord(t, rec, "stdin")
	for _, want := range []string{`"file":"web/src/a.ts"`, `export function run()`, "@babel/parser"} {
		if !strings.Contains(stdin, want) {
			t.Errorf("stdin lacks %q", want)
		}
	}
	if !strings.HasPrefix(stdin, "const __dross = {") {
		t.Errorf("stdin does not open with the request prelude: %q", stdin[:40])
	}
}

// A docker Prefix is honoured — the project's node_modules live inside the
// container — and the remote is NEVER used, whatever the adapter carries.
func TestConstructsHonoursPrefixAndNeverTheRemote(t *testing.T) {
	prev := launcherCommand
	launcherCommand = func(_ []string, _ string) *exec.Cmd {
		t.Fatal("the construct resolver went through the launcher")
		return nil
	}
	t.Cleanup(func() { launcherCommand = prev })

	var built []string
	prevBuild := strykerBuildCmd
	strykerBuildCmd = func(s *Stryker, args []string) *exec.Cmd {
		c := s.buildCmd(args)
		built = c.Args
		return c
	}
	t.Cleanup(func() { strykerBuildCmd = prevBuild })

	t.Setenv("PATH", t.TempDir()) // nothing on PATH: the local spawn fails fast
	s := strykerWithSource(t, "web/src/a.ts", "x\n")
	s.Prefix = "docker compose exec -T app"
	s.Remote = &remote.Target{Host: "helicon", Workdir: "/srv/x"}

	_, err := s.Constructs("web/src/a.ts")
	if !errors.Is(err, ErrASTUnavailable) {
		t.Fatalf("err = %v, want ErrASTUnavailable", err)
	}
	want := []string{"docker", "compose", "exec", "-T", "app", "node", "-"}
	if !reflect.DeepEqual(built, want) {
		t.Errorf("argv = %v, want %v", built, want)
	}
}

func TestConstructsClassifiesNodeMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	s := strykerWithSource(t, "web/src/a.ts", "x\n")
	_, err := s.Constructs("web/src/a.ts")
	if !errors.Is(err, ErrASTUnavailable) || !strings.Contains(err.Error(), "node not on PATH") {
		t.Fatalf("err = %v, want ErrASTUnavailable naming node", err)
	}
}

func TestConstructsClassifiesParseError(t *testing.T) {
	fakeNode(t, `echo '{"error":{"message":"Unexpected token","line":12,"column":4}}'; exit 2`)
	s := strykerWithSource(t, "web/src/a.ts", "x\n")
	_, err := s.Constructs("web/src/a.ts")
	if !errors.Is(err, ErrASTUnavailable) || !strings.Contains(err.Error(), "parse error at 12:4") {
		t.Fatalf("err = %v, want the parse position", err)
	}
	if strings.Contains(err.Error(), "Unexpected token") {
		t.Errorf("the parser's own message leaked into the persisted detail: %v", err)
	}
}

func TestConstructsClassifiesUnresolvableParser(t *testing.T) {
	fakeNode(t, `echo '{"error":{"message":"cannot resolve @babel/parser from /x"}}'; exit 2`)
	s := strykerWithSource(t, "web/src/a.ts", "x\n")
	_, err := s.Constructs("web/src/a.ts")
	if !errors.Is(err, ErrASTUnavailable) || !strings.Contains(err.Error(), "@babel/parser") {
		t.Fatalf("err = %v, want the unresolvable parser named", err)
	}
}

func TestConstructsClassifiesMalformedOutput(t *testing.T) {
	fakeNode(t, `echo nope`)
	s := strykerWithSource(t, "web/src/a.ts", "x\n")
	_, err := s.Constructs("web/src/a.ts")
	if !errors.Is(err, ErrASTUnavailable) || !strings.Contains(err.Error(), "malformed resolver output") {
		t.Fatalf("err = %v, want malformed resolver output", err)
	}
}

func TestConstructsClassifiesTimeout(t *testing.T) {
	prev := constructTimeout
	constructTimeout = 200 * time.Millisecond
	t.Cleanup(func() { constructTimeout = prev })
	// /bin/sleep by path: PATH holds only the fake node.
	fakeNode(t, `/bin/sleep 5; echo '{"constructs":[]}'`)
	s := strykerWithSource(t, "web/src/a.ts", "x\n")
	start := time.Now()
	_, err := s.Constructs("web/src/a.ts")
	if !errors.Is(err, ErrASTUnavailable) || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v, want a timeout", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("the timeout did not cut the spawn short")
	}
}

// Svelte is refused before any process exists: Babel cannot parse the
// markup, and a doomed spawn per run is a cost with no signal.
func TestSvelteIsUnavailableWithoutASpawn(t *testing.T) {
	rec := fakeNode(t, `echo '{"constructs":[]}'`)
	s := strykerWithSource(t, "web/src/App.svelte", "<script>let x = 1;</script>\n")
	_, err := s.Constructs("web/src/App.svelte")
	if !errors.Is(err, ErrASTUnavailable) || !strings.Contains(err.Error(), "no parser for .svelte") {
		t.Fatalf("err = %v, want the svelte refusal", err)
	}
	if calls := readRecord(t, rec, "calls"); calls != "" {
		t.Errorf("node was spawned %q times for a .svelte file", strings.TrimSpace(calls))
	}
}

// A file with no top-level nodes is an empty slice, not nil: the planner
// treats "resolved, nothing there" and "unresolved" differently.
func TestConstructsEmptyFileIsEmptyNotNil(t *testing.T) {
	fakeNode(t, `echo '{"constructs":[]}'`)
	s := strykerWithSource(t, "web/src/a.ts", "// nothing\n")
	got, err := s.Constructs("web/src/a.ts")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("Constructs = %#v, want an empty non-nil slice", got)
	}
}

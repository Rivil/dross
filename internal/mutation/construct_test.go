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
	for _, want := range []string{"@babel/parser", "svelte/compiler", "constructs", "@stryker-mutator/instrumenter", "process.exit(2)"} {
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
	s.Remote = helicon("/srv/x")

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

// The blanket .svelte refusal is gone; the name stays because tests_before.txt
// holds it. What is left of "unavailable" is a named cause: a compiler that does
// not resolve, a file the compiler refuses, and a file that cannot be read,
// which is still the one case with no spawn. The real compiler's half of this
// is TestUnresolvableParserIsUnavailableForSvelte.
func TestSvelteIsUnavailableWithoutASpawn(t *testing.T) {
	const file = "web/src/App.svelte"

	t.Run("compiler unresolvable", func(t *testing.T) {
		fakeNode(t, `echo '{"error":{"message":"cannot resolve svelte/compiler from /x"}}'; exit 2`)
		s := strykerWithSource(t, file, "<p>a</p>\n")
		_, err := s.Constructs(file)
		want := "cannot resolve svelte/compiler from " + s.workDir()
		if !errors.Is(err, ErrASTUnavailable) || !strings.Contains(err.Error(), want) {
			t.Fatalf("err = %v, want ErrASTUnavailable with %q", err, want)
		}
	})

	t.Run("malformed file", func(t *testing.T) {
		fakeNode(t, `echo '{"error":{"message":"attempted to close an element","line":4,"column":2}}'; exit 2`)
		s := strykerWithSource(t, file, "<div>\n")
		_, err := s.Constructs(file)
		if !errors.Is(err, ErrASTUnavailable) || !strings.Contains(err.Error(), "parse error at 4:2") {
			t.Fatalf("err = %v, want ErrASTUnavailable with the position", err)
		}
		if strings.Contains(err.Error(), "attempted to close") {
			t.Errorf("the compiler's own message leaked into the persisted detail: %v", err)
		}
	})

	t.Run("unreadable file", func(t *testing.T) {
		rec := fakeNode(t, `echo '{"constructs":[]}'`)
		s := strykerWithSource(t, "web/src/Other.svelte", "<p>a</p>\n")
		_, err := s.Constructs(file)
		if !errors.Is(err, ErrASTUnavailable) || !strings.Contains(err.Error(), "cannot read "+file) {
			t.Fatalf("err = %v, want ErrASTUnavailable naming the file", err)
		}
		if calls := readRecord(t, rec, "calls"); calls != "" {
			t.Errorf("node was spawned %q times for a file that cannot be read", strings.TrimSpace(calls))
		}
	})
}

// A .svelte goes to node like any other file, and what comes back is merged;
// every other extension keeps the spans the parser gave it.
func TestSvelteSpansAreMergedOthersPassThrough(t *testing.T) {
	const answer = `echo '{"constructs":[` +
		`{"start":5,"end":5,"kind":"Element","name":"div"},` +
		`{"start":5,"end":5,"kind":"VariableDeclaration","name":"x"}]}'`
	merged := []Construct{{Start: 5, End: 5, Kind: mergedKind}}
	untouched := []Construct{
		{Start: 5, End: 5, Kind: "Element", Name: "div"},
		{Start: 5, End: 5, Kind: "VariableDeclaration", Name: "x"},
	}
	for _, tc := range []struct {
		file string
		want []Construct
	}{
		{"web/src/App.svelte", merged},
		{"web/src/App.SVELTE", merged},
		{"web/src/a.ts", untouched},
	} {
		t.Run(tc.file, func(t *testing.T) {
			rec := fakeNode(t, answer)
			s := strykerWithSource(t, tc.file, "<script>let x = 1;</script>\n")
			got, err := s.Constructs(tc.file)
			if err != nil {
				t.Fatalf("Constructs: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Constructs = %v, want %v", got, tc.want)
			}
			if stdin := readRecord(t, rec, "stdin"); !strings.Contains(stdin, `"file":"`+tc.file+`"`) {
				t.Errorf("stdin does not name %s", tc.file)
			}
		})
	}
}

// The merge is what keeps ConstructResolver's sorted-and-disjoint promise, and
// enclosing()'s binary search relies on it.
func TestMergeConstructs(t *testing.T) {
	fn := func(start, end int, name string) Construct {
		return Construct{Start: start, End: end, Kind: "FunctionDeclaration", Name: name}
	}
	fused := func(start, end int) Construct {
		return Construct{Start: start, End: end, Kind: mergedKind}
	}
	for _, tc := range []struct {
		name string
		in   []Construct
		want []Construct
	}{
		{"nothing stays empty and non-nil", []Construct{}, []Construct{}},
		{"disjoint spans keep their labels", []Construct{fn(1, 2, "a"), fn(4, 5, "b")}, []Construct{fn(1, 2, "a"), fn(4, 5, "b")}},
		{"adjacent lines do not touch", []Construct{fn(1, 2, "a"), fn(3, 4, "b")}, []Construct{fn(1, 2, "a"), fn(3, 4, "b")}},
		{"markup ahead of script is sorted", []Construct{fn(7, 8, "markup"), fn(2, 3, "script")}, []Construct{fn(2, 3, "script"), fn(7, 8, "markup")}},
		{"one shared line fuses", []Construct{fn(1, 1, "a"), fn(1, 1, "b")}, []Construct{fused(1, 1)}},
		{"a shared last line widens the span", []Construct{fn(1, 3, "a"), fn(3, 5, "b")}, []Construct{fused(1, 5)}},
		{"a span inside another cannot shrink it", []Construct{fn(1, 9, "a"), fn(3, 4, "b")}, []Construct{fused(1, 9)}},
		{"a chain fuses end to end", []Construct{fn(3, 4, "c"), fn(1, 2, "a"), fn(2, 3, "b")}, []Construct{fused(1, 4)}},
		{"only the colliding pair fuses", []Construct{fn(1, 1, "a"), fn(1, 2, "b"), fn(4, 5, "c")}, []Construct{fused(1, 2), fn(4, 5, "c")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeConstructs(tc.in)
			if got == nil || !reflect.DeepEqual(got, tc.want) {
				t.Errorf("mergeConstructs = %#v, want %#v", got, tc.want)
			}
		})
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

// Label is what every persisted range names its construct by. Pinned here,
// in Label's own package, so the per-package mutation run sees both arms —
// the verify-side test that reads the label cannot cover it for gremlins.
func TestLabelInPackage(t *testing.T) {
	for _, tc := range []struct {
		c    Construct
		want string
	}{
		{Construct{Kind: "ExpressionStatement"}, "ExpressionStatement"},
		{Construct{Kind: "FunctionDeclaration", Name: "run"}, "FunctionDeclaration run"},
		{Construct{Kind: "ClassDeclaration", Name: "Tally"}, "ClassDeclaration Tally"},
	} {
		if got := tc.c.Label(); got != tc.want {
			t.Errorf("Label(%+v) = %q, want %q", tc.c, got, tc.want)
		}
	}
	if got := (Construct{Kind: "K"}).Label(); strings.HasSuffix(got, " ") {
		t.Errorf("an unnamed construct labelled with a trailing space: %q", got)
	}
}

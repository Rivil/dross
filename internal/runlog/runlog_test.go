package runlog

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/gitrun"
	"github.com/Rivil/dross/internal/remote"
)

// The seams are package globals, so none of these tests runs in parallel.

// quiet points the terminal and warning seams at buffers for one test.
func quiet(t *testing.T) (out, errOut, warn *bytes.Buffer) {
	t.Helper()
	out, errOut, warn = &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{}
	prevT, prevW := terminal, warnOut
	terminal = func(tm Terminal) io.Writer {
		if tm == Stderr {
			return errOut
		}
		return out
	}
	warnOut = func() io.Writer { return warn }
	t.Cleanup(func() { terminal, warnOut = prevT, prevW })
	return out, errOut, warn
}

func mustWrite(t *testing.T, l *Log, s string) {
	t.Helper()
	if n, err := l.Write([]byte(s)); n != len(s) || err != nil {
		t.Fatalf("Write(%q) = %d, %v; want %d, nil", s, n, err, len(s))
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// trackedRepo is a git repo whose .dross/ holds one committed file.
func trackedRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := gitrun.Run(dir, "init", "-q"); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".dross"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".dross", "project.toml"), []byte("[project]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := gitrun.Run(dir, "add", "--", ".dross/project.toml"); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if err := gitrun.Run(dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "seed"); err != nil {
		t.Fatalf("git commit: %v", err)
	}
	return dir
}

func logsDir(root string) string { return filepath.Join(root, filepath.FromSlash(Dir)) }

func TestLogDirIgnored(t *testing.T) {
	quiet(t)
	t.Run("a fresh log leaves the tree clean", func(t *testing.T) {
		dir := trackedRepo(t)
		l := Open(dir, Name{Kind: KindTest}, Options{})
		mustWrite(t, l, "ok  pkg\n")
		if err := l.Close(); err != nil {
			t.Fatal(err)
		}
		if err := l.Err(); err != nil {
			t.Fatalf("Err() = %v", err)
		}
		out, err := gitrun.Raw(dir, "status", "--porcelain", "-uall")
		if err != nil || out != "" {
			t.Fatalf("git status after a log = %q, %v; want a clean tree", out, err)
		}
		rel := Dir + "/" + filepath.Base(l.Path())
		if err := gitrun.Quiet(dir, "check-ignore", "-q", "--", rel); err != nil {
			t.Fatalf("git check-ignore %s: %v; want it ignored", rel, err)
		}
	})
	t.Run("an edited ignore file is rewritten before the log", func(t *testing.T) {
		dir := trackedRepo(t)
		if err := os.MkdirAll(logsDir(dir), 0o755); err != nil {
			t.Fatal(err)
		}
		ignore := filepath.Join(logsDir(dir), ".gitignore")
		if err := os.WriteFile(ignore, []byte("foo\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		var order []string
		prev := created
		created = func(p string) { order = append(order, filepath.Base(p)) }
		t.Cleanup(func() { created = prev })

		l := Open(dir, Name{Kind: KindTest}, Options{})
		mustWrite(t, l, "x\n")
		l.Close()
		if got := read(t, ignore); got != IgnoreContent {
			t.Fatalf(".gitignore = %q, want %q", got, IgnoreContent)
		}
		if len(order) != 2 || order[0] != ".gitignore" || order[1] != filepath.Base(l.Path()) {
			t.Fatalf("create order = %v, want .gitignore then the log", order)
		}
		if out, _ := gitrun.Raw(dir, "status", "--porcelain", "-uall"); out != "" {
			t.Fatalf("git status = %q, want clean", out)
		}
	})
}

func TestGitignoreDirRefused(t *testing.T) {
	_, _, warn := quiet(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(logsDir(dir), ".gitignore"), 0o755); err != nil {
		t.Fatal(err)
	}
	l := Open(dir, Name{Kind: KindTest}, Options{})
	mustWrite(t, l, "x\n")
	if err := l.Err(); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("Err() = %v, want the not-a-regular-file refusal", err)
	}
	if l.Path() != "" {
		t.Fatalf("Path() = %q, want empty for a refused log", l.Path())
	}
	if !strings.Contains(warn.String(), "warning:") {
		t.Fatalf("no warning printed: %q", warn.String())
	}
}

func TestIgnoreBeforeLog(t *testing.T) {
	quiet(t)
	var order []string
	prev := created
	created = func(p string) { order = append(order, p) }
	t.Cleanup(func() { created = prev })

	dir := t.TempDir()
	l := Open(dir, Name{Kind: KindVerify, Phase: "p", Leg: "gremlins"}, Options{})
	mustWrite(t, l, "x\n")
	l.Close()
	if len(order) != 2 {
		t.Fatalf("created %v, want exactly the .gitignore and the log", order)
	}
	if filepath.Base(order[0]) != ".gitignore" || order[1] != l.Path() {
		t.Fatalf("create order = %v, want .gitignore before %s", order, l.Path())
	}
}

func TestUnwrittenLogCreatesNothing(t *testing.T) {
	quiet(t)
	dir := t.TempDir()
	l := Open(dir, Name{Kind: KindTest}, Options{Echo: true})
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if l.Path() != "" {
		t.Fatalf("Path() = %q before any write, want empty", l.Path())
	}
	if _, err := os.Stat(logsDir(dir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stat %s = %v, want it absent", Dir, err)
	}
}

func TestSameInstantDistinctFiles(t *testing.T) {
	quiet(t)
	frozen := time.Date(2026, 10, 9, 12, 0, 0, 123, time.UTC)
	prev := now
	now = func() time.Time { return frozen }
	t.Cleanup(func() { now = prev })

	dir := t.TempDir()
	a := Open(dir, Name{Kind: KindTest}, Options{})
	b := Open(dir, Name{Kind: KindTest}, Options{})
	mustWrite(t, a, "one\n")
	mustWrite(t, b, "two\n")
	a.Close()
	b.Close()
	if a.Path() == b.Path() || a.Path() == "" || b.Path() == "" {
		t.Fatalf("paths %q and %q, want two distinct files", a.Path(), b.Path())
	}
	if got := read(t, a.Path()); got != "one\n" {
		t.Fatalf("first log = %q, want only its own bytes", got)
	}
	if got := read(t, b.Path()); got != "two\n" {
		t.Fatalf("second log = %q, want only its own bytes", got)
	}
	if want := "test-20261009T120000.000000123Z.log"; filepath.Base(a.Path()) != want {
		t.Fatalf("first name = %s, want %s (UTC to the nanosecond)", filepath.Base(a.Path()), want)
	}
}

func TestPruneNewest20(t *testing.T) {
	quiet(t)
	dir := t.TempDir()
	logs := logsDir(dir)
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{".gitignore": IgnoreContent, "notes.txt": "keep me\n"} {
		if err := os.WriteFile(filepath.Join(logs, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	type old struct {
		name string
		mod  time.Time
	}
	base := time.Now().Add(-48 * time.Hour)
	var olds []old
	for i := 0; i < 24; i++ {
		name := fmt.Sprintf("test-202601%02dT000000.000000000Z.log", i+1)
		if i%2 == 1 {
			name = fmt.Sprintf("verify-p-gremlins-202601%02dT000000.000000000Z.log", i+1)
		}
		olds = append(olds, old{name, base.Add(time.Duration(i) * time.Minute)})
	}
	// An old NAME with the newest mtime: pruning goes by mtime, not by name.
	olds = append(olds, old{"test-20000101T000000.000000000Z.log", base.Add(time.Hour)})
	for _, o := range olds {
		p := filepath.Join(logs, o.name)
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, o.mod, o.mod); err != nil {
			t.Fatal(err)
		}
	}
	// One victim vanishes between the listing and its removal.
	var raced string
	prev := removeFile
	removeFile = func(p string) error {
		if raced == "" {
			raced = p
			os.Remove(p)
		}
		return os.Remove(p)
	}
	t.Cleanup(func() { removeFile = prev })

	l := Open(dir, Name{Kind: KindTest}, Options{})
	mustWrite(t, l, "new\n")
	l.Close()
	if err := l.Err(); err != nil {
		t.Fatalf("Err() = %v; a log gone mid-prune is not an error", err)
	}
	if raced == "" {
		t.Fatal("nothing was pruned — the ENOENT arm went unexercised")
	}

	sort.Slice(olds, func(i, j int) bool { return olds[i].mod.After(olds[j].mod) })
	want := map[string]bool{filepath.Base(l.Path()): true, ".gitignore": true, "notes.txt": true}
	for _, o := range olds[:Keep-1] {
		want[o.name] = true
	}
	if !want["test-20000101T000000.000000000Z.log"] {
		t.Fatal("fixture: the old-named, newest-mtime log should be a survivor")
	}
	des, err := os.ReadDir(logs)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, d := range des {
		got[d.Name()] = true
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("after prune:\n got %v\nwant %v", keys(got), keys(want))
	}
}

func keys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// failAfter is a log file that fails once limit bytes have been written.
type failAfter struct {
	f     *os.File
	n     int
	limit int
}

func (w *failAfter) Write(p []byte) (int, error) {
	if w.n+len(p) > w.limit {
		return 0, errors.New("disk full")
	}
	w.n += len(p)
	return w.f.Write(p)
}

func (w *failAfter) Close() error { return w.f.Close() }

func TestWriteNeverFails(t *testing.T) {
	out, _, warn := quiet(t)
	prev := wrapFile
	wrapFile = func(f *os.File) io.WriteCloser { return &failAfter{f: f, limit: 1024} }
	t.Cleanup(func() { wrapFile = prev })

	l := Open(t.TempDir(), Name{Kind: KindTest}, Options{})
	chunk := strings.Repeat("y", 255) + "\n"
	for i := 0; i < 10; i++ {
		mustWrite(t, l, chunk)
	}
	err := l.Err()
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("Err() = %v, want the first failure (disk full)", err)
	}
	if n := strings.Count(warn.String(), "warning:"); n != 1 {
		t.Fatalf("%d warnings, want exactly 1: %q", n, warn.String())
	}
	// Four chunks fit in 1 KiB; the other six reach the terminal.
	if out.Len() != 6*len(chunk) {
		t.Fatalf("terminal got %d bytes, want the %d the log could not take", out.Len(), 6*len(chunk))
	}
}

func TestNameStaysInDir(t *testing.T) {
	for _, n := range []Name{
		{Kind: KindVerify, Phase: "../../etc", Leg: "gremlins"},
		{Kind: KindVerify, Phase: "p", Leg: "go/x"},
		{Kind: KindVerify, Phase: "..", Leg: "gremlins"},
		{Kind: KindVerify, Phase: "", Leg: "gremlins"},
	} {
		t.Run(n.Phase+"|"+n.Leg, func(t *testing.T) {
			out, _, _ := quiet(t)
			root := filepath.Join(t.TempDir(), "a", "b", "c")
			l := Open(root, n, Options{})
			mustWrite(t, l, "x\n")
			if l.Err() == nil {
				t.Fatalf("Name %+v was accepted", n)
			}
			if l.Path() != "" {
				t.Fatalf("Path() = %q for a refused name", l.Path())
			}
			if out.String() != "x\n" {
				t.Fatalf("terminal = %q; a refused log's bytes still reach the terminal", out.String())
			}
			// Nothing was created anywhere under the temp root.
			var made []string
			filepath.WalkDir(filepath.Dir(filepath.Dir(filepath.Dir(root))), func(p string, d os.DirEntry, _ error) error {
				if d != nil && !d.IsDir() {
					made = append(made, p)
				}
				return nil
			})
			if len(made) != 0 {
				t.Fatalf("a refused name created %v", made)
			}
		})
	}
}

func TestLogsOffTheWire(t *testing.T) {
	quiet(t)
	dir := trackedRepo(t)
	l := Open(dir, Name{Kind: KindTest}, Options{})
	mustWrite(t, l, "x\n")
	l.Close()
	argv, cleanup, err := remote.SyncArgs(remote.Target{Host: "h", Workdir: "/w"}, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	var excludes string
	for _, a := range argv {
		if p, ok := strings.CutPrefix(a, "--exclude-from="); ok {
			excludes = read(t, p)
		}
	}
	if excludes == "" {
		t.Fatalf("SyncArgs built no exclude list: %v", argv)
	}
	found := false
	for _, line := range strings.Split(excludes, "\n") {
		if line == "/.dross/logs/" {
			found = true
		}
	}
	if !found {
		t.Fatalf("exclude list does not keep /.dross/logs/ off the wire:\n%s", excludes)
	}
}

func TestConcurrentWrites(t *testing.T) {
	quiet(t)
	l := Open(t.TempDir(), Name{Kind: KindTest}, Options{})
	const line = "0123456789\n"
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				l.Write([]byte(line))
			}
		}()
	}
	wg.Wait()
	l.Close()
	if err := l.Err(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(l.Path())
	if err != nil {
		t.Fatal(err)
	}
	if want := int64(8 * 1000 * len(line)); info.Size() != want {
		t.Fatalf("log is %d bytes, want %d", info.Size(), want)
	}
}

func TestPipeIsNotTerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	prev := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = prev }()
	if StdoutIsTerminal() {
		t.Fatal("StdoutIsTerminal() = true with a pipe on stdout")
	}
	probed := uintptr(0)
	prevProbe := isTerminal
	isTerminal = func(fd uintptr) bool { probed = fd; return true }
	defer func() { isTerminal = prevProbe }()
	if !StdoutIsTerminal() || probed != w.Fd() {
		t.Fatalf("the probe saw fd %d, want stdout's current fd %d", probed, w.Fd())
	}
}

func TestEchoTeesTerminal(t *testing.T) {
	t.Run("echo on", func(t *testing.T) {
		out, errOut, _ := quiet(t)
		l := Open(t.TempDir(), Name{Kind: KindTest}, Options{Terminal: Stderr, Echo: true})
		mustWrite(t, l, "abc")
		mustWrite(t, l, "def\n")
		l.Close()
		if got := read(t, l.Path()); got != "abcdef\n" {
			t.Fatalf("log = %q", got)
		}
		if errOut.String() != "abcdef\n" || out.Len() != 0 {
			t.Fatalf("stderr = %q, stdout = %q; want every byte on stderr only", errOut.String(), out.String())
		}
	})
	t.Run("echo off", func(t *testing.T) {
		out, errOut, _ := quiet(t)
		l := Open(t.TempDir(), Name{Kind: KindTest}, Options{Terminal: Stderr})
		mustWrite(t, l, "abc\n")
		l.Close()
		if got := read(t, l.Path()); got != "abc\n" {
			t.Fatalf("log = %q", got)
		}
		if errOut.Len() != 0 || out.Len() != 0 {
			t.Fatalf("stderr = %q, stdout = %q; want nothing on a terminal", errOut.String(), out.String())
		}
	})
}

func TestUnavailableLogFallsBack(t *testing.T) {
	out, errOut, warn := quiet(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".dross"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logsDir(dir), []byte("not a dir\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := Open(dir, Name{Kind: KindTest}, Options{Terminal: Stdout})
	for _, s := range []string{"one\n", "two\n", "three\n"} {
		mustWrite(t, l, s)
	}
	if out.String() != "one\ntwo\nthree\n" || errOut.Len() != 0 {
		t.Fatalf("stdout = %q, stderr = %q; want every byte on the chosen terminal", out.String(), errOut.String())
	}
	if n := strings.Count(warn.String(), "warning:"); n != 1 {
		t.Fatalf("%d warnings across 3 writes, want exactly 1: %q", n, warn.String())
	}
	if l.Err() == nil {
		t.Fatal("Err() = nil for an unopenable log")
	}
	if l.Path() != "" {
		t.Fatalf("Path() = %q for an unopenable log", l.Path())
	}
}

func TestTerminalIsAnEnum(t *testing.T) {
	if k := reflect.TypeOf(Terminal(0)).Kind(); k != reflect.Int {
		t.Fatalf("Terminal is a %s, want an int enum", k)
	}
	if Stdout == Stderr {
		t.Fatal("Stdout and Stderr are the same value")
	}
	writer := reflect.TypeOf((*io.Writer)(nil)).Elem()
	for _, typ := range []reflect.Type{reflect.TypeOf(Options{}), reflect.TypeOf(Log{})} {
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			if !f.IsExported() {
				continue
			}
			ft := f.Type
			if ft.Implements(writer) || ft.Kind() == reflect.Interface || ft.Kind() == reflect.Func {
				t.Errorf("%s.%s is a %s a caller could point anywhere", typ.Name(), f.Name, ft)
			}
		}
	}
	if f, ok := reflect.TypeOf(Options{}).FieldByName("Terminal"); !ok || f.Type != reflect.TypeOf(Terminal(0)) {
		t.Fatal("Options.Terminal is not the Terminal enum")
	}
}

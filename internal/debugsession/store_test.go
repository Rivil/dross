package debugsession

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/gitrun"
)

var opened = time.Date(2026, 10, 5, 12, 0, 0, 0, time.FixedZone("BST", 3600))

func gitInit(t *testing.T, dir string) {
	t.Helper()
	if err := gitrun.Run(dir, "init", "-q"); err != nil {
		t.Fatalf("git init: %v", err)
	}
}

func mustCreate(t *testing.T, root, slug string) string {
	t.Helper()
	p, err := Create(root, slug, opened)
	if err != nil {
		t.Fatalf("Create(%q): %v", slug, err)
	}
	return p
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestTemplateRoundTrip(t *testing.T) {
	tpl := Template("flaky-login", opened)
	s := Parse([]byte(tpl))
	noProblems(t, s)
	if s.State() != StateOpen || s.Hypotheses != 0 || s.FailedSinceReplan != 0 || s.Signals != 0 || len(s.Prevention) != 0 {
		t.Fatalf("Parse(Template) = %+v, want open with 0/0/0 and empty Prevention", s)
	}
	var headings []string
	for _, line := range strings.Split(tpl, "\n") {
		if h, ok := strings.CutPrefix(line, "## "); ok {
			headings = append(headings, h)
		}
	}
	if !reflect.DeepEqual(headings, Headings()) {
		t.Fatalf("template headings = %q, want %q", headings, Headings())
	}
	for _, want := range []string{"# Debug: flaky-login\n", "\nstatus: open\n", "\nopened: 2026-10-05T11:00:00Z\n"} {
		if !strings.Contains(tpl, want) {
			t.Errorf("template lacks %q", want)
		}
	}

	// Three failures under the template's OWN Fix attempts heading — the one
	// Parse must recognise — trip the threshold.
	at := strings.Index(tpl, "\n## "+HeadingResolution+"\n")
	failed := tpl[:at] + "\n- [failed] a\n- [failed] b\n- [failed] c\n" + tpl[at:]
	if got := Parse([]byte(failed)); got.State() != StateNeedsReplan || len(got.Problems) != 0 {
		t.Fatalf("three failures under the template's Fix attempts: State=%s Problems=%q", got.State(), got.Problems)
	}
}

func TestValidSlug(t *testing.T) {
	for _, s := range []string{"flaky-login", "x1", "a", "1-2-3", strings.Repeat("a", 64)} {
		if !ValidSlug(s) {
			t.Errorf("ValidSlug(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "..", "a/b", "../x", ".x", "-x", "x-", "a--b", "A", "a.md", "a b", "é", "a\x1bb", strings.Repeat("a", 65)} {
		if ValidSlug(s) {
			t.Errorf("ValidSlug(%q) = true, want false", s)
		}
		root := t.TempDir()
		if _, err := Create(root, s, opened); err == nil || !strings.Contains(err.Error(), "invalid session name") {
			t.Errorf("Create(%q) err = %v, want an invalid-name refusal", s, err)
		}
		if _, err := os.Lstat(filepath.Join(root, ".dross")); !os.IsNotExist(err) {
			t.Errorf("Create(%q) created .dross (%v); it must write nothing", s, err)
		}
	}
}

func TestCreateRefusesExisting(t *testing.T) {
	root := t.TempDir()
	p := mustCreate(t, root, "flaky-login")
	if p != filepath.Join(root, ".dross", "debug", "flaky-login.md") {
		t.Fatalf("Create returned %q", p)
	}
	for _, body := range []string{"", strings.Replace(Template("flaky-login", opened), "status: open", "status: resolved", 1)} {
		if body != "" {
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		before := mustRead(t, p)
		_, err := Create(root, "flaky-login", opened.Add(time.Hour))
		if err == nil || !strings.Contains(err.Error(), "already exists") || !strings.Contains(err.Error(), `"flaky-login"`) {
			t.Fatalf("second Create err = %v, want already exists naming the slug", err)
		}
		if !bytes.Equal(mustRead(t, p), before) {
			t.Fatal("a refused Create changed the existing session")
		}
	}
}

func TestCreateIgnoredBeforeWrite(t *testing.T) {
	t.Run("in a work tree the session is ignored", func(t *testing.T) {
		root := t.TempDir()
		gitInit(t, root)
		var argvs [][]string
		gitrun.ArgvRecorder = func(a []string) { argvs = append(argvs, a) }
		t.Cleanup(func() { gitrun.ArgvRecorder = nil })

		mustCreate(t, root, "flaky-login")
		gitrun.ArgvRecorder = nil
		out, err := gitrun.Raw(root, "status", "--porcelain", "--untracked-files=all")
		if err != nil || out != "" {
			t.Fatalf("git status after Create = %q, %v; want nothing to commit", out, err)
		}
		if err := gitrun.Quiet(root, "check-ignore", "-q", "--", ".dross/debug/flaky-login.md"); err != nil {
			t.Fatalf("check-ignore: %v", err)
		}
		want := []string{"check-ignore", "-q", "--", ".dross/debug/flaky-login.md"}
		if !slices.ContainsFunc(argvs, func(a []string) bool { return slices.Equal(a, want) }) {
			t.Fatalf("recorded argvs %q lack %q", argvs, want)
		}
	})

	t.Run("a .gitignore directory refuses", func(t *testing.T) {
		root := t.TempDir()
		gitInit(t, root)
		if err := os.MkdirAll(filepath.Join(root, ".dross", "debug", ".gitignore"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := Create(root, "x", opened); err == nil || !strings.Contains(err.Error(), ".gitignore is not a regular file") {
			t.Fatalf("Create err = %v, want the .gitignore refusal", err)
		}
		if _, err := os.Lstat(Path(root, "x")); !os.IsNotExist(err) {
			t.Fatal("a session was written past a broken ignore")
		}
	})

	t.Run("a re-including .gitignore refuses", func(t *testing.T) {
		root := t.TempDir()
		gitInit(t, root)
		ignore := filepath.Join(root, ".dross", "debug", ".gitignore")
		if err := os.MkdirAll(filepath.Dir(ignore), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(ignore, []byte("!*\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := Create(root, "x", opened)
		if err == nil || !strings.Contains(err.Error(), "git does not ignore it") || !strings.Contains(err.Error(), ".dross/debug/.gitignore") {
			t.Fatalf("Create err = %v, want a refusal naming .dross/debug/.gitignore", err)
		}
		if _, err := os.Lstat(Path(root, "x")); !os.IsNotExist(err) {
			t.Fatal("a session was written though git would track it")
		}
		if got := mustRead(t, ignore); string(got) != "!*\n" {
			t.Fatalf("Create rewrote the user's .gitignore to %q", got)
		}
	})

	t.Run("outside a work tree the seed is still written", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(root))
		mustCreate(t, root, "x")
		got := string(mustRead(t, filepath.Join(root, ".dross", "debug", ".gitignore")))
		if !slices.Contains(strings.Split(got, "\n"), "*") {
			t.Fatalf(".gitignore = %q, want a `*` line", got)
		}
	})
}

func TestLoad(t *testing.T) {
	t.Run("no directory is no sessions", func(t *testing.T) {
		got, err := Load(t.TempDir())
		if got != nil || err != nil {
			t.Fatalf("Load = %v, %v; want nil, nil", got, err)
		}
	})

	t.Run("a directory that cannot be listed errors", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, ".dross"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".dross", "debug"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(root); err == nil {
			t.Fatal("Load over a regular file named debug succeeded")
		}
	})

	t.Run("sessions newest first, nothing else", func(t *testing.T) {
		root := t.TempDir()
		dir := filepath.Join(root, ".dross", "debug")
		base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
		replan := strings.Replace(Template("middle", opened), "\n## "+HeadingResolution, "\n- [failed] a\n- [failed] b\n- [failed] c\n\n## "+HeadingResolution, 1)
		mtimes := map[string]time.Time{"oldest": base, "middle": base.Add(time.Hour), "newest": base.Add(2 * time.Hour)}
		for _, slug := range []string{"oldest", "newest", "middle"} {
			p := mustCreate(t, root, slug)
			if slug == "middle" {
				if err := os.WriteFile(p, []byte(replan), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Chtimes(p, mtimes[slug], mtimes[slug]); err != nil {
				t.Fatal(err)
			}
		}
		for _, name := range []string{".x.md.123.tmp", "Bad Name.md", "notes.txt", "esc\x1b.md", "UPPER.md"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(Template("x", opened)), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Mkdir(filepath.Join(dir, "d.md"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(Path(root, "oldest"), filepath.Join(dir, "link.md")); err != nil {
			t.Fatal(err)
		}

		got, err := Load(root)
		if err != nil {
			t.Fatal(err)
		}
		var slugs []string
		for _, e := range got {
			slugs = append(slugs, e.Slug)
		}
		if want := []string{"newest", "middle", "oldest"}; !reflect.DeepEqual(slugs, want) {
			t.Fatalf("Load slugs = %q, want %q", slugs, want)
		}
		if !got[0].Updated.Equal(base.Add(2 * time.Hour)) {
			t.Errorf("Updated = %v, want the file mtime", got[0].Updated)
		}
		if got[1].Session.State() != StateNeedsReplan || !bytes.Equal(got[1].Raw, []byte(replan)) {
			t.Errorf("middle = %+v, want needs-replan parsed from its own bytes", got[1].Session)
		}
	})

	t.Run("equal mtimes order by name", func(t *testing.T) {
		root := t.TempDir()
		for _, slug := range []string{"b", "a", "c"} {
			if err := os.Chtimes(mustCreate(t, root, slug), opened, opened); err != nil {
				t.Fatal(err)
			}
		}
		got, err := Load(root)
		if err != nil || len(got) != 3 || got[0].Slug != "a" || got[1].Slug != "b" || got[2].Slug != "c" {
			t.Fatalf("Load = %+v, %v; want a, b, c", got, err)
		}
	})

	t.Run("an unreadable session is listed with a Problem", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root reads a chmod-000 file")
		}
		root := t.TempDir()
		mustCreate(t, root, "fine")
		locked := mustCreate(t, root, "locked")
		if err := os.Chmod(locked, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(locked, 0o644) })
		got, err := Load(root)
		if err != nil || len(got) != 2 {
			t.Fatalf("Load = %+v, %v; want both sessions", got, err)
		}
		for _, e := range got {
			switch e.Slug {
			case "locked":
				if e.Raw != nil || e.Session.State() != StateOpen || len(e.Session.Problems) != 1 || !strings.Contains(e.Session.Problems[0], "cannot read the session") {
					t.Errorf("locked = %+v", e)
				}
				if strings.Contains(e.Session.Problems[0], root) {
					t.Errorf("the Problem %q carries the absolute path", e.Session.Problems[0])
				}
			case "fine":
				if len(e.Session.Problems) != 0 || e.Raw == nil {
					t.Errorf("fine = %+v", e)
				}
			}
		}
	})
}

func TestRewrite(t *testing.T) {
	t.Run("a file changed since read is not overwritten", func(t *testing.T) {
		root := t.TempDir()
		p := mustCreate(t, root, "x")
		read := mustRead(t, p)
		newer := append(append([]byte(nil), read...), "\n- an Edit the agent landed\n"...)
		if err := os.WriteFile(p, newer, 0o644); err != nil {
			t.Fatal(err)
		}
		err := Rewrite(root, "x", read, []byte("replacement"))
		if err == nil || !strings.Contains(err.Error(), "changed since it was read") {
			t.Fatalf("Rewrite err = %v, want a changed-since-read refusal", err)
		}
		if !bytes.Equal(mustRead(t, p), newer) {
			t.Fatal("Rewrite lost the newer bytes")
		}
	})

	t.Run("success replaces atomically and keeps the mode", func(t *testing.T) {
		root := t.TempDir()
		p := mustCreate(t, root, "x")
		if err := os.Chmod(p, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := Rewrite(root, "x", mustRead(t, p), []byte("next\n")); err != nil {
			t.Fatal(err)
		}
		if got := mustRead(t, p); string(got) != "next\n" {
			t.Fatalf("file = %q", got)
		}
		info, err := os.Stat(p)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("mode = %v, %v; want 0600", info.Mode().Perm(), err)
		}
		des, err := os.ReadDir(filepath.Dir(p))
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, d := range des {
			names = append(names, d.Name())
		}
		if want := []string{".gitignore", "x.md"}; !reflect.DeepEqual(names, want) {
			t.Fatalf("directory holds %q, want %q (a temp file was left)", names, want)
		}
	})

	t.Run("a missing session or bad name errors", func(t *testing.T) {
		root := t.TempDir()
		if err := Rewrite(root, "absent", nil, []byte("x")); err == nil {
			t.Error("Rewrite of a missing session succeeded")
		}
		if err := Rewrite(root, "../x", nil, []byte("x")); err == nil || !strings.Contains(err.Error(), "invalid session name") {
			t.Errorf("Rewrite(../x) err = %v", err)
		}
		if _, err := os.Lstat(filepath.Join(root, ".dross")); !os.IsNotExist(err) {
			t.Error("a refused Rewrite wrote something")
		}
	})
}

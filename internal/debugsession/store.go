package debugsession

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Rivil/dross/internal/gitrun"
)

// Dir is where sessions live, relative to the project root. It carries its own
// `*` .gitignore (the locked session_tracking decision): a session quotes
// captured output, so it is machine-local like handoff.md and never rides a
// phase PR or a chore commit.
const Dir = ".dross/debug"

// maxSlug bounds a session name.
const maxSlug = 64

var slugRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ValidSlug reports whether s can name a session: lowercase letters and digits
// in hyphen-separated runs, at most 64 characters. Nothing else — no dot, no
// slash — so a slug joined under Dir cannot leave it.
func ValidSlug(s string) bool {
	return len(s) <= maxSlug && slugRE.MatchString(s)
}

// Path is the session file for slug under root.
func Path(root, slug string) string {
	return filepath.Join(root, filepath.FromSlash(Dir), slug+".md")
}

func rel(slug string) string { return Dir + "/" + slug + ".md" }

// Template is a fresh session: the header marked open, then the seven fixed
// headings, each holding only an HTML-comment placeholder — which Parse never
// counts, so a fresh session can satisfy no gate.
func Template(slug string, now time.Time) string {
	guide := map[string]string{
		HeadingSymptom:       "What fails, exactly: the command, what it printed, what it should have printed.",
		HeadingHypotheses:    "One list item per candidate cause.",
		HeadingCurrentTheory: "The one hypothesis the next probe tests, and why it leads.",
		HeadingNextProbe:     "The single probe that confirms or kills the current theory.",
		HeadingFixAttempts:   "One item per attempt: `- [failed] <what was tried>`; after re-planning, `- [replan] <the new model>`.",
		HeadingResolution:    "At least two independent signals, one list item each.",
		HeadingPrevention:    "The rule that would have caught this, as one statement. No captured output, paths, hostnames or tokens.",
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Debug: %s\n\n%s: %s\nopened: %s\n", slug, StatusKey, StateOpen, now.UTC().Format(time.RFC3339))
	for _, h := range Headings() {
		fmt.Fprintf(&b, "\n## %s\n\n<!-- %s -->\n", h, guide[h])
	}
	return b.String()
}

// Create scaffolds the session for slug and returns its path. The directory's
// self-ignore is written — and, inside a git work tree, proved with
// `git check-ignore` — before the session itself, which is created exclusively:
// an existing session is never overwritten, whatever its state.
func Create(root, slug string, now time.Time) (string, error) {
	if !ValidSlug(slug) {
		return "", fmt.Errorf("invalid session name %q: want lowercase letters and digits in hyphen-separated runs, at most %d characters", slug, maxSlug)
	}
	dir := filepath.Join(root, filepath.FromSlash(Dir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", Dir, err)
	}
	if err := ensureIgnored(root, dir, slug); err != nil {
		return "", err
	}
	p := Path(root, slug)
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return "", fmt.Errorf("debug session %q already exists (%s); resume it with /dross-debug %s", slug, rel(slug), slug)
	}
	if err != nil {
		return "", fmt.Errorf("create %s: %w", rel(slug), err)
	}
	if _, err := f.WriteString(Template(slug, now)); err != nil {
		f.Close()
		return "", fmt.Errorf("write %s: %w", rel(slug), err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("write %s: %w", rel(slug), err)
	}
	return p, nil
}

// ensureIgnored seeds dir's `*` .gitignore when absent and, inside a git work
// tree, refuses unless git really ignores the session about to be written —
// a hand-edited or hostile .gitignore must not let captured output into a
// commit.
func ensureIgnored(root, dir, slug string) error {
	ignore := filepath.Join(dir, ".gitignore")
	info, err := os.Lstat(ignore)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if err := os.WriteFile(ignore, []byte("# machine-local /dross-debug sessions — never committed\n*\n"), 0o644); err != nil {
			return fmt.Errorf("write %s/.gitignore: %w", Dir, err)
		}
	case err != nil:
		return fmt.Errorf("read %s/.gitignore: %w", Dir, err)
	case !info.Mode().IsRegular():
		return fmt.Errorf("%s/.gitignore is not a regular file; it must be the `*` ignore that keeps sessions out of git", Dir)
	}
	if out, err := gitrun.Trim(root, "rev-parse", "--is-inside-work-tree"); err != nil || out != "true" {
		return nil
	}
	switch err := gitrun.Quiet(root, "check-ignore", "-q", "--", rel(slug)); gitrun.ExitCode(err) {
	case 0:
		return nil
	case 1:
		return fmt.Errorf("refusing to write %s: git does not ignore it. Sessions hold captured output and must stay machine-local — "+
			"make %s/.gitignore read `*` (and check no other ignore rule re-includes it)", rel(slug), Dir)
	default:
		return fmt.Errorf("git check-ignore %s: %w", rel(slug), err)
	}
}

// Entry is one session on disk.
type Entry struct {
	Slug string
	// Updated is the file's modification time: the agent edits sessions with
	// Edit, so a header field would go stale.
	Updated time.Time
	Session Session
	// Raw is the bytes Session was parsed from — the basis Rewrite compares
	// against. Nil when the file could not be read.
	Raw []byte
}

// Load returns every session under root, most recently updated first. Only
// regular files named <valid-slug>.md count; the .gitignore, temp files and
// anything else in the directory are skipped. A session that cannot be read
// is still listed, open, with a Problem naming why. No directory is no
// sessions.
func Load(root string) ([]Entry, error) {
	dir := filepath.Join(root, filepath.FromSlash(Dir))
	des, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", Dir, err)
	}
	var out []Entry
	for _, d := range des {
		slug, ok := strings.CutSuffix(d.Name(), ".md")
		if !ok || !d.Type().IsRegular() || !ValidSlug(slug) {
			continue
		}
		info, err := d.Info()
		if err != nil {
			continue // removed between the listing and now
		}
		e := Entry{Slug: slug, Updated: info.ModTime()}
		b, err := os.ReadFile(filepath.Join(dir, d.Name()))
		if err != nil {
			e.Session = Session{Status: StateOpen, Problems: []string{fmt.Sprintf("cannot read the session: %v", unwrapPath(err))}}
		} else {
			e.Raw, e.Session = b, Parse(b)
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Updated.Equal(out[j].Updated) {
			return out[i].Updated.After(out[j].Updated)
		}
		return out[i].Slug < out[j].Slug
	})
	return out, nil
}

// unwrapPath drops a PathError's absolute path, keeping only what went wrong.
func unwrapPath(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}

// Rewrite replaces slug's session with next, atomically and keeping its mode.
// It refuses when the file no longer holds read — the bytes the caller parsed
// — so an Edit the agent landed in between is never silently lost.
func Rewrite(root, slug string, read, next []byte) error {
	if !ValidSlug(slug) {
		return fmt.Errorf("invalid session name %q", slug)
	}
	p := Path(root, slug)
	cur, err := os.ReadFile(p)
	if err != nil {
		return fmt.Errorf("read %s: %w", rel(slug), unwrapPath(err))
	}
	if !bytes.Equal(cur, read) {
		return fmt.Errorf("%s changed since it was read; nothing was written — re-run the command", rel(slug))
	}
	info, err := os.Stat(p)
	if err != nil {
		return fmt.Errorf("read %s: %w", rel(slug), unwrapPath(err))
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), "."+slug+".md.*.tmp")
	if err != nil {
		return fmt.Errorf("write %s: %w", rel(slug), err)
	}
	defer os.Remove(tmp.Name()) // a no-op once the rename has moved it
	if _, err := tmp.Write(next); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", rel(slug), err)
	}
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", rel(slug), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", rel(slug), err)
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		return fmt.Errorf("write %s: %w", rel(slug), err)
	}
	return nil
}

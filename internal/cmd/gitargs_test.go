package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

// TestGitRefArgsSeparator pins the argv exactly. The unconditional separator is
// the whole design: a builder that emitted it only for a value that looked
// dangerous would omit it in precisely the case it exists for, because the
// classifier deciding "dangerous" is the thing being attacked.
func TestGitRefArgsSeparator(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  []string
		want []string
	}{
		{
			// A benign-looking ref still gets the separator.
			name: "benign ref",
			got:  gitRefArgs("checkout", nil, "main"),
			want: []string{"checkout", "--end-of-options", "main"},
		},
		{
			name: "injection payload",
			got:  gitRefArgs("checkout", nil, "-x"),
			want: []string{"checkout", "--end-of-options", "-x"},
		},
		{
			name: "opts stay ahead of the separator",
			got:  gitRefArgs("log", []string{"--reverse", "--pretty=format:%s"}, "main"),
			want: []string{"log", "--reverse", "--pretty=format:%s", "--end-of-options", "main"},
		},
		{
			name: "multiple refs",
			got:  gitRefArgs("merge-base", []string{"--is-ancestor"}, "origin/main", "origin/phase/x"),
			want: []string{"merge-base", "--is-ancestor", "--end-of-options", "origin/main", "origin/phase/x"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !reflect.DeepEqual(tc.got, tc.want) {
				t.Errorf("argv =\n  %q\nwant\n  %q", tc.got, tc.want)
			}
		})
	}
}

// TestGitPathArgsSeparator covers the pathspec half. Options must stay ahead of
// "--": behind it, "--porcelain" is not a flag any more, it is a file named
// --porcelain that git will not find.
func TestGitPathArgsSeparator(t *testing.T) {
	got := gitPathArgs("status", []string{"--porcelain"}, ".dross/")
	want := []string{"status", "--porcelain", "--", ".dross/"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv =\n  %q\nwant\n  %q", got, want)
	}
	sep := indexOf(got, "--")
	if opt := indexOf(got, "--porcelain"); opt > sep {
		t.Errorf("--porcelain is behind the separator, so git reads it as a pathspec: %q", got)
	}
}

// TestGitRefPathArgsSeparators pins the mixed shape. One separator would not
// do: `checkout -- <ref> <paths>` classifies the ref as a pathspec, which is a
// different command, not a safer one.
func TestGitRefPathArgsSeparators(t *testing.T) {
	got := gitRefPathArgs("checkout", nil, []string{"-x"}, ".dross/state.json")
	want := []string{"checkout", "--end-of-options", "-x", "--", ".dross/state.json"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv =\n  %q\nwant\n  %q", got, want)
	}
	if indexOf(got, "--end-of-options") > indexOf(got, "--") {
		t.Errorf("ref separator must precede the path separator: %q", got)
	}
}

// TestSeparatorsAreDistinct guards the one substitution that looks like a
// simplification and is not: collapsing both tokens to "--" would turn every
// rewritten ref call site into a pathspec lookup.
func TestSeparatorsAreDistinct(t *testing.T) {
	if endOfOptions == pathSeparator {
		t.Fatal("ref and path separators must differ — see the locked ref_separator_token decision")
	}
	if endOfOptions != "--end-of-options" || pathSeparator != "--" {
		t.Fatalf("separator tokens drifted: ref=%q path=%q", endOfOptions, pathSeparator)
	}
}

// TestGitAcceptsEndOfOptions is the compatibility floor made executable. The
// locked decision requires git >= 2.24; if the git on PATH is older, the
// builders produce argv git cannot parse and every rewritten call site breaks
// at once. dross doctor reports this for users — this asserts it for CI.
func TestGitAcceptsEndOfOptions(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "")
	mustGit(t, dir, "commit", "-q", "--allow-empty", "-m", "baseline")

	args := append([]string{"-C", dir}, gitRefArgs("rev-parse", nil, "main")...)
	out, err := exec.Command("git", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("git does not accept %s (needs git >= 2.24): %v\n%s", endOfOptions, err, out)
	}

	// And the payload it is there to stop is inert behind it: a ref that would
	// otherwise be read as `--output=<file>` must fail to resolve as a ref, and
	// must not write the file.
	sentinel := filepath.Join(dir, "dross-pwned")
	args = append([]string{"-C", dir}, gitRefArgs("log", nil, "--output="+sentinel)...)
	if out, err := exec.Command("git", args...).CombinedOutput(); err == nil {
		t.Fatalf("git resolved an option-shaped ref behind the separator: %s", out)
	}
	if _, err := os.Stat(sentinel); err == nil {
		t.Fatalf("the separator did not stop --output=: %s exists", sentinel)
	}
}

func indexOf(args []string, want string) int {
	for i, a := range args {
		if a == want {
			return i
		}
	}
	return -1
}

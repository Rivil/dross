package cmd

import (
	"fmt"
	"strings"
)

// validateGitRef is the trust boundary between `.dross/` and `git`'s argument
// parser.
//
// A ref name dross hands to git is rarely typed by the user at the moment of
// use: it comes from `[repo].git_main_branch`, from a rendered
// `[repo].branch_pattern`, from a recorded base in changes.json, or from argv.
// All of those travel with the repo, which means "the repo you cloned" gets to
// choose them. git reads a leading `-` as an option, not a name, so a committed
// `git_main_branch = "--output=/home/you/.ssh/authorized_keys"` is not a branch
// that fails to resolve — it is an argument that succeeds. `git log`, `git
// show` and `git diff` all accept `--output=<file>` and write there;
// `--upload-pack=<cmd>` runs a command for the fetch family.
//
// So the guard is a pre-check, deliberately placed before the first exec rather
// than translated out of git's own refusal — there is no refusal to translate.
// It pairs with the argument separators in gitargs.go: the separator stops a
// value from being *read* as a flag at the call sites that carry one, and this
// stops a value that git would reject for other reasons from getting that far.
// Either alone leaves a gap; the separator does nothing about `..` or a control
// character, and a guard nobody calls at a new call site does nothing at all.
//
// kind names the *source* of the ref, not its role — "repo.git_main_branch",
// not "branch". A refusal that says which committed line to go and fix is
// actionable; one that says "invalid branch" sends the user hunting.
func validateGitRef(kind, name string) error {
	reject := func(why string) error {
		return fmt.Errorf("unsafe git ref for %s: %q %s", kind, name, why)
	}

	if name == "" {
		return reject("is empty")
	}
	// The vector this whole phase exists for. Checked first so its message is
	// the one the user sees, rather than an incidental character complaint.
	if strings.HasPrefix(name, "-") {
		return reject(`begins with "-", which git reads as an option rather than a ref name`)
	}

	// The rest is git check-ref-format's own reject set, applied here so the
	// refusal happens in dross rather than several argv layers later.
	for _, r := range name {
		if r <= ' ' || r == '\x7f' {
			return reject("contains a space or control character")
		}
		if strings.ContainsRune("~^:?*[\\", r) {
			return reject(fmt.Sprintf("contains %q, which git forbids in a ref name", r))
		}
	}
	if strings.Contains(name, "..") {
		return reject(`contains ".."`)
	}
	if strings.Contains(name, "@{") {
		return reject(`contains "@{"`)
	}
	if name == "@" {
		return reject(`is "@"`)
	}
	if strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") || strings.Contains(name, "//") {
		return reject("has an empty path component")
	}
	if strings.HasSuffix(name, ".") {
		return reject(`ends with "."`)
	}
	// ".lock" is rejected per component, not just at the end: "a.lock/b" is a
	// ref whose first component collides with git's lock file.
	for _, part := range strings.Split(name, "/") {
		if strings.HasSuffix(part, ".lock") {
			return reject(`has a component ending in ".lock"`)
		}
		if strings.HasPrefix(part, ".") {
			return reject("has a component beginning with a dot")
		}
	}
	return nil
}

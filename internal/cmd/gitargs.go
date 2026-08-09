package cmd

// Argument-vector builders that place a separator between git's options and the
// positional values dross derives from config, argv or recorded state.
//
// The problem they solve is the same one validateGitRef guards from the other
// side: git's argument parser decides what is an option by looking at the
// leading character, not at the position. `git log <mainBranch>` with a
// mainBranch of "--output=/tmp/x" is not a lookup that fails — it is a flag that
// succeeds. So is "--upload-pack=<cmd>" in a fetch positional.
//
// Two separators, not one, and they are not interchangeable — see the locked
// `ref_separator_token` decision in this phase's spec:
//
//   --                 ends options and starts PATHSPECS.
//   --end-of-options   ends options and starts everything else (refs included).
//
// Putting `--` in front of a ref is not a safer version of this; it tells git
// the branch name is a *path*, which turns an injection into a different bug
// (`git checkout -- main` checks out a file called main). `--end-of-options`
// (git >= 2.24, reported by `dross doctor`) is the one that means "stop reading
// options" without also reclassifying what follows.
//
// The separator is emitted UNCONDITIONALLY. A builder that only emitted it for
// a value that "looks dangerous" would be absent in exactly the case it was
// written for — the classifier is the vulnerability. Everything after the
// separator is data by construction, which is also what makes the audit in
// gitargs_audit_test.go decidable: it looks for a positional that is not
// preceded by one, not for a value it judges suspicious.

const (
	// endOfOptions stops git reading further arguments as options, without
	// reclassifying them as pathspecs.
	endOfOptions = "--end-of-options"
	// pathSeparator stops git reading further arguments as options or refs;
	// everything after it is a pathspec.
	pathSeparator = "--"
)

// gitRefArgs builds `<sub> <opts...> --end-of-options <refs...>`.
//
// opts stay ahead of the separator because they are dross's own literals — the
// separator's job is to fence off the caller-derived refs, and demoting the
// flags dross chose would just break the command.
func gitRefArgs(sub string, opts []string, refs ...string) []string {
	args := make([]string, 0, len(opts)+len(refs)+2)
	args = append(args, sub)
	args = append(args, opts...)
	args = append(args, endOfOptions)
	return append(args, refs...)
}

// gitPathArgs builds `<sub> <opts...> -- <paths...>`.
func gitPathArgs(sub string, opts []string, paths ...string) []string {
	args := make([]string, 0, len(opts)+len(paths)+2)
	args = append(args, sub)
	args = append(args, opts...)
	args = append(args, pathSeparator)
	return append(args, paths...)
}

// gitRefPathArgs builds `<sub> <opts...> --end-of-options <refs...> -- <paths...>`
// for the mixed shape — `git checkout <ref> -- <paths>`, `git diff <a> <b> --
// <paths>` — where both halves are caller-derived and each needs its own
// separator. Dropping to one would classify the refs as pathspecs.
func gitRefPathArgs(sub string, opts []string, refs []string, paths ...string) []string {
	args := gitRefArgs(sub, opts, refs...)
	args = append(args, pathSeparator)
	return append(args, paths...)
}

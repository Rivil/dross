package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// InstallFlagHints registers a FlagErrorFunc on root so an unrecognised flag
// gets the same curated-then-distance treatment an unrecognised subcommand
// does.
//
// Flag-parse failures never reach a RunE — cobra aborts during parsing — so
// EnforceSubcommandKnown's guard cannot see them. Cobra resolves
// FlagErrorFunc by walking up to the nearest ancestor that has one, so
// registering at the root covers every command in the tree.
func InstallFlagHints(root *cobra.Command) {
	if root == nil {
		return
	}
	root.SetFlagErrorFunc(decorateFlagError)
}

// decorateFlagError APPENDS a hint to cobra's message and never replaces it.
// The literal "unknown flag" / "unknown shorthand flag" prefix is load-bearing:
// telemetry.ClassifyMessage buckets the event by that substring, so rewriting
// the message would silently demote a well-understood failure to `other`.
func decorateFlagError(cmd *cobra.Command, err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	flag := offendingFlag(msg)
	if flag == "" {
		return err
	}
	if hint, ok := CuratedHint(cmd.CommandPath(), flag); ok {
		return fmt.Errorf("%s\n\n%s", msg, hintLines(hint))
	}
	if near := Nearest(strings.TrimLeft(flag, "-"), declaredFlagNames(cmd)); len(near) > 0 {
		for i, n := range near {
			near[i] = "--" + n
		}
		return fmt.Errorf("%s\n\nDid you mean this?\n\t%s", msg, strings.Join(near, "\n\t"))
	}
	return err
}

// offendingFlag pulls the flag out of cobra's own wording. Returns "" when the
// message is some other parse failure (a flag missing its value, say), which
// is left undecorated rather than guessed at.
func offendingFlag(msg string) string {
	if rest, ok := strings.CutPrefix(msg, "unknown flag: "); ok {
		return strings.TrimSpace(rest)
	}
	// "unknown shorthand flag: 'x' in -xyz"
	if rest, ok := strings.CutPrefix(msg, "unknown shorthand flag: "); ok {
		rest = strings.TrimSpace(rest)
		if i := strings.Index(rest, " in "); i > 0 {
			rest = rest[:i]
		}
		return "-" + strings.Trim(rest, "'")
	}
	return ""
}

// declaredFlagNames returns every long flag actually declared on cmd,
// inherited flags included — the same set the user could legally have typed.
func declaredFlagNames(cmd *cobra.Command) []string {
	var names []string
	seen := map[string]bool{}
	add := func(f *pflag.Flag) {
		if f.Hidden || seen[f.Name] {
			return
		}
		seen[f.Name] = true
		names = append(names, f.Name)
	}
	cmd.LocalFlags().VisitAll(add)
	cmd.InheritedFlags().VisitAll(add)
	return names
}

// hintLines renders a Hint as the two-line block both the subcommand guard and
// the flag hook append, so a user sees one shape wherever a mis-reach is
// caught.
func hintLines(h Hint) string {
	out := "Try:\n\t" + h.Fix
	if h.Why != "" {
		out += "\n\n" + h.Why + "."
	}
	return out
}

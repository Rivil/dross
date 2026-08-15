package cmd

// `dross mutation remote *` — the deprecated spelling of `dross remote *`.
//
// The verb lived here while mutation was the only thing that ran off-box. It is
// not any more: `dross test` reads the same grant, so a name scoped under
// `mutation` says the wrong thing about what the user is authorizing.
//
// This file keeps the old path working and nothing else. It registers the SAME
// builders `dross remote` does (remoteGrantTree in remote_grant.go), so the two
// spellings cannot drift into two behaviours — a second copy would be a second
// consent surface for one decision. The whole implementation, and the reasoning
// behind the print-before-write ordering, lives there.
//
// It is an alias rather than a removal because the README documents the old
// verb and it is in muscle memory; breaking it would make a working setup fail
// for a rename.

import "github.com/spf13/cobra"

// Mutation registers `dross mutation`.
func Mutation() *cobra.Command {
	c := &cobra.Command{
		Use:   "mutation",
		Short: "Configure how mutation testing runs",
	}
	c.AddCommand(mutationRemote())
	return c
}

// mutationRemote is the alias subtree. It is remoteGrantTree with a deprecation
// note appended — same commands, same store, same banner.
func mutationRemote() *cobra.Command {
	c := remoteGrantTree()
	c.Short = "Deprecated: use `dross remote` — authorize a host dross runs this repo's code on"
	c.Long = c.Long + "\n\nDeprecated: this is `dross remote` under its old name, kept working because\n" +
		"the grant is no longer mutation-specific. Prefer `dross remote`."
	return c
}

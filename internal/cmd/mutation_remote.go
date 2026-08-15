package cmd

// The remote-mutation consent verb: `dross mutation remote grant|status|revoke`.
//
// It is the ONLY writer of mutation_remote_host and mutation_remote_workdir,
// and that exclusivity is the locked consent_model decision rather than a
// stylistic choice. Configuring a remote is code execution on a machine of the
// config's choosing — dross rsyncs the working tree there and runs the repo's
// test suite on it, as the user. `dross local set` is a generic key-writer:
// anything it can write, an agent can write without ever showing the user what
// it is authorizing. So the two keys are absent from localKeys (see local.go)
// and granted only here, by a verb that prints the host and workdir it is about
// to authorize BEFORE it writes them.
//
// That ordering is the whole mechanism, not a nicety. A grant that wrote first
// and printed after would have authorized the host by the time the user read
// its name. TestRemoteGrantPrintsBeforeItWrites pins it by injecting a failing
// write and asserting the banner is already on stdout with nothing stored.
//
// This mirrors `dross trust` exactly — the same precedent, one step further:
// trust consents to a command running HERE, this consents to it running THERE.

import (
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/remote"
)

// grantRemoteWrite is the store write, held as a seam so the ordering test can
// make it fail and observe that the banner was printed anyway. Production code
// never reassigns it.
var grantRemoteWrite = writeRemoteGrant

// writeRemoteGrant records the grant in .dross/local.toml, leaving every other
// key — notably the mutation_workers / mutation_test_cpu tuning knobs — alone.
// It loads and re-saves rather than truncating, because a grant is one key in a
// shared store and a revoke that also reset the user's worker count would be a
// second, unannounced change.
func writeRemoteGrant(root, host, workdir string) error {
	path := localPath(root)
	l, err := loadLocal(path)
	if err != nil {
		return err
	}
	l.MutationRemoteHost = host
	l.MutationRemoteWorkdir = workdir
	return l.save(path)
}

// Mutation registers `dross mutation`.
func Mutation() *cobra.Command {
	c := &cobra.Command{
		Use:   "mutation",
		Short: "Configure how mutation testing runs",
	}
	c.AddCommand(mutationRemote())
	return c
}

func mutationRemote() *cobra.Command {
	c := &cobra.Command{
		Use:   "remote",
		Short: "Authorize (or inspect) a host that mutation runs execute on",
		Long: "A remote mutation run rsyncs this working tree to another machine and runs\n" +
			"the mutation adapters there, as you. The authorization lives in the\n" +
			"gitignored .dross/local.toml, so it never travels with the repo — a clone\n" +
			"carries no grant, and a host named in the tracked project.toml is refused.",
	}
	c.AddCommand(mutationRemoteGrant(), mutationRemoteStatus(), mutationRemoteRevoke())
	return c
}

func mutationRemoteGrant() *cobra.Command {
	return &cobra.Command{
		Use:   "grant <host> <workdir>",
		Short: "Authorize mutation runs to execute on <host> under <workdir>",
		Long: "Records the host and workdir in the gitignored .dross/local.toml. Prints\n" +
			"what it is about to authorize before it writes it — a grant you did not\n" +
			"read is a rubber stamp.",
		Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			host, workdir := args[0], args[1]

			// Refuse a tracked store before anything else. Writing a grant into
			// a committed local.toml would put the authorization on the wire to
			// every clone — the exact self-authorizing shape this store exists
			// to prevent.
			if err := refuseTrackedLocal(filepath.Dir(root)); err != nil {
				return err
			}

			// Validate BEFORE the banner. Announcing a grant of a host or
			// workdir the transport would refuse anyway tells the user
			// something was authorized when nothing was.
			t := remote.Target{Host: host, Workdir: workdir}
			if err := t.Validate(); err != nil {
				return err
			}

			Printf(
				"granting mutation runs on another machine:\n\n"+
					"    host:    %s\n"+
					"    workdir: %s\n\n"+
					"dross will rsync this working tree to that path and run the mutation\n"+
					"adapters there, as you.\n\n",
				t.Host, t.Workdir)

			if err := grantRemoteWrite(root, t.Host, t.Workdir); err != nil {
				return err
			}

			Printf("recorded in %s/%s (gitignored — it does not travel with the repo).\n", RootDirName, LocalFile)
			Print("Withdraw it with `dross mutation remote revoke`.")
			return nil
		},
	}
}

func mutationRemoteStatus() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Print the remote this machine has authorized, if any",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			// Reads through the same readRemoteGrant a run does, so status can
			// never report a grant the run would refuse (or vice versa). A
			// tracked local.toml surfaces here as the refusal, not as the host
			// it happens to contain — that host is not authorization.
			t, err := readRemoteGrant(root, filepath.Dir(root))
			if err != nil {
				return err
			}
			if t == nil {
				// Exits 0: "no remote configured" is the normal state of most
				// repos, and callers branch on the text, not on a failure.
				Print("remote mutation: not granted")
				return nil
			}
			Printf("remote mutation: granted\n\n    host:    %s\n    workdir: %s\n", t.Host, t.Workdir)
			return nil
		},
	}
}

func mutationRemoteRevoke() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke",
		Short: "Withdraw the remote authorization",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			repoDir := filepath.Dir(root)
			if err := refuseTrackedLocal(repoDir); err != nil {
				return err
			}
			l, err := loadLocal(localPath(root))
			if err != nil {
				return err
			}
			if l.MutationRemoteHost == "" && l.MutationRemoteWorkdir == "" {
				Print("remote mutation: not granted — nothing to revoke")
				return nil
			}
			host := l.MutationRemoteHost
			// Both keys clear together. A workdir left behind after its host is
			// gone is a leftover that the next grant would silently inherit.
			if err := grantRemoteWrite(root, "", ""); err != nil {
				return err
			}
			if host == "" {
				host = "(none)"
			}
			Printf("revoked: mutation runs no longer authorized on %s.\n", host)
			return nil
		},
	}
}

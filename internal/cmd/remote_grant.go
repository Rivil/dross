package cmd

// The remote-execution consent verb: `dross remote grant|status|revoke`.
//
// It is the ONLY writer of remote_host and remote_workdir, and that exclusivity
// is the locked consent_model decision rather than a stylistic choice.
// Configuring a remote is code execution on a machine of the config's choosing
// — dross rsyncs the working tree there and runs this repo's code on it, as the
// user. `dross local set` is a generic key-writer: anything it can write, an
// agent can write without ever showing the user what it is authorizing. So the
// keys are absent from localKeys (see local.go) and granted only here, by a verb
// that prints the host and workdir it is about to authorize BEFORE it writes
// them.
//
// That ordering is the whole mechanism, not a nicety. A grant that wrote first
// and printed after would have authorized the host by the time the user read
// its name. TestRemoteGrantPrintsBeforeItWrites pins it by injecting a failing
// write and asserting the banner is already on stdout with nothing stored.
//
// This mirrors `dross trust` exactly — the same precedent, one step further:
// trust consents to a command running HERE, this consents to it running THERE.
//
// The verb used to be `dross mutation remote *`, from when mutation was the
// only thing that ran off-box. It is not any more — the test-suite runner reads
// the same grant — so the name moved out from under `mutation`, and the old
// path stays
// registered as an alias onto these exact builders (see mutation_remote.go).
// One implementation, two spellings: a second copy would be a second consent
// surface for one decision.

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
//
// It writes the CURRENT keys and clears the deprecated mutation_remote_*
// aliases in the same save, so re-granting migrates a legacy file instead of
// leaving both generations on disk for effectiveRemote to arbitrate. A revoke
// (host and workdir empty) therefore clears all four, which is what makes
// "withdraw it" mean withdrawn rather than withdrawn-from-one-spelling.
func writeRemoteGrant(root, host, workdir string) error {
	path := localPath(root)
	l, err := loadLocal(path)
	if err != nil {
		return err
	}
	l.RemoteHost = host
	l.RemoteWorkdir = workdir
	l.MutationRemoteHost = ""
	l.MutationRemoteWorkdir = ""
	if host == "" && workdir == "" {
		// A revoke clears the POOL too. Leaving additional authorized hosts
		// behind would make "withdraw it" mean withdrawn-from-one-machine,
		// which is worse than not revoking at all: the user believes nothing is
		// authorized while a run still has somewhere to go.
		l.RemotePool = nil
	}
	return l.save(path)
}

// appendRemoteGrant adds a host to the pool, leaving the scalar grant and every
// other entry in place. Re-adding an identical entry is a no-op rather than a
// duplicate, so a repeated grant does not make the same host get probed twice.
var appendRemoteGrant = func(root, host, workdir string) error {
	path := localPath(root)
	l, err := loadLocal(path)
	if err != nil {
		return err
	}
	if l.RemoteHost == host && l.RemoteWorkdir == workdir {
		return nil
	}
	for _, c := range l.RemotePool {
		if c.Host == host && c.Workdir == workdir {
			return nil
		}
	}
	l.RemotePool = append(l.RemotePool, remoteCandidate{Host: host, Workdir: workdir})
	return l.save(path)
}

// Remote registers `dross remote`.
func Remote() *cobra.Command {
	c := remoteGrantTree()
	c.Use = "remote"
	return c
}

// remoteGrantTree builds the grant/status/revoke tree. Both `dross remote` and
// the deprecated `dross mutation remote` call it, so the two spellings cannot
// drift into two behaviours.
func remoteGrantTree() *cobra.Command {
	c := &cobra.Command{
		Use:   "remote",
		Short: "Authorize (or inspect) a host that dross runs this repo's code on",
		Long: "A remote run rsyncs this working tree to another machine and runs this\n" +
			"repo's code there, as you. The authorization lives in the gitignored\n" +
			".dross/local.toml, so it never travels with the repo — a clone carries no\n" +
			"grant, and a host named in the tracked project.toml is refused.",
	}
	c.AddCommand(remoteGrant(), remoteStatus(), remoteRevoke(), remoteBootstrap())
	return c
}

func remoteGrant() *cobra.Command {
	var addToPool bool
	c := &cobra.Command{
		Use:   "grant <host> <workdir>",
		Short: "Authorize remote runs to execute on <host> under <workdir>",
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
				"granting remote runs on another machine:\n\n"+
					"    host:    %s\n"+
					"    workdir: %s\n\n"+
					"dross will rsync this working tree to that path and run this repo's\n"+
					"code there, as you.\n\n",
				t.Host, t.Workdir)

			write := grantRemoteWrite
			if addToPool {
				write = appendRemoteGrant
			}
			if err := write(root, t.Host, t.Workdir); err != nil {
				return err
			}

			Printf("recorded in %s/%s (gitignored — it does not travel with the repo).\n", RootDirName, LocalFile)
			if addToPool {
				Print("Added to the pool — the first authorized host that answers runs the job.")
			}
			Print("Withdraw it with `dross remote revoke`.")
			return nil
		},
	}
	c.Flags().BoolVar(&addToPool, "add", false,
		"authorize an ADDITIONAL host, keeping the existing grant; runs take the first that answers")
	return c
}

func remoteStatus() *cobra.Command {
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
				Print("remote: not granted")
				return nil
			}
			Printf("remote: granted\n\n    host:    %s\n    workdir: %s\n", t.Host, t.Workdir)
			return nil
		},
	}
}

func remoteRevoke() *cobra.Command {
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
			host, workdir := l.effectiveRemote()
			if host == "" && workdir == "" {
				Print("remote: not granted — nothing to revoke")
				return nil
			}
			// Both keys clear together, in both spellings. A workdir left
			// behind after its host is gone is a leftover that the next grant
			// would silently inherit.
			if err := grantRemoteWrite(root, "", ""); err != nil {
				return err
			}
			if host == "" {
				host = "(none)"
			}
			Printf("revoked: remote runs no longer authorized on %s.\n", host)
			return nil
		},
	}
}

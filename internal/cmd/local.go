package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/consent"
	"github.com/Rivil/dross/internal/localstore"
	"github.com/Rivil/dross/internal/remote"
)

// Local manages .dross/local.toml — machine-local values that must NOT ride
// cumulative history. The store, its key table and every typed reader live in
// internal/localstore; this is only the `dross local get|set` command tree.
//
// Phase work does not use this store — a phase's forked-from base lives in its
// phase-scoped changes.json, which cannot be dragged forward either.
func Local() *cobra.Command {
	c := &cobra.Command{
		Use:   "local",
		Short: "Read and write .dross/local.toml (gitignored, machine-local values)",
	}
	c.AddCommand(localGet(), localSet())
	return c
}

func localGet() *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Print a machine-local value (empty when unset)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			acc, ok := localstore.Keys[args[0]]
			if !ok {
				return fmt.Errorf("unknown local key %q (want %s)", args[0], localstore.KeyNames())
			}
			l, err := localstore.Load(localstore.Path(root))
			if err != nil {
				return err
			}
			// An unset key prints nothing and exits 0 — callers branch on
			// empty output, so a missing value must not look like a failure.
			if v := acc.Get(l); v != "" {
				Print(v)
			}
			return nil
		},
	}
}

func localSet() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Write a machine-local value to .dross/local.toml",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			acc, ok := localstore.Keys[args[0]]
			if !ok {
				return fmt.Errorf("unknown local key %q (want %s)", args[0], localstore.KeyNames())
			}
			path := localstore.Path(root)
			l, err := localstore.Load(path)
			if err != nil {
				return err
			}
			acc.Set(l, args[1])
			if err := l.Save(path); err != nil {
				return err
			}
			Printf("%s = %s\n", args[0], args[1])
			return nil
		},
	}
}

// ---- forwarders onto internal/localstore ------------------------------------
//
// The names cmd's other files and tests still call. Each is a one-line hop
// with no logic of its own; cmd-exec-baseline-drain's t-10 rewires the callers
// onto localstore directly and deletes this block.

// LocalFile is the store's basename under .dross/.
const LocalFile = localstore.File

// localStore is localstore.Store under the name cmd's callers still use.
type localStore struct{ localstore.Store }

func (l *localStore) save(path string) error { return l.Store.Save(path) }

func (l *localStore) effectiveRemote() (host, workdir string) { return l.Store.EffectiveRemote() }

type (
	detachedRun     = localstore.DetachedRun
	remoteCandidate = localstore.RemoteCandidate
)

var localKeys = localstore.Keys

func loadLocal(path string) (*localStore, error) {
	s, err := localstore.Load(path)
	if err != nil {
		return nil, err
	}
	return &localStore{Store: *s}, nil
}

func localPath(root string) string { return localstore.Path(root) }

func grantStore(root string) consent.Store { return localstore.GrantStore(root) }

func readLocalKey(root, key string) string { return localstore.ReadKey(root, key) }

func readDetachedRuns(root, repoDir string) ([]detachedRun, error) {
	return localstore.ReadDetachedRuns(root, repoDir)
}

func findDetachedRun(root, repoDir, phaseID string) (*detachedRun, error) {
	return localstore.FindDetachedRun(root, repoDir, phaseID)
}

func recordDetachedRun(root, repoDir string, rec detachedRun) error {
	return localstore.RecordDetachedRun(root, repoDir, rec)
}

func clearDetachedRun(root, repoDir, phaseID string) (bool, error) {
	return localstore.ClearDetachedRun(root, repoDir, phaseID)
}

func readAllowHosts(root, repoDir string) ([]string, error) {
	return localstore.ReadAllowHosts(root, repoDir)
}

func readRemoteGrants(root, repoDir string) ([]*remote.Target, error) {
	return localstore.ReadRemoteGrants(root, repoDir)
}

func resolveRemoteEnv(allowlist string) ([]remote.EnvVar, error) {
	return localstore.ResolveRemoteEnv(allowlist)
}

func readMutationTuning(root string) (workers, testCPU int, err error) {
	return localstore.ReadMutationTuning(root)
}

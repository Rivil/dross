package cmd

import (
	"github.com/Rivil/dross/internal/consent"
	"github.com/Rivil/dross/internal/localstore"
	"github.com/Rivil/dross/internal/remote"
)

// local_shim_test.go keeps the names this package's tests have always used
// for the local.toml store, now that the store lives in internal/localstore
// and cmd's production code calls it directly. Each is a one-line hop with no
// logic of its own, so a test reads exactly as it did — the phase changed
// where the store lives, not what any e2e test asserts.

const LocalFile = localstore.File

type detachedRun = localstore.DetachedRun

var localKeys = localstore.Keys

// localStore wraps the store so tests can keep calling save.
type localStore struct{ localstore.Store }

func (l *localStore) save(path string) error { return l.Store.Save(path) }

func loadLocal(path string) (*localStore, error) {
	s, err := localstore.Load(path)
	if err != nil {
		return nil, err
	}
	return &localStore{Store: *s}, nil
}

func localPath(root string) string { return localstore.Path(root) }

func grantStore(root string) consent.Store { return localstore.GrantStore(root) }

func findDetachedRun(root, repoDir, phaseID string) (*detachedRun, error) {
	return localstore.FindDetachedRun(root, repoDir, phaseID)
}

func recordDetachedRun(root, repoDir string, rec detachedRun) error {
	return localstore.RecordDetachedRun(root, repoDir, rec)
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

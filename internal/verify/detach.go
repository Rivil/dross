package verify

import (
	"os"
	"os/exec"
)

// RunDetachArgv runs one built transport argv for a detached verify — the tree
// push before dispatch and each report fetch at collect — with its output on
// this process's stderr, where the user watching the run is looking. The argv
// comes from internal/remote's SyncArgs/FetchArgs, which refuse to build one
// for a target outside the host/workdir allowlist; every caller in
// internal/cmd checks consent before handing it one.
func RunDetachArgv(argv []string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	return cmd.Run()
}

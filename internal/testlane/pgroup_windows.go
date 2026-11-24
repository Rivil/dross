//go:build windows

package testlane

import "os/exec"

// killGroupOnCancel is a no-op on Windows, which has no POSIX process groups:
// exec.CommandContext's default cancel kills the process itself, and
// WaitDelay still bounds how long a surviving child can hold the pipes.
func killGroupOnCancel(c *exec.Cmd) {}

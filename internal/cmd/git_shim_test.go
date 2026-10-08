package cmd

import "github.com/Rivil/dross/internal/gitrun"

// git_shim_test.go keeps the git helper names this package's tests use for
// fixture setup, now that production code calls internal/gitrun directly. Each
// is a one-line hop onto the gitrun verb it always stood for, so a test's
// setup reads exactly as it did. Test code only: the argv audits scan non-test
// files, and no production file may declare these names again.

func gitTrim(repoDir string, args ...string) (string, error) { return gitrun.Trim(repoDir, args...) }

func gitRead(repoDir string, args ...string) (string, error) { return gitrun.Read(repoDir, args...) }

func gitRun(repoDir string, args ...string) error { return gitrun.Run(repoDir, args...) }

func gitNoOut(repoDir string, args ...string) error { return gitrun.Quiet(repoDir, args...) }

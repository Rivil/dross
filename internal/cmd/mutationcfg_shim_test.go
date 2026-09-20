package cmd

import (
	"github.com/Rivil/dross/internal/mutationcfg"
	"github.com/Rivil/dross/internal/project"
)

// Test-only shims over internal/mutationcfg with the signatures cmd's wiring
// tests were written against, so mutation_remote_wiring_test.go,
// remote_preflight_wiring_test.go, doctor_remote_test.go and
// remote_bootstrap_test.go keep asserting the SAME construction path
// production uses (localSource) without a single assertion edit. They are
// deliberately not production code: production callers name the package.

// resolveMutationTuning is mutationcfg.ResolveTuning over cmd's localSource.
func resolveMutationTuning(p *project.Project, root string) (mutationTuning, error) {
	return mutationcfg.ResolveTuning(p, root, localSource(root))
}

// remoteMutationTools is mutationcfg.Tools under its former cmd name.
func remoteMutationTools(p *project.Project) ([]string, map[string]string) {
	return mutationcfg.Tools(p)
}

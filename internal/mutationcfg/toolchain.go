package mutationcfg

import (
	"os/exec"

	"github.com/Rivil/dross/internal/project"
)

// LookPath is the PATH lookup seam. internal/cmd rebinds its own seam var to
// it, so a doctor test can drive both arms without depending on what the
// developer happens to have installed.
var LookPath = exec.LookPath

// Tools returns the binaries the project's adapters need, in roster order,
// plus which adapter needs each — so a missing binary can name the adapter
// that wanted it rather than leaving the user to guess. Only the adapters the
// project actually runs are listed: a Go-only repo has no business failing
// doctor because the mutation host has no dotnet.
func Tools(p *project.Project) ([]string, map[string]string) {
	var tools []string
	needBy := map[string]string{}
	for _, e := range selected(p) {
		if _, seen := needBy[e.tool]; seen {
			continue
		}
		tools = append(tools, e.tool)
		needBy[e.tool] = e.name
	}
	return tools, needBy
}

// Gap is one toolchain binary an adapter needs and the machine lacks: what
// is absent, which adapter wanted it, what goes unmeasured without it, and
// how to get it. A diagnostic that names a gap without naming the fix sends
// the reader searching.
type Gap struct {
	Tool     string
	Adapter  string
	Language string
	Install  string
}

// Missing probes the project's adapter tools through lookPath and returns the
// gaps in roster order. An empty result means every configured adapter can
// run on this machine.
func Missing(p *project.Project, lookPath func(string) (string, error)) []Gap {
	var gaps []Gap
	seen := map[string]bool{}
	for _, e := range selected(p) {
		if seen[e.tool] {
			continue
		}
		seen[e.tool] = true
		if _, err := lookPath(e.tool); err != nil {
			gaps = append(gaps, Gap{Tool: e.tool, Adapter: e.name, Language: e.language, Install: e.install})
		}
	}
	return gaps
}

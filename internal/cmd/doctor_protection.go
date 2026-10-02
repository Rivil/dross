package cmd

import (
	"fmt"

	"github.com/Rivil/dross/internal/configenum"
	"github.com/Rivil/dross/internal/diag"
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/protect"
	"github.com/Rivil/dross/internal/ship"
)

// protectionSection is doctor's "Branch protection:" block (phase
// main-branch-protection, c-3): main's live rules on GitHub, assessed against
// the jobs of origin/<main>'s pull_request workflows, one ⚠ per gap ending in
// the fix. present is false with no [remote], so the section is omitted as the
// other remote-only ones are.
//
// Locked decision doctor_reach: GitHub only. Any other forge is
// `not checked (<provider>)`; a GitHub answer dross can't get — no
// origin/<main> to read workflows from, a workflow protect can't read, an
// unreachable or unauthenticated API — is `unknown (<reason>)`, never ✓. None
// of it moves the exit code: /dross-ship gates on doctor.
func protectionSection(root, repoDir string, p *project.Project) (sec diag.Section, present bool) {
	provider := configenum.Normalize(p.Remote.Provider)
	if provider == "" {
		return diag.Section{}, false
	}
	sec.Heading = "Branch protection:"
	if provider != "github" {
		sec.Lines = []doctorLine{{Level: diag.Note, Text: "  " + notChecked(provider) + " — branch protection is read from GitHub only"}}
		return sec, true
	}
	main := p.Repo.GitMainBranch
	if main == "" {
		main = "main"
	}
	unknown := func(reason string) (diag.Section, bool) {
		sec.Lines = []doctorLine{{Level: doctorWarn, Text: "unknown (" + reason + ")"}}
		return sec, true
	}

	workflows, err := originWorkflows(repoDir, main)
	if err != nil {
		return unknown(err.Error())
	}
	jobs, err := protect.PullRequestJobs(workflows)
	if err != nil {
		return unknown(err.Error()) // a *protect.Refusal: "<workflow>/<job>: <reason>"
	}
	contexts := protect.Contexts(jobs)

	hosts, err := remotePolicy(root, repoDir, p)
	if err != nil {
		return unknown(err.Error())
	}
	res := ship.BranchRulesFunc(buildOpenOpts(p, hosts), main)
	if !res.Known {
		return unknown(res.Reason)
	}
	gaps := protect.Assess(res.Rules, res.Rulesets, contexts)
	if len(gaps) == 0 {
		sec.Lines = []doctorLine{{Level: doctorOK, Text: fmt.Sprintf(
			"%s is protected — changes only through a PR, all %d pull_request job checks required, no force push, no deletion, no bypass", main, len(contexts))}}
		return sec, true
	}
	for _, g := range gaps {
		sec.Lines = append(sec.Lines, doctorLine{Level: doctorWarn, Text: fmt.Sprintf("%s: %s — fix: %s", main, g, ProtectFixHint)})
	}
	return sec, true
}

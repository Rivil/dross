package cmd

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/configenum"
	"github.com/Rivil/dross/internal/gitrun"
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/protect"
	"github.com/Rivil/dross/internal/ship"
)

// ProtectFixHint is the fix a branch-protection gap names: doctor ends each
// gap line with it.
const ProtectFixHint = "run `dross protect` to preview the rulesets, then `dross protect --apply`"

// Protect is `dross protect`: the branch protection dross wants on GitHub —
// a "dross: main" ruleset requiring every pull_request job's check on the main
// branch, and a "dross: milestones" ruleset requiring a PR into milestone/*,
// both with no bypass, admin included.
//
// It previews by default and writes only with --apply (locked decision
// apply_verb). --check <branch> answers the one question prompts ask before
// pushing: are direct pushes to that branch refused?
func Protect() *cobra.Command {
	var (
		apply bool
		check string
	)
	c := &cobra.Command{
		Use:   "protect",
		Short: "Preview, apply or check the branch-protection rulesets for main and milestone/*",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if apply && cmd.Flags().Changed("check") {
				return errors.New("--apply and --check don't combine: --check only reads")
			}
			env, err := loadProtectEnv()
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("check") {
				return protectCheck(env, check)
			}
			if env.provider != "github" {
				Print(notChecked(env.provider) + " — branch protection is applied on GitHub only")
				if apply {
					return fmt.Errorf("nothing applied: %s", notChecked(env.provider))
				}
				return nil
			}
			return protectPreviewOrApply(env, apply)
		},
	}
	c.Flags().BoolVar(&apply, "apply", false, "write both rulesets and turn on allow_auto_merge (default: preview only)")
	c.Flags().StringVar(&check, "check", "", "print whether direct pushes to `branch` are refused: protected, unprotected, unknown or not checked (<provider>)")
	return c
}

// protectEnv is what every mode of `dross protect` reads from the project.
type protectEnv struct {
	repoDir  string
	main     string
	provider string // normalised; "" when there is no [remote]
	opts     ship.OpenOpts
}

func loadProtectEnv() (protectEnv, error) {
	root, err := FindRoot()
	if err != nil {
		return protectEnv{}, err
	}
	repoDir := filepath.Dir(root)
	p, err := project.Load(filepath.Join(root, project.File))
	if err != nil {
		return protectEnv{}, err
	}
	main := p.Repo.GitMainBranch
	if main == "" {
		main = "main"
	}
	hosts, err := remotePolicy(root, repoDir, p)
	if err != nil {
		return protectEnv{}, err
	}
	return protectEnv{repoDir: repoDir, main: main, provider: configenum.Normalize(p.Remote.Provider), opts: buildOpenOpts(p, hosts)}, nil
}

// notChecked is the answer for a forge dross doesn't read protection from
// (locked decision doctor_reach).
func notChecked(provider string) string {
	if provider == "" {
		return "not checked (no remote)"
	}
	return "not checked (" + provider + ")"
}

// protectCheck prints exactly one of protected / unprotected / unknown /
// not checked (<provider>) and exits 0 whatever the answer: the caller is a
// prompt choosing a route, and every answer is a route. It asks only whether a
// direct push is refused — no required-check comparison, so a milestone
// branch under a PR-only ruleset is protected.
func protectCheck(env protectEnv, branch string) error {
	if branch == "" {
		return errors.New("--check needs a branch name")
	}
	if env.provider != "github" {
		Print(notChecked(env.provider))
		return nil
	}
	//dross:taint-cleared BranchRules decodes gh's --json answer into typed rule records — rule types, ruleset ids, names, enforcement and bypass states, check contexts — forge metadata, not gh's prose
	res := ship.BranchRulesFunc(env.opts, branch)
	switch {
	case !res.Known:
		fmt.Fprintf(os.Stderr, "dross protect --check %s: %s\n", branch, res.Reason)
		Print("unknown")
	case protect.RequiresPR(res.Rules):
		Print("protected")
	default:
		Print("unprotected")
	}
	return nil
}

// originWorkflows reads every .github/workflows/*.{yml,yaml} on the local
// origin/<main> ref. Required checks come from main as it is on origin, never
// from the working tree: a job that hasn't merged yet would be a required
// check no PR can satisfy.
func originWorkflows(repoDir, main string) (map[string]string, error) {
	ref := "refs/remotes/origin/" + main
	if gitrun.Quiet(repoDir, gitRefArgs("rev-parse", []string{"--verify", "--quiet"}, ref)...) != nil {
		return nil, fmt.Errorf("no origin/%s to read workflows from — run git fetch", main)
	}
	list, err := gitrun.Read(repoDir, gitRefPathArgs("ls-tree", []string{"--name-only"}, []string{ref}, ".github/workflows/")...)
	if err != nil {
		return nil, fmt.Errorf("git ls-tree origin/%s: %w", main, err)
	}
	out := map[string]string{}
	//dross:taint-cleared ls-tree --name-only prints one repo path per line under .github/workflows/, and name is one of them
	for _, name := range strings.Split(list, "\n") {
		if path.Dir(name) != ".github/workflows" || (path.Ext(name) != ".yml" && path.Ext(name) != ".yaml") {
			continue
		}
		//dross:taint-cleared git show <ref>:<path> prints a committed workflow file — repository content, the bytes os.ReadFile gives for the working tree — read only for its triggers and job keys
		src, err := gitrun.Raw(repoDir, gitRefArgs("show", nil, ref+":"+name)...)
		if err != nil {
			return nil, fmt.Errorf("git show origin/%s:%s: %w", main, name, err)
		}
		out[name] = src
	}
	return out, nil
}

// worktreeWorkflows reads the working tree's workflows, for the preview's
// "not on main yet" list only.
func worktreeWorkflows(repoDir string) map[string]string {
	out := map[string]string{}
	for _, pattern := range []string{".github/workflows/*.yml", ".github/workflows/*.yaml"} {
		matches, _ := filepath.Glob(filepath.Join(repoDir, filepath.FromSlash(pattern)))
		for _, m := range matches {
			if b, err := os.ReadFile(m); err == nil {
				rel, _ := filepath.Rel(repoDir, m)
				out[filepath.ToSlash(rel)] = string(b)
			}
		}
	}
	return out
}

// protectPlan is what `dross protect` would write.
type protectPlan struct {
	main, milestones protect.Ruleset
	contexts         []string
	pending          []string // pull_request job contexts in the working tree that origin/<main> lacks
}

func buildProtectPlan(env protectEnv) (protectPlan, error) {
	if err := gitrun.Run(env.repoDir, gitRefArgs("fetch", []string{"-q"}, "origin", env.main)...); err != nil {
		return protectPlan{}, fmt.Errorf("git fetch origin %s: %w", env.main, err)
	}
	workflows, err := originWorkflows(env.repoDir, env.main)
	if err != nil {
		return protectPlan{}, err
	}
	jobs, err := protect.PullRequestJobs(workflows)
	if err != nil {
		return protectPlan{}, fmt.Errorf("origin/%s's workflows: %w — nothing written", env.main, err)
	}
	contexts := protect.Contexts(jobs)
	mainRS, err := protect.MainRuleset(env.main, contexts)
	if err != nil {
		return protectPlan{}, err
	}
	plan := protectPlan{main: mainRS, milestones: protect.MilestoneRuleset(), contexts: contexts}
	if local, err := protect.PullRequestJobs(worktreeWorkflows(env.repoDir)); err == nil {
		for _, ctx := range protect.Contexts(local) {
			if !slices.Contains(contexts, ctx) {
				plan.pending = append(plan.pending, ctx)
			}
		}
	}
	return plan, nil
}

func protectPreviewOrApply(env protectEnv, apply bool) error {
	plan, err := buildProtectPlan(env)
	if err != nil {
		return err
	}
	settings := ship.RepoMergeSettingsFunc(env.opts)
	existing, listErr := ship.ListRulesetsFunc(env.opts)

	if apply {
		Printf("dross protect --apply on %s\n\n", env.opts.URL)
	} else {
		Printf("dross protect — preview: nothing is written (re-run with --apply)\n\n")
	}
	Printf("%s → refs/heads/%s%s\n", protect.MainRulesetName, env.main, rulesetFate(plan.main.Name, existing, listErr))
	Printf("  no deletion, no force push; changes only through a PR (0 approvals; merge or squash); no bypass, admin included\n")
	Printf("  required checks, from origin/%s's pull_request workflows:\n", env.main)
	for _, c := range plan.contexts {
		Printf("    - %s\n", c)
	}
	if len(plan.pending) > 0 {
		Printf("  not on %s yet — in the working tree only, required once it merges and protect re-runs:\n", env.main)
		for _, c := range plan.pending {
			Printf("    - %s\n", c)
		}
	}
	Printf("%s → %s%s\n", protect.MilestoneRulesetName, protect.MilestoneRef, rulesetFate(plan.milestones.Name, existing, listErr))
	Printf("  changes only through a PR (0 approvals; merge or squash); no bypass; deletion allowed, so --finalize can delete the branch\n")
	switch {
	case !settings.Known:
		Printf("repo setting allow_auto_merge: unknown (%s) → on\n", settings.Reason)
	case settings.AllowAutoMerge:
		Printf("repo setting allow_auto_merge: on already\n")
	default:
		Printf("repo setting allow_auto_merge: off → on\n")
	}
	if settings.Known && !settings.AllowMergeCommit {
		Printf("⚠ allow_merge_commit is off: chore PRs and milestone PRs merge as merge commits and can't land without it — " +
			"turn on \"Allow merge commits\" in the repo's settings (dross never changes it)\n")
	}
	if !apply {
		return nil
	}
	return protectApply(env, plan)
}

// rulesetFate says what --apply would do to the ruleset called name.
func rulesetFate(name string, existing []ship.RulesetSummary, listErr error) string {
	if listErr != nil {
		return "  (existing rulesets unreadable — " + listErr.Error() + ")"
	}
	for _, s := range existing {
		if s.Name == name {
			return fmt.Sprintf("  (updates ruleset #%d)", s.ID)
		}
	}
	return "  (creates it)"
}

// protectApply writes both rulesets and allow_auto_merge, then reads main
// back. Any shortfall exits non-zero naming what did and didn't apply.
func protectApply(env protectEnv, plan protectPlan) error {
	Print("")
	mainID, created, err := ship.UpsertRuleset(env.opts, plan.main)
	if err != nil {
		return fmt.Errorf("nothing applied: %s: %w", protect.MainRulesetName, err)
	}
	Printf("applied %s (%s #%d)\n", protect.MainRulesetName, createdOrUpdated(created), mainID)
	msID, created, err := ship.UpsertRuleset(env.opts, plan.milestones)
	if err != nil {
		return fmt.Errorf("partly applied: %s is in place (#%d), but %s is not: %w — re-run `dross protect --apply`",
			protect.MainRulesetName, mainID, protect.MilestoneRulesetName, err)
	}
	Printf("applied %s (%s #%d)\n", protect.MilestoneRulesetName, createdOrUpdated(created), msID)
	if err := ship.SetAllowAutoMergeFunc(env.opts, true); err != nil {
		return fmt.Errorf("partly applied: both rulesets are in place, but allow_auto_merge is not on: %w — re-run `dross protect --apply`", err)
	}
	Printf("allow_auto_merge: on\n")

	//dross:taint-cleared BranchRules decodes gh's --json answer into typed rule records — rule types, ruleset ids, names, enforcement and bypass states, check contexts — forge metadata, not gh's prose
	res := ship.BranchRulesFunc(env.opts, env.main)
	if !res.Known {
		return fmt.Errorf("applied, but %s's rules could not be read back: %s", env.main, res.Reason)
	}
	gaps := protect.Assess(res.Rules, res.Rulesets, plan.contexts)
	if len(gaps) > 0 {
		for _, g := range gaps {
			Printf("⚠ %s: %s\n", env.main, g)
		}
		return fmt.Errorf("applied, but %s reads back with %d gap(s) (above)", env.main, len(gaps))
	}
	Printf("read back %s: protected — every required check in place, no bypass\n", env.main)
	return nil
}

func createdOrUpdated(created bool) string {
	if created {
		return "created"
	}
	return "updated"
}

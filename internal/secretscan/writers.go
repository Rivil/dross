package secretscan

import (
	"fmt"
	"strings"
)

// The writer registry: every non-test file under internal/ that reaches a
// file-write primitive (os.WriteFile, os.Create, os.CreateTemp, os.OpenFile
// with a write flag, pathfence.WriteFile), and where what it writes stands
// with respect to the .dross secret scan (criterion c-4 of secret-detection).
//
// Three dispositions, one per entry:
//
//   - UnderDross: the artifact lands under .dross/ and is therefore covered by
//     scanDrossArtifacts' location-based scope — tracked files plus stageable
//     untracked ones. No writer has to call the scanner; the walk reaches the
//     file by where it lives, and the enum test proves each artifact name is
//     reached in both walker modes.
//   - MachineLocal: the artifact lands under .dross/ but a seeded .gitignore
//     line keeps it out of every commit, so it can never ride `git add .dross`
//     into history. IgnoreSeed names the seed; the enum test proves the claim
//     with `git check-ignore` in a repo that seed prepared. ONLY the two
//     paths ensureDrossGitignore seeds qualify: handoff.md, reap-log.json and
//     the security/quality/techdebt run dirs are ignored by this repo's own
//     hand-edited .gitignore, not by anything dross scaffolds, so in a fresh
//     repo they are stageable and the scanner reaches them — they are
//     UnderDross, and their entries say so.
//   - OutsideDross: the write does not land under .dross/ at all — the repo
//     root, ~/.claude, a temp dir, the binary — so it is not a .dross
//     artifact. Why says what it is; Paths lets the enum test check none of
//     them resolves under .dross/.
//
// Artifacts written by an agent rather than by Go code (panel notes, REVIEW.md,
// pr-body.md, the criterion notes in verify.toml) are declared under the
// AgentAuthored sentinel: they are in the scanner's reach like any other
// UnderDross artifact, and the stale-declaration arm skips resolving them to a
// Go file.
//
// Artifact paths are .dross-relative slash paths. <id>, <v>, <run> and <name>
// are placeholders the enum test fills with a plausible value.

// AgentAuthored is the File of the entry that declares artifacts no Go writer
// produces.
const AgentAuthored = "agent"

// Dispositions. Exactly one is non-nil on a well-formed entry.
type (
	// UnderDross marks a writer whose artifacts land under .dross/ and are
	// reached by the scanner's location-based walk.
	UnderDross struct {
		Artifacts []string
	}

	// MachineLocal marks a writer whose artifact lands under .dross/ but is
	// kept out of git by a seeded ignore line.
	MachineLocal struct {
		Path       string
		IgnoreSeed string
		Why        string
	}

	// OutsideDross marks a writer whose output is not a .dross artifact.
	OutsideDross struct {
		Paths []string
		Why   string
	}
)

// Writer is one declared writer file.
type Writer struct {
	File string // repo-relative Go file, or AgentAuthored

	UnderDross   *UnderDross
	MachineLocal *MachineLocal
	OutsideDross *OutsideDross
}

// Writers returns the registry. The walker judges it in both directions.
func Writers() []Writer { return append([]Writer(nil), writers...) }

// ArtifactNames returns every artifact path the registry declares, across all
// three dispositions, so a test can match persisted-file constants against it.
func ArtifactNames(in []Writer) []string {
	var out []string
	for _, w := range in {
		switch {
		case w.UnderDross != nil:
			out = append(out, w.UnderDross.Artifacts...)
		case w.MachineLocal != nil:
			out = append(out, w.MachineLocal.Path)
		case w.OutsideDross != nil:
			out = append(out, w.OutsideDross.Paths...)
		}
	}
	return out
}

// ValidateWriters reports every way in which a registry is malformed. It takes
// the slice so a test can feed it synthetic bad entries.
func ValidateWriters(in []Writer) []error {
	var errs []error
	seen := map[string]bool{}
	for _, w := range in {
		name := w.File
		if strings.TrimSpace(name) == "" {
			errs = append(errs, fmt.Errorf("entry with empty File"))
		}
		set := 0
		for _, on := range []bool{w.UnderDross != nil, w.MachineLocal != nil, w.OutsideDross != nil} {
			if on {
				set++
			}
		}
		switch {
		case set == 0:
			errs = append(errs, fmt.Errorf("entry %q: no disposition — every writer lands under .dross, is machine-local under .dross, or is outside .dross", name))
		case set > 1:
			errs = append(errs, fmt.Errorf("entry %q: %d dispositions set, want exactly one", name, set))
		case w.UnderDross != nil:
			if len(w.UnderDross.Artifacts) == 0 {
				errs = append(errs, fmt.Errorf("entry %q: under-dross with no Artifacts — then nothing proves the scanner reaches what it writes", name))
			}
			for _, a := range w.UnderDross.Artifacts {
				if strings.HasPrefix(a, ".dross/") || strings.HasPrefix(a, "/") {
					errs = append(errs, fmt.Errorf("entry %q: artifact %q must be .dross-relative", name, a))
				}
			}
		case w.MachineLocal != nil:
			if strings.TrimSpace(w.MachineLocal.Path) == "" {
				errs = append(errs, fmt.Errorf("entry %q: machine-local with no Path", name))
			}
			if strings.TrimSpace(w.MachineLocal.IgnoreSeed) == "" {
				errs = append(errs, fmt.Errorf("entry %q: machine-local with no IgnoreSeed — the claim is only as good as the seed that keeps it out of git", name))
			}
			if strings.TrimSpace(w.MachineLocal.Why) == "" {
				errs = append(errs, fmt.Errorf("entry %q: machine-local with no Why", name))
			}
		case w.OutsideDross != nil:
			if strings.TrimSpace(w.OutsideDross.Why) == "" {
				errs = append(errs, fmt.Errorf("entry %q: outside-dross with no Why — an unfalsifiable claim", name))
			}
		}
		if name == AgentAuthored && w.UnderDross == nil {
			errs = append(errs, fmt.Errorf("entry %q: agent-authored artifacts are under .dross by definition", name))
		}
		if seen[name] {
			errs = append(errs, fmt.Errorf("entry %q: declared twice", name))
		}
		seen[name] = true
	}
	return errs
}

// ignoreSeed is the one seed dross itself scaffolds: drossIgnoreEntries,
// written by ensureDrossGitignore at init and onboard.
const ignoreSeed = "cmd.ensureDrossGitignore (drossIgnoreEntries)"

// userSettings is the Claude user-level settings file several writers merge
// into.
const userSettings = "~/.claude/settings.json"

var writers = []Writer{
	// ---- UnderDross: reached by the location-based scan ------------------
	{File: "internal/board/board.go", UnderDross: &UnderDross{Artifacts: []string{"board.json"}}},
	{File: "internal/changes/changes.go", UnderDross: &UnderDross{Artifacts: []string{"phases/<id>/changes.json"}}},
	{File: "internal/milestone/milestone.go", UnderDross: &UnderDross{Artifacts: []string{"milestones/<v>.toml"}}},
	// deferred.toml is Spec-shaped and saved through Spec.Save.
	{File: "internal/phase/phase.go", UnderDross: &UnderDross{Artifacts: []string{"phases/<id>/spec.toml", "phases/<id>/plan.toml", "deferred.toml"}}},
	{File: "internal/project/project.go", UnderDross: &UnderDross{Artifacts: []string{"project.toml"}}},
	{File: "internal/profile/profile.go", UnderDross: &UnderDross{Artifacts: []string{"profile.toml"}}},
	{File: "internal/rules/rules.go", UnderDross: &UnderDross{Artifacts: []string{"rules.toml"}}},
	{File: "internal/survivor/store.go", UnderDross: &UnderDross{Artifacts: []string{"survivors.toml"}}},
	{File: "internal/verify/verify.go", UnderDross: &UnderDross{Artifacts: []string{"phases/<id>/tests.json", "phases/<id>/verify.toml"}}},
	{File: "internal/watch/watch.go", UnderDross: &UnderDross{Artifacts: []string{"watch.state.json"}}},
	// The red-proof repoint rewrites the doc changes.json pins, which is any
	// repo-contained path; its plausible home is the phase dir.
	{File: "internal/cmd/redproof_repoint.go", UnderDross: &UnderDross{Artifacts: []string{"phases/<id>/proof.md"}}},
	// Ignored by this repo's own .gitignore, NOT by a dross seed: stageable in
	// any other repo, so in scope.
	{File: "internal/cmd/pause.go", UnderDross: &UnderDross{Artifacts: []string{"handoff.md"}}},
	{File: "internal/reaplog/reaplog.go", UnderDross: &UnderDross{Artifacts: []string{"reap-log.json"}}},
	{File: "internal/cmd/security.go", UnderDross: &UnderDross{Artifacts: []string{"security/<run>/report.md"}}},
	{File: "internal/security/findings.go", UnderDross: &UnderDross{Artifacts: []string{"security/<run>/findings.toml"}}},
	{File: "internal/security/gitleaks.go", UnderDross: &UnderDross{Artifacts: []string{"security/<run>/gitleaks.toml"}}},
	{File: "internal/cmd/quality.go", UnderDross: &UnderDross{Artifacts: []string{"quality/<run>/report.md"}}},
	{File: "internal/quality/findings.go", UnderDross: &UnderDross{Artifacts: []string{"quality/<run>/findings.toml"}}},
	{File: "internal/techdebt/run.go", UnderDross: &UnderDross{Artifacts: []string{"techdebt/<run>/report.md"}}},
	{File: "internal/findings/state.go", UnderDross: &UnderDross{Artifacts: []string{"security/state.toml", "quality/state.toml", "techdebt/state.toml"}}},
	{File: AgentAuthored, UnderDross: &UnderDross{Artifacts: []string{
		"phases/<id>/panel/<name>.md", "phases/<id>/REVIEW.md", "phases/<id>/pr-body.md", "phases/<id>/notes.md",
	}}},

	// ---- MachineLocal: under .dross, kept out of git by the dross seed -----
	{File: "internal/state/state.go", MachineLocal: &MachineLocal{
		Path: "state.json", IgnoreSeed: ignoreSeed,
		Why: "machine-local position and history; tracking it let a checkout replay a stale copy (locked state_tracking)",
	}},
	{File: "internal/localstore/store.go", MachineLocal: &MachineLocal{
		Path: "local.toml", IgnoreSeed: ignoreSeed,
		Why: "host allowlist additions and quick_base; a committed copy would let a repo authorize its own API host",
	}},

	// ---- OutsideDross: not a .dross artifact ------------------------------
	{File: "internal/cmd/architecture.go", OutsideDross: &OutsideDross{Paths: []string{"ARCHITECTURE.md"}, Why: "repo-root doc ship merges landmarks into"}},
	{File: "internal/cmd/init.go", OutsideDross: &OutsideDross{Paths: []string{"ARCHITECTURE.md"}, Why: "seeds the repo-root ARCHITECTURE.md skeleton"}},
	{File: "internal/cmd/gitattributes.go", OutsideDross: &OutsideDross{Paths: []string{".gitattributes"}, Why: "repo-root git config line"}},
	{File: "internal/cmd/gitignore.go", OutsideDross: &OutsideDross{Paths: []string{".gitignore"}, Why: "repo-root ignore seed"}},
	{File: "internal/hooks/settings.go", OutsideDross: &OutsideDross{Paths: []string{userSettings}, Why: "env var block in the user-level Claude settings"}},
	{File: "internal/cmd/hooks.go", OutsideDross: &OutsideDross{Paths: []string{userSettings}, Why: "hook wiring in the user-level Claude settings"}},
	{File: "internal/cmd/statusline.go", OutsideDross: &OutsideDross{Paths: []string{userSettings}, Why: "statusline block in the user-level Claude settings"}},
	{File: "internal/cmd/install.go", OutsideDross: &OutsideDross{Paths: []string{"~/.claude/skills/<name>/SKILL.md", "~/.claude/dross/prompts/<name>.md"}, Why: "embedded skills and prompts re-linked into ~/.claude"}},
	{File: "internal/defaults/defaults.go", OutsideDross: &OutsideDross{Paths: []string{"~/.claude/dross/defaults.toml"}, Why: "user-level defaults under ~/.claude/dross"}},
	{File: "internal/telemetry/telemetry.go", OutsideDross: &OutsideDross{Paths: []string{"~/.claude/dross/telemetry.jsonl"}, Why: "append-only user-level telemetry log; never under a repo"}},
	{File: "internal/update/update.go", OutsideDross: &OutsideDross{Paths: []string{"<bin>/dross"}, Why: "the verified release binary, swapped in beside the running one"}},
	{File: "internal/remote/remote.go", OutsideDross: &OutsideDross{Paths: []string{"<tmp>/dross-rsync-exclude-*"}, Why: "an rsync exclude list in the OS temp dir, removed after the transfer"}},
	{File: "internal/compilefence/compilefence.go", OutsideDross: &OutsideDross{Paths: []string{"<tmp>/module/*.go"}, Why: "a throwaway build module in t.TempDir for must-not-compile fixtures"}},
	{File: "internal/cmd/root.go", OutsideDross: &OutsideDross{Why: "MustWriteFile is a generic parent-creating helper with no callers outside tests; it names no artifact of its own"}},
	{File: "internal/pathfence/pathfence.go", OutsideDross: &OutsideDross{Why: "the contained-write primitive itself: it writes whatever contained path a caller hands it, and the caller's own entry names the artifact"}},
}

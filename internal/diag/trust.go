package diag

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/Rivil/dross/internal/consent"
	"github.com/Rivil/dross/internal/hostallow"
	"github.com/Rivil/dross/internal/project"
)

// EndOfOptionsMinGit is the git version that introduced --end-of-options,
// which every rewritten call site now emits. Below it, those argv are
// unparseable — so this is not advice, it is the floor the locked
// ref_separator_token decision depends on and nothing else enforces.
const EndOfOptionsMinGit = "2.24"

// TrustInputs is everything ConfigTrust needs that only internal/cmd can
// read: the machine-local host allowlist (or the refusal reading it), the
// git version, the consent store, the gitignore matcher, the ref validator
// and the roster of gated commands the exec-consent section names.
type TrustInputs struct {
	AllowHosts    []string
	AllowHostsErr error
	// GitVersion returns `git --version`'s output. A seam, so a test can
	// drive the version floor on a machine whose git is new enough.
	GitVersion func() (string, error)
	Grants     consent.Store
	// IgnoresPath reports whether a .gitignore body ignores target.
	IgnoresPath func(body, target string) bool
	// LocalIgnorePath is the store's path as .gitignore should carry it.
	LocalIgnorePath string
	// ValidateRef reports why a configured branch name is not a git ref.
	ValidateRef func(kind, name string) error
	// GatedCommands is the roster the whole-suite grant authorizes.
	GatedCommands []string
}

// ConfigTrust reports hostile-or-broken config BEFORE a command refuses
// mid-run: branch names git would reject, API hosts outside the derived
// allowlist, an untracked-but-not-ignored local store, a git too old for
// --end-of-options, and the exec-consent state of the test command and every
// lane. It returns the sections in print order and the number of issues.
//
// Every finding that costs counts as an issue, not a warning. A finding
// printed without moving doctor's exit code is a finding nobody acts on — and
// for two of these the alternative to acting is a command dying halfway
// through a branch operation, or a token going somewhere the user never chose.
func ConfigTrust(root, repoDir string, p *project.Project, in TrustInputs) ([]Section, int) {
	sections := []Section{
		branchNames(p, in),
		apiHosts(p, in),
		localStore(repoDir, in),
		gitVersion(in),
		execConsent(root, repoDir, p, in),
	}
	return sections, Issues(sections)
}

// branchNames: 1. branch names git would reject.
func branchNames(p *project.Project, in TrustInputs) Section {
	s := Section{Heading: "Branch names:"}
	checks := []struct{ kind, value string }{
		{"repo.git_main_branch", p.Repo.GitMainBranch},
	}
	// branch_pattern is rendered with a placeholder id rather than read raw:
	// the pattern itself is not a ref, the thing it produces is. Nothing
	// consumes it today (branch names are built as "phase/"+id), which is
	// exactly why it needs reporting — it is broken config that becomes a live
	// vector the day something starts honouring it.
	if bp := p.Repo.BranchPattern; bp != "" {
		rendered := strings.ReplaceAll(bp, "<id>", "example-phase")
		checks = append(checks, struct{ kind, value string }{"repo.branch_pattern", rendered})
	}
	clean := true
	for _, bc := range checks {
		if bc.value == "" {
			continue
		}
		if err := in.ValidateRef(bc.kind, bc.value); err != nil {
			s.Lines = append(s.Lines,
				issue(fmt.Sprintf("%v", err)),
				note(fmt.Sprintf("    Fix: `dross project set %s <name>` — git reads a leading dash as an option, not a branch.", bc.kind)))
			clean = false
		}
	}
	if clean {
		s.Lines = append(s.Lines, ok("configured branch names are valid git refs"))
	}
	return s
}

// apiHosts: 2. API hosts outside the derived allowlist.
func apiHosts(p *project.Project, in TrustInputs) Section {
	s := Section{Heading: "API host:"}
	if in.AllowHostsErr != nil {
		s.Lines = append(s.Lines, issue(fmt.Sprintf("%v", in.AllowHostsErr)))
	}
	policy := hostallow.Derive(p.Remote.URL, in.AllowHosts)
	checks := []struct{ kind, value string }{
		{"[remote].api_base", p.Remote.APIBase},
		{"[board].base_url", p.Board.BaseURL},
	}
	clean := true
	for _, hc := range checks {
		if hc.value == "" {
			continue
		}
		if err := policy.Check(hc.kind, hc.value); err != nil {
			s.Lines = append(s.Lines, issue(fmt.Sprintf("%v", err)))
			// The escape hatch is named here and nowhere else in the runtime
			// paths: a refusal with no way forward is where a legitimate
			// self-hosted user gets stuck and starts editing the guard out.
			if h := hostOf(hc.value); h != "" {
				s.Lines = append(s.Lines, note(fmt.Sprintf("    Fix (only if you trust this host): `dross local set allow_hosts %s`", h)))
			}
			clean = false
		}
	}
	if clean && in.AllowHostsErr == nil {
		s.Lines = append(s.Lines, ok("configured API hosts are within the allowlist derived from [remote].url"))
	}
	return s
}

// localStore: 3. local.toml not gitignored.
//
// Doctor is the ONLY command that runs against already-onboarded repos, which
// never re-run init or onboard. Without this, those repos would never gain the
// ignore line at all.
func localStore(repoDir string, in TrustInputs) Section {
	s := Section{Heading: "Machine-local store:"}
	body, rerr := os.ReadFile(filepath.Join(repoDir, ".gitignore"))
	// if/else-if rather than a tagless switch: go-cover attributes a switch
	// case-condition to no basic block, so a mutant sitting on one is reported
	// NOT-COVERED even when a test drives the arm. An `else if` condition does
	// get a block, so the existing tests can kill it.
	if rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
		s.Lines = append(s.Lines, warn(fmt.Sprintf("couldn't read .gitignore: %v", rerr)))
	} else if !in.IgnoresPath(string(body), in.LocalIgnorePath) {
		s.Lines = append(s.Lines,
			issue(fmt.Sprintf("%s is not gitignored — a committed copy would let a cloned repo authorize its own API host.", in.LocalIgnorePath)),
			note(fmt.Sprintf("    Fix: add `%s` to .gitignore (and `git rm --cached %s` if it is already tracked).", in.LocalIgnorePath, in.LocalIgnorePath)))
	} else {
		s.Lines = append(s.Lines, ok(fmt.Sprintf("%s is gitignored", in.LocalIgnorePath)))
	}
	return s
}

// gitVersion: 4. git too old for --end-of-options.
func gitVersion(in TrustInputs) Section {
	s := Section{Heading: "git version:"}
	raw, gerr := in.GitVersion()
	// if/else-if for the same coverage-attribution reason as localStore.
	if gerr != nil {
		s.Lines = append(s.Lines, warn(fmt.Sprintf("couldn't read `git --version`: %v", gerr)))
	} else if GitVersionAtLeast(raw, EndOfOptionsMinGit) {
		s.Lines = append(s.Lines, ok(fmt.Sprintf("%s supports --end-of-options", strings.TrimSpace(raw))))
	} else {
		s.Lines = append(s.Lines,
			issue(fmt.Sprintf("%s is older than git %s, which introduced --end-of-options.", strings.TrimSpace(raw), EndOfOptionsMinGit)),
			note("    dross places that separator before every config-derived ref, so git will reject those commands. Fix: upgrade git."))
	}
	return s
}

// execConsent: 5. exec consent for runtime.test_command, the gated-surface
// roster, and one row per lane.
//
// The half the locked exec_consent_gate decision admits the CLI cannot enforce
// on its own: the gate refuses at the moment of use, but nothing tells the
// user what state they are in until something has already refused. Doctor is
// where that becomes visible before it bites.
//
// Severity is split deliberately. ABSENT is the honest state of every fresh
// clone and is reported as an advisory with the remedy — failing doctor on it
// would make a clean checkout look broken. STALE is an exit-code issue:
// something WAS trusted here and the command has since changed, which is
// precisely the signature the consent binding exists to catch.
func execConsent(root, repoDir string, p *project.Project, in TrustInputs) Section {
	s := Section{Heading: "Exec consent:"}
	switch state, cerr := consent.CheckConsent(in.Grants, repoDir, p.Runtime.TestCommand); state {
	case consent.Granted:
		s.Lines = append(s.Lines, ok("this machine has trusted the configured test command"))
	case consent.Stale:
		s.Lines = append(s.Lines,
			issue("consent is stale — the test command has CHANGED since it was trusted here:"),
			note("      "+p.Runtime.TestCommand),
			note("    Fix (only after reading that line): `dross trust`"))
	case consent.Refused:
		s.Lines = append(s.Lines, issue(fmt.Sprintf("%v", cerr)))
	case consent.NotApplicable:
		// Lane-aware since lanes gained their own grants: in a lanes-only repo
		// `dross test --files` runs the lanes under those grants and never
		// reaches this gate, so the old wording — "the loop commands refuse" —
		// is false in exactly the repo shape lanes exist to serve, and telling
		// that user to configure a whole-suite command sends them to fix
		// something that is not broken.
		if len(p.Runtime.TestLane) > 0 {
			s.Lines = append(s.Lines,
				warn("no runtime.test_command is configured; `dross test --files` still runs the lanes below."),
				note("    A bare `dross test` has nothing to run — set one only if you want a whole-suite command."))
		} else {
			s.Lines = append(s.Lines,
				warn("no runtime.test_command is configured, so the loop commands refuse."),
				note("    Fix: `dross project set runtime.test_command \"<cmd>\"`, then `dross trust`."))
		}
	default:
		s.Lines = append(s.Lines,
			warn("this machine has not trusted the configured test command:"),
			note("      "+p.Runtime.TestCommand),
			note("    Fix (only after reading that line): `dross trust`"))
	}
	s.Lines = append(s.Lines, gatedSurface(in.GatedCommands)...)
	lanes, _ := LaneConsent(root, repoDir, p, in.Grants)
	s.Lines = append(s.Lines, lanes...)
	return s
}

// gatedSurface says what the whole-suite grant authorizes, and where the
// answer actually comes from.
//
// The roster is READ from the caller rather than restated, and no count is
// printed. A section that spelled the size out in prose was true until the
// day it wasn't, and the reader had no way to tell which day that was.
//
// The exemption marker is named here, beside the state, because that is where
// someone learns the gate exists. An escape hatch documented only in a test
// file is one that gets rediscovered by working around it.
func gatedSurface(commands []string) []Line {
	return []Line{
		note(fmt.Sprintf("  Authorizes the dross commands that spawn a process: %s.", strings.Join(commands, ", "))),
		note("  The gated surface is enumerated from the source, not listed here; a spawn that"),
		note("  cannot reach repo-authored code carries a //dross:exec-exempt <reason> marker."),
	}
}

// LaneConsent renders one row per declared [[runtime.test_lane]], on the same
// state machine and the same severity split as the whole-suite grant, and
// returns the lines with the number of issues among them.
//
// It exists for the timing, not for the information. A lane grant that first
// announces itself by refusing mid-gate is discovered at the worst possible
// moment — after the code is written, while the agent is trying to commit — and
// the refusal arrives per lane, so a repo with four lanes can surface four
// separate surprises across four tasks. Doctor answers the same question in one
// place, before any of it.
//
// A repo with no lanes renders nothing at all: the section would otherwise
// grow a permanent "no lanes configured" line in every repo that never wanted
// them.
func LaneConsent(root, repoDir string, p *project.Project, grants consent.Store) ([]Line, int) {
	var lines []Line
	for _, lane := range p.Runtime.TestLane {
		state, cerr := consent.LaneConsented(grants, repoDir, lane.Name, consent.LaneLine(lane))
		switch state {
		case consent.Granted:
			lines = append(lines, ok(fmt.Sprintf("lane %q: trusted", lane.Name)))
		case consent.Stale:
			// An issue, exactly as the whole-suite stale case is: something
			// WAS trusted under this name and the command has since changed,
			// which is the signature the binding exists to catch.
			lines = append(lines,
				issue(fmt.Sprintf("lane %q: consent is stale — what it runs has CHANGED since it was trusted here:", lane.Name)),
				note("      "+lane.Command))
			lines = append(lines, lanePrepare(lane)...)
			// Named as the fix in every arm that prints lines, prepare
			// included: the state doctor reports and the state that refuses
			// mid-run must agree on what closes it, or a stale prepare would
			// send the reader looking for a second verb that does not exist.
			lines = append(lines, note(fmt.Sprintf("    Fix (only after reading that): `dross trust --lane %s`", lane.Name)))
		case consent.Refused:
			lines = append(lines, issue(fmt.Sprintf("lane %q: %v", lane.Name, cerr)))
		case consent.NotApplicable:
			lines = append(lines,
				warn(fmt.Sprintf("lane %q declares no command, so it can never be trusted or run.", lane.Name)),
				note("    Fix: re-add it with a command, or `dross validate` for the full report."))
		default:
			// Advisory, like the whole-suite ABSENT case: this is the honest
			// state of every fresh clone, and failing doctor on it would make
			// a clean checkout look broken.
			lines = append(lines,
				warn(fmt.Sprintf("lane %q: not trusted on this machine:", lane.Name)),
				note("      "+lane.Command))
			lines = append(lines, lanePrepare(lane)...)
			lines = append(lines, note(fmt.Sprintf("    Fix (only after reading that): `dross trust --lane %s`", lane.Name)))
		}
	}
	return lines, CountIssues(lines)
}

// lanePrepare renders one lane's bootstrap line under its command, and
// nothing at all for a lane declaring none.
//
// Under rather than beside, and only when declared: the same grant covers both
// lines, so a report that showed one of them would understate what the user is
// being asked to trust — while a `prepare: -` row on every pre-existing lane
// would read as something they are expected to go and set.
func lanePrepare(lane project.TestLane) []Line {
	if lane.Prepare == "" {
		return nil
	}
	return []Line{note("      prepare: " + lane.Prepare)}
}

// hostOf extracts host[:port] from a URL for the allow_hosts fix line.
func hostOf(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Hostname() == "" {
		return ""
	}
	if port := u.Port(); port != "" {
		return u.Hostname() + ":" + port
	}
	return u.Hostname()
}

package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/mutation"
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/state"
	"github.com/Rivil/dross/internal/survivor"
	"github.com/Rivil/dross/internal/verify"
)

// The drain is `dross verify`'s complement, and the two differ on purpose:
//
//   - verify asks "did THIS PHASE leave debt behind?" — it scopes to the
//     phase's diff, and a survivor in an untouched sibling is not its problem.
//   - drain asks "does this repo have ANY survivor nobody has decided about?" —
//     no diff scoping at all, over every package.
//
// Only the second question can make "zero unclassified" a true statement about
// standing debt. A package-scoped or diff-scoped drain leaves never-mutated
// packages accumulating silently, which is the state this milestone found the
// repo in.

// goListDirs is the package-discovery seam, overridable so a test can pin the
// default package set without shelling out to the toolchain.
var goListDirs = func(repoRoot string) ([]string, error) {
	cmd := exec.Command("go", "list", "-f", "{{.Dir}}", "./...")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list ./...: %w", err)
	}
	var dirs []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			dirs = append(dirs, line)
		}
	}
	return dirs, nil
}

// drainRunner is the mutation seam: it runs the adapter over pkgs and returns
// the packages it could not measure. Overridable so the classify path is
// testable against recorded reports rather than a live gremlins run.
var drainRunner = runGremlinsOverPackages

// allGoPackages returns every package in the repo as a gremlins package path
// ("." for the module root, "./internal/cmd" otherwise).
func allGoPackages(repoRoot string) ([]string, error) {
	dirs, err := goListDirs(repoRoot)
	if err != nil {
		return nil, err
	}
	pkgs := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		rel, err := filepath.Rel(repoRoot, dir)
		if err != nil {
			return nil, err
		}
		pkgs = append(pkgs, pkgPath(rel))
	}
	sort.Strings(pkgs)
	return pkgs, nil
}

// pkgPath normalises a repo-relative directory to a gremlins package path.
func pkgPath(rel string) string {
	rel = filepath.ToSlash(rel)
	if rel == "" || rel == "." {
		return "."
	}
	return "./" + strings.TrimSuffix(strings.TrimPrefix(rel, "./"), "/")
}

// normalizePackageArg accepts what a user would type — "./internal/cmd/...",
// "internal/cmd", "./internal/cmd" — and returns the concrete package path.
// The "/..." suffix is trimmed rather than expanded: gremlins is invoked per
// concrete package, and a recursive scope makes it gather empty coverage.
func normalizePackageArg(arg string) string {
	arg = strings.TrimSuffix(filepath.ToSlash(arg), "/...")
	arg = strings.TrimSuffix(arg, "/")
	return pkgPath(arg)
}

// expandPackageArgs resolves each argument against the repo's real package
// list, so "./internal/cmd/..." covers the subtree the user meant rather than
// only its root. An argument matching no package is an error — silently
// draining nothing is the failure mode this command exists to prevent.
func expandPackageArgs(repoRoot string, args []string) ([]string, error) {
	all, err := allGoPackages(repoRoot)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for _, arg := range args {
		recursive := strings.HasSuffix(filepath.ToSlash(arg), "/...")
		want := normalizePackageArg(arg)
		matched := false
		for _, pkg := range all {
			hit := pkg == want || (recursive && strings.HasPrefix(pkg, strings.TrimSuffix(want, "/")+"/"))
			if want == "." && recursive {
				hit = true
			}
			if !hit || seen[pkg] {
				continue
			}
			seen[pkg] = true
			out = append(out, pkg)
			matched = true
		}
		if !matched && !seen[want] {
			return nil, fmt.Errorf("no Go package matches %q", arg)
		}
	}
	sort.Strings(out)
	return out, nil
}

// runGremlinsOverPackages runs the configured gremlins adapter once over the
// given packages, leaving one raw report per package on disk. The merged report
// it returns is deliberately discarded: the drain reads the raw files.
func runGremlinsOverPackages(repoRoot string, pkgs []string) ([]mutation.Unmeasured, error) {
	p, err := project.Load(filepath.Join(repoRoot, ".dross", project.File))
	if err != nil {
		return nil, err
	}
	// The SAME resolution verify uses, through the same constructor. The drain
	// running locally while verify ran on the granted remote would classify
	// survivors against a different machine's measurements — and the drain is
	// what decides whether a survivor is real.
	mt, err := resolveMutationTuning(p, filepath.Join(repoRoot, RootDirName))
	if err != nil {
		return nil, err
	}
	g := mt.gremlins(repoRoot, p, profileCacheVars(p, repoRoot))
	// Gremlins derives its package set from the directories of the files it is
	// handed, so one representative path per package is the whole input.
	files := make([]string, 0, len(pkgs))
	for _, pkg := range pkgs {
		dir := strings.TrimPrefix(pkg, "./")
		if dir == "." {
			dir = ""
		}
		files = append(files, filepath.Join(dir, "drain.go"))
	}
	if _, err := g.Run(files); err != nil {
		return nil, err
	}
	return g.Unmeasured, nil
}

// drainSurvivor is one survivor read out of a raw report, with the package it
// came from and what the mutation tool said about its coverage.
type drainSurvivor struct {
	verify.Survivor
	Package string
	// ReportedNotCovered is the tool's own NOT COVERED status. Kept because the
	// attribution ceiling is the DISAGREEMENT between this and go-cover, and a
	// deriver that only saw one of the two could not detect it.
	ReportedNotCovered bool
}

// coverageProfileFn is the coverage seam: it produces a go-cover profile over
// the drained packages. Overridable so the classify path is testable without a
// live test run, and so a caller with no working toolchain degrades to
// "unknown" rather than to a wrong answer.
var coverageProfileFn = runCoverageProfile

// runCoverageProfile runs `go test -coverprofile` over pkgs and parses the
// result. A failure is not fatal: coverage is EVIDENCE, and a drain that
// refused to run without it would be less useful than one that reports
// unknown — as long as unknown never reads as "not covered", which is
// Profile's contract.
func runCoverageProfile(repoRoot string, pkgs []string) *survivor.Profile {
	out := filepath.Join(os.TempDir(), "dross-drain-cover.out")
	args := append([]string{"test", "-count=1", "-coverprofile=" + out}, pkgs...)
	cmd := exec.Command("go", args...)
	cmd.Dir = repoRoot
	// Output is discarded: a failing suite still produces a usable profile for
	// the packages that did run, and the drain is not a test runner.
	_ = cmd.Run()
	prof, err := survivor.ParseProfile(out)
	if err != nil {
		return nil
	}
	return prof
}

// readRawReport parses one gremlins report and returns its survivors with
// repo-relative paths. pkg may be empty, in which case paths are left as the
// tool wrote them.
func readRawReport(path, pkg string) ([]drainSurvivor, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	rep, err := mutation.ParseGremlinsJSON(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if pkg != "" {
		mutation.RePrefixGremlinsFiles(rep, pkg)
	}
	// Ceiling eligibility turns on whether the TOOL called this exact mutant
	// NOT COVERED, so the status is read per mutant from the raw payload.
	// mutation.Report folds LIVED and NOT COVERED into one Surviving list, and
	// a file-granular approximation would let one uncovered mutant grant the
	// ceiling to every other survivor in its file — accepting killable code.
	notCovered, err := notCoveredPositions(b, pkg)
	if err != nil {
		return nil, err
	}

	out := make([]drainSurvivor, 0, len(rep.Surviving))
	for _, m := range rep.Surviving {
		if verify.IsTestdataPath(m.File) {
			continue
		}
		out = append(out, drainSurvivor{
			Survivor:           verify.Survivor{File: m.File, Line: m.Line, Op: m.Op, Language: "go"},
			Package:            pkg,
			ReportedNotCovered: notCovered[mutantPos(m.File, m.Line, m.Op)],
		})
	}
	return out, nil
}

// mutantPos keys a mutant by the triple that identifies it within a report.
func mutantPos(file string, line int, op string) string {
	return file + ":" + strconv.Itoa(line) + ":" + op
}

// rawGremlinsPayload is the minimal view of the report needed to recover each
// mutant's STATUS, which mutation.Report deliberately does not carry (it folds
// LIVED and NOT COVERED into one Surviving list, because for scoring they are
// the same thing). For deciding accept-vs-kill they are opposites.
type rawGremlinsPayload struct {
	Files []struct {
		Filename  string `json:"file_name"`
		Mutations []struct {
			Type   string `json:"type"`
			Status string `json:"status"`
			Line   int    `json:"line"`
		} `json:"mutations"`
	} `json:"files"`
}

// notCoveredPositions returns the set of mutants the tool reported NOT COVERED,
// keyed the same way the survivor list is, with pkg applied so the paths match.
func notCoveredPositions(payload []byte, pkg string) (map[string]bool, error) {
	var raw rawGremlinsPayload
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("read mutant statuses: %w", err)
	}
	prefix := strings.TrimPrefix(filepath.ToSlash(pkg), "./")
	out := map[string]bool{}
	for _, f := range raw.Files {
		name := filepath.ToSlash(f.Filename)
		if prefix != "" && prefix != "." && !strings.HasPrefix(name, prefix+"/") {
			name = prefix + "/" + name
		}
		for _, m := range f.Mutations {
			if m.Status == "NOT COVERED" {
				out[mutantPos(name, m.Line, m.Type)] = true
			}
		}
	}
	return out, nil
}

// The testdata scope rule now lives at verify.IsTestdataPath, so the drain and
// `dross verify` cannot disagree about which files are anybody's debt. Do not
// reintroduce a local copy here — divergence between the two copies is exactly
// what made the same repo state read as 0 unclassified from the drain and 24
// from verify.

// reportStemToPackage inverts the report filename stem back to a package path
// by matching against the repo's real package list. Deriving it by string
// surgery would be ambiguous — a directory named "internal_cmd" and the package
// "./internal/cmd" share a stem — so the answer comes from the packages that
// actually exist.
func reportStemToPackage(repoRoot, path string) string {
	stem := strings.TrimSuffix(filepath.Base(path), ".json")
	all, err := allGoPackages(repoRoot)
	if err != nil {
		return ""
	}
	for _, pkg := range all {
		if filepath.Base(mutation.GremlinsReportPath("", pkg)) == stem+".json" {
			return pkg
		}
	}
	return ""
}

func survivorDrain() *cobra.Command {
	var reports, packages []string
	var phaseID string

	c := &cobra.Command{
		Use:   "drain",
		Short: "Report every survivor with no disposition, across whole packages (no diff scoping)",
		Long: "Classify every surviving mutant in the repo and report the ones carrying no " +
			"disposition — neither accepted with a reason nor routed to a destination. Unlike " +
			"`dross verify`, the drain applies NO diff scoping: it answers \"does this repo have " +
			"standing debt?\", which only a repo-wide run can answer honestly.\n\n" +
			"With --report it classifies recorded gremlins reports; otherwise it runs the " +
			"configured adapter over --packages, or over every Go package in the repo.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			// FIRST, before FindRoot's siblings and before any survivor is
			// gathered. The drain has two spawn seams — goListDirs shells the
			// toolchain, and --report's coverage pass runs `go test
			// -coverprofile` over the repo's own packages — so the gate goes at
			// the RunE top rather than at each seam, where a third seam added
			// later would arrive ungated by default.
			if err := requireExecConsent(); err != nil {
				return err
			}
			root, err := FindRoot()
			if err != nil {
				return err
			}
			repoRoot := filepath.Dir(root)

			if phaseID == "" {
				s, err := state.Load(filepath.Join(root, state.File))
				if err != nil {
					return err
				}
				phaseID = s.CurrentPhase
			}

			survivors, drainedPackages, err := gatherDrainSurvivors(repoRoot, reports, packages)
			if err != nil {
				return err
			}

			store, err := survivor.Load(survivor.Path(root))
			if err != nil {
				return err
			}
			accepted, err := acceptedReasons(store)
			if err != nil {
				return err
			}
			routed, err := routedSurvivors(root)
			if err != nil {
				return err
			}
			// A survivor routed to the phase being drained is outstanding, not
			// routed: this IS its destination. Without this the drain could be
			// closed by re-routing everything to itself — debt with a home that
			// is the room it is standing in.
			selfRouted := 0
			for key, target := range routed {
				if phaseID != "" && target == phaseID {
					delete(routed, key)
					selfRouted++
				}
			}

			plain := make([]verify.Survivor, 0, len(survivors))
			for _, s := range survivors {
				plain = append(plain, s.Survivor)
			}
			life := verify.Classify(plain, accepted, routed, workTreeIdentifier{repoRoot: repoRoot})

			outstanding := append(append([]verify.Classified{}, life.InDiff...), life.Unclassified...)
			sort.Slice(outstanding, func(i, j int) bool {
				if outstanding[i].File != outstanding[j].File {
					return outstanding[i].File < outstanding[j].File
				}
				if outstanding[i].Line != outstanding[j].Line {
					return outstanding[i].Line < outstanding[j].Line
				}
				return outstanding[i].Op < outstanding[j].Op
			})

			Printf("drain: %d survivor(s) classified — %d accepted, %d routed, %d outstanding\n",
				life.Total(), len(life.Accepted), len(life.Routed), len(outstanding))
			if selfRouted > 0 {
				Printf("  (%d routed to %s — this phase's own destination, so still outstanding)\n",
					selfRouted, phaseID)
			}
			if len(outstanding) == 0 {
				Print("0 unclassified")
				return nil
			}

			// Attach the derived evidence, so kill-vs-accept is answered by
			// code rather than by judgement repeated across ~91 near-identical
			// entries. This is the whole reason the drain is worth running
			// before the acceptance work rather than after it.
			prof := coverageProfileFn(repoRoot, drainedPackages)
			reported := map[string]bool{}
			for _, s := range survivors {
				reported[fmt.Sprintf("%s:%d", s.File, s.Line)] = s.ReportedNotCovered
			}
			killable := 0
			for _, o := range outstanding {
				line := fmt.Sprintf("  %s:%d (%s)", o.File, o.Line, o.Op)
				if o.Note != "" {
					line += " — " + o.Note
				}
				Print(line)
				e := survivor.Derive(repoRoot, o.File, o.Line, o.Op, prof,
					reported[fmt.Sprintf("%s:%d", o.File, o.Line)])
				if e.Killable {
					killable++
				}
				Printf("      coverage=%s applicable=%s%s → %s\n",
					e.Coverage, e.Applicable, ceilingTag(e), e.Why)
			}
			if killable > 0 {
				Printf("  %d of these are covered AND operator-applicable: kill them with a test. "+
					"An acceptance on one of those is a coverage gap wearing a reason.\n", killable)
			}
			return fmt.Errorf("%d survivor(s) have no disposition: kill each with a test, "+
				"accept it with `dross survivor accept --reason`, or route it with "+
				"`dross survivor route --target`", len(outstanding))
		},
	}
	c.Flags().StringSliceVar(&reports, "report", nil, "classify a recorded gremlins JSON report instead of running the adapter (repeatable)")
	c.Flags().StringSliceVar(&packages, "packages", nil, "Go packages to drain (default: every package in the repo)")
	c.Flags().StringVar(&phaseID, "phase", "", "phase being drained; a survivor routed here counts outstanding (default: current phase)")
	return c
}

// gatherDrainSurvivors collects survivors from recorded reports or from a live
// adapter run.
//
// An UNMEASURED package is fatal. A package gremlins wrote no report for tells
// us nothing, and "no survivors found" and "never looked" are byte-identical in
// a count — treating absence as clean is precisely how a never-mutated package
// accumulates silently. A package whose report exists but holds only NOT
// COVERED mutants is the opposite case: it WAS measured, and its survivors flow
// into the normal classify path to be killed or accepted like any other.
func gatherDrainSurvivors(repoRoot string, reports, packages []string) ([]drainSurvivor, []string, error) {
	if len(reports) > 0 {
		var out []drainSurvivor
		var pkgs []string
		for _, path := range reports {
			pkg := reportStemToPackage(repoRoot, path)
			found, err := readRawReport(path, pkg)
			if err != nil {
				return nil, nil, err
			}
			if pkg != "" {
				pkgs = append(pkgs, pkg)
			}
			out = append(out, found...)
		}
		return out, pkgs, nil
	}

	var pkgs []string
	var err error
	if len(packages) > 0 {
		pkgs, err = expandPackageArgs(repoRoot, packages)
	} else {
		pkgs, err = allGoPackages(repoRoot)
	}
	if err != nil {
		return nil, nil, err
	}
	if len(pkgs) == 0 {
		// Nothing to do is not an error: a repo with no Go packages has no Go
		// survivors, and erroring here would make the drain unusable as a gate.
		return nil, nil, nil
	}

	unmeasured, err := drainRunner(repoRoot, pkgs)
	if err != nil {
		return nil, nil, err
	}
	var blind []string
	for _, u := range unmeasured {
		if u.Kind == mutation.UnmeasuredUncovered {
			continue // measured, just with no usable coverage — classify its rows
		}
		blind = append(blind, u.Message)
	}
	if len(blind) > 0 {
		return nil, nil, fmt.Errorf("%d package(s) were not measured, so nothing is known about them:\n  %s",
			len(blind), strings.Join(blind, "\n  "))
	}

	var out []drainSurvivor
	for _, pkg := range pkgs {
		path := mutation.GremlinsReportPath(repoRoot, pkg)
		found, err := readRawReport(path, pkg)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, nil, fmt.Errorf("no report for package %s at %s — the package was not measured", pkg, path)
			}
			return nil, nil, err
		}
		out = append(out, found...)
	}
	return out, pkgs, nil
}

// ceilingTag renders the attribution-ceiling marker, empty when it does not
// apply, so the evidence line stays one line.
func ceilingTag(e survivor.Evidence) string {
	if e.CeilingEligible {
		return " ceiling-eligible=yes"
	}
	return ""
}

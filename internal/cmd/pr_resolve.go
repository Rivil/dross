package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/pathfence"
	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/prtriage"
	"github.com/Rivil/dross/internal/secretscan"
)

// resolveFlags is one `dross pr resolve` invocation's verdict and evidence.
type resolveFlags struct {
	seen                        string
	accept, reject, route       bool
	title, description, reason  string
	target                      string
	covers, files, testContract []string
	at, cmd, outputFile         string
}

// triageItemID is the shape of an id `dross pr comments` prints.
var triageItemID = regexp.MustCompile(`^[cir][0-9]+(#[1-9][0-9]*)?$`)

// beforeTriageSave runs between the plan or spec write and the record save.
// Tests use it to make the record save fail and prove the rollback.
var beforeTriageSave = func(recPath string) {}

// mirrorRoute is the board mirror a route runs once its record is saved.
// Tests swap it to count calls.
var mirrorRoute = mirrorDeferredAdd

func prResolve() *cobra.Command {
	var f resolveFlags
	c := &cobra.Command{
		Use:   "resolve <pr> <item-id>",
		Short: "Record one verdict for a phase PR's review comment: accept as a task, reject with a reason, or route as deferred",
		Long: "Records the verdict for comment <item-id> of PR <pr>, as `dross pr comments` listed it " +
			"(--seen carries the digest it printed). Exactly one of --accept (a new task in the PR " +
			"phase's plan), --reject (with --reason) or --route (a deferred item with --target), and " +
			"exactly one form of evidence: --at <path:line> or --cmd <command> with --output-file " +
			"<path|-> holding what it printed. --cmd is recorded, never run.",
		Args: cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			// `dross pr comments` prints the digest as seen=<hex>; take the
			// token whole or bare.
			f.seen = strings.TrimPrefix(strings.TrimSpace(f.seen), "seen=")
			if err := f.check(); err != nil {
				return err
			}
			// Everything typed here can land in plan.toml or spec.toml, which
			// the record's own redaction never reaches.
			f.title, f.description, f.reason = redacted(f.title), redacted(f.description), redacted(f.reason)
			for i := range f.testContract {
				f.testContract[i] = redacted(f.testContract[i])
			}
			for i := range f.files {
				f.files[i] = redacted(f.files[i])
			}
			if _, err := parsePRNumber(args[0]); err != nil {
				return err
			}
			id := args[1]
			if !triageItemID.MatchString(id) {
				return fmt.Errorf("item id %s is invalid — use an id `dross pr comments` printed, like c12 or c12#2", prtriage.OneLine(id))
			}
			root, err := FindRoot()
			if err != nil {
				return err
			}
			ev, err := f.evidence(filepath.Dir(root), c.InOrStdin())
			if err != nil {
				return err
			}
			t, err := loadPRThread(args[0])
			if err != nil {
				return err
			}
			if !t.head.Open {
				return fmt.Errorf("PR #%d is closed — a verdict on it is invalid; triage an open PR", t.pr)
			}
			item, err := t.item(id)
			if err != nil {
				return err
			}
			if f.seen != item.Digest {
				return fmt.Errorf("--seen is invalid: %s changed since listed — re-run `dross pr comments %d` and read it again", id, t.pr)
			}
			if r, ok := t.rec.Find(id); ok && r.Digest == item.Digest {
				return fmt.Errorf("%s already exists in %s with verdict %s and has not changed since — a comment gets one verdict", id, prtriage.File, prtriage.OneLine(r.Verdict))
			}
			return resolveItem(c, t, item, ev, f)
		},
	}
	fl := c.Flags()
	fl.StringVar(&f.seen, "seen", "", "the seen=<digest> value dross pr comments printed for the item (required)")
	fl.BoolVar(&f.accept, "accept", false, "accept: add a task to the PR phase's plan (needs --title)")
	fl.BoolVar(&f.reject, "reject", false, "reject: say why in --reason; the reason goes back to the PR in its reply")
	fl.BoolVar(&f.route, "route", false, "route: file a deferred item for --target (needs --title)")
	fl.StringVar(&f.title, "title", "", "the task's or deferred item's title")
	fl.StringVar(&f.description, "description", "", "accept: the task's description")
	fl.StringSliceVar(&f.covers, "covers", nil, "accept: criterion ids the task covers")
	fl.StringSliceVar(&f.files, "files", nil, "accept: files the task touches")
	fl.StringArrayVar(&f.testContract, "test-contract", nil, "accept: one test-contract line (repeatable)")
	fl.StringVar(&f.reason, "reason", "", "reject: why; route: why it is parked")
	fl.StringVar(&f.target, "target", "", "route: the phase slug the deferred item goes to")
	fl.StringVar(&f.at, "at", "", "evidence: a path:line or path:line-line in the repo")
	fl.StringVar(&f.cmd, "cmd", "", "evidence: the command whose output backs the verdict (recorded, never run)")
	fl.StringVar(&f.outputFile, "output-file", "", "evidence: a file holding what --cmd printed, or - for stdin")
	return c
}

// check refuses a malformed invocation before anything is read or fetched.
func (f resolveFlags) check() error {
	const working = "use one of: --accept --title <t>, --reject --reason <r>, or --route --title <t> --target <slug>"
	n := 0
	for _, v := range []bool{f.accept, f.reject, f.route} {
		if v {
			n++
		}
	}
	switch {
	case n != 1:
		return fmt.Errorf("exactly one verdict is required — %s", working)
	case strings.TrimSpace(f.seen) == "":
		return errors.New("--seen <digest> is required — copy the seen= value `dross pr comments` printed")
	case f.accept && strings.TrimSpace(f.title) == "":
		return fmt.Errorf("--title is required with --accept — %s", working)
	case f.reject && strings.TrimSpace(f.reason) == "":
		return fmt.Errorf("--reason is required with --reject — %s", working)
	case f.route && (strings.TrimSpace(f.title) == "" || strings.TrimSpace(f.target) == ""):
		return fmt.Errorf("--title is required with --route, and so is --target — %s", working)
	case f.at != "" && f.cmd != "":
		return errors.New("evidence is invalid: give --at or --cmd, not both")
	case f.cmd != "" && f.outputFile == "":
		return errors.New("--output-file is required with --cmd — the file (or - for stdin) holding what the command printed")
	case f.at == "" && f.cmd == "":
		return prtriage.ErrNoEvidence
	}
	return nil
}

// evidence builds the one piece of evidence the flags name. --cmd is never
// run: what it printed comes from --output-file.
func (f resolveFlags) evidence(repoDir string, stdin io.Reader) (prtriage.Evidence, error) {
	if f.at != "" {
		ev, err := prtriage.ParseAt(repoDir, f.at)
		if err != nil {
			return prtriage.Evidence{}, fmt.Errorf("invalid --at: %w", err)
		}
		return ev, nil
	}
	var out []byte
	var err error
	if f.outputFile == "-" {
		out, err = io.ReadAll(stdin)
	} else {
		out, err = os.ReadFile(f.outputFile)
	}
	if err != nil {
		return prtriage.Evidence{}, fmt.Errorf("invalid --output-file: %w", err)
	}
	ev, err := prtriage.FromOutput(f.cmd, string(out))
	if err != nil {
		return prtriage.Evidence{}, fmt.Errorf("invalid evidence: %w", err)
	}
	return ev, nil
}

// item is the comment id names on this PR's thread. A split /dross-review
// comment is resolved finding by finding, never whole.
func (t *prThread) item(id string) (prtriage.Item, error) {
	var parts []string
	for _, it := range t.items {
		if it.ID == id {
			return it, nil
		}
		if strings.HasPrefix(it.ID, id+"#") {
			parts = append(parts, it.ID)
		}
	}
	if len(parts) > 0 {
		sort.Strings(parts)
		return prtriage.Item{}, fmt.Errorf("%s is split into findings %s — resolve each finding; %s itself is not found as one item", id, strings.Join(parts, ", "), id)
	}
	return prtriage.Item{}, fmt.Errorf("%s not found on PR #%d — use an id `dross pr comments %d` printed", id, t.pr, t.pr)
}

// resolveItem writes the verdict: the task or deferred item first, then the
// record. If the record save fails, the plan or spec it changed is put back
// byte for byte, and a route mirrors to the board only once the record holds.
func resolveItem(c *cobra.Command, t *prThread, item prtriage.Item, ev prtriage.Evidence, f resolveFlags) error {
	dir, err := phase.ContainID(t.root, t.phase)
	if err != nil {
		return err
	}
	planPath := filepath.Join(dir.String(), "plan.toml")
	specPath := filepath.Join(dir.String(), "spec.toml")
	plan, err := phase.LoadPlan(planPath)
	if err != nil {
		return err
	}
	// A phase planned without a spec has no criteria and no deferred items to
	// point at; only a route needs one, to file its item in.
	spec := &phase.Spec{}
	if _, err := os.Stat(specPath); err == nil {
		if spec, err = phase.LoadSpec(specPath); err != nil {
			return err
		}
	} else if f.route {
		return fmt.Errorf("invalid route: phase %s has no spec.toml to file a deferred item in", t.phase)
	}

	res := prtriage.ResolutionFrom(item)
	res.PR = t.pr
	res.Evidence = ev
	var restore *pathfence.Contained
	var original []byte
	var routed *deferredEntry

	switch {
	case f.accept:
		res.Verdict = prtriage.VerdictAccept
		if restore, original, err = snapshot(dir, "plan.toml"); err != nil {
			return err
		}
		desc := strings.TrimSpace(f.description)
		if desc != "" {
			desc += "\n\n"
		}
		desc += "From review comment " + item.URL
		task, err := plan.AddTask(phase.NewTask{
			Title: f.title, Files: f.files, Covers: f.covers, TestContract: f.testContract, Description: desc,
		}, "", false)
		if err != nil {
			return err
		}
		if err := saveIfValid(plan, spec, planPath); err != nil {
			return fmt.Errorf("invalid task for %s: %w", item.ID, err)
		}
		res.Task = task.ID
	case f.reject:
		res.Verdict = prtriage.VerdictReject
		res.Reason = f.reason
	case f.route:
		res.Verdict = prtriage.VerdictRoute
		if err := validDeferredTarget(t.root, f.target); err != nil {
			return fmt.Errorf("invalid --target: %w", err)
		}
		if err := refuseCompleteTarget(t.root, f.target); err != nil {
			return fmt.Errorf("invalid --target: %w", err)
		}
		id, err := mintDeferredID(t.root)
		if err != nil {
			return err
		}
		if restore, original, err = snapshot(dir, "spec.toml"); err != nil {
			return err
		}
		why := "review comment " + item.URL
		if r := strings.TrimSpace(f.reason); r != "" {
			why = r + " — " + why
		}
		spec.Deferred = append(spec.Deferred, phase.Deferred{ID: id, Text: f.title, Why: why, Target: f.target})
		if err := spec.Save(specPath); err != nil {
			return fmt.Errorf("save %s: %w", specPath, err)
		}
		res.Deferred, res.Target, res.Reason = id, f.target, f.reason
		routed = &deferredEntry{Source: t.phase, Index: len(spec.Deferred) - 1, ID: id, Text: f.title, Why: why, Target: f.target}
	}

	t.rec.Upsert(res)
	beforeTriageSave(t.recPath)
	if err := prtriage.Save(t.recPath, t.recBytes, t.rec, refsOf(plan, spec)); err != nil {
		if restore != nil {
			if rerr := pathfence.WriteFile(*restore, original, 0o644); rerr != nil {
				return fmt.Errorf("%w (and putting %s back failed: %v)", err, restore.Rel(), rerr)
			}
		}
		return err
	}
	if routed != nil {
		mirrorRoute(t.root, *routed)
	}
	fmt.Fprintf(c.OutOrStdout(), "%s → %s", item.ID, res.Verdict)
	switch {
	case res.Task != "":
		fmt.Fprintf(c.OutOrStdout(), " as task %s in phase/%s", res.Task, t.phase)
	case res.Deferred != "":
		fmt.Fprintf(c.OutOrStdout(), " as deferred %s for %s", res.Deferred, prtriage.OneLine(res.Target))
	}
	fmt.Fprintln(c.OutOrStdout())
	return nil
}

// snapshot reads a phase file's bytes so a failed record save can put them
// back exactly.
func snapshot(dir pathfence.Contained, name string) (*pathfence.Contained, []byte, error) {
	p, err := pathfence.Contain(dir.String(), "phase file", name)
	if err != nil {
		return nil, nil, err
	}
	b, err := pathfence.ReadFile(p)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", p.Rel(), err)
	}
	return &p, b, nil
}

// redacted is s with every credential-shaped value replaced.
func redacted(s string) string {
	r, _ := secretscan.Redact(s)
	return r
}

// refsOf is what a resolution may point at in this phase.
func refsOf(plan *phase.Plan, spec *phase.Spec) prtriage.Refs {
	var refs prtriage.Refs
	for _, t := range plan.Task {
		refs.Tasks = append(refs.Tasks, t.ID)
	}
	for _, d := range spec.Deferred {
		refs.Deferred = append(refs.Deferred, d.ID)
	}
	return refs
}

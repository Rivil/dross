package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/state"
	"github.com/Rivil/dross/internal/survivor"
)

// Survivor manages the accepted-survivor registry and routes survivors to a
// destination phase.
//
// The verbs hang off `dross survivor` rather than `dross verify` on purpose
// (the locked cli_namespace decision): the store is a registry whose records
// deliberately outlive the verify run that produced them, and nesting them
// under verify would imply a per-run artifact.
func Survivor() *cobra.Command {
	c := &cobra.Command{
		Use:   "survivor",
		Short: "Accept, route and list surviving mutants (the drain for out-of-diff debt)",
	}
	c.AddCommand(survivorAccept(), survivorRoute(), survivorList(), survivorRetire())
	return c
}

// survivorLocation is a resolved <file>:<line> argument: the path as the caller
// typed it, the repo-relative path the store records, and the line.
type survivorLocation struct {
	repoRoot string
	rel      string
	line     int
}

// parseLocation splits "<file>:<line>" and resolves the file against the
// working directory, returning it repo-relative so a record made from a nested
// subdirectory reads the same as one made from the root.
func parseLocation(root, arg string) (survivorLocation, error) {
	idx := strings.LastIndex(arg, ":")
	if idx < 0 {
		return survivorLocation{}, fmt.Errorf("expected <file>:<line>, got %q", arg)
	}
	line, err := strconv.Atoi(arg[idx+1:])
	if err != nil {
		return survivorLocation{}, fmt.Errorf("expected <file>:<line>, got %q: %w", arg, err)
	}
	repoRoot := filepath.Dir(root)
	abs, err := filepath.Abs(arg[:idx])
	if err != nil {
		return survivorLocation{}, err
	}
	rel, err := filepath.Rel(repoRoot, abs)
	if err != nil {
		return survivorLocation{}, err
	}
	return survivorLocation{
		repoRoot: repoRoot,
		rel:      filepath.ToSlash(rel),
		line:     line,
	}, nil
}

func survivorAccept() *cobra.Command {
	var op, reason, category string
	c := &cobra.Command{
		Use:   "accept <file>:<line>",
		Short: "Record a survivor as permanently accepted, with the reason why",
		Long: "Accept a surviving mutant into <repo>/.dross/survivors.toml. An acceptance is " +
			"the only lifecycle state that silences a survivor, so a reason is mandatory — " +
			"give --reason, or --category to reference shared prose (with --reason once to define it).",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if op == "" {
				return errors.New("--op is required: the mutation operator is part of the survivor's identity")
			}
			// The reason gate runs before anything is read or written: a
			// reasonless acceptance must not leave a store behind.
			if reason == "" && category == "" {
				return errors.New("a reason is required to accept a survivor: pass --reason, or --category to reference shared prose")
			}
			root, err := FindRoot()
			if err != nil {
				return err
			}
			loc, err := parseLocation(root, args[0])
			if err != nil {
				return err
			}
			res, err := survivor.ResolveAt(loc.repoRoot, loc.rel, loc.line, op)
			if err != nil {
				return fmt.Errorf("resolve %s:%d: %w", loc.rel, loc.line, err)
			}
			if res.Ambiguous {
				// Loud, because an ambiguous acceptance will not suppress: the
				// user would otherwise see a clean "accepted" and keep seeing
				// the survivor in every later run with no idea why.
				Printf("warning: %s:%d is ambiguous — its source text occurs on %d lines in this file, "+
					"so this acceptance will not suppress it until the text is unique\n",
					loc.rel, loc.line, res.Occurrences)
			}

			s, err := state.Load(filepath.Join(root, state.File))
			if err != nil {
				return err
			}
			if err := survivor.Accept(survivor.Path(root), survivor.Acceptance{
				Key:        res.Key,
				File:       loc.rel,
				Op:         op,
				Text:       res.Text,
				Line:       loc.line,
				Reason:     reason,
				Category:   category,
				AcceptedIn: s.CurrentPhase,
			}); err != nil {
				return err
			}
			Printf("accepted %s:%d (%s) → %s\n", loc.rel, loc.line, op, res.Key)
			return nil
		},
	}
	c.Flags().StringVar(&op, "op", "", "mutation operator, e.g. CONDITIONALS_NEGATION (required)")
	c.Flags().StringVar(&reason, "reason", "", "why this survivor is permanently acceptable")
	c.Flags().StringVar(&category, "category", "", "shared reason name; pass --reason once alongside it to define the prose")
	return c
}

func survivorRoute() *cobra.Command {
	var op, target string
	c := &cobra.Command{
		Use:   "route <file>:<line>",
		Short: "Route a survivor to a destination phase as a deferred item",
		Long: "Append a [[deferred]] entry carrying this survivor's identity key and a target " +
			"to the CURRENT phase's spec.toml — deferred items live in the spec that deferred " +
			"them. Routed debt stays visible; only an acceptance earns silence.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if op == "" {
				return errors.New("--op is required: the mutation operator is part of the survivor's identity")
			}
			if target == "" {
				return errors.New("--target is required: a routed survivor must name where it is going")
			}
			root, err := FindRoot()
			if err != nil {
				return err
			}
			s, err := state.Load(filepath.Join(root, state.File))
			if err != nil {
				return err
			}
			if s.CurrentPhase == "" {
				return errors.New("no current phase: a deferred item lives in the spec of the phase that deferred it")
			}
			// Validate the destination BEFORE touching the spec, so routing to
			// a typo'd slug leaves nothing half-written behind.
			if _, err := os.Stat(phase.Dir(root, target)); err != nil {
				return fmt.Errorf("no such phase %q to route to: %w", target, err)
			}
			specPath := filepath.Join(phase.Dir(root, s.CurrentPhase), "spec.toml")
			spec, err := phase.LoadSpec(specPath)
			if err != nil {
				return err
			}
			loc, err := parseLocation(root, args[0])
			if err != nil {
				return err
			}
			res, err := survivor.ResolveAt(loc.repoRoot, loc.rel, loc.line, op)
			if err != nil {
				return fmt.Errorf("resolve %s:%d: %w", loc.rel, loc.line, err)
			}

			spec.Deferred = append(spec.Deferred, phase.Deferred{
				Text:     fmt.Sprintf("survivor %s:%d (%s)", loc.rel, loc.line, op),
				Why:      "surviving mutant routed out of a verify run",
				Target:   target,
				Survivor: res.Key,
			})
			if err := spec.Save(specPath); err != nil {
				return err
			}
			Printf("routed %s:%d (%s) → %s [%s]\n", loc.rel, loc.line, op, target, res.Key)
			return nil
		},
	}
	c.Flags().StringVar(&op, "op", "", "mutation operator, e.g. CONDITIONALS_NEGATION (required)")
	c.Flags().StringVar(&target, "target", "", "destination phase slug (required)")
	return c
}

func survivorRetire() *cobra.Command {
	var stale bool
	c := &cobra.Command{
		Use:   "retire <key>...",
		Short: "Remove acceptances from the store (--stale for every acceptance whose subject is gone)",
		Long: "Retire acceptances from <repo>/.dross/survivors.toml by identity key. This is the " +
			"CLI path out of the store, so retiring an entry never means hand-editing the file. " +
			"--stale retires exactly the acceptances whose subject is gone; one that could not " +
			"be checked is left alone — \"I could not look\" is not \"it is gone\".",
		Args: cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			path := survivor.Path(root)
			keys := args
			if stale {
				if len(args) > 0 {
					return errors.New("--stale retires every stale acceptance; do not also name keys")
				}
				store, err := survivor.Load(path)
				if err != nil {
					return err
				}
				for _, s := range survivor.StaleAcceptances(filepath.Dir(root), store).Stale {
					keys = append(keys, s.Key)
				}
				if len(keys) == 0 {
					Print("(no stale acceptances to retire)")
					return nil
				}
			}
			if len(keys) == 0 {
				return errors.New("name at least one acceptance key to retire, or pass --stale")
			}
			if err := survivor.Retire(path, keys...); err != nil {
				return err
			}
			Printf("retired %d acceptance(s): %s\n", len(keys), strings.Join(keys, ", "))
			return nil
		},
	}
	c.Flags().BoolVar(&stale, "stale", false, "retire every acceptance whose subject no longer exists")
	return c
}

func survivorList() *cobra.Command {
	var staleOnly, asJSON bool
	c := &cobra.Command{
		Use:   "list",
		Short: "List accepted survivors (--stale for those whose subject is gone)",
		RunE: func(_ *cobra.Command, _ []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			store, err := survivor.Load(survivor.Path(root))
			if err != nil {
				return err
			}
			entries := store.Accepted
			if staleOnly {
				stale := survivor.StaleAcceptances(filepath.Dir(root), store)
				entries = nil
				for _, s := range stale.Stale {
					entries = append(entries, s.Acceptance)
				}
			}

			if asJSON {
				// A bare array, never a `# <path>` header line: a `#` line is
				// not JSON and every consumer would have to strip it.
				if entries == nil {
					entries = []survivor.Acceptance{}
				}
				return emitJSON(entries)
			}
			if len(entries) == 0 {
				if staleOnly {
					Print("(no stale acceptances)")
				} else {
					Print("(no accepted survivors)")
				}
				return nil
			}
			Printf("%-18s %-40s %-24s %s\n", "KEY", "FILE:LINE", "OP", "REASON")
			for _, a := range entries {
				reason, err := store.ReasonFor(a)
				if err != nil {
					reason = "(unresolved: " + err.Error() + ")"
				}
				Printf("%-18s %-40s %-24s %s\n", a.Key, fmt.Sprintf("%s:%d", a.File, a.Line), a.Op, reason)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&staleOnly, "stale", false, "only acceptances whose subject no longer exists")
	c.Flags().BoolVar(&asJSON, "json", false, jsonFlagUsage)
	return c
}

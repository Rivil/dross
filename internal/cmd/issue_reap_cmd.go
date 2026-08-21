package cmd

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/Rivil/dross/internal/board"

	"github.com/spf13/cobra"
)

// `dross issue reap` — the stranded-mirror sweep.
//
// It is DRY-RUN BY DEFAULT. Without --apply it classifies and prints, and
// issues no write of any kind. That is not politeness about a destructive verb:
// the first live run of this command against a real board moves ninety cards at
// once, and a plan that can be read, scoped and re-read before anything moves is
// what makes that a reviewable act rather than a leap.
//
// One verb owns all five namespaces rather than a --reap mode on each existing
// sync verb (the locked verb_shape decision): one classify-then-close pipeline,
// one test site, and a whole-board plan printable in a single pass.
func issueReap() *cobra.Command {
	var namespaces []string
	var apply bool
	c := &cobra.Command{
		Use:   "reap",
		Short: "Close board mirrors the forward lifecycle left stranded",
		Long: `Classify every dross-authored board card against the record on disk and
close the ones whose artefact provably finished.

Dry-run by default: with no --apply it prints the plan — card id, lane, and the
record that justifies closing it — and writes nothing.

Every close decision comes from the on-disk record, never from the card's own
state. A card whose artefact is not complete is never closed; a card no record
explains is named as unattributable and left open.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			ctx, enabled, err := openBoard()
			if err != nil {
				return err
			}
			if !enabled {
				return nil // no-op when board sync is off
			}
			if err := validateReapNamespaces(namespaces); err != nil {
				return err
			}
			plan, err := classifyReap(ctx, namespaces)
			if err != nil {
				return err
			}
			printReapPlan(plan)
			if apply {
				return fmt.Errorf("--apply is not implemented yet; the dry-run plan above is what this command can do so far")
			}
			return nil
		},
	}
	c.Flags().StringArrayVar(&namespaces, "namespace", nil,
		"limit the sweep to one mirror namespace (repeatable; default: every namespace)")
	c.Flags().BoolVar(&apply, "apply", false, "write the plan to the board (default: dry run)")
	return c
}

// printReapPlan renders the classified inventory grouped by lane.
//
// Every stranded line carries the RECORD that justified it, not just the card
// id. A plan that only lists ids asks the reader to take ninety closes on
// trust; a plan that names `phases/03-auth/changes.json status=complete` beside
// each one can be argued with.
func printReapPlan(plan *reapPlan) {
	byLane := map[string][]reapCard{}
	unattributableByLane := map[string][]reapCard{}
	for _, c := range plan.Cards {
		byLane[c.Lane] = append(byLane[c.Lane], c)
	}
	for _, c := range plan.Unattributable {
		unattributableByLane[c.Lane] = append(unattributableByLane[c.Lane], c)
	}

	lanes := 0
	for _, lane := range reapLanes {
		stranded := byLane[lane.Name]
		unattributable := unattributableByLane[lane.Name]
		if len(stranded) == 0 && len(unattributable) == 0 {
			continue
		}
		lanes++
		Printf("%s (%d stranded, %d unattributable) -> %s\n",
			lane.Name, len(stranded), len(unattributable), lane.Terminal)
		for _, c := range stranded {
			Printf("  %-10s %s\n", c.Key, c.Why)
		}
		for _, c := range unattributable {
			Printf("  %-10s [unattributable] %s\n", c.Key, c.Why)
		}
	}

	if len(plan.Cards) == 0 && len(plan.Unattributable) == 0 {
		Print("no stranded mirrors — every card matches its record")
		return
	}
	Printf("\n%d stranded across %d %s, %d unattributable (named, never closed)\n",
		len(plan.Cards), lanes, plural(lanes, "lane", "lanes"), len(plan.Unattributable))
}

// boardNamespaceNames enumerates board.Board's map-typed fields — the mirror
// namespaces themselves, read off the struct rather than transcribed.
//
// The flag validates against THIS, not against a literal list beside the flag
// definition. A namespace added to board.Board becomes a legal --namespace
// value in the same commit that adds it, and the error a typo produces names
// the real set rather than a stale copy of it.
func boardNamespaceNames() []string {
	rt := reflect.TypeOf(board.Board{})
	var out []string
	for i := 0; i < rt.NumField(); i++ {
		if f := rt.Field(i); f.Type.Kind() == reflect.Map {
			out = append(out, f.Name)
		}
	}
	sort.Strings(out)
	return out
}

// validateReapNamespaces refuses an unknown --namespace by name, listing the
// namespaces that exist.
func validateReapNamespaces(namespaces []string) error {
	if len(namespaces) == 0 {
		return nil
	}
	known := map[string]bool{}
	for _, n := range boardNamespaceNames() {
		known[strings.ToLower(n)] = true
	}
	var unknown []string
	for _, n := range namespaces {
		if !known[strings.ToLower(strings.TrimSpace(n))] {
			unknown = append(unknown, n)
		}
	}
	if len(unknown) > 0 {
		return fmt.Errorf("unknown --namespace %s; expected one of %s",
			strings.Join(unknown, ", "), strings.Join(boardNamespaceNames(), ", "))
	}
	return nil
}

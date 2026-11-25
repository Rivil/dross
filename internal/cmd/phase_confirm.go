package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/phase"
)

// phaseConfirm records the user's explicit yes on a one-way decision (the
// locked confirmation_record decision): it writes confirmed =
// phase.DecisionHash(key, choice) onto the spec's locked one-way decision
// named key. /dross-spec runs it only after the user answers yes to that
// decision's confirm turn; the plan-lock gate then reads the record through
// phase.UnconfirmedOneWay.
//
// The hash binds the confirmation to the choice as it stood: a choice edited
// afterwards no longer matches, so the gate refuses it again, and re-running
// this verb is how it is re-confirmed (c-8). A current confirmation is a no-op
// that leaves spec.toml byte-identical.
//
// Only a locked one-way decision can be confirmed — a reversible, untagged or
// unlocked one is no door — and the key must name exactly one decision. Every
// check runs before the write; any refusal leaves the spec untouched.
func phaseConfirm() *cobra.Command {
	return &cobra.Command{
		Use:   "confirm <phase-id> <key>",
		Short: "Record the user's confirmation of a locked one-way decision in a phase's spec",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			id, key := args[0], args[1]
			specPath, spec, err := loadConfirmSpec(root, id)
			if err != nil {
				return err
			}
			i, err := confirmTarget(spec, id, key)
			if err != nil {
				return err
			}

			d := &spec.Decisions[i]
			hash := phase.DecisionHash(d.Key, d.Choice)
			if d.Confirmed == hash {
				Printf("already confirmed: %s\n", key)
				return nil
			}
			stale := d.Confirmed != ""
			d.Confirmed = hash
			if err := spec.Save(specPath); err != nil {
				return fmt.Errorf("write %s: %w", specPath, err)
			}
			if stale {
				Printf("re-confirmed: %s (the recorded confirmation was for an earlier choice)\n", key)
				return nil
			}
			Printf("confirmed: %s\n", key)
			return nil
		},
	}
}

// loadConfirmSpec reads the spec a confirmation is written into, returning its
// path. An id that would escape phases/ is refused before anything is read; an
// unknown phase, a missing spec and one that does not decode are each an error
// naming what is wrong.
func loadConfirmSpec(root, id string) (string, *phase.Spec, error) {
	if _, err := phase.ContainID(root, id); err != nil {
		return "", nil, fmt.Errorf("phase id %q refused: %w", id, err)
	}
	dir := phase.Dir(root, id)
	if !isDir(dir) {
		return "", nil, fmt.Errorf("unknown phase %q — no directory at %s (run `dross phase list` to see ids)", id, dir)
	}
	specPath := filepath.Join(dir, "spec.toml")
	if _, err := os.Stat(specPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil, fmt.Errorf("phase %q has no spec at %s, so there is no decision to confirm: %w", id, specPath, fs.ErrNotExist)
		}
		return "", nil, fmt.Errorf("stat %s: %w", specPath, err)
	}
	spec, err := phase.LoadSpec(specPath)
	if err != nil {
		return "", nil, err
	}
	return specPath, spec, nil
}

// confirmTarget returns the index of the one decision in spec that key names,
// refusing an empty or unknown key (listing the keys the spec holds), a key
// two decisions share, and a decision that is not a locked one-way one.
func confirmTarget(spec *phase.Spec, id, key string) (int, error) {
	if key == "" {
		return 0, fmt.Errorf("confirm refused: the decision key is empty (phase %s holds %s)", id, decisionKeyList(spec))
	}
	var at []int
	for i, d := range spec.Decisions {
		if d.Key == key {
			at = append(at, i)
		}
	}
	switch len(at) {
	case 0:
		return 0, fmt.Errorf("confirm refused: phase %s has no decision %q (it holds %s)", id, key, decisionKeyList(spec))
	case 1:
	default:
		return 0, fmt.Errorf("confirm refused: key %q is ambiguous — %d decisions in phase %s carry it; give each its own key first", key, len(at), id)
	}

	d := spec.Decisions[at[0]]
	if !d.Locked || d.Reversibility != phase.DoorOneWay {
		return 0, fmt.Errorf("confirm refused: %q is not a locked one-way decision (%s) — only a locked one-way decision needs confirming", key, confirmWhyNot(d))
	}
	return at[0], nil
}

// confirmWhyNot names the way d falls short of a locked one-way decision.
func confirmWhyNot(d phase.Decision) string {
	var why []string
	switch d.Reversibility {
	case phase.DoorOneWay:
	case "":
		why = append(why, "untagged")
	default:
		why = append(why, fmt.Sprintf("reversibility %q", d.Reversibility))
	}
	if !d.Locked {
		why = append(why, "not locked")
	}
	return strings.Join(why, ", ")
}

// decisionKeyList renders spec's decision keys for a refusal message.
func decisionKeyList(spec *phase.Spec) string {
	if len(spec.Decisions) == 0 {
		return "no decisions"
	}
	keys := make([]string, len(spec.Decisions))
	for i, d := range spec.Decisions {
		keys[i] = fmt.Sprintf("%q", d.Key)
	}
	return "decisions " + strings.Join(keys, ", ")
}

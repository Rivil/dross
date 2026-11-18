package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/milestone"
	"github.com/Rivil/dross/internal/state"
)

// milestoneAck records a `[[closeout_ack]]` for one unresolved close-out item
// (locked ack_storage): the item stops blocking `milestone complete`, and its
// reason rides into the integration PR body.
//
// The item must be one the close-out check produces right now, or one already
// acknowledged — a re-ack replaces the earlier record rather than adding a
// second (c-9), and it stays allowed after the item resolves so a stale ack's
// reason can still be corrected. Anything else is refused: an ack for an item
// that is not there would be a record no report ever shows. An unverified phase
// is never ackable (locked force_unverified): it clears only by verifying it or
// by --force-unverified.
func milestoneAck() *cobra.Command {
	var reason string
	c := &cobra.Command{
		Use:   "ack <item> [version]",
		Short: "Acknowledge an unresolved close-out item with a reason (version defaults to state.current_milestone)",
		Long: "Acknowledge one unresolved close-out item so it no longer blocks `milestone complete`.\n" +
			"Items are phase:<slug>, deferred:<source>#<index> or survivor:<key>; the reason is\n" +
			"required and appears in the integration PR body. Re-acking replaces the earlier reason.",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			parsed, err := milestone.ParseAckItem(args[0])
			if err != nil {
				return err
			}
			id := milestone.FormatAckItem(parsed)
			if strings.TrimSpace(reason) == "" {
				return fmt.Errorf("acknowledge %s: a non-empty --reason is required — it is carried into the integration PR body", id)
			}
			root, err := FindRoot()
			if err != nil {
				return err
			}
			version := ""
			if len(args) == 2 {
				version = args[1]
			}
			version, err = ackMilestoneVersion(root, version)
			if err != nil {
				return err
			}
			path := milestone.FilePath(root, version)
			if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("milestone %s has no record (%s) — nothing to acknowledge", version, path)
			} else if err != nil {
				return err
			}
			m, err := milestone.Load(path)
			if err != nil {
				return err
			}
			if err := checkAckable(root, version, m, parsed, id); err != nil {
				return err
			}
			reacked := slices.ContainsFunc(m.Closeout, func(a milestone.CloseoutAck) bool { return a.Item == id })
			if err := m.Ack(id, reason, time.Now().UTC().Truncate(time.Second)); err != nil {
				return err
			}
			if err := m.Save(path); err != nil {
				return err
			}
			if reacked {
				Printf("re-acknowledged %s in milestone %s (replaces the earlier reason)\n", id, version)
			} else {
				Printf("acknowledged %s in milestone %s\n", id, version)
			}
			return nil
		},
	}
	c.Flags().StringVar(&reason, "reason", "", "why this item may ride the integration PR unresolved (required, non-empty)")
	return c
}

// ackMilestoneVersion falls back to state.current_milestone when no version is
// given, as every other milestone subcommand does.
func ackMilestoneVersion(root, version string) (string, error) {
	if version != "" {
		return version, nil
	}
	s, err := state.Load(filepath.Join(root, state.File))
	if err != nil {
		return "", fmt.Errorf("no version given and load state: %w", err)
	}
	if s.CurrentMilestone == "" {
		return "", errors.New("no version given and state has no current_milestone; run `dross milestone list` to see options")
	}
	return s.CurrentMilestone, nil
}

// checkAckable returns nil when id is an item the close-out check produces now
// or one m already acknowledges, and otherwise a refusal saying why the item is
// not ackable and what does clear it.
func checkAckable(root, version string, m *milestone.Milestone, it milestone.AckItem, id string) error {
	r, err := milestoneCloseout(root, version)
	if err != nil {
		return err
	}
	for _, x := range r.Items {
		if x.ID == id {
			return nil
		}
	}
	for _, a := range m.Closeout {
		if a.Item == id {
			return nil
		}
	}

	if it.Kind == milestone.AckPhase {
		if !slices.Contains(m.Phases, it.Payload) {
			return fmt.Errorf("cannot acknowledge %s: phase %s is not in milestone %s's phases", id, it.Payload, version)
		}
		for _, x := range r.Items {
			if x.Kind == closeoutUnverified && x.Subject == it.Payload {
				return fmt.Errorf("cannot acknowledge %s: phase %s is unverified (%s), and an acknowledgement never clears that — run /dross-verify %s, or pass --force-unverified to `dross milestone complete`",
					id, it.Payload, x.Detail, it.Payload)
			}
		}
		return fmt.Errorf("cannot acknowledge %s: phase %s is done — nothing to acknowledge", id, it.Payload)
	}

	msg := fmt.Sprintf("cannot acknowledge %s: it is not an unresolved close-out item of milestone %s — nothing to acknowledge", id, version)
	var open []string
	for _, x := range r.Items {
		if x.ID != "" {
			open = append(open, x.ID)
		}
	}
	if len(open) > 0 {
		msg += "\nackable items now: " + strings.Join(open, ", ")
	}
	for _, e := range r.Errors {
		msg += fmt.Sprintf("\n%s items could not be read (%v) — fix that, then retry", e.Kind, e.Err)
	}
	return errors.New(msg)
}

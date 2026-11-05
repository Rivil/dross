// Package killswitch reads the kill-switch record a phase trialling the solo
// writer keeps in its directory (solo-task-subagent c-6): the thresholds, the
// [before] leg mined from earlier solo runs, the [trial] verdict and — once
// mined — the [after] leg. The record is agent-authored; dross only reads it,
// and only for the two facts a reminder needs: whether the trial is still
// pending, and whether its [after] leg has any rows yet.
package killswitch

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// File is the record's name inside .dross/phases/<id>/.
const File = "kill-switch.toml"

// StatusPending is [trial].status before the [after] leg has been compared
// against the thresholds.
const StatusPending = "pending"

// Status is what a reader needs from a record to tell whether its [after] leg
// is still owed.
type Status struct {
	Trial     string // [trial].status
	AfterRows int    // [[after.task]] rows recorded
}

// AwaitingAfter reports a trial still pending with no [after] row mined: the
// one state in which the writer stays on with nothing measuring it.
func (s Status) AwaitingAfter() bool {
	return s.Trial == StatusPending && s.AfterRows == 0
}

// record is the decode target: the two facts Status carries, and nothing of
// the rest of the document — its other tables are read by people and by the
// kill-switch tests, not by dross.
type record struct {
	Trial struct {
		Status string `toml:"status"`
	} `toml:"trial"`
	After struct {
		Task []map[string]any `toml:"task"`
	} `toml:"after"`
}

// Load reads the record at path. A missing file is the os error itself, so
// errors.Is(err, fs.ErrNotExist) holds; a file that is not TOML of the
// record's shape is an error saying it does not decode.
func Load(path string) (Status, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Status{}, err
	}
	var rec record
	if err := toml.Unmarshal(b, &rec); err != nil {
		return Status{}, fmt.Errorf("does not decode: %w", err)
	}
	return Status{Trial: rec.Trial.Status, AfterRows: len(rec.After.Task)}, nil
}

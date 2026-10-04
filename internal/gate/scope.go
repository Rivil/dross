package gate

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/Rivil/dross/internal/gatestate"
	"github.com/Rivil/dross/internal/pathfence"
	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/review"
	"github.com/Rivil/dross/internal/rules"
	"github.com/Rivil/dross/internal/treefp"
)

// ReviewScope is what one solo review covers: a plan task of a solo
// /dross-execute run, or a solo /dross-quick. Attempt is the ledger key a
// fresh attempt changes — for a task its code base (treefp.Base), so a
// .dross/-only or empty commit keeps the same ledger while a code commit
// starts a fresh one; for a quick the moment `dross quick begin` recorded it.
type ReviewScope struct {
	Kind        review.Kind
	Phase       string
	Task        string
	Attempt     string
	Description string
}

// Matches reports whether a stored ledger belongs to this scope.
func (s *ReviewScope) Matches(rec *gatestate.Review) bool {
	return s != nil && rec != nil && rec.Kind == s.Kind && rec.Phase == s.Phase &&
		rec.Task == s.Task && rec.Attempt == s.Attempt
}

// Name is how a refusal or a verb names the scope.
func (s *ReviewScope) Name() string {
	if s.Kind == review.KindQuick {
		return "the solo quick"
	}
	return fmt.Sprintf("%s %s", s.Phase, s.Task)
}

// ArmedScope is the solo review scope armed in root, or nil when none is:
// a solo quick begun at the current HEAD wins (a quick runs inside whatever
// else is going on); otherwise the one in_progress task of a solo execute
// run, while HEAD is on its phase branch. Pair mode arms nothing — the human
// is the gate. A damaged record is an error naming the file, never a silent
// nil: the solo-review gate fails closed on it.
func ArmedScope(root string) (*ReviewScope, error) {
	q, err := gatestate.LoadQuick(root)
	if err != nil {
		return nil, err
	}
	if q != nil && q.Mode == "solo" {
		head, err := headOf(root)
		if err != nil {
			return nil, err
		}
		if head == q.Head {
			return &ReviewScope{Kind: review.KindQuick, Attempt: "quick@" + q.At.UTC().Format(time.RFC3339Nano), Description: q.Description}, nil
		}
	}

	ex, err := gatestate.LoadExecute(root)
	if err != nil {
		return nil, err
	}
	id, tasks, err := inProgress(root)
	if err != nil || len(tasks) == 0 {
		return nil, err
	}
	if ex == nil || ex.Phase != id || ex.Mode != "solo" {
		return nil, nil
	}
	on, err := onBranch(root, "phase/"+id)
	if err != nil || !on {
		return nil, err
	}
	if len(tasks) > 1 {
		return nil, ambiguous(id, tasks)
	}
	base, err := treefp.Base(root)
	if err != nil {
		return nil, err
	}
	return &ReviewScope{Kind: review.KindTask, Phase: id, Task: tasks[0], Attempt: base}, nil
}

// ContextScope loads what a review of s may see (c-5): for a task its plan
// record, the spec's criteria and decisions (review.BuildContext narrows them
// to the covered and the locked), and the hard rules; for a quick its
// description and the hard rules. The review verb and the recorder both build
// from this one loader, so the digest the verb prints is the digest the
// recorder regenerates.
func ContextScope(root, home string, s *ReviewScope) (review.Scope, error) {
	rs, err := loadRules(root, home)
	if err != nil {
		return review.Scope{}, err
	}
	if s.Kind == review.KindQuick {
		return review.Scope{Kind: review.KindQuick, Description: s.Description, Rules: rs}, nil
	}
	dir, err := phase.ContainID(filepath.Join(root, ".dross"), s.Phase)
	if err != nil {
		return review.Scope{}, err
	}
	var spec phase.Spec
	if err := decodePhaseFile(dir.String(), s.Phase, "spec.toml", &spec); err != nil {
		return review.Scope{}, err
	}
	var plan phase.Plan
	if err := decodePhaseFile(dir.String(), s.Phase, "plan.toml", &plan); err != nil {
		return review.Scope{}, err
	}
	t := plan.FindTask(s.Task)
	if t == nil {
		return review.Scope{}, fmt.Errorf("phase %s's plan has no task %s", s.Phase, s.Task)
	}
	return review.Scope{
		Kind: review.KindTask, Phase: s.Phase, Task: *t,
		Criteria: spec.Criteria, Decisions: spec.Decisions, Rules: rs,
	}, nil
}

// ReviewSecrets is the secret-path list a review context withholds — the
// secret-read gate's own, gates.toml extensions included — with its load
// error, which refuses the build.
func ReviewSecrets(home string) review.Secrets {
	l := LoadLists(home)
	return review.Secrets{Patterns: l.SecretPaths, Err: l.Err}
}

func decodePhaseFile(dir, id, name string, into any) error {
	f, err := pathfence.Contain(dir, "phase "+name, name)
	if err != nil {
		return err
	}
	rel := ".dross/phases/" + id + "/" + name
	b, err := pathfence.ReadFile(f)
	if err != nil {
		return fmt.Errorf("read %s: %w", rel, err)
	}
	if _, err := toml.Decode(string(b), into); err != nil {
		return fmt.Errorf("decode %s: %w", rel, err)
	}
	return nil
}

// loadRules is the configured rules: the user's ~/.claude/dross/rules.toml
// merged with the project's .dross/rules.toml, as `dross rule show` merges
// them. A missing file is no rules; a damaged one is an error.
func loadRules(root, home string) ([]rules.Resolved, error) {
	global := &rules.Set{}
	if home != "" {
		g, err := rules.LoadFile(filepath.Join(home, ".claude", "dross", rules.File))
		if err != nil {
			return nil, err
		}
		global = g
	}
	pf, err := pathfence.Contain(root, "dross rules", ".dross/"+rules.File)
	if err != nil {
		return nil, err
	}
	project := &rules.Set{}
	b, err := pathfence.ReadFile(pf)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, fmt.Errorf("read .dross/%s: %w", rules.File, err)
	default:
		if _, err := toml.Decode(string(b), project); err != nil {
			return nil, fmt.Errorf("decode .dross/%s: %w", rules.File, err)
		}
	}
	return rules.Merge(global, project), nil
}

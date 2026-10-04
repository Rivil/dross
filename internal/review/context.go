package review

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/rules"
	"github.com/Rivil/dross/internal/secretpath"
	"github.com/Rivil/dross/internal/treefp"
)

// Scope is everything a review may see besides the diff (c-5): the task
// record and the criteria it covers, the phase's locked decisions and the
// hard rules — or, for a quick, its stated description and the hard rules.
// BuildContext does the narrowing: callers hand it the whole spec.
type Scope struct {
	Kind  Kind
	Phase string
	// Task is the plan task under review. Its Status is never rendered: a
	// status flip must not change what the reviewer sees.
	Task phase.Task
	// Criteria and Decisions are the spec's; only the task's covered criteria
	// and the locked decisions are rendered.
	Criteria  []phase.Criterion
	Decisions []phase.Decision
	// Description is a quick's stated description, its only spec source.
	Description string
	// Rules are the configured rules; the hard ones are rendered after the
	// builtins.
	Rules []rules.Resolved
}

// Secrets decides which changed paths have their content withheld from the
// review: those matching Patterns (the secret-read gate's list, gates.toml
// extensions included). Err is the error loading that list — a malformed
// gates.toml — and refuses the build: falling back to the defaults would
// render a user-added secret path's content.
type Secrets struct {
	Patterns []string
	Err      error
}

// Context is one rendered review context.
type Context struct {
	// Digest is "sha256:<hex>" over Body.
	Digest string
	// Base and Tree are the fingerprints the diff runs between; a pass is
	// bound to Tree.
	Base, Tree string
	Body       string
}

// File is the context file's bytes: a header naming the digest, then the
// body. The reviewer checks the header against the digest in its prompt, so a
// file rewritten after the prompt was composed is never reviewed.
func (c Context) File() []byte {
	return []byte("dross-review-context " + c.Digest + "\n\n" + c.Body)
}

// ErrNoCode is BuildContext's refusal when the work tree differs from HEAD
// only under .dross/: there is nothing to review, and a .dross/-only commit is
// ungated anyway.
var ErrNoCode = errors.New("no code changes to review: the work tree differs from HEAD only under .dross/ (or not at all)")

// BuildContext renders the review context for the repository at dir.
func BuildContext(dir string, s Scope, sec Secrets) (Context, error) {
	if sec.Err != nil {
		return Context{}, fmt.Errorf("refusing to build a review context: the secret-path list did not load, so a secret's content could be rendered: %w", sec.Err)
	}
	if s.Kind != KindTask && s.Kind != KindQuick {
		return Context{}, fmt.Errorf("review scope kind %q is neither %q nor %q", s.Kind, KindTask, KindQuick)
	}
	ch, err := treefp.Changes(dir)
	if err != nil {
		return Context{}, err
	}
	if ch.Base == ch.Tree {
		return Context{}, ErrNoCode
	}
	var withheld []string
	for _, p := range ch.Paths {
		if secretpath.Pattern(p, sec.Patterns) != "" {
			withheld = append(withheld, p)
		}
	}
	//dross:taint-cleared git diff-tree -p output is the repository's own code diff, secret paths withheld; it is rendered only into the machine-local, self-ignoring review context the reviewer reads and hashed for its digest — never into a tracked artifact or a PR body
	patch, err := treefp.Patch(dir, ch, withheld)
	if err != nil {
		return Context{}, err
	}

	var b strings.Builder
	b.WriteString("# Solo review context\n\n")
	fmt.Fprintf(&b, "kind: %s\n", s.Kind)
	if s.Kind == KindTask {
		fmt.Fprintf(&b, "phase: %s\ntask: %s\n", s.Phase, s.Task.ID)
	}
	fmt.Fprintf(&b, "base: %s\ntree: %s\n\n", ch.Base, ch.Tree)

	if s.Kind == KindQuick {
		b.WriteString("## Quick description\n\n")
		b.WriteString(strings.TrimSpace(s.Description) + "\n\n")
		b.WriteString("Spec findings cite criterion \"" + QuickCriterion + "\".\n\n")
	} else {
		renderTask(&b, s.Task)
		renderCriteria(&b, s.Task.Covers, s.Criteria)
		renderDecisions(&b, s.Decisions)
	}
	renderRules(&b, s.Rules)

	b.WriteString("## Diff\n\n")
	b.WriteString("The whole uncommitted change against HEAD — staged, unstaged and untracked — with .dross/ bookkeeping left out.\n\n")
	for _, p := range withheld {
		fmt.Fprintf(&b, "(content withheld — secret path: %s)\n", p)
	}
	if len(withheld) > 0 {
		b.WriteString("\n")
	}
	fence := strings.Repeat("`", max(3, longestRun(patch, '`')+1))
	b.WriteString(fence + "diff\n" + patch)
	if !strings.HasSuffix(patch, "\n") {
		b.WriteString("\n")
	}
	b.WriteString(fence + "\n")

	body := b.String()
	sum := sha256.Sum256([]byte(body))
	return Context{Digest: "sha256:" + hex.EncodeToString(sum[:]), Base: ch.Base, Tree: ch.Tree, Body: body}, nil
}

func renderTask(b *strings.Builder, t phase.Task) {
	b.WriteString("## Task record\n\n")
	fmt.Fprintf(b, "id: %s\ntitle: %s\n", t.ID, t.Title)
	fmt.Fprintf(b, "files: %s\n", strings.Join(t.Files, ", "))
	fmt.Fprintf(b, "covers: %s\n", strings.Join(t.Covers, ", "))
	if d := strings.TrimSpace(t.Description); d != "" {
		b.WriteString("description:\n" + d + "\n")
	}
	b.WriteString("test_contract:\n")
	for _, c := range t.TestContract {
		b.WriteString("- " + c + "\n")
	}
	b.WriteString("\n")
}

// renderCriteria renders only the criteria the task covers, in covers order.
func renderCriteria(b *strings.Builder, covers []string, all []phase.Criterion) {
	b.WriteString("## Covered criteria\n\n")
	b.WriteString("Judge spec compliance only on the sub-surfaces this task's record claims for each criterion; a criterion is often split across tasks.\n\n")
	for _, id := range covers {
		i := slices.IndexFunc(all, func(c phase.Criterion) bool { return c.ID == id })
		if i < 0 {
			fmt.Fprintf(b, "- %s: (not in the spec)\n", id)
			continue
		}
		fmt.Fprintf(b, "- %s: %s\n", id, all[i].Text)
	}
	b.WriteString("\n")
}

func renderDecisions(b *strings.Builder, all []phase.Decision) {
	b.WriteString("## Locked decisions\n\n")
	n := 0
	for _, d := range all {
		if !d.Locked {
			continue
		}
		n++
		fmt.Fprintf(b, "- %s: %s\n", d.Key, d.Choice)
	}
	if n == 0 {
		b.WriteString("(none)\n")
	}
	b.WriteString("\n")
}

// renderRules renders the builtin hard rules, then the configured hard ones.
func renderRules(b *strings.Builder, configured []rules.Resolved) {
	b.WriteString("## Hard rules\n\n")
	for _, r := range append(slices.Clone(rules.Builtins), configured...) {
		if r.Severity != "" && r.Severity != rules.Hard {
			continue
		}
		fmt.Fprintf(b, "- [%s] %s\n", r.ID, r.Text)
	}
	b.WriteString("\n")
}

func longestRun(s string, c byte) int {
	best, run := 0, 0
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			run++
			best = max(best, run)
		} else {
			run = 0
		}
	}
	return best
}

// Package review holds the solo per-task reviewer's vocabulary: the verdict
// the dross-task-reviewer agent writes, the ledger of review rounds a task or
// quick accumulates, and how each finding was resolved.
//
// It imports neither internal/gate nor internal/gatestate: gatestate stores
// []review.Round, so either import would cycle.
package review

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ReviewerAgent is the subagent_type of the dross-installed reviewer
// definition (assets/agents/dross-task-reviewer.md). The PostToolUse recorder
// claims Agent calls naming it, and nothing else.
const ReviewerAgent = "dross-task-reviewer"

// VerdictFence is the info string of the one fenced block the reviewer ends
// its reply with.
const VerdictFence = "dross-verdict"

// QuickCriterion is the criterion a spec finding cites on a solo quick, whose
// only spec source is the quick's stated description.
const QuickCriterion = "description"

// Kind is what a review covers: a plan task or a solo quick.
type Kind string

const (
	KindTask  Kind = "task"
	KindQuick Kind = "quick"
)

// Severity grades a code-quality finding (quality_blocking). A spec finding
// always blocks, whatever severity the reviewer wrote on it.
type Severity string

const (
	Blocking Severity = "BLOCKING"
	Flag     Severity = "FLAG"
	Note     Severity = "NOTE"
)

func (s Severity) valid() bool { return s == Blocking || s == Flag || s == Note }

// Finding is one reviewer finding. Spec findings cite the criterion whose
// claimed sub-surface has no code or test in the diff; quality findings carry
// a severity and may cite a criterion or test_contract line.
type Finding struct {
	Criterion string   `json:"criterion,omitempty"`
	Severity  Severity `json:"severity,omitempty"`
	Text      string   `json:"text"`
}

// Verdict is a parsed reviewer verdict. Spec and Quality stay separate — c-2
// reports them apart — and Pass is derived from the findings, never taken from
// the reviewer's word alone.
type Verdict struct {
	Pass    bool
	Spec    []Finding
	Quality []Finding
}

type wireVerdict struct {
	Verdict string    `json:"verdict"`
	Spec    []Finding `json:"spec"`
	Quality []Finding `json:"quality"`
}

// ParseVerdict reads the single dross-verdict fence from a reviewer reply.
//
// Every spec finding blocks: its severity is normalised to BLOCKING. A quality
// finding blocks only when graded BLOCKING. The reviewer's stated verdict must
// agree with what its findings derive; a disagreement is an error, never a
// pass — a verdict that contradicts itself is not one the gate can trust.
func ParseVerdict(reply string, kind Kind) (Verdict, error) {
	body, err := fence(reply)
	if err != nil {
		return Verdict{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	var w wireVerdict
	if err := dec.Decode(&w); err != nil {
		return Verdict{}, fmt.Errorf("%s fence is not the verdict JSON: %w", VerdictFence, err)
	}
	if dec.More() {
		return Verdict{}, fmt.Errorf("%s fence carries more than one JSON value", VerdictFence)
	}

	v := Verdict{Spec: make([]Finding, 0, len(w.Spec)), Quality: make([]Finding, 0, len(w.Quality))}
	for i, f := range w.Spec {
		if strings.TrimSpace(f.Text) == "" {
			return Verdict{}, fmt.Errorf("spec finding %d has no text", i+1)
		}
		switch c := strings.TrimSpace(f.Criterion); {
		case c == "":
			return Verdict{}, fmt.Errorf("spec finding %d cites no criterion", i+1)
		case kind == KindQuick && c != QuickCriterion:
			return Verdict{}, fmt.Errorf("spec finding %d on a quick cites %q; a quick's only spec source is %q", i+1, c, QuickCriterion)
		case kind != KindQuick && c == QuickCriterion:
			return Verdict{}, fmt.Errorf("spec finding %d cites %q, which only a quick review may cite", i+1, QuickCriterion)
		}
		if f.Severity != "" && !f.Severity.valid() {
			return Verdict{}, fmt.Errorf("spec finding %d has unknown severity %q", i+1, f.Severity)
		}
		f.Severity = Blocking
		v.Spec = append(v.Spec, f)
	}
	blocking := len(v.Spec) > 0
	for i, f := range w.Quality {
		if strings.TrimSpace(f.Text) == "" {
			return Verdict{}, fmt.Errorf("quality finding %d has no text", i+1)
		}
		if !f.Severity.valid() {
			return Verdict{}, fmt.Errorf("quality finding %d has unknown severity %q (want BLOCKING, FLAG or NOTE)", i+1, f.Severity)
		}
		blocking = blocking || f.Severity == Blocking
		v.Quality = append(v.Quality, f)
	}
	v.Pass = !blocking

	switch w.Verdict {
	case "pass":
		if !v.Pass {
			return Verdict{}, errors.New(`verdict says "pass" but carries a blocking finding (a spec finding, or a quality finding graded BLOCKING)`)
		}
	case "block":
		if v.Pass {
			return Verdict{}, errors.New(`verdict says "block" but carries no blocking finding (only FLAG/NOTE quality findings)`)
		}
	default:
		return Verdict{}, fmt.Errorf(`verdict is %q, want "pass" or "block"`, w.Verdict)
	}
	return v, nil
}

// fence returns the body of the one ```dross-verdict block in reply.
func fence(reply string) ([]byte, error) {
	open := "```" + VerdictFence
	var bodies []string
	lines := strings.Split(reply, "\n")
	for i := 0; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != open {
			continue
		}
		var b strings.Builder
		closed := false
		for i++; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "```" {
				closed = true
				break
			}
			b.WriteString(lines[i])
			b.WriteByte('\n')
		}
		if !closed {
			return nil, fmt.Errorf("%s fence is never closed", VerdictFence)
		}
		bodies = append(bodies, b.String())
	}
	switch len(bodies) {
	case 0:
		return nil, fmt.Errorf("reply carries no %s fence", VerdictFence)
	case 1:
		return []byte(bodies[0]), nil
	default:
		return nil, fmt.Errorf("reply carries %d %s fences, want exactly one", len(bodies), VerdictFence)
	}
}

// Package statusline is the pure, cobra-free core behind `dross statusline`: it
// renders the Claude Code status line from an explicit Inputs struct and never
// reads the environment, the filesystem, the clock, or git itself. The impure
// resolution (stdin JSON, ~/.claude todos/jobs, .dross/state.json, git branch)
// lives in the gather layer so this render logic is a pure function of its inputs
// and its byte-for-byte fidelity to the reference node statusline can be pinned by
// goldens. It deliberately holds no cobra dependency, mirroring internal/update.
package statusline

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
)

// Inputs is the fully-resolved data the renderer needs. The gather layer (t-4)
// populates it from stdin + the filesystem; the renderer treats it as authoritative
// and applies only the numeric meter defaults (the model/dir/identity defaults are
// resolved upstream in gather). Later tasks extend this struct: t-2 adds the line-2
// todo/state fields, t-3 adds the peer-jobs slice.
type Inputs struct {
	// Model is the (already-defaulted) model display name shown dim on line 1.
	Model string
	// Dir is the workspace current_dir; line 1 shows its basename.
	Dir string
	// Branch is the git branch (short SHA on detached HEAD, "" outside a repo).
	Branch string

	// TodoActiveForm is the active session's in-progress todo activeForm. When set
	// it is line 2's body (bold) and wins over DrossState.
	TodoActiveForm string
	// DrossState is the pre-formatted ".dross" project state ("milestone · phase ·
	// status") shown dim on line 2 when there is no in-progress todo. The gather
	// layer formats it (degrading gracefully when fields are absent); the renderer
	// only dims it.
	DrossState string

	// RemainingPercent is context_window.remaining_percentage. Nil => no meter.
	RemainingPercent *float64
	// TotalTokens is context_window.total_tokens; <=0 falls back to 1_000_000
	// (the JS default), matching the reference's `total_tokens || 1_000_000`.
	TotalTokens int
	// AutoCompactWindow is the parsed CLAUDE_CODE_AUTO_COMPACT_WINDOW token count;
	// >0 derives the auto-compact buffer as a fraction of TotalTokens, otherwise the
	// renderer uses the ~16.5% default buffer.
	AutoCompactWindow int
}

// defaultAutoCompactBufferPct is the assumed auto-compact reserve when
// CLAUDE_CODE_AUTO_COMPACT_WINDOW is unset, matching the reference statusline.
const defaultAutoCompactBufferPct = 16.5

// Render returns the status line bytes for in. Line 1 is "dim model │ dim
// basename(dir) ⎇ dim branch". Line 2 is the in-progress todo activeForm (bold,
// winning over state) or the dross state (dim), followed by the context meter; the
// meter's leading space is kept as a separator when there is a body and stripped
// when there is not — the reference's `ctxOnLine2` rule. Lines that come out empty
// are omitted. The peer-jobs line is folded in by a later task.
func Render(in Inputs) []byte {
	dirname := filepath.Base(in.Dir)
	branchSuffix := ""
	if in.Branch != "" {
		branchSuffix = fmt.Sprintf(" \x1b[2m⎇ %s\x1b[0m", in.Branch)
	}
	line1 := fmt.Sprintf("\x1b[2m%s\x1b[0m │ \x1b[2m%s\x1b[0m%s", in.Model, dirname, branchSuffix)

	// Line-2 body: bold todo (wins) else dim dross state, mirroring the reference's
	// `middle` and its " │ "-append-then-strip (a no-op net of `middle`, replicated
	// literally for fidelity).
	middle := ""
	switch {
	case in.TodoActiveForm != "":
		middle = fmt.Sprintf("\x1b[1m%s\x1b[0m", in.TodoActiveForm)
	case in.DrossState != "":
		middle = fmt.Sprintf("\x1b[2m%s\x1b[0m", in.DrossState)
	}
	line2Body := ""
	if middle != "" {
		line2Body = strings.TrimSuffix(middle+" │ ", " │ ")
	}

	ctx := contextMeter(in)
	ctxOnLine2 := ctx
	if line2Body == "" {
		ctxOnLine2 = strings.TrimPrefix(ctx, " ")
	}
	line2 := line2Body + ctxOnLine2

	lines := []string{line1}
	if line2 != "" {
		lines = append(lines, line2)
	}
	return []byte(strings.Join(lines, "\n"))
}

// contextMeter renders the 10-cell USED% meter, normalized for Claude Code's
// auto-compact buffer, with a leading space (the reference keeps the space so the
// caller decides whether to strip it). It returns "" when RemainingPercent is nil.
// The math mirrors the reference byte-for-byte: same IEEE-754 doubles, same
// floor/round, same color thresholds and blinking 💀 at >=80% used.
func contextMeter(in Inputs) string {
	if in.RemainingPercent == nil {
		return ""
	}
	remaining := *in.RemainingPercent

	totalCtx := float64(in.TotalTokens)
	if in.TotalTokens <= 0 {
		totalCtx = 1_000_000
	}
	bufferPct := defaultAutoCompactBufferPct
	if in.AutoCompactWindow > 0 {
		bufferPct = math.Min(100, float64(in.AutoCompactWindow)/totalCtx*100)
	}

	usableRemaining := math.Max(0, ((remaining-bufferPct)/(100-bufferPct))*100)
	used := int(math.Max(0, math.Min(100, math.Round(100-usableRemaining))))
	filled := used / 10 // Math.floor(used/10); used is non-negative so this is floor
	bar := strings.Repeat("█", filled) + strings.Repeat("░", 10-filled)

	switch {
	case used < 50:
		return fmt.Sprintf(" \x1b[32m%s %d%%\x1b[0m", bar, used)
	case used < 65:
		return fmt.Sprintf(" \x1b[33m%s %d%%\x1b[0m", bar, used)
	case used < 80:
		return fmt.Sprintf(" \x1b[38;5;208m%s %d%%\x1b[0m", bar, used)
	default:
		return fmt.Sprintf(" \x1b[5;31m💀 %s %d%%\x1b[0m", bar, used)
	}
}

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

// Render returns the status line bytes for in. In this task it emits line 1
// (dim model │ dim basename(dir) ⎇ dim branch) and, when a context meter is
// present, the meter on line 2 with its leading space stripped (the reference's
// `ctx.replace(/^ /, "")` path, taken because there is no line-2 body yet). Later
// tasks fold the todo/state body and the peer-jobs line into this assembly.
func Render(in Inputs) []byte {
	dirname := filepath.Base(in.Dir)
	branchSuffix := ""
	if in.Branch != "" {
		branchSuffix = fmt.Sprintf(" \x1b[2m⎇ %s\x1b[0m", in.Branch)
	}
	line1 := fmt.Sprintf("\x1b[2m%s\x1b[0m │ \x1b[2m%s\x1b[0m%s", in.Model, dirname, branchSuffix)

	lines := []string{line1}
	if ctx := contextMeter(in); ctx != "" {
		// No line-2 body in this task, so strip the meter's leading space exactly
		// as the reference does when there is nothing in front of it.
		lines = append(lines, strings.TrimPrefix(ctx, " "))
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

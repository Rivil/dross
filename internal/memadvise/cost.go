package memadvise

import "fmt"

// CacheReadUSDPerMTok is the one rate the estimate prices at: the prompt-cache
// read rate per million tokens, $0.20 for Claude Opus 5.5 and Sonnet 5.5 alike
// (published pricing, 2026-10). MEMORY.md sits in the cached prefix of every
// turn, so a cache read is what each of its bytes costs per turn. The render
// names this rate next to the figure: it will drift from published pricing,
// and the estimate is only as good as it (locked cost_model).
const CacheReadUSDPerMTok = 0.20

// Tokens estimates the tokens in n bytes: ceil(n/4), the usual English
// average. It is an estimate; nothing here pretends to tokenize.
func Tokens(n int) int {
	if n <= 0 {
		return 0
	}
	return (n + 3) / 4
}

// MonthlyUSD is what n index bytes cost over the window: their tokens, read
// once per assistant turn this repo ran, at the cache-read rate. Only this
// repo's turns count — another project never loads this MEMORY.md — and
// recalling a memory file's body is not priced (locked cost_model).
func MonthlyUSD(n, repoTurns int) float64 {
	if repoTurns <= 0 {
		return 0
	}
	return float64(Tokens(n)) * float64(repoTurns) * CacheReadUSDPerMTok / 1e6
}

// ShareText is this repo's share of all turns for display, or "n/a" when no
// turns were counted at all.
func ShareText(c TurnCounts) string {
	if c.All <= 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.0f%%", 100*c.RepoShare())
}

package watch

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Rivil/dross/internal/ship"
)

// BotPR is one open PR whose author the forge marks as a bot. AgeDays is whole
// days since the PR was opened — raw, with no stale threshold (pr_age).
type BotPR struct {
	Number  int              `json:"number"`
	Title   string           `json:"title"`
	Author  string           `json:"author"`
	URL     string           `json:"url"`
	AgeDays int              `json:"age_days"`
	Checks  ship.CheckRollup `json:"checks"`
}

// ShipPR is one open PR whose head is a dross phase or milestone branch.
type ShipPR struct {
	Number int              `json:"number"`
	Head   string           `json:"head"`
	URL    string           `json:"url"`
	Checks ship.CheckRollup `json:"checks"`
	// Untriaged is how many review comments on the PR are not yet triaged.
	// Zero is omitted: a PR with nothing waiting carries no count.
	Untriaged int `json:"untriaged,omitempty"`
}

// shipHeadPrefixes are the branch prefixes dross ships from.
var shipHeadPrefixes = []string{"phase/", "milestone/"}

// SplitPRs sorts the open PRs into the digest's two lists, each non-nil and
// ordered by number. The predicates are independent: a bot PR on a phase
// branch lands in both. Bot identity is the forge's flag, never the login. A
// ship PR is same-repo only — a fork can name its branch phase/anything.
func SplitPRs(prs []ship.OpenPRRecord, now time.Time) ([]BotPR, []ShipPR) {
	bots, ships := []BotPR{}, []ShipPR{}
	for _, p := range prs {
		if p.Author.IsBot {
			bots = append(bots, BotPR{
				Number:  p.Number,
				Title:   p.Title,
				Author:  p.Author.Login,
				URL:     p.URL,
				AgeDays: ageDays(p, now),
				Checks:  p.Checks,
			})
		}
		if !p.IsCrossRepository && isShipHead(p.HeadRefName) {
			ships = append(ships, ShipPR{Number: p.Number, Head: p.HeadRefName, URL: p.URL, Checks: p.Checks})
		}
	}
	sort.Slice(bots, func(i, j int) bool { return bots[i].Number < bots[j].Number })
	sort.Slice(ships, func(i, j int) bool { return ships[i].Number < ships[j].Number })
	return bots, ships
}

func isShipHead(head string) bool {
	for _, prefix := range shipHeadPrefixes {
		if strings.HasPrefix(head, prefix) {
			return true
		}
	}
	return false
}

// ageDays is whole days from the PR's opening to now, floored, and never
// negative: a clock a little behind the forge's reads 0, not -1.
func ageDays(p ship.OpenPRRecord, now time.Time) int {
	days := int(now.Sub(p.CreatedAt) / (24 * time.Hour))
	if days < 0 {
		return 0
	}
	return days
}

// BotSummary is the digest's one bot-PR line — `bot PRs: 2 open (1 failing),
// oldest 12d` — or "" when there are none. The parenthetical appears only when
// something is failing; pending and none never count.
func BotSummary(bots []BotPR) string {
	if len(bots) == 0 {
		return ""
	}
	failing, oldest := 0, 0
	for _, b := range bots {
		if b.Checks == ship.ChecksFailing {
			failing++
		}
		oldest = max(oldest, b.AgeDays)
	}
	if failing == 0 {
		return fmt.Sprintf("bot PRs: %d open, oldest %dd", len(bots), oldest)
	}
	return fmt.Sprintf("bot PRs: %d open (%d failing), oldest %dd", len(bots), failing, oldest)
}

// ShipPRLine is the digest's line for one ship PR: `pr: #138 phase/x — failing`,
// followed by ` · 3 untriaged — /dross-respond 138` only when review comments
// are waiting.
func ShipPRLine(p ShipPR) string {
	line := fmt.Sprintf("pr: #%d %s — %s", p.Number, p.Head, p.Checks)
	if p.Untriaged > 0 {
		line += fmt.Sprintf(" · %d untriaged — /dross-respond %d", p.Untriaged, p.Number)
	}
	return line
}

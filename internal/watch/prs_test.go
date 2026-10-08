package watch

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/ship"
)

var prNow = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

func openPR(number int, login string, isBot bool, head string, cross bool, age time.Duration, checks ship.CheckRollup) ship.OpenPRRecord {
	return ship.OpenPRRecord{
		Number:            number,
		Title:             "PR title",
		URL:               "https://github.com/o/r/pull/1",
		Author:            ship.PRAuthor{Login: login, IsBot: isBot},
		CreatedAt:         prNow.Add(-age),
		HeadRefName:       head,
		IsCrossRepository: cross,
		Checks:            checks,
	}
}

func botNumbers(bots []BotPR) []int {
	out := []int{}
	for _, b := range bots {
		out = append(out, b.Number)
	}
	return out
}

func shipNumbers(ships []ShipPR) []int {
	out := []int{}
	for _, s := range ships {
		out = append(out, s.Number)
	}
	return out
}

func TestSplitPRsBotsByFlag(t *testing.T) {
	bots, _ := SplitPRs([]ship.OpenPRRecord{
		openPR(3, "dependabot-fan", false, "dependabot/npm/x", false, time.Hour, ship.ChecksPassing),
		openPR(2, "app/github-actions", true, "pin-bump/weekly", false, time.Hour, ship.ChecksPassing),
		openPR(1, "app/dependabot", true, "dependabot/go_modules/x", false, time.Hour, ship.ChecksFailing),
	}, prNow)
	if got := botNumbers(bots); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("bots = %v, want [1 2] — the human dependabot-fan is not a bot, whatever the login says", got)
	}
	if bots[0].Author != "app/dependabot" || bots[0].Checks != ship.ChecksFailing {
		t.Errorf("bot #1 = %+v", bots[0])
	}
}

func TestSplitPRsShipHeads(t *testing.T) {
	prs := []ship.OpenPRRecord{
		openPR(1, "rivil", false, "phase/x", false, time.Hour, ship.ChecksPending),
		openPR(2, "rivil", false, "milestone/v1.7", false, time.Hour, ship.ChecksNone),
		openPR(3, "rivil", false, "feature/x", false, time.Hour, ship.ChecksNone),
		openPR(4, "rivil", false, "phases/x", false, time.Hour, ship.ChecksNone),
		openPR(5, "rivil", false, "feature/phase/x", false, time.Hour, ship.ChecksNone),
		openPR(6, "rivil", false, "release/phase/x", false, time.Hour, ship.ChecksNone),
		openPR(7, "rivil", false, "Phase/X", false, time.Hour, ship.ChecksNone),
		openPR(8, "rivil", false, "main", false, time.Hour, ship.ChecksNone),
		openPR(9, "mallory", false, "phase/evil", true, time.Hour, ship.ChecksNone),
		openPR(10, "app/dependabot", true, "phase/x", false, time.Hour, ship.ChecksPassing),
	}
	bots, ships := SplitPRs(prs, prNow)
	if got := shipNumbers(ships); !reflect.DeepEqual(got, []int{1, 2, 10}) {
		t.Errorf("ships = %v, want [1 2 10] — same-repo phase/ and milestone/ heads only, forks out", got)
	}
	if got := botNumbers(bots); !reflect.DeepEqual(got, []int{10}) {
		t.Errorf("bots = %v, want [10] — a bot PR on phase/x lands in both lists", got)
	}
	if ships[0].Head != "phase/x" || ships[0].Checks != ship.ChecksPending {
		t.Errorf("ship #1 = %+v", ships[0])
	}
}

func TestSplitPRsNonNilAndSorted(t *testing.T) {
	bots, ships := SplitPRs(nil, prNow)
	if bots == nil || ships == nil {
		t.Fatalf("SplitPRs(nil) = (%v, %v), want two non-nil empty lists", bots, ships)
	}
	bots, ships = SplitPRs([]ship.OpenPRRecord{
		openPR(150, "app/dependabot", true, "phase/b", false, time.Hour, ship.ChecksNone),
		openPR(7, "app/dependabot", true, "phase/a", false, time.Hour, ship.ChecksNone),
		openPR(42, "app/dependabot", true, "milestone/c", false, time.Hour, ship.ChecksNone),
	}, prNow)
	if got := botNumbers(bots); !sort.IntsAreSorted(got) || len(got) != 3 {
		t.Errorf("bots = %v, want sorted by number", got)
	}
	if got := shipNumbers(ships); !sort.IntsAreSorted(got) || len(got) != 3 {
		t.Errorf("ships = %v, want sorted by number", got)
	}
}

func TestPRAgeDays(t *testing.T) {
	plus5 := time.FixedZone("+05:00", 5*60*60)
	for _, tc := range []struct {
		name    string
		created time.Time
		want    int
	}{
		{"12d1h", prNow.Add(-(12*24 + 1) * time.Hour), 12},
		{"23h", prNow.Add(-23 * time.Hour), 0},
		{"47h59m", prNow.Add(-(47*time.Hour + 59*time.Minute)), 1},
		{"exactly 48h", prNow.Add(-48 * time.Hour), 2},
		{"3h in the future", prNow.Add(3 * time.Hour), 0},
		{"+05:00 offset", prNow.Add(-(12*24 + 1) * time.Hour).In(plus5), 12},
	} {
		p := ship.OpenPRRecord{Number: 1, Author: ship.PRAuthor{IsBot: true}, CreatedAt: tc.created}
		bots, _ := SplitPRs([]ship.OpenPRRecord{p}, prNow)
		if bots[0].AgeDays != tc.want {
			t.Errorf("%s: age_days = %d, want %d", tc.name, bots[0].AgeDays, tc.want)
		}
	}
}

func TestBotSummary(t *testing.T) {
	bot := func(age int, checks ship.CheckRollup) BotPR { return BotPR{Number: age, AgeDays: age, Checks: checks} }
	for _, tc := range []struct {
		name string
		bots []BotPR
		want string
	}{
		{"none", nil, ""},
		{"zero failing drops the parenthetical", []BotPR{bot(3, ship.ChecksPassing), bot(12, ship.ChecksPending)}, "bot PRs: 2 open, oldest 12d"},
		{"one failing", []BotPR{bot(3, ship.ChecksFailing), bot(12, ship.ChecksPassing)}, "bot PRs: 2 open (1 failing), oldest 12d"},
		{"pending and none never count", []BotPR{bot(3, ship.ChecksPending), bot(12, ship.ChecksNone)}, "bot PRs: 2 open, oldest 12d"},
		{"single passing", []BotPR{bot(3, ship.ChecksPassing)}, "bot PRs: 1 open, oldest 3d"},
	} {
		if got := BotSummary(tc.bots); got != tc.want {
			t.Errorf("%s: BotSummary = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestShipPRLine(t *testing.T) {
	for _, tc := range []struct {
		pr   ShipPR
		want string
	}{
		{ShipPR{Number: 138, Head: "phase/x", Checks: ship.ChecksFailing}, "pr: #138 phase/x — failing"},
		{ShipPR{Number: 7, Head: "milestone/v1.7", Checks: ship.ChecksNone}, "pr: #7 milestone/v1.7 — none"},
	} {
		if got := ShipPRLine(tc.pr); got != tc.want {
			t.Errorf("ShipPRLine(%+v) = %q, want %q", tc.pr, got, tc.want)
		}
	}
}

func TestShipPRLineUntriaged(t *testing.T) {
	p := ShipPR{Number: 138, Head: "phase/x", Checks: ship.ChecksFailing}
	if got := ShipPRLine(p); got != "pr: #138 phase/x — failing" {
		t.Errorf("no untriaged comments: %q, want the line unchanged", got)
	}
	p.Untriaged = 3
	if got, want := ShipPRLine(p), "pr: #138 phase/x — failing · 3 untriaged — /dross-respond 138"; got != want {
		t.Errorf("ShipPRLine = %q, want %q", got, want)
	}
	p.Untriaged = 1
	if got, want := ShipPRLine(p), "pr: #138 phase/x — failing · 1 untriaged — /dross-respond 138"; got != want {
		t.Errorf("ShipPRLine = %q, want %q", got, want)
	}
}

func jsonKeys(t *testing.T, v any) []string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestPRRecordJSONKeys (pr_age): the digest items carry exactly these keys —
// no stale flag, no threshold.
func TestPRRecordJSONKeys(t *testing.T) {
	if got, want := jsonKeys(t, BotPR{}), []string{"age_days", "author", "checks", "number", "title", "url"}; !reflect.DeepEqual(got, want) {
		t.Errorf("BotPR keys = %v, want %v", got, want)
	}
	if got, want := jsonKeys(t, ShipPR{}), []string{"checks", "head", "number", "url"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ShipPR keys = %v, want %v (untriaged omitted at 0)", got, want)
	}
	if got, want := jsonKeys(t, ShipPR{Untriaged: 3}), []string{"checks", "head", "number", "untriaged", "url"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ShipPR keys with a count = %v, want %v", got, want)
	}
}

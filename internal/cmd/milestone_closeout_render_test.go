package cmd

import (
	"errors"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/milestone"
)

func renderFixture() closeoutReport {
	return closeoutReport{Items: []closeoutItem{
		{Kind: closeoutSurvivor, ID: "survivor:K", Subject: "f.go:12", Detail: "f.go:12 (op) recorded by p1", Clear: "`dross survivor accept f.go:12`"},
		{Kind: closeoutDeferred, ID: "deferred:src#1", Subject: "src#1", Detail: "src deferred[1] → p1: x", Clear: "`dross deferred absorb src 1`"},
		{Kind: closeoutNotDone, ID: "phase:built", Subject: "built", Detail: "no shipped or complete record", Clear: "finish it"},
		{Kind: closeoutUnverified, Subject: "unv", Detail: "no verify.toml", Clear: "/dross-verify unv, or pass --force-unverified"},
	}}
}

// c-4: one item per kind yields four headings in fixed order, each line with
// its clearing command.
func TestRenderCloseoutListsEveryKindInOrder(t *testing.T) {
	out := renderCloseout(joinCloseoutAcks(renderFixture(), nil))
	var last int
	for _, kind := range closeoutKinds {
		h := closeoutKindTitle[kind] + " (1):"
		i := strings.Index(out, h)
		if i < 0 {
			t.Fatalf("render lacks heading %q:\n%s", h, out)
		}
		if i < last {
			t.Errorf("heading %q out of order:\n%s", h, out)
		}
		last = i
	}
	for _, it := range renderFixture().Items {
		if !strings.Contains(out, "clear: "+it.Clear) {
			t.Errorf("render drops the clearing command for %s:\n%s", it.Subject, out)
		}
	}
	if !strings.HasPrefix(out, "close-out: 4 unresolved\n") {
		t.Errorf("render header wrong:\n%s", out)
	}
}

// c-11 and the ack/unverified boundary.
func TestJoinCloseoutAcks(t *testing.T) {
	at := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)
	r := renderFixture()
	r.Errors = []closeoutError{{Kind: closeoutDeferred, Err: errors.New("spec broke")}}
	acks := []milestone.CloseoutAck{
		{Item: "phase:built", Reason: "moved to v2.0", At: at},
		{Item: "phase:gone", Reason: "was not done", At: at},
		// unv is only unverified: the ack matches no ackable item.
		{Item: "phase:unv", Reason: "trust me", At: at},
	}
	j := joinCloseoutAcks(r, acks)

	if len(j.Applied) != 1 || j.Applied[0].Ack.Item != "phase:built" {
		t.Errorf("applied = %+v, want phase:built only", j.Applied)
	}
	stale := map[string]bool{}
	for _, a := range j.Stale {
		stale[a.Item] = true
	}
	if !stale["phase:gone"] || !stale["phase:unv"] || len(j.Stale) != 2 {
		t.Errorf("stale = %+v, want phase:gone and phase:unv", j.Stale)
	}
	var unvOpen bool
	for _, it := range j.Open {
		if it.Kind == closeoutUnverified && it.Subject == "unv" {
			unvOpen = true
		}
		if it.ID == "phase:built" {
			t.Error("an acked item is still open")
		}
	}
	if !unvOpen {
		t.Error("ack phase:unv cleared the unverified item")
	}
	// 3 open items (unverified, deferred, survivor) + 1 error; the stale
	// acks count for nothing.
	if got := j.Blocking(); got != 4 {
		t.Errorf("Blocking() = %d, want 4 (stale acks must not count, errors must)", got)
	}

	out := renderCloseout(j)
	if !strings.Contains(out, "acknowledged (1):\n  phase:built — moved to v2.0") {
		t.Errorf("render lacks the applied ack:\n%s", out)
	}
	staleAt := strings.Index(out, "stale acknowledgements (2)")
	if staleAt < 0 || !strings.Contains(out[staleAt:], "phase:gone") {
		t.Errorf("render lacks the stale heading with phase:gone:\n%s", out)
	}
	if !strings.Contains(out, "ERROR — spec broke") {
		t.Errorf("render lacks the error line:\n%s", out)
	}
}

// An error line is never cleared by an ack, whatever the ack names.
func TestCloseoutErrorIsNeverAcked(t *testing.T) {
	r := closeoutReport{Errors: []closeoutError{{Kind: closeoutSurvivor, Err: errors.New("survivors.toml broke")}}}
	j := joinCloseoutAcks(r, []milestone.CloseoutAck{{Item: "survivor:K", Reason: "r"}})
	if j.Blocking() != 1 {
		t.Errorf("Blocking() = %d, want the error line to block", j.Blocking())
	}
}

var wallClock = regexp.MustCompile(`\d{4}-\d{2}-\d{2}|\d{2}:\d{2}:\d{2}`)

// c-8: an empty milestone — no record, or no phases — blocks nothing, and
// renders are byte-stable with no wall-clock value.
func TestLoadCloseoutEmpty(t *testing.T) {
	for name, root := range map[string]string{
		"no record":    filepath.Join(t.TempDir(), ".dross"),
		"empty phases": ckRoot(t),
	} {
		j, err := loadCloseout(root, "v9.9")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if j.Blocking() != 0 {
			t.Errorf("%s: Blocking() = %d, want 0", name, j.Blocking())
		}
		if out := renderCloseout(j); out != "close-out: nothing unresolved\n" {
			t.Errorf("%s: render = %q", name, out)
		}
	}
}

func TestRenderCloseoutIsDeterministic(t *testing.T) {
	root := ckRoot(t, "built", "ghost")
	ckPhase(t, root, "built", "")
	m, err := milestone.Load(milestone.FilePath(root, "v9.9"))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Ack("phase:built", "moved", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := m.Save(milestone.FilePath(root, "v9.9")); err != nil {
		t.Fatal(err)
	}
	first, err := loadCloseout(root, "v9.9")
	if err != nil {
		t.Fatal(err)
	}
	second, err := loadCloseout(root, "v9.9")
	if err != nil {
		t.Fatal(err)
	}
	a, b := renderCloseout(first), renderCloseout(second)
	if a != b {
		t.Errorf("two renders differ:\n%s\n---\n%s", a, b)
	}
	if wallClock.MatchString(a) {
		t.Errorf("render carries a wall-clock value:\n%s", a)
	}
	if len(first.Applied) != 1 {
		t.Errorf("loadCloseout did not join the saved ack: %+v", first)
	}
}

// Within a kind, items sharing a subject order by ack id, numerically: from
// one source, deferred:src#2 lists before deferred:src#10 whichever arrives
// first, and an item is never less than itself.
func TestCloseoutLessTieBreaksOnIDNumerically(t *testing.T) {
	two := closeoutItem{Kind: closeoutDeferred, ID: "deferred:src#2", Subject: "src"}
	ten := closeoutItem{Kind: closeoutDeferred, ID: "deferred:src#10", Subject: "src"}
	if !closeoutLess(two, ten) || closeoutLess(ten, two) {
		t.Errorf("closeoutLess(#2, #10) = %v, (#10, #2) = %v; want #2 first",
			closeoutLess(two, ten), closeoutLess(ten, two))
	}
	if closeoutLess(two, two) {
		t.Error("closeoutLess(x, x) = true: an item is less than itself")
	}
	for name, in := range map[string][]closeoutItem{
		"#10 first": {ten, two},
		"#2 first":  {two, ten},
	} {
		items := slices.Clone(in)
		sortCloseoutItems(items)
		if items[0].ID != two.ID || items[1].ID != ten.ID {
			t.Errorf("%s: sorted = %s, %s; want deferred:src#2 then deferred:src#10", name, items[0].ID, items[1].ID)
		}
	}

	// Identical keys keep their arrival order: a tie-break that called equal
	// items less would swap them.
	a := closeoutItem{Kind: closeoutDeferred, ID: "deferred:src#2", Subject: "src", Detail: "first"}
	b := a
	b.Detail = "second"
	items := []closeoutItem{a, b}
	sortCloseoutItems(items)
	if items[0].Detail != "first" {
		t.Errorf("equal items reordered: %q before %q", items[0].Detail, items[1].Detail)
	}
}

// The rendered report lists one source's deferred items in index order, and
// stale acks the same way.
func TestRenderCloseoutOrdersIndexesNumerically(t *testing.T) {
	r := closeoutReport{Items: []closeoutItem{
		{Kind: closeoutDeferred, ID: "deferred:src#10", Subject: "src#10", Detail: "ten", Clear: "c10"},
		{Kind: closeoutDeferred, ID: "deferred:src#2", Subject: "src#2", Detail: "two", Clear: "c2"},
	}}
	acks := []milestone.CloseoutAck{
		{Item: "deferred:old#10", Reason: "r10"},
		{Item: "deferred:old#2", Reason: "r2"},
	}
	out := renderCloseout(joinCloseoutAcks(r, acks))
	for _, pair := range [][2]string{
		{"deferred:src#2 — two", "deferred:src#10 — ten"},
		{"deferred:old#2 — r2", "deferred:old#10 — r10"},
	} {
		i, k := strings.Index(out, pair[0]), strings.Index(out, pair[1])
		if i < 0 || k < 0 || i > k {
			t.Errorf("want %q before %q:\n%s", pair[0], pair[1], out)
		}
	}
}

func TestNaturalCompare(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"src#2", "src#10", -1},
		{"f.go:9", "f.go:10", -1},
		{"a19", "a21", -1},
		{"a10b", "a10c", -1},
		{"a10", "a10", 0},
		{"a", "b", -1},
		{"a", "ab", -1},
		{"a1", "a1b", -1},
		{"a9", "ab", -1},
		{"a#", "a1", -1},
		// Equal as numbers, so byte order decides.
		{"#02", "#2", -1},
		{"001", "1", -1},
		{"0", "00", -1},
		// A zero is a digit: a0b and a00c are 0 then b, 0 then c.
		{"a0b", "a00c", -1},
		{"", "", 0},
		{"", "0", -1},
	} {
		if got := naturalCompare(tc.a, tc.b); got != tc.want {
			t.Errorf("naturalCompare(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
		if got := naturalCompare(tc.b, tc.a); got != -tc.want {
			t.Errorf("naturalCompare(%q, %q) = %d, want %d", tc.b, tc.a, got, -tc.want)
		}
	}
}

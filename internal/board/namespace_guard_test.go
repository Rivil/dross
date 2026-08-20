package board

import (
	"reflect"
	"testing"
)

// Namespace escape guard (c-5).
//
// IsLinked is what keeps dross's own mirrors out of the inbound triage feed,
// and it walks the link namespaces by hand. That hand-written walk is the
// failure mode: `backlog` and `tasks` were added to Board as later features and
// nobody went back to the filter, so ~115 mirrors reached the feed and asked
// the user to triage dross's own writing.
//
// The guard therefore enumerates namespaces by REFLECTION over the struct
// rather than from a list a reader has to remember to update — a hand-written
// list is the same class of thing as the hand-written walk, and would fall out
// of date in the same way. A namespace added to Board and not to IsLinked fails
// here, by field name, the day the field is added.

// namespaceSentinel is deliberately issue-key shaped (a project prefix, a dash,
// a number). The Milestones namespace is gated on that shape — see
// milestoneIssueShape — so a sentinel like "sentinel" would make the milestones
// case fail for the wrong reason.
const namespaceSentinel = "SENT-1"

// linkNamespaces returns the name of every map-typed field on Board: the
// link registries. Non-map fields are skipped rather than listed as exclusions,
// so Dismissed ([]string) and LastPull (time.Time) need no exemption and a new
// non-link field never has to be added to one.
func linkNamespaces(t *testing.T) []string {
	t.Helper()
	typ := reflect.TypeOf(Board{})
	var out []string
	for i := 0; i < typ.NumField(); i++ {
		if typ.Field(i).Type.Kind() == reflect.Map {
			out = append(out, typ.Field(i).Name)
		}
	}
	if len(out) == 0 {
		t.Fatal("reflection found no map-typed fields on Board — the guard would pass vacuously")
	}
	return out
}

// plantSentinel writes namespaceSentinel into one namespace as an issue id.
//
// Every namespace records an issue id, but not all of them record it the same
// way: Tasks holds a record whose Issue field carries the id alongside the
// agreement point. A namespace whose element is neither a string nor a record
// with an Issue field fails loudly rather than being silently skipped — a
// silently skipped namespace is exactly the hole this guard exists to close.
func plantSentinel(t *testing.T, b *Board, field string) {
	t.Helper()
	m := reflect.ValueOf(b).Elem().FieldByName(field)
	if m.IsNil() {
		m.Set(reflect.MakeMap(m.Type()))
	}
	elem := reflect.New(m.Type().Elem()).Elem()
	switch elem.Kind() {
	case reflect.String:
		elem.SetString(namespaceSentinel)
	case reflect.Struct:
		issue := elem.FieldByName("Issue")
		if !issue.IsValid() || issue.Kind() != reflect.String {
			t.Fatalf("namespace %s holds %s, which has no string Issue field — teach plantSentinel how that namespace records an issue id",
				field, m.Type().Elem())
		}
		issue.SetString(namespaceSentinel)
	default:
		t.Fatalf("namespace %s holds %s — teach plantSentinel how that namespace records an issue id", field, m.Type().Elem())
	}
	m.SetMapIndex(reflect.ValueOf("sentinel-key"), elem)
}

// TestEveryBoardNamespaceIsFiltered is the guard itself: an issue recorded in
// ANY link namespace must count as linked, so it never reaches inbound triage.
func TestEveryBoardNamespaceIsFiltered(t *testing.T) {
	for _, field := range linkNamespaces(t) {
		t.Run(field, func(t *testing.T) {
			b := New()
			plantSentinel(t, b, field)
			if !b.IsLinked(namespaceSentinel) {
				t.Errorf("an issue recorded in %s is not filtered by IsLinked — every %s mirror would reach the inbound triage feed, asking the user to triage dross's own writing",
					field, field)
			}
		})
	}
}

// TestNamespaceReflectionSkipsNonLinkFields pins the selection rule. Selecting
// by "exported field" rather than by map type would drag Dismissed and LastPull
// in and fail on day one; selecting from a hand-written list would drift.
func TestNamespaceReflectionSkipsNonLinkFields(t *testing.T) {
	got := map[string]bool{}
	for _, f := range linkNamespaces(t) {
		got[f] = true
	}
	for _, notALink := range []string{"Dismissed", "LastPull"} {
		if got[notALink] {
			t.Errorf("%s is not a link namespace but the reflection selected it — the selector must be map-typed fields, not exported fields", notALink)
		}
	}
	// The namespaces that exist today, as a floor. A namespace REMOVED from
	// Board is a real change worth failing on; a namespace added is caught by
	// TestEveryBoardNamespaceIsFiltered without touching this list.
	for _, want := range []string{"Milestones", "Phases", "Quicks", "Backlog", "Tasks"} {
		if !got[want] {
			t.Errorf("%s is no longer a map-typed field on Board — if the namespace really went away, drop it here; if it changed shape, the guard has stopped covering it", want)
		}
	}
}

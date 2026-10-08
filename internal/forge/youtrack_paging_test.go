package forge

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// The tag index and the issue list are both collections YouTrack truncates
// silently, and both were read in one shot. These tests are written against
// indexes LARGER than one page for exactly that reason: at any size that fits
// in a single page, a paginating client and a capped one are indistinguishable,
// which is how the bug survived a green suite.

// TestTagIndexIsReadWholeAcrossPages is the live failure, reproduced. The
// instance had 1243 tags, `loadTags` asked for one page of 1000, and the 243 it
// never saw included the tag the caller was about to resolve — so `ensureTag`
// concluded the name was free, POSTed it, and took an HTTP 400 naming the tag
// that already existed.
func TestTagIndexIsReadWholeAcrossPages(t *testing.T) {
	f := newYTTagFake()
	// Deliberately more than one page; the names sort so that "zz-…" lands on
	// the last page, which is the half a capped read cannot reach.
	for i := 0; i < ytPageSize+243; i++ {
		f.nextID++
		f.index[fmt.Sprintf("tag-%05d", i)] = fmt.Sprintf("t-%d", f.nextID)
	}
	const late = "zz-late-tag"
	f.nextID++
	f.index[late] = "t-late"

	c, srv := newTestYTClient(t, f.handler(t))
	defer srv.Close()

	index, err := c.loadTags()
	if err != nil {
		t.Fatal(err)
	}
	if len(index) != len(f.index) {
		t.Errorf("read %d of %d tags — the walk stopped short", len(index), len(f.index))
	}

	id, err := c.ensureTag(late)
	if err != nil {
		t.Fatalf("resolving a tag on the last page failed: %v", err)
	}
	if id != "t-late" {
		t.Errorf("ensureTag returned %q, want the existing id t-late", id)
	}
	if len(f.created) != 0 {
		t.Errorf("ensureTag created %v — it re-created a tag that already existed, which is the HTTP 400 the live board returned", f.created)
	}
}

// TestTagPagingStopsOnAShortPage is the precision half. A walk that kept asking
// past the end would satisfy the assertions above and hammer the server, and a
// walk that stopped on the first EMPTY page rather than the first SHORT one
// would cost an extra request per read forever.
func TestTagPagingStopsOnAShortPage(t *testing.T) {
	f := newYTTagFake()
	for i := 0; i < ytPageSize+1; i++ {
		f.nextID++
		f.index[fmt.Sprintf("tag-%05d", i)] = fmt.Sprintf("t-%d", f.nextID)
	}
	var gets int
	c, srv := newTestYTClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/issueTags" && r.Method == "GET" {
			gets++
		}
		f.handler(t)(w, r)
	})
	defer srv.Close()

	if _, err := c.loadTags(); err != nil {
		t.Fatal(err)
	}
	if gets != 2 {
		t.Errorf("read %d pages for %d tags, want exactly 2 — one full page then one short", gets, len(f.index))
	}
}

// TestTagIndexIsCachedAcrossCalls pins the contract loadTags already had, now
// that it costs more than one request: the pages are walked once per run, not
// once per resolution.
func TestTagIndexIsCachedAcrossCalls(t *testing.T) {
	f := newYTTagFake("alpha", "beta")
	var gets int
	c, srv := newTestYTClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/issueTags" && r.Method == "GET" {
			gets++
		}
		f.handler(t)(w, r)
	})
	defer srv.Close()

	for range 3 {
		if _, err := c.loadTags(); err != nil {
			t.Fatal(err)
		}
	}
	if gets != 1 {
		t.Errorf("walked the index %d times, want 1 — the cache no longer holds", gets)
	}
}

// TestAServerIgnoringSkipIsAnErrorNotATruncation: a server that hands back a
// full page regardless of $skip would loop forever, and the tempting fix — stop
// after N pages and return what you have — silently re-creates the truncation
// this whole change removes. It must fail loudly instead.
func TestAServerIgnoringSkipIsAnErrorNotATruncation(t *testing.T) {
	page := make([]string, ytPageSize)
	for i := range page {
		page[i] = fmt.Sprintf(`{"id":"t-%d","name":"tag-%d"}`, i, i)
	}
	body := "[" + strings.Join(page, ",") + "]"
	c, srv := newTestYTClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, body) // same full page, whatever $skip says
	})
	defer srv.Close()

	_, err := c.loadTags()
	if err == nil {
		t.Fatal("a server ignoring $skip was read as a complete index")
	}
	if !strings.Contains(err.Error(), "ignore $skip") {
		t.Errorf("the error does not name the cause: %v", err)
	}
}

// TestListIssuesReadsEveryPage covers the second capped read. ListIssues sent
// no $top at all, so it took the server's own default page — the read behind a
// backlog sync that reported 0 closed and a doctor that kept reporting stranded
// mirrors. The fake's default page is deliberately small, so a client that asks
// for nothing gets a fraction of the set.
func TestListIssuesReadsEveryPage(t *testing.T) {
	const total = ytFakeDefaultPage * 3
	c, srv := newTestYTClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/issues") {
			_, _ = io.WriteString(w, "[]")
			return
		}
		all := make([]string, total)
		for i := range all {
			all[i] = fmt.Sprintf(`{"idReadable":"DRO-%d","summary":"issue %d"}`, i, i)
		}
		var names []string
		for i := range all {
			names = append(names, fmt.Sprint(i))
		}
		window := ytPageWindow(names, r.URL.Query())
		out := make([]string, 0, len(window))
		for _, n := range window {
			var idx int
			_, _ = fmt.Sscanf(n, "%d", &idx)
			out = append(out, all[idx])
		}
		_, _ = io.WriteString(w, "["+strings.Join(out, ",")+"]")
	})
	defer srv.Close()

	got, err := c.ListIssues(IssueFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != total {
		t.Errorf("read %d of %d issues — the list is truncated at the server's default page", len(got), total)
	}
}

// TestListIssuesAsksForAPageSize is the mechanism behind the test above, pinned
// directly: the bug was not a wrong page size, it was sending no $top at all.
func TestListIssuesAsksForAPageSize(t *testing.T) {
	var sawTop, sawSkip bool
	c, srv := newTestYTClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/issues") {
			q := r.URL.Query()
			sawTop = q.Get("$top") != ""
			sawSkip = q.Get("$skip") != ""
		}
		_, _ = io.WriteString(w, "[]")
	})
	defer srv.Close()

	if _, err := c.ListIssues(IssueFilter{}); err != nil {
		t.Fatal(err)
	}
	if !sawTop || !sawSkip {
		t.Errorf("ListIssues sent $top=%v $skip=%v — a request naming neither takes whatever the server feels like returning", sawTop, sawSkip)
	}
}

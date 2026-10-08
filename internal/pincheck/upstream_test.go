package pincheck

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// proxyStub serves canned bodies by exact path; any other path is a 404, the
// proxy's answer for a path that is not a module.
func proxyStub(t *testing.T, bodies map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := bodies[r.URL.EscapedPath()]
		if !ok {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func stubResolver(url string) *HTTPResolver {
	return NewResolver(Endpoints{GoProxy: url, NodeDist: url, NPM: url}, 2*time.Second)
}

func info(v, ts string) string { return fmt.Sprintf(`{"Version":%q,"Time":%q}`, v, ts) }

func TestFetchWalksUpToModuleRoot(t *testing.T) {
	srv := proxyStub(t, map[string]string{
		"/golang.org/x/vuln/@v/list":        "v1.7.0\nv1.8.0\nv1.8.1\n",
		"/golang.org/x/vuln/@v/v1.8.1.info": info("v1.8.1", "2026-09-01T00:00:00Z"),
	})
	site := Site{Kind: KindGoInstall, Name: "golang.org/x/vuln/cmd/govulncheck", Version: "v1.8.0", Pinned: true}
	rels, err := stubResolver(srv.URL).Releases(context.Background(), site)
	if err != nil {
		t.Fatalf("Releases: %v", err)
	}
	if got := versionsOf(rels); got != "v1.7.0 v1.8.0 v1.8.1" {
		t.Fatalf("versions = %s, want the golang.org/x/vuln list", got)
	}
	if !rels[2].Published.Equal(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("v1.8.1 published %v, want its .info time", rels[2].Published)
	}

	// Every prefix 404s: nothing to judge against.
	empty := proxyStub(t, nil)
	if got := Judge(context.Background(), stubResolver(empty.URL), site, classifyNow); got.Verdict != Unknown || got.Reason == "" {
		t.Errorf("no module on any prefix: got %+v, want unknown with a reason", got)
	}
}

func versionsOf(rels []Release) string {
	var vs []string
	for _, r := range rels {
		vs = append(vs, r.Version)
	}
	return strings.Join(vs, " ")
}

func TestFetchGoreleaserActionVersion(t *testing.T) {
	srv := proxyStub(t, map[string]string{
		"/github.com/goreleaser/goreleaser/v2/@v/list":         "v2.18.2\nv2.19.0\n",
		"/github.com/goreleaser/goreleaser/v2/@v/v2.19.0.info": info("v2.19.0", "2026-09-01T00:00:00Z"),
		"/github.com/goreleaser/goreleaser/@v/list":            "v1.26.1\nv1.26.2\n",
	})
	r := stubResolver(srv.URL)
	for _, tc := range []struct{ version, want string }{
		{"v2.18.2", "v2.18.2 v2.19.0"},
		{"v1.26.2", "v1.26.1 v1.26.2"},
	} {
		rels, err := r.Releases(context.Background(), Site{Kind: KindGoreleaserAction, Name: "goreleaser", Version: tc.version, Pinned: true})
		if err != nil {
			t.Fatalf("%s: %v", tc.version, err)
		}
		if got := versionsOf(rels); got != tc.want {
			t.Errorf("%s resolved to %s, want %s", tc.version, got, tc.want)
		}
	}
	pro := Site{Kind: KindGoreleaserAction, Name: "goreleaser-pro", Version: "v2.18.2", Pinned: true}
	if got := Judge(context.Background(), r, pro, classifyNow); got.Verdict != Unknown || !strings.Contains(got.Reason, "goreleaser-pro") {
		t.Errorf("goreleaser-pro: got %+v, want unknown naming the distribution", got)
	}
}

func TestFetchFailuresAreUnknown(t *testing.T) {
	site := Site{Kind: KindNode, Name: "node", Version: "24.19.0", Pinned: true}
	status := func(code int) *httptest.Server {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }))
		t.Cleanup(srv.Close)
		return srv
	}
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(slow.Close)
	t.Cleanup(func() { close(release) })
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	malformed := proxyStub(t, map[string]string{"/index.json": `[{"version":"v24.20.0",`})

	for _, tc := range []struct {
		name string
		url  string
		site Site
	}{
		{"503", status(http.StatusServiceUnavailable).URL, site},
		{"410 on every candidate", status(http.StatusGone).URL, Site{Kind: KindGoInstall, Name: "example.com/a/cmd/b", Version: "v1.0.0", Pinned: true}},
		{"timeout", slow.URL, site},
		{"closed listener", closed.URL, site},
		{"malformed JSON", malformed.URL, site},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewResolver(Endpoints{GoProxy: tc.url, NodeDist: tc.url, NPM: tc.url}, 200*time.Millisecond)
			got := Judge(context.Background(), r, tc.site, classifyNow)
			if got.Verdict != Unknown || got.Reason == "" {
				t.Fatalf("got %+v, want unknown with a reason — never current", got)
			}
		})
	}
}

func TestFetchReadsTheListNotLatest(t *testing.T) {
	srv := proxyStub(t, map[string]string{
		"/github.com/go-gremlins/gremlins/@latest": info("v0.5.1", "2026-06-01T00:00:00Z"),
		"/github.com/go-gremlins/gremlins/@v/list": "v0.5.0\nv0.5.1\nv0.6.0\n",
	})
	site := Site{Kind: KindGoInstall, Name: "github.com/go-gremlins/gremlins/cmd/gremlins", Version: "v0.6.0", Pinned: true}
	got := Judge(context.Background(), stubResolver(srv.URL), site, classifyNow)
	if got.Verdict != Current || got.Latest != "v0.6.0" {
		t.Fatalf("got %+v, want current with v0.6.0 as the list maximum", got)
	}
}

func TestFetchParsesEachUpstream(t *testing.T) {
	srv := proxyStub(t, map[string]string{
		// Proxy list + info, with an upper-case path escaped the proxy's way.
		"/github.com/!burnt!sushi/toml/@v/list":        "v1.5.0\nv1.6.0\n",
		"/github.com/!burnt!sushi/toml/@v/v1.6.0.info": info("v1.6.0", "2026-08-02T03:04:05Z"),
		// nodejs.org/dist/index.json: newest first, dated by day.
		"/index.json": `[{"version":"v26.1.0","date":"2026-09-20","lts":false},{"version":"v24.20.0","date":"2026-09-07","lts":"Krypton"},{"version":"v24.19.0","date":"2026-08-10","lts":"Krypton"}]`,
		// npm packument: `time` carries created/modified besides versions.
		"/@stryker-mutator%2Fcore": `{"name":"@stryker-mutator/core","time":{"created":"2018-01-01T00:00:00.000Z","modified":"2026-09-02T00:00:00.000Z","9.6.1":"2026-07-01T10:00:00.000Z","9.6.2":"2026-09-01T10:00:00.000Z"}}`,
		// golang.org/toolchain: per-platform entries and an rc interleaved.
		"/golang.org/toolchain/@v/list": strings.Join([]string{
			"v0.0.1-go1.27.1.linux-amd64", "v0.0.1-go1.27rc1.linux-amd64", "v0.0.1-go1.27.1.darwin-arm64",
			"v0.0.1-go1.27.2.darwin-arm64", "v0.0.1-go1.27rc1.darwin-arm64", "v0.0.1-go1.27.2.linux-amd64",
		}, "\n"),
		"/golang.org/toolchain/@v/v0.0.1-go1.27.2.darwin-arm64.info": info("v0.0.1-go1.27.2.darwin-arm64", "2026-09-10T00:00:00Z"),
	})
	r := stubResolver(srv.URL)
	ctx := context.Background()

	for _, tc := range []struct {
		name  string
		site  Site
		want  string
		timed map[string]time.Time
	}{
		{
			name: "proxy list + info",
			site: Site{Kind: KindGoInstall, Name: "github.com/BurntSushi/toml", Version: "v1.5.0", Pinned: true},
			want: "v1.5.0 v1.6.0",
			timed: map[string]time.Time{
				"v1.6.0": time.Date(2026, 8, 2, 3, 4, 5, 0, time.UTC),
			},
		},
		{
			name: "nodejs index.json",
			site: Site{Kind: KindNode, Name: "node", Version: "24.19.0", Pinned: true},
			want: "26.1.0 24.20.0 24.19.0",
			timed: map[string]time.Time{
				"24.20.0": time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC),
				"26.1.0":  time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "npm packument",
			site: Site{Kind: KindNPM, Name: "@stryker-mutator/core", Version: "9.6.1", Pinned: true},
			want: "9.6.1 9.6.2",
			timed: map[string]time.Time{
				"9.6.2": time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "golang.org/toolchain",
			site: Site{Kind: KindGoToolchain, Name: "go", Version: "go1.27.1", Pinned: true},
			want: "go1.27.1 go1.27.2",
			timed: map[string]time.Time{
				"go1.27.2": time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rels, err := r.Releases(ctx, tc.site)
			if err != nil {
				t.Fatalf("Releases: %v", err)
			}
			if tc.site.Kind == KindNPM {
				sortReleases(rels)
			}
			if got := versionsOf(rels); got != tc.want {
				t.Fatalf("versions = %q, want %q", got, tc.want)
			}
			for _, rel := range rels {
				if want, ok := tc.timed[rel.Version]; ok && !rel.Published.Equal(want) {
					t.Errorf("%s published %v, want %v", rel.Version, rel.Published, want)
				}
			}
		})
	}
}

// sortReleases orders releases by version string — npm's `time` is a map.
func sortReleases(rels []Release) {
	for i := 1; i < len(rels); i++ {
		for j := i; j > 0 && rels[j].Version < rels[j-1].Version; j-- {
			rels[j], rels[j-1] = rels[j-1], rels[j]
		}
	}
}

func TestJudgeUnpinnedSkipsTheFetch(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls++ }))
	t.Cleanup(srv.Close)
	got := Judge(context.Background(), stubResolver(srv.URL), Site{Kind: KindNode, Reason: "node-version: 24.x is not exact"}, classifyNow)
	if got.Verdict != Unpinned || got.Reason == "" || calls != 0 {
		t.Errorf("unpinned site: got %+v with %d fetches, want unpinned, its reason, no fetch", got, calls)
	}
}

func TestResolverReadsEachURLOnce(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		fmt.Fprint(w, `[{"version":"v24.19.0","date":"2026-08-10"}]`)
	}))
	t.Cleanup(srv.Close)
	r := stubResolver(srv.URL)
	site := Site{Kind: KindNode, Name: "node", Version: "24.19.0", Pinned: true}
	for range 2 {
		if _, err := r.Releases(context.Background(), site); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Errorf("two node sites fetched index.json %d times, want 1", calls)
	}
}

// TestGoreleaserModule: goreleaser-action's version input may be written with
// or without its v — both forms name the same module, and a version that is
// not semver has none.
func TestGoreleaserModule(t *testing.T) {
	for _, tc := range []struct{ version, want string }{
		{"v2.18.2", "github.com/goreleaser/goreleaser/v2"},
		{"2.18.2", "github.com/goreleaser/goreleaser/v2"},
		{"v1.26.2", "github.com/goreleaser/goreleaser"},
		{"1.26.2", "github.com/goreleaser/goreleaser"},
	} {
		got, err := goreleaserModule("goreleaser", tc.version)
		if err != nil || got != tc.want {
			t.Errorf("goreleaserModule(%q) = %q, %v; want %q", tc.version, got, err, tc.want)
		}
	}
	if got, err := goreleaserModule("goreleaser", "2.x"); err == nil {
		t.Errorf("goreleaserModule(\"2.x\") = %q, want an error: not semver", got)
	}
}

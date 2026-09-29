package pincheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

// Resolver fetches the release list a site is judged against. Any failure is
// an error, and an error is an Unknown verdict — never a pass.
//
// It is an interface so a caller outside this package can hold a resolver (a
// seam for tests, a counting wrapper) without importing net/http.
type Resolver interface {
	Releases(ctx context.Context, s Site) ([]Release, error)
}

// Endpoints are the upstream base URLs a resolver reads. Tests point them at
// httptest servers.
type Endpoints struct {
	GoProxy  string // Go module proxy: module lists, .info times, golang.org/toolchain
	NodeDist string // nodejs.org/dist: index.json
	NPM      string // the npm registry: package documents
}

// DefaultEndpoints are the public upstreams.
func DefaultEndpoints() Endpoints {
	return Endpoints{
		GoProxy:  "https://proxy.golang.org",
		NodeDist: "https://nodejs.org/dist",
		NPM:      "https://registry.npmjs.org",
	}
}

// maxBody caps any upstream response. npm package documents are the largest
// thing read, and a few MB covers them.
const maxBody = 32 << 20

// HTTPResolver reads release lists over unauthenticated HTTPS GETs. Each
// request is bounded by Timeout; a response is read once per resolver and
// reused, so two sites on one upstream cost one fetch.
type HTTPResolver struct {
	endpoints Endpoints
	client    *http.Client

	mu    sync.Mutex
	cache map[string][]byte
}

// NewResolver returns a resolver over the given endpoints with a per-request
// timeout.
func NewResolver(e Endpoints, timeout time.Duration) *HTTPResolver {
	return &HTTPResolver{
		endpoints: e,
		client:    &http.Client{Timeout: timeout},
		cache:     map[string][]byte{},
	}
}

// errNotFound marks a 404 or 410: the proxy's answer for a path that is not a
// module, which the module-root walk steps past.
var errNotFound = errors.New("not found")

// get fetches u. A 404/410 wraps errNotFound; any other non-200, a transport
// failure or a timeout is an error naming the host.
func (r *HTTPResolver) get(ctx context.Context, u string) ([]byte, error) {
	r.mu.Lock()
	body, ok := r.cache[u]
	r.mu.Unlock()
	if ok {
		return body, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build request for %s: %w", u, err)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", u, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return nil, fmt.Errorf("GET %s: %s: %w", u, resp.Status, errNotFound)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("GET %s: %s", u, resp.Status)
	}
	body, err = io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("GET %s: read body: %w", u, err)
	}
	r.mu.Lock()
	r.cache[u] = body
	r.mu.Unlock()
	return body, nil
}

// Releases returns the upstream releases for s, by kind.
func (r *HTTPResolver) Releases(ctx context.Context, s Site) ([]Release, error) {
	switch s.Kind {
	case KindGoInstall:
		return r.moduleReleases(ctx, s.Name, s.Version, true)
	case KindGoreleaserAction:
		mod, err := goreleaserModule(s.Name, s.Version)
		if err != nil {
			return nil, err
		}
		return r.moduleReleases(ctx, mod, s.Version, false)
	case KindGoToolchain:
		return r.toolchainReleases(ctx, s.Version)
	case KindNode:
		return r.nodeReleases(ctx)
	case KindNPM:
		return r.npmReleases(ctx, s.Name)
	}
	return nil, fmt.Errorf("no upstream known for a %s pin", s.Kind)
}

// goreleaserModule maps goreleaser-action's inputs onto the Go module its
// releases are tagged in: v0/v1 under github.com/goreleaser/goreleaser, vN
// under …/vN. goreleaser-pro is closed source and not on the proxy.
func goreleaserModule(distribution, version string) (string, error) {
	if distribution != "goreleaser" {
		return "", fmt.Errorf("goreleaser-action distribution %q has no public release list pincheck can read", distribution)
	}
	v := version
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	switch major := semver.Major(v); major {
	case "":
		return "", fmt.Errorf("goreleaser version %q is not semver", version)
	case "v0", "v1":
		return "github.com/goreleaser/goreleaser", nil
	default:
		return "github.com/goreleaser/goreleaser/" + major, nil
	}
}

// proxyInfo is the proxy's `@v/<version>.info` document.
type proxyInfo struct {
	Version string
	Time    string
}

// moduleReleases reads a module's version list from the proxy — the list, not
// @latest, which can name a lower version than one already pinned. With walk,
// path is a package path and the module root is found by trying each prefix
// from the full path up: the proxy answers 404/410 for a path that is not a
// module (golang.org/x/vuln/cmd/govulncheck lives in golang.org/x/vuln).
//
// Publish times are read for the versions newer than pinned — the only ones
// whose age can change a verdict. Older versions carry a zero time.
func (r *HTTPResolver) moduleReleases(ctx context.Context, path, pinned string, walk bool) ([]Release, error) {
	var (
		mod  string
		list []byte
	)
	for p := path; ; {
		esc, err := module.EscapePath(p)
		if err != nil {
			return nil, fmt.Errorf("module path %q: %w", p, err)
		}
		body, err := r.get(ctx, r.endpoints.GoProxy+"/"+esc+"/@v/list")
		if err == nil && strings.TrimSpace(string(body)) != "" {
			mod, list = p, body
			break
		}
		if err != nil && !errors.Is(err, errNotFound) {
			return nil, err
		}
		i := strings.LastIndex(p, "/")
		if !walk || i < 0 {
			return nil, fmt.Errorf("the Go module proxy lists no module for %s", path)
		}
		p = p[:i]
	}

	pin := comparable(KindGoInstall, pinned)
	var releases []Release
	for _, v := range strings.Fields(string(list)) {
		rel := Release{Version: v}
		if c := comparable(KindGoInstall, v); c != "" && pin != "" && semver.Compare(c, pin) > 0 {
			t, err := r.moduleTime(ctx, mod, v)
			if err != nil {
				return nil, err
			}
			rel.Published = t
		}
		releases = append(releases, rel)
	}
	return releases, nil
}

// moduleTime reads one version's publish time from the proxy.
func (r *HTTPResolver) moduleTime(ctx context.Context, mod, v string) (time.Time, error) {
	escMod, err := module.EscapePath(mod)
	if err != nil {
		return time.Time{}, fmt.Errorf("module path %q: %w", mod, err)
	}
	escV, err := module.EscapeVersion(v)
	if err != nil {
		return time.Time{}, fmt.Errorf("module version %q: %w", v, err)
	}
	body, err := r.get(ctx, r.endpoints.GoProxy+"/"+escMod+"/@v/"+escV+".info")
	if err != nil {
		return time.Time{}, err
	}
	var info proxyInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return time.Time{}, fmt.Errorf("%s@%s .info: %w", mod, v, err)
	}
	t, err := ParseReleaseDate(info.Time)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s@%s .info: %w", mod, v, err)
	}
	return t, nil
}

// toolchainVersion picks a released toolchain name out of a golang.org/toolchain
// module version: v0.0.1-go1.27.1.linux-amd64 → go1.27.1. rc and beta builds do
// not match.
var toolchainVersion = regexp.MustCompile(`^v0\.0\.1-(go\d+\.\d+\.\d+)\.[a-z0-9]+-[a-z0-9]+$`)

// toolchainReleases lists the released Go toolchains. golang.org/toolchain
// carries one module version per toolchain per platform, so each toolchain is
// collapsed to one release, timed from the first platform build listed.
func (r *HTTPResolver) toolchainReleases(ctx context.Context, pinned string) ([]Release, error) {
	body, err := r.get(ctx, r.endpoints.GoProxy+"/golang.org/toolchain/@v/list")
	if err != nil {
		return nil, err
	}
	pin := comparable(KindGoToolchain, pinned)
	seen := map[string]bool{}
	var releases []Release
	for _, mv := range strings.Fields(string(body)) {
		m := toolchainVersion.FindStringSubmatch(mv)
		if m == nil || seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		rel := Release{Version: m[1]}
		if c := comparable(KindGoToolchain, m[1]); pin != "" && semver.Compare(c, pin) > 0 {
			t, err := r.moduleTime(ctx, "golang.org/toolchain", mv)
			if err != nil {
				return nil, err
			}
			rel.Published = t
		}
		releases = append(releases, rel)
	}
	if len(releases) == 0 {
		return nil, errors.New("golang.org/toolchain lists no released toolchain")
	}
	return releases, nil
}

// nodeRelease is one entry of nodejs.org/dist/index.json.
type nodeRelease struct {
	Version string `json:"version"`
	Date    string `json:"date"`
}

// nodeReleases reads nodejs.org's release index. Its dates carry only the
// day, which ParseReleaseDate takes as 00:00 UTC.
func (r *HTTPResolver) nodeReleases(ctx context.Context) ([]Release, error) {
	body, err := r.get(ctx, r.endpoints.NodeDist+"/index.json")
	if err != nil {
		return nil, err
	}
	var index []nodeRelease
	if err := json.Unmarshal(body, &index); err != nil {
		return nil, fmt.Errorf("nodejs index.json: %w", err)
	}
	if len(index) == 0 {
		return nil, errors.New("nodejs index.json lists no releases")
	}
	releases := make([]Release, 0, len(index))
	for _, n := range index {
		t, err := ParseReleaseDate(n.Date)
		if err != nil {
			return nil, fmt.Errorf("nodejs index.json %s: %w", n.Version, err)
		}
		releases = append(releases, Release{Version: strings.TrimPrefix(n.Version, "v"), Published: t})
	}
	return releases, nil
}

// npmPackument is the part of an npm package document pincheck reads: the
// publish time of every version (plus `created`/`modified`, skipped).
type npmPackument struct {
	Time map[string]string `json:"time"`
}

// npmReleases reads a package's versions and publish times from the registry.
func (r *HTTPResolver) npmReleases(ctx context.Context, name string) ([]Release, error) {
	body, err := r.get(ctx, r.endpoints.NPM+"/"+url.PathEscape(name))
	if err != nil {
		return nil, err
	}
	var doc npmPackument
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("npm %s: %w", name, err)
	}
	var releases []Release
	for v, ts := range doc.Time {
		if v == "created" || v == "modified" {
			continue
		}
		t, err := ParseReleaseDate(ts)
		if err != nil {
			return nil, fmt.Errorf("npm %s@%s: %w", name, v, err)
		}
		releases = append(releases, Release{Version: v, Published: t})
	}
	if len(releases) == 0 {
		return nil, fmt.Errorf("npm %s lists no versions", name)
	}
	return releases, nil
}

// Judge fetches s's upstream and classifies it as of now. An unpinned site is
// Unpinned without a fetch; any fetch failure — unreachable host, 404 on every
// candidate, 5xx, timeout, malformed body — is Unknown with the failure as its
// reason, so an upstream that cannot be read never passes as current.
func Judge(ctx context.Context, r Resolver, s Site, now time.Time) Classification {
	if !s.Pinned {
		return Classification{Verdict: Unpinned, Reason: s.Reason}
	}
	releases, err := r.Releases(ctx, s)
	if err != nil {
		return Classification{Verdict: Unknown, Reason: err.Error()}
	}
	if len(releases) == 0 {
		return Classification{Verdict: Unknown, Reason: "upstream lists no releases"}
	}
	return Classify(s.Kind, s.Version, releases, now)
}

// Command pincheck checks every version pin Dependabot cannot reach against its
// upstream, and fails when one is stale.
//
//	go run ./cmd/pincheck
//
// It reads the generic sites internal/pincheck scans — run-block `go install`
// pins in workflows and composite actions, setup-node's version, goreleaser-
// action's version, go.mod's toolchain — plus the pins this repo holds in Go
// source (gremlinsPin, strykerPin), and prints one line per pin. The check is
// strict: a stale pin (a newer release on its line, out longer than the 7-day
// cooldown), an upstream that could not be read, or an unpinned site each fail
// the run. A newer major is a line of information, never a failure.
//
// When $GITHUB_OUTPUT is set it appends `stale=true|false`, so the weekly
// workflow's bump job can run on a red check that was red because something is
// stale — and not on one that was red only because upstream was unreachable.
//
// Exit 0 when every pin is current, 1 on a strict failure, 2 when the tree
// could not be scanned or the output could not be written.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Rivil/dross/internal/pincheck"
)

// upstreamTimeout bounds each request to an upstream. The cron has minutes to
// spare; a hung host must still end as unknown rather than stall the job.
const upstreamTimeout = 30 * time.Second

// sourcePins are the pins this repo holds in Go source. Only dross has them, so
// the cron checks them and doctor, run in other repos, never does.
var sourcePins = []struct {
	file, constName string
	kind            pincheck.Kind
}{
	{"internal/cmd/remote_bootstrap.go", "gremlinsPin", pincheck.KindGoInstall},
	{"internal/mutation/stryker.go", "strykerPin", pincheck.KindNPM},
}

// deps are what a run reads outside the tree: upstream, the clock, the
// environment.
type deps struct {
	resolver pincheck.Resolver
	now      time.Time
	getenv   func(string) string
}

func main() {
	os.Exit(run(".", os.Args[1:], os.Stdout, os.Stderr, deps{
		resolver: pincheck.NewResolver(pincheck.DefaultEndpoints(), upstreamTimeout),
		now:      time.Now(),
		getenv:   os.Getenv,
	}))
}

func run(root string, args []string, stdout, stderr io.Writer, d deps) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "usage: go run ./cmd/pincheck")
		return 2
	}
	sites, err := allSites(root)
	if err != nil {
		fmt.Fprintln(stderr, "pincheck:", err)
		return 2
	}
	rep := pincheck.Check(context.Background(), sites, d.resolver, pincheck.Strict, d.now)
	for _, res := range rep.Results {
		fmt.Fprintln(stdout, res)
	}
	if out := d.getenv("GITHUB_OUTPUT"); out != "" {
		if err := appendOutput(out, fmt.Sprintf("stale=%t\n", rep.Stale())); err != nil {
			fmt.Fprintln(stderr, "pincheck: write $GITHUB_OUTPUT:", err)
			return 2
		}
	}
	if rep.Failed {
		fmt.Fprintf(stdout, "pincheck: %d pin(s) checked — FAIL: a pin is stale, unpinned, or its upstream could not be read\n", len(rep.Results))
		return 1
	}
	fmt.Fprintf(stdout, "pincheck: %d pin(s) checked — all current\n", len(rep.Results))
	return 0
}

// allSites is every pin the cron checks: the generic scan plus the Go-source
// pins. A source pin that cannot be read is an error, not a skipped site — a
// renamed const must not quietly leave the check.
func allSites(root string) ([]pincheck.Site, error) {
	sites, err := pincheck.Scan(root)
	if err != nil {
		return nil, err
	}
	for _, p := range sourcePins {
		v, line, err := pincheck.GoConst(filepath.Join(root, filepath.FromSlash(p.file)), p.constName)
		if err != nil {
			return nil, err
		}
		sites = append(sites, pincheck.SpecSite(p.file, line, p.kind, v))
	}
	return sites, nil
}

// appendOutput appends one line to the GitHub Actions output file.
func appendOutput(path, line string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(f, line); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

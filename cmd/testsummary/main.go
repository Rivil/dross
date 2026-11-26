// Command testsummary reduces a `go test -json` stream on stdin to a plain
// go-test log on stdout, and exits non-zero unless every package passed.
//
// CI pipes every `go test` through it (`set -o pipefail; go test -json … |
// go run ./cmd/testsummary`): the raw -json stream would bury a failure in
// JSON lines, while this prints ok/FAIL per package, the output of failed
// tests only, a failed package's own output (a timeout panic included) and
// build errors. It then writes a timing table — the slowest tests and every
// package's total — to $GITHUB_STEP_SUMMARY (stdout when unset), and a
// `::warning` for each package over the 300s budget. It is stdlib-only by
// decision (timing_tooling) — no gotestsum or other third-party tool in CI.
//
// With $TESTSUMMARY_HISTORY set it also keeps a timing history in that file
// (CI persists it through actions/cache): the package totals table gains each
// package's baseline — the median of its history — and this run's delta; a
// package at or over 90% of go test's 10m cap draws a cap `::warning`, and one
// under it whose history is heading there draws a trend `::warning`. The
// entry for $GITHUB_SHA is left out of both, so a re-run never compares
// against itself, and this run's totals are then saved under $GITHUB_SHA,
// replacing that commit's entry. A missing, empty or unparseable history
// prints one note line and nothing else changes. Unset, it reads and writes
// no history.
package main

import (
	"fmt"
	"io"
	"os"
)

func main() { os.Exit(run(os.Stdin, os.Stdout, os.Getenv)) }

// run is main with its inputs as parameters, so tests drive it directly. The
// timing table, the warnings and the history are reporting only: the exit
// code is the test verdict and nothing else.
func run(stdin io.Reader, stdout io.Writer, getenv func(string) string) int {
	r := newReducer(stdout)
	if err := r.read(stdin); err != nil {
		fmt.Fprintf(stdout, "testsummary: reading the go test stream: %v\n", err)
		return 1
	}
	code := r.finish()

	histPath, sha := getenv("TESTSUMMARY_HISTORY"), getenv("GITHUB_SHA")
	var h history
	if histPath != "" {
		var note string
		if h, note = loadHistory(histPath); note != "" {
			fmt.Fprintln(stdout, note)
		}
	}
	for _, w := range budgetWarnings(r.results) {
		fmt.Fprint(stdout, w)
	}
	for _, w := range capWarnings(r.results, h, sha) {
		fmt.Fprint(stdout, w)
	}
	writeSummary(stdout, getenv, timingTable(r.results, h, sha))

	// A run that recorded no package totals measures nothing: saving it would
	// only push a real entry out of the window.
	if totals := packageTotals(r.results); histPath != "" && len(totals) > 0 {
		h.upsert(sha, totals)
		if err := saveHistory(histPath, h); err != nil {
			fmt.Fprintf(stdout, "::warning title=test timing history::%s\n", escapeData(err.Error()))
		}
	}
	return code
}

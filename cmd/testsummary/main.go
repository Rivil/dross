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
package main

import (
	"fmt"
	"io"
	"os"
)

func main() { os.Exit(run(os.Stdin, os.Stdout, os.Getenv)) }

// run is main with its inputs as parameters, so tests drive it directly. The
// timing table and the budget warnings are reporting only: the exit code is
// the test verdict and nothing else.
func run(stdin io.Reader, stdout io.Writer, getenv func(string) string) int {
	r := newReducer(stdout)
	if err := r.read(stdin); err != nil {
		fmt.Fprintf(stdout, "testsummary: reading the go test stream: %v\n", err)
		return 1
	}
	code := r.finish()
	for _, w := range budgetWarnings(r.results) {
		fmt.Fprint(stdout, w)
	}
	writeSummary(stdout, getenv, timingTable(r.results))
	return code
}

package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// budgetSeconds is the per-package budget (locked decision timeout_ceiling). A
// package over it earns a warning annotation, never a failure: a warning
// cannot flake CI on a slow runner, and go test's own 10m default stays the
// only hard wall.
const budgetSeconds = 300

// slowestRows is how many tests the timing table lists (locked decision
// timing_surface).
const slowestRows = 20

// timingTable renders the step-summary markdown from the reduced results: the
// slowest top-level tests, then every package's total as its own terminal
// event reports it — which counts setup, TestMain and parallel overlap that a
// sum of test rows would not — beside its baseline (the median of h, minus the
// entry for excludeSHA) and this run's delta against it. A package h does not
// hold shows baseline "—" and no delta.
func timingTable(results []result, h history, excludeSHA string) string {
	var tests, pkgs []result
	for _, r := range results {
		switch {
		case r.Test == "":
			pkgs = append(pkgs, r)
		case !strings.Contains(r.Test, "/"):
			tests = append(tests, r)
		}
	}
	slowestFirst := func(rs []result) {
		sort.SliceStable(rs, func(i, j int) bool { return rs[i].Elapsed > rs[j].Elapsed })
	}
	slowestFirst(tests)
	slowestFirst(pkgs)
	if len(tests) > slowestRows {
		tests = tests[:slowestRows]
	}

	var b strings.Builder
	fmt.Fprintf(&b, "### Slowest tests\n\n| Package | Test | Seconds | Result |\n|---|---|---:|---|\n")
	for _, r := range tests {
		fmt.Fprintf(&b, "| %s | %s | %.2f | %s |\n", r.Package, r.Test, r.Elapsed, resultLabel(r.Action))
	}
	fmt.Fprintf(&b, "\n### Package totals\n\n| Package | Seconds | Baseline | Delta | Result |\n|---|---:|---:|---:|---|\n")
	for _, r := range pkgs {
		base, delta := "—", ""
		if m, ok := h.baseline(r.Package, excludeSHA); ok {
			base, delta = fmt.Sprintf("%.2f", m), fmt.Sprintf("%+.2f", r.Elapsed-m)
		}
		fmt.Fprintf(&b, "| %s | %.2f | %s | %s | %s |\n", r.Package, r.Elapsed, base, delta, resultLabel(r.Action))
	}
	return b.String()
}

func resultLabel(action string) string {
	switch action {
	case "fail":
		return "FAIL"
	case "running":
		return "running when the package ended"
	}
	return action
}

// budgetWarnings is one `::warning` workflow command per package over budget.
func budgetWarnings(results []result) []string {
	var out []string
	for _, r := range results {
		if r.Test == "" && r.Elapsed > budgetSeconds {
			out = append(out, fmt.Sprintf("::warning title=test budget::%s took %.1fs, over the %ds per-package budget\n",
				escapeData(r.Package), r.Elapsed, budgetSeconds))
		}
	}
	return out
}

// capWarnings is the cap and trend `::warning`s for this run's packages. A
// package at or over capWarnSeconds (90% of go test's 10m wall) draws a cap
// warning; one under it whose history — minus the entry for excludeSHA, so a
// re-run never trends against itself — is heading for that line draws a trend
// warning instead. Neither ever changes the exit code.
func capWarnings(results []result, h history, excludeSHA string) []string {
	var out []string
	for _, r := range results {
		switch {
		case r.Test != "":
		case r.Elapsed >= capWarnSeconds:
			out = append(out, fmt.Sprintf("::warning title=test cap::%s took %.1fs, at or over 90%% of the %ds cap (go test's default timeout)\n",
				escapeData(r.Package), r.Elapsed, capSeconds))
		case h.trend(r.Package, excludeSHA):
			out = append(out, fmt.Sprintf("::warning title=test trend::%s took %.1fs and its timing history is on course to reach %ds (90%% of the %ds cap) within %d runs\n",
				escapeData(r.Package), r.Elapsed, capWarnSeconds, capSeconds, trendHorizon))
		}
	}
	return out
}

// writeSummary appends table to the file $GITHUB_STEP_SUMMARY names — append,
// since earlier steps may have written there — or prints it to stdout when the
// variable is unset, as on a local run. A summary it cannot write is a warning:
// the exit code stays the test verdict.
func writeSummary(stdout io.Writer, getenv func(string) string, table string) {
	path := getenv("GITHUB_STEP_SUMMARY")
	if path == "" {
		fmt.Fprint(stdout, "\n"+table)
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err == nil {
		_, err = io.WriteString(f, table)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		fmt.Fprintf(stdout, "::warning title=test summary::could not write the timing table to $GITHUB_STEP_SUMMARY: %s\n", escapeData(err.Error()))
	}
}

// escapeData escapes a workflow command's message the way the Actions toolkit
// does, so a stray % or newline cannot end or reshape the command.
func escapeData(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(s)
}

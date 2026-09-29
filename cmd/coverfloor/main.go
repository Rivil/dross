// Command coverfloor fails CI when a non-test Go file under internal/ — other
// than internal/cmd — has less than 50% statement coverage from its own
// package's tests.
//
//	go test -coverprofile=cover.out ./... && go run ./cmd/coverfloor cover.out
//
// The profile must come from a plain per-package run (no -coverpkg), so each
// file's count is what its own package's tests reach: gremlins mutates and
// measures one package at a time, and a file covered only from another
// package is invisible to it. internal/cmd is out of scope because its tests
// are the cobra end-to-end layer, which covers it by design.
//
// The floor is flat: no flag, environment variable or allowlist moves it
// (locked decision guard_shape). Exit 0 when every in-scope file is at or
// above it, 1 naming each file below it, 2 when nothing could be measured —
// bad usage, a missing or malformed profile, or no in-scope file at all, so a
// broken pipeline can never read as a pass.
package main

import "os"

func main() { os.Exit(run(".", os.Args[1:], os.Stdout, os.Stderr)) }

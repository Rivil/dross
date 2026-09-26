package cmd

import (
	"go/ast"
	"path/filepath"
	"strings"
	"testing"
)

// The scanner and codex burn-down's guards. Its zero-findings gate is retired
// into TestNoSpawnOutputEscapes (taint_audit_test.go); what stays is the proof
// that each marker is load-bearing. A finding is a scanner's when any of its
// origins is a spawn in internal/codex, security, quality or techdebt — or in
// internal/gitrun, which every one of them now spawns git through — wherever
// it escapes. The short SHA each scanner reads from git names its run
// directory, so before it was marked its taint reached every path derived from
// a run dir — the escapes land all over the tree, the origin stays with the
// spawn.

var scannerOriginDirs = []string{"internal/codex", "internal/security", "internal/quality", "internal/techdebt", "internal/gitrun"}

// scanRunDirs are the scanners whose run directories carry gitrun.ShortSHA.
var scanRunDirs = []string{"internal/security", "internal/quality", "internal/techdebt"}

// TestScannerRevParseMarkerIsLoadBearing: the scanners' short SHA is read
// through gitrun.ShortSHA over Trim, so the one rev-parse marker that clears it
// is Trim's. Deleting it puts findings back that start at Trim's git spawn and
// escape in each scanner's own package — one shared marker standing in for the
// three the ShortSHA copies used to carry.
func TestScannerRevParseMarkerIsLoadBearing(t *testing.T) {
	var marker *taintMarker
	for _, m := range taintMarkersIn(t, "internal/gitrun") {
		if filepath.Base(m.file) == "gitrun.go" && strings.Contains(m.Reason, "pinned ref invocations") {
			m := m
			marker = &m
		}
	}
	if marker == nil {
		t.Fatal("internal/gitrun/gitrun.go carries no ref-plumbing taint-cleared marker")
	}
	spawn := spawnLineIn(t, gitrunPath, "TrimWith")
	taint, _ := execTaintScan(viewWithoutComment(liveView(t), marker.file, marker.line))
	root := sourceProgram(t).Root
	escapedIn := map[string]bool{}
	for _, f := range taint {
		fromTrim := false
		for _, o := range f.Origins {
			if o.Filename == marker.file && o.Line == spawn {
				fromTrim = true
			}
		}
		if !fromTrim {
			continue
		}
		rel, err := filepath.Rel(root, f.Escape.Filename)
		if err != nil {
			continue
		}
		for _, d := range scanRunDirs {
			if strings.HasPrefix(filepath.ToSlash(rel), d+"/") {
				escapedIn[d] = true
			}
		}
	}
	for _, d := range scanRunDirs {
		if !escapedIn[d] {
			t.Errorf("removing the marker at %s:%d reported no finding from the Trim spawn at line %d escaping in %s",
				filepath.Base(marker.file), marker.line, spawn, d)
		}
	}
}

// spawnLineIn is the line of the exec.Command or exec.CommandContext call
// inside the named function — top-level or a method — of a live package.
func spawnLineIn(t *testing.T, pkgPath, fn string) int {
	t.Helper()
	v := liveView(t)
	for _, p := range v.Pkgs {
		if p.Path != pkgPath {
			continue
		}
		for _, f := range p.Syntax {
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Name.Name != fn || fd.Body == nil {
					continue
				}
				line := 0
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					if call, ok := n.(*ast.CallExpr); ok && line == 0 {
						if obj := execCalleeObject(p.Info, call); obj != nil && obj.Pkg() != nil &&
							obj.Pkg().Path() == "os/exec" && (obj.Name() == "Command" || obj.Name() == "CommandContext") {
							line = v.Fset.Position(call.Pos()).Line
						}
					}
					return true
				})
				if line > 0 {
					return line
				}
			}
		}
	}
	t.Fatalf("no exec.Command in %s.%s", pkgPath, fn)
	return 0
}

// TestEveryScannerMarkerIsLoadBearing: each marker in the scanner and codex
// packages clears a flow a gate would see.
func TestEveryScannerMarkerIsLoadBearing(t *testing.T) {
	var all []taintMarker
	for _, d := range scannerOriginDirs {
		all = append(all, taintMarkersIn(t, d)...)
	}
	if len(all) == 0 {
		t.Fatal("no taint-cleared marker in the scanner packages — the SHA conversions are unmarked")
	}
	for _, m := range all {
		taint, _ := execTaintScan(viewWithoutComment(liveView(t), m.file, m.line))
		if len(taintFindingsFrom(t, taint, scannerOriginDirs...)) == 0 {
			t.Errorf("removing the marker at %s:%d leaves no scanner-origin finding — it clears nothing a gate would see",
				m.file, m.line)
		}
	}
}

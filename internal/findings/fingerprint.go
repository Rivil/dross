// Package findings provides the shared, cross-run findings-lifecycle primitives
// reused by both the security and quality audits: a stable per-finding
// fingerprint, a fingerprint-keyed durable state store, and the post-scan
// reconcile engine. The security and quality packages adapt their per-run
// ledgers into this package's Items; the lifecycle logic lives here exactly
// once rather than mirrored across the two audits.
package findings

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
)

// Fingerprint returns a stable cross-run identity for a finding, derived from
// its class/dimension, normalized file path, and title — deliberately NOT its
// line number, which drifts when unrelated edits shift code. The same issue in
// two runs produces the same fingerprint as long as its class, file, and title
// hold; an edit that only moves it down the file does not change it.
//
// The file path is normalized (filepath.Clean + forward slashes) so that
// "./internal/x.go", "internal/x.go", "internal//x.go", and "internal/x.go/"
// all collapse to one key. Fields are joined with a NUL separator so no
// concatenation of (class, file, title) can be mistaken for a different split.
func Fingerprint(class, file, title string) string {
	key := strings.Join([]string{class, normalizePath(file), title}, "\x00")
	sum := sha256.Sum256([]byte(key))
	// 64-bit hex key: short and hand-editable in state.toml, with ample
	// collision headroom for one repo's finding set.
	return hex.EncodeToString(sum[:8])
}

// normalizePath collapses ./ , // , and trailing-slash forms to one canonical,
// forward-slash path so equivalent spellings fingerprint identically. An empty
// path stays empty — filepath.Clean would turn "" into ".", which we don't want
// polluting the key — so an empty file remains deterministic and distinguishable
// from a real one.
func normalizePath(p string) string {
	if p == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(p))
}

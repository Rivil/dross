package cmd

import (
	"bytes"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Rivil/dross/internal/pathfence"
	"github.com/Rivil/dross/internal/secretscan"
)

// scanDrossArtifacts runs the secret scanner over every .dross artifact that
// could reach a commit, and returns the fingerprints it found.
//
// repoDir is the REPO root — the parent of .dross — never the .dross dir
// itself. The three gates that call this (validate, ship's pre-flight, and
// autoCommitDrossDirt) all hold the repo root already, and pinning one
// signature is what lets a single AST test prove they share one scanner rather
// than three walks that drift apart.
//
// Each hit's Location is the repo-relative, `.dross/`-prefixed slash path, so
// the line a user has to fix reads the same in validate's ✗ line, in ship's
// refusal and in a board comment. A read error is a refusal, never a skip: an
// artifact the scanner could not open is an artifact it cannot vouch for, and
// a gate that shrugged past it would be green over an unknown.
func scanDrossArtifacts(repoDir string) ([]secretscan.Hit, error) {
	paths, err := listDrossArtifacts(repoDir)
	if err != nil {
		return nil, fmt.Errorf("secret scan: %w", err)
	}
	drossRoot := filepath.Join(repoDir, RootDirName)
	var hits []secretscan.Hit
	for _, rel := range paths {
		under := strings.TrimPrefix(rel, RootDirName+"/")
		c, err := pathfence.Contain(drossRoot, "dross-artifact", under)
		if err != nil {
			return nil, fmt.Errorf("secret scan: %w", err)
		}
		// Read whole rather than streamed: pathfence has no Open seam, and
		// reaching os.Open with the contained path is exactly what its
		// residual scan forbids. Scan still walks the bytes line by line
		// with no per-line ceiling, so a one-line minified JSON is fine.
		data, err := pathfence.ReadFile(c)
		if err != nil {
			return nil, fmt.Errorf("secret scan: %w", err)
		}
		found, err := secretscan.Scan(rel, bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		hits = append(hits, found...)
	}
	return hits, nil
}

// listDrossArtifacts enumerates the .dross files the scanner must cover, as
// sorted repo-relative slash paths prefixed `.dross/`.
//
// Inside a git work tree the set is what `git add .dross` would stage: tracked
// files PLUS untracked files that .gitignore does not exclude. That is the
// exact population a commit can carry, so it neither misses the untracked
// note about to be auto-committed nor flags the machine-local run reports
// (security/, quality/, state.json) that .gitignore already keeps out of
// history. Outside git — a fresh `dross init` in a bare directory — there is
// no staging boundary to read, so every regular file under .dross counts.
func listDrossArtifacts(repoDir string) ([]string, error) {
	if gitNoOut(repoDir, "rev-parse", "--is-inside-work-tree") == nil {
		return listDrossArtifactsGit(repoDir)
	}
	return listDrossArtifactsWalk(repoDir)
}

func listDrossArtifactsGit(repoDir string) ([]string, error) {
	out, err := gitRead(repoDir, gitPathArgs("ls-files",
		[]string{"-z", "--cached", "--others", "--exclude-standard"}, RootDirName)...)
	if err != nil {
		return nil, fmt.Errorf("git ls-files %s: %w", RootDirName, err)
	}
	var paths []string
	//dross:taint-cleared ls-files -z prints NUL-separated repo paths, and p is one of them
	for _, p := range strings.Split(out, "\x00") {
		if p == "" {
			continue
		}
		paths = append(paths, filepath.ToSlash(p))
	}
	sort.Strings(paths)
	return paths, nil
}

func listDrossArtifactsWalk(repoDir string) ([]string, error) {
	drossRoot := filepath.Join(repoDir, RootDirName)
	var paths []string
	err := filepath.WalkDir(drossRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(repoDir, p)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", RootDirName, err)
	}
	sort.Strings(paths)
	return paths, nil
}

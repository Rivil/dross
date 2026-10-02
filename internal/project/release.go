package project

import (
	"errors"
	"regexp"
	"strings"
)

// The two patterns scripts/release-version.sh slices project.toml with: the
// [project] header that opens the range, the next table header that closes it,
// and the version line inside it.
var (
	releaseRangeStart = regexp.MustCompile(`^[[:space:]]*\[project\][[:space:]]*$`)
	releaseRangeEnd   = regexp.MustCompile(`^[[:space:]]*\[`)
	releaseVersion    = regexp.MustCompile(`^[[:space:]]*version[[:space:]]*=[[:space:]]*"([^"]*)"`)
)

// ReleaseTag returns the release tag release.yml would cut from a
// project.toml with content src: "v<major>.<minor>.<patch>" from
// [project].version, the dev-only 4th part dropped. It is the same projection
// scripts/release-version.sh makes, line for line rather than through a TOML
// decoder, so it answers "would this change cut a release?" exactly as the
// release workflow will — quirks included.
func ReleaseTag(src []byte) (string, error) {
	version, found := "", false
	inProject := false
	for _, line := range strings.Split(string(src), "\n") {
		if !inProject {
			// sed's /start/,/end/ range: the start line opens it and is
			// never itself checked against the end pattern.
			inProject = releaseRangeStart.MatchString(line)
			continue
		}
		if m := releaseVersion.FindStringSubmatch(line); m != nil && !found {
			version, found = m[1], true
		}
		if releaseRangeEnd.MatchString(line) {
			inProject = false
		}
	}
	if version == "" {
		return "", errors.New("release tag: no [project].version in project.toml")
	}
	parts := strings.SplitN(version, ".", 4)
	if len(parts) > 3 {
		parts = parts[:3]
	}
	return "v" + strings.Join(parts, "."), nil
}

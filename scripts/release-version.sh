#!/bin/sh
# Resolve the release tag from .dross/project.toml's [project].version.
#
# The release-facing version lives in project.toml (locked decision:
# version_home). It is tracked, so a clean CI checkout of main carries it, and
# it is written by the same writer that bumps state.json — one bump, both homes.
# state.json is machine-local and gitignored, so it is not in a CI checkout at
# all; reading the version from there meant reading whatever stale copy happened
# to ride a squash onto main.
#
# No jq and no dross binary: this runs on a bare runner before any toolchain is
# set up. Prints "v<major>.<minor>.<patch>" — the 4th, dev-only .internal part is
# dropped so the tag is valid semver for `dross update`.
set -eu

file=".dross/project.toml"

if [ ! -f "$file" ]; then
	echo "release-version: $file not found" >&2
	exit 1
fi

# Slice the [project] table before looking for the key: a `version` under any
# other table ([stack], [remote], …) is not the project's own version, and an
# unanchored grep would happily return it.
version=$(
	sed -n '/^[[:space:]]*\[project\][[:space:]]*$/,/^[[:space:]]*\[/p' "$file" |
		sed -n 's/^[[:space:]]*version[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' |
		head -n 1
)

if [ -z "$version" ]; then
	echo "release-version: no [project].version in $file" >&2
	exit 1
fi

# 4-part major.minor.patch.internal → the 3-part semver tag.
printf 'v%s\n' "$(printf '%s' "$version" | cut -d. -f1-3)"

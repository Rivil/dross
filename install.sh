#!/bin/sh
# dross installer — bootstrap dross on a fresh machine with no Go toolchain or git
# checkout. Downloads the latest release binary for this platform, verifies its
# SHA-256 against the release checksums.txt, installs it on PATH, then runs
# `dross install` to materialize the slash commands and prompts into ~/.claude.
#
# Usage:  curl -fsSL https://raw.githubusercontent.com/Rivil/dross/main/install.sh | sh
#
# POSIX sh on purpose: it is piped to `sh`, so it must not depend on bash features
# (no `pipefail`). `set -eu` aborts on any error or unset variable; the binary is
# staged in a temp dir and moved onto PATH only AFTER the checksum verifies, so a
# failed/interrupted download never leaves a partial binary on PATH.
#
# Env overrides (used by the smoke test against a local fixture):
#   DROSS_INSTALL_BASE  asset base URL (default: GitHub latest release download)
#   DROSS_API_BASE      GitHub API base (default: https://api.github.com)
#   DROSS_VERSION       skip the API lookup and use this tag (e.g. v0.6.0)
#   DROSS_BIN_DIR       install dir (default: $HOME/.local/bin)
set -eu

REPO="Rivil/dross"
API_BASE="${DROSS_API_BASE:-https://api.github.com}"
INSTALL_BASE="${DROSS_INSTALL_BASE:-https://github.com/${REPO}/releases/latest/download}"
BIN_DIR="${DROSS_BIN_DIR:-${HOME}/.local/bin}"

err() {
	echo "install.sh: $1" >&2
	exit 1
}

detect_os() {
	case "$(uname -s)" in
		Linux) echo linux ;;
		Darwin) echo darwin ;;
		*) err "unsupported OS: $(uname -s) (dross ships darwin and linux)" ;;
	esac
}

detect_arch() {
	case "$(uname -m)" in
		x86_64 | amd64) echo amd64 ;;
		arm64 | aarch64) echo arm64 ;;
		*) err "unsupported architecture: $(uname -m) (dross ships amd64 and arm64)" ;;
	esac
}

# download URL DEST — fetch URL to DEST, failing (non-zero) on any HTTP error so a
# 404 aborts the script before anything reaches PATH.
download() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL "$1" -o "$2"
	elif command -v wget >/dev/null 2>&1; then
		wget -q -O "$2" "$1"
	else
		err "need curl or wget to download dross"
	fi
}

# resolve_tag TMPDIR — print the release tag, honoring DROSS_VERSION or querying the API.
resolve_tag() {
	if [ -n "${DROSS_VERSION:-}" ]; then
		echo "${DROSS_VERSION}"
		return
	fi
	download "${API_BASE}/repos/${REPO}/releases/latest" "$1/release.json"
	tag=$(grep '"tag_name"' "$1/release.json" | head -n1 | sed -E 's/.*"tag_name"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/')
	[ -n "$tag" ] || err "could not determine latest release tag"
	echo "$tag"
}

# verify_checksum TMPDIR ASSET — abort unless ASSET's SHA-256 matches checksums.txt.
verify_checksum() {
	want=$(grep " ${2}\$" "${1}/checksums.txt" | awk '{print $1}')
	[ -n "$want" ] || err "no checksum entry for ${2}"
	if command -v sha256sum >/dev/null 2>&1; then
		got=$(sha256sum "${1}/${2}" | awk '{print $1}')
	elif command -v shasum >/dev/null 2>&1; then
		got=$(shasum -a 256 "${1}/${2}" | awk '{print $1}')
	else
		err "need sha256sum or shasum to verify the download"
	fi
	[ "$want" = "$got" ] || err "checksum mismatch for ${2} (refusing to install)"
}

main() {
	os=$(detect_os)
	arch=$(detect_arch)

	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' EXIT INT TERM

	tag=$(resolve_tag "$tmp")
	version="${tag#v}"
	asset="dross_${version}_${os}_${arch}.tar.gz"

	echo "Downloading ${asset} (${tag})..."
	download "${INSTALL_BASE}/${asset}" "${tmp}/${asset}"
	download "${INSTALL_BASE}/checksums.txt" "${tmp}/checksums.txt"

	verify_checksum "$tmp" "$asset"

	tar -xzf "${tmp}/${asset}" -C "$tmp"
	[ -f "${tmp}/dross" ] || err "release archive did not contain a dross binary"

	mkdir -p "$BIN_DIR"
	# Move onto PATH only now — after a verified download and successful extraction.
	mv "${tmp}/dross" "${BIN_DIR}/dross"
	chmod +x "${BIN_DIR}/dross"
	echo "Installed dross ${tag} → ${BIN_DIR}/dross"

	# Materialize slash commands + prompts into ~/.claude (copy mode off a checkout-less box).
	"${BIN_DIR}/dross" install

	echo ""
	echo "Done. If ${BIN_DIR} is not on your PATH, add it:"
	echo "  export PATH=\"${BIN_DIR}:\$PATH\""
}

main "$@"

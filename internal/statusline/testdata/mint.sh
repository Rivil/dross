#!/bin/sh
# mint.sh — regenerate the *.golden byte-fidelity fixtures from the REFERENCE node
# statusline (~/.claude/hooks/statusline.js), the single source of truth for the
# native port. Run ONCE at development time; the Go tests read only the committed
# *.golden bytes and NEVER invoke node.
#
# Pinned sandbox (so node's FS/env/clock-derived output is reproducible):
#   - CLAUDE_CONFIG_DIR -> a fresh empty temp dir => no todos, no jobs, so line 2
#     is the context meter only and there is never a line 3.
#   - workspace.current_dir (carried in the stdin JSON) -> /work/myproject, a path
#     that does not exist => git branch "" and no .dross/state.json up-walk match.
#     The *_branch case instead points current_dir at a throwaway `git init -b main`
#     repo named "myproject" => branch "main", still no .dross above it.
#   - CLAUDE_CODE_AUTO_COMPACT_WINDOW pinned per meter case (empty => JS default
#     ~16.5% buffer). total_tokens carried in the stdin JSON.
#   - CLAUDE_JOB_DIR unset.
#
# Usage:  sh mint.sh [/abs/path/to/statusline.js]
set -eu

JS="${1:-$HOME/.claude/hooks/statusline.js}"
DIR="$(cd "$(dirname "$0")" && pwd)"
SBX="$(mktemp -d)"
CFG="$SBX/cfg"; mkdir -p "$CFG"            # empty config dir: no todos / no jobs
REPO="$SBX/myproject"; git init -q -b main "$REPO" >/dev/null 2>&1
trap 'rm -rf "$SBX"' EXIT

# mint <name> <acw> <stdin-json>
mint() {
	name="$1"; acw="$2"; json="$3"
	printf '%s' "$json" | \
		env -u CLAUDE_JOB_DIR CLAUDE_CONFIG_DIR="$CFG" CLAUDE_CODE_AUTO_COMPACT_WINDOW="$acw" \
		node "$JS" > "$DIR/$name.golden"
	printf '  %-22s -> %s\n' "$name" "$(cat "$DIR/$name.golden" | tail -1)"
}

NOPROJ='"workspace":{"current_dir":"/work/myproject"}'
REPOJSON="\"workspace\":{\"current_dir\":\"$REPO\"}"
M='"model":{"display_name":"Claude"}'

# --- line 1 only (no context_window => no meter) ---
mint line1_plain  "" "{$M,$NOPROJ,\"session_id\":\"\"}"
mint line1_branch "" "{$M,$REPOJSON,\"session_id\":\"\"}"

# --- context meter, auto-compact window pinned to 200000 / total 1_000_000 =>
#     a clean 20% buffer, so remaining maps to the six band-edge used% values. ---
CW='"context_window":{"total_tokens":1000000,"remaining_percentage":'
mint meter_u49 200000 "{$M,$NOPROJ,$CW 60.8}}"
mint meter_u50 200000 "{$M,$NOPROJ,$CW 60.0}}"
mint meter_u64 200000 "{$M,$NOPROJ,$CW 48.8}}"
mint meter_u65 200000 "{$M,$NOPROJ,$CW 48.0}}"
mint meter_u79 200000 "{$M,$NOPROJ,$CW 36.8}}"
mint meter_u80 200000 "{$M,$NOPROJ,$CW 36.0}}"

# --- auto-compact normalization variants ---
# default ~16.5% buffer (acw unset):
mint meter_default 0 "{$M,$NOPROJ,$CW 60.0}}"
# total_tokens override changes the buffer (200000/500000 => 40%):
mint meter_total_override 200000 "{$M,$NOPROJ,\"context_window\":{\"total_tokens\":500000,\"remaining_percentage\":60.0}}"

echo "minted into $DIR"

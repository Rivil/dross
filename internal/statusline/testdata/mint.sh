#!/bin/sh
# mint.sh — regenerate the *.golden byte-fidelity fixtures from the REFERENCE node
# statusline (~/.claude/hooks/statusline.js), the single source of truth for the
# native port. Run ONCE at development time; the Go tests read only the committed
# *.golden bytes and NEVER invoke node.
#
# Pinned sandbox (so node's FS/env/clock-derived output is reproducible):
#   - CLAUDE_CONFIG_DIR -> a fresh temp dir. $CFG is empty (no todos/jobs => line 2
#     is the meter/state only, never a line 3). $CFGT additionally stages
#     todos/<session>-agent-1.json with one in_progress todo.
#   - workspace.current_dir (carried in the stdin JSON) -> /work/myproject, a path
#     that does not exist => git branch "" and no .dross up-walk; the *_branch case
#     uses a `git init -b main` repo named "myproject" => branch "main"; the state
#     cases point current_dir at $PROJ/$PROJP which carry a fixture .dross/state.json.
#   - CLAUDE_CODE_AUTO_COMPACT_WINDOW pinned per meter case (empty => JS default
#     ~16.5% buffer). total_tokens carried in the stdin JSON.
#   - CLAUDE_JOB_DIR unset. session_id "sess123" only for todo cases.
#
# Usage:  sh mint.sh [/abs/path/to/statusline.js]
set -eu

JS="${1:-$HOME/.claude/hooks/statusline.js}"
DIR="$(cd "$(dirname "$0")" && pwd)"
SBX="$(mktemp -d)"
trap 'rm -rf "$SBX"' EXIT

CFG="$SBX/cfg"; mkdir -p "$CFG"                 # empty config: no todos / no jobs
CFGT="$SBX/cfgt"; mkdir -p "$CFGT/todos"        # config with one in_progress todo
printf '%s' '[{"content":"x","status":"in_progress","activeForm":"Doing the thing"}]' \
	> "$CFGT/todos/sess123-agent-1.json"
REPO="$SBX/myproject"; git init -q -b main "$REPO" >/dev/null 2>&1

# Fixture project dirs with a .dross/state.json reachable via current_dir up-walk.
PROJ="$SBX/proj"; mkdir -p "$PROJ/.dross"
printf '%s' '{"current_milestone":"v0.8","current_phase":"native-statusline","current_phase_status":"planned"}' \
	> "$PROJ/.dross/state.json"
PROJP="$SBX/projpartial"; mkdir -p "$PROJP/.dross"   # missing current_phase_status
printf '%s' '{"current_milestone":"v0.8","current_phase":"native-statusline"}' \
	> "$PROJP/.dross/state.json"

# mint <name> <cfgdir> <acw> <stdin-json>
mint() {
	name="$1"; cfg="$2"; acw="$3"; json="$4"
	printf '%s' "$json" | \
		env -u CLAUDE_JOB_DIR CLAUDE_CONFIG_DIR="$cfg" CLAUDE_CODE_AUTO_COMPACT_WINDOW="$acw" \
		node "$JS" > "$DIR/$name.golden"
	printf '  %-22s -> %s\n' "$name" "$(tail -1 "$DIR/$name.golden")"
}

M='"model":{"display_name":"Claude"}'
NOPROJ='"workspace":{"current_dir":"/work/myproject"}'
REPOJSON="\"workspace\":{\"current_dir\":\"$REPO\"}"
PROJJSON="\"workspace\":{\"current_dir\":\"$PROJ\"}"
PROJPJSON="\"workspace\":{\"current_dir\":\"$PROJP\"}"
CW='"context_window":{"total_tokens":1000000,"remaining_percentage":'

# --- t-1: line 1 only (no context_window => no meter) ---
mint line1_plain  "$CFG" "" "{$M,$NOPROJ,\"session_id\":\"\"}"
mint line1_branch "$CFG" "" "{$M,$REPOJSON,\"session_id\":\"\"}"

# --- t-1: context meter band edges (acw 200000 / total 1_000_000 => 20% buffer) ---
mint meter_u49 "$CFG" 200000 "{$M,$NOPROJ,$CW 60.8}}"
mint meter_u50 "$CFG" 200000 "{$M,$NOPROJ,$CW 60.0}}"
mint meter_u64 "$CFG" 200000 "{$M,$NOPROJ,$CW 48.8}}"
mint meter_u65 "$CFG" 200000 "{$M,$NOPROJ,$CW 48.0}}"
mint meter_u79 "$CFG" 200000 "{$M,$NOPROJ,$CW 36.8}}"
mint meter_u80 "$CFG" 200000 "{$M,$NOPROJ,$CW 36.0}}"

# --- t-1: auto-compact normalization variants ---
mint meter_default "$CFG" 0 "{$M,$NOPROJ,$CW 60.0}}"
mint meter_total_override "$CFG" 200000 "{$M,$NOPROJ,\"context_window\":{\"total_tokens\":500000,\"remaining_percentage\":60.0}}"

# --- t-2: line 2 todo / dross state composition ---
# todo present (bold), no meter:
mint todo_only "$CFGT" "" "{$M,$NOPROJ,\"session_id\":\"sess123\"}"
# todo present + meter (leading space separates body and bar):
mint todo_with_meter "$CFGT" 200000 "{$M,$NOPROJ,\"session_id\":\"sess123\",$CW 60.0}}"
# no todo => dross state (dim), no meter:
mint state_only "$CFG" "" "{$M,$PROJJSON,\"session_id\":\"\"}"
# dross state + meter:
mint state_with_meter "$CFG" 200000 "{$M,$PROJJSON,\"session_id\":\"\",$CW 60.0}}"
# dross state with a missing field => graceful, no trailing separator:
mint state_missing_field "$CFG" "" "{$M,$PROJPJSON,\"session_id\":\"\"}"
# todo AND dross state present => todo wins:
mint todo_wins "$CFGT" "" "{$M,$PROJJSON,\"session_id\":\"sess123\"}"

echo "minted into $DIR"

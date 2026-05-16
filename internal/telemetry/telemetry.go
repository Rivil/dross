// Package telemetry records local usage events to a JSONL file at
// ~/.claude/dross/telemetry.jsonl. The intent is single-developer
// self-observation: dogfooders learn where they hit friction by
// reading their own event log.
//
// Privacy posture:
//   - Local-only. No network. No daemon. No third party.
//   - Logs shapes and counts, never user-typed strings (no criterion
//     text, no commit messages, no file paths beyond a stable hash).
//   - Default ON, opt-out via DROSS_NO_TELEMETRY=1 or
//     `dross stats opt-out`. Users are asked at init/onboard time so
//     consent is explicit.
//
// Event ordering is append-only. Files rotate at MaxBytes by renaming
// to telemetry.jsonl.<unix> — readers must merge across rotations.
package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// File is the basename written under ~/.claude/dross/.
const File = "telemetry.jsonl"

// MaxBytes triggers rotation. Set deliberately small so a busy day of
// dogfooding doesn't grow the file unboundedly.
const MaxBytes = 10 * 1024 * 1024

// SchemaVersion lets future readers detect format changes. Bump when
// fields are renamed or removed; new optional fields don't require a
// bump.
const SchemaVersion = 1

// OptOutEnv disables telemetry recording when set to a non-empty value.
// Honored independently of the on-disk config so users who set it in
// their shell rc never produce events even on a fresh install.
const OptOutEnv = "DROSS_NO_TELEMETRY"

// Event is one line in the JSONL log. Keep fields stable — readers
// (including future-me with `dross stats`) parse this directly.
type Event struct {
	Schema     int               `json:"schema"`
	Timestamp  time.Time         `json:"ts"`
	Kind       string            `json:"kind"`              // "cli" | "outcome"
	Command    string            `json:"cmd,omitempty"`     // resolved cobra path, e.g. "milestone create"
	Args       []string          `json:"args,omitempty"`    // sanitized: keys only for flag values that might leak content
	DurationMS int64             `json:"dur_ms,omitempty"`  // CLI invocation duration
	ExitCode   int               `json:"exit,omitempty"`    // 0 for success
	ErrorClass string            `json:"err,omitempty"`     // bucketed error type, never the message
	RepoHash   string            `json:"repo,omitempty"`    // sha256 of repo root path, first 12 chars
	Phase      string            `json:"phase,omitempty"`   // phase id when relevant
	Counts     map[string]int    `json:"counts,omitempty"`  // size/shape data: criteria=4, tasks=12
	Numbers    map[string]float64 `json:"nums,omitempty"`   // floats: mutation_score=0.83
	Tags       map[string]string `json:"tags,omitempty"`    // small enums: verdict=pass, provider=github
}

// Append writes one event to the configured path. Rotates if the file
// exceeds MaxBytes. Honors OptOutEnv; if disabled, returns nil silently.
//
// Append never fails the calling command — telemetry should not break
// the user's workflow. Errors are returned for tests but callers
// typically ignore them.
func Append(path string, ev Event) error {
	if os.Getenv(OptOutEnv) != "" {
		return nil
	}
	if ev.Schema == 0 {
		ev.Schema = SchemaVersion
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir telemetry dir: %w", err)
	}
	if err := maybeRotate(path); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open telemetry: %w", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(&ev); err != nil {
		return fmt.Errorf("encode event: %w", err)
	}
	return nil
}

// Load returns every event in path plus all rotated siblings, in
// timestamp order. Used by `dross stats`.
func Load(path string) ([]Event, error) {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var paths []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if name == base || strings.HasPrefix(name, base+".") {
			paths = append(paths, filepath.Join(dir, name))
		}
	}
	var out []Event
	for _, p := range paths {
		evs, err := readFile(p)
		if err != nil {
			return nil, err
		}
		out = append(out, evs...)
	}
	// Stable sort by timestamp — events arrive in append order per file
	// but rotated files might interleave near the boundary.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].Timestamp.After(out[j].Timestamp); j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out, nil
}

func readFile(path string) ([]Event, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []Event
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			// Tolerate corrupt lines — telemetry should never fail
			// loud enough to lose the rest of the history.
			continue
		}
		out = append(out, ev)
	}
	return out, nil
}

func maybeRotate(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if info.Size() < MaxBytes {
		return nil
	}
	dest := fmt.Sprintf("%s.%d", path, time.Now().Unix())
	return os.Rename(path, dest)
}

// HashRepo returns a stable 12-char identifier for a repo path. Used
// to group events per project without leaking the path itself. Same
// repo on the same machine always hashes the same.
func HashRepo(repoRoot string) string {
	if repoRoot == "" {
		return ""
	}
	abs, err := filepath.Abs(repoRoot)
	if err != nil {
		abs = repoRoot
	}
	sum := sha256.Sum256([]byte(abs))
	return hex.EncodeToString(sum[:])[:12]
}

// ClassifyError buckets errors into a small set of strings. Never
// returns the raw message — that might contain user paths or content.
//
// Order matters: specific buckets (verify state, mutation adapters,
// phase/plan/spec state) are checked before generic ones (invalid,
// missing) so a "no current_phase" error doesn't end up in
// "missing".
func ClassifyError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	switch {
	// Root / scaffold state.
	case strings.Contains(msg, "no .dross"):
		return "no_root"

	// Phase / plan / spec state — the user is somewhere the workflow
	// can't pick up. Distinct from generic "missing" because the fix
	// is a specific dross command, not a file path.
	case strings.Contains(msg, "no current_phase"),
		strings.Contains(msg, "phaseid is required"),
		strings.Contains(msg, "no phase id given"):
		return "no_phase"
	case strings.Contains(msg, "load spec"),
		strings.Contains(msg, "read spec"),
		strings.Contains(msg, "decode spec"):
		return "no_spec"
	case strings.Contains(msg, "decode plan"),
		strings.Contains(msg, "load plan"),
		strings.Contains(msg, "read plan"):
		return "no_plan"

	// Verify / mutation pipeline — these errors actively hide what's
	// wrong when bucketed as "other".
	case strings.Contains(msg, "verify.toml"),
		strings.Contains(msg, "load verify"),
		strings.Contains(msg, "verify verdict"):
		return "verify_state"
	case strings.Contains(msg, "stryker"),
		strings.Contains(msg, "gremlins"),
		strings.Contains(msg, "mutation adapter"),
		strings.Contains(msg, "ast-grep"):
		return "mutation"

	// Provider / remote.
	case strings.Contains(msg, "github backend"),
		strings.Contains(msg, "forgejo backend"),
		strings.Contains(msg, "unsupported provider"),
		strings.Contains(msg, "no [remote]"),
		strings.Contains(msg, "[remote].url"):
		return "provider"

	// CLI surface: arg validation, unknown fields, user-facing config.
	case strings.Contains(msg, "unknown field"),
		strings.Contains(msg, "unknown or unsettable"),
		strings.Contains(msg, "unknown scope"),
		strings.Contains(msg, "unsupported segment"):
		return "unknown_field"
	case strings.Contains(msg, "is required"),
		strings.Contains(msg, "must be set"),
		strings.Contains(msg, "must be non-empty"),
		strings.Contains(msg, "is empty"):
		return "cli_args"

	// User cancelled mid-flow.
	case strings.Contains(msg, "aborted:"),
		strings.Contains(msg, "cancelled"),
		strings.Contains(msg, "canceled"):
		return "cancelled"

	// Generic buckets — kept for safety-net coverage.
	case strings.Contains(msg, "already exists"):
		return "already_exists"
	case strings.Contains(msg, "validate"), strings.Contains(msg, "invalid"):
		return "invalid"
	case strings.Contains(msg, "not found"), strings.Contains(msg, "missing"):
		return "missing"
	case strings.Contains(msg, "permission"), strings.Contains(msg, "denied"):
		return "permission"
	case strings.Contains(msg, "git "):
		return "git"
	case strings.Contains(msg, "http"), strings.Contains(msg, "network"):
		return "network"
	default:
		return "other"
	}
}

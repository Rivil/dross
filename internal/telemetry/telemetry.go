// Package telemetry records local usage events to a JSONL file at
// ~/.claude/dross/telemetry.jsonl. The intent is single-developer
// self-observation: dogfooders learn where they hit friction by
// reading their own event log.
//
// Privacy posture:
//   - Local-only. No network. No daemon. No third party.
//   - Logs shapes and counts, never user-typed strings (no criterion
//     text, no commit messages, no file paths beyond a stable hash).
//   - A narrow exception: a small allowlist of buckets also carry a
//     redacted, length-capped copy of the message in err_detail. "other" is
//     the unclassified tail (countable but otherwise opaque); the identifier-only
//     buckets — unknown_subcommand, unknown_field, unknown_flag, arg_count and
//     landmark_parse — carry only the rejected CLI identifier (a bad subcommand,
//     field path, flag, arg count or landmark pair the user typed) — not content
//     like criterion text or commit messages — which is the diagnostic needed to
//     see WHAT was typed wrong. The home directory is collapsed to "~" so absolute
//     paths don't leak, and the log stays local-only. Every other classified
//     error stores no text. See CarriesDetail.
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
	Schema      int                `json:"schema"`
	Timestamp   time.Time          `json:"ts"`
	Kind        string             `json:"kind"`                 // "cli" | "outcome"
	Command     string             `json:"cmd,omitempty"`        // resolved cobra path, e.g. "milestone create"
	Args        []string           `json:"args,omitempty"`       // sanitized: keys only for flag values that might leak content
	DurationMS  int64              `json:"dur_ms,omitempty"`     // CLI invocation duration
	ExitCode    int                `json:"exit,omitempty"`       // 0 for success
	ErrorClass  string             `json:"err,omitempty"`        // bucketed error type, never the message
	ErrorDetail string             `json:"err_detail,omitempty"` // redacted message, only for the CarriesDetail allowlist — the "other" tail plus the identifier-only buckets
	RepoHash    string             `json:"repo,omitempty"`       // sha256 of repo root path, first 12 chars
	Phase       string             `json:"phase,omitempty"`      // phase id when relevant
	Counts      map[string]int     `json:"counts,omitempty"`     // size/shape data: criteria=4, tasks=12
	Numbers     map[string]float64 `json:"nums,omitempty"`       // floats: mutation_score=0.83
	Tags        map[string]string  `json:"tags,omitempty"`       // small enums: verdict=pass, provider=github
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
// Order matters: classification walks the classRules table
// first-match-wins, arranged in tiers — root/scaffold state, then
// workflow-specific buckets (verify state, mutation adapters,
// phase/plan/spec state, provider/board), then the CLI surface, then
// the generic safety-net buckets (where the more diagnostic bucket
// wins: already_exists, permission, git, network, then
// invalid/missing), with "other" as the fallback — so a "no
// current_phase" error doesn't end up in "missing". The tier order is
// enforced by TestNoTokenShadowing and documented in the README's
// error-bucket section, kept in sync by TestReadmeDocumentsBucketTiers.
func ClassifyError(err error) string {
	if err == nil {
		return ""
	}
	return ClassifyMessage(err.Error())
}

// ClassifyMessage is ClassifyError over a bare message string. It exists so
// stored err_detail strings can be re-run through the classifier at read time
// (see Reclassify) without reconstructing an error value. An empty message
// classifies as "" — the same as a nil error — rather than manufacturing a
// phantom "other".
func ClassifyMessage(message string) string {
	if message == "" {
		return ""
	}
	msg := strings.ToLower(message)
	for _, r := range classRules {
		for _, tokens := range r.matchers {
			all := true
			for _, tok := range tokens {
				if !strings.Contains(msg, tok) {
					all = false
					break
				}
			}
			if all {
				return r.bucket
			}
		}
	}
	return "other"
}

// classRule is one row of the ordered taxonomy table. A rule fires when
// ANY of its matchers fires; a matcher fires when ALL of its tokens are
// present in the (lowercased) message. Most matchers are single-token —
// the multi-token form exists for compound shapes like
// {"carries no", "record"}.
type classRule struct {
	bucket   string
	matchers [][]string
}

// classRules is the taxonomy. ORDER IS THE CONTRACT: rules are walked
// top to bottom and the first match wins, so the table is arranged in
// tiers — root/scaffold state, then workflow-specific buckets, then the
// CLI surface, then the generic safety-net buckets, with "other" as the
// implicit fallback when nothing matches. A more-specific bucket must
// sit above any generic bucket whose token could also match its
// messages; TestNoTokenShadowing enforces that an earlier token never
// makes a later rule unreachable.
var classRules = []classRule{
	// Root / scaffold state. incomplete_root sits above no_root: a `.dross/`
	// that exists but is missing a required file is a different kind of
	// friction from never having run init — the fix is `dross onboard` on a
	// directory the user thinks is already set up, and folding it into
	// no_root would hide that.
	{"incomplete_root", [][]string{{"not a dross repo"}}},
	{"no_root", [][]string{{"no .dross"}}},

	// Phase / plan / spec state — the user is somewhere the workflow
	// can't pick up. Distinct from generic "missing" because the fix
	// is a specific dross command, not a file path.
	{"no_phase", [][]string{{"no current_phase"}, {"phaseid is required"}, {"no phase id given"}}},
	{"no_spec", [][]string{{"load spec"}, {"read spec"}, {"decode spec"}}},
	{"no_plan", [][]string{{"decode plan"}, {"load plan"}, {"read plan"}}},
	{"no_milestone", [][]string{{"no current_milestone"}, {"load milestone"}}},

	// Phase-complete pre-flight failures — these used to land in "other"
	// even though they're user-actionable: dirty tree, or the upstream
	// merge hasn't actually landed yet so the ff-only refuses to advance.
	// Bucketing them separately makes the friction visible in stats.
	{"dirty_tree", [][]string{{"working tree is dirty"}}},
	// The ff-only shapes above are joined by the explicit refusals: phase
	// complete and milestone complete both refuse to advance while the PR is
	// still open, and `dross status` reports the shipped-but-unmerged window
	// where machine-local state records `shipped <id>` (written by ship) but
	// the PR hasn't landed on the base yet. All four are the same friction —
	// waiting on an upstream merge — so they share a bucket. Detail-free: the
	// messages embed phase ids and branch names (locked: detail_allowlist).
	{"merge_pending", [][]string{
		{"hasn't advanced past"},
		{"has the pr actually merged"},
		{"fast-forward of"},
		{"ff-only"},
		{"is not merged upstream"},
		{"is not merged into"},
		{"cannot confirm"},
		{"carries no", "record"},
	}},
	// Phase start refused because we're not on the main branch — usually
	// still on a previous phase/<id> branch. User-actionable (switch back
	// or pass --no-branch), not a tool failure. Used to land in "other".
	{"wrong_branch", [][]string{{"to start a phase"}}},

	// Verify / mutation pipeline — these errors actively hide what's
	// wrong when bucketed as "other".
	{"verify_state", [][]string{{"verify.toml"}, {"load verify"}, {"verify verdict"}}},
	{"mutation", [][]string{{"stryker"}, {"gremlins"}, {"mutation adapter"}, {"ast-grep"}}},

	// Provider / remote.
	{"provider", [][]string{
		{"github backend"},
		{"forgejo backend"},
		{"gitlab backend"},
		{"unsupported provider"},
		{"no [remote]"},
		{"[remote].url"},
	}},

	// Issue-board sync (Forgejo board mirroring). Operational failures from
	// the forge client are wrapped "board:" so a flaky board never hides
	// inside the generic network/other buckets. Placed after provider so
	// config errors (missing api_base etc.) still read as provider.
	{"board", [][]string{{"board:"}, {"issue-board"}}},

	// A required auth token is missing from the shell. The fix is `dross env
	// set <VAR>`, not a config or CLI change, so it gets its own bucket.
	// Placed below "board:" so a board op that fails for want of a token
	// still reads as a board failure. Detail-free: the message names the
	// env var (locked: detail_allowlist).
	{"env_token", [][]string{{"is not set"}}},

	// CLI surface: arg validation, unknown fields, user-facing config.
	//
	// The cobra-generated shapes below (unknown command / unknown flag / arg
	// counts) were the biggest contributors to the "other" tail. Each has a
	// different fix, so each earns its own line in stats — except cobra's
	// "unknown command" wording for a bad top-level verb, which routes into
	// the existing unknown_subcommand rather than a near-duplicate bucket.
	{"unknown_subcommand", [][]string{{"unknown subcommand"}, {"unknown command"}}},
	// Cobra flag-parse failures: an unrecognised long flag, an unrecognised
	// shorthand, or a flag given without its value.
	{"unknown_flag", [][]string{{"unknown flag"}, {"unknown shorthand flag"}, {"flag needs an argument"}}},
	// Cobra arity failures. Every wording cobra emits — "accepts N arg(s)",
	// "requires at least N arg(s)", "accepts between N and M arg(s)" — shares
	// the "arg(s)" token, so match that rather than enumerating each phrasing.
	{"arg_count", [][]string{{"arg(s)"}}},
	// `dross changes record --landmark` parse failures. Placed above the
	// unknown-field group so an "unknown landmark key" reads as a malformed
	// landmark rather than a generic unknown field.
	{"landmark_parse", [][]string{{"landmark pair"}, {"unknown landmark key"}}},
	{"unknown_field", [][]string{
		{"unknown field"},
		{"unknown milestone field"},
		{"unknown or unsettable"},
		{"unknown scope"},
		{"unsupported segment"},
		{"not a list field"},
	}},
	{"cli_args", [][]string{{"is required"}, {"must be set"}, {"must be non-empty"}, {"is empty"}}},

	// User cancelled mid-flow.
	{"cancelled", [][]string{{"aborted:"}, {"cancelled"}, {"canceled"}}},

	// Health-check commands (doctor) return errors to gate CI / exit
	// non-zero, but "N issue(s) found" is a useful outcome, not a tool
	// failure. Bucketing distinguishes the two so a noisy doctor doesn't
	// look like a broken doctor in stats.
	{"check_issues", [][]string{{"issue(s) found"}, {"issues found"}, {"problem(s) found"}, {"problems found"}}},

	// dross state.json read/write failures — the workflow can't persist
	// progress. Distinct from a generic file error because the fix is
	// about the state file specifically. Used to land in "other".
	{"state_io", [][]string{{"save state"}, {"marshal state"}, {"unmarshal state"}, {"load state"}}},

	// The .dross config itself is absent or unreadable: no project.toml, no
	// state.json, or a TOML syntax error in either. Distinct from state_io
	// (which is dross failing to persist its own progress) — here the config
	// never loaded, so the workflow can't start at all. Placed AFTER state_io
	// so a state-write failure keeps its more specific bucket, and after the
	// phase/plan/spec/verify/milestone group so their own `.toml` load errors
	// keep theirs. Detail-free: the messages embed absolute config paths
	// (locked: detail_allowlist).
	{"config_io", [][]string{{"project.toml"}, {"state.json"}, {"toml:"}}},

	// Generic buckets — kept for safety-net coverage. Within the tier the
	// more diagnostic bucket wins: already_exists (near-exact phrase) and
	// permission first, then the transport/tool buckets (git, network), and
	// only then the vague invalid/missing pair — an "http 404: pr not found"
	// is a network failure that happens to mention absence, not a missing
	// file, so network must see it before "not found" does.
	{"already_exists", [][]string{{"already exists"}}},
	{"permission", [][]string{{"permission"}, {"denied"}}},
	{"git", [][]string{{"git "}}},
	{"network", [][]string{{"http"}, {"network"}}},
	{"invalid", [][]string{{"validate"}, {"invalid"}}},
	{"missing", [][]string{{"not found"}, {"missing"}}},
}

// detailBuckets are the error classes that additionally store a redacted
// err_detail. "other" is the unclassified tail — undiagnosable without it.
// The rest carry only a CLI identifier the user typed (a rejected subcommand,
// field path, flag, arg count or landmark pair), not user content, which is
// exactly what graduates them from "agent typed something wrong" into "agent
// typed THIS wrong". Keep this list tight: only add a bucket whose message is
// known to be a bounded identifier, never free-form user text — buckets whose
// messages embed phase ids, config paths or token names stay detail-free.
var detailBuckets = map[string]bool{
	"other":              true,
	"unknown_subcommand": true,
	"unknown_field":      true,
	"arg_count":          true,
	"unknown_flag":       true,
	"landmark_parse":     true,
}

// CarriesDetail reports whether an error class should store a redacted
// err_detail alongside the bucket. The caller pairs this with Detail.
func CarriesDetail(class string) bool {
	return detailBuckets[class]
}

// Reclassify returns the effective error class of a stored event: the class it
// was recorded with, unless that class is the catch-all "other" and the event
// kept a detail, in which case the detail is re-run through the current
// classifier.
//
// This is what lets a newly added bucket apply retroactively. Without it a
// graduation only affects future events and the historical "other" count stays
// fat, hiding the very shapes that were just solved. Readers call this; the log
// itself is append-only and is never rewritten.
//
// Gating on the stored class (not merely on the presence of a detail) is
// deliberate: an already-classified bucket keeps its recorded class even when
// it carries a detail, so a re-run can never move an event out of a named
// bucket — only out of "other".
func Reclassify(ev Event) string {
	if ev.ErrorClass != "other" || ev.ErrorDetail == "" {
		return ev.ErrorClass
	}
	return ClassifyMessage(ev.ErrorDetail)
}

// maxDetailLen caps the err_detail string. Error messages are short; the
// cap guards against a pathological wrapped error blowing up the log.
const maxDetailLen = 240

// Detail returns a redacted, length-capped form of an error message,
// intended for the buckets CarriesDetail allows — the "other" tail plus the
// identifier-only buckets, whose messages are bounded CLI identifiers.
// Callers must gate on CarriesDetail; attaching a detail to any other
// classified bucket would leak text for no diagnostic gain.
//
// Redaction: the user's home directory is collapsed to "~" so absolute
// paths don't leak. This is a deliberate, narrow exception to the
// "never the message" rule — the log is local-only, single-developer.
func Detail(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.TrimSpace(err.Error())
	if home, herr := os.UserHomeDir(); herr == nil && home != "" {
		msg = strings.ReplaceAll(msg, home, "~")
	}
	if r := []rune(msg); len(r) > maxDetailLen {
		msg = string(r[:maxDetailLen]) + "…"
	}
	return msg
}

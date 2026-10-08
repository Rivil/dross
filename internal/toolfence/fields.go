// Package toolfence declares the persisted schema fields that may carry
// tool-derived text, and what each one's disposition is.
//
// THE SCOPE IS THE internal/cmd → verify → mutation → telemetry PATH, and only
// that path. Those four packages are where dross spawns a mutation tool, tees
// its output, and writes the result into tests.json, verify.toml and
// telemetry.jsonl. The shared recorder (mutation.RecordToolFailure /
// mutation.RecordLegError) is the only writer allowed to put a tool's own
// failure into any of them, and the walker in internal/cmd enforces that over
// exactly these four roots — see Roots.
//
// THE AGENT-AUTHORED LEDGERS ARE OUT OF SCOPE ON PURPOSE, not by oversight.
// security.Finding, quality.Finding and techdebt.Finding also hold free text
// that reaches disk, but that text is written by an agent composing a report,
// not by a subprocess dross spawned and tee'd. Fencing them means constraining
// what an agent may write, which is a different mechanism (a pattern-based
// detector) and a different milestone criterion. Declaring them here with a
// disposition nobody could check would make this registry look complete while
// proving nothing.
//
// Mirrors internal/pathfence deliberately: registry plus residual, judged in
// both directions. A declared field that no longer exists is a stale
// declaration; a matching field with no declaration is an undeclared sink.
package toolfence

import (
	"fmt"
	"strings"
)

// Roots are the package directories the walker covers, relative to the repo
// root. Named here rather than only in the walker so the scope statement above
// is data a test can check, not prose a reader has to trust.
//
// internal/cmd is in scope because it holds three declared fields' live
// writers: verify.go constructs the LanguageRun carrying the Recorded Error,
// verify.go assigns SkippedFile.Reason, and telemetry.go is Event.ErrorDetail's
// only writer in the tree.
func Roots() []string {
	return []string{"internal/cmd", "internal/verify", "internal/mutation", "internal/telemetry"}
}

// Disposition thirds. Exactly one is non-nil on a well-formed entry.
type (
	// Recorded marks a field whose value may only come from the shared
	// recorder. Every assignment to it must have the carrier on its
	// right-hand side.
	Recorded struct {
		// Carrier names what holds the recorded value — a function whose
		// return is the carrier type, or a Type.Field that is. Named the way
		// pathfence names one, and for the same reason: the stored field
		// stays a Go string, so the type lives on the DERIVED value.
		Carrier string
	}

	// NotToolStream marks a persisted text field that is provably not the
	// captured stream — dross-authored prose, a parsed report value, an
	// enum label.
	NotToolStream struct {
		// Why records what the field actually holds, so the claim can be
		// judged rather than trusted.
		Why string
		// Writers names every site that assigns it. A writer that starts
		// assigning captured output makes this declaration false.
		//
		// For a field no Go code writes — CriterionResult.Notes is authored
		// by the agent editing verify.toml by hand — the Writer names that
		// authoring route in prose. The stale-declaration arm is not expected
		// to resolve such an entry to a call site.
		Writers []string
	}

	// Renderable marks a field DERIVED from a Recorded one, which a PR or
	// board composer may print (spec decision renderable_derivation).
	Renderable struct {
		// Why records what makes the derivation safe.
		Why string
		// SourcedFrom names the Recorded field it is derived from. The
		// exception is only as safe as the pin that keeps it derived, so the
		// source is named rather than implied.
		SourcedFrom string
	}
)

// Field is one declared field that may carry tool-derived text.
type Field struct {
	Struct   string // "verify.LanguageRun"
	Field    string // "Error"
	Tag      string // the toml/json key, lowercased, options stripped
	Artifact string // the file it is serialized into

	Recorded      *Recorded
	NotToolStream *NotToolStream
	Renderable    *Renderable
}

// Name is the "Struct.Field" key used for dedupe and reporting.
func (f Field) Name() string { return f.Struct + "." + f.Field }

// Package is the Go package qualifier of the declared struct.
func (f Field) Package() string {
	if i := strings.Index(f.Struct, "."); i > 0 {
		return f.Struct[:i]
	}
	return ""
}

// Fields returns the registry. The walker judges it in both directions: a
// matching persisted field with no entry fails, and an entry naming a field
// that no longer exists fails as a stale declaration.
func Fields() []Field { return append([]Field(nil), fields...) }

// Validate reports every way in which a registry is malformed.
//
// It takes the slice rather than reading the package var so a test can feed it
// synthetic bad entries. Asserting only that the REAL registry is clean would
// pass just as well against a Validate that checked nothing.
func Validate(in []Field) []error {
	var errs []error
	seen := map[string]bool{}

	for _, f := range in {
		name := f.Name()
		if f.Struct == "" {
			errs = append(errs, fmt.Errorf("entry %q: empty Struct", name))
		}
		if f.Field == "" {
			errs = append(errs, fmt.Errorf("entry %q: empty Field", name))
		}
		if f.Tag == "" {
			errs = append(errs, fmt.Errorf("entry %q: empty Tag", name))
		}
		if f.Artifact == "" {
			errs = append(errs, fmt.Errorf("entry %q: empty Artifact — an entry must say which file it is serialized into", name))
		}

		set := 0
		for _, on := range []bool{f.Recorded != nil, f.NotToolStream != nil, f.Renderable != nil} {
			if on {
				set++
			}
		}
		switch {
		case set == 0:
			errs = append(errs, fmt.Errorf("entry %q: no disposition — every declared field is recorded, provably not the tool stream, or a checked derivation of a recorded one", name))
		case set > 1:
			errs = append(errs, fmt.Errorf("entry %q: %d dispositions set, want exactly one", name, set))
		case f.Recorded != nil:
			if strings.TrimSpace(f.Recorded.Carrier) == "" {
				errs = append(errs, fmt.Errorf("entry %q: recorded with no Carrier — it must name what holds the recorded value", name))
			}
		case f.NotToolStream != nil:
			if strings.TrimSpace(f.NotToolStream.Why) == "" {
				errs = append(errs, fmt.Errorf("entry %q: not-tool-stream with no Why — an unfalsifiable claim", name))
			}
			if len(f.NotToolStream.Writers) == 0 {
				errs = append(errs, fmt.Errorf("entry %q: not-tool-stream naming no Writer — then nothing pins the claim when a writer is added", name))
			}
		case f.Renderable != nil:
			if strings.TrimSpace(f.Renderable.Why) == "" {
				errs = append(errs, fmt.Errorf("entry %q: renderable with no Why", name))
			}
			if strings.TrimSpace(f.Renderable.SourcedFrom) == "" {
				errs = append(errs, fmt.Errorf("entry %q: renderable with no SourcedFrom — the exception is only as safe as the recorded field it is derived from, so that field must be named", name))
			}
		}

		if seen[name] {
			errs = append(errs, fmt.Errorf("entry %q: declared twice", name))
		}
		seen[name] = true
	}
	return errs
}

// carrier is the one accepted source for a Recorded field's value.
const carrier = "mutation.RecordLegError"

var fields = []Field{
	// ---- Recorded: only the shared recorder may write these -------------
	{
		Struct: "verify.LanguageRun", Field: "Error", Tag: "error", Artifact: "tests.json",
		Recorded: &Recorded{Carrier: carrier},
	},
	{
		Struct: "verify.LegSummary", Field: "Error", Tag: "error", Artifact: "verify.toml",
		// Populated from LanguageRun.Error, which is itself Recorded — the
		// copy is an accepted right-hand side, so the guarantee travels with
		// the value rather than being re-established here.
		Recorded: &Recorded{Carrier: carrier},
	},

	// ---- Renderable: derived from a Recorded field ----------------------
	{
		Struct: "verify.Finding", Field: "Text", Tag: "text", Artifact: "verify.toml",
		Renderable: &Renderable{
			Why: "composed as \"mutation adapter <tool> failed: \" + LanguageRun.Error. It is " +
				"how a failed leg reaches a human at all, and forbidding a composer to render " +
				"it would delete the Findings section from every PR body while removing no " +
				"tool output — the record it interpolates carries none. Pinned rather than " +
				"asserted: persist_toolfence_test.go fixes this field to exactly the recorder " +
				"line under that prefix, so a composer interpolating anything else fails there.",
			SourcedFrom: "verify.LanguageRun.Error",
		},
	},

	// ---- NotToolStream: persisted text that is provably not the stream --
	{
		Struct: "telemetry.Event", Field: "ErrorDetail", Tag: "err_detail", Artifact: "telemetry.jsonl",
		NotToolStream: &NotToolStream{
			Why: "the redacted top-level message of a dross command's own error, kept only for " +
				"the CarriesDetail allowlist so the `other` bucket is not opaque. An adapter " +
				"failure arrives here as the recorder's message, which carries no tool output.",
			Writers: []string{"internal/cmd/telemetry.go (the only writer in the tree)"},
		},
	},
	{
		Struct: "verify.SkippedFile", Field: "Reason", Tag: "reason", Artifact: "tests.json",
		NotToolStream: &NotToolStream{
			Why: "dross's own reason no adapter ran on a file — a missing extension mapping, or " +
				"a path that no longer exists in the working tree. No tool is invoked on a " +
				"skipped file, so there is no stream to leak.",
			Writers: []string{
				"internal/verify/verify.go (no mutation adapter for <ext>)",
				"internal/cmd/verify.go (file no longer exists in the working tree)",
			},
		},
	},
	{
		Struct: "verify.CriterionResult", Field: "Notes", Tag: "notes", Artifact: "verify.toml",
		NotToolStream: &NotToolStream{
			Why: "the agent's own note on a criterion's verdict. No Go code assigns it: " +
				"Skeleton writes the field empty and it is filled in by the agent editing " +
				"verify.toml during /dross-verify.",
			Writers: []string{
				"authored by the agent editing verify.toml (no Go writer; the stale-declaration " +
					"arm resolves this entry to no call site by design)",
			},
		},
	},
	{
		Struct: "verify.OutOfScopeMutant", Field: "Note", Tag: "note", Artifact: "tests.json",
		NotToolStream: &NotToolStream{
			Why: "the survivor-lifecycle state's explanation — routed to <target>, acceptance " +
				"withheld, identity unresolved. Composed by dross from its own survivor ledger, " +
				"never from the tool's output.",
			Writers: []string{"internal/verify/lifecycle.go (applies the candidate's state to the mutant)"},
		},
	},
	{
		Struct: "verify.Scope", Field: "Degraded", Tag: "degraded", Artifact: "tests.json",
		NotToolStream: &NotToolStream{
			Why: "dross's own list of what the scope could not establish. The git-failure " +
				"entries interpolate gitReason(err), whose exec error renders as \"exit status 1\" " +
				"— it carries no tool output only because exec.ExitError.Error() omits the " +
				"stderr that .Output() captured. That is a stdlib PROPERTY this entry relies " +
				"on, not a guarantee dross makes; a writer switching to CombinedOutput would " +
				"make the claim false. []string rather than a scalar, which is why the walker " +
				"covers slice-of-string sinks.",
			Writers: []string{
				"internal/verify/scope.go (source degradations and out-of-repo rejections)",
				"internal/cmd/verifyscope.go (git resolution and diff failures, via gitReason)",
			},
		},
	},
	{
		Struct: "mutation.Mutant", Field: "Snippet", Tag: "snippet", Artifact: "tests.json",
		NotToolStream: &NotToolStream{
			Why: "the mutated source slice, read out of the tool's PARSED REPORT (a mutant's " +
				"replacement text), not out of its captured stdout. It is the project's own " +
				"source as the tool rewrote it. Untagged on this struct — capitalised in " +
				"tests.json — which is why the walker matches untagged exported string fields " +
				"by lowercased field name.",
			Writers: []string{"internal/mutation/stryker.go (from the report's mutant replacement)"},
		},
	},
	{
		Struct: "mutation.Mutant", Field: "Note", Tag: "note", Artifact: "tests.json",
		NotToolStream: &NotToolStream{
			Why: "the same survivor-lifecycle explanation as OutOfScopeMutant.Note, applied to " +
				"an in-scope survivor. Composed by dross from its own ledger. Untagged, as " +
				"Snippet is.",
			Writers: []string{"internal/verify/lifecycle.go (applies the candidate's state to the mutant)"},
		},
	},
}

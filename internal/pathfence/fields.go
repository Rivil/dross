package pathfence

import (
	"fmt"
	"strings"
)

// The path-shaped field registry.
//
// artifact_scope (locked) puts EVERY path-shaped field in the .dross schemas in
// scope, not merely the ones something opens today: scoping to today's consumers
// would leave the same bug one field over the moment a consumer is added. So
// each field is either routed through the shared check, or carries an explicit
// declaration that nothing opens it — a declaration a test can falsify, rather
// than a guarantee nobody can check.
//
// DISPOSITION CLASSIFIES READERS. A field is NotConsumed when nothing OPENS it,
// even if a validator gates it at write time: a validator constructs no
// Contained, so it proves nothing about what happens on the read path.
//
// THE STORED FIELDS STAY string / []string. They are serialized to changes.json,
// plan.toml and tests.json, and a Contained's unexported fields cannot marshal.
// The type therefore lives on the DERIVED value — hence Carrier, which names the
// Go type+field or function that HOLDS the checked value rather than naming the
// stored field. Carrier is deliberately not "the routing function's signature":
// internal/cmd imports this package, so a test here cannot import internal/cmd
// back to inspect it, and the relevant symbols are unexported there anyway. This
// package asserts a Carrier is DECLARED and well-formed; that the carrier is
// really typed Contained is asserted from a package cmd test, where the symbols
// are visible.

// Disposition halves. Exactly one is non-nil on a well-formed entry; both nil
// or both set is a malformed declaration, which Validate reports.
type (
	// ConsumedBy marks a field routed through the shared check.
	ConsumedBy struct {
		// Carrier names what holds the checked value — a function whose
		// return is Contained-typed, or a Type.Field that is.
		Carrier string
	}

	// NotConsumedBy marks a field nothing opens.
	NotConsumedBy struct {
		// Why records what the field IS, so the declaration can be judged
		// rather than trusted.
		Why string
		// Readers names every site allowed to touch it. A reader that opens
		// the value makes this declaration false.
		Readers []string
	}
)

// Field is one declared path-shaped schema field.
type Field struct {
	Struct   string // "changes.TaskRecord"
	Field    string // "Files"
	Tag      string // the toml/json key, lowercased, options stripped
	Artifact string // the file it is serialized into

	Consumed    *ConsumedBy
	NotConsumed *NotConsumedBy
}

// Name is the "Struct.Field" key used for dedupe and reporting.
func (f Field) Name() string { return f.Struct + "." + f.Field }

// Fields returns the registry. t-9's walker judges it in both directions: a
// path-shaped field with no entry fails, and an entry naming a field that no
// longer exists fails as a stale declaration.
func Fields() []Field {
	out := append([]Field(nil), fields...)
	return append(out, unguessedFields...)
}

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
		if f.Artifact == "" {
			errs = append(errs, fmt.Errorf("entry %q: empty Artifact — an entry must say which file it is serialized into", name))
		}
		if f.Tag == "" {
			errs = append(errs, fmt.Errorf("entry %q: empty Tag", name))
		}

		switch {
		case f.Consumed == nil && f.NotConsumed == nil:
			errs = append(errs, fmt.Errorf("entry %q: no disposition — every field is either consumed through the check or explicitly declared unopened", name))
		case f.Consumed != nil && f.NotConsumed != nil:
			errs = append(errs, fmt.Errorf("entry %q: both dispositions set", name))
		case f.Consumed != nil:
			if strings.TrimSpace(f.Consumed.Carrier) == "" {
				errs = append(errs, fmt.Errorf("entry %q: consumed with no Carrier — it must name what holds the checked value", name))
			}
		case f.NotConsumed != nil:
			if strings.TrimSpace(f.NotConsumed.Why) == "" {
				errs = append(errs, fmt.Errorf("entry %q: not-consumed with no Why — an unfalsifiable claim", name))
			}
			if len(f.NotConsumed.Readers) == 0 {
				errs = append(errs, fmt.Errorf("entry %q: not-consumed naming no reader — then nothing pins the claim when a reader is added", name))
			}
		}

		if seen[name] {
			errs = append(errs, fmt.Errorf("entry %q: declared twice", name))
		}
		seen[name] = true
	}
	return errs
}

var fields = []Field{
	// ---- Consumed: routed through Contain -------------------------------
	{
		Struct: "changes.TaskRecord", Field: "Files", Tag: "files", Artifact: "changes.json",
		Consumed: &ConsumedBy{Carrier: "containScope"},
	},
	{
		Struct: "verify.Scope", Field: "Files", Tag: "files", Artifact: "tests.json",
		Consumed: &ConsumedBy{Carrier: "containScope"},
	},
	{
		Struct: "changes.RedProof", Field: "Doc", Tag: "doc", Artifact: "changes.json",
		Consumed: &ConsumedBy{Carrier: "redProofPin.Doc"},
	},
	{
		Struct: "survivor.Acceptance", Field: "File", Tag: "file", Artifact: "survivors.toml",
		Consumed: &ConsumedBy{Carrier: "survivor.AcceptanceFile"},
	},

	// ---- NotConsumed: nothing opens these -------------------------------
	{
		Struct: "phase.Task", Field: "Files", Tag: "files", Artifact: "plan.toml",
		NotConsumed: &NotConsumedBy{
			Why: "ValidatePlan gates these at WRITE time and opens nothing; a validator " +
				"constructs no Contained, so it proves nothing about a reader. Every " +
				"reader is print-only.",
			Readers: []string{
				"internal/cmd/task.go (prints the file list)",
				"internal/cmd/issue_task.go (renders the board checklist)",
			},
		},
	},
	{
		Struct: "project.Env", Field: "Files", Tag: "files", Artifact: "project.toml",
		NotConsumed: &NotConsumedBy{
			Why: "an env-file load order dross records but never opens itself; the shell " +
				"sources these, dross only displays and edits the list.",
			Readers: []string{
				"internal/cmd/project.go (get/set of the csv value)",
			},
		},
	},
	{
		Struct: "project.TestLane", Field: "Match", Tag: "match", Artifact: "project.toml",
		NotConsumed: &NotConsumedBy{
			Why: "glob PATTERNS matched against candidate paths, not paths themselves. " +
				"They are arguments to filepath.Match, never to an open.",
			Readers: []string{
				"internal/cmd/lane_plan.go (builds the glob set)",
				"internal/cmd/validate.go (compiles each pattern)",
			},
		},
	},

	// project.Paths.*: the repo layout, displayed and edited, never opened by
	// dross itself. Declared one per field rather than as a group so t-9's
	// two-way check has a name to match against.
	pathsField("Source", "source"),
	pathsField("Tests", "tests"),
	pathsField("E2E", "e2e"),
	pathsField("Migrations", "migrations"),
	pathsField("Schemas", "schemas"),
	pathsField("I18n", "i18n"),
	pathsField("Public", "public"),

	// ---- NotConsumed: path-shaped BY TAG ONLY ---------------------------
	// These three are what t-9's walker reaches by tag and would otherwise
	// report as undeclared. Declaring them here — rather than special-casing
	// them inside the walker — keeps the walker's rule simple and puts the
	// judgement where a reader can audit it.
	{
		Struct: "verify.Scope", Field: "Source", Tag: "source", Artifact: "tests.json",
		NotConsumed: &NotConsumedBy{
			Why: "an ENUM LABEL (git-only / changes-only / union / none) recording which " +
				"inputs contributed to the scope. The `source` tag is path-shaped; the " +
				"value never is.",
			Readers: []string{"internal/verify/scope.go (set by NewScope, rendered in reports)"},
		},
	},
	{
		Struct: "verify.LanguageRun", Field: "Files", Tag: "files", Artifact: "tests.json",
		NotConsumed: &NotConsumedBy{
			Why: "paths a mutation TOOL reported in its own output, recorded verbatim for " +
				"the report. dross does not open them — it already has the tool's verdict.",
			Readers: []string{"internal/verify (report rendering)"},
		},
	},
	{
		Struct: "verify.Provenance", Field: "Files", Tag: "files", Artifact: "tests.json",
		NotConsumed: &NotConsumedBy{
			Why: "a READOUT of verify.Scope.Files — the projection `dross verify scope` " +
				"prints and emits as --json. Copied field-for-field from a loaded " +
				"tests.json, whose Files were already contained on the way in; nothing " +
				"opens these paths, they are printed.",
			Readers: []string{"internal/cmd/verifyscope.go (verify scope: print / --json)"},
		},
	},
	{
		Struct: "verify.ProvenanceLeg", Field: "Files", Tag: "files", Artifact: "tests.json",
		NotConsumed: &NotConsumedBy{
			Why: "a READOUT of verify.LanguageRun.Files — the paths the leg was " +
				"dispatched, copied verbatim into the `dross verify scope` projection. " +
				"Printed beside the leg's ranges and whole-file reasons, never opened.",
			Readers: []string{"internal/cmd/verifyscope.go (verify scope: print / --json)"},
		},
	},
	{
		Struct: "verify.CriterionResult", Field: "Tests", Tag: "tests", Artifact: "tests.json",
		NotConsumed: &NotConsumedBy{
			Why: "TEST NAMES (e.g. TestContainRefusesEscape), not paths. The `tests` tag " +
				"collides with project.Paths.Tests, which is a directory; this is not.",
			Readers: []string{"internal/verify (criterion-to-test mapping in the report)"},
		},
	},
}

// The fields below were outside the old walker's four schema directories and
// eleven tag words, and were found when the enumeration stopped guessing. None
// of them is opened by this process: each is a location on ANOTHER machine, a
// path printed or emitted for a reader, a pattern, or a path a tool reported
// about its own work. The path-field taint scan (pathtaint_audit_test.go) holds
// every one of these claims to the code.

// remotePath declares a location on the granted remote host. It is an operand
// in the ssh/rsync argv internal/remote builds — checked there against the host
// allowlist — and never a path on this machine's filesystem.
func remotePath(strct, field, tag, artifact string, readers ...string) Field {
	return Field{
		Struct: strct, Field: field, Tag: tag, Artifact: artifact,
		NotConsumed: &NotConsumedBy{
			Why: "a directory on the REMOTE host, carried into ssh/rsync argv by internal/remote; " +
				"this process never opens it.",
			Readers: readers,
		},
	}
}

// reportPath declares a path a command prints or emits for a reader.
func reportPath(strct, field, tag, artifact, why string, readers ...string) Field {
	return Field{
		Struct: strct, Field: field, Tag: tag, Artifact: artifact,
		NotConsumed: &NotConsumedBy{Why: why, Readers: readers},
	}
}

var unguessedFields = []Field{
	// ---- remote-host locations ------------------------------------------
	remotePath("cmd.detachedRun", "RunDir", "run_dir", "local.toml", "internal/cmd/verify.go (detached status/collect)"),
	remotePath("cmd.detachedRun", "Workdir", "workdir", "local.toml", "internal/cmd/test.go", "internal/cmd/verify.go"),
	remotePath("cmd.localStore", "RemoteWorkdir", "remote_workdir", "local.toml", "internal/cmd/local.go", "internal/cmd/remote_grant.go"),
	remotePath("cmd.localStore", "MutationRemoteWorkdir", "mutation_remote_workdir", "local.toml", "internal/cmd/local.go", "internal/cmd/remote_grant.go"),
	remotePath("cmd.localStore", "RemoteScratchBase", "remote_scratch_base", "local.toml", "internal/cmd/local.go"),
	remotePath("cmd.remoteCandidate", "Workdir", "workdir", "local.toml", "internal/cmd/local.go", "internal/cmd/remote_grant.go"),
	remotePath("project.Mutation", "RemoteWorkdir", "remote_workdir", "project.toml", "internal/project/project.go"),

	// ---- repo-layout settings recorded, not opened -----------------------
	reportPath("project.MutationStryker", "Workdir", "workdir", "project.toml",
		"the repo-relative directory the Stryker leg runs in: handed to the adapter as the child "+
			"process's Dir and the base its --mutate paths are made relative to.",
		"internal/cmd/project.go (get/set)", "internal/mutationcfg/mutationcfg.go (adapter config)"),
	reportPath("project.Repo", "RootRunDir", "root_run_dir", "project.toml",
		"a repo-layout setting dross records and edits; nothing in this process opens it.",
		"internal/cmd/project.go (get/set)"),
	reportPath("project.Repo", "Workspaces", "workspaces", "project.toml",
		"workspace directories of a multi-package repo, recorded and edited; never opened.",
		"internal/cmd/project.go (get/set)"),
	reportPath("project.Techdebt", "Exclude", "exclude", "project.toml",
		"path PREFIXES a techdebt scan skips — matched against candidate paths, never opened.",
		"internal/cmd/techdebt.go (builds the exclusion set)"),

	// ---- printed or emitted for a reader ---------------------------------
	reportPath("cmd.previewReport", "OutOfTree", "out_of_tree", "`dross test --preview --json` output",
		"paths a lane preview could not place in the tree, printed and emitted — never opened.",
		"internal/cmd/lane_preview.go", "internal/cmd/lane_preview_json.go"),
	reportPath("cmd.previewReport", "Unmatched", "unmatched", "`dross test --preview --json` output",
		"paths no declared lane matched, printed and emitted — never opened.",
		"internal/cmd/lane_preview.go", "internal/cmd/lane_preview_json.go"),
	reportPath("cmd.previewLaneReport", "Dropped", "dropped", "`dross test --preview --json` output",
		"paths a lane's selector dropped, printed and emitted — never opened.",
		"internal/cmd/lane_preview.go"),
	reportPath("cmd.previewLaneReport", "Selector", "selector", "`dross test --preview --json` output",
		"the selector arguments a lane would append to its command, printed — never opened.",
		"internal/cmd/lane_preview.go"),
	reportPath("cmd.statsSummary", "Path", "path", "`dross stats --json` output",
		"where the telemetry log lives, printed for the reader — the log itself is opened elsewhere, "+
			"from its own resolved location.",
		"internal/cmd/stats.go"),
	reportPath("changes.Landmark", "Loc", "loc", "changes.json",
		"a file:line pointer recorded for ARCHITECTURE.md, copied into the doc as text.",
		"internal/changes/changes.go"),
	reportPath("verify.SkippedFile", "File", "file", "tests.json",
		"a file the mutation pass skipped, with why — rendered in the report.",
		"internal/cmd/verify.go", "internal/verify/verify.go"),
	reportPath("verify.LegSummary", "WholeFile", "whole_file", "tests.json",
		"files a leg measured whole rather than by range, summarised for the report.",
		"internal/verify/verify.go"),
	reportPath("verify.LegSummary", "Ranges", "ranges", "tests.json",
		"file:range labels a leg measured, summarised for the report.",
		"internal/verify/verify.go"),
	reportPath("verify.OutOfScopeMutant", "File", "file", "tests.json",
		"the file a mutation tool reported an out-of-scope survivor in, carried into its lifecycle "+
			"and the report.",
		"internal/verify/lifecycle.go", "internal/verify/verify.go"),

	// ---- scan findings: where a finding points ---------------------------
	reportPath("findings.Record", "File", "file", "findings.toml",
		"the file a recorded finding points at — compared and printed, never opened.",
		"internal/cmd/findings.go", "internal/findings/reconcile.go"),
	reportPath("security.Finding", "File", "file", "security run findings.toml",
		"the file a security finding points at — compared across runs and printed.",
		"internal/security/lifecycle.go"),
	reportPath("quality.Finding", "File", "file", "quality run findings.toml",
		"the file a quality finding points at — compared across runs and printed.",
		"internal/quality/lifecycle.go"),

	// ---- paths a tool reported about its own work --------------------------
	reportPath("codex.astGrepMatch", "File", "file", "ast-grep --json output",
		"decoded from ast-grep's own report; dross does not open it.",
		"internal/codex/ast_grep.go (decoded)"),
	reportPath("mutation.astRequest", "File", "file", "the Stryker AST helper's stdin request",
		"a path handed to the node AST helper, which opens it itself; dross does not.",
		"internal/mutation/construct.go"),
	reportPath("mutation.gremlinsFile", "Filename", "file_name", "the gremlins JSON report",
		"a file gremlins reported mutating, recorded against its verdict; dross already has the verdict.",
		"internal/mutation/gremlins.go"),
	reportPath("mutation.strykerNetRoot", "ProjectRoot", "projectroot", "the Stryker.NET JSON report",
		"the project root Stryker.NET reports, used as a PREFIX to re-root the files it names.",
		"internal/mutation/stryker_net.go"),

	// ---- stack profiles: names matched against a listing -------------------
	reportPath("stack.Signals", "Files", "files", "stack profile TOML",
		"marker FILE NAMES looked up in the set of files the detector listed; never opened.",
		"internal/stack/detect.go"),
	reportPath("stack.Signals", "FilePatterns", "file_patterns", "stack profile TOML",
		"glob PATTERNS matched against listed file names with filepath.Match.",
		"internal/stack/profile.go"),
	reportPath("stack.PackageManager", "Lockfile", "lockfile", "stack profile TOML",
		"a lockfile NAME describing the package manager; matched, never opened.",
		"internal/stack (profile data)"),
}

// pathsField builds one project.Paths entry. They differ only in name and tag.
func pathsField(field, tag string) Field {
	return Field{
		Struct: "project.Paths", Field: field, Tag: tag, Artifact: "project.toml",
		NotConsumed: &NotConsumedBy{
			Why: "a repo-layout directory dross records so commands can SUGGEST where " +
				"things live. dross displays and edits it; it never opens the path.",
			Readers: []string{"internal/cmd/project.go (get/set of the value)"},
		},
	}
}

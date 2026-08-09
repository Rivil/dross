package argfence

// The per-tool argv policy table.
//
// dross spawns a handful of binaries with values it derived from config, argv
// or recorded state. Every one of those binaries decides what is an option by
// looking at the leading character, not at the position — so a package path of
// "--output=/tmp/x" is not a lookup that fails, it is a flag that succeeds.
//
// Not every tool can be defended the same way, and the locked
// `analyzer_arg_policy` decision says so explicitly: each tool gets the
// strongest guarantee it can actually offer rather than a uniform rule levelled
// to the weakest.
//
//   - The tools that honour an end-of-options token get one, emitted
//     UNCONDITIONALLY. Everything after it is data by construction, which is
//     what makes the audit gate decidable: it looks for a positional that is
//     not preceded by a separator, not for a value it judges suspicious.
//   - The tools that have no such token get outright rejection of a
//     leading-dash value. Rejection IS a classifier, and a classifier is the
//     vulnerability config-trust-hardening was written about — so it is the
//     fallback where no separator exists, never the default.
//
// The table is exported through Policy() so the runtime call sites and the
// subprocess audit gate read one map rather than two that drift.

// Kind is which of the two defences a tool supports.
type Kind int

const (
	// Separator means the tool honours an end-of-options token: derived values
	// go behind Rule.Token and are safe whatever they contain.
	Separator Kind = iota + 1
	// Reject means the tool has no end-of-options token, so a derived value
	// beginning with a dash is refused before exec rather than fenced.
	Reject
)

func (k Kind) String() string {
	switch k {
	case Separator:
		return "separator"
	case Reject:
		return "reject"
	}
	return "unknown"
}

// Rule is one tool's entry in the table.
type Rule struct {
	// Kind is the defence this tool supports.
	Kind Kind
	// Token is the end-of-options token for Separator tools, empty for Reject.
	// It is not the same string for every tool: git distinguishes
	// `--end-of-options` (stop reading options) from `--` (stop reading options
	// AND treat the rest as pathspecs), and using the wrong one turns an
	// injection into a different bug.
	Token string
	// Why records why this tool got this policy, so an entry cannot be flipped
	// without someone reading the reason it was set.
	Why string
}

// table is the single runtime source. PolicyFor and Policy() both read it;
// nothing else may hold a second copy.
var table = map[string]Rule{
	"git": {
		Kind:  Separator,
		Token: "--end-of-options",
		Why:   "git >= 2.24 ends option parsing without reclassifying refs as pathspecs; `--` would make a branch name a path",
	},
	"gh": {
		Kind:  Separator,
		Token: "--",
		Why:   "cobra ends flag parsing at `--`; flags must stay ahead of it or they become positionals",
	},
	"ast-grep": {
		Kind:  Separator,
		Token: "--",
		Why:   "clap honours `--`, so the file operand is safe behind it",
	},
	"semgrep": {
		Kind:  Separator,
		Token: "--",
		Why:   "argparse honours `--`; no Go call site today, listed so the first one is fenced from birth",
	},
	"gremlins": {
		Kind: Reject,
		Why:  "no end-of-options token; a leading-dash package path is refused before exec",
	},
	"npx": {
		Kind: Reject,
		Why:  "npx consumes leading-dash arguments itself before the wrapped binary sees them",
	},
	"dotnet": {
		Kind: Reject,
		Why:  "dotnet and stryker.net both parse options positionally with no end-of-options token",
	},
}

// PolicyFor returns the rule for a binary, and whether one exists. It fails
// closed: an unlisted binary returns ok=false, never a permissive default, so a
// tool nobody anticipated is in scope the day it is added.
func PolicyFor(tool string) (Rule, bool) {
	r, ok := table[tool]
	return r, ok
}

// Policy returns a copy of the whole table, for the audit gate and for tests
// that enumerate rather than sample. Mutating the returned map does not affect
// PolicyFor.
func Policy() map[string]Rule {
	out := make(map[string]Rule, len(table))
	for k, v := range table {
		out[k] = v
	}
	return out
}

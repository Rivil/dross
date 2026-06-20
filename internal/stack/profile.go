// Package stack handles stack profiles: declarative, per-stack bundles that tune
// dross to a detected technology stack. One profile supplies three things:
//
//   - runtime command tuning (test/typecheck/format/build), with multiple
//     command variants per slot and per-OS restriction;
//   - the tool loadout (security scanners + quality analyzers), with optional
//     availability-gated tools and per-OS binary names;
//   - the agent loadout (recommended MCP tools, guardrails, conventions).
//
// Built-in profiles are embedded in the binary (see embed.go); a user can add or
// override profiles by dropping TOML into ~/.claude/dross/profiles/. The schema is
// deliberately general so a new stack is a single TOML drop-in with zero code
// change — honoring the locked schema_extensibility and scope_go_first decisions.
package stack

import (
	"errors"
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"
)

// Profile is one stack profile keyed by ID (e.g. "go"). Built-in profiles ship
// embedded; user profiles override by matching ID.
type Profile struct {
	ID       string           `toml:"id"`                // required, non-empty (e.g. "go")
	Title    string           `toml:"title,omitempty"`   // human label
	Signals  Signals          `toml:"signals,omitempty"` // how Detect matches this profile
	Packages []PackageManager `toml:"package_managers,omitempty"`
	Runtime  RuntimeCommands  `toml:"runtime,omitempty"`
	Tools    []Tool           `toml:"tools,omitempty"`   // scanner/analyzer loadout
	Loadout  Loadout          `toml:"loadout,omitempty"` // agent loadout (c-4)
}

// Signals declare the filesystem evidence that selects this profile. Detect (see
// detect.go) keys off these declared signals rather than a hardcoded language
// switch, so a new profile is selectable by data alone.
type Signals struct {
	Files    []string `toml:"files,omitempty"` // root filenames, e.g. "go.mod"
	Exts     []string `toml:"exts,omitempty"`  // file extensions, e.g. ".go"
	Priority int      `toml:"priority,omitempty"`
}

// PackageManager is one package-manager variant the stack may use (npm/pnpm/yarn,
// pip/poetry/uv, …). A profile lists every variant it can drive; the first whose
// Bin (or Lockfile) is present is the active one.
type PackageManager struct {
	Name     string `toml:"name"`               // e.g. "go", "pnpm"
	Bin      string `toml:"bin,omitempty"`      // executable; defaults to Name
	Lockfile string `toml:"lockfile,omitempty"` // e.g. "go.sum", "pnpm-lock.yaml"
}

// RuntimeCommands are the command slots dross seeds into project.toml [runtime].
type RuntimeCommands struct {
	Test      Command `toml:"test,omitempty"`
	Typecheck Command `toml:"typecheck,omitempty"`
	Format    Command `toml:"format,omitempty"`
	Build     Command `toml:"build,omitempty"`
}

// Command is a single runtime slot. It supports either a shorthand single command
// (Run) or an ordered list of Variants — multiple commands per slot — from which a
// resolver picks the first available (see runtime.go). Variants may be gated by an
// availability Bin and/or restricted to an OS.
type Command struct {
	Run      string           `toml:"run,omitempty"`      // shorthand: single command line
	Variants []CommandVariant `toml:"variants,omitempty"` // ordered alternatives
}

// CommandVariant is one alternative for a runtime slot.
type CommandVariant struct {
	Run string `toml:"run"`           // the command line, e.g. "go test -count=1 ./..."
	Bin string `toml:"bin,omitempty"` // availability gate: usable only if on PATH
	OS  string `toml:"os,omitempty"`  // restrict to GOOS, e.g. "darwin"; empty = any
}

// Tool is one scanner or analyzer in the profile's loadout. Kind routes it to the
// security ("scanner") or quality ("analyzer") catalog. Optional marks an
// availability-gated tool that is silently skipped when absent (vs Core, whose
// absence warrants a prominent warning). BinByOS overrides the looked-up binary
// name per GOOS (e.g. a tool packaged under a different name on macOS vs Linux).
type Tool struct {
	Name      string            `toml:"name"`
	Bin       string            `toml:"bin,omitempty"`       // defaults to Name
	BinByOS   map[string]string `toml:"bin_by_os,omitempty"` // GOOS -> binary name
	Kind      string            `toml:"kind,omitempty"`      // "scanner" | "analyzer"
	Dimension string            `toml:"dimension,omitempty"` // for analyzers: the maintainability axis measured
	Optional  bool              `toml:"optional,omitempty"`
	Core      bool              `toml:"core,omitempty"`
	Install   string            `toml:"install,omitempty"`
}

// Loadout is the agent loadout rendered by `dross stack loadout` (c-4).
type Loadout struct {
	MCPTools    []string `toml:"mcp_tools,omitempty"`
	Guardrails  []string `toml:"guardrails,omitempty"`
	Conventions []string `toml:"conventions,omitempty"`
}

// EffectiveBin returns the binary name to look up on PATH for the given GOOS,
// preferring a per-OS override, then Bin, then Name.
func (t Tool) EffectiveBin(goos string) string {
	if b, ok := t.BinByOS[goos]; ok && b != "" {
		return b
	}
	if t.Bin != "" {
		return t.Bin
	}
	return t.Name
}

// Decode parses a profile from raw TOML bytes (used for embedded and user
// profiles) and validates it.
func Decode(data []byte) (*Profile, error) {
	var p Profile
	if _, err := toml.Decode(string(data), &p); err != nil {
		return nil, fmt.Errorf("decode profile: %w", err)
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

// Load reads and validates a profile from a TOML file path.
func Load(path string) (*Profile, error) {
	var p Profile
	if _, err := toml.DecodeFile(path, &p); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("invalid profile %s: %w", path, err)
	}
	return &p, nil
}

// Validate enforces the invariants every profile must hold: a non-empty id (a
// profile must be addressable) and every tool resolving to some binary name (an
// unnameable tool can never be detected on PATH).
func (p *Profile) Validate() error {
	if strings.TrimSpace(p.ID) == "" {
		return errors.New("profile id is empty")
	}
	for i, t := range p.Tools {
		if t.EffectiveBin("") == "" && len(t.BinByOS) == 0 {
			return fmt.Errorf("tool[%d] (%q) has no bin", i, t.Name)
		}
	}
	return nil
}

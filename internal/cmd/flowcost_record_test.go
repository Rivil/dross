package cmd

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// flowLeg is one measured run of a phase's flow: the agent's turns (distinct
// main-agent message ids), the tool each turn called, and its wall-clock with
// the human's waits excluded, on a named binary in a named session.
type flowLeg struct {
	AgentTurns    int      `toml:"agent_turns"`
	TurnTools     []string `toml:"turn_tools"`
	WallClockS    float64  `toml:"wall_clock_s"`
	BinaryVersion string   `toml:"binary_version"`
	SessionID     string   `toml:"session_id"`
	NudgeFired    bool     `toml:"nudge_fired"`
	NudgesSeen    int      `toml:"nudges_seen"`
	MidWave       bool     `toml:"mid_wave"`
	GateLead      string   `toml:"gate_lead"`
	DrossCalls    []string `toml:"dross_calls"`

	GreenTestBytes      int `toml:"green_test_bytes"`
	RedTestBytes        int `toml:"red_test_bytes"`
	VerifyLegBytes      int `toml:"verify_leg_bytes"`
	RuleShowRepeatBytes int `toml:"rule_show_repeat_bytes"`
	LogReads            int `toml:"log_reads"`

	PromptBytes    int    `toml:"prompt_bytes"`
	PartialFetches int    `toml:"partial_fetches"`
	Verdict        string `toml:"verdict"`
	Detached       bool   `toml:"detached"`
	FixtureTree    string `toml:"fixture_tree"`

	StatusBytes int `toml:"status_bytes"`
}

// markerLive records the show-repeat marker observed in a real session: a
// repeat in one context, a full block after /clear and after /compact, and a
// subagent's repeat naming the --full command.
type markerLive struct {
	Repeat                  bool `toml:"repeat"`
	ClearFull               bool `toml:"clear_full"`
	CompactFull             bool `toml:"compact_full"`
	SubagentMarkerNamesFull bool `toml:"subagent_marker_names_full"`
}

type flowCost struct {
	Scenario     map[string]any `toml:"scenario"`
	Before       *flowLeg       `toml:"before"`
	After        *flowLeg       `toml:"after"`
	VerifyBefore *flowLeg       `toml:"verify_before"`
	VerifyAfter  *flowLeg       `toml:"verify_after"`
	Control      *flowLeg       `toml:"control"`
	MarkerLive   *markerLive    `toml:"marker_live"`
}

// TestFlowCostRecords guards milestone v1.8's c-6 record: every phase's
// flow-cost.toml names its scenario well enough to replay, measures a before
// and an after leg completely, and the after leg adds no round-trip: it
// invokes no dross call the before leg did not. Turns and wall-clock stay
// recorded but ungated — the same steps batched into more or fewer tool calls
// move them from run to run (rivil, 2026-10-08). For
// context-boundaries (c-8) it also pins what the after leg exists to show:
// the nudge fired once on the new binary and turned the mid-wave §1g lead into
// checkpoint, while a below-threshold control kept continue. For
// test-output-summaries (c-8) it pins the shrink: smaller green and verify
// output, a red step that never opened the log, a repeat rule show under 300
// bytes, and the marker live across /clear, /compact and a subagent. For
// prompt-partials (c-6) it adds a verify pair under the same gates and pins
// the base prompts' bytes, the budget and the zero-fetch common path. For
// memory-prune-advisor (c-6) it pins a cold-cache after leg and the bytes
// dross status printed on both legs.
func TestFlowCostRecords(t *testing.T) {
	root := filepath.Join(repoRootFromTest(t), RootDirName)
	paths, err := filepath.Glob(filepath.Join(root, "phases", "*", "flow-cost.toml"))
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, path := range paths {
		phase := filepath.Base(filepath.Dir(path))
		found[phase] = true
		t.Run(phase, func(t *testing.T) {
			var fc flowCost
			if _, err := toml.DecodeFile(path, &fc); err != nil {
				t.Fatalf("decode %s: %v", path, err)
			}
			for _, key := range []string{"repo_shape", "wave_position", "flow_start", "flow_end", "jq_method"} {
				if s, _ := fc.Scenario[key].(string); s == "" {
					t.Errorf("[scenario] has no %s — the flow cannot be replayed identically", key)
				}
			}
			if !checkLegPair(t, "before", fc.Before, "after", fc.After) {
				return
			}
			switch phase {
			case "context-boundaries":
				checkContextBoundariesFlow(t, fc)
			case "test-output-summaries":
				checkTestOutputSummariesFlow(t, fc, filepath.Dir(path))
			case "prompt-partials":
				checkPromptPartialsFlow(t, fc, filepath.Dir(path))
			case "memory-prune-advisor":
				checkMemoryPruneAdvisorFlow(t, fc)
			}
		})
	}
	for _, want := range []string{"context-boundaries", "test-output-summaries", "prompt-partials", "memory-prune-advisor"} {
		if !found[want] {
			t.Errorf("%s has no flow-cost.toml — its before/after flow-cost record is missing", want)
		}
	}
}

// checkLegPair checks that both legs are complete measurements and that the
// after leg ran a new binary with no dross call the before leg lacked. It
// reports whether both legs exist, so a caller can stop short of nil legs.
func checkLegPair(t *testing.T, beforeName string, before *flowLeg, afterName string, after *flowLeg) bool {
	t.Helper()
	for _, l := range []struct {
		name string
		leg  *flowLeg
	}{{beforeName, before}, {afterName, after}} {
		name, leg := l.name, l.leg
		if leg == nil {
			t.Errorf("[%s] is missing", name)
			continue
		}
		if leg.AgentTurns <= 0 || leg.WallClockS <= 0 {
			t.Errorf("[%s] agent_turns = %d, wall_clock_s = %v — not a measurement", name, leg.AgentTurns, leg.WallClockS)
		}
		if leg.BinaryVersion == "" || leg.SessionID == "" {
			t.Errorf("[%s] lacks binary_version or session_id", name)
		}
		if len(leg.TurnTools) != leg.AgentTurns {
			t.Errorf("[%s] turn_tools has %d entries for %d turns — a turn-count difference cannot be attributed", name, len(leg.TurnTools), leg.AgentTurns)
		}
		if len(leg.DrossCalls) == 0 {
			t.Errorf("[%s] records no dross_calls — the round-trip gate has nothing to compare", name)
		}
	}
	if before == nil || after == nil {
		return false
	}
	for _, call := range after.DrossCalls {
		if !slices.Contains(before.DrossCalls, call) {
			t.Errorf("[%s] runs %q, which [%s] did not — the change added a round-trip", afterName, call, beforeName)
		}
	}
	if after.BinaryVersion == before.BinaryVersion {
		t.Errorf("[%s] and [%s] both ran on %q — the after leg did not measure the new binary", beforeName, afterName, after.BinaryVersion)
	}
	return true
}

func checkContextBoundariesFlow(t *testing.T, fc flowCost) {
	t.Helper()
	a := fc.After
	if !a.NudgeFired || a.NudgesSeen != 1 {
		t.Errorf("after: nudge_fired = %v, nudges_seen = %d; want one nudge, fired once", a.NudgeFired, a.NudgesSeen)
	}
	if !a.MidWave || a.GateLead != "checkpoint" {
		t.Errorf("after: mid_wave = %v, gate_lead = %q; want a mid-wave §1g that led with checkpoint", a.MidWave, a.GateLead)
	}
	if fc.Control == nil {
		t.Error("[control] is missing — nothing shows a below-threshold mid-wave §1g still leads with continue")
	} else if c := fc.Control; c.GateLead != "continue" || !c.MidWave || c.NudgesSeen != 0 || c.NudgeFired {
		t.Errorf("[control] gate_lead = %q, mid_wave = %v, nudges_seen = %d, nudge_fired = %v; want a nudge-free mid-wave §1g that still led with continue",
			c.GateLead, c.MidWave, c.NudgesSeen, c.NudgeFired)
	}
}

// checkTestOutputSummariesFlow pins test-output-summaries' c-8 numbers. Red
// output is recorded on both legs but not gated (D6): a failing test's own
// lines are the summary's payload.
func checkTestOutputSummariesFlow(t *testing.T, fc flowCost, dir string) {
	t.Helper()
	b, a := fc.Before, fc.After
	for name, leg := range map[string]*flowLeg{"before": b, "after": a} {
		if leg.GreenTestBytes <= 0 || leg.RedTestBytes <= 0 || leg.VerifyLegBytes <= 0 || leg.RuleShowRepeatBytes <= 0 {
			t.Errorf("[%s] green %d, red %d, verify %d, rule-repeat %d bytes — every *_bytes must be measured",
				name, leg.GreenTestBytes, leg.RedTestBytes, leg.VerifyLegBytes, leg.RuleShowRepeatBytes)
		}
	}
	if a.GreenTestBytes >= b.GreenTestBytes || a.GreenTestBytes > 1024 {
		t.Errorf("green dross test printed %d bytes after, %d before; want fewer, and at most 1024", a.GreenTestBytes, b.GreenTestBytes)
	}
	if a.VerifyLegBytes >= b.VerifyLegBytes {
		t.Errorf("a verify leg printed %d bytes after, %d before; want fewer", a.VerifyLegBytes, b.VerifyLegBytes)
	}
	if a.LogReads != 0 {
		t.Errorf("after: log_reads = %d; the red step should act on the summary without opening the log", a.LogReads)
	}
	if a.RuleShowRepeatBytes >= 300 {
		t.Errorf("after: a repeat rule show printed %d bytes; want the marker line, under 300", a.RuleShowRepeatBytes)
	}
	if m := fc.MarkerLive; m == nil {
		t.Error("[marker_live] is missing — nothing shows the marker works in a real session")
	} else if !m.Repeat || !m.ClearFull || !m.CompactFull || !m.SubagentMarkerNamesFull {
		t.Errorf("[marker_live] repeat = %v, clear_full = %v, compact_full = %v, subagent_marker_names_full = %v; want all true",
			m.Repeat, m.ClearFull, m.CompactFull, m.SubagentMarkerNamesFull)
	}
	if _, err := os.Stat(filepath.Join(dir, "flow-cost-before.toml")); err == nil {
		t.Error("flow-cost-before.toml still exists — t-16 moves its [before] into flow-cost.toml and deletes it")
	}
}

// promptPartialsBase is the build the before legs ran: the phase's base
// commit, prompts unedited, before any of the phase's tasks landed.
const promptPartialsBase = "commit 3ee0fe71"

// checkPromptPartialsFlow pins prompt-partials' c-6 numbers: on the same
// fixture trees, a pair execute task and an attached verify load base prompts
// under the 15,000-byte cap and smaller than before, fetch no partial on the
// common path, and the verify still reaches a clean pass. The before legs are
// pinned to the unedited prompts on the base build, so they cannot drift.
func checkPromptPartialsFlow(t *testing.T, fc flowCost, dir string) {
	t.Helper()
	if !checkLegPair(t, "verify_before", fc.VerifyBefore, "verify_after", fc.VerifyAfter) {
		return
	}
	const budget = 15000
	for _, p := range []struct {
		before, after   string
		b, a            *flowLeg
		basePromptBytes int
	}{
		{"before", "after", fc.Before, fc.After, 25853},
		{"verify_before", "verify_after", fc.VerifyBefore, fc.VerifyAfter, 25864},
	} {
		if p.b.PromptBytes != p.basePromptBytes {
			t.Errorf("[%s] prompt_bytes = %d, want %d — the before leg did not load the unedited prompt", p.before, p.b.PromptBytes, p.basePromptBytes)
		}
		if !strings.Contains(p.b.BinaryVersion, promptPartialsBase) {
			t.Errorf("[%s] ran on %q, want the %s base build", p.before, p.b.BinaryVersion, promptPartialsBase)
		}
		if p.a.PromptBytes <= 0 || p.a.PromptBytes > budget || p.a.PromptBytes >= p.b.PromptBytes {
			t.Errorf("[%s] prompt_bytes = %d, [%s] %d; want measured, at most %d, and below before", p.after, p.a.PromptBytes, p.before, p.b.PromptBytes, budget)
		}
		if p.a.PartialFetches != 0 {
			t.Errorf("[%s] partial_fetches = %d; the common path should fetch no partial", p.after, p.a.PartialFetches)
		}
		if p.a.FixtureTree == "" || p.a.FixtureTree != p.b.FixtureTree {
			t.Errorf("[%s] fixture_tree = %q, [%s] %q — the legs did not start from the same tree", p.after, p.a.FixtureTree, p.before, p.b.FixtureTree)
		}
	}
	if v := fc.VerifyAfter; v.Verdict != "pass" || v.Detached {
		t.Errorf("[verify_after] verdict = %q, detached = %v; want an attached pass", v.Verdict, v.Detached)
	}
	if _, err := os.Stat(filepath.Join(dir, "flow-cost-before.toml")); err == nil {
		t.Error("flow-cost-before.toml still exists — t-16 moves its legs into flow-cost.toml and deletes it")
	}
}

// checkMemoryPruneAdvisorFlow pins memory-prune-advisor's c-6 record: the
// after leg ran with the turn cache cold, so its wall-clock includes the
// transcript recount, and both legs measured what dross status printed.
// Turns and wall-clock must be measured but are not compared (rivil,
// 2026-10-08).
func checkMemoryPruneAdvisorFlow(t *testing.T, fc flowCost) {
	t.Helper()
	for _, p := range memoryPruneAdvisorFlowProblems(fc) {
		t.Error(p)
	}
}

// memoryPruneAdvisorFlowProblems is checkMemoryPruneAdvisorFlow's body,
// returning what it would report so a test can feed it a broken record.
func memoryPruneAdvisorFlowProblems(fc flowCost) []string {
	var out []string
	if s, _ := fc.Scenario["turn_cache"].(string); s != "cold" {
		out = append(out, fmt.Sprintf("[scenario] turn_cache = %q; want \"cold\" — a warm cache hides the recount's wall-clock", s))
	}
	for _, l := range []struct {
		name string
		leg  *flowLeg
	}{{"before", fc.Before}, {"after", fc.After}} {
		if l.leg == nil {
			out = append(out, fmt.Sprintf("[%s] is missing", l.name))
			continue
		}
		if l.leg.StatusBytes <= 0 {
			out = append(out, fmt.Sprintf("[%s] status_bytes = %d — the bytes dross status printed were not measured", l.name, l.leg.StatusBytes))
		}
		if l.leg.AgentTurns <= 0 || l.leg.WallClockS <= 0 {
			out = append(out, fmt.Sprintf("[%s] agent_turns = %d, wall_clock_s = %v — recorded, not compared, but must be measured", l.name, l.leg.AgentTurns, l.leg.WallClockS))
		}
	}
	return out
}

// flowCostStringKeys are the keys a memory-prune-advisor flow-cost.toml may
// hold free text under: identities, timestamps, the replay prose
// TestFlowCostRecords requires, and the method. Any other string could be a
// memory's content copied out of a transcript.
var flowCostStringKeys = map[string]bool{
	"binary_version": true, "session_id": true, "measured_at": true, "filter": true, "method": true,
	"repo_shape": true, "wave_position": true, "flow_start": true, "flow_end": true, "jq_method": true,
	"turn_cache": true,
}

var (
	// flowCostDrossCall is one `dross <verb> [<sub>]`, the shape the record's
	// jq filter scans Bash commands for — no arguments, so no paths or text.
	flowCostDrossCall = regexp.MustCompile(`^dross [a-z][a-z-]*( [a-z][a-z-]*)?$`)
	// flowCostTurnTools is one turn's tool names joined by +, or "text".
	flowCostTurnTools = regexp.MustCompile(`^(text|(?:[A-Z][A-Za-z]*|mcp__[A-Za-z0-9_-]+)(?:\+(?:[A-Z][A-Za-z]*|mcp__[A-Za-z0-9_-]+))*)$`)
)

// flowCostContentProblems walks a decoded flow-cost.toml and reports every
// string that is not under an allowlisted key, and every dross_calls or
// turn_tools entry outside its grammar. Numbers, bools and datetimes carry no
// content and pass.
func flowCostContentProblems(doc map[string]any) []string {
	var out []string
	var walk func(path, key string, v any)
	walk = func(path, key string, v any) {
		switch v := v.(type) {
		case map[string]any:
			for _, k := range slices.Sorted(maps.Keys(v)) {
				walk(path+"."+k, k, v[k])
			}
		case []map[string]any:
			for i, m := range v {
				walk(fmt.Sprintf("%s[%d]", path, i), key, m)
			}
		case []any:
			for i, e := range v {
				walk(fmt.Sprintf("%s[%d]", path, i), key, e)
			}
		case string:
			switch {
			case key == "dross_calls":
				if !flowCostDrossCall.MatchString(v) {
					out = append(out, fmt.Sprintf("%s = %q is not a `dross <verb> [<sub>]` call", path, v))
				}
			case key == "turn_tools":
				if !flowCostTurnTools.MatchString(v) {
					out = append(out, fmt.Sprintf("%s = %q is not tool names joined by + (or text)", path, v))
				}
			case !flowCostStringKeys[key]:
				out = append(out, fmt.Sprintf("%s holds a string under a key outside the allowlist", path))
			}
		}
	}
	walk("", "", doc)
	return out
}

// TestFlowCostCopiesNoContent guards memory-prune-advisor's record, the one
// measured over a session that printed memory suggestions: it holds counts,
// tool names and dross verbs, and free text only under allowlisted keys, so
// no memory's content rides into the tracked phase dir.
func TestFlowCostCopiesNoContent(t *testing.T) {
	path := filepath.Join(repoRootFromTest(t), RootDirName, "phases", "memory-prune-advisor", "flow-cost.toml")
	var doc map[string]any
	if _, err := toml.DecodeFile(path, &doc); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	for _, p := range flowCostContentProblems(doc) {
		t.Error(p)
	}
}

// TestFlowCostContentProblemsCatches feeds the content walker records that
// copy text out, so a walker that stopped looking would fail here rather than
// pass the real file vacuously.
func TestFlowCostContentProblemsCatches(t *testing.T) {
	clean := func() map[string]any {
		return map[string]any{
			"scenario": map[string]any{"repo_shape": "x", "turn_cache": "cold", "filter": "def f: .;"},
			"before": map[string]any{
				"binary_version": "dross 0.1.0.0 (commit abc1234)", "session_id": "s", "flow_start": "t",
				"agent_turns": int64(2), "wall_clock_s": 1.5,
				"turn_tools":  []any{"Bash+Read", "text", "mcp__plugin_serena__find_symbol"},
				"dross_calls": []any{"dross status", "dross deferred list"},
			},
		}
	}
	if got := flowCostContentProblems(clean()); len(got) != 0 {
		t.Fatalf("clean record reported %v", got)
	}
	for name, mutate := range map[string]func(map[string]any){
		"string under an unlisted leg key": func(d map[string]any) { d["before"].(map[string]any)["memory_line"] = "- [x](x.md) — hook" },
		"string under an unlisted scenario key": func(d map[string]any) {
			d["scenario"].(map[string]any)["status_output"] = "memory:\n  m-1 x.md delete"
		},
		"string array under an unlisted key": func(d map[string]any) { d["before"].(map[string]any)["suggestions"] = []any{"m-1"} },
		"string in an array of tables":       func(d map[string]any) { d["runs"] = []map[string]any{{"note": "copied"}} },
		"dross call with an argument": func(d map[string]any) {
			d["before"].(map[string]any)["dross_calls"] = []any{"dross memory apply m-1"}
		},
		"dross call that is a command line": func(d map[string]any) {
			d["before"].(map[string]any)["dross_calls"] = []any{"cat MEMORY.md"}
		},
		"turn tool with text": func(d map[string]any) {
			d["before"].(map[string]any)["turn_tools"] = []any{"Read MEMORY.md"}
		},
		"turn tool lower-case word": func(d map[string]any) {
			d["before"].(map[string]any)["turn_tools"] = []any{"Bash+secret"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			d := clean()
			mutate(d)
			if got := flowCostContentProblems(d); len(got) != 1 {
				t.Errorf("reported %d problems, want 1: %v", len(got), got)
			}
		})
	}
}

// TestMemoryPruneAdvisorFlowProblems breaks the c-6 record one field at a
// time; each break must be reported, and a sound record must report nothing.
func TestMemoryPruneAdvisorFlowProblems(t *testing.T) {
	sound := func() flowCost {
		return flowCost{
			Scenario: map[string]any{"turn_cache": "cold"},
			Before:   &flowLeg{AgentTurns: 4, WallClockS: 11.4, StatusBytes: 820},
			After:    &flowLeg{AgentTurns: 2, WallClockS: 15.5, StatusBytes: 1796},
		}
	}
	if got := memoryPruneAdvisorFlowProblems(sound()); len(got) != 0 {
		t.Fatalf("sound record reported %v", got)
	}
	for name, mutate := range map[string]func(*flowCost){
		"before status_bytes zero": func(fc *flowCost) { fc.Before.StatusBytes = 0 },
		"after status_bytes zero":  func(fc *flowCost) { fc.After.StatusBytes = 0 },
		"after status_bytes below": func(fc *flowCost) { fc.After.StatusBytes = -1 },
		"turn_cache missing":       func(fc *flowCost) { delete(fc.Scenario, "turn_cache") },
		"turn_cache warm":          func(fc *flowCost) { fc.Scenario["turn_cache"] = "warm" },
		"before agent_turns zero":  func(fc *flowCost) { fc.Before.AgentTurns = 0 },
		"after wall_clock_s zero":  func(fc *flowCost) { fc.After.WallClockS = 0 },
		"after leg missing":        func(fc *flowCost) { fc.After = nil },
	} {
		t.Run(name, func(t *testing.T) {
			fc := sound()
			mutate(&fc)
			if got := memoryPruneAdvisorFlowProblems(fc); len(got) != 1 {
				t.Errorf("reported %d problems, want 1: %v", len(got), got)
			}
		})
	}
	// Turns and wall-clock are recorded, not compared: an after leg slower
	// and with more turns than before is still a sound record.
	fc := sound()
	fc.After.AgentTurns, fc.After.WallClockS = 9, 99
	if got := memoryPruneAdvisorFlowProblems(fc); len(got) != 0 {
		t.Errorf("a slower after leg reported %v; turns and wall-clock are not compared", got)
	}
}

// TestReadmeDocumentsMemoryAdvisor is the README grep guard for the advisor:
// the memory command row names the apply verb and its confirmation flag, and
// the status row describes its memory: section.
func TestReadmeDocumentsMemoryAdvisor(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRootFromTest(t), "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	var memoryRow, statusRow string
	for _, l := range strings.Split(string(b), "\n") {
		switch {
		case strings.HasPrefix(l, "| `dross memory "):
			memoryRow = l
		case strings.HasPrefix(l, "| `dross status` |"):
			statusRow = l
		}
	}
	if memoryRow == "" {
		t.Fatal("README has no `dross memory` row in the command table")
	}
	for _, want := range []string{"dross memory apply", "--yes"} {
		if !strings.Contains(memoryRow, want) {
			t.Errorf("README's `dross memory` row does not mention %q", want)
		}
	}
	if !strings.Contains(statusRow, "`memory:`") {
		t.Error("README's `dross status` row does not describe its `memory:` section")
	}
}

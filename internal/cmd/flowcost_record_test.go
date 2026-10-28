package cmd

import (
	"os"
	"path/filepath"
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
// the base prompts' bytes, the budget and the zero-fetch common path.
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
			}
		})
	}
	for _, want := range []string{"context-boundaries", "test-output-summaries", "prompt-partials"} {
		if !found[want] {
			t.Errorf("%s has no flow-cost.toml — the c-8 record is missing", want)
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

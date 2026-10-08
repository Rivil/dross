package cmd

import (
	"path/filepath"
	"testing"

	"github.com/BurntSushi/toml"
)

// TestHookLatencyRecord guards context-boundaries' c-7 measurement: the
// installed `dross hooks nudge` was fired at least 50 times on each path —
// below the threshold, band already claimed, and emitting — against a 50 MB
// transcript, by a named binary, and every path's p95 kept the 50 ms budget.
// The record is required (no vacuous pass). [hook_latency.stress] — a 10 MiB
// tool_response payload — is observed only: c-7 scopes the budget to the
// transcript, so it is read for presence and never compared against 50 ms.
func TestHookLatencyRecord(t *testing.T) {
	path := filepath.Join(repoRootFromTest(t), RootDirName, "phases", "context-boundaries", "flow-cost.toml")
	type pathTiming struct {
		P50 float64 `toml:"p50_ms"`
		P95 float64 `toml:"p95_ms"`
	}
	var rec struct {
		HookLatency *struct {
			Fires         int         `toml:"fires"`
			TranscriptMB  float64     `toml:"transcript_mb"`
			BinaryVersion string      `toml:"binary_version"`
			Below         *pathTiming `toml:"below"`
			Claimed       *pathTiming `toml:"claimed"`
			Emit          *pathTiming `toml:"emit"`
			Stress        *pathTiming `toml:"stress"`
		} `toml:"hook_latency"`
	}
	if _, err := toml.DecodeFile(path, &rec); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	h := rec.HookLatency
	if h == nil {
		t.Fatalf("%s has no [hook_latency] section — the c-7 measurement is missing", path)
	}
	if h.Fires < 50 {
		t.Errorf("fires = %d, want ≥ 50", h.Fires)
	}
	if h.TranscriptMB < 50 {
		t.Errorf("transcript_mb = %v, want ≥ 50", h.TranscriptMB)
	}
	if h.BinaryVersion == "" {
		t.Error("binary_version is empty — the record does not say which build was measured")
	}
	for name, p := range map[string]*pathTiming{"below": h.Below, "claimed": h.Claimed, "emit": h.Emit} {
		switch {
		case p == nil:
			t.Errorf("[hook_latency.%s] is missing", name)
		case p.P95 <= 0:
			t.Errorf("[hook_latency.%s] p95_ms = %v — not a measurement", name, p.P95)
		case p.P95 > 50:
			t.Errorf("[hook_latency.%s] p95_ms = %v, over the 50 ms budget", name, p.P95)
		}
	}
	if h.Stress == nil || h.Stress.P95 <= 0 {
		t.Error("[hook_latency.stress] is missing — the large-payload run is recorded even though it is not gated")
	}
}

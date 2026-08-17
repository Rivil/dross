package verify

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSummaryRecordsMeasuredOn: a remote score and a local one are not
// interchangeable evidence — different toolchain versions, different core
// counts — so the record has to say which machine produced it. Skeleton is the
// carrier: it is the only place the run's provenance meets the file a human
// later reads.
func TestSummaryRecordsMeasuredOn(t *testing.T) {
	for _, tc := range []struct {
		name string
		on   string
		want string
	}{
		{"a remote run names the host", MeasuredOnHost("helicon"), "helicon"},
		{"a local run says local", MeasuredLocally(), "local"},
		{"a blank host degrades to local", MeasuredOnHost("  "), "local"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := Skeleton(&Tests{Phase: "p", MeasuredOn: tc.on}, []string{"c-1"})
			if v.Summary.MeasuredOn != tc.want {
				t.Errorf("measured_on = %q, want %q", v.Summary.MeasuredOn, tc.want)
			}
		})
	}
}

// TestSummaryRecordsFallback: a run that MEANT to use a host and could not
// reach it is not a plain local run. Recording it as one loses the fact that a
// remote measurement was expected and did not happen — the state
// board-task-mirror hit, where revoking the grant was the workaround and
// nothing in the numbers said they came from a different machine.
func TestSummaryRecordsFallback(t *testing.T) {
	on := MeasuredAfterFallback("helicon", "ssh: connect: no route to host")

	v := Skeleton(&Tests{Phase: "p", MeasuredOn: on}, []string{"c-1"})

	got := v.Summary.MeasuredOn
	if !strings.Contains(got, "helicon") {
		t.Errorf("measured_on = %q — a fallback must still name the host it could not reach", got)
	}
	if !strings.Contains(got, "local") {
		t.Errorf("measured_on = %q — a fallback must say the numbers were measured locally", got)
	}
	if !strings.Contains(got, "no route to host") {
		t.Errorf("measured_on = %q — the reason is what tells a reader whether to retry", got)
	}
	// A fallback must never be mistakable for an ordinary remote run.
	if got == "helicon" {
		t.Error("a fallback recorded as a clean remote measurement")
	}
	if blank := MeasuredAfterFallback("", "whatever"); blank != "local" {
		t.Errorf("a fallback with no host = %q, want plain local — there was no host to name", blank)
	}
}

// TestMeasuredOnComesFromTheRun: the field answers "where did these numbers
// come from", which is not the question a grant on disk answers. Skeleton must
// copy what the run recorded and never substitute anything.
func TestMeasuredOnComesFromTheRun(t *testing.T) {
	v := Skeleton(&Tests{Phase: "p", MeasuredOn: MeasuredLocally()}, []string{"c-1"})
	if v.Summary.MeasuredOn != "local" {
		t.Errorf("measured_on = %q, want local — Skeleton substituted its own answer", v.Summary.MeasuredOn)
	}
	// And a run that recorded nothing gets nothing invented for it.
	blank := Skeleton(&Tests{Phase: "p"}, []string{"c-1"})
	if blank.Summary.MeasuredOn != "" {
		t.Errorf("measured_on = %q on a run that recorded none, want empty", blank.Summary.MeasuredOn)
	}
}

// TestLegacyVerifyTomlLoads: the field is additive. A verify.toml written
// before it existed must load and re-save without gaining an invented
// provenance — a record cannot be retro-labelled with a machine nobody knows.
func TestLegacyVerifyTomlLoads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, VerifyFile)
	legacy := "[verify]\n" +
		"  phase = \"old-phase\"\n" +
		"  verdict = \"pass\"\n\n" +
		"[summary]\n" +
		"  mutation_status = \"measured\"\n" +
		"  mutation_score = 0.95\n" +
		"  mutants_in_scope = 100\n"
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	v, err := LoadVerify(path)
	if err != nil {
		t.Fatalf("a pre-field verify.toml no longer loads: %v", err)
	}
	if v.Summary.MeasuredOn != "" {
		t.Errorf("measured_on = %q on a legacy record, want empty", v.Summary.MeasuredOn)
	}
	if v.Summary.MutationScore != 0.95 {
		t.Errorf("the legacy score did not survive the load: %v", v.Summary.MutationScore)
	}

	// Re-saving must not emit the field at all — omitempty is what keeps a
	// round-trip from inventing one.
	out := filepath.Join(dir, "again.toml")
	if err := v.Save(out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "measured_on") {
		t.Errorf("re-saving a legacy record invented a measured_on line:\n%s", b)
	}
}

// TestMeasuredOnRoundTripsThroughTests: the value travels tests.json → Skeleton
// → verify.toml. A field that only survived in memory would be gone by the time
// anyone compared two runs.
func TestMeasuredOnRoundTripsThroughTests(t *testing.T) {
	dir := t.TempDir()
	testsPath := filepath.Join(dir, TestsFile)
	tests := &Tests{Phase: "p", GeneratedAt: time.Now().UTC(), MeasuredOn: MeasuredOnHost("helicon")}
	if err := tests.Save(testsPath); err != nil {
		t.Fatal(err)
	}

	reloaded, err := LoadTests(testsPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.MeasuredOn != "helicon" {
		t.Fatalf("tests.json lost the provenance: %q", reloaded.MeasuredOn)
	}

	verifyPath := filepath.Join(dir, VerifyFile)
	if err := Skeleton(reloaded, []string{"c-1"}).Save(verifyPath); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(verifyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "measured_on = \"helicon\"") {
		t.Errorf("verify.toml does not carry the provenance:\n%s", b)
	}
}

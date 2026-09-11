package mutation

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/telemetry"
)

// canary is a token no adapter, tool or dross message would ever produce, so
// finding it in an error means it travelled there from the tool's output.
const canary = "CANARY-TOOLFAIL-9f2a"

// TestHeadBufferCountsEveryByteNotTheRetainedPrefix is the difference between
// `observed` and buf.Len(). A record built from the retained prefix saturates
// at the cap and reports a run that printed 100 KiB as having printed 64 KiB.
func TestHeadBufferCountsEveryByteNotTheRetainedPrefix(t *testing.T) {
	const limit = 64 << 10
	const fed = 100 << 10

	h := &headBuffer{limit: limit}
	if _, err := h.Write([]byte(strings.Repeat("x", fed))); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if got := h.observed(); got != fed {
		t.Errorf("observed() = %d, want %d — the counter must total every byte written, not the retained prefix", got, fed)
	}
	if h.buf.Len() != limit {
		t.Fatalf("precondition: buf.Len() = %d, want the saturated %d", h.buf.Len(), limit)
	}

	// The failing arm, written down: a record built from the retained prefix
	// disagrees with a record built from the counter, and it is the prefix
	// that is wrong.
	fromCounter := RecordToolFailure("stryker", 1, Observed(h.observed())).Error()
	fromPrefix := RecordToolFailure("stryker", 1, Observed(h.buf.Len())).Error()
	if fromCounter == fromPrefix {
		t.Error("a record built from buf.Len() reads the same as one built from observed() — the cap is not being exercised")
	}
	if !strings.Contains(fromCounter, fmt.Sprint(fed)) {
		t.Errorf("record does not report %d bytes: %q", fed, fromCounter)
	}
	if strings.Contains(fromCounter, fmt.Sprint(limit)) {
		t.Errorf("record reports the saturated %d rather than the %d observed: %q", limit, fed, fromCounter)
	}
}

// TestRecordToolFailureShape is the structural guarantee. Tool output can only
// re-enter the record through a new parameter carrying text or bytes, or
// through an exported field a caller could set after construction. Both are
// pinned here rather than left to review.
func TestRecordToolFailureShape(t *testing.T) {
	ft := reflect.TypeOf(RecordToolFailure)
	want := []reflect.Kind{reflect.String, reflect.Int, reflect.Struct}
	if ft.NumIn() != len(want) {
		t.Fatalf("RecordToolFailure takes %d parameters, want exactly %d (tool, exit, capture) — a new parameter is how captured output would re-enter", ft.NumIn(), len(want))
	}
	for i, k := range want {
		if got := ft.In(i).Kind(); got != k {
			t.Errorf("parameter %d is %s, want %s", i, got, k)
		}
	}
	if ft.In(0) != reflect.TypeOf("") || ft.In(1) != reflect.TypeOf(0) {
		t.Errorf("parameter list is (%s, %s, %s), want (string, int, capture)", ft.In(0), ft.In(1), ft.In(2))
	}
	if got, want := ft.In(2), reflect.TypeOf(capture{}); got != want {
		t.Errorf("third parameter is %s, want %s", got, want)
	}

	rt := reflect.TypeOf(RecordToolFailure("stryker", 1, Observed(0))).Elem()
	for i := 0; i < rt.NumField(); i++ {
		if f := rt.Field(i); f.IsExported() {
			t.Errorf("record field %s is exported — a caller could assign tool text to it after construction", f.Name)
		}
	}
	if rt.NumField() == 0 {
		t.Error("record has no fields at all; the no-exported-field check would pass vacuously")
	}
}

// TestRecordCarriesNoToolOutput feeds a canary through the head buffer at every
// truncation length and asserts it reaches the error at none of them.
func TestRecordCarriesNoToolOutput(t *testing.T) {
	for _, lines := range []int{0, 1, 5, 40, 1000} {
		t.Run(fmt.Sprintf("lines=%d", lines), func(t *testing.T) {
			h := &headBuffer{limit: 64 << 10}
			for i := 0; i < 200; i++ {
				fmt.Fprintf(h, "%s line %d\n", canary, i)
			}
			// The head is rendered — to a writer, where it is allowed to go.
			var live strings.Builder
			h.printHead(&live, "stryker", lines)
			if lines > 0 && !strings.Contains(live.String(), canary) {
				t.Fatal("precondition: the canary never reached the head buffer, so the assertion below is vacuous")
			}

			err := RecordToolFailure("stryker", 3, Observed(h.observed())).Error()
			if strings.Contains(err, canary) {
				t.Errorf("tool output reached the record: %q", err)
			}
		})
	}
}

// TestRecordErrorClauses pins every clause the record must carry. Each arm
// fails on a rewording that drops one, which is the only way a downstream
// reader silently loses the fact.
func TestRecordErrorClauses(t *testing.T) {
	got := RecordToolFailure("stryker", 3, Observed(1234)).Error()

	if !strings.Contains(got, "status 3") {
		t.Errorf("record drops the exit status: %q", got)
	}
	if !strings.Contains(got, "1234") {
		t.Errorf("record drops the capture clause: %q", got)
	}
	if !strings.Contains(got, "stderr") {
		t.Errorf("record drops the stderr-pointer sentence: %q", got)
	}
	// Fixed prose, no path: dross cannot know where its own stderr was
	// redirected, so a recorded path would often be wrong (spec decision
	// stderr_pointer).
	if strings.Contains(got, "/") {
		t.Errorf("record names a path; the stderr pointer is fixed prose: %q", got)
	}
	// OBSERVED, not retained. c-1 and c-2 word this as `captured`; the tee is
	// capped at 64 KiB, so reporting the retained figure would understate the
	// output and make verify.toml read as disagreeing with the criterion.
	if !strings.Contains(got, "observed") {
		t.Errorf("the byte count is not labelled as observed: %q", got)
	}
}

// TestNotCapturedCarriesNoByteCount is the detached-run shape. `0 bytes` is a
// claim that the tool was silent; NotCaptured says nothing was watched.
func TestNotCapturedCarriesNoByteCount(t *testing.T) {
	const why = "this run was detached"
	got := RecordToolFailure("gremlins", 1, NotCaptured(why)).Error()

	if !strings.Contains(got, why) {
		t.Errorf("record does not say why nothing was captured: %q", got)
	}
	if strings.Contains(got, "bytes") || strings.Contains(got, "0 ") {
		t.Errorf("record reports a byte count for a run that captured nothing: %q", got)
	}
	if zero := RecordToolFailure("gremlins", 1, Observed(0)).Error(); zero == got {
		t.Error("Observed(0) renders identically to NotCaptured — the distinction is not being carried")
	}
}

// TestRecordStaysInTheMutationTelemetryBucket runs the real classifier. The
// mutation bucket keys on the tool name as a bare substring, so a rewording
// that drops it pushes every adapter failure into `other`, which is the bucket
// that hides what is wrong.
func TestRecordStaysInTheMutationTelemetryBucket(t *testing.T) {
	for _, tool := range []string{"stryker", "gremlins"} {
		err := error(RecordToolFailure(tool, 2, Observed(10)))
		if got := telemetry.ClassifyError(err); got != "mutation" {
			t.Errorf("%s failure classified as %q, want %q — the tool name is no longer a bare token in the message: %q", tool, got, "mutation", err.Error())
		}
	}
}

// TestRecordPreservesWrappedIdentity. verify.go and cmd/test.go classify a leg
// off the LIVE error value (errors.Is against remote.ErrTransport and
// remote.ErrRemoteCommand); replacing the message must not replace the identity.
func TestRecordPreservesWrappedIdentity(t *testing.T) {
	sentinel := errors.New("some transport sentinel")
	rec := RecordToolFailure("stryker", 1, Observed(4)).wrapping(fmt.Errorf("ssh: %w", sentinel))

	if !errors.Is(rec, sentinel) {
		t.Error("errors.Is does not see through the record — remote classification would silently stop firing")
	}
	if strings.Contains(rec.Error(), "ssh") {
		t.Errorf("the wrapped error's text leaked into the record's message: %q", rec.Error())
	}
	if bare := RecordToolFailure("stryker", 1, Observed(4)); errors.Is(bare, sentinel) {
		t.Error("an unwrapped record matches the sentinel; the assertion above is vacuous")
	}
}

// TestRecordStartFailure. Without an *exec.ExitError there is no status, and
// "-1" reads as a status the tool chose.
func TestRecordStartFailure(t *testing.T) {
	got := RecordToolFailure("stryker", exitDidNotStart, NotCaptured("the tool never started")).Error()

	if !strings.Contains(got, "did not start") {
		t.Errorf("a start failure does not say so: %q", got)
	}
	if strings.Contains(got, "-1") {
		t.Errorf("a start failure reports exit status -1: %q", got)
	}
}

// TestRecordRealExitStatusOne is the counterpart TestRecordStartFailure cannot
// be. That test passes exitDidNotStart as the very value it asserts on, so a
// sentinel of 1 still reads "did not start" and the test still passes — while
// every tool that really exited 1 would be reported as never having started.
// A LITERAL 1 here is the only thing that tells the two apart.
func TestRecordRealExitStatusOne(t *testing.T) {
	got := RecordToolFailure("stryker", 1, Observed(4)).Error()

	if !strings.Contains(got, "exit status 1") {
		t.Errorf("a real exit status of 1 is not reported as one: %q", got)
	}
	if strings.Contains(got, "did not start") {
		t.Errorf("exit status 1 collides with the did-not-start sentinel: %q", got)
	}
}

// TestRecordLegErrorCarriesTheText is the recorder's only package-local test
// of RecordLegError. Its nil-guard has one caller in internal/verify, which a
// per-package mutation run cannot see; without this, inverting the guard
// survives in this package while the carrier goes empty for every real error.
func TestRecordLegErrorCarriesTheText(t *testing.T) {
	const text = "gremlins failed with exit status 2"
	if got := RecordLegError(errors.New(text)).String(); got != text {
		t.Errorf("RecordLegError(%q).String() = %q — the carrier dropped the error's text", text, got)
	}
	if got := RecordLegError(nil).String(); got != "" {
		t.Errorf("RecordLegError(nil).String() = %q, want empty", got)
	}
}

// TestPrintHeadMatchesTheQuoteItReplaced is the byte-for-byte golden. The head
// is the live diagnostic c-3 promises to keep unchanged, so its rendering is
// pinned against exactly what quote() emitted before it was parameterised.
func TestPrintHeadMatchesTheQuoteItReplaced(t *testing.T) {
	feed := func() *headBuffer {
		h := &headBuffer{limit: 64 << 10}
		fmt.Fprint(h, "first line\nsecond line\nthird line\n")
		return h
	}

	t.Run("untruncated", func(t *testing.T) {
		var b strings.Builder
		feed().printHead(&b, "stryker", 40)
		const want = "the head of stryker's output, which is where the cause is:\n\n" +
			"    first line\n    second line\n    third line\n"
		if b.String() != want {
			t.Errorf("printHead wrote\n%q\nwant\n%q", b.String(), want)
		}
	})

	t.Run("truncated", func(t *testing.T) {
		var b strings.Builder
		feed().printHead(&b, "stryker", 2)
		const want = "the head of stryker's output, which is where the cause is:\n\n" +
			"    first line\n    second line\n" +
			"    … (output continues above)\n"
		if b.String() != want {
			t.Errorf("printHead dropped the continuation tail; wrote\n%q\nwant\n%q", b.String(), want)
		}
	})

	t.Run("silent", func(t *testing.T) {
		var b strings.Builder
		(&headBuffer{limit: 1 << 10}).printHead(&b, "stryker", 40)
		const want = "stryker produced no output at all — it may not have started."
		if b.String() != want {
			t.Errorf("printHead wrote %q, want %q", b.String(), want)
		}
	})

	// Tool-parameterised: t-4 routes gremlins and stryker.net through the same
	// helper, and each must get its own name in the banner, not stryker's.
	t.Run("othertool", func(t *testing.T) {
		for _, tool := range []string{"gremlins", "stryker.net"} {
			var stryker, other strings.Builder
			feed().printHead(&stryker, "stryker", 40)
			feed().printHead(&other, tool, 40)

			want := strings.ReplaceAll(stryker.String(), "stryker", tool)
			if other.String() != want {
				t.Errorf("printHead for %s wrote\n%q\nwant the identical shape\n%q", tool, other.String(), want)
			}
			if strings.Contains(other.String(), "stryker's output") {
				t.Errorf("printHead for %s still names stryker: %q", tool, other.String())
			}
		}
	})
}

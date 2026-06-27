package security

import (
	"strings"
	"testing"
)

// TestDockleNoImageSkipsWithReason pins the never-silent-all-clear contract: when
// dockle is installed but no image is supplied, the decision is Skipped with a
// non-empty Reason — not a quiet pass and not a run.
func TestDockleNoImageSkipsWithReason(t *testing.T) {
	d := DecideDockle("", true)
	if d.Run {
		t.Fatal("no image supplied: dockle must not run")
	}
	if !d.Skipped {
		t.Fatal("no image supplied: decision must be Skipped, never a silent all-clear")
	}
	if strings.TrimSpace(d.Reason) == "" {
		t.Fatal("a skipped dockle must carry a non-empty reason")
	}
	if d.Reason != dockleNoImageReason {
		t.Fatalf("no-image reason = %q, want %q", d.Reason, dockleNoImageReason)
	}
}

// TestDockleNeverBuilds pins that a supplied-image plan targets the exact ref and
// never shells out to build it — no argv element (nor the joined command) may contain
// "build".
func TestDockleNeverBuilds(t *testing.T) {
	const image = "registry.example.com/app:1.2.3"
	d := DecideDockle(image, true)
	if !d.Run {
		t.Fatal("installed + image supplied: dockle must run")
	}
	if d.Skipped {
		t.Fatal("a runnable dockle must not be marked Skipped")
	}
	joined := strings.Join(d.Args, " ")
	if !strings.Contains(joined, image) {
		t.Fatalf("dockle args %v must reference the supplied image %q", d.Args, image)
	}
	if strings.Contains(strings.ToLower(joined), "build") {
		t.Fatalf("dockle args %v must never contain a build step — dross does not build images", d.Args)
	}
}

// TestDockleMissingBinHint pins that a missing dockle binary is reported with the
// install hint, distinct from the no-image skip — even when no image is supplied, the
// binary gap is surfaced first, never masked as a no-image skip.
func TestDockleMissingBinHint(t *testing.T) {
	for _, image := range []string{"", "registry.example.com/app:1.2.3"} {
		d := DecideDockle(image, false)
		if d.Run {
			t.Fatalf("image=%q: a missing dockle binary must not run", image)
		}
		if !d.Skipped {
			t.Fatalf("image=%q: a missing dockle binary must be Skipped", image)
		}
		if strings.TrimSpace(d.Install) == "" {
			t.Fatalf("image=%q: a missing dockle binary must carry an install hint", image)
		}
		if d.Reason == dockleNoImageReason {
			t.Fatalf("image=%q: a missing binary must not be reported as a no-image skip", image)
		}
	}
}

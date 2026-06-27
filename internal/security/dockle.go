package security

import "strings"

// dockle is a container image-layer (CIS benchmark) scanner. Unlike the static-source
// scanners in the catalog, it inspects a BUILT image — which dross deliberately never
// builds itself (no docker daemon dependency, no side effects). So running dockle is a
// three-state decision: run it (an image ref was supplied and the binary is present),
// or skip it with a reason — either because no image was supplied or because the
// binary is missing. A skip is never a silent all-clear; it always carries a reason a
// user can act on.

// DockleBin is the executable looked up on PATH to detect dockle's availability.
const DockleBin = "dockle"

// DockleInstall is the hint shown when the dockle binary is missing.
const DockleInstall = "brew install goodwithtech/r/dockle  (or see github.com/goodwithtech/dockle)"

// dockleNoImageReason is the skip reason when dockle is installed but no image was
// supplied. It names the two ways forward and is deliberately distinct from the
// missing-binary reason so the two situations never blur together.
const dockleNoImageReason = "dockle needs a built image; run docker build or supply --image <ref>"

// dockleMissingReason is the skip reason when the dockle binary is not on PATH.
const dockleMissingReason = "dockle is not installed"

// DockleDecision is the outcome of deciding whether and how to run dockle for a scan.
type DockleDecision struct {
	Run     bool     // true only when dockle is installed AND an image ref was supplied
	Args    []string // the dockle argv (image ref included); empty when skipped
	Skipped bool     // true when dockle will not run this scan
	Reason  string   // why it was skipped (non-empty whenever Skipped); empty when Run
	Install string   // install hint, set only when skipped because the binary is missing
}

// DecideDockle decides whether dockle runs for the given image ref. installed reports
// whether the dockle binary is on PATH (inject in tests; the CLI passes the real PATH
// lookup). dross NEVER builds an image: a blank image is skipped with a reason, never
// a `docker build`. A missing binary takes precedence — it is reported with the
// install hint, not as a no-image skip — because that is the first gap to close.
// When installed and an image ref is supplied, the returned Args target that exact
// ref and contain no build step.
func DecideDockle(image string, installed bool) DockleDecision {
	image = strings.TrimSpace(image)
	if !installed {
		return DockleDecision{
			Skipped: true,
			Reason:  dockleMissingReason,
			Install: DockleInstall,
		}
	}
	if image == "" {
		return DockleDecision{
			Skipped: true,
			Reason:  dockleNoImageReason,
		}
	}
	return DockleDecision{
		Run:  true,
		Args: []string{"--exit-code", "1", image},
	}
}

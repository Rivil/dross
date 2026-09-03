package cmd

// `dross test lane preview --json`.
//
// A bare document carrying every fact the human rendering shows, in the SAME
// vocabulary: the consent state is the string ConsentState.String() produced,
// and the locality is the string that was printed. Two vocabularies for one
// answer is the failure this guards against — a payload emitting "ok" where the
// transcript said "granted" would make the two renderings describe different
// runs, and only one of them would be checkable.
//
// It serializes previewReport rather than re-walking the plan, for the reason
// preview reads lanePlan rather than deriving its own line: a second traversal
// is a second answer.

import (
	"github.com/spf13/cobra"
)

// previewJSONFlagUsage is registered instead of jsonFlagUsage, whose text
// promises "the bare JSON document instead of toml". Preview has no toml
// rendering to be instead of — its other form is a transcript — and a usage
// line naming a format the command cannot emit is a small lie in --help.
const previewJSONFlagUsage = "emit the preview as a bare JSON document instead of the transcript"

// addPreviewJSONFlag registers --json on the preview command.
func addPreviewJSONFlag(c *cobra.Command, dst *bool) {
	c.Flags().BoolVar(dst, "json", false, previewJSONFlagUsage)
}

// emitPreviewJSON writes the report as the bare document.
//
// The slices are normalized to empty first. encoding/json renders a nil slice
// as `null`, and a consumer asking "which lanes would run" must be able to
// range over `lanes` without a null check — an unmatched-only preview is an
// EMPTY answer, not an absent one, and the difference is exactly what locked
// preview_exit_status says preview is for.
func emitPreviewJSON(r previewReport) error {
	if r.Lanes == nil {
		r.Lanes = []previewLaneReport{}
	}
	if r.Unmatched == nil {
		r.Unmatched = []string{}
	}
	if r.OutOfTree == nil {
		r.OutOfTree = []string{}
	}
	return emitJSON(r)
}

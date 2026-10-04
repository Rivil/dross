package project

// patch_save.go opens the lossless write door to the rest of dross. A TOML
// store other than project.toml (plan.toml, survivors.toml) that a human
// annotates by hand gets the guarantee project.toml has: a write changes the
// lines its differing fields occupy and leaves every other byte — comments,
// trailing markers, hand-wrapped arrays — where it was.

// doorOps is the diff step SaveTOML runs: the door's own seam, as a variable so
// a test can hand it an op the patcher renders wrongly and prove the verify
// step refuses it. Project.Save keeps its own seam, planOps.
var doorOps = func(old, new any) ([]op, error) { return diff(old, new) }

// SaveTOML writes v — a non-nil pointer to a TOML-tagged struct — to path
// through the lossless patcher.
//
// An absent path gets a fresh encode. An existing file is decoded into v's
// type, diffed against v, patched in place, and written only after the patched
// text is shown to load as v; on any failure the file is left untouched. A
// save that changes nothing writes nothing.
func SaveTOML(path string, v any) error {
	return saveLossless(path, v, doorOps)
}

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

// keyedOps is the diff step SaveTOML runs when it is given ArrayKeys — the
// keyed door's seam, a variable for the same reason doorOps is.
var keyedOps = func(old, new any, keys map[string]string) ([]op, error) { return diffKeyed(old, new, keys) }

// ArrayKey names the field that identifies the elements of one
// array-of-tables: Path is its dotted TOML path ("task"), Field the key inside
// each element ("id").
type ArrayKey struct {
	Path, Field string
}

// SaveTOML writes v — a non-nil pointer to a TOML-tagged struct — to path
// through the lossless patcher.
//
// An absent path gets a fresh encode. An existing file is decoded into v's
// type, diffed against v, patched in place, and written only after the patched
// text is shown to load as v; on any failure the file is left untouched. A
// save that changes nothing writes nothing.
//
// keys, optional, make arrays-of-tables match their elements by an identity
// field instead of by position: a reorder moves a block verbatim (its leading
// comments with it), an insertion lands at its index, and a removal takes only
// its own block. With no keys every array keeps positional matching.
func SaveTOML(path string, v any, keys ...ArrayKey) error {
	if len(keys) == 0 {
		return saveLossless(path, v, doorOps)
	}
	byPath := make(map[string]string, len(keys))
	for _, k := range keys {
		byPath[k.Path] = k.Field
	}
	return saveLossless(path, v, func(old, new any) ([]op, error) { return keyedOps(old, new, byPath) })
}

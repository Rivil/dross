package project

// patch_diff.go turns two documents — two Projects, or two values of any
// other TOML-tagged struct type — into the smallest list of patcher ops that
// carries the on-disk document from one to the other. Both sides are
// first rendered by the encoder and decoded back into generic trees, so
// omitempty, `toml:"-"` and every other tag semantic come from the one
// encoder the package has — the differ never interprets a struct tag itself.

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// encodeCanonical is the door the differ walks through to the package's
// single encoder: the canonical text of p, from which the generic tree — and
// the key order the encoder chose — is read back.
func encodeCanonical(p any) ([]byte, error) {
	return encodeFresh(p)
}

// tree is a document as the decoder sees the encoder's output: tables are
// map[string]any, arrays-of-tables are []map[string]any, everything else is
// a leaf. order records each table's child keys in document order so ops
// for a table come out in the order the encoder would have written them.
type tree struct {
	root  map[string]any
	order map[string][]string
}

func toTree(v any) (*tree, error) {
	src, err := encodeCanonical(v)
	if err != nil {
		return nil, fmt.Errorf("encode: %w", err)
	}
	t := &tree{order: map[string][]string{}}
	if _, err := toml.Decode(string(src), &t.root); err != nil {
		return nil, fmt.Errorf("decode canonical form: %w", err)
	}
	// Key order comes from the patcher's own index of the canonical text,
	// not MetaData.Keys(): that slice aliases its backing arrays and repeats
	// the last key of a table in place of its siblings.
	d, err := indexDoc(src)
	if err != nil {
		return nil, fmt.Errorf("index canonical form: %w", err)
	}
	for _, ln := range d.lines {
		var parent []string
		var name string
		switch ln.kind {
		case lineHeader:
			names := segNames(ln.hdr.path)
			parent, name = names[:len(names)-1], names[len(names)-1]
		case lineKey:
			parent, name = segNames(ln.key.table), ln.key.key
		default:
			continue
		}
		if k := orderKey(parent); !slices.Contains(t.order[k], name) {
			t.order[k] = append(t.order[k], name)
		}
	}
	return t, nil
}

func orderKey(path []string) string { return strings.Join(path, "\x00") }

type nodeKind uint8

const (
	kindLeaf nodeKind = iota
	kindTable
	kindArrayOfTables
)

// classify names the shape of a decoded node. A shape it does not know is an
// error carrying the path, never a silent skip: a new field of a new shape
// must fail the differ the day it lands rather than be dropped from writes.
func classify(path []string, v any) (nodeKind, error) {
	switch v.(type) {
	case map[string]any:
		return kindTable, nil
	case []map[string]any:
		return kindArrayOfTables, nil
	case string, bool, int64, float64, []any:
		return kindLeaf, nil
	}
	if rv := reflect.ValueOf(v); rv.IsValid() && rv.Kind() == reflect.Struct {
		// time.Time and the like decode as structs; they render as scalars.
		return kindLeaf, nil
	}
	return 0, fmt.Errorf("%s: cannot classify a %T", strings.Join(path, "."), v)
}

type differ struct {
	old, new *tree
	ops      []op
	// keys names the identity field of each keyed array-of-tables, by dotted
	// path (task → id). A keyed array matches elements by that field; every
	// other array keeps the positional / deep-equal matching.
	idFields map[string]string
}

// diff returns the ops that take old to new, or an error naming the first
// path whose shape it cannot express. Zero ops means the documents agree.
// old and new are values of the same TOML-tagged struct type.
func diff(old, new any) ([]op, error) {
	return diffKeyed(old, new, nil)
}

// diffKeyed is diff with keyed arrays-of-tables: keys maps an array's dotted
// path to the field that identifies its elements.
func diffKeyed(old, new any, keys map[string]string) ([]op, error) {
	d := &differ{idFields: keys}
	var err error
	if d.old, err = toTree(old); err != nil {
		return nil, fmt.Errorf("diff old: %w", err)
	}
	if d.new, err = toTree(new); err != nil {
		return nil, fmt.Errorf("diff new: %w", err)
	}
	if err := d.table(nil, -1, d.old.root, d.new.root); err != nil {
		return nil, err
	}
	return d.ops, nil
}

// keys lists the children of a table in the encoder's order for the new
// side, then any old-only keys sorted, so deletes follow inserts and both
// are deterministic.
func (d *differ) keys(path []string, old, new map[string]any) []string {
	var out []string
	for _, k := range d.new.order[orderKey(path)] {
		if _, ok := new[k]; ok && !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	// Keys the order map did not see (an empty table decodes with no
	// children listed) and keys only the old side has.
	var rest []string
	for k := range new {
		if !slices.Contains(out, k) {
			rest = append(rest, k)
		}
	}
	for k := range old {
		if !slices.Contains(out, k) && !slices.Contains(rest, k) {
			rest = append(rest, k)
		}
	}
	slices.Sort(rest)
	return append(out, rest...)
}

// table diffs two tables key by key. elem is the array element the table is,
// when it is one; a table nested INSIDE such an element has no op spelling
// and is refused by path.
func (d *differ) table(path []string, elem int, old, new map[string]any) error {
	for _, k := range d.keys(path, old, new) {
		ov, oOK := old[k]
		nv, nOK := new[k]
		child := append(append([]string{}, path...), k)
		var err error
		switch {
		case oOK && nOK:
			err = d.change(path, elem, k, child, ov, nv)
		case nOK:
			err = d.add(path, elem, k, child, nv)
		default:
			err = d.remove(path, elem, k, child, ov)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (d *differ) nested(path []string, elem int, child []string) error {
	if elem >= 0 {
		return fmt.Errorf("%s: a table nested inside an array-of-tables element cannot be patched", strings.Join(child, "."))
	}
	return nil
}

func (d *differ) add(path []string, elem int, k string, child []string, nv any) error {
	kind, err := classify(child, nv)
	if err != nil {
		return err
	}
	switch kind {
	case kindLeaf:
		d.ops = append(d.ops, op{kind: opKey, table: path, elem: elem, key: k, value: nv})
	case kindTable:
		if err := d.nested(path, elem, child); err != nil {
			return err
		}
		return d.table(child, -1, map[string]any{}, nv.(map[string]any))
	case kindArrayOfTables:
		if err := d.nested(path, elem, child); err != nil {
			return err
		}
		for _, e := range nv.([]map[string]any) {
			b, err := d.block(child, e)
			if err != nil {
				return err
			}
			d.ops = append(d.ops, op{kind: opAppendElem, table: child, elem: -1, value: b})
		}
	}
	return nil
}

func (d *differ) remove(path []string, elem int, k string, child []string, ov any) error {
	kind, err := classify(child, ov)
	if err != nil {
		return err
	}
	switch kind {
	case kindLeaf:
		d.ops = append(d.ops, op{kind: opKey, table: path, elem: elem, key: k})
	case kindTable:
		if err := d.nested(path, elem, child); err != nil {
			return err
		}
		return d.table(child, -1, ov.(map[string]any), map[string]any{})
	case kindArrayOfTables:
		if err := d.nested(path, elem, child); err != nil {
			return err
		}
		// Highest index first: each delete renumbers the elements after it.
		for i := len(ov.([]map[string]any)) - 1; i >= 0; i-- {
			d.ops = append(d.ops, op{kind: opDeleteElem, table: child, elem: i})
		}
	}
	return nil
}

func (d *differ) change(path []string, elem int, k string, child []string, ov, nv any) error {
	okind, err := classify(child, ov)
	if err != nil {
		return err
	}
	nkind, err := classify(child, nv)
	if err != nil {
		return err
	}
	if okind != nkind {
		return fmt.Errorf("%s: was a %T, now a %T", strings.Join(child, "."), ov, nv)
	}
	switch okind {
	case kindLeaf:
		if !reflect.DeepEqual(ov, nv) {
			d.ops = append(d.ops, op{kind: opKey, table: path, elem: elem, key: k, value: nv})
		}
	case kindTable:
		if err := d.nested(path, elem, child); err != nil {
			return err
		}
		return d.table(child, -1, ov.(map[string]any), nv.(map[string]any))
	case kindArrayOfTables:
		if err := d.nested(path, elem, child); err != nil {
			return err
		}
		return d.arrayOfTables(child, ov.([]map[string]any), nv.([]map[string]any))
	}
	return nil
}

// arrayOfTables diffs two element lists. Equal lengths are an edit in place:
// each element is diffed field by field against its positional twin, so
// changing one lane's command is one op inside that lane's block. Unequal
// lengths are matched by deep equality instead — an element the new side no
// longer has is deleted whole, one the old side never had is appended whole
// — because guessing which elements shifted would rewrite blocks that did
// not change.
func (d *differ) arrayOfTables(path []string, old, new []map[string]any) error {
	if field, ok := d.idFields[strings.Join(path, ".")]; ok {
		return d.keyedArray(path, field, old, new)
	}
	if len(old) == len(new) {
		for i := range old {
			if err := d.table(path, i, old[i], new[i]); err != nil {
				return err
			}
		}
		return nil
	}
	matched := make([]bool, len(new))
	var gone []int
	for i, oe := range old {
		found := false
		for j, ne := range new {
			if !matched[j] && reflect.DeepEqual(oe, ne) {
				matched[j], found = true, true
				break
			}
		}
		if !found {
			gone = append(gone, i)
		}
	}
	for i := len(gone) - 1; i >= 0; i-- {
		d.ops = append(d.ops, op{kind: opDeleteElem, table: path, elem: gone[i]})
	}
	for j, ne := range new {
		if matched[j] {
			continue
		}
		b, err := d.block(path, ne)
		if err != nil {
			return err
		}
		d.ops = append(d.ops, op{kind: opAppendElem, table: path, elem: -1, value: b})
	}
	return nil
}

// keyedArray diffs an array-of-tables whose elements carry an identity field.
// Elements are matched by that field, never by position, so a reorder, an
// insertion or a removal touches only the elements involved:
//
//  1. elements whose key is gone are deleted, highest index first;
//  2. the survivors are brought into the new order by block moves — the
//     block's bytes, comments included, travel verbatim;
//  3. new elements are inserted at their final index;
//  4. each surviving element is diffed field by field at its final index.
//
// A duplicate or missing identity on either side is an error: matching by a
// key that does not identify would quietly edit the wrong element.
func (d *differ) keyedArray(path []string, field string, old, new []map[string]any) error {
	oldIdx, err := identities(path, field, old)
	if err != nil {
		return fmt.Errorf("old: %w", err)
	}
	newIdx, err := identities(path, field, new)
	if err != nil {
		return fmt.Errorf("new: %w", err)
	}
	cur := make([]string, len(old))
	for i, e := range old {
		cur[i] = e[field].(string)
	}
	for i := len(cur) - 1; i >= 0; i-- {
		if _, keep := newIdx[cur[i]]; !keep {
			d.ops = append(d.ops, op{kind: opDeleteElem, table: path, elem: i})
			cur = slices.Delete(cur, i, i+1)
		}
	}
	var order []string // the survivors, in the new order
	for _, e := range new {
		if id := e[field].(string); hasKey(oldIdx, id) {
			order = append(order, id)
		}
	}
	for i, id := range order {
		if cur[i] == id {
			continue
		}
		j := slices.Index(cur, id)
		d.ops = append(d.ops, op{kind: opMoveElem, table: path, elem: j, to: i})
		cur = slices.Insert(slices.Delete(cur, j, j+1), i, id)
	}
	for i, e := range new {
		id := e[field].(string)
		if _, ok := oldIdx[id]; ok {
			continue
		}
		b, err := d.block(path, e)
		if err != nil {
			return err
		}
		d.ops = append(d.ops, op{kind: opInsertElem, table: path, elem: i, value: b})
		cur = slices.Insert(cur, i, id)
	}
	for i, e := range new {
		if oi, ok := oldIdx[e[field].(string)]; ok {
			if err := d.table(path, i, old[oi], e); err != nil {
				return err
			}
		}
	}
	return nil
}

func hasKey(m map[string]int, k string) bool {
	_, ok := m[k]
	return ok
}

// identities maps each element's identity value to its index, refusing a
// missing, non-string or repeated one.
func identities(path []string, field string, elems []map[string]any) (map[string]int, error) {
	out := make(map[string]int, len(elems))
	for i, e := range elems {
		id, ok := e[field].(string)
		if !ok || id == "" {
			return nil, fmt.Errorf("[[%s]] element %d has no string %s to match it by", strings.Join(path, "."), i, field)
		}
		if _, dup := out[id]; dup {
			return nil, fmt.Errorf("[[%s]] has two elements with %s = %q; elements matched by %s must be unique", strings.Join(path, "."), field, id, field)
		}
		out[id] = i
	}
	return out, nil
}

// block renders one element as an ordered block for opAppendElem. Every
// value in it is classified, so an element carrying a shape the patcher
// cannot write is refused here rather than at splice time.
func (d *differ) block(path []string, e map[string]any) (block, error) {
	var b block
	for _, k := range d.keys(path, nil, e) {
		child := append(append([]string{}, path...), k)
		kind, err := classify(child, e[k])
		if err != nil {
			return nil, err
		}
		if kind != kindLeaf {
			return nil, fmt.Errorf("%s: a table nested inside an array-of-tables element cannot be patched", strings.Join(child, "."))
		}
		b = append(b, kv{k, e[k]})
	}
	return b, nil
}

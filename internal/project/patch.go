package project

// patch.go is the raw-byte project.toml patcher: the one write path every
// project.toml writer routes through so a single-field change leaves every
// other line — comments, hand-added keys, indentation, line endings —
// byte-identical.
//
// It deliberately owns no TOML dependency of its own. indexDoc reads the
// document just far enough to know where every header, key and value starts
// and ends; apply splices rendered values into those spans and re-indexes
// after every op so no offset goes stale. BurntSushi/toml stays the decoder
// and the validator; the only rendering it does here is of single values,
// through encodeFresh, so a spliced value is byte-for-byte what the encoder
// would have written.

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
)

type lineKind uint8

const (
	lineBlank   lineKind = iota
	lineComment          // first non-blank byte is '#'
	lineHeader           // [table] or [[array.of.tables]]
	lineKey              // key = value; the value may run on for more lines
	lineCont             // continuation of a multi-line value owned by an earlier lineKey
)

// seg is one component of a table path. elem is the element index when the
// component names an array-of-tables, -1 for a plain table, so the third
// [[stack.locked]] block is [{stack -1} {locked 2}] and the keys under it
// cannot be confused with the first block's.
type seg struct {
	name string
	elem int
}

type header struct {
	path  []seg
	array bool
}

type keyLine struct {
	table        []seg // enclosing header's path, plus any dotted-key prefix
	key          string
	keyStart     int  // offset of the key's first byte
	lastKeyStart int  // offset of the final dotted component's first byte
	valStart     int  // offset of the value's first byte
	valEnd       int  // offset just past the value; a trailing comment starts after it
	endLine      int  // index of the last line the value occupies
	inline       bool // the value is, or contains, an inline table
	hdrLine      int  // index of the enclosing header line; -1 for a root key
}

type line struct {
	kind   lineKind
	start  int // offset of the line's first byte
	end    int // offset just past the last content byte, before the EOL
	indent string
	hdr    *header
	key    *keyLine
}

// doc is the index of one project.toml: every line classified, every value
// span located, and the layout facts (line ending, indent unit, whether the
// last line is newline-terminated) a later splice must reproduce.
type doc struct {
	src          []byte
	eol          string
	finalNewline bool
	lines        []line
	unit         string // one indent level, as the file itself spells it
	// arrays counts the [[...]] elements seen so far per qualified parent
	// path, so nested arrays-of-tables restart at 0 under each new parent
	// element the way TOML defines them.
	arrays map[string]int
}

// indexDoc classifies every line of src and locates every value span. It
// refuses a document it cannot follow rather than guessing at spans: the
// caller decodes the file through BurntSushi first, so anything refused here
// is a shape this patcher does not yet model, not a TOML error.
func indexDoc(src []byte) (*doc, error) {
	d := &doc{src: src, eol: "\n", unit: "  ", arrays: map[string]int{}}
	if i := bytes.IndexByte(src, '\n'); i > 0 && src[i-1] == '\r' {
		d.eol = "\r\n"
	}
	d.finalNewline = len(src) > 0 && src[len(src)-1] == '\n'

	type bound struct{ start, end int }
	var bounds []bound
	for pos := 0; pos < len(src); {
		end, next := len(src), len(src)
		if nl := bytes.IndexByte(src[pos:], '\n'); nl >= 0 {
			end = pos + nl
			next = end + 1
		}
		if d.eol == "\r\n" && end > pos && src[end-1] == '\r' {
			end--
		}
		bounds = append(bounds, bound{pos, end})
		pos = next
	}

	d.lines = make([]line, len(bounds))
	for i := range bounds {
		d.lines[i] = line{start: bounds[i].start, end: bounds[i].end}
	}

	var (
		curHdr     *header
		curHdrLine = -1
	)
	for i := 0; i < len(d.lines); i++ {
		ln := &d.lines[i]
		if ln.kind == lineCont {
			continue
		}
		content := src[ln.start:ln.end]
		ln.indent = leadingWS(content)
		rest := content[len(ln.indent):]
		switch {
		case len(rest) == 0:
			ln.kind = lineBlank
		case rest[0] == '#':
			ln.kind = lineComment
		case rest[0] == '[':
			h, err := d.parseHeader(ln.start+len(ln.indent), ln.end, i)
			if err != nil {
				return nil, err
			}
			ln.kind = lineHeader
			ln.hdr = h
			curHdr, curHdrLine = h, i
		default:
			kl, err := d.parseKeyLine(i, ln.start+len(ln.indent), curHdr, curHdrLine)
			if err != nil {
				return nil, err
			}
			ln.kind = lineKey
			ln.key = kl
			for j := i + 1; j <= kl.endLine; j++ {
				d.lines[j].kind = lineCont
			}
			i = kl.endLine
		}
	}

	// The indent unit is whatever the file's first key under a header is
	// indented by relative to that header — the encoder's "  ", or nothing
	// for a hand-written flush-left file — so inserted lines match.
	for _, ln := range d.lines {
		if ln.kind != lineKey || ln.key.hdrLine < 0 {
			continue
		}
		d.unit = strings.TrimPrefix(ln.indent, d.lines[ln.key.hdrLine].indent)
		break
	}
	return d, nil
}

func leadingWS(b []byte) string {
	i := 0
	for i < len(b) && (b[i] == ' ' || b[i] == '\t') {
		i++
	}
	return string(b[:i])
}

func skipWS(src []byte, pos int) int {
	for pos < len(src) && (src[pos] == ' ' || src[pos] == '\t') {
		pos++
	}
	return pos
}

// parseHeader reads `[a.b]` or `[[a.b]]` at src[pos:end] and qualifies the
// path against the arrays seen so far: a plain header under an array-of-
// tables element takes that element's index, and an array header takes the
// next index for its parent.
func (d *doc) parseHeader(pos, end, lineNo int) (*header, error) {
	src := d.src
	array := pos+1 < end && src[pos+1] == '['
	pos++
	if array {
		pos++
	}
	names, _, next, err := parseKeyPath(src, pos)
	if err != nil {
		return nil, fmt.Errorf("line %d: header: %w", lineNo+1, err)
	}
	next = skipWS(src, next)
	closer := "]"
	if array {
		closer = "]]"
	}
	if !bytes.HasPrefix(src[next:end], []byte(closer)) {
		return nil, fmt.Errorf("line %d: header: expected %s", lineNo+1, closer)
	}
	if err := d.checkTrailing(next+len(closer), end, lineNo); err != nil {
		return nil, err
	}

	h := &header{array: array}
	for i, name := range names {
		key := qualifiedKey(h.path, name)
		if i == len(names)-1 && array {
			h.path = append(h.path, seg{name, d.arrays[key]})
			d.arrays[key]++
			continue
		}
		if n, ok := d.arrays[key]; ok {
			h.path = append(h.path, seg{name, n - 1})
			continue
		}
		h.path = append(h.path, seg{name, -1})
	}
	return h, nil
}

// qualifiedKey names a child of path for the arrays map; the elem indices are
// part of the key so [[a]]/[[a.b]]/[[a]]/[[a.b]] numbers b from 0 twice.
func qualifiedKey(path []seg, name string) string {
	var b strings.Builder
	for _, s := range path {
		fmt.Fprintf(&b, "%s[%d]\x00", s.name, s.elem)
	}
	b.WriteString(name)
	return b.String()
}

func (d *doc) checkTrailing(pos, end, lineNo int) error {
	pos = skipWS(d.src, pos)
	if pos < end && d.src[pos] != '#' {
		return fmt.Errorf("line %d: unexpected %q after value", lineNo+1, string(d.src[pos:end]))
	}
	return nil
}

// parseKeyLine reads `key = value` starting at pos (past the indent). The
// value may span lines; every line it occupies through endLine is owned by
// this key.
func (d *doc) parseKeyLine(lineNo, pos int, hdr *header, hdrLine int) (*keyLine, error) {
	src := d.src
	names, lastStart, next, err := parseKeyPath(src, pos)
	if err != nil {
		return nil, fmt.Errorf("line %d: %w", lineNo+1, err)
	}
	next = skipWS(src, next)
	if next >= len(src) || src[next] != '=' {
		return nil, fmt.Errorf("line %d: expected '=' after key %q", lineNo+1, strings.Join(names, "."))
	}
	valStart := skipWS(src, next+1)
	valEnd, inline, err := scanValue(src, valStart)
	if err != nil {
		return nil, fmt.Errorf("line %d: %w", lineNo+1, err)
	}
	endLine := lineNo + bytes.Count(src[valStart:valEnd], []byte("\n"))
	if endLine >= len(d.lines) {
		return nil, fmt.Errorf("line %d: value runs past end of file", lineNo+1)
	}
	if err := d.checkTrailing(valEnd, d.lines[endLine].end, endLine); err != nil {
		return nil, err
	}

	kl := &keyLine{
		key:          names[len(names)-1],
		keyStart:     pos,
		lastKeyStart: lastStart,
		valStart:     valStart,
		valEnd:       valEnd,
		endLine:      endLine,
		inline:       inline,
		hdrLine:      hdrLine,
	}
	if hdr != nil {
		kl.table = append(kl.table, hdr.path...)
	}
	for _, n := range names[:len(names)-1] {
		kl.table = append(kl.table, seg{n, -1})
	}
	return kl, nil
}

// parseKeyPath reads a dotted key at src[pos:]: bare, basic-quoted or
// literal-quoted components separated by '.', with whitespace allowed around
// the dots. It returns the decoded components, the offset of the last
// component and the offset just past it.
func parseKeyPath(src []byte, pos int) (names []string, lastStart, end int, err error) {
	for {
		pos = skipWS(src, pos)
		lastStart = pos
		var name string
		switch {
		case pos < len(src) && src[pos] == '"':
			end, err = scanBasic(src, pos)
			if err != nil {
				return nil, 0, 0, err
			}
			name, err = unescapeBasic(string(src[pos+1 : end-1]))
			if err != nil {
				return nil, 0, 0, err
			}
		case pos < len(src) && src[pos] == '\'':
			end, err = scanLiteral(src, pos)
			if err != nil {
				return nil, 0, 0, err
			}
			name = string(src[pos+1 : end-1])
		default:
			end = pos
			for end < len(src) && isBareKeyByte(src[end]) {
				end++
			}
			if end == pos {
				return nil, 0, 0, fmt.Errorf("expected a key at %q", firstBytes(src[pos:], 12))
			}
			name = string(src[pos:end])
		}
		names = append(names, name)
		next := skipWS(src, end)
		if next < len(src) && src[next] == '.' {
			pos = next + 1
			continue
		}
		return names, lastStart, end, nil
	}
}

func isBareKeyByte(c byte) bool {
	return c == '_' || c == '-' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func firstBytes(b []byte, n int) string {
	if i := bytes.IndexByte(b, '\n'); i >= 0 {
		b = b[:i]
	}
	if len(b) > n {
		b = b[:n]
	}
	return string(b)
}

// scanValue returns the offset just past the TOML value starting at pos, and
// whether an inline table sits anywhere in it. Strings are skipped as
// strings, so a '#' inside one never ends a scalar and a ']' inside one never
// closes an array.
func scanValue(src []byte, pos int) (end int, inline bool, err error) {
	if pos >= len(src) {
		return 0, false, fmt.Errorf("missing value")
	}
	switch {
	case bytes.HasPrefix(src[pos:], []byte(`"""`)):
		end, err = scanMultilineBasic(src, pos)
	case src[pos] == '"':
		end, err = scanBasic(src, pos)
	case bytes.HasPrefix(src[pos:], []byte(`'''`)):
		end, err = scanMultilineLiteral(src, pos)
	case src[pos] == '\'':
		end, err = scanLiteral(src, pos)
	case src[pos] == '[':
		end, inline, err = scanBracketed(src, pos, '[', ']')
	case src[pos] == '{':
		end, _, err = scanBracketed(src, pos, '{', '}')
		inline = true
	default:
		end = pos
		for end < len(src) && src[end] != '#' && src[end] != '\n' && src[end] != '\r' {
			end++
		}
		for end > pos && (src[end-1] == ' ' || src[end-1] == '\t') {
			end--
		}
		if end == pos {
			return 0, false, fmt.Errorf("missing value")
		}
	}
	return end, inline, err
}

// scanString dispatches on the quote at src[pos].
func scanString(src []byte, pos int) (int, error) {
	switch {
	case bytes.HasPrefix(src[pos:], []byte(`"""`)):
		return scanMultilineBasic(src, pos)
	case src[pos] == '"':
		return scanBasic(src, pos)
	case bytes.HasPrefix(src[pos:], []byte(`'''`)):
		return scanMultilineLiteral(src, pos)
	default:
		return scanLiteral(src, pos)
	}
}

func scanBasic(src []byte, pos int) (int, error) {
	for i := pos + 1; i < len(src); i++ {
		switch src[i] {
		case '\\':
			i++
		case '"':
			return i + 1, nil
		case '\n':
			return 0, fmt.Errorf("unterminated string")
		}
	}
	return 0, fmt.Errorf("unterminated string")
}

func scanMultilineBasic(src []byte, pos int) (int, error) {
	for i := pos + 3; i < len(src); i++ {
		if src[i] == '\\' {
			i++
			continue
		}
		if bytes.HasPrefix(src[i:], []byte(`"""`)) {
			i += 3
			// Up to two extra quotes belong to the string, not the delimiter.
			for extra := 0; extra < 2 && i < len(src) && src[i] == '"'; extra++ {
				i++
			}
			return i, nil
		}
	}
	return 0, fmt.Errorf("unterminated multi-line string")
}

func scanLiteral(src []byte, pos int) (int, error) {
	for i := pos + 1; i < len(src); i++ {
		switch src[i] {
		case '\'':
			return i + 1, nil
		case '\n':
			return 0, fmt.Errorf("unterminated literal string")
		}
	}
	return 0, fmt.Errorf("unterminated literal string")
}

func scanMultilineLiteral(src []byte, pos int) (int, error) {
	i := bytes.Index(src[pos+3:], []byte(`'''`))
	if i < 0 {
		return 0, fmt.Errorf("unterminated multi-line literal string")
	}
	i += pos + 6
	for extra := 0; extra < 2 && i < len(src) && src[i] == '\''; extra++ {
		i++
	}
	return i, nil
}

// scanBracketed skips a balanced open..close run — an array or an inline
// table — stepping over strings and, inside arrays, over comments. inline
// reports whether a '{' was seen outside a string, which is how an array of
// inline tables is told apart from a plain array.
func scanBracketed(src []byte, pos int, open, close byte) (end int, inline bool, err error) {
	depth := 0
	for i := pos; i < len(src); i++ {
		switch c := src[i]; {
		case c == open:
			depth++
		case c == close:
			depth--
			if depth == 0 {
				return i + 1, inline, nil
			}
		case c == '"' || c == '\'':
			next, err := scanString(src, i)
			if err != nil {
				return 0, false, err
			}
			i = next - 1
		case c == '#':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '{':
			inline = true
		}
	}
	return 0, false, fmt.Errorf("unterminated %c...%c", open, close)
}

// unescapeBasic decodes the escapes a quoted key may carry.
func unescapeBasic(s string) (string, error) {
	if !strings.Contains(s, `\`) {
		return s, nil
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			continue
		}
		i++
		if i >= len(s) {
			return "", fmt.Errorf("dangling escape in key %q", s)
		}
		switch s[i] {
		case 'b':
			b.WriteByte('\b')
		case 't':
			b.WriteByte('\t')
		case 'n':
			b.WriteByte('\n')
		case 'f':
			b.WriteByte('\f')
		case 'r':
			b.WriteByte('\r')
		case '"', '\\':
			b.WriteByte(s[i])
		case 'u', 'U':
			width := 4
			if s[i] == 'U' {
				width = 8
			}
			if i+width >= len(s) {
				return "", fmt.Errorf("short unicode escape in key %q", s)
			}
			var r rune
			for _, h := range s[i+1 : i+1+width] {
				r <<= 4
				switch {
				case h >= '0' && h <= '9':
					r |= h - '0'
				case h >= 'a' && h <= 'f':
					r |= h - 'a' + 10
				case h >= 'A' && h <= 'F':
					r |= h - 'A' + 10
				default:
					return "", fmt.Errorf("bad unicode escape in key %q", s)
				}
			}
			b.WriteRune(r)
			i += width
		default:
			return "", fmt.Errorf("unknown escape \\%c in key %q", s[i], s)
		}
	}
	return b.String(), nil
}

// --- ops -------------------------------------------------------------------

type opKind uint8

const (
	// opKey sets key under table to value, inserting the line when the key
	// is absent; a nil value deletes the key line instead.
	opKey opKind = iota
	// opAppendElem appends a new [[table]] element; value is a block.
	opAppendElem
	// opDeleteElem removes element elem of the [[table]] array. Issue these
	// highest index first: every op re-indexes, so removing element 1 makes
	// the old element 2 the new element 1.
	opDeleteElem
)

// op is one surgical edit. table is the dotted path of the table holding key;
// elem is the element index when that table is an array-of-tables, -1
// otherwise. A table nested INSIDE an array element (a [[a]] with its own
// [a.sub]) has no spelling here and is refused by name — the project schema
// has none, and an op shape that guesses which element would be worse than
// no shape.
type op struct {
	kind  opKind
	table []string
	elem  int
	key   string
	value any
}

func (o op) String() string {
	t := strings.Join(o.table, ".")
	if o.elem >= 0 {
		t = fmt.Sprintf("%s[%d]", t, o.elem)
	}
	switch o.kind {
	case opAppendElem:
		return "append [[" + t + "]]"
	case opDeleteElem:
		return "delete [[" + t + "]]"
	}
	if o.value == nil {
		return "delete " + t + "." + o.key
	}
	return "set " + t + "." + o.key
}

// kv is one key of an appended element, in the order it should be written.
type kv struct {
	key   string
	value any
}

// block is the ordered body of an array-of-tables element for opAppendElem.
// A value that is itself a block becomes a sub-table header after the keys.
type block []kv

// apply performs ops on src in order, re-indexing after each so no offset
// goes stale, and returns the patched bytes. On any error the input bytes are
// returned unchanged — never a half-applied document.
func apply(src []byte, ops []op) ([]byte, error) {
	cur := src
	for i, o := range ops {
		d, err := indexDoc(cur)
		if err != nil {
			return src, fmt.Errorf("op %d (%s): index: %w", i, o, err)
		}
		next, err := d.applyOne(o)
		if err != nil {
			return src, fmt.Errorf("op %d (%s): %w", i, o, err)
		}
		cur = next
	}
	return cur, nil
}

func (d *doc) applyOne(o op) ([]byte, error) {
	target, err := d.qualify(o)
	if err != nil {
		return nil, err
	}
	names := append(append([]string{}, o.table...), o.key)
	if o.kind != opKey {
		names = o.table
	}
	if err := d.refuseInline(names); err != nil {
		return nil, err
	}
	switch o.kind {
	case opAppendElem:
		return d.appendElem(target, o.value)
	case opDeleteElem:
		return d.deleteElem(target)
	}
	if o.value == nil {
		return d.deleteKey(target, o.key)
	}
	return d.setKey(target, o.key, o.value)
}

// qualify turns an op's table + elem into the seg path the index uses.
func (d *doc) qualify(o op) ([]seg, error) {
	var path []seg
	for i, name := range o.table {
		key := qualifiedKey(path, name)
		n, isArray := d.arrays[key]
		last := i == len(o.table)-1
		switch {
		case !last && isArray:
			return nil, fmt.Errorf("[[%s]] is an array-of-tables: a table nested inside one of its elements cannot be patched",
				strings.Join(o.table[:i+1], "."))
		case !last:
			path = append(path, seg{name, -1})
		case o.kind == opAppendElem:
			path = append(path, seg{name, -1})
		case o.elem < 0 && isArray:
			return nil, fmt.Errorf("[[%s]] is an array-of-tables; the op names no element", strings.Join(o.table, "."))
		case o.elem >= 0 && o.elem >= n:
			return nil, fmt.Errorf("[[%s]] has %d element(s), no element %d; append it instead", strings.Join(o.table, "."), n, o.elem)
		default:
			path = append(path, seg{name, o.elem})
		}
	}
	return path, nil
}

// refuseInline fails by key and line when any prefix of names is spelled as
// an inline table (or an array of them): splicing keys into `{...}` or
// emitting a [header] beside it would either corrupt the value or redefine
// the table, so the user is asked to rewrite it in header form first.
func (d *doc) refuseInline(names []string) error {
	for i, ln := range d.lines {
		if ln.kind != lineKey || !ln.key.inline {
			continue
		}
		full := segNames(ln.key.table)
		full = append(full, ln.key.key)
		if len(full) > len(names) || !slices.Equal(full, names[:len(full)]) {
			continue
		}
		dotted := strings.Join(full, ".")
		return fmt.Errorf("%s at line %d is an inline table, which dross does not patch in place; rewrite it as a [%s] table (one key per line) and retry",
			dotted, i+1, dotted)
	}
	return nil
}

func segNames(path []seg) []string {
	out := make([]string, len(path))
	for i, s := range path {
		out[i] = s.name
	}
	return out
}

func hasNamePrefix(path []seg, prefix []string) bool {
	if len(path) < len(prefix) {
		return false
	}
	return slices.Equal(segNames(path[:len(prefix)]), prefix)
}

// findKey returns the index of the key line for table.key, or -1.
func (d *doc) findKey(table []seg, key string) int {
	for i, ln := range d.lines {
		if ln.kind == lineKey && ln.key.key == key && slices.Equal(ln.key.table, table) {
			return i
		}
	}
	return -1
}

// lastHeader returns the index of the last header line whose path satisfies
// match, or -1.
func (d *doc) lastHeader(match func(*header) bool) int {
	for i := len(d.lines) - 1; i >= 0; i-- {
		if d.lines[i].kind == lineHeader && match(d.lines[i].hdr) {
			return i
		}
	}
	return -1
}

// blockEnd returns the index just past the last line of the block that header
// line h owns: everything up to the next header.
func (d *doc) blockEnd(h int) int {
	for i := h + 1; i < len(d.lines); i++ {
		if d.lines[i].kind == lineHeader {
			return i
		}
	}
	return len(d.lines)
}

// lastContent returns the last non-blank line in lines[from:to), or from-1
// when there is none.
func (d *doc) lastContent(from, to int) int {
	for i := to - 1; i >= from; i-- {
		if d.lines[i].kind != lineBlank {
			return i
		}
	}
	return from - 1
}

func (d *doc) setKey(table []seg, key string, value any) ([]byte, error) {
	rendered, err := renderValue(value)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", key, err)
	}
	if i := d.findKey(table, key); i >= 0 {
		kl := d.lines[i].key
		return d.splice(kl.valStart, kl.valEnd, rendered), nil
	}
	keyText, err := renderKey(key)
	if err != nil {
		return nil, err
	}

	// An existing header for the table: the new key goes after its last key
	// line, at the indent its siblings use.
	if h := d.lastHeader(func(x *header) bool { return slices.Equal(x.path, table) }); h >= 0 {
		end := d.blockEnd(h)
		after, indent := h, d.lines[h].indent+d.unit
		for i := h + 1; i < end; i++ {
			if d.lines[i].kind == lineKey {
				after, indent = d.lines[i].key.endLine, d.lines[i].indent
			}
		}
		return d.insertAfter(after, []string{indent + keyText + " = " + rendered}), nil
	}

	// The table spelled as dotted keys under a parent header (`state_map.x`
	// inside [board]): mirror that spelling after the last such key.
	for i := len(d.lines) - 1; i >= 0; i-- {
		ln := d.lines[i]
		if ln.kind != lineKey || !slices.Equal(ln.key.table, table) {
			continue
		}
		prefix := string(d.src[ln.key.keyStart:ln.key.lastKeyStart])
		return d.insertAfter(ln.key.endLine, []string{ln.indent + prefix + keyText + " = " + rendered}), nil
	}

	if len(table) == 0 {
		// Only root keys anchor a root key: a document whose keys all live
		// under headers gets it at the top, ahead of the first header,
		// which is the one place TOML lets a root key sit.
		after := -1
		for _, ln := range d.lines {
			if ln.kind == lineKey && ln.key.hdrLine < 0 {
				after = ln.key.endLine
			}
		}
		return d.insertAfter(after, []string{keyText + " = " + rendered}), nil
	}
	if table[len(table)-1].elem >= 0 {
		// qualify already bounds-checked, so the element exists and would
		// have matched a header above; reaching here means an index bug.
		return nil, fmt.Errorf("[[%s]] element %d has no header line", strings.Join(segNames(table), "."), table[len(table)-1].elem)
	}
	lines, err := d.renderTable(segNames(table), false, block{{key, value}})
	if err != nil {
		return nil, err
	}
	return d.insertBlock(d.newTableAnchor(segNames(table)), lines), nil
}

// newTableAnchor picks the line a brand-new header block goes after: the end
// of the block owned by the last header sharing the longest parent prefix,
// so [board.state_map] lands inside the board region rather than at EOF.
// With no related header at all it is the end of the file.
func (d *doc) newTableAnchor(names []string) int {
	for k := len(names) - 1; k >= 1; k-- {
		prefix := names[:k]
		if h := d.lastHeader(func(x *header) bool { return hasNamePrefix(x.path, prefix) }); h >= 0 {
			return max(d.lastContent(h+1, d.blockEnd(h)), h)
		}
	}
	return d.lastContent(0, len(d.lines))
}

func (d *doc) deleteKey(table []seg, key string) ([]byte, error) {
	i := d.findKey(table, key)
	if i < 0 {
		return nil, fmt.Errorf("%s.%s is not present", strings.Join(segNames(table), "."), key)
	}
	kl := d.lines[i].key
	out := d.removeLines(i, kl.endLine)
	if kl.hdrLine < 0 {
		return out, nil
	}
	// A header left sheltering nothing — no key, modeled or not, and no
	// comment — is removed with the separator blank above it. One that still
	// holds a hand-added key or a comment stays exactly as it is. An
	// array-of-tables element stays even when empty: dropping its header
	// would shorten the array, and a delete-key op must never change more
	// than the key it names.
	nd, err := indexDoc(out)
	if err != nil {
		return nil, err
	}
	h := kl.hdrLine
	if nd.lines[h].hdr.array {
		return out, nil
	}
	path := nd.lines[h].hdr.path
	for {
		if nd.shelters(h) {
			return out, nil
		}
		out = nd.removeBlock(h, h)
		// The parent may now shelter nothing either — a [runtime.services]
		// whose last [runtime.services.x] just went — so walk up while
		// that holds. An array element is never removed this way.
		path = path[:len(path)-1]
		if len(path) == 0 {
			return out, nil
		}
		if nd, err = indexDoc(out); err != nil {
			return nil, err
		}
		h = nd.lastHeader(func(x *header) bool { return !x.array && slices.Equal(x.path, path) })
		if h < 0 {
			return out, nil
		}
	}
}

// shelters reports whether header line h still has a reason to exist: a key
// (modeled or not) or a comment in its own block, or a sub-table header
// beneath it anywhere in the document.
func (d *doc) shelters(h int) bool {
	end := d.blockEnd(h)
	for j := h + 1; j < end; j++ {
		if d.lines[j].kind != lineBlank {
			return true
		}
	}
	path := d.lines[h].hdr.path
	for j, ln := range d.lines {
		if j != h && ln.kind == lineHeader && hasSegPrefix(ln.hdr.path, path) {
			return true
		}
	}
	return false
}

func (d *doc) deleteElem(target []seg) ([]byte, error) {
	h := d.lastHeader(func(x *header) bool { return x.array && slices.Equal(x.path, target) })
	if h < 0 {
		return nil, fmt.Errorf("[[%s]] element %d has no header line", strings.Join(segNames(target), "."), target[len(target)-1].elem)
	}
	// The element's span runs through its own sub-table headers, up to the
	// first header that is not beneath this element.
	end := len(d.lines)
	for i := h + 1; i < len(d.lines); i++ {
		if d.lines[i].kind == lineHeader && !hasSegPrefix(d.lines[i].hdr.path, target) {
			end = i
			break
		}
	}
	return d.removeBlock(h, max(d.lastContent(h+1, end), h)), nil
}

func hasSegPrefix(path, prefix []seg) bool {
	return len(path) >= len(prefix) && slices.Equal(path[:len(prefix)], prefix)
}

func (d *doc) appendElem(target []seg, value any) ([]byte, error) {
	body, err := toBlock(value)
	if err != nil {
		return nil, err
	}
	names := segNames(target)
	lines, err := d.renderTable(names, true, body)
	if err != nil {
		return nil, err
	}
	// After the last existing sibling (including any sub-tables it owns),
	// else where a new table of this name would go.
	anchor := d.newTableAnchor(names)
	if h := d.lastHeader(func(x *header) bool { return hasNamePrefix(x.path, names) }); h >= 0 {
		anchor = max(d.lastContent(h+1, d.blockEnd(h)), h)
	}
	return d.insertBlock(anchor, lines), nil
}

func toBlock(v any) (block, error) {
	switch b := v.(type) {
	case block:
		return b, nil
	case map[string]any:
		keys := make([]string, 0, len(b))
		for k := range b {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		out := make(block, 0, len(keys))
		for _, k := range keys {
			out = append(out, kv{k, b[k]})
		}
		return out, nil
	}
	return nil, fmt.Errorf("an element body must be a block or map[string]any, not %T", v)
}

// renderTable lays out a header and its keys the way the encoder does:
// headers indented one unit per level of depth below the top, keys one unit
// deeper, sub-tables after the keys behind a blank line.
func (d *doc) renderTable(names []string, array bool, body block) ([]string, error) {
	dotted := make([]string, len(names))
	for i, n := range names {
		k, err := renderKey(n)
		if err != nil {
			return nil, err
		}
		dotted[i] = k
	}
	hdrIndent := strings.Repeat(d.unit, len(names)-1)
	open, close := "[", "]"
	if array {
		open, close = "[[", "]]"
	}
	lines := []string{hdrIndent + open + strings.Join(dotted, ".") + close}
	keyIndent := hdrIndent + d.unit
	var subs []kv
	for _, e := range body {
		switch e.value.(type) {
		case block, map[string]any:
			subs = append(subs, e)
			continue
		}
		k, err := renderKey(e.key)
		if err != nil {
			return nil, err
		}
		v, err := renderValue(e.value)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.key, err)
		}
		lines = append(lines, keyIndent+k+" = "+v)
	}
	for _, s := range subs {
		b, err := toBlock(s.value)
		if err != nil {
			return nil, err
		}
		sub, err := d.renderTable(append(append([]string{}, names...), s.key), false, b)
		if err != nil {
			return nil, err
		}
		lines = append(append(lines, ""), sub...)
	}
	return lines, nil
}

// --- splicing --------------------------------------------------------------

// splice replaces src[from:to] with text, giving any newline inside text the
// document's own line ending.
func (d *doc) splice(from, to int, text string) []byte {
	if d.eol != "\n" {
		text = strings.ReplaceAll(text, "\n", d.eol)
	}
	out := make([]byte, 0, len(d.src)-(to-from)+len(text))
	out = append(out, d.src[:from]...)
	out = append(out, text...)
	out = append(out, d.src[to:]...)
	return out
}

// insertAfter places lines after line index after (-1 for the top of the
// file), each terminated with the document's line ending. A file whose last
// line is not newline-terminated gets that newline first, so nothing is ever
// glued onto an existing line.
func (d *doc) insertAfter(after int, lines []string) []byte {
	if len(lines) == 0 {
		return d.src
	}
	// Built with bare newlines: splice gives every one the document's EOL.
	text := strings.Join(lines, "\n") + "\n"
	if after < 0 {
		return d.splice(0, 0, text)
	}
	at := d.lines[after].end
	if at >= len(d.src) {
		// Last line, no trailing newline: terminate it before appending.
		return d.splice(at, at, "\n"+text)
	}
	at += len(d.eol)
	return d.splice(at, at, text)
}

// insertBlock inserts a header block after line index after, separated from
// the content above it by one blank line — the encoder's spacing.
func (d *doc) insertBlock(after int, lines []string) []byte {
	if after >= 0 {
		lines = append([]string{""}, lines...)
	}
	return d.insertAfter(after, lines)
}

// removeLines drops lines from..to inclusive, EOLs included.
func (d *doc) removeLines(from, to int) []byte {
	start := d.lines[from].start
	end := d.lines[to].end
	if end < len(d.src) {
		end += len(d.eol)
	} else if from > 0 {
		// Dropping the unterminated last line: the previous line's EOL now
		// terminates the file, which is what every other line already has.
		end = len(d.src)
	}
	return d.splice(start, end, "")
}

// removeBlock drops a header line and its content through last, together
// with the separator blank lines above the header. The blank lines below the
// block stay — they are the separator the next header still needs.
func (d *doc) removeBlock(h, last int) []byte {
	from := h
	for from > 0 && d.lines[from-1].kind == lineBlank {
		from--
	}
	if from == 0 {
		for last+1 < len(d.lines) && d.lines[last+1].kind == lineBlank {
			last++
		}
	}
	return d.removeLines(from, last)
}

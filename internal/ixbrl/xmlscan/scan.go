// Package xmlscan tokenises a whole XML document held in memory.
//
// It reproduces the token stream of encoding/xml's Decoder.Token configured
// the way ch-xbrl configured it: Strict false, Entity xml.HTMLEntity,
// AutoClose unset, and a CharsetReader that accepts only ASCII. A document
// that Decoder accepts gives the same start elements, end elements and
// character data here. A document that Decoder rejects is an error here too.
// Comments, processing instructions and directives are checked the same way
// and then skipped. The parity tests in this package compare the two.
//
// Names, attribute values and text are sub-slices of the input. A value is
// decoded only when the caller asks for it, and only copied when it contains
// an entity reference or a carriage return.
package xmlscan

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// xmlURL is the namespace of the reserved xml prefix.
const xmlURL = "http://www.w3.org/XML/1998/namespace"

// Kind is the type of the current token.
type Kind uint8

const (
	StartElement Kind = iota + 1
	EndElement
	CharData
)

// SyntaxError is a document Decoder would also reject. The message has the
// same form as xml.SyntaxError. The line is counted from the scan position.
type SyntaxError struct {
	Msg  string
	Line int
}

func (e *SyntaxError) Error() string {
	return "XML syntax error on line " + strconv.Itoa(e.Line) + ": " + e.Msg
}

type span struct{ start, end int }

type attr struct {
	name      span
	colon     int // prefix separator in name, or -1
	val       span
	quoted    bool
	valueless bool // <option selected>: the value is the local name
	decode    bool // the quoted value holds '&' or '\r'
}

type elem struct {
	prefix, local span
	nsCount       int // bindings this element pushed onto undo
}

type nsUndo struct {
	prefix string
	old    string
	ok     bool
}

// Scanner reads one document. The zero value is not usable; call New or Reset.
type Scanner struct {
	data []byte
	pos  int
	err  error

	space string
	local span
	attrs []attr
	text  span
	cdata bool
	buf   []byte

	// needClose: the next token is an end element for closePrefix:closeLocal
	// (after <x/>, or after a mismatched end tag closed the open element).
	needClose  bool
	closeName  elem
	orphanedNS bool

	stack  []elem
	undo   []nsUndo
	ns     map[string]string
	intern map[string]string

	validChars bool // every byte of data is a legal XML character
	hasCR      bool
	hasCDEnd   bool
}

// New returns a scanner over data. The scanner keeps data and does not
// modify it.
func New(data []byte) *Scanner {
	s := &Scanner{}
	s.Reset(data)
	return s
}

// Reset starts a new document and keeps the scanner's buffers.
func (s *Scanner) Reset(data []byte) {
	s.data = data
	s.pos = 0
	s.err = nil
	s.space = ""
	s.attrs = s.attrs[:0]
	s.needClose = false
	s.orphanedNS = false
	s.stack = s.stack[:0]
	s.undo = s.undo[:0]
	if s.ns == nil {
		s.ns = map[string]string{}
	} else {
		clear(s.ns)
	}
	s.validChars = legalChars(data)
	s.hasCR = bytes.IndexByte(data, '\r') >= 0
	s.hasCDEnd = bytes.Contains(data, cdataEnd)
}

var cdataEnd = []byte("]]>")

// Next advances to the next start element, end element or character data.
// It returns io.EOF at the end of a complete document. Any other error is
// final and is returned again by later calls.
func (s *Scanner) Next() (Kind, error) {
	if s.err != nil {
		return 0, s.err
	}
	if s.needClose {
		s.needClose = false
		return s.endElement(s.closeName.prefix, s.closeName.local)
	}
	for {
		if s.pos >= len(s.data) {
			if len(s.stack) > 0 || s.orphanedNS {
				return s.fail(s.syntaxError(s.pos, "unexpected EOF"))
			}
			s.err = io.EOF
			return 0, io.EOF
		}
		if s.data[s.pos] != '<' {
			return s.charData()
		}
		k, err := s.markup()
		if err != nil || k != 0 {
			return k, err
		}
	}
}

// Space is the namespace of the current element, resolved like Decoder: the
// in-scope binding for its prefix, the default namespace for no prefix, or
// the prefix itself when it is not bound. An end element that Decoder
// invents for a mismatched end tag keeps the raw prefix, as Decoder does.
func (s *Scanner) Space() string { return s.space }

// Local is the local name of the current element. It aliases the input.
func (s *Scanner) Local() []byte { return s.data[s.local.start:s.local.end] }

// Text is the current character data with entity references decoded and
// line endings normalised. It is valid until the next call to Next.
func (s *Scanner) Text() []byte {
	b := s.data[s.text.start:s.text.end]
	if s.cdata {
		if s.hasCR && bytes.IndexByte(b, '\r') >= 0 {
			s.buf = decode(s.buf[:0], b, false)
			return s.buf
		}
		return b
	}
	if bytes.IndexByte(b, '&') >= 0 || (s.hasCR && bytes.IndexByte(b, '\r') >= 0) {
		s.buf = decode(s.buf[:0], b, true)
		return s.buf
	}
	return b
}

// NumAttr is the number of attributes on the current start element.
func (s *Scanner) NumAttr() int { return len(s.attrs) }

// AttrLocal is the local name of attribute i. It aliases the input.
func (s *Scanner) AttrLocal(i int) []byte {
	a := &s.attrs[i]
	if a.colon >= 0 {
		return s.data[a.colon+1 : a.name.end]
	}
	return s.data[a.name.start:a.name.end]
}

// AttrSpace is the namespace of attribute i, resolved like Decoder: empty
// for no prefix, "xmlns" for the xmlns prefix, the in-scope binding, or the
// prefix itself when it is not bound.
func (s *Scanner) AttrSpace(i int) string {
	a := &s.attrs[i]
	if a.colon < 0 {
		return ""
	}
	p := s.data[a.name.start:a.colon]
	switch string(p) {
	case "xmlns":
		return "xmlns"
	case "xml":
		return xmlURL
	}
	if v, ok := s.ns[string(p)]; ok {
		return v
	}
	return s.str(p)
}

// AttrValue is the decoded value of attribute i.
func (s *Scanner) AttrValue(i int) string {
	a := &s.attrs[i]
	switch {
	case a.valueless:
		return string(s.AttrLocal(i))
	case a.decode:
		s.buf = decode(s.buf[:0], s.data[a.val.start:a.val.end], true)
		return string(s.buf)
	}
	return string(s.data[a.val.start:a.val.end])
}

// Attr is the value of the first attribute whose local name is local, in
// any namespace, or "" when there is none.
func (s *Scanner) Attr(local string) string {
	for i := range s.attrs {
		if string(s.AttrLocal(i)) == local {
			return s.AttrValue(i)
		}
	}
	return ""
}

func (s *Scanner) fail(err error) (Kind, error) {
	s.err = err
	return 0, err
}

func (s *Scanner) syntaxError(pos int, msg string) error {
	if pos > len(s.data) {
		pos = len(s.data)
	}
	return &SyntaxError{Msg: msg, Line: 1 + bytes.Count(s.data[:pos], []byte{'\n'})}
}

func (s *Scanner) eof() (Kind, error) {
	return s.fail(s.syntaxError(len(s.data), "unexpected EOF"))
}

// str returns b as a string, allocating once per distinct value.
func (s *Scanner) str(b []byte) string {
	if v, ok := s.intern[string(b)]; ok {
		return v
	}
	if s.intern == nil {
		s.intern = map[string]string{}
	}
	v := string(b)
	s.intern[v] = v
	return v
}

func (s *Scanner) bytesOf(sp span) []byte { return s.data[sp.start:sp.end] }

// charData reads text up to the next '<' or the end of the input.
func (s *Scanner) charData() (Kind, error) {
	d := s.data
	start := s.pos
	end := len(d)
	atEOF := true
	if i := bytes.IndexByte(d[start:], '<'); i >= 0 {
		end = start + i
		atEOF = false
	}
	run := d[start:end]
	if s.hasCDEnd {
		if i := bytes.Index(run, cdataEnd); i >= 0 {
			return s.fail(s.syntaxError(start+i, "unescaped ]]> not in CDATA section"))
		}
	}
	if i := bytes.IndexByte(run, '&'); i >= 0 {
		if err := s.checkEntities(run, i, atEOF, start); err != nil {
			return s.fail(err)
		}
	}
	if !s.validChars {
		if err := s.checkChars(run, start); err != nil {
			return s.fail(err)
		}
	}
	s.text = span{start, end}
	s.cdata = false
	s.pos = end
	return CharData, nil
}

// checkEntities finds what Decoder.text would reject in run among its entity
// references: a reference cut off by the end of the input, or a numeric
// reference to a character outside the XML Char production.
func (s *Scanner) checkEntities(run []byte, i int, atEOF bool, base int) error {
	for i >= 0 {
		end, repl, ok, eof := entityAt(run, i, atEOF)
		if eof {
			return s.syntaxError(base+len(run), "unexpected EOF")
		}
		if ok {
			// Only a numeric reference can produce an illegal character.
			// A surrogate has already become U+FFFD, as string(rune) does.
			if r, _ := utf8.DecodeRuneInString(repl); !inCharRange(r) {
				return s.syntaxError(base+i, fmt.Sprintf("illegal character code %U", r))
			}
		}
		j := bytes.IndexByte(run[end:], '&')
		if j < 0 {
			break
		}
		i = end + j
	}
	return nil
}

// checkChars is Decoder's UTF-8 and Char check, for a document that holds an
// illegal byte somewhere. Only text and attribute values are checked, as in
// Decoder; comments, processing instructions and directives are not.
func (s *Scanner) checkChars(b []byte, base int) error {
	for i := 0; i < len(b); {
		r, size := rune(b[i]), 1
		if r >= utf8.RuneSelf {
			r, size = utf8.DecodeRune(b[i:])
			if r == utf8.RuneError && size == 1 {
				return s.syntaxError(base+i, "invalid UTF-8")
			}
		}
		if !inCharRange(r) {
			return s.syntaxError(base+i, fmt.Sprintf("illegal character code %U", r))
		}
		i += size
	}
	return nil
}

func (s *Scanner) markup() (Kind, error) {
	d := s.data
	p := s.pos + 1
	if p >= len(d) {
		return s.eof()
	}
	switch d[p] {
	case '/':
		return s.endTag(p + 1)
	case '?':
		return s.procInst(p + 1)
	case '!':
		return s.bang(p + 1)
	}
	return s.startTag(p)
}

// readName reads Decoder's name bytes from p: ASCII letters, digits, '_',
// ':', '.', '-', and every byte from 0x80. ok is false when the first byte
// is another ASCII byte. Decoder reads the byte after the name too, so a
// name that runs to the end of the input is an unexpected EOF.
func (s *Scanner) readName(p int) (end int, ok bool, err error) {
	d := s.data
	if p >= len(d) {
		return p, false, s.syntaxError(p, "unexpected EOF")
	}
	if c := d[p]; c < utf8.RuneSelf && !isNameByte(c) {
		return p, false, nil
	}
	q := p + 1
	for q < len(d) && (d[q] >= utf8.RuneSelf || isNameByte(d[q])) {
		q++
	}
	if q >= len(d) {
		return q, false, s.syntaxError(q, "unexpected EOF")
	}
	return q, true, nil
}

// name reads a name and checks it like Decoder.name. missing is the message
// when there is no name at p.
func (s *Scanner) name(p int, missing string) (int, error) {
	q, ok, err := s.readName(p)
	if err != nil {
		return q, err
	}
	if !ok {
		return q, s.syntaxError(p, missing)
	}
	if b := s.data[p:q]; !isName(b) {
		return q, s.syntaxError(p, "invalid XML name: "+string(b))
	}
	return q, nil
}

// splitName is Decoder.nsname on data[p:q]. More than one ':' is an error.
// A leading or trailing ':' leaves the whole name as the local part.
func (s *Scanner) splitName(p, q int) (prefix, local span, ok bool) {
	b := s.data[p:q]
	i := bytes.IndexByte(b, ':')
	if i < 0 {
		return span{p, p}, span{p, q}, true
	}
	if bytes.IndexByte(b[i+1:], ':') >= 0 {
		return span{}, span{}, false
	}
	if i == 0 || i == len(b)-1 {
		return span{p, p}, span{p, q}, true
	}
	return span{p, p + i}, span{p + i + 1, q}, true
}

func (s *Scanner) skipSpace(p int) int {
	d := s.data
	for p < len(d) {
		switch d[p] {
		case ' ', '\r', '\n', '\t':
			p++
		default:
			return p
		}
	}
	return p
}

func (s *Scanner) startTag(p int) (Kind, error) {
	d := s.data
	q, err := s.name(p, "expected element name after <")
	if err != nil {
		return s.fail(err)
	}
	prefix, local, ok := s.splitName(p, q)
	if !ok {
		return s.fail(s.syntaxError(p, "expected element name after <"))
	}
	s.attrs = s.attrs[:0]
	empty := false
	pos := q
	for {
		pos = s.skipSpace(pos)
		if pos >= len(d) {
			return s.eof()
		}
		c := d[pos]
		if c == '/' {
			pos++
			if pos >= len(d) {
				return s.eof()
			}
			if d[pos] != '>' {
				return s.fail(s.syntaxError(pos, "expected /> in element"))
			}
			pos++
			empty = true
			break
		}
		if c == '>' {
			pos++
			break
		}
		aq, err := s.name(pos, "expected attribute name in element")
		if err != nil {
			return s.fail(err)
		}
		a := attr{name: span{pos, aq}, colon: -1}
		ap, _, ok := s.splitName(pos, aq)
		if !ok {
			return s.fail(s.syntaxError(pos, "expected attribute name in element"))
		}
		if ap.end > ap.start {
			a.colon = ap.end
		}
		pos = s.skipSpace(aq)
		if pos >= len(d) {
			return s.eof()
		}
		if d[pos] != '=' {
			a.valueless = true
			s.attrs = append(s.attrs, a)
			continue
		}
		pos = s.skipSpace(pos + 1)
		if pos >= len(d) {
			return s.eof()
		}
		if quote := d[pos]; quote == '"' || quote == '\'' {
			vs := pos + 1
			ve := bytes.IndexByte(d[vs:], quote)
			limit := len(d)
			if ve >= 0 {
				limit = vs + ve
			}
			if lt := bytes.IndexByte(d[vs:limit], '<'); lt >= 0 {
				return s.fail(s.syntaxError(vs+lt, "unescaped < inside quoted string"))
			}
			if ve < 0 {
				return s.eof()
			}
			val := d[vs:limit]
			amp := bytes.IndexByte(val, '&')
			if amp >= 0 {
				if err := s.checkEntities(val, amp, false, vs); err != nil {
					return s.fail(err)
				}
			}
			if !s.validChars {
				if err := s.checkChars(val, vs); err != nil {
					return s.fail(err)
				}
			}
			a.val = span{vs, limit}
			a.quoted = true
			a.decode = amp >= 0 || (s.hasCR && bytes.IndexByte(val, '\r') >= 0)
			pos = limit + 1
		} else {
			vs := pos
			for pos < len(d) && isUnquotedByte(d[pos]) {
				pos++
			}
			if pos >= len(d) {
				return s.eof()
			}
			a.val = span{vs, pos}
		}
		s.attrs = append(s.attrs, a)
	}
	s.pos = pos

	// Bindings on this element apply to its own name and attributes.
	n := 0
	for i := range s.attrs {
		a := &s.attrs[i]
		var key string
		switch {
		case a.colon >= 0 && string(d[a.name.start:a.colon]) == "xmlns":
			key = s.str(d[a.colon+1 : a.name.end])
		case a.colon < 0 && string(d[a.name.start:a.name.end]) == "xmlns":
			key = ""
		default:
			continue
		}
		old, had := s.ns[key]
		s.undo = append(s.undo, nsUndo{prefix: key, old: old, ok: had})
		s.ns[key] = s.AttrValue(i)
		n++
	}
	s.stack = append(s.stack, elem{prefix: prefix, local: local, nsCount: n})
	s.space = s.elementSpace(prefix, local)
	s.local = local
	if empty {
		s.needClose = true
		s.closeName = elem{prefix: prefix, local: local}
	}
	return StartElement, nil
}

// elementSpace is Decoder.translate for an element name.
func (s *Scanner) elementSpace(prefix, local span) string {
	p := s.bytesOf(prefix)
	switch {
	case string(p) == "xmlns":
		return "xmlns"
	case string(p) == "xml":
		return xmlURL
	case len(p) == 0 && string(s.bytesOf(local)) == "xmlns":
		return ""
	}
	if v, ok := s.ns[string(p)]; ok {
		return v
	}
	if len(p) == 0 {
		return ""
	}
	return s.str(p)
}

func (s *Scanner) endTag(p int) (Kind, error) {
	d := s.data
	q, err := s.name(p, "expected element name after </")
	if err != nil {
		return s.fail(err)
	}
	prefix, local, ok := s.splitName(p, q)
	if !ok {
		return s.fail(s.syntaxError(p, "expected element name after </"))
	}
	pos := s.skipSpace(q)
	if pos >= len(d) {
		return s.eof()
	}
	if d[pos] != '>' {
		return s.fail(s.syntaxError(pos, "invalid characters between </"+string(s.bytesOf(local))+" and >"))
	}
	s.pos = pos + 1
	return s.endElement(prefix, local)
}

// endElement is Decoder.popElement with Strict false.
func (s *Scanner) endElement(prefix, local span) (Kind, error) {
	if s.orphanedNS || len(s.stack) == 0 {
		return s.fail(s.syntaxError(s.pos, "unexpected end element </"+string(s.bytesOf(local))+">"))
	}
	top := s.stack[len(s.stack)-1]
	if !bytes.Equal(s.bytesOf(top.local), s.bytesOf(local)) {
		// Decoder closes the open element under its raw name, without
		// restoring that element's namespace bindings, and then reads the
		// end tag again. The bindings left on its stack make that second
		// read an "unexpected end element".
		s.stack = s.stack[:len(s.stack)-1]
		if top.nsCount > 0 {
			s.orphanedNS = true
		}
		s.needClose = true
		s.closeName = elem{prefix: prefix, local: local}
		s.space = ""
		if p := s.bytesOf(top.prefix); len(p) > 0 {
			s.space = s.str(p)
		}
		s.local = top.local
		return EndElement, nil
	}
	if !bytes.Equal(s.bytesOf(top.prefix), s.bytesOf(prefix)) {
		ns := string(s.bytesOf(prefix))
		if ns == "" {
			ns = `""`
		}
		l := string(s.bytesOf(local))
		return s.fail(s.syntaxError(s.pos, "element <"+l+"> in space "+string(s.bytesOf(top.prefix))+
			" closed by </"+l+"> in space "+ns))
	}
	s.space = s.elementSpace(prefix, local)
	s.local = local
	for i := 0; i < top.nsCount; i++ {
		u := s.undo[len(s.undo)-1]
		s.undo = s.undo[:len(s.undo)-1]
		if u.ok {
			s.ns[u.prefix] = u.old
		} else {
			delete(s.ns, u.prefix)
		}
	}
	s.stack = s.stack[:len(s.stack)-1]
	return EndElement, nil
}

// procInst checks a processing instruction and skips it. An XML
// declaration's version and encoding are checked like Decoder: only version
// 1.0, and only UTF-8 or ASCII.
func (s *Scanner) procInst(p int) (Kind, error) {
	d := s.data
	q, err := s.name(p, "expected target name after <?")
	if err != nil {
		return s.fail(err)
	}
	pos := s.skipSpace(q)
	i := bytes.Index(d[pos:], []byte("?>"))
	if i < 0 {
		return s.eof()
	}
	content := d[pos : pos+i]
	s.pos = pos + i + 2
	if string(d[p:q]) != "xml" {
		return 0, nil
	}
	c := string(content)
	if ver := procInstParam("version", c); ver != "" && ver != "1.0" {
		return s.fail(fmt.Errorf("xml: unsupported version %q; only version 1.0 is supported", ver))
	}
	if enc := procInstParam("encoding", c); enc != "" && !strings.EqualFold(enc, "utf-8") {
		switch strings.ToLower(strings.TrimSpace(enc)) {
		case "ascii", "us-ascii":
			// ASCII is a subset of UTF-8.
		default:
			return s.fail(fmt.Errorf("xml: opening charset %q: unsupported charset %q", enc, enc))
		}
	}
	return 0, nil
}

// bang handles markup that starts "<!": a comment (skipped), a CDATA section
// (character data) or a directive such as DOCTYPE (skipped).
func (s *Scanner) bang(p int) (Kind, error) {
	d := s.data
	if p >= len(d) {
		return s.eof()
	}
	switch d[p] {
	case '-':
		p++
		if p >= len(d) {
			return s.eof()
		}
		if d[p] != '-' {
			return s.fail(s.syntaxError(p, "invalid sequence <!- not part of <!--"))
		}
		start := p + 1
		i := bytes.Index(d[start:], []byte("--"))
		if i < 0 {
			return s.eof()
		}
		k := start + i + 2
		if k >= len(d) {
			return s.eof()
		}
		if d[k] != '>' {
			return s.fail(s.syntaxError(k, `invalid sequence "--" not allowed in comments`))
		}
		s.pos = k + 1
		return 0, nil
	case '[':
		p++
		for i := 0; i < len("CDATA["); i++ {
			if p+i >= len(d) {
				return s.eof()
			}
			if d[p+i] != "CDATA["[i] {
				return s.fail(s.syntaxError(p+i, "invalid <![ sequence"))
			}
		}
		start := p + len("CDATA[")
		i := bytes.Index(d[start:], cdataEnd)
		if i < 0 {
			return s.fail(s.syntaxError(len(d), "unexpected EOF in CDATA section"))
		}
		end := start + i
		if !s.validChars {
			if err := s.checkChars(d[start:end], start); err != nil {
				return s.fail(err)
			}
		}
		s.text = span{start, end}
		s.cdata = true
		s.pos = end + len(cdataEnd)
		return CharData, nil
	}
	return s.directive(p)
}

// directive skips <!DOCTYPE …> and the like with Decoder's rules: quoted
// '>' does not end it, nested <…> are counted, and <!-- … --> inside is
// skipped.
func (s *Scanner) directive(p int) (Kind, error) {
	d := s.data
	p++ // Decoder keeps the first byte without looking at it.
	var inquote byte
	depth := 0
	for {
		if p >= len(d) {
			return s.eof()
		}
		b := d[p]
		p++
		if inquote == 0 && b == '>' && depth == 0 {
			break
		}
	handle:
		switch {
		case b == inquote:
			inquote = 0
		case inquote != 0:
		case b == '\'' || b == '"':
			inquote = b
		case b == '>' && inquote == 0:
			depth--
		case b == '<' && inquote == 0:
			const open = "!--"
			for i := 0; i < len(open); i++ {
				if p >= len(d) {
					return s.eof()
				}
				b = d[p]
				p++
				if b != open[i] {
					depth++
					goto handle
				}
			}
			var b0, b1 byte
			for {
				if p >= len(d) {
					return s.eof()
				}
				b = d[p]
				p++
				if b0 == '-' && b1 == '-' && b == '>' {
					break
				}
				b0, b1 = b1, b
			}
		}
	}
	s.pos = p
	return 0, nil
}

// entityAt reads the reference that starts at b[i] == '&' the way
// Decoder.text does. end is where scanning resumes. ok reports a decoded
// reference and repl is its text; otherwise b[i:end] stays as written. eof
// reports a reference cut off by the end of the input, which Decoder rejects.
// When atEOF is false the byte after b is '<' or a quote, which ends any
// reference.
func entityAt(b []byte, i int, atEOF bool) (end int, repl string, ok, eof bool) {
	j := i + 1
	if j >= len(b) {
		return j, "", false, atEOF
	}
	if b[j] == '#' {
		j++
		if j >= len(b) {
			return j, "", false, atEOF
		}
		hex := b[j] == 'x'
		if hex {
			j++
			if j >= len(b) {
				return j, "", false, atEOF
			}
		}
		start := j
		var n uint64
		big := false
		for j < len(b) {
			v, isDigit := digitVal(b[j], hex)
			if !isDigit {
				break
			}
			if !big {
				if hex {
					n = n<<4 | v
				} else {
					n = n*10 + v
				}
				if n > unicode.MaxRune {
					big = true
				}
			}
			j++
		}
		if j >= len(b) {
			return j, "", false, atEOF
		}
		if b[j] != ';' {
			return j, "", false, false
		}
		if j == start || big {
			return j + 1, "", false, false
		}
		return j + 1, string(rune(n)), true, false
	}
	start := j
	for j < len(b) && (b[j] >= utf8.RuneSelf || isNameByte(b[j])) {
		j++
	}
	if j >= len(b) {
		return j, "", false, atEOF
	}
	if b[j] != ';' {
		return j, "", false, false
	}
	name := b[start:j]
	if v, ok := predefined[string(name)]; ok {
		return j + 1, v, true, false
	}
	if v, ok := xml.HTMLEntity[string(name)]; ok {
		return j + 1, v, true, false
	}
	return j + 1, "", false, false
}

var predefined = map[string]string{
	"lt":   "<",
	"gt":   ">",
	"amp":  "&",
	"apos": "'",
	"quot": `"`,
}

func digitVal(c byte, hex bool) (uint64, bool) {
	switch {
	case '0' <= c && c <= '9':
		return uint64(c - '0'), true
	case hex && 'a' <= c && c <= 'f':
		return uint64(c-'a') + 10, true
	case hex && 'A' <= c && c <= 'F':
		return uint64(c-'A') + 10, true
	}
	return 0, false
}

// decode appends b to dst with "\r\n" and "\r" turned into "\n" and, when
// entities is set, references decoded. b has already been checked, so no
// reference in it runs into the end of the input.
func decode(dst, b []byte, entities bool) []byte {
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c == '\r':
			dst = append(dst, '\n')
			i++
			if i < len(b) && b[i] == '\n' {
				i++
			}
		case c == '&' && entities:
			end, repl, ok, _ := entityAt(b, i, false)
			if ok {
				dst = append(dst, repl...)
			} else {
				dst = append(dst, b[i:end]...)
			}
			i = end
		default:
			j := i + 1
			for j < len(b) && b[j] != '\r' && (b[j] != '&' || !entities) {
				j++
			}
			dst = append(dst, b[i:j]...)
			i = j
		}
	}
	return dst
}

func isNameByte(c byte) bool {
	return 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || '0' <= c && c <= '9' ||
		c == '_' || c == ':' || c == '.' || c == '-'
}

func isUnquotedByte(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' ||
		c == '_' || c == ':' || c == '-'
}

// isName is Decoder's name check. An ASCII name must start with a letter,
// '_' or ':'. A name with other bytes is rare and is checked by Decoder
// itself, so the Unicode tables stay those of encoding/xml.
func isName(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	ascii := true
	for _, c := range b {
		if c >= utf8.RuneSelf {
			ascii = false
			break
		}
	}
	if ascii {
		c := b[0]
		return 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || c == '_' || c == ':'
	}
	// A processing-instruction target is read by Decoder.name, which is
	// the check wanted, without the colon rules of element names.
	dec := xml.NewDecoder(io.MultiReader(strings.NewReader("<?"), bytes.NewReader(b), strings.NewReader("?>")))
	_, err := dec.Token()
	return err == nil
}

func inCharRange(r rune) bool {
	return r == 0x09 || r == 0x0A || r == 0x0D ||
		r >= 0x20 && r <= 0xD7FF ||
		r >= 0xE000 && r <= 0xFFFD ||
		r >= 0x10000 && r <= 0x10FFFF
}

// legalChars reports whether every byte of b is part of valid UTF-8 for a
// character in the XML Char production. When it holds, Decoder's per-value
// check can only fail on a numeric reference, which checkEntities covers.
func legalChars(b []byte) bool {
	const (
		lo = 0x2020202020202020
		hi = 0x8080808080808080
	)
	for i := 0; i < len(b); {
		for i+8 <= len(b) {
			// No byte has the high bit set and none is below 0x20.
			w := binary.LittleEndian.Uint64(b[i:])
			if (w|(w-lo))&hi != 0 {
				break
			}
			i += 8
		}
		if i >= len(b) {
			break
		}
		c := b[i]
		if c < utf8.RuneSelf {
			if c < 0x20 && c != '\t' && c != '\n' && c != '\r' {
				return false
			}
			i++
			continue
		}
		r, size := utf8.DecodeRune(b[i:])
		if r == utf8.RuneError && size == 1 || r == 0xFFFE || r == 0xFFFF {
			return false
		}
		i += size
	}
	return true
}

// procInstParam is encoding/xml's procInst: the value of param="…" or
// param='…' in s, or "".
func procInstParam(param, s string) string {
	param = param + "="
	lenp := len(param)
	i := 0
	var sep byte
	for i < len(s) {
		sub := s[i:]
		k := strings.Index(sub, param)
		if k < 0 || lenp+k >= len(sub) {
			return ""
		}
		i += lenp + k + 1
		if c := sub[lenp+k]; c == '\'' || c == '"' {
			sep = c
			break
		}
	}
	if sep == 0 {
		return ""
	}
	j := strings.IndexByte(s[i:], sep)
	if j < 0 {
		return ""
	}
	return s[i : i+j]
}

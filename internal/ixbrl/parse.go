// Package ixbrl parses Companies House iXBRL and non-inline XBRL instance
// documents into long-format facts.
package ixbrl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/mrbrianevans/ch-xbrl/internal/fact"
	"github.com/mrbrianevans/ch-xbrl/internal/ixbrl/xmlscan"
)

// Well-known namespaces.
const (
	nsIXBRL  = "http://www.xbrl.org/2013/inlineXBRL"
	nsIXBRL1 = "http://www.xbrl.org/2008/inlineXBRL"
	nsXBRLI  = "http://www.xbrl.org/2003/instance"
	nsXBRLDI = "http://xbrl.org/2006/xbrldi"
	nsLink   = "http://www.xbrl.org/2003/linkbase"
	nsXLink  = "http://www.w3.org/1999/xlink"
	nsXMLNS  = "http://www.w3.org/2000/xmlns/"
	nsXML    = "http://www.w3.org/XML/1998/namespace"
	nsXHTML  = "http://www.w3.org/1999/xhtml"
)

type contextInfo struct {
	ID          string
	PeriodStart string
	PeriodEnd   string
	Dimensions  map[string]string // dimension local/qname → member local/qname
}

type unitInfo struct {
	ID      string
	Measure string
}

// continuationPart is one ix:continuation fragment in a continuedAt chain.
type continuationPart struct {
	Text        string
	ContinuedAt string
}

// joinContinuation concatenates base fact text with the continuedAt chain.
// Segments are joined without inserting separators (matches Arelle fact-list
// behaviour; presentation spaces outside ix tags are not part of the fact).
func joinContinuation(base, nextID string, conts map[string]*continuationPart) string {
	var b strings.Builder
	b.WriteString(base)
	seen := map[string]bool{}
	for nextID != "" && !seen[nextID] {
		seen[nextID] = true
		p, ok := conts[nextID]
		if !ok {
			break
		}
		b.WriteString(p.Text)
		nextID = p.ContinuedAt
	}
	return strings.TrimSpace(b.String())
}

// Parse reads an iXBRL/XHTML document and returns long-format facts.
// sourceFile is recorded on every row (archive member name).
func Parse(r io.Reader, sourceFile string) ([]fact.Fact, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return ParseBytes(data, sourceFile)
}

// ParseBytes is like Parse but from a byte slice.
func ParseBytes(data []byte, sourceFile string) ([]fact.Fact, error) {
	facts, _, err := parseBytes(data, sourceFile, "")
	return facts, err
}

// ParseBytesOnlyTarget is ParseBytes restricted to facts whose target
// attribute equals target. Facts with no target attribute are left out.
// Report packages pass "UKFRS" so the company's own statements are kept.
func ParseBytesOnlyTarget(data []byte, sourceFile, target string) ([]fact.Fact, error) {
	facts, _, err := parseBytes(data, sourceFile, target)
	return facts, err
}

// Document is one parsed instance. Corrections is how many fact values were
// adjusted for a known producer tagging error. Those rows are ordinary facts:
// the CSV schema does not mark them. ch-xbrl adds the count to its done line.
type Document struct {
	Facts       []fact.Fact
	Corrections int
}

// ParseDocument parses an iXBRL or XBRL instance.
func ParseDocument(data []byte, sourceFile string) (Document, error) {
	return parseDocument(data, sourceFile, "")
}

// ParseDocumentOnlyTarget is ParseDocument restricted to facts whose target
// attribute equals target. Facts with no target attribute are left out.
func ParseDocumentOnlyTarget(data []byte, sourceFile, target string) (Document, error) {
	return parseDocument(data, sourceFile, target)
}

func parseDocument(data []byte, sourceFile, onlyTarget string) (Document, error) {
	facts, n, err := parseBytes(data, sourceFile, onlyTarget)
	if err != nil {
		return Document{}, err
	}
	return Document{Facts: facts, Corrections: n}, nil
}

// scanners keeps tokenisers, and their buffers, between members. Nothing a
// fact holds points into a scanner: names and values are copied out.
var scanners = sync.Pool{New: func() any { return xmlscan.New(nil) }}

func parseBytes(data []byte, sourceFile, onlyTarget string) ([]fact.Fact, int, error) {
	// Strip BOM / leading junk before the first '<' (seen in older CH dumps).
	data = stripXMLPreamble(data)

	// xmlscan gives the tokens encoding/xml's Decoder gave with Strict
	// false, HTML entities, and an ASCII-only charset reader, and rejects
	// the same documents. ASCII is a subset of UTF-8, so encoding="ASCII"
	// is read as is and &#160; in a date is decoded (docs/edge-cases.md).
	// There is no HTML void-element auto-close: `<col> </col>` is an
	// ordinary element (docs/edge-cases.md).
	sc := scanners.Get().(*xmlscan.Scanner)
	sc.Reset(data)
	defer func() {
		sc.Reset(nil)
		scanners.Put(sc)
	}()

	// prefix → namespace URI (document-level, updated as we go)
	ns := map[string]string{}
	contexts := map[string]*contextInfo{}
	units := map[string]*unitInfo{}

	// Active parse state for nested context/unit construction.
	var curCtx *contextInfo
	var curUnit *unitInfo
	var inPeriod bool
	var textBuf strings.Builder
	var captureText bool
	var lastDim string     // dimension attr of the most recent start element
	var explicitDim string // dimension attr while reading explicitMember text

	// ix:continuation fragments (id → text + next continuedAt).
	// Used to reassemble nonNumeric/nonFraction values split across the document.
	continuations := map[string]*continuationPart{}

	// Facts collected as we stream (ix:nonFraction / ix:nonNumeric).
	type pendingFact struct {
		Name        string
		ContextRef  string
		UnitRef     string
		Namespace   string
		Scale       string
		Sign        string
		Format      string
		Decimals    string
		IsNumeric   bool
		Value       string
		ContinuedAt string // head of ix:continuation chain, if any
		Target      string // ix:target; empty when the attribute is absent
	}
	// Open facts, continuations, and excludes share one stack. Text is
	// written only to the innermost frame. On close, that text is stored
	// and copied onto the parent unless the parent is ix:exclude.
	type openKind int
	const (
		openFact openKind = iota
		openContinuation
		openExclude
	)
	type openFrame struct {
		kind     openKind
		fact     pendingFact
		contID   string
		contNext string
		buf      strings.Builder
		space    string
		local    string
	}
	var facts []pendingFact
	var openStack []*openFrame

	for {
		kind, err := sc.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			// A document encoding/xml would reject fails the member. There
			// is no second parser to fall back to.
			return nil, 0, errSyntax(sourceFile, err)
		}

		switch kind {
		case xmlscan.StartElement:
			mergeNS(ns, sc)
			// local aliases the input; string(local) == "…" does not allocate.
			local := sc.Local()
			space := sc.Space()
			ix := isIX(space)
			lastDim = sc.Attr("dimension")

			// ix:exclude hides its text from ancestor facts and continuations.
			// A fact nested inside it is still its own frame.
			if ix && string(local) == "exclude" {
				openStack = append(openStack, &openFrame{kind: openExclude, space: space, local: string(local)})
				continue
			}

			switch {
			case isNS(space, nsXBRLI) && string(local) == "context":
				curCtx = &contextInfo{
					ID:         sc.Attr("id"),
					Dimensions: map[string]string{},
				}

			case isNS(space, nsXBRLI) && string(local) == "period":
				inPeriod = true

			case isNS(space, nsXBRLDI) && string(local) == "explicitMember" && curCtx != nil:
				explicitDim = lastDim
				captureText = true
				textBuf.Reset()

			case isNS(space, nsXBRLDI) && string(local) == "typedMember" && curCtx != nil:
				// typed members: dimension attr + child element text
				explicitDim = lastDim
				captureText = true
				textBuf.Reset()

			case isNS(space, nsXBRLI) && (string(local) == "startDate" ||
				string(local) == "endDate" || string(local) == "instant" || string(local) == "measure"):
				captureText = true
				textBuf.Reset()

			case isNS(space, nsXBRLI) && string(local) == "unit":
				curUnit = &unitInfo{ID: sc.Attr("id")}

			case ix && (string(local) == "nonFraction" || string(local) == "nonNumeric"):
				qname := sc.Attr("name")
				nsURI, badPrefix := inlineNamespace(qname, ns)
				if badPrefix != "" {
					return nil, 0, errUndeclaredPrefix(sourceFile, qnameLocal(qname), badPrefix)
				}
				pf := pendingFact{
					Name:        qname,
					ContextRef:  sc.Attr("contextRef"),
					UnitRef:     sc.Attr("unitRef"),
					Namespace:   nsURI,
					Scale:       sc.Attr("scale"),
					Sign:        sc.Attr("sign"),
					Format:      sc.Attr("format"),
					Decimals:    sc.Attr("decimals"),
					IsNumeric:   string(local) == "nonFraction",
					ContinuedAt: sc.Attr("continuedAt"),
					Target:      sc.Attr("target"),
				}
				// A nilled or empty element is still a fact. Its value is
				// whatever text it holds when it closes.
				// Nested ix:nonNumeric / ix:nonFraction (Workiva wraps one
				// fact inside another). Each layer is a separate fact.
				openStack = append(openStack, &openFrame{kind: openFact, fact: pf, space: space, local: string(local)})

			case isClassicItem(space, sc):
				// Non-inline XBRL: an item is an element with contextRef
				// (tuple wrappers have no contextRef and are not facts).
				// The scanner has already resolved the prefix. A blank
				// prefix is the in-scope default xmlns. An unbound prefix
				// is left as the prefix string.
				nsURI, badPrefix := classicNamespace(space, ns)
				if badPrefix != "" {
					return nil, 0, errUndeclaredPrefix(sourceFile, string(local), badPrefix)
				}
				unitRef := sc.Attr("unitRef")
				decimals := sc.Attr("decimals")
				pf := pendingFact{
					Name:       string(local),
					ContextRef: sc.Attr("contextRef"),
					UnitRef:    unitRef,
					Namespace:  nsURI,
					Scale:      sc.Attr("scale"),
					Sign:       sc.Attr("sign"),
					Format:     sc.Attr("format"),
					Decimals:   decimals,
					IsNumeric:  unitRef != "" || decimals != "" || sc.Attr("precision") != "",
				}
				openStack = append(openStack, &openFrame{kind: openFact, fact: pf, space: space, local: string(local)})

			case ix && string(local) == "continuation":
				// Continuation of a prior fact (continuedAt chain).
				// Nested continuations each keep their own frame.
				openStack = append(openStack, &openFrame{
					kind:     openContinuation,
					contID:   sc.Attr("id"),
					contNext: sc.Attr("continuedAt"),
					space:    space,
					local:    string(local),
				})
			}

		case xmlscan.CharData:
			// Text outside any capture is never decoded.
			if n := len(openStack); n > 0 {
				openStack[n-1].buf.Write(sc.Text())
			} else if captureText {
				textBuf.Write(sc.Text())
			}

		case xmlscan.EndElement:
			local := sc.Local()
			space := sc.Space()
			if n := len(openStack); n > 0 && openStack[n-1].local == string(local) && openStack[n-1].space == space {
				fr := openStack[n-1]
				openStack = openStack[:n-1]
				raw := fr.buf.String()
				bubble := raw
				switch fr.kind {
				case openFact:
					fr.fact.Value = raw
					facts = append(facts, fr.fact)
				case openContinuation:
					if fr.contID != "" {
						continuations[fr.contID] = &continuationPart{
							Text:        raw,
							ContinuedAt: fr.contNext,
						}
					}
				case openExclude:
					// Excluded text, including a nested fact already stored,
					// does not belong to ancestor facts or continuations.
					bubble = ""
				}
				if bubble != "" && len(openStack) > 0 && openStack[len(openStack)-1].kind != openExclude {
					openStack[len(openStack)-1].buf.WriteString(bubble)
				}
				if fr.kind != openFact {
					continue
				}
			}
			text := strings.TrimSpace(textBuf.String())

			switch {
			case isNS(space, nsXBRLI) && string(local) == "context":
				if curCtx != nil && curCtx.ID != "" {
					contexts[curCtx.ID] = curCtx
				}
				curCtx = nil
				inPeriod = false
				explicitDim = ""

			case isNS(space, nsXBRLI) && string(local) == "period":
				inPeriod = false

			case isNS(space, nsXBRLI) && string(local) == "startDate" && curCtx != nil && inPeriod:
				curCtx.PeriodStart = text
			case isNS(space, nsXBRLI) && string(local) == "endDate" && curCtx != nil && inPeriod:
				curCtx.PeriodEnd = text
			case isNS(space, nsXBRLI) && string(local) == "instant" && curCtx != nil && inPeriod:
				// Instant: mirror date into both bounds (matches common wide-row grouping).
				curCtx.PeriodEnd = text
				curCtx.PeriodStart = text

			case isNS(space, nsXBRLDI) && string(local) == "explicitMember" && curCtx != nil:
				dim := explicitDim
				if dim == "" {
					dim = lastDim
				}
				if dim != "" {
					curCtx.Dimensions[qnameLocal(dim)] = qnameLocal(text)
				}
				explicitDim = ""

			case isNS(space, nsXBRLDI) && string(local) == "typedMember" && curCtx != nil:
				dim := explicitDim
				if dim == "" {
					dim = lastDim
				}
				if dim != "" {
					val := text
					if val == "" {
						val = strings.TrimSpace(textBuf.String())
					}
					curCtx.Dimensions[qnameLocal(dim)] = strings.TrimSpace(val)
				}
				explicitDim = ""

			case isNS(space, nsXBRLI) && string(local) == "measure" && curUnit != nil:
				if curUnit.Measure == "" {
					curUnit.Measure = text
				} else {
					curUnit.Measure += "/" + text
				}

			case isNS(space, nsXBRLI) && string(local) == "unit":
				if curUnit != nil && curUnit.ID != "" {
					units[curUnit.ID] = curUnit
				}
				curUnit = nil
			}

			// A typed member's value is the child element's text. That child's
			// end tag is not </typedMember>, so keep the buffer until the
			// typed member itself ends. An explicit member's text is direct;
			// its own end tag still clears the buffer.
			keepMemberText := explicitDim != "" && !(isNS(space, nsXBRLDI) && (string(local) == "typedMember" || string(local) == "explicitMember"))
			if captureText && !keepMemberText && (!isIX(space) || (string(local) != "nonFraction" && string(local) != "nonNumeric")) {
				if len(openStack) == 0 {
					captureText = false
					textBuf.Reset()
				}
			}
		}
	}

	// Reassemble continued facts (ix:continuation chains).
	for i := range facts {
		if facts[i].ContinuedAt != "" {
			facts[i].Value = joinContinuation(facts[i].Value, facts[i].ContinuedAt, continuations)
		} else {
			facts[i].Value = strings.TrimSpace(facts[i].Value)
		}
	}

	var prodName, prodVersion string
	for i := range facts {
		noteProducerFact(qnameLocal(facts[i].Name), facts[i].Value, facts[i].Format, &prodName, &prodVersion)
	}
	// Known CCH headcount scale. Other producers are left spec-correct.
	gate := cchEmployeeProducer(prodName, prodVersion)
	corrections := 0

	out := make([]fact.Fact, 0, len(facts))
	for _, pf := range facts {
		if pf.Name == "" {
			continue
		}
		if onlyTarget != "" && pf.Target != onlyTarget {
			continue
		}
		ctx := contexts[pf.ContextRef]
		periodStart, periodEnd := "", ""
		dimsJSON := ""
		if ctx != nil {
			periodStart = ctx.PeriodStart
			periodEnd = ctx.PeriodEnd
			if len(ctx.Dimensions) > 0 {
				b, _ := json.Marshal(ctx.Dimensions)
				dimsJSON = string(b)
			}
		}

		unit := ""
		if pf.UnitRef != "" {
			if u, ok := units[pf.UnitRef]; ok {
				unit = u.Measure
				if unit == "" {
					unit = pf.UnitRef
				}
			} else {
				unit = pf.UnitRef
			}
		}

		val := pf.Value
		if pf.IsNumeric {
			scale, sign, fixed := adjustEmployeeValue(qnameLocal(pf.Name), pf.Scale, unit, pf.Value, pf.Format, pf.Sign, gate)
			if fixed {
				corrections++
			}
			val = normaliseNumeric(val, scale, sign, pf.Format)
		} else {
			var err error
			val, err = normaliseNonNumeric(val, pf.Format)
			if err != nil {
				return nil, 0, errDateTransform(sourceFile, qnameLocal(pf.Name), pf.Format, val, err)
			}
		}

		decimals := ""
		if pf.IsNumeric {
			decimals = pf.Decimals
		}
		out = append(out, fact.Fact{
			PeriodStart: periodStart,
			PeriodEnd:   periodEnd,
			Concept:     qnameLocal(pf.Name),
			Value:       val,
			Unit:        unit,
			Dimensions:  dimsJSON,
			Namespace:   pf.Namespace,
			SourceFile:  sourceFile,
			Decimals:    decimals,
		})
	}

	if len(out) == 0 {
		return nil, 0, errNoFacts(sourceFile)
	}
	return out, corrections, nil
}

// isIX reports an inline XBRL namespace. Most elements of a filing are
// XHTML, so that answer is checked first.
func isIX(space string) bool {
	if space == nsXHTML {
		return false
	}
	return space == nsIXBRL || space == nsIXBRL1 || space == "" ||
		strings.Contains(space, "inlineXBRL")
}

func isNS(space, want string) bool {
	return space == want
}

// mergeNS records xmlns declarations on the current start element.
// The scanner presents xmlns:prefix as Space="xmlns" Local=prefix, and a
// default xmlns as Space="" Local="xmlns". A prefixed declaration must not
// replace the default.
func mergeNS(ns map[string]string, sc *xmlscan.Scanner) {
	for i := 0; i < sc.NumAttr(); i++ {
		local := sc.AttrLocal(i)
		space := sc.AttrSpace(i)
		switch {
		case (space == "xmlns" || space == nsXMLNS) && len(local) != 0:
			ns[string(local)] = sc.AttrValue(i)
		case string(local) == "xmlns" && space == "":
			ns[""] = sc.AttrValue(i)
		}
	}
}

// inlineNamespace resolves an ix: name QName. A name with no prefix is empty
// and does not use the default xmlns. Clark notation {uri}local uses uri.
// The second result is the prefix when it is not declared.
func inlineNamespace(name string, ns map[string]string) (uri, undeclared string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ""
	}
	if strings.HasPrefix(name, "{") {
		if i := strings.Index(name, "}"); i > 1 && i < len(name)-1 {
			return name[1:i], ""
		}
	}
	prefix, _, found := strings.Cut(name, ":")
	if !found || prefix == "" {
		return "", ""
	}
	if prefix == "xml" {
		if declared, ok := ns["xml"]; ok {
			return declared, ""
		}
		return nsXML, ""
	}
	declared, ok := ns[prefix]
	if !ok {
		return "", prefix
	}
	return declared, ""
}

// classicNamespace accepts the element namespace encoding/xml already resolved.
// A blank prefix arrives as the in-scope default xmlns, or empty when none is
// in scope. An unbound prefix is left as the prefix string.
func classicNamespace(space string, ns map[string]string) (uri, undeclared string) {
	if space == "" || space == nsXML {
		return space, ""
	}
	for _, v := range ns {
		if v == space {
			return space, ""
		}
	}
	return "", space
}

func errUndeclaredPrefix(sourceFile, concept, prefix string) error {
	return fmt.Errorf("undeclared prefix %q on %s in %s", prefix, concept, sourceFile)
}

func errDateTransform(sourceFile, concept, format, text string, err error) error {
	return fmt.Errorf("date transform %s format %q value %q in %s: %v", concept, format, text, sourceFile, err)
}

func errSyntax(sourceFile string, err error) error {
	return fmt.Errorf("%w in %s", err, sourceFile)
}

func errNoFacts(sourceFile string) error {
	return fmt.Errorf("no facts extracted from %s", sourceFile)
}

// qnameLocal returns the local part of a QName (prefix:local or {uri}local).
func qnameLocal(q string) string {
	q = strings.TrimSpace(q)
	if q == "" {
		return ""
	}
	if i := strings.LastIndex(q, "}"); i >= 0 && i < len(q)-1 {
		return q[i+1:]
	}
	if i := strings.Index(q, ":"); i >= 0 && i < len(q)-1 {
		return q[i+1:]
	}
	return q
}

// isClassicItem reports a non-inline XBRL item: an element that carries
// contextRef and is not part of the instance, link, or inline-XBRL machinery.
// Tuple containers omit contextRef, so they are not facts.
func isClassicItem(space string, sc *xmlscan.Scanner) bool {
	if sc.Attr("contextRef") == "" {
		return false
	}
	if space == nsIXBRL || space == nsIXBRL1 || strings.Contains(space, "inlineXBRL") {
		return false
	}
	switch space {
	case nsXBRLI, nsLink, nsXLink, nsXBRLDI, nsXMLNS, nsXHTML,
		"http://www.w3.org/2001/XMLSchema-instance",
		"http://www.w3.org/2001/XMLSchema",
		"http://xbrl.org/2005/xbrldt",
		"http://www.w3.org/2005/xpath-functions",
		"http://www.xbrl.org/2003/iso4217":
		return false
	}
	return true
}

func stripXMLPreamble(data []byte) []byte {
	// UTF-8 BOM
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	// Older dumps sometimes have junk before the first tag.
	if i := bytes.IndexByte(data, '<'); i > 0 {
		data = data[i:]
	}
	return data
}

func formatLocal(format string) string {
	format = strings.ToLower(strings.TrimSpace(format))
	if i := strings.LastIndex(format, ":"); i >= 0 {
		return format[i+1:]
	}
	return format
}

func normaliseNonNumeric(val, format string) (string, error) {
	fl := formatLocal(format)
	switch fl {
	case "booleantrue":
		return "true", nil
	case "booleanfalse":
		return "false", nil
	case "nocontent":
		return "", nil
	}
	// Collapse whitespace. Quotes in the filer's text stay; the CSV writer
	// escapes a field that contains one. Space separators that unicode.IsSpace
	// does not treat as space (thin space, narrow no-break space) become an
	// ASCII space first, so date formats that require one still match.
	val = strings.Join(strings.Fields(foldSpaces(val)), " ")
	out, err := transformDate(fl, val)
	if err != nil {
		return val, err
	}
	return out, nil
}

// humanNumericPrefixRE strips junk like "2017 - 2" or "employees: 12" → residual number part.
var humanNumericPrefixRE = regexp.MustCompile(`(?i)(.*:)|(.+-\s)`)

func normaliseNumeric(val, scale, sign, format string) string {
	fl := formatLocal(format)
	v := strings.TrimSpace(foldSpaces(val))

	// Null / dash markers (incl. unicode dashes)
	if v == "" || v == "-" || v == "\u002d" || v == "\u2013" || v == "\u2014" || fl == "zerodash" {
		if v == "-" || v == "\u002d" || v == "\u2013" || v == "\u2014" || fl == "zerodash" {
			v = "0"
		} else {
			return ""
		}
	}

	// Strip human-readable prefixes sometimes embedded in tagged text ("2017 - 2").
	if cleaned := strings.TrimSpace(humanNumericPrefixRE.ReplaceAllString(v, "")); cleaned != "" && cleaned != v {
		// Only apply when residual looks numeric
		if looksNumeric(cleaned) {
			v = cleaned
		}
	}

	// iXT format transforms for thousands/decimal separators.
	switch fl {
	case "numdotcomma":
		// 1.234,56 → 1234.56
		v = strings.ReplaceAll(v, ".", "")
		v = strings.ReplaceAll(v, ",", ".")
	case "numspacedot", "numspacecomma":
		v = strings.ReplaceAll(v, " ", "")
		v = strings.ReplaceAll(v, "\u00a0", "")
		if fl == "numspacecomma" {
			v = strings.ReplaceAll(v, ",", ".")
		}
	case "numcommadot", "numdotdecimal", "numcommadecimal", "":
		// default: remove thousands commas / spaces
		v = strings.ReplaceAll(v, ",", "")
		v = strings.ReplaceAll(v, " ", "")
		v = strings.ReplaceAll(v, "\u00a0", "")
	default:
		v = strings.ReplaceAll(v, ",", "")
		v = strings.ReplaceAll(v, " ", "")
		v = strings.ReplaceAll(v, "\u00a0", "")
	}

	// Multi-token numbers occasionally appear as space-separated addends; sum them
	// only when every token is numeric (after separator cleanup above spaces are gone).
	// (Handled below as a single token.)

	// Parentheses for negatives: (123)
	neg := sign == "-"
	if strings.HasPrefix(v, "(") && strings.HasSuffix(v, ")") {
		neg = true
		v = v[1 : len(v)-1]
	}
	if strings.HasPrefix(v, "-") {
		neg = !neg
		v = strings.TrimPrefix(v, "-")
	}
	v = strings.TrimPrefix(v, "+")

	if v == "" {
		return ""
	}

	// Apply scale: value * 10^scale
	sc := 0
	if scale != "" {
		if n, err := strconv.Atoi(scale); err == nil {
			sc = n
		}
	}

	// Use big.Rat for precision
	r := new(big.Rat)
	if _, ok := r.SetString(v); !ok {
		// not a plain number — return cleaned string with sign
		if neg {
			return "-" + v
		}
		return v
	}
	if sc != 0 {
		// multiply by 10^sc
		exp := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(abs(sc))), nil)
		factor := new(big.Rat).SetInt(exp)
		if sc > 0 {
			r.Mul(r, factor)
		} else {
			r.Quo(r, factor)
		}
	}
	if neg {
		r.Neg(r)
	}

	// Emit as plain decimal without unnecessary trailing zeros where possible
	s := r.FloatString(10)
	s = trimTrailingZeros(s)
	return s
}

func looksNumeric(s string) bool {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "-")
	s = strings.TrimPrefix(s, "+")
	s = strings.ReplaceAll(s, ",", "")
	s = strings.ReplaceAll(s, " ", "")
	if s == "" {
		return false
	}
	_, ok := new(big.Rat).SetString(s)
	return ok
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func trimTrailingZeros(s string) string {
	if !strings.Contains(s, ".") {
		return s
	}
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" || s == "-" {
		return "0"
	}
	return s
}

// foldSpaces maps Unicode space separators, and line and paragraph
// separators, to an ASCII space. unicode.IsSpace already covers U+00A0.
func foldSpaces(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\u2028' || r == '\u2029' || unicode.Is(unicode.Zs, r) {
			return ' '
		}
		return r
	}, s)
}

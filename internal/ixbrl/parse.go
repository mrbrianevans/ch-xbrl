// Package ixbrl parses Companies House inline XBRL (iXBRL) instance documents
// into long-format facts.
package ixbrl

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"math/big"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/mrbrianevans/ch-xbrl/internal/fact"
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
)

type contextInfo struct {
	ID          string
	CompanyID   string
	PeriodStart string
	PeriodEnd   string
	Dimensions  map[string]string // dimension local/qname → member local/qname
}

type unitInfo struct {
	ID      string
	Measure string
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
	// Strip BOM / leading junk before the first '<' (seen in older CH dumps).
	data = stripXMLPreamble(data)

	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	dec.AutoClose = xml.HTMLAutoClose
	dec.Entity = xml.HTMLEntity

	// prefix → namespace URI (document-level, updated as we go)
	ns := map[string]string{}
	contexts := map[string]*contextInfo{}
	units := map[string]*unitInfo{}
	var schemaRefs []string

	// Active parse state for nested context/unit construction.
	var curCtx *contextInfo
	var curUnit *unitInfo
	var inPeriod, inEntity bool
	var textBuf strings.Builder
	var captureText bool
	var excludeDepth int // skip text under ix:exclude (iXBRL)
	var lastStart xml.StartElement
	var explicitDim string // dimension attr while reading explicitMember text

	// Facts collected as we stream (ix:nonFraction / ix:nonNumeric).
	type pendingFact struct {
		Name       string
		ContextRef string
		UnitRef    string
		Scale      string
		Sign       string
		Format     string
		Decimals   string
		IsNumeric  bool
		Value      string
	}
	var facts []pendingFact
	var curFact *pendingFact

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			// Recover: many CH files are not well-formed XML; try a lenient pass.
			return parseLenient(data, sourceFile)
		}

		switch t := tok.(type) {
		case xml.StartElement:
			mergeNS(ns, t)
			local := t.Name.Local
			space := t.Name.Space
			lastStart = t

			// ix:exclude content must not contribute to fact text.
			if isIX(space) && local == "exclude" {
				excludeDepth++
				continue
			}

			switch {
			case isNS(space, nsLink) && local == "schemaRef":
				if href := attr(t, nsXLink, "href"); href != "" {
					schemaRefs = append(schemaRefs, href)
				} else if href := attrAny(t, "href"); href != "" {
					schemaRefs = append(schemaRefs, href)
				}

			case isNS(space, nsXBRLI) && local == "context":
				curCtx = &contextInfo{
					ID:         attrAny(t, "id"),
					Dimensions: map[string]string{},
				}

			case isNS(space, nsXBRLI) && local == "entity":
				inEntity = true
			case isNS(space, nsXBRLI) && local == "period":
				inPeriod = true

			case isNS(space, nsXBRLDI) && local == "explicitMember" && curCtx != nil:
				explicitDim = attrAny(t, "dimension")
				captureText = true
				textBuf.Reset()
				lastStart = t

			case isNS(space, nsXBRLDI) && local == "typedMember" && curCtx != nil:
				// typed members: dimension attr + child element text
				explicitDim = attrAny(t, "dimension")
				captureText = true
				textBuf.Reset()
				lastStart = t

			case isNS(space, nsXBRLI) && (local == "identifier" || local == "startDate" ||
				local == "endDate" || local == "instant" || local == "measure"):
				captureText = true
				textBuf.Reset()

			case isNS(space, nsXBRLI) && local == "unit":
				curUnit = &unitInfo{ID: attrAny(t, "id")}

			case isIX(space) && (local == "nonFraction" || local == "nonNumeric"):
				pf := &pendingFact{
					Name:       attrAny(t, "name"),
					ContextRef: attrAny(t, "contextRef"),
					UnitRef:    attrAny(t, "unitRef"),
					Scale:      attrAny(t, "scale"),
					Sign:       attrAny(t, "sign"),
					Format:     attrAny(t, "format"),
					Decimals:   attrAny(t, "decimals"),
					IsNumeric:  local == "nonFraction",
				}
				// nilled / empty elements still count as facts
				if hasAttr(t, "xsi", "nil") || hasAttrAny(t, "nil") {
					pf.Value = ""
				}
				curFact = pf
				captureText = true
				textBuf.Reset()
			}

		case xml.CharData:
			if captureText && excludeDepth == 0 {
				textBuf.Write(t)
			}

		case xml.EndElement:
			local := t.Name.Local
			space := t.Name.Space
			if isIX(space) && local == "exclude" {
				if excludeDepth > 0 {
					excludeDepth--
				}
				continue
			}
			text := strings.TrimSpace(textBuf.String())

			switch {
			case isNS(space, nsXBRLI) && local == "context":
				if curCtx != nil && curCtx.ID != "" {
					contexts[curCtx.ID] = curCtx
				}
				curCtx = nil
				inEntity, inPeriod = false, false
				explicitDim = ""

			case isNS(space, nsXBRLI) && local == "entity":
				inEntity = false
			case isNS(space, nsXBRLI) && local == "period":
				inPeriod = false

			case isNS(space, nsXBRLI) && local == "identifier" && curCtx != nil && inEntity:
				if curCtx.CompanyID == "" {
					curCtx.CompanyID = text
				}
			case isNS(space, nsXBRLI) && local == "startDate" && curCtx != nil && inPeriod:
				curCtx.PeriodStart = text
			case isNS(space, nsXBRLI) && local == "endDate" && curCtx != nil && inPeriod:
				curCtx.PeriodEnd = text
			case isNS(space, nsXBRLI) && local == "instant" && curCtx != nil && inPeriod:
				// Instant: mirror date into both bounds (matches common wide-row grouping).
				curCtx.PeriodEnd = text
				curCtx.PeriodStart = text

			case isNS(space, nsXBRLDI) && local == "explicitMember" && curCtx != nil:
				dim := explicitDim
				if dim == "" {
					dim = attrAny(lastStart, "dimension")
				}
				if dim != "" {
					curCtx.Dimensions[qnameLocal(dim)] = qnameLocal(text)
				}
				explicitDim = ""

			case isNS(space, nsXBRLDI) && local == "typedMember" && curCtx != nil:
				dim := explicitDim
				if dim == "" {
					dim = attrAny(lastStart, "dimension")
				}
				if dim != "" {
					val := text
					if val == "" {
						val = strings.TrimSpace(textBuf.String())
					}
					curCtx.Dimensions[qnameLocal(dim)] = strings.TrimSpace(val)
				}
				explicitDim = ""

			case isNS(space, nsXBRLI) && local == "measure" && curUnit != nil:
				if curUnit.Measure == "" {
					curUnit.Measure = text
				} else {
					curUnit.Measure += "/" + text
				}

			case isNS(space, nsXBRLI) && local == "unit":
				if curUnit != nil && curUnit.ID != "" {
					units[curUnit.ID] = curUnit
				}
				curUnit = nil

			case isIX(space) && (local == "nonFraction" || local == "nonNumeric") && curFact != nil:
				curFact.Value = strings.TrimSpace(textBuf.String())
				facts = append(facts, *curFact)
				curFact = nil
			}

			if captureText && !(isIX(space) && (local == "nonFraction" || local == "nonNumeric")) {
				// keep capturing until fact ends; for other fields stop
				if curFact == nil {
					captureText = false
					textBuf.Reset()
				}
			}
			if isIX(space) && (local == "nonFraction" || local == "nonNumeric") {
				captureText = false
				textBuf.Reset()
			}
		}
	}

	taxonomy := ""
	if len(schemaRefs) > 0 {
		taxonomy = schemaRefs[0]
	}

	// Fallback company id from filename (e.g. 03024914_aa_2023-03-13.xhtml)
	fileCompany := companyFromFilename(sourceFile)

	out := make([]fact.Fact, 0, len(facts))
	for _, pf := range facts {
		if pf.Name == "" {
			continue
		}
		ctx := contexts[pf.ContextRef]
		company := fileCompany
		periodStart, periodEnd := "", ""
		dimsJSON := ""
		if ctx != nil {
			if ctx.CompanyID != "" {
				company = ctx.CompanyID
			}
			periodStart = ctx.PeriodStart
			periodEnd = ctx.PeriodEnd
			if len(ctx.Dimensions) > 0 {
				b, _ := json.Marshal(ctx.Dimensions)
				dimsJSON = string(b)
			}
		}
		if company == "" {
			// last resort: UKCompaniesHouseRegisteredNumber fact is handled later
			// as a normal fact; leave empty for now
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
			val = normaliseNumeric(val, pf.Scale, pf.Sign, pf.Format)
		} else {
			val = normaliseNonNumeric(val, pf.Format)
		}

		out = append(out, fact.Fact{
			CompanyID:   company,
			PeriodStart: periodStart,
			PeriodEnd:   periodEnd,
			Concept:     qnameLocal(pf.Name),
			Value:       val,
			Unit:        unit,
			Dimensions:  dimsJSON,
			Taxonomy:    taxonomy,
			SourceFile:  sourceFile,
		})
	}

	// Backfill company_id from registered-number facts if missing.
	regNo := ""
	for _, f := range out {
		if f.Concept == "UKCompaniesHouseRegisteredNumber" && f.Value != "" {
			regNo = f.Value
			break
		}
	}
	if regNo != "" {
		for i := range out {
			if out[i].CompanyID == "" {
				out[i].CompanyID = regNo
			}
		}
	}

	return out, nil
}

func isIX(space string) bool {
	return space == nsIXBRL || space == nsIXBRL1 || space == "" ||
		strings.Contains(space, "inlineXBRL")
}

func isNS(space, want string) bool {
	return space == want
}

func mergeNS(ns map[string]string, se xml.StartElement) {
	for _, a := range se.Attr {
		if a.Name.Space == "xmlns" || (a.Name.Space == "" && a.Name.Local == "xmlns") {
			ns[""] = a.Value
		} else if a.Name.Space == "xmlns" || a.Name.Space == nsXMLNS {
			ns[a.Name.Local] = a.Value
		} else if a.Name.Local == "xmlns" {
			ns[""] = a.Value
		}
		// encoding/xml puts xmlns:foo as Space="xmlns" Local="foo" or Space="" Local with prefix
		if a.Name.Space == "xmlns" {
			ns[a.Name.Local] = a.Value
		}
	}
}

func attr(se xml.StartElement, space, local string) string {
	for _, a := range se.Attr {
		if a.Name.Local == local && (space == "" || a.Name.Space == space) {
			return a.Value
		}
	}
	return ""
}

func attrAny(se xml.StartElement, local string) string {
	for _, a := range se.Attr {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

func hasAttr(se xml.StartElement, space, local string) bool {
	return attr(se, space, local) != ""
}

func hasAttrAny(se xml.StartElement, local string) bool {
	return attrAny(se, local) != ""
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

// Filename patterns seen in Companies House dumps:
//   - {company}_{type}_{date}.xhtml   e.g. 03024914_aa_2023-03-13.xhtml
//   - Prod{run}_{batch}_{company}_{yyyymmdd}.{html|xml|xhtml}
//     e.g. Prod223_4203_00134794_20250927.html
//   - Optional LLP / SC / NI style ids: SC123456, NI123456, etc.
var (
	// Prefer Prod* first so the middle company field is not confused with batch numbers.
	prodFileRE = regexp.MustCompile(`(?i)^Prod\d+_\d+_([A-Z]{0,2}\d{6,8})_\d{8}\.(html|htm|xml|xhtml|zip)$`)
	// Leading company id before underscore or extension.
	companyFileRE = regexp.MustCompile(`(?i)^([0-9]{6,8}|[A-Z]{2}[0-9]{6})[_\.]`)
	// Fallback: first 6–8 digit run in the basename (last resort).
	companyAnyRE = regexp.MustCompile(`(?i)(?:^|_)([0-9]{6,8}|[A-Z]{2}[0-9]{6})(?:[_\.]|$)`)
)

func companyFromFilename(name string) string {
	base := filepath.Base(name)
	if m := prodFileRE.FindStringSubmatch(base); len(m) > 1 {
		return m[1]
	}
	if m := companyFileRE.FindStringSubmatch(base); len(m) > 1 {
		return m[1]
	}
	if m := companyAnyRE.FindStringSubmatch(base); len(m) > 1 {
		return m[1]
	}
	return ""
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

func normaliseNonNumeric(val, format string) string {
	fl := formatLocal(format)
	switch fl {
	case "booleantrue":
		return "true"
	case "booleanfalse":
		return "false"
	case "nocontent":
		return ""
	}
	// Collapse whitespace / drop surrounding quotes (entity names etc.).
	val = strings.Join(strings.Fields(val), " ")
	val = strings.ReplaceAll(val, `"`, "")
	return val
}

// humanNumericPrefixRE strips junk like "2017 - 2" or "employees: 12" → residual number part.
var humanNumericPrefixRE = regexp.MustCompile(`(?i)(.*:)|(.+-\s)`)

func normaliseNumeric(val, scale, sign, format string) string {
	fl := formatLocal(format)
	v := strings.TrimSpace(val)

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
	if strings.HasPrefix(v, "+") {
		v = strings.TrimPrefix(v, "+")
	}

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

// --- Lenient fallback parser using regex when strict XML fails ----------------

var (
	reSchemaRef = regexp.MustCompile(`(?is)<[^>]*schemaRef[^>]+href=["']([^"']+)["']`)
	reContext   = regexp.MustCompile(`(?is)<(?:[\w.]+:)?context\s+[^>]*id=["']([^"']+)["'][^>]*>(.*?)</(?:[\w.]+:)?context>`)
	reIdentifier = regexp.MustCompile(`(?is)<(?:[\w.]+:)?identifier[^>]*>([^<]*)</(?:[\w.]+:)?identifier>`)
	reStartDate  = regexp.MustCompile(`(?is)<(?:[\w.]+:)?startDate[^>]*>([^<]*)</(?:[\w.]+:)?startDate>`)
	reEndDate    = regexp.MustCompile(`(?is)<(?:[\w.]+:)?endDate[^>]*>([^<]*)</(?:[\w.]+:)?endDate>`)
	reInstant    = regexp.MustCompile(`(?is)<(?:[\w.]+:)?instant[^>]*>([^<]*)</(?:[\w.]+:)?instant>`)
	reExplicit   = regexp.MustCompile(`(?is)<(?:[\w.]+:)?explicitMember\s+[^>]*dimension=["']([^"']+)["'][^>]*>([^<]*)</(?:[\w.]+:)?explicitMember>`)
	reTyped      = regexp.MustCompile(`(?is)<(?:[\w.]+:)?typedMember\s+[^>]*dimension=["']([^"']+)["'][^>]*>(.*?)</(?:[\w.]+:)?typedMember>`)
	reUnit       = regexp.MustCompile(`(?is)<(?:[\w.]+:)?unit\s+[^>]*id=["']([^"']+)["'][^>]*>(.*?)</(?:[\w.]+:)?unit>`)
	reMeasure    = regexp.MustCompile(`(?is)<(?:[\w.]+:)?measure[^>]*>([^<]*)</(?:[\w.]+:)?measure>`)
	reNonFraction = regexp.MustCompile(`(?is)<(?:[\w.]+:)?nonFraction\b([^>]*)>(.*?)</(?:[\w.]+:)?nonFraction>`)
	reNonNumeric  = regexp.MustCompile(`(?is)<(?:[\w.]+:)?nonNumeric\b([^>]*)>(.*?)</(?:[\w.]+:)?nonNumeric>`)
	reNonFractionEmpty = regexp.MustCompile(`(?is)<(?:[\w.]+:)?nonFraction\b([^>]*)/>`)
	reNonNumericEmpty  = regexp.MustCompile(`(?is)<(?:[\w.]+:)?nonNumeric\b([^>]*)/>`)
	reAttr = regexp.MustCompile(`(?i)([:\w]+)\s*=\s*["']([^"']*)["']`)
)

func parseLenient(data []byte, sourceFile string) ([]fact.Fact, error) {
	data = stripXMLPreamble(data)
	s := string(data)
	taxonomy := ""
	if m := reSchemaRef.FindStringSubmatch(s); len(m) > 1 {
		taxonomy = m[1]
	}

	contexts := map[string]*contextInfo{}
	for _, m := range reContext.FindAllStringSubmatch(s, -1) {
		id, body := m[1], m[2]
		ctx := &contextInfo{ID: id, Dimensions: map[string]string{}}
		if im := reIdentifier.FindStringSubmatch(body); len(im) > 1 {
			ctx.CompanyID = strings.TrimSpace(im[1])
		}
		if sm := reStartDate.FindStringSubmatch(body); len(sm) > 1 {
			ctx.PeriodStart = strings.TrimSpace(sm[1])
		}
		if em := reEndDate.FindStringSubmatch(body); len(em) > 1 {
			ctx.PeriodEnd = strings.TrimSpace(em[1])
		}
		if im := reInstant.FindStringSubmatch(body); len(im) > 1 {
			d := strings.TrimSpace(im[1])
			ctx.PeriodEnd = d
			ctx.PeriodStart = d
		}
		for _, dm := range reExplicit.FindAllStringSubmatch(body, -1) {
			ctx.Dimensions[qnameLocal(dm[1])] = qnameLocal(strings.TrimSpace(dm[2]))
		}
		for _, dm := range reTyped.FindAllStringSubmatch(body, -1) {
			// strip tags from typed body
			val := regexp.MustCompile(`<[^>]+>`).ReplaceAllString(dm[2], "")
			ctx.Dimensions[qnameLocal(dm[1])] = strings.TrimSpace(val)
		}
		contexts[id] = ctx
	}

	units := map[string]*unitInfo{}
	for _, m := range reUnit.FindAllStringSubmatch(s, -1) {
		id, body := m[1], m[2]
		u := &unitInfo{ID: id}
		var measures []string
		for _, mm := range reMeasure.FindAllStringSubmatch(body, -1) {
			measures = append(measures, strings.TrimSpace(mm[1]))
		}
		u.Measure = strings.Join(measures, "/")
		units[id] = u
	}

	fileCompany := companyFromFilename(sourceFile)
	var out []fact.Fact

	collect := func(attrs, body string, numeric bool) {
		am := parseAttrs(attrs)
		name := am["name"]
		if name == "" {
			return
		}
		ctxRef := am["contextref"]
		if ctxRef == "" {
			ctxRef = am["contextRef"]
		}
		// attrs keys are lowercased
		for k, v := range am {
			if strings.EqualFold(k, "contextRef") {
				ctxRef = v
			}
		}
		unitRef := ""
		for k, v := range am {
			if strings.EqualFold(k, "unitRef") {
				unitRef = v
			}
		}
		scale, sign, format := am["scale"], am["sign"], am["format"]

		// strip nested tags from body for text value
		val := stripTags(body)
		val = strings.TrimSpace(val)
		val = xmlUnescape(val)

		ctx := contexts[ctxRef]
		company := fileCompany
		ps, pe := "", ""
		dimsJSON := ""
		if ctx != nil {
			if ctx.CompanyID != "" {
				company = ctx.CompanyID
			}
			ps, pe = ctx.PeriodStart, ctx.PeriodEnd
			if len(ctx.Dimensions) > 0 {
				b, _ := json.Marshal(ctx.Dimensions)
				dimsJSON = string(b)
			}
		}
		unit := unitRef
		if u, ok := units[unitRef]; ok && u.Measure != "" {
			unit = u.Measure
		}
		if numeric {
			val = normaliseNumeric(val, scale, sign, format)
		} else {
			val = normaliseNonNumeric(val, format)
		}
		out = append(out, fact.Fact{
			CompanyID:   company,
			PeriodStart: ps,
			PeriodEnd:   pe,
			Concept:     qnameLocal(name),
			Value:       val,
			Unit:        unit,
			Dimensions:  dimsJSON,
			Taxonomy:    taxonomy,
			SourceFile:  sourceFile,
		})
	}

	for _, m := range reNonFraction.FindAllStringSubmatch(s, -1) {
		collect(m[1], m[2], true)
	}
	for _, m := range reNonFractionEmpty.FindAllStringSubmatch(s, -1) {
		collect(m[1], "", true)
	}
	for _, m := range reNonNumeric.FindAllStringSubmatch(s, -1) {
		collect(m[1], m[2], false)
	}
	for _, m := range reNonNumericEmpty.FindAllStringSubmatch(s, -1) {
		collect(m[1], "", false)
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("no facts extracted from %s", sourceFile)
	}

	regNo := ""
	for _, f := range out {
		if f.Concept == "UKCompaniesHouseRegisteredNumber" && f.Value != "" {
			regNo = f.Value
			break
		}
	}
	if regNo != "" {
		for i := range out {
			if out[i].CompanyID == "" {
				out[i].CompanyID = regNo
			}
		}
	}
	return out, nil
}

func parseAttrs(s string) map[string]string {
	m := map[string]string{}
	for _, a := range reAttr.FindAllStringSubmatch(s, -1) {
		key := a[1]
		if i := strings.Index(key, ":"); i >= 0 {
			key = key[i+1:]
		}
		m[strings.ToLower(key)] = a[2]
		// also keep original-case local
		m[key] = a[2]
	}
	return m
}

func stripTags(s string) string {
	// Drop ix:exclude blocks entirely (iXBRL presentation-only content).
	s = regexp.MustCompile(`(?is)<(?:[\w.]+:)?exclude\b[^>]*>.*?</(?:[\w.]+:)?exclude>`).ReplaceAllString(s, "")
	return regexp.MustCompile(`(?s)<[^>]*>`).ReplaceAllString(s, "")
}

func xmlUnescape(s string) string {
	r := strings.NewReplacer(
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", `"`,
		"&apos;", "'",
		"&#39;", "'",
		"&#34;", `"`,
	)
	return r.Replace(s)
}

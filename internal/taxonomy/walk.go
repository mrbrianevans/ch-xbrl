package taxonomy

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

const (
	xsdNS   = "http://www.w3.org/2001/XMLSchema"
	xbrliNS = "http://www.xbrl.org/2003/instance"
	xmlNS   = "http://www.w3.org/XML/1998/namespace"

	// maxSchemaBytes is the largest XSD member we will parse.
	// The pinned suites are well under this.
	maxSchemaBytes = 32 << 20
)

type nsFrame struct {
	xmlns    map[string]string
	local    string
	space    string
	targetNS string
}

// ScanZip reads every .xsd member and returns item and tuple concepts.
// Identical namespace + local name rows collapse to one. Different
// attributes are a ConflictError.
func ScanZip(path string) (map[string]placed, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return scanReader(&r.Reader, path)
}

func scanReader(r *zip.Reader, label string) (map[string]placed, error) {
	out := map[string]placed{}
	var sawXSD bool
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := strings.ReplaceAll(f.Name, "\\", "/")
		if strings.Contains(name, "..") {
			return nil, fmt.Errorf("%s: unsafe member %q", label, f.Name)
		}
		if !strings.HasSuffix(strings.ToLower(name), ".xsd") {
			continue
		}
		sawXSD = true
		if f.UncompressedSize64 > maxSchemaBytes {
			return nil, fmt.Errorf("%s: %s is larger than %d bytes", label, name, maxSchemaBytes)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("%s: %s: %w", label, name, err)
		}
		data, err := io.ReadAll(io.LimitReader(rc, maxSchemaBytes+1))
		_ = rc.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %s: %w", label, name, err)
		}
		if len(data) > maxSchemaBytes {
			return nil, fmt.Errorf("%s: %s is larger than %d bytes", label, name, maxSchemaBytes)
		}
		concepts, err := parseSchema(data, name)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", label, err)
		}
		where := label + ": " + name
		for _, c := range concepts {
			if err := addConcept(out, c, where); err != nil {
				return nil, err
			}
		}
	}
	if !sawXSD {
		return nil, fmt.Errorf("%s: no .xsd members", label)
	}
	return out, nil
}

func parseSchema(data []byte, name string) ([]Concept, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	root := map[string]string{"xml": xmlNS}
	stack := []nsFrame{{xmlns: root}}
	var out []Concept
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			parent := stack[len(stack)-1]
			fr := nsFrame{
				xmlns:    cloneNS(parent.xmlns),
				local:    t.Name.Local,
				space:    t.Name.Space,
				targetNS: parent.targetNS,
			}
			applyXMLNS(fr.xmlns, t.Attr)
			if t.Name.Local == "schema" && t.Name.Space == xsdNS {
				fr.targetNS = attrLocal(t.Attr, "targetNamespace")
			}
			if t.Name.Local == "element" && t.Name.Space == xsdNS && parent.local == "schema" && parent.space == xsdNS {
				c, ok, err := conceptFrom(t.Attr, fr)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", name, err)
				}
				if ok {
					out = append(out, c)
				}
			}
			stack = append(stack, fr)
		case xml.EndElement:
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	return out, nil
}

func conceptFrom(attrs []xml.Attr, fr nsFrame) (Concept, bool, error) {
	name := attrLocal(attrs, "name")
	if name == "" {
		return Concept{}, false, nil
	}
	subst := strings.TrimSpace(attrLocal(attrs, "substitutionGroup"))
	if subst == "" {
		return Concept{}, false, nil
	}
	sns, slocal, err := resolveQName(subst, fr.xmlns)
	if err != nil {
		return Concept{}, false, fmt.Errorf("element %s: %w", name, err)
	}
	if sns != xbrliNS || (slocal != "item" && slocal != "tuple") {
		return Concept{}, false, nil
	}
	var tns, tlocal string
	if typ := strings.TrimSpace(attrLocal(attrs, "type")); typ != "" {
		tns, tlocal, err = resolveQName(typ, fr.xmlns)
		if err != nil {
			return Concept{}, false, fmt.Errorf("element %s type: %w", name, err)
		}
	}
	return Concept{
		Namespace:                  fr.targetNS,
		LocalName:                  name,
		Abstract:                   attrLocal(attrs, "abstract"),
		TypeNamespace:              tns,
		TypeName:                   tlocal,
		SubstitutionGroupNamespace: sns,
		SubstitutionGroupName:      slocal,
		PeriodType:                 attrLocal(attrs, "periodType"),
		Balance:                    attrLocal(attrs, "balance"),
		Nillable:                   attrLocal(attrs, "nillable"),
	}, true, nil
}

func resolveQName(q string, xmlns map[string]string) (string, string, error) {
	prefix, local := "", q
	if i := strings.IndexByte(q, ':'); i >= 0 {
		prefix, local = q[:i], q[i+1:]
	}
	if local == "" {
		return "", "", fmt.Errorf("empty qname %q", q)
	}
	ns, ok := xmlns[prefix]
	if !ok || ns == "" {
		if prefix == "" {
			return "", local, nil
		}
		return "", "", fmt.Errorf("unbound prefix %q in %q", prefix, q)
	}
	return ns, local, nil
}

func applyXMLNS(dst map[string]string, attrs []xml.Attr) {
	for _, a := range attrs {
		switch {
		case a.Name.Space == "xmlns":
			dst[a.Name.Local] = a.Value
		case a.Name.Space == "" && a.Name.Local == "xmlns":
			dst[""] = a.Value
		}
	}
}

func attrLocal(attrs []xml.Attr, local string) string {
	for _, a := range attrs {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

func cloneNS(m map[string]string) map[string]string {
	out := make(map[string]string, len(m)+4)
	for k, v := range m {
		out[k] = v
	}
	return out
}

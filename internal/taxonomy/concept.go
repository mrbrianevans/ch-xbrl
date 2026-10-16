package taxonomy

import (
	"encoding/csv"
	"fmt"
	"io"
	"slices"
	"strings"
)

// CSVHeader is the concept lookup written by ch-xbrl-taxonomy.
// The key is namespace + local_name, the same pair as a fact row's
// namespace and concept columns. schemaRef is not a column.
var CSVHeader = []string{
	"namespace",
	"local_name",
	"abstract",
	"type_namespace",
	"type_name",
	"substitution_group_namespace",
	"substitution_group_name",
	"period_type",
	"balance",
	"nillable",
}

// Concept is one global schema element whose substitution group is
// xbrli:item or xbrli:tuple. Attribute strings are the schema text;
// a missing attribute is empty.
type Concept struct {
	Namespace                  string
	LocalName                  string
	Abstract                   string
	TypeNamespace              string
	TypeName                   string
	SubstitutionGroupNamespace string
	SubstitutionGroupName      string
	PeriodType                 string
	Balance                    string
	Nillable                   string
}

func (c Concept) key() string {
	return c.Namespace + "\x00" + c.LocalName
}

func (c Concept) record() []string {
	return []string{
		c.Namespace,
		c.LocalName,
		c.Abstract,
		c.TypeNamespace,
		c.TypeName,
		c.SubstitutionGroupNamespace,
		c.SubstitutionGroupName,
		c.PeriodType,
		c.Balance,
		c.Nillable,
	}
}

// ConflictError means one namespace + local name was declared twice
// with different attributes. The lookup cannot pick a row.
type ConflictError struct {
	Concept       Concept
	Existing      Concept
	Where         string
	ExistingWhere string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("concept %s %s: %s differ (%s vs %s)",
		e.Concept.Namespace, e.Concept.LocalName, diffFields(e.Existing, e.Concept), e.ExistingWhere, e.Where)
}

func diffFields(a, b Concept) string {
	var parts []string
	check := func(name, x, y string) {
		if x != y {
			parts = append(parts, fmt.Sprintf("%s %q vs %q", name, x, y))
		}
	}
	check("abstract", a.Abstract, b.Abstract)
	check("type_namespace", a.TypeNamespace, b.TypeNamespace)
	check("type_name", a.TypeName, b.TypeName)
	check("substitution_group_namespace", a.SubstitutionGroupNamespace, b.SubstitutionGroupNamespace)
	check("substitution_group_name", a.SubstitutionGroupName, b.SubstitutionGroupName)
	check("period_type", a.PeriodType, b.PeriodType)
	check("balance", a.Balance, b.Balance)
	check("nillable", a.Nillable, b.Nillable)
	if len(parts) == 0 {
		return "attributes"
	}
	return strings.Join(parts, ", ")
}

type placed struct {
	c     Concept
	where string
}

func addConcept(dst map[string]placed, c Concept, where string) error {
	k := c.key()
	prev, ok := dst[k]
	if !ok {
		dst[k] = placed{c: c, where: where}
		return nil
	}
	if prev.c != c {
		return &ConflictError{Concept: c, Existing: prev.c, Where: where, ExistingWhere: prev.where}
	}
	return nil
}

// WriteCSV writes a header and one row per concept, sorted by namespace
// then local name.
func WriteCSV(w io.Writer, concepts []Concept) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(CSVHeader); err != nil {
		return err
	}
	for _, c := range concepts {
		if err := cw.Write(c.record()); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func sortedConcepts(m map[string]placed) []Concept {
	out := make([]Concept, 0, len(m))
	for _, p := range m {
		out = append(out, p.c)
	}
	slices.SortFunc(out, func(a, b Concept) int {
		if a.Namespace != b.Namespace {
			return strings.Compare(a.Namespace, b.Namespace)
		}
		return strings.Compare(a.LocalName, b.LocalName)
	})
	return out
}
